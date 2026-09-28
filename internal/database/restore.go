package database

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/platform"
	"github.com/burggraf/sparc-cli/internal/tools"
)

// RestoreRequest contains only the target, archive, and caller-declared empty scope.
type RestoreRequest struct {
	Target            ConnectionParams
	TargetPassword    []byte
	ArchivePath       string
	ArchivePassphrase string
	EmptyScope        EmptyTargetScopeV1
}

// Restore verifies the archive and target scope before streaming to pg_restore.
func Restore(ctx context.Context, request RestoreRequest) error {
	return restoreWith(ctx, request, defaultRecoveryOps())
}

func restoreWith(ctx context.Context, request RestoreRequest, ops recoveryOps) error {
	if ctx == nil || !validRecoveryConnection(request.Target, request.TargetPassword) || !validRecoveryArchivePath(request.ArchivePath) ||
		archive.ValidatePassphrase(request.ArchivePassphrase) != nil || !validEmptyTargetScope(request.EmptyScope) ||
		ops.verify == nil || ops.rootCert == nil || ops.observe == nil || ops.prepareTool == nil || ops.open == nil || ops.run == nil {
		return ErrRestore
	}
	manifest, err := ops.verify(request.ArchivePath, request.ArchivePassphrase)
	if err != nil || !validDatabaseDumpManifest(manifest) || ops.prepareTool(ctx, tools.PGRestore) != nil {
		return ErrRestore
	}
	component := manifest.Components[0]
	rootCert, err := ops.rootCert(request.Target)
	if err != nil || len(rootCert) == 0 || len(rootCert) > maxNativeRootBytes {
		return ErrRestore
	}
	schemas := append(append([]string(nil), request.EmptyScope.RequiredPresent...), request.EmptyScope.RequiredAbsent...)
	observation, err := ops.observe(ctx, request.Target, request.TargetPassword, schemas)
	if err != nil || CheckEmptyTargetV1(observation, request.EmptyScope) != nil {
		return ErrRestore
	}
	input, err := ops.open(request.ArchivePath, component, request.ArchivePassphrase)
	if err != nil || input == nil {
		return ErrRestore
	}
	result, runErr := ops.run(ctx, tools.RunRequest{
		Tool: tools.PGRestore, Mode: tools.ModeRestore,
		Connection: &tools.PGConnection{
			Host: request.Target.Host, Port: request.Target.Port, User: request.Target.User, Database: request.Target.Database,
			Password: request.TargetPassword, RootCertPEM: rootCert,
		},
		Input: input, InputLimit: uint64(component.Length),
		Timeout: databaseToolTimeout, CleanupTimeout: 5 * time.Second,
		StdoutLimit: maxToolStderrBytes, StderrLimit: maxToolStderrBytes, Stdout: discardToolOutput{},
	})
	closeErr := input.Close()
	if errors.Is(runErr, tools.ErrPayloadUnavailable) {
		return tools.ErrPayloadUnavailable
	}
	if runErr != nil || result.ExitCode != 0 || closeErr != nil {
		return ErrRestore
	}
	return nil
}

const restoreReplicationRole = "\nSET LOCAL session_replication_role = replica;\n"

func restoreSchemaDataWith(ctx context.Context, run func(context.Context, tools.RunRequest) (tools.RunResult, error), connection *tools.PGConnection, schema, data io.ReadCloser, schemaLength, dataLength uint64) error {
	return restoreSchemaDataAndVerifyWith(ctx, run, connection, schema, data, schemaLength, dataLength, nil)
}

func restoreSchemaDataAndVerifyWith(ctx context.Context, run func(context.Context, tools.RunRequest) (tools.RunResult, error), connection *tools.PGConnection, schema, data io.ReadCloser, schemaLength, dataLength uint64, fingerprints []RecoveryTableFingerprint) error {
	maxBytes := uint64(archive.MaxComponentBytes)
	prefixLength := uint64(len(restoreReplicationRole))
	if ctx == nil || run == nil || connection == nil || schema == nil || data == nil || schemaLength == 0 || dataLength == 0 || schemaLength > maxBytes-prefixLength || dataLength > maxBytes-prefixLength-schemaLength {
		_ = closeRestoreSQLInputs(schema, data)
		return ErrRestore
	}
	verificationSQL := ""
	var gate *restoreCommitGate
	var stdout tools.OutputSink = discardToolOutput{}
	stdoutLimit := uint64(maxToolStderrBytes)
	if fingerprints != nil {
		var err error
		verificationSQL, err = buildFingerprintVerificationSQL(fingerprints)
		if err != nil || uint64(len(verificationSQL)) > maxBytes-prefixLength-schemaLength-dataLength {
			_ = closeRestoreSQLInputs(schema, data)
			return ErrRestore
		}
		gate = newRestoreCommitGate()
		stdout = newFingerprintVerificationSink(fingerprints, gate)
		stdoutLimit = maxBytes
	}
	readers := []io.Reader{
		&exactRestoreReader{source: schema, remaining: schemaLength},
		strings.NewReader(restoreReplicationRole),
		&exactRestoreReader{source: data, remaining: dataLength},
	}
	if fingerprints != nil {
		readers = append(readers, strings.NewReader(verificationSQL), gate)
	}
	input := &restoreSQLInput{Reader: io.MultiReader(readers...), schema: schema, data: data, gate: gate}
	result, err := run(ctx, tools.RunRequest{
		Tool: tools.PSQL, Mode: tools.ModeRestoreSQL, Connection: connection,
		Input: input, InputLimit: schemaLength + prefixLength + dataLength + uint64(len(verificationSQL)),
		Timeout: databaseToolTimeout, CleanupTimeout: 5 * time.Second,
		StdoutLimit: stdoutLimit, StderrLimit: maxToolStderrBytes, Stdout: stdout,
	})
	closeErr := input.Close()
	if err != nil || result.ExitCode != 0 || closeErr != nil {
		return ErrRestore
	}
	return nil
}

type restoreSQLInput struct {
	io.Reader
	schema, data io.ReadCloser
	gate         *restoreCommitGate
	once         sync.Once
	err          error
}

func (r *restoreSQLInput) Close() error {
	r.once.Do(func() {
		if r.gate != nil {
			r.gate.cancel()
		}
		r.err = closeRestoreSQLInputs(r.schema, r.data)
	})
	return r.err
}

func closeRestoreSQLInputs(schema, data io.ReadCloser) error {
	failed := false
	if schema != nil && schema.Close() != nil {
		failed = true
	}
	if data != nil && data.Close() != nil {
		failed = true
	}
	if failed {
		return ErrRestore
	}
	return nil
}

type exactRestoreReader struct {
	source    io.Reader
	remaining uint64
	done      bool
}

func (r *exactRestoreReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if r.done {
		return 0, io.EOF
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.source.Read(probe[:])
		if n != 0 || err != io.EOF {
			r.done = true
			return 0, io.ErrUnexpectedEOF
		}
		r.done = true
		return 0, io.EOF
	}
	if uint64(len(buffer)) > r.remaining {
		buffer = buffer[:int(r.remaining)]
	}
	n, err := r.source.Read(buffer)
	if n < 0 || n > len(buffer) {
		r.done = true
		return 0, io.ErrUnexpectedEOF
	}
	r.remaining -= uint64(n)
	if err != nil {
		if err == io.EOF && r.remaining == 0 {
			r.done = true
			return n, nil
		}
		r.done = true
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

func openDatabaseDump(dir string, component archive.Component, passphrase string) (io.ReadCloser, error) {
	if component.ID != "00000000" {
		return nil, ErrRestore
	}
	return openArchiveComponent(dir, component, passphrase)
}

func openArchiveComponent(dir string, component archive.Component, passphrase string) (io.ReadCloser, error) {
	if !validArchiveComponentID(component.ID) || component.Length <= 0 || component.Length > archive.MaxComponentBytes {
		return nil, ErrRestore
	}
	path := filepath.Join(dir, component.ID+".age")
	ciphertext, err := platform.OpenPrivatePayloadFile(path, false)
	if err != nil {
		return nil, ErrRestore
	}
	info, err := ciphertext.Stat()
	maxCiphertext := component.Length + component.Length/(64<<10)*32 + 1<<20
	if err != nil || info.Size() <= 0 || info.Size() > maxCiphertext {
		_ = ciphertext.Close()
		return nil, ErrRestore
	}
	plain, err := archive.Decrypt(ciphertext, passphrase)
	if err != nil {
		_ = ciphertext.Close()
		return nil, ErrRestore
	}
	return &archiveComponentReader{Reader: plain, file: ciphertext}, nil
}

func validArchiveComponentID(id string) bool {
	if len(id) != 8 {
		return false
	}
	for _, char := range id {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}

type archiveComponentReader struct {
	io.Reader
	file *os.File
	once sync.Once
	err  error
}

func (r *archiveComponentReader) Close() error {
	r.once.Do(func() { r.err = r.file.Close() })
	return r.err
}

type discardToolOutput struct{}

func (discardToolOutput) WriteContext(ctx context.Context, data []byte) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return len(data), nil
}
func (discardToolOutput) CloseContext(context.Context) error { return nil }

var _ tools.OutputSink = discardToolOutput{}
