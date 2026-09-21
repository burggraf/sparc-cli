package tools

import (
	"bytes"
	"context"
	"debug/macho"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestValidExecutableDarwinRejectsNonExecutableMachO(t *testing.T) {
	executable, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	order := machoByteOrder(t, executable)
	target := payloadTarget{OS: "darwin", Architecture: runtimeArchitecture()}
	tests := []struct {
		name string
		edit func([]byte)
	}{
		{"object", func(data []byte) { order.PutUint32(data[12:16], uint32(macho.TypeObj)) }},
		{"dylib", func(data []byte) { order.PutUint32(data[12:16], uint32(macho.TypeDylib)) }},
		{"wrong cpu", func(data []byte) {
			cpu := macho.CpuAmd64
			if target.Architecture == "amd64" {
				cpu = macho.CpuArm64
			}
			order.PutUint32(data[4:8], uint32(cpu))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := append([]byte(nil), executable...)
			test.edit(data)
			file := parserFixtureFile(t, data)
			defer file.Close()
			if validExecutable(file, target) {
				t.Fatal("non-executable Mach-O accepted")
			}
		})
	}
	file := parserFixtureFile(t, executable)
	defer file.Close()
	if !validExecutable(file, target) {
		t.Fatal("native executable rejected")
	}
}

func TestValidatePayloadPackageDarwinRejectsModeOnlyTamper(t *testing.T) {
	for _, test := range []struct {
		name string
		path func(string, packageManifest) string
		mode os.FileMode
	}{
		{"executable", func(root string, manifest packageManifest) string {
			return filepath.Join(root, filepath.FromSlash(manifest.Files[0].Path))
		}, 0o600},
		{"receipt", func(root string, _ packageManifest) string { return filepath.Join(root, payloadReceiptName) }, 0o644},
		{"directory", func(root string, _ packageManifest) string { return filepath.Join(root, "bin") }, 0o755},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest, archive, _ := syntheticExecutableArchive(t)
			root, err := preparePayload(context.Background(), privateCacheRoot(t), bytes.NewReader(archive), manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(test.path(root, manifest), test.mode); err != nil {
				t.Fatal(err)
			}
			if err := validatePayloadPackage(context.Background(), root, manifest); err != ErrExtraction {
				t.Fatalf("mode-only tamper accepted: %v", err)
			}
		})
	}
}

func machoByteOrder(t *testing.T, data []byte) binary.ByteOrder {
	t.Helper()
	if len(data) < 16 {
		t.Fatal("short Mach-O fixture")
	}
	switch binary.LittleEndian.Uint32(data[:4]) {
	case macho.Magic32, macho.Magic64:
		return binary.LittleEndian
	}
	switch binary.BigEndian.Uint32(data[:4]) {
	case macho.Magic32, macho.Magic64:
		return binary.BigEndian
	}
	t.Fatal("native executable is not thin Mach-O")
	return nil
}

func runtimeArchitecture() string {
	file, err := macho.Open(os.Args[0])
	if err != nil {
		return ""
	}
	defer file.Close()
	if file.Cpu == macho.CpuArm64 {
		return "arm64"
	}
	if file.Cpu == macho.CpuAmd64 {
		return "amd64"
	}
	return ""
}
