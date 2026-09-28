//go:build localdemo

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHostedTryRawDiagnosticsRequireTerminal(t *testing.T) {
	var out, err bytes.Buffer
	code := run([]string{"--archive", "tmp/test", "--target-file", ".env.target", "--project-ref", "wxqganvfxvpzqdyzmpmv", "--confirm-disposable-target", "--show-postgres-error"}, nil, &out, &err)
	if code != 2 || out.Len() != 0 || !strings.Contains(err.String(), "require terminal stderr") {
		t.Fatalf("status=%d stderr=%q", code, err.String())
	}
}

func TestHostedTryRequiresExplicitTargetConfirmation(t *testing.T) {
	var out, err bytes.Buffer
	code := run([]string{"--archive", "tmp/test", "--target-file", ".env.target", "--project-ref", "wxqganvfxvpzqdyzmpmv"}, nil, &out, &err)
	if code != 2 || out.Len() != 0 || !strings.Contains(err.String(), "--confirm-disposable-target") {
		t.Fatalf("status=%d stderr=%q", code, err.String())
	}
}

func TestHostedTryRejectsUnreadableTargetBeforePassphrase(t *testing.T) {
	var out, err bytes.Buffer
	code := run([]string{"--archive", "tmp/test", "--target-file", t.TempDir() + "/missing", "--project-ref", "wxqganvfxvpzqdyzmpmv", "--confirm-disposable-target"}, nil, &out, &err)
	if code != 2 || out.Len() != 0 || !strings.Contains(err.String(), "target credential") {
		t.Fatalf("status=%d stderr=%q", code, err.String())
	}
}
