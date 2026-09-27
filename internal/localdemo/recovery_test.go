//go:build localdemo

package localdemo

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestRehearseExistingArchiveRejectsInvalidInputBeforeStartingCluster(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Homebrew restore rehearsal targets macOS arm64")
	}
	if err := RehearseExistingArchive(context.Background(), "/opt/homebrew/opt/postgresql@17/bin", filepath.Join(t.TempDir(), "missing"), []byte("wrong")); err != ErrArchive {
		t.Fatalf("missing archive = %v, want ErrArchive", err)
	}
}

func TestRestoreDiagnosticsRequiresOptInAndBoundsOutput(t *testing.T) {
	const secret = "sensitive-sql-canary"
	var defaultDiagnostics restoreDiagnostics
	if n, err := defaultDiagnostics.Write([]byte(secret)); n != len(secret) || err != nil || defaultDiagnostics.output.Len() != len(secret) || defaultDiagnostics.localOutput != nil {
		t.Fatalf("default diagnostics failed to keep output private: n=%d err=%v", n, err)
	}
	var terminal bytes.Buffer
	diagnostics := restoreDiagnostics{localOutput: &terminal}
	payload := bytes.Repeat([]byte("x"), maxRestoreDiagnosticBytes+10)
	if n, err := diagnostics.Write(payload); n != len(payload) || err != nil || diagnostics.output.Len() != maxRestoreDiagnosticBytes || terminal.Len() != maxRestoreDiagnosticBytes {
		t.Fatalf("diagnostics not bounded: n=%d err=%v captured=%d terminal=%d", n, err, diagnostics.output.Len(), terminal.Len())
	}
}

func TestClassifyRestoreFailureWithoutExposingPostgresOutput(t *testing.T) {
	for _, test := range []struct {
		stderr string
		want   error
	}{
		{"pg_restore: error: could not execute query: ERROR:  role \"private-role-canary\" does not exist\n", ErrRestoreMissingRole},
		{"pg_restore: error: could not execute query: ERROR:  extension \"private-extension-canary\" is not available\n", ErrRestoreMissingExtension},
		{"pg_restore: error: could not execute query: ERROR:  syntax error near 'private-canary'\n", ErrRestore},
	} {
		if got := classifyRestoreFailure([]byte(test.stderr)); got != test.want || strings.Contains(got.Error(), "private-") {
			t.Fatalf("classified failure = %v, want %v", got, test.want)
		}
	}
}

func TestSummarizeArchiveTOCWithoutIdentifiers(t *testing.T) {
	input := "; Archive header\n1; 2615 100 SCHEMA - auth supabase_admin\n2; 1259 101 TABLE auth users supabase_admin\n3; 0 101 TABLE DATA auth users supabase_admin\n4; 1259 102 TABLE public items postgres\n5; 0 102 TABLE DATA public items postgres\n6; 3079 103 EXTENSION - pgcrypto postgres\n"
	summary, err := summarizeArchiveTOC([]byte(input))
	if err != nil || summary.Total != 6 || summary.Auth != 3 || summary.Public != 2 || summary.Storage != 0 || summary.Extension != 1 {
		t.Fatalf("TOC summary = %#v, %v", summary, err)
	}
}

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
