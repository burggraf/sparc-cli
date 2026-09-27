//go:build localdemo

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLocalRestoreRawDiagnosticsRequireTerminalBeforeReadingArchive(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--archive", "missing", "--show-postgres-error"}, nil, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || stderr.String() != "sparc-localrestore: raw PostgreSQL diagnostics require terminal stderr\n" {
		t.Fatalf("raw diagnostics = %d, %q", code, stderr.String())
	}
}

func TestLocalRestoreHelpAndMissingArchiveDoNotStartPostgres(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, nil, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "NEW temporary PostgreSQL 17") || stderr.Len() != 0 {
		t.Fatalf("help = %d, %q, %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run(nil, nil, &stdout, &stderr); code != 2 || stderr.String() != "sparc-localrestore: --archive is required\n" {
		t.Fatalf("missing archive = %d, %q", code, stderr.String())
	}
}
