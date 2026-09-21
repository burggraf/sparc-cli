package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrivateDarwinLocationsHaveNoSideEffects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := NativeLocations()
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigFile != filepath.Join(home, "Library", "Application Support", "sparc", "config.json") || got.DataDir != filepath.Join(home, "Library", "Application Support", "sparc") || got.CacheDir != filepath.Join(home, "Library", "Caches", "sparc") {
		t.Fatalf("locations = %#v", got)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("locations wrote to filesystem")
	}
	t.Setenv("HOME", "relative-canary")
	_, err = NativeLocations()
	fixedError(t, err)
}

func TestPrivateDarwinModesAndDeniedPermissions(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "file")
	if err := WritePrivateFile(path, []byte("secret-canary")); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		path string
		mode os.FileMode
	}{{root, 0700}, {path, 0600}} {
		info, err := os.Stat(entry.path)
		if err != nil || info.Mode().Perm() != entry.mode {
			t.Fatalf("mode: %v %v", info, err)
		}
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	_, err := ReadPrivateFile(path, 100)
	fixedError(t, err)
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	fixedError(t, CheckPrivateDir(root))
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0700) })
	fixedError(t, WritePrivateFile(filepath.Join(root, "denied"), nil))
	fixedError(t, CreatePrivateDir(filepath.Join(root, "denied-dir")))
}

func TestPrivateDarwinRejectsSymlinksAndSpecialFiles(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "file")
	if err := WritePrivateFile(path, []byte("secret-canary")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	_, err := ReadPrivateFile(link, 100)
	fixedError(t, err)
	fixedError(t, WritePrivateFile(link, nil))
	if opened, err := OpenPrivatePayloadFile(link, false); err != ErrPrivateStorage || opened != nil {
		t.Fatalf("payload symlink opened: %v", err)
	}
	dirlink := filepath.Join(root, "dirlink")
	if err := os.Symlink(root, dirlink); err != nil {
		t.Fatal(err)
	}
	fixedError(t, CheckPrivateDir(dirlink))
	fixedError(t, CreatePrivateDir(filepath.Join(dirlink, "child")))
	fixedError(t, WritePrivateFile(filepath.Join(dirlink, "child"), nil))
	_, err = ReadPrivateFile(filepath.Join(dirlink, "file"), 100)
	fixedError(t, err)
	fifo := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = ReadPrivateFile(fifo, 100)
	fixedError(t, err)
}

func TestPrivateDarwinRejectsUnsafeAncestors(t *testing.T) {
	root := testRoot(t)
	parent := filepath.Join(root, "writable")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	fixedError(t, WritePrivateFile(filepath.Join(parent, "file"), nil))
	fixedError(t, CreatePrivateDir(filepath.Join(parent, "directory")))
	// A user-owned sticky directory is not the root-owned shared-temp exception.
	if err := os.Chmod(parent, 0777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	fixedError(t, CreatePrivateDir(filepath.Join(parent, "directory")))
}

func TestPrivateDarwinPayloadExecutableModes(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "tool")
	file, err := CreatePrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	if info, err := file.Stat(); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("creation mode = %v, %v", info, err)
	}
	if err := SealPrivateExecutable(file); err != nil {
		t.Fatal(err)
	}
	if info, err := file.Stat(); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("sealed mode = %v, %v", info, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := OpenPrivatePayloadFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenPrivatePayloadFile(path, false); err != ErrPrivateStorage || opened != nil {
		t.Fatalf("executable opened as data: %v", err)
	}
	if data, err := ReadPrivateFile(path, 100); err != ErrPrivateStorage || data != nil {
		t.Fatalf("legacy reader accepted executable: %v", err)
	}
}

func TestPrivateDarwinPayloadRejectsWrongModeAndLinks(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "tool")
	file, err := CreatePrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenPrivatePayloadFile(path, false); err != ErrPrivateStorage || opened != nil {
		t.Fatalf("wrong mode accepted: %v", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	file, err = os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fixedError(t, SealPrivateExecutable(file))
}

func TestPrivateDarwinPublishRejectsWrongModeAndSymlink(t *testing.T) {
	parent := testRoot(t)
	staging := filepath.Join(parent, "staging")
	if err := CreatePrivateDir(staging); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(staging, 0755); err != nil {
		t.Fatal(err)
	}
	published, err := PublishPrivateDir(staging, filepath.Join(parent, "published"))
	if published {
		t.Fatal("published wrong-mode source")
	}
	fixedError(t, err)
	if err := os.Chmod(staging, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(staging, link); err != nil {
		t.Fatal(err)
	}
	published, err = PublishPrivateDir(link, filepath.Join(parent, "linked-published"))
	if published {
		t.Fatal("published symlink source")
	}
	fixedError(t, err)
}

func TestPrivateDarwinPublishRequiresLocalParentDescriptor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		statfs func(int, *unix.Statfs_t) error
		close  func(int) error
	}{
		{
			name: "non-local filesystem",
			statfs: func(_ int, stat *unix.Statfs_t) error {
				*stat = unix.Statfs_t{}
				return nil
			},
			close: unix.Close,
		},
		{
			name:   "parent descriptor close failure",
			statfs: unix.Fstatfs,
			close: func(fd int) error {
				_ = unix.Close(fd)
				return errors.New("secret-canary")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parent := testRoot(t)
			staging := filepath.Join(parent, "staging")
			destination := filepath.Join(parent, "published")
			if err := CreatePrivateDir(staging); err != nil {
				t.Fatal(err)
			}
			renameCalled := false
			published, err := publishPrivateDirDarwinWith(staging, destination, test.statfs, test.close, func(from, to string, flags uint32) error {
				renameCalled = true
				return unix.RenamexNp(from, to, flags)
			})
			if published || renameCalled {
				t.Fatal("rename called through unqualified parent")
			}
			fixedError(t, err)
			if err := CheckPrivateDir(staging); err != nil {
				t.Fatal("staging changed")
			}
			if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
				t.Fatal("destination created")
			}
		})
	}
}

func TestPrivateDarwinPayloadSyncFailure(t *testing.T) {
	t.Parallel()
	root := testRoot(t)
	path := filepath.Join(root, "tool")
	file, err := CreatePrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fixedError(t, sealPrivateExecutableWith(file, func(*os.File) error { return errors.New("secret-canary") }))
}
