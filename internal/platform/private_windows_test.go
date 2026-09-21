package platform

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func setTestDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateWindowsRejectsAmbiguousPaths(t *testing.T) {
	root := testRoot(t)
	for _, path := range []string{`C:relative`, `\\server\share\file`, `\\?\C:\file`, `\\.\pipe\file`, filepath.Join(root, "file:stream"), filepath.Join(root, "CON.txt"), filepath.Join(root, "LPT1"), filepath.Join(root, "trailing."), filepath.Join(root, "trailing ")} {
		fixedError(t, CreatePrivateDir(path))
		fixedError(t, WritePrivateFile(path, nil))
		_, err := ReadPrivateFile(path, 100)
		fixedError(t, err)
	}
}

func TestPrivateWindowsProtectedDACLAtCreation(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "file")
	if err := WritePrivateFile(path, []byte("secret-canary")); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, path} {
		sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		control, _, err := sd.Control()
		if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
			t.Fatal("DACL not protected")
		}
		owner, _, err := sd.Owner()
		if err != nil || !owner.Equals(user.User.Sid) {
			t.Fatal("wrong owner")
		}
		// Exact canonical DACL freezes the at-creation policy independently of the validator.
		want, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;SY)")
		if err != nil {
			t.Fatal(err)
		}
		if sd.String() != want.String() {
			t.Fatalf("security descriptor = %s, want %s", sd.String(), want.String())
		}
	}
	setTestDACL(t, path, "D:P(A;;FA;;;WD)")
	_, err = ReadPrivateFile(path, 100)
	fixedError(t, err)
}

func TestPrivateWindowsRejectsWritableAncestor(t *testing.T) {
	root := testRoot(t)
	parent := filepath.Join(root, "parent")
	if err := CreatePrivateDir(parent); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	safe := "D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;SY)"
	setTestDACL(t, parent, safe+"(A;;GRGX;;;WD)")
	if err := WritePrivateFile(filepath.Join(parent, "readable-parent"), nil); err != nil {
		t.Fatalf("read/traverse ancestor rejected: %v", err)
	}
	setTestDACL(t, parent, safe+"(A;;GW;;;WD)")
	fixedError(t, CreatePrivateDir(filepath.Join(parent, "directory")))
	fixedError(t, WritePrivateFile(filepath.Join(parent, "file"), nil))
	_, err = ReadPrivateFile(filepath.Join(parent, "readable-parent"), 100)
	fixedError(t, err)
}

func TestPrivateWindowsRejectsReparsePoints(t *testing.T) {
	root := testRoot(t)
	target := filepath.Join(root, "target")
	if err := CreatePrivateDir(target); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	// This native qualification test requires Developer Mode or symlink privilege;
	// lack of privilege is a failing gate, not a skipped proof.
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	fixedError(t, CheckPrivateDir(link))
	fixedError(t, CreatePrivateDir(filepath.Join(link, "child")))
	fixedError(t, WritePrivateFile(filepath.Join(link, "child"), nil))
	file := filepath.Join(target, "file")
	if err := WritePrivateFile(file, []byte("secret-canary")); err != nil {
		t.Fatal(err)
	}
	_, err := ReadPrivateFile(filepath.Join(link, "file"), 100)
	fixedError(t, err)
	filelink := filepath.Join(root, "filelink")
	if err := os.Symlink(file, filelink); err != nil {
		t.Fatal(err)
	}
	_, err = ReadPrivateFile(filelink, 100)
	fixedError(t, err)
	fixedError(t, WritePrivateFile(filelink, nil))
}

func TestPrivateWindowsCreationDoesNotInheritBroadReadEntries(t *testing.T) {
	root := testRoot(t)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	setTestDACL(t, root, "D:P(A;;FA;;;"+user.User.Sid.String()+")(A;;FA;;;SY)(A;OICI;GRGX;;;WD)")
	child := filepath.Join(root, "child")
	if err := CreatePrivateDir(child); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateDir(child); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "COM12")
	if err := WritePrivateFile(path, []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(path, 100); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateWindowsRejectsNullDACL(t *testing.T) {
	root := testRoot(t)
	path := filepath.Join(root, "file")
	if err := WritePrivateFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	_, err := ReadPrivateFile(path, 100)
	fixedError(t, err)
}

func TestPrivateWindowsLocationsHaveNoSideEffects(t *testing.T) {
	roaming, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		t.Fatal(err)
	}
	local, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		t.Fatal(err)
	}
	want := Locations{ConfigFile: filepath.Join(roaming, "sparc", "config.json"), DataDir: filepath.Join(local, "sparc", "data"), CacheDir: filepath.Join(local, "sparc", "cache")}
	before := make(map[string]bool)
	for _, path := range []string{filepath.Dir(want.ConfigFile), filepath.Dir(want.DataDir), want.ConfigFile, want.DataDir, want.CacheDir} {
		_, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		before[path] = err == nil
	}
	got, err := NativeLocations()
	if err != nil || got != want {
		t.Fatalf("locations = %#v, %v", got, err)
	}
	for path, existed := range before {
		_, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if (err == nil) != existed {
			t.Fatal("location lookup changed filesystem")
		}
	}
}
