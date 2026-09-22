package archive

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePublishesEncryptedManifestLast(t *testing.T) {
	dir := t.TempDir()
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

func TestWriteRefusesExistingManifestBeforeWritingPayloads(t *testing.T) {
	dir := t.TempDir()
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
	dir := t.TempDir()
	_, err := Write(dir, []Input{{Key: "database/schema.sql", Scope: "database", Source: failingReader{}}}, "passphrase")
	if err == nil {
		t.Fatal("Write() succeeded with a failed source")
	}
	if _, err := os.Stat(filepath.Join(dir, manifestName)); !os.IsNotExist(err) {
		t.Fatalf("manifest stat error = %v, want not exist", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
