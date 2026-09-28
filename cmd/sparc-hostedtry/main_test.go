//go:build localdemo

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSelectedAuthTrialRejectsNonAtomicFlag(t *testing.T) {
	var out, err bytes.Buffer
	code := run([]string{"--archive", "tmp/test", "--target-file", ".env.target", "--project-ref", "wxqganvfxvpzqdyzmpmv", "--confirm-disposable-target", "--continue-on-error", "--auth-table", "sessions"}, nil, &out, &err)
	if code != 2 || out.Len() != 0 || !strings.Contains(err.String(), "cannot combine") {
		t.Fatalf("status=%d stderr=%q", code, err.String())
	}
}

func TestContinueTrialStillRequiresExplicitTargetConfirmation(t *testing.T) {
	var out, err bytes.Buffer
	code := run([]string{"--archive", "tmp/test", "--target-file", ".env.target", "--project-ref", "wxqganvfxvpzqdyzmpmv", "--continue-on-error"}, nil, &out, &err)
	if code != 2 || out.Len() != 0 || !strings.Contains(err.String(), "--confirm-disposable-target") {
		t.Fatalf("status=%d stderr=%q", code, err.String())
	}
}

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
