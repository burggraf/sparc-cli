package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/burggraf/sparc-cli/internal/platform"
)

// RunExternal executes a closed PostgreSQL operation from an explicitly
// selected local PostgreSQL 17 client directory. It is an unbundled macOS
// arm64 developer path, not a production payload or PATH fallback.
func RunExternal(ctx context.Context, clientBin string, request RunRequest) (RunResult, error) {
	return runExternal(ctx, clientBin, request, "")
}

func runExternal(ctx context.Context, clientBin string, request RunRequest, testHostAddr string) (RunResult, error) {
	if ctx == nil || !validRunRequest(request) || runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" ||
		(testHostAddr != "" && testHostAddr != "127.0.0.1") {
		return RunResult{}, ErrRun
	}
	manifest, archive, err := buildExternalPayloadArchive(clientBin)
	if err != nil {
		return RunResult{}, ErrRun
	}
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil || !filepath.IsAbs(tempRoot) || filepath.Clean(tempRoot) != tempRoot {
		return RunResult{}, ErrRun
	}
	cacheRoot, err := os.MkdirTemp(tempRoot, "sparc-external-cache-")
	if err != nil {
		return RunResult{}, ErrRun
	}
	defer os.RemoveAll(cacheRoot)
	if platform.CheckPrivateDir(cacheRoot) != nil {
		return RunResult{}, ErrRun
	}
	packagePath, err := preparePayload(ctx, cacheRoot, bytes.NewReader(archive), manifest)
	if err != nil {
		return RunResult{}, ErrRun
	}
	request.testHostAddr = testHostAddr
	return runWith(ctx, request, manifest, packagePath, cacheRoot, defaultRunOps())
}

func buildExternalPayloadArchive(clientBin string) (packageManifest, []byte, error) {
	if !filepath.IsAbs(clientBin) || filepath.Clean(clientBin) != clientBin || filepath.Base(clientBin) != "bin" {
		return packageManifest{}, nil, ErrInvalidPayload
	}
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
	var raw bytes.Buffer
	tarWriter := tar.NewWriter(&raw)
	for _, expected := range files {
		path := filepath.Join(filepath.Dir(clientBin), filepath.FromSlash(expected.path))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || uint64(info.Size()) > maxPayloadFileBytes {
			return packageManifest{}, nil, ErrInvalidPayload
		}
		data, err := os.ReadFile(path)
		if err != nil || int64(len(data)) != info.Size() {
			return packageManifest{}, nil, ErrInvalidPayload
		}
		file := payloadFile{Path: expected.path, Purpose: expected.purpose, Mode: expected.mode, Length: uint64(len(data)), SHA256: sha256.Sum256(data)}
		manifest.Files = append(manifest.Files, file)
		if err := tarWriter.WriteHeader(&tar.Header{Name: file.Path, Mode: int64(file.Mode), Size: int64(file.Length), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			return packageManifest{}, nil, ErrInvalidPayload
		}
		if _, err := tarWriter.Write(data); err != nil {
			return packageManifest{}, nil, ErrInvalidPayload
		}
	}
	if err := tarWriter.Close(); err != nil {
		return packageManifest{}, nil, ErrInvalidPayload
	}
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if _, err := io.Copy(gzipWriter, &raw); err != nil || gzipWriter.Close() != nil || uint64(compressed.Len()) == 0 || uint64(compressed.Len()) > maxCompressedBytes {
		return packageManifest{}, nil, ErrInvalidPayload
	}
	archive := compressed.Bytes()
	manifest.CompressedLength = uint64(len(archive))
	manifest.CompressedSHA256 = sha256.Sum256(archive)
	if validateManifest(manifest) != nil {
		return packageManifest{}, nil, ErrInvalidPayload
	}
	return manifest, archive, nil
}
