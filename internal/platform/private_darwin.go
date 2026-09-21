package platform

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func NativeLocations() (Locations, error) {
	home, err := os.UserHomeDir()
	if err != nil || !validPath(home) {
		return Locations{}, ErrPrivateStorage
	}
	data := filepath.Join(home, "Library", "Application Support", "sparc")
	return locations(filepath.Join(data, "config.json"), data, filepath.Join(home, "Library", "Caches", "sparc"))
}

func validNativePath(path string) bool { return true }

func safeAncestors(path string) bool {
	if !validPath(path) {
		return false
	}
	for _, parent := range ancestors(path) {
		var st unix.Stat_t
		if unix.Lstat(parent, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || (st.Uid != uint32(os.Getuid()) && st.Uid != 0) {
			return false
		}
		if st.Mode&0022 != 0 {
			// Only named root-owned sticky system temp anchors may be shared/writable.
			if st.Uid != 0 || st.Mode&unix.S_ISVTX == 0 || (parent != "/private/tmp" && parent != "/private/var/tmp") {
				return false
			}
		}
	}
	return true
}

// This checks POSIX ownership/modes only, not inherited macOS extended ACLs.
// Effective ACL privacy remains a release gate; do not infer it from these modes.
func privateFD(fd int, directory bool) bool {
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) {
		return false
	}
	if directory {
		return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Mode&07777 == 0700 && st.Nlink >= 2
	}
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&07777 == 0600 && st.Nlink == 1
}

func CreatePrivateDir(path string) error {
	if !safeAncestors(path) || unix.Mkdir(path, 0700) != nil {
		return ErrPrivateStorage
	}
	if err := CheckPrivateDir(path); err != nil {
		_ = os.Remove(path)
		return ErrPrivateStorage
	}
	return nil
}

func CheckPrivateDir(path string) error {
	if !safeAncestors(path) {
		return ErrPrivateStorage
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return ErrPrivateStorage
	}
	valid := privateFD(fd, true)
	closeErr := unix.Close(fd)
	if !valid || closeErr != nil {
		return ErrPrivateStorage
	}
	return nil
}

func openPrivateFile(path string, write bool) (*os.File, error) {
	if !safeAncestors(path) {
		return nil, ErrPrivateStorage
	}
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
	if write {
		flags = unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_NOFOLLOW | unix.O_CLOEXEC
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return nil, ErrPrivateStorage
	}
	if !privateFD(fd, false) {
		_ = unix.Close(fd)
		if write {
			_ = os.Remove(path)
		}
		return nil, ErrPrivateStorage
	}
	return os.NewFile(uintptr(fd), path), nil
}
