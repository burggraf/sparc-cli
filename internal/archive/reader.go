package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/burggraf/sparc-cli/internal/platform"
)

const maxManifestCiphertext = maxManifestBytes + 1<<20

var ErrArchive = errors.New("archive verification failed")

// Verify authenticates the manifest and every listed payload, then requires the
// directory to contain exactly those payloads and manifest.age.
func Verify(dir, passphrase string) (Manifest, error) {
	if validatePassphrase(passphrase) != nil || platform.CheckPrivateDir(dir) != nil {
		return Manifest{}, ErrArchive
	}
	directory, err := os.Open(dir)
	if err != nil {
		return Manifest{}, ErrArchive
	}
	entries, readErr := directory.ReadDir(maxComponents + 2)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil || len(entries) == 0 || len(entries) > maxComponents+1 {
		return Manifest{}, ErrArchive
	}
	manifestBytes, err := decryptFile(filepath.Join(dir, manifestName), maxManifestCiphertext, passphrase, maxManifestBytes)
	if err != nil {
		return Manifest{}, ErrArchive
	}
	manifest, err := ParseManifest(bytes.NewReader(manifestBytes))
	if err != nil || len(entries) != len(manifest.Components)+1 {
		return Manifest{}, ErrArchive
	}
	expected := make(map[string]struct{}, len(entries))
	expected[manifestName] = struct{}{}
	for _, component := range manifest.Components {
		name := component.ID + ".age"
		expected[name] = struct{}{}
		if verifyPayload(filepath.Join(dir, name), component, passphrase) != nil {
			return Manifest{}, ErrArchive
		}
	}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok {
			return Manifest{}, ErrArchive
		}
		delete(expected, entry.Name())
	}
	if len(expected) != 0 {
		return Manifest{}, ErrArchive
	}
	return manifest, nil
}

func verifyPayload(path string, component Component, passphrase string) error {
	file, err := platform.OpenPrivatePayloadFile(path, false)
	if err != nil {
		return ErrArchive
	}
	info, statErr := file.Stat()
	if statErr != nil || info.Size() > maxCiphertextSize(component.Length) {
		_ = file.Close()
		return ErrArchive
	}
	plain, decryptErr := Decrypt(file, passphrase)
	if decryptErr != nil {
		_ = file.Close()
		return ErrArchive
	}
	hash := sha256.New()
	counter := &countWriter{Writer: hash}
	_, readErr := io.Copy(counter, plain)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || counter.n != component.Length || hex.EncodeToString(hash.Sum(nil)) != component.SHA256 {
		return ErrArchive
	}
	return nil
}

func decryptFile(path string, maxCiphertext int64, passphrase string, maxPlaintext int64) ([]byte, error) {
	file, err := platform.OpenPrivatePayloadFile(path, false)
	if err != nil {
		return nil, ErrArchive
	}
	info, statErr := file.Stat()
	if statErr != nil || info.Size() > maxCiphertext {
		_ = file.Close()
		return nil, ErrArchive
	}
	plain, decryptErr := Decrypt(file, passphrase)
	if decryptErr != nil {
		_ = file.Close()
		return nil, ErrArchive
	}
	data, readErr := io.ReadAll(io.LimitReader(plain, maxPlaintext+1))
	_, drainErr := io.Copy(io.Discard, plain)
	closeErr := file.Close()
	if readErr != nil || drainErr != nil || closeErr != nil || int64(len(data)) > maxPlaintext {
		return nil, ErrArchive
	}
	return data, nil
}

func maxCiphertextSize(plaintext int64) int64 {
	// Age adds one authentication tag per 64 KiB chunk, plus its header.
	return plaintext + plaintext/(64<<10)*32 + 1<<20
}
