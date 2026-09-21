package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

const fileFullControl = 0x1f01ff

func NativeLocations() (Locations, error) {
	config, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return Locations{}, ErrPrivateStorage
	}
	local, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return Locations{}, ErrPrivateStorage
	}
	return locations(filepath.Join(config, "sparc", "config.json"), filepath.Join(local, "sparc", "data"), filepath.Join(local, "sparc", "cache"))
}

func validNativePath(path string) bool {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' || len(path) < 3 || path[2] != '\\' || strings.Contains(path[2:], ":") {
		return false
	}
	if (volume[0] < 'A' || volume[0] > 'Z') && (volume[0] < 'a' || volume[0] > 'z') {
		return false
	}
	for _, part := range pathComponents(path[3:]) {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsAny(part, `<>"|?*`) {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if len(base) > 3 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && utf8.RuneCountInString(base[3:]) == 1 && strings.Contains("123456789¹²³", base[3:]) {
			return false
		}
	}
	return true
}

func userSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, ErrPrivateStorage
	}
	return user.User.Sid, nil
}

func privateSecurity() (*windows.SecurityAttributes, error) {
	sid, err := userSID()
	if err != nil {
		return nil, ErrPrivateStorage
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;;FA;;;" + sid.String() + ")(A;;FA;;;SY)")
	if err != nil {
		return nil, ErrPrivateStorage
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}

func trustedSID(sid, user *windows.SID, ancestor bool) bool {
	return sid.Equals(user) || sid.IsWellKnown(windows.WinLocalSystemSid) || (ancestor && sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
}

// Ancestors may grant ordinary read/traverse to others, but not any additional
// rights. Unsupported ACE forms fail closed rather than attempting full AccessCheck.
func safeDACL(handle windows.Handle, private bool) bool {
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	defer func() { runtime.KeepAlive(sd) }()
	user, err := userSID()
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !trustedSID(owner, user, !private) || (private && !owner.Equals(user)) {
		return false
	}
	control, _, err := sd.Control()
	if err != nil || (private && control&windows.SE_DACL_PROTECTED == 0) {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || (private && acl.AceCount != 2) {
		return false
	}
	seenUser, seenSystem := false, false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace == nil || ace.Header.AceSize < 20 {
			return false
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE && ace.Header.AceType != windows.ACCESS_DENIED_ACE_TYPE {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() || sid.Len()+8 > int(ace.Header.AceSize) {
			return false
		}
		if private {
			if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != 0 || ace.Mask != fileFullControl {
				return false
			}
			if sid.Equals(user) && !seenUser {
				seenUser = true
			} else if sid.IsWellKnown(windows.WinLocalSystemSid) && !seenSystem {
				seenSystem = true
			} else {
				return false
			}
		} else {
			if ace.Header.AceFlags & ^uint8(0x1f) != 0 {
				return false
			}
			const readOnly = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE | windows.GENERIC_READ | windows.GENERIC_EXECUTE
			if ace.Header.AceType == windows.ACCESS_ALLOWED_ACE_TYPE && uint32(ace.Mask)&^uint32(readOnly) != 0 && !trustedSID(sid, user, true) {
				return false
			}
		}
	}
	return !private || (seenUser && seenSystem)
}

func openDirectory(path string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, ErrPrivateStorage
	}
	return windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
}

func diskObject(handle windows.Handle, directory bool, private bool) bool {
	kind, err := windows.GetFileType(handle)
	if err != nil || kind != windows.FILE_TYPE_DISK {
		return false
	}
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	if (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		return false
	}
	if private && info.NumberOfLinks != 1 {
		return false
	}
	return true
}

func safeAncestors(path string) bool {
	if !validPath(path) {
		return false
	}
	for i, parent := range ancestors(path) {
		handle, err := openDirectory(parent)
		if err != nil {
			return false
		}
		valid := diskObject(handle, true, false)
		if i == 0 {
			// The local fixed/removable volume root is the only trusted ACL anchor.
			name, err := windows.UTF16PtrFromString(parent)
			if err != nil {
				valid = false
			} else {
				kind := windows.GetDriveType(name)
				valid = valid && (kind == windows.DRIVE_FIXED || kind == windows.DRIVE_REMOVABLE)
			}
		} else {
			valid = valid && safeDACL(handle, false)
		}
		closeErr := windows.CloseHandle(handle)
		if !valid || closeErr != nil {
			return false
		}
	}
	return true
}

func CreatePrivateDir(path string) error {
	if !safeAncestors(path) {
		return ErrPrivateStorage
	}
	sa, err := privateSecurity()
	if err != nil {
		return ErrPrivateStorage
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrPrivateStorage
	}
	err = windows.CreateDirectory(name, sa)
	runtime.KeepAlive(sa)
	if err != nil {
		return ErrPrivateStorage
	}
	if CheckPrivateDir(path) != nil {
		_ = os.Remove(path)
		return ErrPrivateStorage
	}
	return nil
}

func CheckPrivateDir(path string) error {
	if !safeAncestors(path) {
		return ErrPrivateStorage
	}
	handle, err := openDirectory(path)
	if err != nil {
		return ErrPrivateStorage
	}
	valid := diskObject(handle, true, true) && safeDACL(handle, true)
	closeErr := windows.CloseHandle(handle)
	if !valid || closeErr != nil {
		return ErrPrivateStorage
	}
	return nil
}

func createPrivatePayloadFile(path string) (*os.File, error) {
	return openPrivateFile(path, true)
}

func openPrivatePayloadFile(path string, _ bool) (*os.File, error) {
	return openPrivateFile(path, false)
}

func sealPrivateExecutable(file *os.File) error {
	return sealPrivateExecutableWith(file, (*os.File).Sync)
}

func sealPrivateExecutableWith(file *os.File, sync func(*os.File) error) error {
	handle := windows.Handle(file.Fd())
	if !diskObject(handle, false, true) || !safeDACL(handle, true) || sync(file) != nil || !diskObject(handle, false, true) || !safeDACL(handle, true) {
		return ErrPrivateStorage
	}
	return nil
}

func publishPrivateDir(staging, destination string) (bool, error) {
	from, err := windows.UTF16PtrFromString(staging)
	if err != nil {
		return false, err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return false, err
	}
	err = windows.MoveFile(from, to)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
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
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, ErrPrivateStorage
	}
	access, disposition := uint32(windows.GENERIC_READ|windows.READ_CONTROL), uint32(windows.OPEN_EXISTING)
	var sa *windows.SecurityAttributes
	if write {
		access, disposition = windows.GENERIC_WRITE|windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.CREATE_NEW
		sa, err = privateSecurity()
		if err != nil {
			return nil, ErrPrivateStorage
		}
	}
	handle, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ, sa, disposition, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	runtime.KeepAlive(sa)
	if err != nil {
		return nil, ErrPrivateStorage
	}
	if !diskObject(handle, false, true) || !safeDACL(handle, true) {
		_ = windows.CloseHandle(handle)
		if write {
			_ = os.Remove(path)
		}
		return nil, ErrPrivateStorage
	}
	return os.NewFile(uintptr(handle), path), nil
}
