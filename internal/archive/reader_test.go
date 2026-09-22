package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestVerifyChecksManifestPayloadsAndExactInventory(t *testing.T) {
	dir := privateArchive(t, []Input{{Key: "database/schema.sql", Scope: "database", Source: strings.NewReader("schema")}}, "passphrase")
	manifest, err := Verify(dir, "passphrase")
	if err != nil || len(manifest.Components) != 1 {
		t.Fatalf("Verify() = %#v, %v", manifest, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unexpected.age"), []byte("extra"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir, "passphrase"); err == nil {
		t.Fatal("Verify() accepted an extra file")
	}
}

func TestVerifyRejectsTruncatedManifest(t *testing.T) {
	dir := privateArchive(t, []Input{{Key: "database/schema.sql", Scope: "database", Source: strings.NewReader("schema")}}, "passphrase")
	path := filepath.Join(dir, manifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data[:len(data)-1], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir, "passphrase"); err == nil {
		t.Fatal("Verify() accepted a truncated encrypted manifest")
	}
}

func TestVerifyRejectsMissingPayloadAndWrongPassphrase(t *testing.T) {
	dir := privateArchive(t, []Input{{Key: "database/schema.sql", Scope: "database", Source: strings.NewReader("schema")}}, "passphrase")
	if _, err := Verify(dir, "wrong"); err == nil {
		t.Fatal("Verify() accepted a wrong passphrase")
	}
	if err := os.Remove(filepath.Join(dir, "00000000.age")); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir, "passphrase"); err == nil {
		t.Fatal("Verify() accepted a missing payload")
	}
}

func TestVerifyRejectsPlaintextDigestMismatch(t *testing.T) {
	dir := privateDir(t)
	if err := platform.CheckPrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	data := []byte("expected")
	digest := sha256.Sum256([]byte("different"))
	manifest := Manifest{Format: Format, Version: Version, Components: []Component{{ID: "00000000", Key: "database/schema.sql", Scope: "database", Status: "complete", Length: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}}}
	if err := writePayloadForTest(filepath.Join(dir, "00000000.age"), []byte("expected"), "passphrase"); err != nil {
		t.Fatal(err)
	}
	if err := writePayloadForTest(filepath.Join(dir, manifestName), mustJSON(t, manifest), "passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir, "passphrase"); err == nil {
		t.Fatal("Verify() accepted a plaintext digest mismatch")
	}
}

func privateArchive(t *testing.T, inputs []Input, passphrase string) string {
	t.Helper()
	dir := privateDir(t)
	if err := platform.CheckPrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, inputs, passphrase); err != nil {
		t.Fatal(err)
	}
	return dir
}

func privateDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "archive")
	if err := platform.CreatePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writePayloadForTest(path string, data []byte, passphrase string) error {
	file, err := platform.CreatePrivateFile(path)
	if err != nil {
		return err
	}
	if err := Encrypt(file, bytes.NewReader(data), passphrase); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
