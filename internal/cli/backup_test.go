package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/database"
	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestRunBackupBuildsIncompleteLocalDatabaseArchive(t *testing.T) {
	urlFile, passphraseFile := makeBackupInputFiles(t)
	var gotPassword, gotPassphrase string
	var gotRequest database.CaptureRequest
	ops := backupOps{
		input: credentials.Input,
		parse: database.ParseSupavisorSessionURL,
		capture: func(_ context.Context, request database.CaptureRequest) (archive.Manifest, error) {
			gotRequest = request
			gotPassword, gotPassphrase = string(request.SourcePassword), request.ArchivePassphrase
			return archive.Manifest{}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runBackupWith([]string{
		"--project-ref", "abcdefghijklmnopqrst",
		"--database-url-file", urlFile,
		"--archive", "/private/archive-location-canary",
		"--passphrase-file", passphraseFile,
	}, strings.NewReader(""), &stdout, &stderr, ops)
	if code != 3 || stdout.String() != "Database archive created.\nCapture declaration: incomplete.\n" || stderr.Len() != 0 {
		t.Fatalf("backup = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	if gotRequest.Source.ExpectedProjectRef != "abcdefghijklmnopqrst" || gotRequest.Source.Host != "aws-1-us-east-2.pooler.supabase.com" ||
		gotRequest.ArchivePath != "/private/archive-location-canary" || gotPassword != "db-password-canary" || gotPassphrase != "archive-passphrase-canary" {
		t.Fatalf("capture request = %#v", gotRequest)
	}
	if strings.Contains(stdout.String()+stderr.String(), "archive-location-canary") || strings.Contains(stdout.String()+stderr.String(), "db-password-canary") || strings.Contains(stdout.String()+stderr.String(), "archive-passphrase-canary") {
		t.Fatal("backup output leaked sensitive input")
	}
}

func TestRunBackupResolvesRelativePaths(t *testing.T) {
	root := t.TempDir()
	urlPath := filepath.Join(root, ".env.db")
	if err := os.WriteFile(urlPath, []byte("postgresql://postgres.abcdefghijklmnopqrst:db-password-canary@aws-1-us-east-2.pooler.supabase.com:5432/postgres"), 0o644); err != nil {
		t.Fatal(err)
	}
	passphrasePath := filepath.Join(root, ".passphrase")
	if err := os.WriteFile(passphrasePath, []byte("archive-passphrase-canary"), 0o644); err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	var got database.CaptureRequest
	ops := backupOps{
		input: credentials.Input, parse: database.ParseSupavisorSessionURL,
		capture: func(_ context.Context, request database.CaptureRequest) (archive.Manifest, error) {
			got = request
			return archive.Manifest{}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	if code := runBackupWith([]string{"--project-ref", "abcdefghijklmnopqrst", "--database-url-file", ".env.db", "--archive", "tmp/test", "--passphrase-file", ".passphrase"}, strings.NewReader(""), &stdout, &stderr, ops); code != 3 || stderr.Len() != 0 {
		t.Fatalf("backup = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivePath != filepath.Join(workingDir, "tmp", "test") {
		t.Fatalf("archive path = %q", got.ArchivePath)
	}
}

func TestRunBackupRejectsMismatchedPromptedPassphrasesBeforeCapture(t *testing.T) {
	calls, captured := 0, false
	ops := backupOps{
		input: func(_ *credentials.Reference, _ *os.File, _ io.Writer) ([]byte, error) {
			calls++
			switch calls {
			case 1:
				return []byte("postgresql://postgres.abcdefghijklmnopqrst:db-password-canary@aws-1-us-east-2.pooler.supabase.com:5432/postgres"), nil
			case 2:
				return []byte("first archive passphrase"), nil
			case 3:
				return []byte("second archive passphrase"), nil
			default:
				return nil, credentials.ErrInput
			}
		},
		parse: database.ParseSupavisorSessionURL,
		capture: func(context.Context, database.CaptureRequest) (archive.Manifest, error) {
			captured = true
			return archive.Manifest{}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runBackupWith([]string{"--project-ref", "abcdefghijklmnopqrst", "--database-url-file", "/private/database-url", "--archive", "/private/archive"}, strings.NewReader(""), &stdout, &stderr, ops)
	if code != 2 || stdout.Len() != 0 || stderr.String() != "sparc: archive passphrases do not match\n" || captured || calls != 3 {
		t.Fatalf("backup = code %d, stdout %q, stderr %q, captured=%t calls=%d", code, stdout.String(), stderr.String(), captured, calls)
	}
}

func TestRunBackupRejectsInvalidInputWithoutCapture(t *testing.T) {
	urlFile, passphraseFile := makeBackupInputFiles(t)
	called := false
	ops := backupOps{
		input: credentials.Input,
		parse: func([]byte, string) (database.ConnectionParams, []byte, error) {
			return database.ConnectionParams{}, nil, database.ErrConnectionParameters
		},
		capture: func(context.Context, database.CaptureRequest) (archive.Manifest, error) {
			called = true
			return archive.Manifest{}, nil
		},
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"missing flags", nil, "sparc: invalid backup arguments\n"},
		{"extra argument", []string{"--project-ref", "abcdefghijklmnopqrst", "--database-url-file", urlFile, "--archive", "/private/archive", "--passphrase-file", passphraseFile, "extra-canary"}, "sparc: invalid backup arguments\n"},
		{"invalid database URL", []string{"--project-ref", "abcdefghijklmnopqrst", "--database-url-file", urlFile, "--archive", "/private/archive", "--passphrase-file", passphraseFile}, "sparc: unable to read database connection\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			called = false
			var stdout, stderr bytes.Buffer
			if code := runBackupWith(test.args, strings.NewReader("secret-canary"), &stdout, &stderr, ops); code != 2 || stdout.Len() != 0 || stderr.String() != test.want || called {
				t.Fatalf("backup = code %d, stdout %q, stderr %q, capture=%t", code, stdout.String(), stderr.String(), called)
			}
			if strings.Contains(stderr.String(), "secret-canary") {
				t.Fatal("backup disclosed supplied input")
			}
		})
	}
}

func TestRunBackupReportsCaptureFailureWithoutLeakingInput(t *testing.T) {
	urlFile, passphraseFile := makeBackupInputFiles(t)
	ops := backupOps{
		input: credentials.Input,
		parse: database.ParseSupavisorSessionURL,
		capture: func(context.Context, database.CaptureRequest) (archive.Manifest, error) {
			return archive.Manifest{}, errors.New("capture secret-canary")
		},
	}
	var stdout, stderr bytes.Buffer
	code := runBackupWith([]string{"--project-ref", "abcdefghijklmnopqrst", "--database-url-file", urlFile, "--archive", "/private/archive", "--passphrase-file", passphraseFile}, strings.NewReader(""), &stdout, &stderr, ops)
	if code != 1 || stdout.Len() != 0 || stderr.String() != "sparc: database backup failed\n" || strings.Contains(stderr.String(), "secret-canary") {
		t.Fatalf("backup failure = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunBackupHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"backup", "--help"}, strings.NewReader(""), &stdout, &stderr); code != 0 || stdout.String() != backupHelpText || stderr.Len() != 0 {
		t.Fatalf("backup help = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "--pg-client-bin") {
		t.Fatal("backup help requires an external PostgreSQL installation")
	}
}

func TestRunThirdPartyNotices(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"licenses"}, strings.NewReader(""), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("licenses = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	for _, notice := range []string{"PostgreSQL", "OpenSSL", "Apache License", "Henry Spencer"} {
		if !strings.Contains(stdout.String(), notice) {
			t.Fatalf("third-party notices omit %q", notice)
		}
	}
}

func makeBackupInputFiles(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	privateDir := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(privateDir); err != nil {
		t.Fatal("unable to create private test directory")
	}
	urlFile := filepath.Join(privateDir, "database-url")
	url := "postgresql://postgres.abcdefghijklmnopqrst:db-password-canary@aws-1-us-east-2.pooler.supabase.com:5432/postgres"
	if err := platform.WritePrivateFile(urlFile, []byte(url)); err != nil {
		t.Fatal("unable to create private database URL file")
	}
	passphraseFile := filepath.Join(privateDir, "passphrase")
	if err := platform.WritePrivateFile(passphraseFile, []byte("archive-passphrase-canary")); err != nil {
		t.Fatal("unable to create private passphrase file")
	}
	return urlFile, passphraseFile
}
