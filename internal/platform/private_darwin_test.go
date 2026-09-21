package platform

import (
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
