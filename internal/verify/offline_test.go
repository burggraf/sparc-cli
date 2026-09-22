package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestOfflineSeparatesIntegrityFromRecoveryCoverage(t *testing.T) {
	dir := privateDir(t)
	if err := platform.CheckPrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(root, "source")
	if err := os.Mkdir(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "schema.sql")
	if err := os.WriteFile(sourcePath, []byte("captured subset"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(dir, []archive.Input{{Key: "database/schema.sql", Scope: "database", Status: "incomplete", Source: source}}, "passphrase"); err != nil {
		_ = source.Close()
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sourceDir); err != nil {
		t.Fatal(err)
	}
	report, err := Offline(dir, "passphrase")
	if err != nil {
		t.Fatalf("Offline() error = %v", err)
	}
	if !report.IntegrityPassed || report.CaptureStatus != "incomplete" || report.ComponentCount != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestOfflineRejectsCorruptionAndWrongPassphrase(t *testing.T) {
	dir := privateDir(t)
	if err := platform.CheckPrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(dir, []archive.Input{{Key: "database/schema.sql", Scope: "database", Source: strings.NewReader("schema")}}, "passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := Offline(dir, "wrong"); err == nil {
		t.Fatal("Offline() accepted wrong passphrase")
	}
	payload := filepath.Join(dir, "00000000.age")
	data, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(payload, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Offline(dir, "passphrase"); err == nil {
		t.Fatal("Offline() accepted corrupted payload")
	}
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
