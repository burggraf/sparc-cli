//go:build darwin && arm64

package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestBundledPostgreSQL17ClientRunsWithoutExternalInstallation(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Library", "Caches"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	t.Setenv("PGHOST", "ambient-host-must-not-be-used")
	manifest, err := lookupProductionPayload(PGDump, supportedPostgreSQLMajor, payloadTarget{OS: "darwin", Architecture: "arm64"})
	if err != nil {
		t.Fatalf("compiled-in manifest unavailable: %v", err)
	}
	if err := verifyCompressedSource(context.Background(), bytes.NewReader(darwinArm64PayloadArchive), manifest); err != nil {
		t.Fatalf("compiled-in archive validation failed: %v", err)
	}
	locations, err := platform.NativeLocations()
	if err != nil {
		t.Fatalf("native cache paths: %v", err)
	}
	if err := ensurePrivateCacheRoot(locations.CacheDir); err != nil {
		t.Fatalf("private cache root: %v", err)
	}
	packagePath, err := preparePayload(context.Background(), locations.CacheDir, bytes.NewReader(darwinArm64PayloadArchive), manifest)
	if err != nil {
		t.Fatalf("compiled-in package extraction failed: %v", err)
	}
	if err := validatePayloadPackage(context.Background(), packagePath, manifest); err != nil {
		t.Fatalf("compiled-in package validation failed: %v", err)
	}
	request := RunRequest{
		Tool: PGDump, Mode: ModeVersion, Timeout: 10 * time.Second,
		CleanupTimeout: time.Second, StdoutLimit: 1024, StderrLimit: 1024, Stdout: &memorySink{},
	}
	result, err := Run(context.Background(), request)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("bundled pg_dump --version = %#v, %v", result, err)
	}
	if got := request.Stdout.(*memorySink).String(); got != "pg_dump (PostgreSQL) 17.11\n" {
		t.Fatalf("bundled pg_dump version = %q", got)
	}
}
