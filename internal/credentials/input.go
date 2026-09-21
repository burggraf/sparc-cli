// Package credentials reads one explicitly selected secret source. It never
// persists secrets or forwards source errors to public diagnostics.
package credentials

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"unicode"
	"unicode/utf8"

	"github.com/burggraf/sparc-cli/internal/platform"
	"golang.org/x/term"
)

const maxSecretBytes = 16 * 1024

var ErrInput = errors.New("credential input unavailable")
var ErrCancelled = errors.New("credential input cancelled")

type Reference struct {
	Env  string `json:"env,omitempty"`
	File string `json:"file,omitempty"`
}

func (r Reference) Validate() error {
	if (r.Env == "") == (r.File == "") {
		return ErrInput
	}
	if r.Env != "" {
		if len(r.Env) > 128 {
			return ErrInput
		}
		for i, c := range []byte(r.Env) {
			if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
				return ErrInput
			}
		}
	} else if len(r.File) > 4096 || !safeText([]byte(r.File)) || !filepath.IsAbs(r.File) || filepath.Clean(r.File) != r.File {
		return ErrInput
	}
	return nil
}

func safeText(value []byte) bool {
	if len(value) == 0 || !utf8.Valid(value) {
		return false
	}
	for _, r := range string(value) {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func secret(value []byte) ([]byte, error) {
	if len(value) > maxSecretBytes || !safeText(value) {
		return nil, ErrInput
	}
	return value, nil
}

// Input never reads stdin when a reference is supplied, and never falls back
// from a missing explicit source to a prompt or an ambient credential.
func Input(ref *Reference, stdin *os.File, stderr io.Writer) ([]byte, error) {
	if ref != nil {
		if ref.Validate() != nil {
			return nil, ErrInput
		}
		if ref.Env != "" {
			value, ok := os.LookupEnv(ref.Env)
			if !ok || len(value) > maxSecretBytes {
				return nil, ErrInput
			}
			return secret([]byte(value))
		}
		value, err := platform.ReadPrivateFile(ref.File, maxSecretBytes+2)
		if err != nil {
			return nil, ErrInput
		}
		if bytes.HasSuffix(value, []byte("\r\n")) {
			value = value[:len(value)-2]
		} else if bytes.HasSuffix(value, []byte("\n")) {
			value = value[:len(value)-1]
		}
		return secret(value)
	}
	if stdin == nil || stderr == nil || !term.IsTerminal(int(stdin.Fd())) {
		return nil, ErrInput
	}
	return readHidden(stdin, stderr, term.MakeRaw, term.Restore)
}

// The injected mode functions test ordinary cleanup paths, not native console
// behavior. Native terminal qualification remains a separate platform gate.
func readHidden(stdin *os.File, stderr io.Writer, makeRaw func(int) (*term.State, error), restore func(int, *term.State) error) (value []byte, err error) {
	fd := int(stdin.Fd())
	state, modeErr := makeRaw(fd)
	if modeErr != nil {
		return nil, ErrInput
	}
	restored := false
	defer func() {
		if !restored && restore(fd, state) != nil {
			value = nil
			err = ErrInput
		}
	}()
	if n, e := io.WriteString(stderr, "Secret: "); e != nil || n != len("Secret: ") {
		return nil, ErrInput
	}
	value, err = readTerminal(stdin)
	if restore(fd, state) != nil {
		return nil, ErrInput
	}
	restored = true
	if n, e := io.WriteString(stderr, "\n"); e != nil || n != 1 {
		return nil, ErrInput
	}
	return value, err
}

func readTerminal(reader io.Reader) ([]byte, error) {
	value := make([]byte, 0, maxSecretBytes)
	invalid := false
	var b [1]byte
	for {
		if _, err := io.ReadFull(reader, b[:]); err != nil {
			return nil, ErrInput
		}
		if invalid && b[0] != '\r' && b[0] != '\n' {
			continue
		}
		switch b[0] {
		case '\r', '\n':
			if invalid {
				return nil, ErrInput
			}
			return secret(value)
		case 3, 4:
			return nil, ErrCancelled
		case '\b', 127:
			if !invalid && len(value) > 0 {
				_, size := utf8.DecodeLastRune(value)
				value = value[:len(value)-size]
			}
		default:
			// Once invalid/overlong, drain through Enter while echo remains disabled.
			// Do not retain further input or allow backspace to hide an overflow.
			if b[0] < 32 || len(value) == maxSecretBytes {
				invalid = true
			}
			if !invalid {
				value = append(value, b[0])
			}
		}
	}
}
