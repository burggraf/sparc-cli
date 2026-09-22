package archive

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePublishesEncryptedManifestLast(t *testing.T) {
	dir := privateDir(t)
	manifest, err := Write(dir, []Input{
		{Key: "database/schema.sql", Scope: "database", Source: strings.NewReader("schema")},
		{Key: "storage/object", Scope: "storage", Source: strings.NewReader("object")},
	}, "passphrase")
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(manifest.Components) != 2 || manifest.Components[0].ID != "00000000" || manifest.Components[1].ID != "00000001" {
		t.Fatalf("components = %#v", manifest.Components)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{entries[0].Name(), entries[1].Name(), entries[2].Name()}; strings.Join(got, ",") != "00000000.age,00000001.age,manifest.age" {
		t.Fatalf("archive files = %v", got)
	}
	ciphertext, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Decrypt(bytes.NewReader(ciphertext), "passphrase")
	if err != nil {
		t.Fatalf("Decrypt(manifest) error = %v", err)
	}
	encoded, err := io.ReadAll(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("passphrase")) || !bytes.Contains(encoded, []byte("database")) {
		t.Fatalf("manifest plaintext = %q", encoded)
	}
	payload, err := os.ReadFile(filepath.Join(dir, "00000000.age"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err = Decrypt(bytes.NewReader(payload), "passphrase")
	if err != nil {
		t.Fatalf("Decrypt(payload) error = %v", err)
	}
	got, err := io.ReadAll(plain)
	if err != nil || string(got) != "schema" {
		t.Fatalf("payload = %q, %v", got, err)
	}
}

func TestWriteRejectsDuplicateKeysBeforeReadingSources(t *testing.T) {
	dir := privateDir(t)
	first, second := &observedReader{Reader: strings.NewReader("first")}, &observedReader{Reader: strings.NewReader("second")}
	_, err := Write(dir, []Input{
		{Key: "database/schema.sql", Scope: "database", Source: first},
		{Key: "DATABASE/SCHEMA.SQL", Scope: "database", Source: second},
	}, "passphrase")
	if err == nil {
		t.Fatal("Write() accepted duplicate logical keys")
	}
	if first.read || second.read {
		t.Fatal("Write() consumed source data before rejecting duplicate keys")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("preflight failure wrote files: %v, %v", entries, readErr)
	}
}

type observedReader struct {
	io.Reader
	read bool
}

func (r *observedReader) Read(p []byte) (int, error) {
	r.read = true
	return r.Reader.Read(p)
}

func TestWriteRefusesExistingManifestBeforeWritingPayloads(t *testing.T) {
	dir := privateDir(t)
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Write(dir, []Input{{Key: "database/schema.sql", Scope: "database", Source: strings.NewReader("schema")}}, "passphrase")
	if err == nil {
		t.Fatal("Write() accepted an existing manifest")
	}
	if _, err := os.Stat(filepath.Join(dir, "00000000.age")); !os.IsNotExist(err) {
		t.Fatalf("payload stat error = %v, want not exist", err)
	}
}

func TestWriteDoesNotPublishManifestWhenSourceFails(t *testing.T) {
	dir := privateDir(t)
	_, err := Write(dir, []Input{{Key: "database/schema.sql", Scope: "database", Source: failingReader{}}}, "passphrase")
	if err != ErrWrite || strings.Contains(err.Error(), "source-canary") {
		t.Fatalf("Write() error = %v, want fixed ErrWrite", err)
	}
	if _, err := os.Stat(filepath.Join(dir, manifestName)); !os.IsNotExist(err) {
		t.Fatalf("manifest stat error = %v, want not exist", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("source-canary") }
