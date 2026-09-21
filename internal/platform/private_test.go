package platform

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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

func TestPrivatePayloadFileCreationAndOpen(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "payload")
	file, err := CreatePrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePrivateFile(path); err != ErrPrivateStorage {
		t.Fatalf("exclusive create error = %v", err)
	}
	opened, err := OpenPrivatePayloadFile(path, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(opened)
	closeErr := opened.Close()
	if err != nil || closeErr != nil || string(got) != "synthetic" {
		t.Fatalf("payload read = %q, %v, close %v", got, err, closeErr)
	}
	link := filepath.Join(root, "hard-link")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, link} {
		if opened, err := OpenPrivatePayloadFile(candidate, false); err != ErrPrivateStorage || opened != nil {
			t.Fatalf("hard-linked payload opened: %v", err)
		}
	}
}

func TestPrivateFileInjectedWriteAndCloseFailures(t *testing.T) {
	t.Parallel()
	root := testRoot(t)
	path := filepath.Join(root, "write-failure")
	fixedError(t, writePrivateFileWith(path, []byte("value"), func(*os.File, []byte) (int, error) {
		return 0, errors.New("secret-canary")
	}, (*os.File).Close))
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("failed write left file")
	}

	path = filepath.Join(root, "close-failure")
	fixedError(t, writePrivateFileWith(path, []byte("value"), (*os.File).Write, func(file *os.File) error {
		_ = file.Close()
		return errors.New("secret-canary")
	}))
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("failed close left file")
	}
}

func TestPrivatePublishNoReplace(t *testing.T) {
	parent := testRoot(t)
	staging := filepath.Join(parent, "staging")
	destination := filepath.Join(parent, "published")
	if err := CreatePrivateDir(staging); err != nil {
		t.Fatal(err)
	}
	published, err := PublishPrivateDir(staging, destination)
	if err != nil || !published {
		t.Fatalf("PublishPrivateDir() = %v, %v", published, err)
	}
	if _, err := os.Lstat(staging); !os.IsNotExist(err) {
		t.Fatal("published source remains")
	}
	if err := CheckPrivateDir(destination); err != nil {
		t.Fatal(err)
	}

	loser := filepath.Join(parent, "loser")
	if err := CreatePrivateDir(loser); err != nil {
		t.Fatal(err)
	}
	published, err = PublishPrivateDir(loser, destination)
	if err != nil || published {
		t.Fatalf("collision = %v, %v", published, err)
	}
	if err := CheckPrivateDir(loser); err != nil {
		t.Fatal("collision removed loser")
	}
	if err := CheckPrivateDir(destination); err != nil {
		t.Fatal("collision altered winner")
	}

	// Collision does not inspect, repair, replace, or delete an invalid winner.
	invalidWinner := filepath.Join(parent, "invalid-winner")
	if err := os.WriteFile(invalidWinner, []byte("unchanged"), 0644); err != nil {
		t.Fatal(err)
	}
	secondLoser := filepath.Join(parent, "second-loser")
	if err := CreatePrivateDir(secondLoser); err != nil {
		t.Fatal(err)
	}
	published, err = PublishPrivateDir(secondLoser, invalidWinner)
	if err != nil || published {
		t.Fatalf("invalid-winner collision = %v, %v", published, err)
	}
	got, err := os.ReadFile(invalidWinner)
	if err != nil || string(got) != "unchanged" {
		t.Fatal("collision inspected or altered invalid winner")
	}
	if err := CheckPrivateDir(secondLoser); err != nil {
		t.Fatal("collision removed second loser")
	}
}

func TestPrivatePublishRejectsInvalidBoundaries(t *testing.T) {
	parent := testRoot(t)
	other := testRoot(t)
	staging := filepath.Join(parent, "staging")
	if err := CreatePrivateDir(staging); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{staging, filepath.Join(other, "destination"), filepath.Join(parent, "..", "escape"), "relative"} {
		published, err := PublishPrivateDir(staging, destination)
		if published {
			t.Fatalf("published invalid destination %q", destination)
		}
		fixedError(t, err)
	}
	file := filepath.Join(parent, "file")
	if err := WritePrivateFile(file, nil); err != nil {
		t.Fatal(err)
	}
	published, err := PublishPrivateDir(file, filepath.Join(parent, "file-destination"))
	if published {
		t.Fatal("published regular file as directory")
	}
	fixedError(t, err)
}

func TestPrivatePublishConcurrentOneWinner(t *testing.T) {
	parent := testRoot(t)
	destination := filepath.Join(parent, "published")
	const contenders = 8
	staging := make([]string, contenders)
	for i := range staging {
		staging[i] = filepath.Join(parent, fmt.Sprintf("staging-%d", i))
		if err := CreatePrivateDir(staging[i]); err != nil {
			t.Fatal(err)
		}
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for _, path := range staging {
		wg.Add(1)
		go func() {
			defer wg.Done()
			published, err := PublishPrivateDir(path, destination)
			if err != nil {
				t.Errorf("publish error: %v", err)
			} else if published {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("winners = %d", winners.Load())
	}
	if err := CheckPrivateDir(destination); err != nil {
		t.Fatal(err)
	}
}

func TestPrivatePublishInjectedFailure(t *testing.T) {
	t.Parallel()
	parent := testRoot(t)
	staging := filepath.Join(parent, "staging")
	destination := filepath.Join(parent, "published")
	if err := CreatePrivateDir(staging); err != nil {
		t.Fatal(err)
	}
	published, err := publishPrivateDirWith(staging, destination, func(string, string) (bool, error) {
		return false, errors.New("secret-canary")
	})
	if published {
		t.Fatal("injected failure published")
	}
	fixedError(t, err)
	if err := CheckPrivateDir(staging); err != nil {
		t.Fatal("injected failure removed staging")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("injected failure created destination")
	}
}
