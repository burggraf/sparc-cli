//go:build localdemo

package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/localdemo"
)

func TestRunHelpDoesNotNeedPostgresTools(t *testing.T) {
	t.Setenv("SPARC_TEST_PG_BIN", "")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit = %d, stderr %q", code, stderr.String())
	}
	if stdout.String() != helpText || stderr.Len() != 0 {
		t.Fatalf("help output = %q, stderr %q", stdout.String(), stderr.String())
	}
}

func TestWriteFailureDoesNotEchoUnderlyingError(t *testing.T) {
	var stderr bytes.Buffer
	writeFailure(&stderr, errors.Join(localdemo.ErrRestore, errors.New("secret-canary")))
	if strings.Contains(stderr.String(), "secret-canary") || stderr.String() != "sparc-localdemo: source deletion or fresh-target restore check failed\n" {
		t.Fatalf("failure diagnostic leaked detail: %q", stderr.String())
	}
}

func TestRunRequiresExplicitPostgresTools(t *testing.T) {
	t.Setenv("SPARC_TEST_PG_BIN", "")
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	var stdout, stderr bytes.Buffer
	code := run([]string{"--archive", "/tmp/sparc-localdemo-test-output"}, stdin, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || stderr.String() != "sparc-localdemo: set SPARC_TEST_PG_BIN to an absolute PostgreSQL 17 bin directory\n" {
		t.Fatalf("run without tool path = %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}
