package tools

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestValidExecutableWindowsRejectsNonExecutablePE(t *testing.T) {
	executable, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	peOffset, characteristicsOffset, optionalOffset := peHeaderOffsets(t, executable)
	tests := []struct {
		name string
		edit func([]byte)
	}{
		{"wrong machine", func(data []byte) { binary.LittleEndian.PutUint16(data[peOffset+4:peOffset+6], 0xaa64) }},
		{"dll", func(data []byte) {
			value := binary.LittleEndian.Uint16(data[characteristicsOffset : characteristicsOffset+2])
			binary.LittleEndian.PutUint16(data[characteristicsOffset:characteristicsOffset+2], value|pe.IMAGE_FILE_DLL)
		}},
		{"not executable image", func(data []byte) {
			value := binary.LittleEndian.Uint16(data[characteristicsOffset : characteristicsOffset+2])
			binary.LittleEndian.PutUint16(data[characteristicsOffset:characteristicsOffset+2], value&^pe.IMAGE_FILE_EXECUTABLE_IMAGE)
		}},
		{"pe32 optional header", func(data []byte) { binary.LittleEndian.PutUint16(data[optionalOffset:optionalOffset+2], 0x10b) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := append([]byte(nil), executable...)
			test.edit(data)
			file := parserFixtureFile(t, data)
			defer file.Close()
			if validExecutable(file, payloadTarget{OS: "windows", Architecture: "amd64"}) {
				t.Fatal("non-executable PE accepted")
			}
		})
	}
	file := parserFixtureFile(t, executable)
	defer file.Close()
	if !validExecutable(file, payloadTarget{OS: "windows", Architecture: "amd64"}) {
		t.Fatal("native executable rejected")
	}
}

func TestValidatePayloadPackageWindowsRejectsDACLOnlyTamper(t *testing.T) {
	manifest, archive, _ := syntheticExecutableArchive(t)
	root, err := preparePayload(context.Background(), privateCacheRoot(t), bytes.NewReader(archive), manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(manifest.Files[0].Path))
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if err := validatePayloadPackage(context.Background(), root, manifest); err != ErrExtraction {
		t.Fatalf("DACL-only tamper accepted: %v", err)
	}
}

func peHeaderOffsets(t *testing.T, data []byte) (peOffset, characteristicsOffset, optionalOffset int) {
	t.Helper()
	if len(data) < 0x40 || string(data[:2]) != "MZ" {
		t.Fatal("invalid DOS header")
	}
	peOffset = int(binary.LittleEndian.Uint32(data[0x3c:0x40]))
	if peOffset < 0 || peOffset+26 > len(data) || string(data[peOffset:peOffset+4]) != "PE\x00\x00" {
		t.Fatal("invalid PE header")
	}
	return peOffset, peOffset + 22, peOffset + 24
}
