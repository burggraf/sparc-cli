//go:build integration

package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBundledPayloadExtractionAndClientVersions(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("embedded PostgreSQL client payload targets macOS arm64")
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("unable to resolve private integration home")
	}
	if err := os.MkdirAll(filepath.Join(home, "Library", "Caches"), 0o700); err != nil {
		t.Fatal("unable to create private integration cache parent")
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	for _, test := range []struct {
		tool Tool
		name string
	}{
		{PGDump, "pg_dump"}, {PGRestore, "pg_restore"}, {PSQL, "psql"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sink := &memorySink{}
			result, err := RunCandidate(context.Background(), RunRequest{
				Tool: test.tool, Mode: ModeVersion, Timeout: runTestTimeout,
				CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: sink,
			})
			if err != nil || result.ExitCode != 0 || sink.String() != test.name+" (PostgreSQL) 17.11\n" {
				t.Fatalf("bundled %s output=%q result=%#v err=%v", test.name, sink.String(), result, err)
			}
		})
	}
}
