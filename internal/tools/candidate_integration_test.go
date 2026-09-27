//go:build integration

package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func candidatePayloadArchive(t *testing.T, clientBin string) (packageManifest, []byte) {
	t.Helper()
	if filepath.Base(clientBin) != "bin" {
		t.Fatal("SPARC_TEST_PG_CLIENT_BIN must end in bin")
	}
	root := filepath.Dir(clientBin)
	files := []struct {
		path    string
		purpose filePurpose
		mode    uint32
	}{
		{"bin/pg_dump", purposeExecutable, 0o700},
		{"bin/pg_restore", purposeExecutable, 0o700},
		{"bin/psql", purposeExecutable, 0o700},
		{"lib/libpq.5.dylib", purposeRuntime, 0o600},
		{"lib/libssl.3.dylib", purposeRuntime, 0o600},
		{"lib/libcrypto.3.dylib", purposeRuntime, 0o600},
	}
	manifest := packageManifest{
		SchemaVersion:   payloadSchemaVersion,
		PostgreSQLMajor: supportedPostgreSQLMajor,
		Target:          payloadTarget{OS: "darwin", Architecture: "arm64"},
		Files:           make([]payloadFile, 0, len(files)),
		Executables: map[Tool]string{
			PGDump: "bin/pg_dump", PGRestore: "bin/pg_restore", PSQL: "bin/psql",
		},
	}
	entries := make([]testArchiveEntry, 0, len(files))
	for _, expected := range files {
		path := filepath.Join(root, filepath.FromSlash(expected.path))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || uint64(info.Size()) > maxPayloadFileBytes {
			t.Fatalf("candidate file %q must be a bounded regular file", expected.path)
		}
		data, err := os.ReadFile(path)
		if err != nil || int64(len(data)) != info.Size() {
			t.Fatalf("read candidate file %q: %v", expected.path, err)
		}
		file := payloadFile{Path: expected.path, Purpose: expected.purpose, Mode: expected.mode, Length: uint64(len(data)), SHA256: sha256.Sum256(data)}
		manifest.Files = append(manifest.Files, file)
		entries = append(entries, testArchiveEntry{header: regularHeader(file.Path, file.Mode, int64(file.Length)), data: data})
	}
	return withArchive(manifest, buildArchive(t, entries, nil))
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
		request := RunRequest{Tool: tool, Version: true, Timeout: runTestTimeout, CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: sink}
		if _, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, defaultRunOps()); err != nil {
			t.Fatalf("run extracted %v --version: %v", tool, err)
		}
		if got := sink.String(); got != want {
			t.Fatalf("extracted %v --version = %q, want %q", tool, got, want)
		}
	}
}
