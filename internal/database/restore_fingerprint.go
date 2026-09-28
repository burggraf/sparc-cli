package database

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/burggraf/sparc-cli/internal/tools"
)

var ErrRecoveryMismatch = errors.New("restored data does not match archive fingerprints")

func buildFingerprintVerificationSQL(fingerprints []RecoveryTableFingerprint) (string, error) {
	if !validTableFingerprints(fingerprints) {
		return "", ErrRecoveryProfile
	}
	var script strings.Builder
	script.WriteByte('\n')
	for index, fingerprint := range fingerprints {
		script.WriteString("\\echo SPARC_VERIFY_BEGIN ")
		script.WriteString(strconv.Itoa(index))
		script.WriteByte('\n')
		script.WriteString("COPY (SELECT ")
		for columnIndex, column := range fingerprint.Columns {
			if columnIndex != 0 {
				script.WriteString(", ")
			}
			script.WriteString(quotePGIdentifier(column))
		}
		script.WriteString(" FROM ONLY ")
		script.WriteString(quotePGIdentifier(fingerprint.Schema))
		script.WriteByte('.')
		script.WriteString(quotePGIdentifier(fingerprint.Name))
		script.WriteString(") TO STDOUT;\n\\echo SPARC_VERIFY_END ")
		script.WriteString(strconv.Itoa(index))
		script.WriteByte('\n')
	}
	script.WriteString("\\echo SPARC_VERIFY_DONE\n")
	return script.String(), nil
}

func quotePGIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

type restoreCommitGate struct {
	ready chan error
	once  sync.Once
}

func newRestoreCommitGate() *restoreCommitGate {
	return &restoreCommitGate{ready: make(chan error, 1)}
}

func (g *restoreCommitGate) allow() {
	g.once.Do(func() {
		g.ready <- nil
		close(g.ready)
	})
}

func (g *restoreCommitGate) cancel() {
	g.once.Do(func() {
		g.ready <- ErrRestore
		close(g.ready)
	})
}

func (g *restoreCommitGate) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	err, ok := <-g.ready
	if !ok || err == nil {
		return 0, io.EOF
	}
	return 0, err
}

type fingerprintVerificationSink struct {
	fingerprints []RecoveryTableFingerprint
	gate         *restoreCommitGate
	index        int
	line         []byte
	rowsLeft     uint64
	current      *copyTableFingerprint
	awaitingEnd  bool
	complete     bool
	failed       bool
}

func newFingerprintVerificationSink(fingerprints []RecoveryTableFingerprint, gate *restoreCommitGate) *fingerprintVerificationSink {
	return &fingerprintVerificationSink{fingerprints: fingerprints, gate: gate}
}

func (s *fingerprintVerificationSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	if ctx == nil || s.gate == nil || ctx.Err() != nil {
		if ctx == nil || s.gate == nil {
			return 0, s.reject()
		}
		return 0, ctx.Err()
	}
	for _, value := range data {
		if value == '\n' {
			if err := s.processLine(s.line); err != nil {
				return 0, err
			}
			s.line = s.line[:0]
			continue
		}
		if len(s.line) >= maxFingerprintCopyLineBytes {
			return 0, s.reject()
		}
		s.line = append(s.line, value)
	}
	return len(data), nil
}

func (s *fingerprintVerificationSink) reject() error {
	s.failed = true
	if s.gate != nil {
		s.gate.cancel()
	}
	return ErrRecoveryMismatch
}

func (s *fingerprintVerificationSink) processLine(line []byte) error {
	if s.complete {
		return s.reject()
	}
	// pg_dump's set_config prologue leaves blank tuples-only output before the first marker.
	if s.index == 0 && s.current == nil && len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	if s.current != nil && s.rowsLeft > 0 {
		if !s.current.addRow(line) {
			return s.reject()
		}
		s.rowsLeft--
		return nil
	}
	if s.awaitingEnd {
		if string(line) != fmt.Sprintf("SPARC_VERIFY_END %d", s.index) {
			return s.reject()
		}
		if s.current == nil || s.current.result().Fingerprint != s.fingerprints[s.index].Fingerprint {
			return s.reject()
		}
		s.index++
		s.current = nil
		s.awaitingEnd = false
		return nil
	}
	if s.index < len(s.fingerprints) {
		if string(line) != fmt.Sprintf("SPARC_VERIFY_BEGIN %d", s.index) {
			return s.reject()
		}
		fingerprint := s.fingerprints[s.index]
		s.current = &copyTableFingerprint{schema: fingerprint.Schema, name: fingerprint.Name, columns: fingerprint.Columns}
		s.rowsLeft = fingerprint.Rows
		s.awaitingEnd = true
		return nil
	}
	if string(line) != "SPARC_VERIFY_DONE" {
		return s.reject()
	}
	s.complete = true
	s.gate.allow()
	return nil
}

func (s *fingerprintVerificationSink) CloseContext(ctx context.Context) error {
	if s.failed {
		return ErrRecoveryMismatch
	}
	if ctx == nil || ctx.Err() != nil || len(s.line) != 0 || !s.complete || s.index != len(s.fingerprints) {
		return s.reject()
	}
	return nil
}

var _ tools.OutputSink = (*fingerprintVerificationSink)(nil)
