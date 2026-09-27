//go:build localdemo && integration

package localdemo

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/destination"
	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestRehearseExistingArchiveOnDisposablePostgres17(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Homebrew restore rehearsal targets macOS arm64")
	}
	binDir := os.Getenv("SPARC_TEST_PG_BIN")
	if binDir == "" {
		t.Skip("local PostgreSQL 17 tools not configured")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(parent); err != nil {
		t.Fatal(err)
	}
	const passphrase = "synthetic-only-rehearsal-passphrase"
	demo := filepath.Join(parent, "demo")
	if err := Run(context.Background(), binDir, demo, []byte(passphrase)); err != nil {
		t.Fatalf("create synthetic database archive: %v", err)
	}
	ciphertext, err := platform.OpenPrivatePayloadFile(filepath.Join(demo, "00000000.age"), false)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := archive.Decrypt(ciphertext, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(parent, "production-shape")
	_, err = destination.Create(backup, []archive.Input{{Key: "database/postgresql.dump", Scope: "synthetic database", Status: "incomplete", Source: plaintext}}, passphrase)
	closeErr := ciphertext.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("package synthetic dump: %v / %v", err, closeErr)
	}
	summary, err := InspectArchiveTOC(context.Background(), binDir, backup, []byte(passphrase))
	if err != nil || summary.Total == 0 {
		t.Fatalf("local TOC inspection failed: %#v / %v", summary, err)
	}
	if err := RehearseExistingArchive(context.Background(), binDir, backup, []byte(passphrase)); err != nil {
		t.Fatalf("disposable local restore failed: %v", err)
	}
	if _, err := archive.Verify(backup, passphrase); err != nil {
		t.Fatalf("rehearsal altered the archive: %v", err)
	}
}
