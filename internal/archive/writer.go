package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/burggraf/sparc-cli/internal/platform"
)

const manifestName = "manifest.age"

var ErrWrite = errors.New("archive write failed")

type Input struct {
	Key    string
	Scope  string
	Status string
	Source io.Reader
}

func Write(dir string, inputs []Input, passphrase string) (Manifest, error) {
	if err := validatePassphrase(passphrase); err != nil || platform.CheckPrivateDir(dir) != nil || validateInputs(inputs) != nil {
		return Manifest{}, ErrWrite
	}
	if _, err := os.Lstat(filepath.Join(dir, manifestName)); err == nil || !os.IsNotExist(err) {
		return Manifest{}, ErrWrite
	}
	manifest := Manifest{Format: Format, Version: Version, Components: make([]Component, 0, len(inputs))}
	for i, input := range inputs {
		id := fmt.Sprintf("%08x", i)
		length, digest, err := writePayload(filepath.Join(dir, id+".age"), input.Source, passphrase)
		if err != nil {
			return Manifest{}, ErrWrite
		}
		status := input.Status
		if status == "" {
			status = "complete"
		}
		manifest.Components = append(manifest.Components, Component{ID: id, Key: input.Key, Scope: input.Scope, Status: status, Length: length, SHA256: digest})
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, ErrWrite
	}
	for _, component := range manifest.Components {
		if err := verifyPayload(filepath.Join(dir, component.ID+".age"), component, passphrase); err != nil {
			return Manifest{}, ErrWrite
		}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, ErrWrite
	}
	if _, _, err := writePayload(filepath.Join(dir, manifestName), bytesReader(encoded), passphrase); err != nil {
		return Manifest{}, ErrWrite
	}
	return manifest, nil
}

func validateInputs(inputs []Input) error {
	if len(inputs) == 0 || len(inputs) > maxComponents {
		return errors.New("invalid archive inputs")
	}
	keys := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		status := input.Status
		if status == "" {
			status = "complete"
		}
		if input.Source == nil || !validKey(input.Key) || input.Scope == "" || len(input.Scope) > maxScopeBytes || !utf8.ValidString(input.Scope) || hasControl(input.Scope) || (status != "complete" && status != "incomplete") {
			return errors.New("invalid archive input")
		}
		key := portableFold(input.Key)
		if _, exists := keys[key]; exists {
			return errors.New("duplicate archive logical key")
		}
		keys[key] = struct{}{}
	}
	return nil
}

func writePayload(path string, source io.Reader, passphrase string) (int64, string, error) {
	file, err := platform.CreatePrivateFile(path)
	if err != nil {
		return 0, "", err
	}
	hash := sha256.New()
	counting := &countWriter{Writer: hash}
	err = Encrypt(file, io.TeeReader(source, counting), passphrase)
	closeErr := file.Close()
	if err != nil {
		return 0, "", err
	}
	if closeErr != nil {
		return 0, "", closeErr
	}
	return counting.n, hex.EncodeToString(hash.Sum(nil)), nil
}

type countWriter struct {
	io.Writer
	n int64
}

func (w *countWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.n += int64(n)
	return n, err
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
