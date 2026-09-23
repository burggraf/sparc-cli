package cli

import (
	"bytes"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/destination"
	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestRunVerifiesIncompleteArchiveOffline(t *testing.T) {
	archiveDir, passphraseFile := makeVerifyFixture(t)
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		panic("offline verify attempted an HTTP request")
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	var stdout, stderr bytes.Buffer
	code := Run([]string{"verify", "--archive", archiveDir, "--passphrase-file", passphraseFile}, strings.NewReader(""), &stdout, &stderr)
	if code != 3 || stdout.String() != "Integrity: passed\nCapture declaration: incomplete\nComponents: 1\n" || stderr.Len() != 0 {
		t.Fatalf("verify = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunVerifiesCompleteArchive(t *testing.T) {
	archiveDir, passphraseFile := makeVerifyFixtureWithStatus(t, "complete")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"verify", "--archive", archiveDir, "--passphrase-file", passphraseFile}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || stdout.String() != "Integrity: passed\nCapture declaration: complete\nComponents: 1\n" || stderr.Len() != 0 {
		t.Fatalf("verify = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunVerifyRequiresArchiveArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"verify"}, strings.NewReader("secret-canary"), &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || stderr.String() != "sparc: invalid verify arguments\n" {
		t.Fatalf("verify accepted missing archive: code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunVerifyDoesNotEchoWrongPassphrase(t *testing.T) {
	archiveDir, _ := makeVerifyFixture(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	privateDir := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(privateDir); err != nil {
		t.Fatal("unable to create private test directory")
	}
	passphraseFile := filepath.Join(privateDir, "wrong-passphrase")
	if err := platform.WritePrivateFile(passphraseFile, []byte("secret-canary")); err != nil {
		t.Fatal("unable to create private test passphrase file")
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"verify", "--archive", archiveDir, "--passphrase-file", passphraseFile}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 || stderr.String() != "sparc: archive verification failed\n" || strings.Contains(stderr.String(), "secret-canary") {
		t.Fatalf("verify leaked details or returned wrong status: code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunVerifyDoesNotReadPipedPassphrase(t *testing.T) {
	archiveDir, _ := makeVerifyFixture(t)
	var stdout, stderr bytes.Buffer
	pipe := strings.NewReader("secret-canary")
	code := Run([]string{"verify", "--archive", archiveDir}, pipe, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || pipe.Len() != len("secret-canary") || stderr.String() != "sparc: unable to read passphrase\n" {
		t.Fatalf("verify accepted non-terminal secret input: code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunVerifyHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"verify", "--help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("verify help exit = %d, stderr %q", code, stderr.String())
	}
	if stdout.String() != verifyHelpText || stderr.Len() != 0 {
		t.Fatalf("verify help output = %q, stderr %q", stdout.String(), stderr.String())
	}
}

func makeVerifyFixture(t *testing.T) (archiveDir, passphraseFile string) {
	return makeVerifyFixtureWithStatus(t, "incomplete")
}

func makeVerifyFixtureWithStatus(t *testing.T, status string) (archiveDir, passphraseFile string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	privateDir := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(privateDir); err != nil {
		t.Fatal("unable to create private test directory")
	}
	passphrase := "synthetic-cli-test-passphrase"
	archiveDir = filepath.Join(privateDir, "archive")
	inputs := []archive.Input{{Key: "database/demo.dump", Scope: "synthetic test", Status: status, Source: strings.NewReader("demo")}}
	if _, err := destination.Create(archiveDir, inputs, passphrase); err != nil {
		t.Fatalf("unable to create test archive: %v", err)
	}
	passphraseFile = filepath.Join(privateDir, "passphrase")
	if err := platform.WritePrivateFile(passphraseFile, []byte(passphrase)); err != nil {
		t.Fatal("unable to create private test passphrase file")
	}
	return archiveDir, passphraseFile
}
