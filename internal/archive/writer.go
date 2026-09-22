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
)

const manifestName = "manifest.age"

type Input struct {
	Key    string
	Scope  string
	Status string
	Source io.Reader
}

func Write(dir string, inputs []Input, passphrase string) (Manifest, error) {
	if err := validatePassphrase(passphrase); err != nil {
		return Manifest{}, err
	}
	if _, err := os.Lstat(filepath.Join(dir, manifestName)); err == nil || !os.IsNotExist(err) {
		return Manifest{}, errors.New("archive manifest already exists")
	}
	manifest := Manifest{Format: Format, Version: Version, Components: make([]Component, 0, len(inputs))}
	for i, input := range inputs {
		if input.Source == nil || input.Key == "" || input.Scope == "" {
			return Manifest{}, errors.New("invalid archive input")
		}
		id := fmt.Sprintf("%08x", i)
		length, digest, err := writePayload(filepath.Join(dir, id+".age"), input.Source, passphrase)
		if err != nil {
			return Manifest{}, err
		}
		status := input.Status
		if status == "" {
			status = "complete"
		}
		manifest.Components = append(manifest.Components, Component{ID: id, Key: input.Key, Scope: input.Scope, Status: status, Length: length, SHA256: digest})
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if _, _, err := writePayload(filepath.Join(dir, manifestName), bytesReader(encoded), passphrase); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func writePayload(path string, source io.Reader, passphrase string) (int64, string, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
