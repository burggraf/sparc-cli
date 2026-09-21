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
	if directory {
		var st unix.Stat_t
		return unix.Fstat(fd, &st) == nil && st.Uid == uint32(os.Getuid()) && st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Mode&07777 == 0700 && st.Nlink >= 2
	}
	return privateRegularFD(fd, 0600)
}

func privateRegularFD(fd int, mode uint16) bool {
	var st unix.Stat_t
	return unix.Fstat(fd, &st) == nil && st.Uid == uint32(os.Getuid()) && st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&07777 == mode && st.Nlink == 1
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

func createPrivatePayloadFile(path string) (*os.File, error) {
	return openPrivateFile(path, true)
}

func openPrivatePayloadFile(path string, executable bool) (*os.File, error) {
	if !safeAncestors(path) {
		return nil, ErrPrivateStorage
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrPrivateStorage
	}
	mode := uint16(0600)
	if executable {
		mode = 0700
	}
	if !privateRegularFD(fd, mode) {
		_ = unix.Close(fd)
		return nil, ErrPrivateStorage
	}
	return os.NewFile(uintptr(fd), path), nil
}

func sealPrivateExecutable(file *os.File) error {
	return sealPrivateExecutableWith(file, (*os.File).Sync)
}

func sealPrivateExecutableWith(file *os.File, sync func(*os.File) error) error {
	fd := int(file.Fd())
	if !privateRegularFD(fd, 0600) || unix.Fchmod(fd, 0700) != nil || sync(file) != nil || !privateRegularFD(fd, 0700) {
		return ErrPrivateStorage
	}
	return nil
}

func localPrivateParentWith(path string, statfs func(int, *unix.Statfs_t) error, close func(int) error) bool {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	valid := privateFD(fd, true)
	var filesystem unix.Statfs_t
	statErr := statfs(fd, &filesystem)
	closeErr := close(fd)
	return valid && statErr == nil && filesystem.Flags&unix.MNT_LOCAL != 0 && closeErr == nil
}

func publishPrivateDir(staging, destination string) (bool, error) {
	return publishPrivateDirDarwinWith(staging, destination, unix.Fstatfs, unix.Close, unix.RenamexNp)
}

func publishPrivateDirDarwinWith(staging, destination string, statfs func(int, *unix.Statfs_t) error, close func(int) error, rename func(string, string, uint32) error) (bool, error) {
	if !localPrivateParentWith(filepath.Dir(staging), statfs, close) {
		return false, ErrPrivateStorage
	}
	err := rename(staging, destination, unix.RENAME_EXCL)
	if err == unix.EEXIST {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
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
