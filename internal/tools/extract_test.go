package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/burggraf/sparc-cli/internal/platform"
)

type testArchiveEntry struct {
	header tar.Header
	data   []byte
}

var syntheticFixture struct {
	once     sync.Once
	manifest packageManifest
	archive  []byte
	entries  []testArchiveEntry
}

func TestExtractSyntheticPayload(t *testing.T) {
	manifest, archive, _ := syntheticExecutableArchive(t)
	root := privateCacheRoot(t)
	if err := verifyCompressedSource(context.Background(), bytes.NewReader(archive), manifest); err != nil {
		t.Fatalf("compressed verification: %v", err)
	}
	got, err := preparePayload(context.Background(), root, bytes.NewReader(archive), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePayloadPackage(context.Background(), got, manifest); err != nil {
		t.Fatal(err)
	}
	// A valid cache hit performs complete validation without consulting source bytes.
	reused, err := preparePayload(context.Background(), root, failingReadSeeker{}, manifest)
	if err != nil || reused != got {
		t.Fatalf("reuse = %q, %v", reused, err)
	}
}

func TestExtractRejectsArchiveAndExecutableTamper(t *testing.T) {
	base, archive, entries := syntheticExecutableArchive(t)
	tests := []struct {
		name  string
		build func() (packageManifest, []byte)
	}{
		{"missing entry", func() (packageManifest, []byte) { return withArchive(base, buildArchive(t, entries[:2], nil)) }},
		{"extra entry", func() (packageManifest, []byte) {
			changed := append(cloneEntries(entries), testArchiveEntry{header: regularHeader("extra", 0o600, 1), data: []byte("x")})
			return withArchive(base, buildArchive(t, changed, nil))
		}},
		{"duplicate entry", func() (packageManifest, []byte) {
			changed := append(cloneEntries(entries), entries[0])
			return withArchive(base, buildArchive(t, changed, nil))
		}},
		{"altered bytes", func() (packageManifest, []byte) {
			changed := cloneEntries(entries)
			changed[0].data = append([]byte(nil), changed[0].data...)
			changed[0].data[0] ^= 0xff
			return withArchive(base, buildArchive(t, changed, nil))
		}},
		{"truncated gzip", func() (packageManifest, []byte) {
			changed := append([]byte(nil), archive[:len(archive)-4]...)
			return withArchive(base, changed)
		}},
		{"bad gzip checksum", func() (packageManifest, []byte) {
			changed := append([]byte(nil), archive...)
			changed[len(changed)-8] ^= 1
			return withArchive(base, changed)
		}},
		{"second gzip member", func() (packageManifest, []byte) {
			changed := append(append([]byte(nil), archive...), archive...)
			return withArchive(base, changed)
		}},
		{"trailing compressed bytes", func() (packageManifest, []byte) {
			changed := append(append([]byte(nil), archive...), 1, 2, 3)
			return withArchive(base, changed)
		}},
		{"malformed gzip", func() (packageManifest, []byte) {
			return withArchive(base, []byte("not-a-gzip-stream"))
		}},
		{"malformed tar", func() (packageManifest, []byte) {
			return withArchive(base, gzipBytes(t, []byte("not-a-tar-stream")))
		}},
		{"trailing decompressed bytes", func() (packageManifest, []byte) {
			return withArchive(base, buildArchive(t, entries, []byte("trailing")))
		}},
		{"symlink", func() (packageManifest, []byte) {
			changed := cloneEntries(entries)
			changed[0].header.Typeflag = tar.TypeSymlink
			changed[0].header.Linkname = "bin/psql"
			changed[0].header.Size = 0
			changed[0].data = nil
			return withArchive(base, buildArchive(t, changed, nil))
		}},
		{"pax metadata", func() (packageManifest, []byte) {
			changed := cloneEntries(entries)
			changed[0].header.Format = tar.FormatPAX
			changed[0].header.PAXRecords = map[string]string{"comment": "unsupported"}
			return withArchive(base, buildArchive(t, changed, nil))
		}},
		{"gnu metadata", func() (packageManifest, []byte) {
			changed := cloneEntries(entries)
			changed[0].header.Format = tar.FormatGNU
			return withArchive(base, buildArchive(t, changed, nil))
		}},
		{"device entry", func() (packageManifest, []byte) {
			changed := cloneEntries(entries)
			changed[0].header.Typeflag = tar.TypeBlock
			changed[0].header.Size = 0
			changed[0].data = nil
			return withArchive(base, buildArchive(t, changed, nil))
		}},
		{"regular entry device metadata", func() (packageManifest, []byte) {
			changed := cloneEntries(entries)
			changed[0].header.Devmajor = 1
			changed[0].header.Devminor = 2
			return withArchive(base, buildArchive(t, changed, nil))
		}},
		{"wrong executable header", func() (packageManifest, []byte) {
			changedManifest := cloneManifest(base)
			changed := cloneEntries(entries)
			for i := range changed {
				changed[i].data = bytes.Repeat([]byte{'x'}, len(changed[i].data))
				changed[i].header.Size = int64(len(changed[i].data))
				changedManifest.Files[i].Length = uint64(len(changed[i].data))
				changedManifest.Files[i].SHA256 = sha256.Sum256(changed[i].data)
			}
			return withArchive(changedManifest, buildArchive(t, changed, nil))
		}},
		{"wrong executable cpu", func() (packageManifest, []byte) {
			changed := cloneManifest(base)
			if changed.Target.Architecture == "arm64" {
				changed.Target.Architecture = "amd64"
			} else {
				changed.Target.Architecture = "arm64"
			}
			return withArchive(changed, archive)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest, compressed := test.build()
			got, err := preparePayload(context.Background(), privateCacheRoot(t), bytes.NewReader(compressed), manifest)
			if got != "" || err != ErrExtraction || errors.Unwrap(err) != nil {
				t.Fatalf("prepare = %q, %v", got, err)
			}
		})
	}
}

func TestExtractRequiresExactlyTwoTarTerminatorBlocks(t *testing.T) {
	base, _, entries := syntheticExecutableArchive(t)
	for _, blocks := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("%d blocks", blocks), func(t *testing.T) {
			archive := buildArchiveWithTerminatorBlocks(t, entries, blocks)
			manifest, archive := withArchive(base, archive)
			path, err := preparePayload(context.Background(), privateCacheRoot(t), bytes.NewReader(archive), manifest)
			if blocks == 2 {
				if err != nil || path == "" {
					t.Fatalf("exact terminator rejected: %q, %v", path, err)
				}
				return
			}
			if path != "" || err != ErrExtraction {
				t.Fatalf("terminator blocks %d accepted: %q, %v", blocks, path, err)
			}
		})
	}
}

func TestExtractAcceptsManifestInventoryReordering(t *testing.T) {
	manifest, archive, _ := syntheticExecutableArchive(t)
	baseID, err := packageID(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for left, right := 0, len(manifest.Files)-1; left < right; left, right = left+1, right-1 {
		manifest.Files[left], manifest.Files[right] = manifest.Files[right], manifest.Files[left]
	}
	reorderedID, err := packageID(manifest)
	if err != nil || reorderedID != baseID {
		t.Fatalf("reordered identity = %q, %v; want %q", reorderedID, err, baseID)
	}
	path, err := preparePayload(context.Background(), privateCacheRoot(t), bytes.NewReader(archive), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePayloadPackage(context.Background(), path, manifest); err != nil {
		t.Fatal(err)
	}
}

func TestExtractRejectsSourceContextAndInjectedFailures(t *testing.T) {
	manifest, archive, _ := syntheticExecutableArchive(t)
	if got, err := preparePayload(context.Background(), privateCacheRoot(t), failingReadSeeker{read: true}, manifest); got != "" || err != ErrExtraction {
		t.Fatal("source read failure accepted")
	}
	if got, err := preparePayload(context.Background(), privateCacheRoot(t), failingReadSeeker{seek: true}, manifest); got != "" || err != ErrExtraction {
		t.Fatal("source seek failure accepted")
	}
	if got, err := preparePayload(context.Background(), privateCacheRoot(t), failingReadSeeker{offset: true}, manifest); got != "" || err != ErrExtraction {
		t.Fatal("nonzero source reset accepted")
	}
	mutated := append([]byte(nil), archive...)
	mutated[len(mutated)/2] ^= 1
	if got, err := preparePayload(context.Background(), privateCacheRoot(t), newMutatingReadSeeker(archive, mutated), manifest); got != "" || err != ErrExtraction {
		t.Fatal("source mutation after verification accepted")
	}
	appended := append(append([]byte(nil), archive...), []byte("appended-after-reset")...)
	if got, err := preparePayload(context.Background(), privateCacheRoot(t), newMutatingReadSeeker(archive, appended), manifest); got != "" || err != ErrExtraction {
		t.Fatal("source append after verification accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := preparePayload(cancelled, privateCacheRoot(t), bytes.NewReader(archive), manifest); got != "" || err != ErrExtraction {
		t.Fatal("cancelled extraction accepted")
	}

	tests := []struct {
		name string
		edit func(*extractionOps)
	}{
		{"write error", func(o *extractionOps) {
			o.write = func(*os.File, []byte) (int, error) { return 0, errors.New("secret-canary") }
		}},
		{"short write", func(o *extractionOps) { o.write = func(*os.File, []byte) (int, error) { return 0, nil } }},
		{"sync error", func(o *extractionOps) { o.sync = func(*os.File) error { return errors.New("secret-canary") } }},
		{"close error", func(o *extractionOps) {
			o.close = func(file *os.File) error { _ = file.Close(); return errors.New("secret-canary") }
		}},
		{"publication error", func(o *extractionOps) {
			o.publish = func(string, string) (bool, error) { return false, errors.New("secret-canary") }
		}},
		{"random error", func(o *extractionOps) { o.random = failingReader{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := privateCacheRoot(t)
			ops := defaultExtractionOps()
			test.edit(&ops)
			got, err := preparePayloadWith(context.Background(), root, bytes.NewReader(archive), manifest, ops)
			if got != "" || err != ErrExtraction || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("prepare = %q, %v", got, err)
			}
			assertNoOwnedStaging(t, filepath.Join(root, payloadCacheDirectory))
		})
	}
}

func TestCacheRejectsInvalidWinnerAndNeverRepairs(t *testing.T) {
	manifest, archive, _ := syntheticExecutableArchive(t)
	root := privateCacheRoot(t)
	toolsDir := filepath.Join(root, payloadCacheDirectory)
	if err := platform.CreatePrivateDir(toolsDir); err != nil {
		t.Fatal(err)
	}
	id, _ := packageID(manifest)
	winner := filepath.Join(toolsDir, id)
	if err := platform.CreatePrivateDir(winner); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(winner, "unchanged")
	if err := platform.WritePrivateFile(marker, []byte("unchanged")); err != nil {
		t.Fatal(err)
	}
	got, err := preparePayload(context.Background(), root, bytes.NewReader(archive), manifest)
	if got != "" || err != ErrExtraction {
		t.Fatalf("invalid winner = %q, %v", got, err)
	}
	contents, readErr := platform.ReadPrivateFile(marker, 100)
	if readErr != nil || string(contents) != "unchanged" {
		t.Fatal("invalid winner was repaired or removed")
	}
}

func TestCacheDetectsPublishedTamperAndExtras(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(t *testing.T, root string, manifest packageManifest)
	}{
		{"receipt", func(t *testing.T, root string, _ packageManifest) {
			path := filepath.Join(root, payloadReceiptName)
			if err := os.Chmod(path, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("forged"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"payload bytes", func(t *testing.T, root string, manifest packageManifest) {
			path := filepath.Join(root, filepath.FromSlash(manifest.Files[0].Path))
			if runtime.GOOS == "darwin" {
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"extra file", func(t *testing.T, root string, _ packageManifest) {
			if err := platform.WritePrivateFile(filepath.Join(root, "extra"), []byte("x")); err != nil {
				t.Fatal(err)
			}
		}},
		{"extra directory", func(t *testing.T, root string, _ packageManifest) {
			if err := platform.CreatePrivateDir(filepath.Join(root, "extra-dir")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest, archive, _ := syntheticExecutableArchive(t)
			cache := privateCacheRoot(t)
			root, err := preparePayload(context.Background(), cache, bytes.NewReader(archive), manifest)
			if err != nil {
				t.Fatal(err)
			}
			test.edit(t, root, manifest)
			if err := validatePayloadPackage(context.Background(), root, manifest); err != ErrExtraction {
				t.Fatalf("tamper validation = %v", err)
			}
			if got, err := preparePayload(context.Background(), cache, bytes.NewReader(archive), manifest); got != "" || err != ErrExtraction {
				t.Fatal("tampered package was repaired")
			}
		})
	}
}

func TestCacheConcurrentFirstRunAndAbandonedStaging(t *testing.T) {
	manifest, archive, _ := syntheticExecutableArchive(t)
	root := privateCacheRoot(t)
	toolsDir := filepath.Join(root, payloadCacheDirectory)
	if err := platform.CreatePrivateDir(toolsDir); err != nil {
		t.Fatal(err)
	}
	abandoned := filepath.Join(toolsDir, ".staging-abandoned")
	if err := platform.CreatePrivateDir(abandoned); err != nil {
		t.Fatal(err)
	}
	if err := platform.WritePrivateFile(filepath.Join(abandoned, "partial"), []byte("partial")); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	paths := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := preparePayload(context.Background(), root, bytes.NewReader(archive), manifest)
			paths <- path
			errs <- err
		}()
	}
	wg.Wait()
	close(paths)
	close(errs)
	var want string
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for path := range paths {
		if want == "" {
			want = path
		}
		if path != want {
			t.Fatal("different winners")
		}
	}
	if err := validatePayloadPackage(context.Background(), want, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abandoned); err != nil {
		t.Fatal("abandoned staging was touched")
	}
	assertNoOwnedStagingExcept(t, toolsDir, filepath.Base(abandoned))
}

func TestCacheKilledImmediatelyAroundPublication(t *testing.T) {
	for _, mode := range []string{"before", "after"} {
		t.Run(mode, func(t *testing.T) {
			manifest, archive, _ := syntheticExecutableArchive(t)
			root := privateCacheRoot(t)
			archivePath, manifestPath := writeExtractHelperInputs(t, root, manifest, archive)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExtractHelperProcess$")
			cmd.Env = append(os.Environ(), "SPARC_EXTRACT_HELPER=1", "SPARC_EXTRACT_ROOT="+root, "SPARC_EXTRACT_ARCHIVE="+archivePath, "SPARC_EXTRACT_MANIFEST="+manifestPath, "SPARC_EXTRACT_KILL_GATE="+mode)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = io.Discard
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			cleanupStartedCommand(t, cmd)
			if _, err := stdin.Write([]byte{1}); err != nil {
				t.Fatal(err)
			}
			var signal [1]byte
			if _, err := io.ReadFull(stdout, signal[:]); err != nil || string(signal[:]) != map[string]string{"before": "B", "after": "A"}[mode] {
				t.Fatalf("publication signal = %q, %v", signal, err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("killed helper exited successfully")
			}
			_ = stdin.Close()

			id, _ := packageID(manifest)
			toolsDir := filepath.Join(root, payloadCacheDirectory)
			destination := filepath.Join(toolsDir, id)
			if mode == "before" {
				if _, err := os.Lstat(destination); !os.IsNotExist(err) {
					t.Fatal("destination existed before publication")
				}
				entries, err := os.ReadDir(toolsDir)
				if err != nil {
					t.Fatal(err)
				}
				var abandoned string
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".staging-") {
						if abandoned != "" {
							t.Fatal("multiple abandoned staging directories")
						}
						abandoned = filepath.Join(toolsDir, entry.Name())
					}
				}
				if abandoned == "" || platform.CheckPrivateDir(abandoned) != nil || validatePayloadPackage(context.Background(), abandoned, manifest) != nil {
					t.Fatal("abandoned staging is absent or invalid")
				}
				winner, err := preparePayload(context.Background(), root, bytes.NewReader(archive), manifest)
				if err != nil || winner != destination {
					t.Fatalf("later prepare = %q, %v", winner, err)
				}
				if validatePayloadPackage(context.Background(), abandoned, manifest) != nil {
					t.Fatal("later prepare touched abandoned staging")
				}
				entries, err = os.ReadDir(toolsDir)
				if err != nil || len(entries) != 2 {
					t.Fatalf("unexpected lock/repair artifacts: %v, %v", entries, err)
				}
				return
			}
			if err := validatePayloadPackage(context.Background(), destination, manifest); err != nil {
				t.Fatal("published package invalid after helper kill", err)
			}
			reused, err := preparePayload(context.Background(), root, failingReadSeeker{}, manifest)
			if err != nil || reused != destination {
				t.Fatalf("published reuse = %q, %v", reused, err)
			}
			entries, err := os.ReadDir(toolsDir)
			if err != nil || len(entries) != 1 || entries[0].Name() != id {
				t.Fatalf("unexpected lock/staging artifacts: %v, %v", entries, err)
			}
		})
	}
}

func TestCacheIndependentProcessFirstRun(t *testing.T) {
	if os.Getenv("SPARC_EXTRACT_HELPER") != "" {
		t.Skip("parent-only")
	}
	manifest, archive, _ := syntheticExecutableArchive(t)
	root := privateCacheRoot(t)
	archivePath, manifestPath := writeExtractHelperInputs(t, root, manifest, archive)

	const workers = 3
	commands := make([]*exec.Cmd, workers)
	gates := make([]io.WriteCloser, workers)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := range commands {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExtractHelperProcess$")
		cmd.Env = append(os.Environ(), "SPARC_EXTRACT_HELPER=1", "SPARC_EXTRACT_ROOT="+root, "SPARC_EXTRACT_ARCHIVE="+archivePath, "SPARC_EXTRACT_MANIFEST="+manifestPath)
		gate, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cleanupStartedCommand(t, cmd)
		commands[i], gates[i] = cmd, gate
	}
	for _, gate := range gates {
		if _, err := gate.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		_ = gate.Close()
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	id, _ := packageID(manifest)
	if err := validatePayloadPackage(context.Background(), filepath.Join(root, payloadCacheDirectory, id), manifest); err != nil {
		t.Fatal(err)
	}
	assertNoOwnedStaging(t, filepath.Join(root, payloadCacheDirectory))
}

func cleanupStartedCommand(t *testing.T, command *exec.Cmd) {
	t.Helper()
	t.Cleanup(func() {
		if command.ProcessState == nil && command.Process != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
}

func writeExtractHelperInputs(t *testing.T, root string, manifest packageManifest, archive []byte) (string, string) {
	t.Helper()
	archivePath := filepath.Join(root, "archive.gz")
	manifestPath := filepath.Join(root, "manifest.gob")
	if err := platform.WritePrivateFile(archivePath, archive); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(manifest); err != nil {
		t.Fatal(err)
	}
	if err := platform.WritePrivateFile(manifestPath, encoded.Bytes()); err != nil {
		t.Fatal(err)
	}
	return archivePath, manifestPath
}

func parserFixtureFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func privateCacheRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func syntheticExecutableArchive(t *testing.T) (packageManifest, []byte, []testArchiveEntry) {
	t.Helper()
	syntheticFixture.once.Do(func() {
		executable, err := os.ReadFile(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		target := payloadTarget{OS: runtime.GOOS, Architecture: runtime.GOARCH}
		files := []payloadFile{
			{Path: "bin/pg_dump", Purpose: purposeExecutable, Mode: 0o700, Length: uint64(len(executable)), SHA256: sha256.Sum256(executable)},
			{Path: "bin/pg_restore", Purpose: purposeExecutable, Mode: 0o700, Length: uint64(len(executable)), SHA256: sha256.Sum256(executable)},
			{Path: "bin/psql", Purpose: purposeExecutable, Mode: 0o700, Length: uint64(len(executable)), SHA256: sha256.Sum256(executable)},
		}
		entries := make([]testArchiveEntry, len(files))
		for i, file := range files {
			entries[i] = testArchiveEntry{header: regularHeader(file.Path, file.Mode, int64(file.Length)), data: executable}
		}
		archive := buildArchive(t, entries, nil)
		manifest := packageManifest{SchemaVersion: payloadSchemaVersion, PostgreSQLMajor: supportedPostgreSQLMajor, Target: target, Files: files, Executables: map[Tool]string{PGDump: files[0].Path, PGRestore: files[1].Path, PSQL: files[2].Path}}
		syntheticFixture.manifest, syntheticFixture.archive = withArchive(manifest, archive)
		syntheticFixture.entries = entries
	})
	return cloneManifest(syntheticFixture.manifest), syntheticFixture.archive, cloneEntries(syntheticFixture.entries)
}

func regularHeader(name string, mode uint32, size int64) tar.Header {
	return tar.Header{Name: name, Mode: int64(mode), Size: size, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
}

func buildArchive(t *testing.T, entries []testArchiveEntry, trailing []byte) []byte {
	t.Helper()
	raw := buildTar(t, entries)
	raw = append(raw, trailing...)
	return gzipBytes(t, raw)
}

func buildArchiveWithTerminatorBlocks(t *testing.T, entries []testArchiveEntry, blocks int) []byte {
	t.Helper()
	raw := buildTar(t, entries)
	if len(raw) < 2*tarBlockBytes {
		t.Fatal("tar writer omitted canonical terminator")
	}
	raw = raw[:len(raw)-2*tarBlockBytes]
	raw = append(raw, make([]byte, blocks*tarBlockBytes)...)
	return gzipBytes(t, raw)
}

func buildTar(t *testing.T, entries []testArchiveEntry) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for i := range entries {
		header := entries[i].header
		if err := tw.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(entries[i].data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), raw.Bytes()...)
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func withArchive(manifest packageManifest, archive []byte) (packageManifest, []byte) {
	manifest = cloneManifest(manifest)
	manifest.CompressedLength = uint64(len(archive))
	manifest.CompressedSHA256 = sha256.Sum256(archive)
	return manifest, archive
}

func cloneEntries(entries []testArchiveEntry) []testArchiveEntry {
	out := make([]testArchiveEntry, len(entries))
	for i := range entries {
		out[i] = testArchiveEntry{header: entries[i].header, data: entries[i].data}
	}
	return out
}

type failingReadSeeker struct{ read, seek, offset bool }

func (f failingReadSeeker) Read([]byte) (int, error) {
	if f.read {
		return 0, errors.New("secret-canary")
	}
	return 0, io.EOF
}
func (f failingReadSeeker) Seek(int64, int) (int64, error) {
	if f.seek {
		return 0, errors.New("secret-canary")
	}
	if f.offset {
		return 1, nil
	}
	return 0, nil
}

type mutatingReadSeeker struct {
	first, second []byte
	reader        *bytes.Reader
	seeks         int
}

func newMutatingReadSeeker(first, second []byte) *mutatingReadSeeker {
	return &mutatingReadSeeker{first: first, second: second, reader: bytes.NewReader(first)}
}
func (r *mutatingReadSeeker) Read(buffer []byte) (int, error) { return r.reader.Read(buffer) }
func (r *mutatingReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == io.SeekStart {
		r.seeks++
		data := r.first
		if r.seeks >= 2 {
			data = r.second
		}
		r.reader = bytes.NewReader(data)
	}
	return r.reader.Seek(offset, whence)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("secret-canary") }

func assertNoOwnedStaging(t *testing.T, toolsDir string) { assertNoOwnedStagingExcept(t, toolsDir, "") }
func assertNoOwnedStagingExcept(t *testing.T, toolsDir, allowed string) {
	t.Helper()
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".staging-") && entry.Name() != allowed {
			t.Fatalf("staging remains: %s", entry.Name())
		}
	}
}
