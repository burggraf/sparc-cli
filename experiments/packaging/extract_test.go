// Package packaging is an isolated, non-production packaging experiment.
package packaging

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func TestExtractRejectsHashMismatch(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "payload")
	if _, err := Extract([]byte("synthetic helper"), testSHA256([]byte("other bytes")), destination); err == nil {
		t.Fatal("Extract accepted a payload whose hash did not match")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination exists after rejected extraction: %v", err)
	}
}

func TestExtractDoesNotClobberExistingDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "payload")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(destination, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Extract([]byte("synthetic helper"), testSHA256([]byte("synthetic helper")), destination); err == nil {
		t.Fatal("Extract accepted an existing destination")
	}
	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("existing destination was modified: %q", got)
	}
}

func TestRunVerifiedRejectsNilEnvironment(t *testing.T) {
	_, err := RunVerified(filepath.Join(t.TempDir(), "missing-helper"), testSHA256([]byte("expected")), nil, nil)
	const want = "sanitized helper environment required"
	if err == nil {
		t.Fatalf("RunVerified accepted a nil environment instead of returning %q", want)
	}
	if err.Error() != want {
		t.Fatal("RunVerified returned the wrong nil-environment category")
	}
}

func TestRunVerifiedRefusesTamperedHelper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	original := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(path, original, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n# tampered\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := RunVerified(path, testSHA256(original), nil, []string{"PATH=/usr/bin:/bin"})
	const want = "synthetic helper hash mismatch"
	if err == nil {
		t.Fatalf("RunVerified executed a tampered helper instead of returning %q", want)
	}
	if err.Error() != want {
		t.Fatalf("RunVerified tamper error = %q, want %q", err.Error(), want)
	}
}
