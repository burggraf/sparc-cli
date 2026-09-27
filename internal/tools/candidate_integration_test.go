//go:build integration

package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func candidatePayloadArchive(t *testing.T, clientBin string) (packageManifest, []byte) {
	t.Helper()
	manifest, archive, err := buildExternalPayloadArchive(clientBin)
	if err != nil {
		t.Fatalf("build private candidate payload: %v", err)
	}
	return manifest, archive
}

func TestOptInCandidatePayloadExtraction(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skipf("macOS arm64 candidate test, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	clientBin := os.Getenv("SPARC_TEST_PG_CLIENT_BIN")
	if clientBin == "" {
		t.Skip("set SPARC_TEST_PG_CLIENT_BIN to run the local client extraction smoke")
	}
	if !filepath.IsAbs(clientBin) || filepath.Clean(clientBin) != clientBin {
		t.Fatal("SPARC_TEST_PG_CLIENT_BIN must name an explicitly selected absolute clean client bin directory")
	}

	manifest, archive := candidatePayloadArchive(t, clientBin)
	cacheRoot := privateCacheRoot(t)
	packagePath, err := preparePayload(context.Background(), cacheRoot, bytes.NewReader(archive), manifest)
	if err != nil {
		t.Fatalf("extract candidate payload: %v", err)
	}
	if err := validatePayloadPackage(context.Background(), packagePath, manifest); err != nil {
		t.Fatalf("validate extracted candidate payload: %v", err)
	}

	for tool, want := range map[Tool]string{
		PGDump:    "pg_dump (PostgreSQL) 17.11\n",
		PGRestore: "pg_restore (PostgreSQL) 17.11\n",
		PSQL:      "psql (PostgreSQL) 17.11\n",
	} {
		sink := &memorySink{}
		request := RunRequest{Tool: tool, Mode: ModeVersion, Timeout: runTestTimeout, CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: sink}
		if _, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, defaultRunOps()); err != nil {
			t.Fatalf("run extracted %v --version: %v", tool, err)
		}
		if got := sink.String(); got != want {
			t.Fatalf("extracted %v --version = %q, want %q", tool, got, want)
		}
	}

	sink := &memorySink{}
	request := RunRequest{Tool: PGDump, Mode: ModeVersion, Timeout: runTestTimeout, CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: sink}
	if _, err := RunCandidate(context.Background(), clientBin, request); err != nil || sink.String() != "pg_dump (PostgreSQL) 17.11\n" {
		t.Fatalf("integration-only candidate runner: output=%q err=%v", sink.String(), err)
	}

	externalSink := &memorySink{}
	externalRequest := RunRequest{Tool: PGDump, Mode: ModeVersion, Timeout: runTestTimeout, CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: externalSink}
	if _, err := RunExternal(context.Background(), clientBin, externalRequest); err != nil || externalSink.String() != "pg_dump (PostgreSQL) 17.11\n" {
		t.Fatalf("explicit external runner: output=%q err=%v", externalSink.String(), err)
	}
}
