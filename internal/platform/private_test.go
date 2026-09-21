package platform

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func testRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(root, "private")
	if err := CreatePrivateDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func fixedError(t *testing.T, err error) {
	t.Helper()
	if err != ErrPrivateStorage || errors.Unwrap(err) != nil || err.Error() != "private storage unavailable" {
		t.Fatalf("error = %v, want fixed sentinel", err)
	}
}

func TestPrivateLocations(t *testing.T) {
	locations, err := NativeLocations()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{locations.ConfigFile, locations.DataDir, locations.CacheDir} {
		if !filepath.IsAbs(path) {
			t.Fatalf("non-absolute location: %q", path)
		}
	}
	if locations.DataDir == locations.CacheDir {
		t.Fatal("data/cache locations overlap")
	}
}

func TestPrivateStorageRoundTripAndExclusiveCreation(t *testing.T) {
	root := testRoot(t)
	if err := CheckPrivateDir(root); err != nil {
		t.Fatal(err)
	}
	fixedError(t, CreatePrivateDir(root))
	path := filepath.Join(root, "秘密")
	want := []byte("synthetic-canary")
	if err := WritePrivateFile(path, want); err != nil {
		t.Fatal(err)
	}
	fixedError(t, WritePrivateFile(path, []byte("overwrite")))
	got, err := ReadPrivateFile(path, int64(len(want)))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("read = %q, %v", got, err)
	}
	fixedError(t, CheckPrivateDir(path))
	for _, limit := range []int64{-1, 0, int64(len(want) - 1), 1<<63 - 1} {
		data, err := ReadPrivateFile(path, limit)
		fixedError(t, err)
		if data != nil {
			t.Fatal("partial data returned")
		}
	}
	data, err := ReadPrivateFile(root, 100)
	fixedError(t, err)
	if data != nil {
		t.Fatal("directory data returned")
	}
	empty := filepath.Join(root, "empty")
	if err := WritePrivateFile(empty, nil); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadPrivateFile(empty, 1); err != nil || len(data) != 0 {
		t.Fatalf("empty read: %v", err)
	}
}

func TestPrivateStorageUnsafePaths(t *testing.T) {
	root := testRoot(t)
	for _, path := range []string{"", "relative-secret-canary", root + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "escape", filepath.Join(root, "secret-canary\x1b"), filepath.Join(root, "nul\x00"), filepath.Join(root, "bad\xff"), filepath.Join(root, "missing", "file")} {
		fixedError(t, CreatePrivateDir(path))
		fixedError(t, CheckPrivateDir(path))
		fixedError(t, WritePrivateFile(path, []byte("secret-canary")))
		data, err := ReadPrivateFile(path, 100)
		fixedError(t, err)
		if data != nil {
			t.Fatal("data returned on error")
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsafe paths changed root: %v", err)
	}
}

func TestPrivateStorageConcurrentCreation(t *testing.T) {
	for _, directory := range []bool{false, true} {
		root := testRoot(t)
		path := filepath.Join(root, "winner")
		var winners atomic.Int32
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if directory {
					err = CreatePrivateDir(path)
				} else {
					err = WritePrivateFile(path, []byte("one"))
				}
				if err == nil {
					winners.Add(1)
				} else if err != ErrPrivateStorage {
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		wg.Wait()
		if winners.Load() != 1 {
			t.Fatalf("winners = %d", winners.Load())
		}
	}
}

func TestPrivateStorageRejectsHardLinks(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "original")
	if err := WritePrivateFile(path, []byte("secret-canary")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{path, link} {
		data, err := ReadPrivateFile(path, 100)
		fixedError(t, err)
		if data != nil {
			t.Fatal("hard-link content disclosed")
		}
	}
}

func TestPrivateStorageMissingErrorsDoNotDisclosePaths(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "secret-canary")
	data, err := ReadPrivateFile(path, 100)
	fixedError(t, err)
	if data != nil || strings.Contains(err.Error(), "secret-canary") {
		t.Fatal("path/data disclosed")
	}
}
