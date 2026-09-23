//go:build localdemo

package localdemo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestPostgresMajorParsesClientOutput(t *testing.T) {
	for output, want := range map[string]int{
		"pg_dump (PostgreSQL) 17.9 (Homebrew)": 17,
		"PostgreSQL 17.11":                     17,
		"pg_restore (PostgreSQL) 16.4":         16,
		"not a PostgreSQL version":             0,
	} {
		if got := postgresMajor(output); got != want {
			t.Errorf("postgresMajor(%q) = %d, want %d", output, got, want)
		}
	}
}

func TestResolveToolsRequiresExplicitAbsoluteDirectory(t *testing.T) {
	if _, err := resolveTools("postgresql/bin"); err != ErrTools {
		t.Fatalf("resolveTools(relative) error = %v, want ErrTools", err)
	}
}

func TestSanitizedEnvRejectsAmbientDatabaseAndProxySettings(t *testing.T) {
	t.Setenv("PGPASSWORD", "secret-canary")
	t.Setenv("PGHOST", "remote.invalid")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid")
	t.Setenv("PATH", "/untrusted/bin")
	env, err := sanitizedEnv("/approved/postgresql/bin", "/private/root", "/private/root/home", "/private/root/appdata", "/private/root/home/.pgpass")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	for _, forbidden := range []string{"secret-canary", "remote.invalid", "proxy.invalid", "PATH=/untrusted/bin"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("sanitized environment retained %q", forbidden)
		}
	}
	if !strings.Contains(joined, "PATH=/approved/postgresql/bin") || !strings.Contains(joined, "PGPASSFILE=/private/root/home/.pgpass") {
		t.Fatalf("sanitized environment lacks explicit local settings: %q", joined)
	}
}

func TestValidateArchivePathRequiresPrivateNewDestination(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	privateDir := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(privateDir); err != nil {
		t.Fatal("unable to create private test directory")
	}
	archivePath := filepath.Join(privateDir, "archive")
	if err := validateArchivePath(archivePath); err != nil {
		t.Fatalf("validateArchivePath(new private path): %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateArchivePath(archivePath); err != ErrArchivePath {
		t.Fatalf("validateArchivePath(existing path) error = %v, want ErrArchivePath", err)
	}
	if err := validateArchivePath("x"); err != ErrArchivePath {
		t.Fatalf("validateArchivePath(relative path) error = %v, want ErrArchivePath", err)
	}
}
