package tools

import (
	"crypto/sha256"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func syntheticManifest() packageManifest {
	files := []payloadFile{
		{Path: "bin/pg_dump", Purpose: purposeExecutable, Mode: 0o700, Length: 11, SHA256: sha256.Sum256([]byte("pg_dump-v1"))},
		{Path: "bin/pg_restore", Purpose: purposeExecutable, Mode: 0o700, Length: 14, SHA256: sha256.Sum256([]byte("pg_restore-v1"))},
		{Path: "bin/psql", Purpose: purposeExecutable, Mode: 0o700, Length: 8, SHA256: sha256.Sum256([]byte("psql-v1"))},
		{Path: "lib/runtime.dat", Purpose: purposeRuntime, Mode: 0o600, Length: 10, SHA256: sha256.Sum256([]byte("runtime-v1"))},
		{Path: "NOTICE.txt", Purpose: purposeNotice, Mode: 0o600, Length: 9, SHA256: sha256.Sum256([]byte("notice-v1"))},
	}
	return packageManifest{
		SchemaVersion:    payloadSchemaVersion,
		PostgreSQLMajor:  supportedPostgreSQLMajor,
		Target:           payloadTarget{OS: "darwin", Architecture: "arm64"},
		CompressedLength: 100,
		CompressedSHA256: sha256.Sum256([]byte("compressed-v1")),
		Files:            files,
		Executables:      map[Tool]string{PGDump: files[0].Path, PGRestore: files[1].Path, PSQL: files[2].Path},
	}
}

func cloneManifest(in packageManifest) packageManifest {
	out := in
	out.Files = append([]payloadFile(nil), in.Files...)
	out.Executables = make(map[Tool]string, len(in.Executables))
	for tool, path := range in.Executables {
		out.Executables[tool] = path
	}
	return out
}

func requireInvalidPayload(t *testing.T, manifest packageManifest) {
	t.Helper()
	err := validateManifest(manifest)
	if err != ErrInvalidPayload || errors.Unwrap(err) != nil || err.Error() != "invalid tool payload" {
		t.Fatalf("validateManifest() error = %v, want fixed invalid sentinel", err)
	}
}

func TestPayloadValidSyntheticManifest(t *testing.T) {
	manifest := syntheticManifest()
	if err := validateManifest(manifest); err != nil {
		t.Fatalf("validateManifest() error = %v", err)
	}
	first, err := packageID(manifest)
	if err != nil {
		t.Fatal(err)
	}
	const wantID = "3e76db2acc8ac60d86de52653e491624f38a2a114eb198769a18c7688d0c764e"
	if first != wantID {
		t.Fatalf("packageID() = %q, want %q", first, wantID)
	}
	second, err := packageID(cloneManifest(manifest))
	if err != nil || first != second || len(first) != sha256.Size*2 {
		t.Fatalf("package IDs = %q, %q, %v", first, second, err)
	}
}

func TestPayloadRejectsUnknownContractValues(t *testing.T) {
	tests := []struct {
		name string
		edit func(*packageManifest)
	}{
		{"schema", func(m *packageManifest) { m.SchemaVersion++ }},
		{"major", func(m *packageManifest) { m.PostgreSQLMajor++ }},
		{"operating system", func(m *packageManifest) { m.Target.OS = "linux" }},
		{"architecture", func(m *packageManifest) { m.Target.Architecture = "386" }},
		{"windows arm64", func(m *packageManifest) { m.Target = payloadTarget{OS: "windows", Architecture: "arm64"} }},
		{"purpose", func(m *packageManifest) { m.Files[3].Purpose = filePurpose(255) }},
		{"tool", func(m *packageManifest) { m.Executables[Tool(255)] = m.Files[0].Path }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneManifest(syntheticManifest())
			test.edit(&manifest)
			requireInvalidPayload(t, manifest)
		})
	}
}

func TestPayloadRejectsPathCollisionsAndNonportableNames(t *testing.T) {
	tests := []struct {
		name string
		path string
		edit func(*packageManifest)
	}{
		{name: "duplicate", edit: func(m *packageManifest) { m.Files = append(m.Files, m.Files[0]) }},
		{name: "case collision", path: "BIN/PG_DUMP"},
		{name: "file directory collision", path: "bin"},
		{name: "case file directory collision", path: "BIN/PG_DUMP/child"},
		{name: "absolute", path: "/bin/tool"},
		{name: "dot", path: "bin/./tool"},
		{name: "traversal", path: "bin/../tool"},
		{name: "backslash", path: `bin\tool`},
		{name: "drive", path: `C:/tool`},
		{name: "empty component", path: "bin//tool"},
		{name: "control", path: "bin/secret\ncanary"},
		{name: "non ASCII", path: "bin/café"},
		{name: "wildcard", path: "bin/to*ol"},
		{name: "trailing dot", path: "bin/tool."},
		{name: "trailing space", path: "bin/tool "},
		{name: "reserved", path: "bin/CON.txt"},
		{name: "too long", path: strings.Repeat("a", maxPayloadPathBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneManifest(syntheticManifest())
			if test.edit != nil {
				test.edit(&manifest)
			} else {
				file := manifest.Files[3]
				file.Path = test.path
				manifest.Files = append(manifest.Files, file)
			}
			requireInvalidPayload(t, manifest)
		})
	}
}

func TestPayloadRejectsInvalidFilesAndBounds(t *testing.T) {
	zeroDigest := [sha256.Size]byte{}
	tests := []struct {
		name string
		edit func(*packageManifest)
	}{
		{"compressed zero length", func(m *packageManifest) { m.CompressedLength = 0 }},
		{"compressed too large", func(m *packageManifest) { m.CompressedLength = maxCompressedBytes + 1 }},
		{"compressed zero digest", func(m *packageManifest) { m.CompressedSHA256 = zeroDigest }},
		{"file zero length", func(m *packageManifest) { m.Files[3].Length = 0 }},
		{"file too large", func(m *packageManifest) { m.Files[3].Length = maxPayloadFileBytes + 1 }},
		{"file zero digest", func(m *packageManifest) { m.Files[3].SHA256 = zeroDigest }},
		{"executable mode", func(m *packageManifest) { m.Files[0].Mode = 0o600 }},
		{"runtime mode", func(m *packageManifest) { m.Files[3].Mode = 0o700 }},
		{"unknown mode bits", func(m *packageManifest) { m.Files[3].Mode = 0o1600 }},
		{"expanded too large", func(m *packageManifest) {
			for i := range m.Files {
				m.Files[i].Length = maxPayloadFileBytes
			}
		}},
		{"too many files", func(m *packageManifest) {
			file := m.Files[3]
			for len(m.Files) <= maxPayloadFiles {
				file.Path = "extra/" + strings.Repeat("x", len(m.Files)/10) + string(rune('a'+len(m.Files)%10))
				m.Files = append(m.Files, file)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneManifest(syntheticManifest())
			test.edit(&manifest)
			requireInvalidPayload(t, manifest)
		})
	}
	if _, ok := boundedAdd(math.MaxUint64, 1, math.MaxUint64); ok {
		t.Fatal("boundedAdd accepted uint64 overflow")
	}
	if total, ok := boundedAdd(maxExpandedBytes-1, 1, maxExpandedBytes); !ok || total != maxExpandedBytes {
		t.Fatal("boundedAdd rejected exact bound")
	}
}

func TestPayloadRejectsMissingOrInvalidExecutableMappings(t *testing.T) {
	tests := []struct {
		name string
		edit func(*packageManifest)
	}{
		{"missing dump", func(m *packageManifest) { delete(m.Executables, PGDump) }},
		{"missing restore", func(m *packageManifest) { delete(m.Executables, PGRestore) }},
		{"missing psql", func(m *packageManifest) { delete(m.Executables, PSQL) }},
		{"extra mapping", func(m *packageManifest) { m.Executables[Tool(99)] = m.Files[0].Path }},
		{"missing file", func(m *packageManifest) { m.Executables[PSQL] = "bin/missing" }},
		{"non executable file", func(m *packageManifest) { m.Executables[PSQL] = "lib/runtime.dat" }},
		{"shared executable", func(m *packageManifest) { m.Executables[PSQL] = m.Executables[PGDump] }},
		{"unmapped executable", func(m *packageManifest) {
			m.Files = append(m.Files, payloadFile{Path: "bin/helper", Purpose: purposeExecutable, Mode: 0o700, Length: 6, SHA256: sha256.Sum256([]byte("helper"))})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneManifest(syntheticManifest())
			test.edit(&manifest)
			requireInvalidPayload(t, manifest)
		})
	}
}

func TestPayloadPackageIDBindsValidSecurityRelevantFields(t *testing.T) {
	base := syntheticManifest()
	baseID, err := packageID(base)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*packageManifest)
	}{
		{"platform", func(m *packageManifest) { m.Target.Architecture = "amd64" }},
		{"runtime purpose", func(m *packageManifest) { m.Files[4].Purpose = purposeRuntime }},
		{"runtime path", func(m *packageManifest) { m.Files[3].Path = "lib/other.dat" }},
		{"length", func(m *packageManifest) { m.Files[3].Length++ }},
		{"file digest", func(m *packageManifest) { m.Files[3].SHA256 = sha256.Sum256([]byte("changed")) }},
		{"compressed digest", func(m *packageManifest) { m.CompressedSHA256 = sha256.Sum256([]byte("changed")) }},
		{"compressed length", func(m *packageManifest) { m.CompressedLength++ }},
		{"executable mappings", func(m *packageManifest) {
			m.Executables[PGDump], m.Executables[PSQL] = m.Executables[PSQL], m.Executables[PGDump]
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			changed := cloneManifest(base)
			mutation.edit(&changed)
			changedID, err := packageID(changed)
			if err != nil {
				t.Fatalf("valid mutation rejected: %v", err)
			}
			if changedID == baseID {
				t.Fatal("package ID did not change")
			}
		})
	}

	reordered := cloneManifest(base)
	for left, right := 0, len(reordered.Files)-1; left < right; left, right = left+1, right-1 {
		reordered.Files[left], reordered.Files[right] = reordered.Files[right], reordered.Files[left]
	}
	reordered.Executables = map[Tool]string{PSQL: base.Executables[PSQL], PGDump: base.Executables[PGDump], PGRestore: base.Executables[PGRestore]}
	reorderedID, err := packageID(reordered)
	if err != nil || reorderedID != baseID {
		t.Fatalf("reordered package ID = %q, %v; want %q", reorderedID, err, baseID)
	}
}

func TestPayloadPackageIDRejectsInvalidManifest(t *testing.T) {
	tests := []struct {
		name string
		edit func(*packageManifest)
	}{
		{"schema", func(m *packageManifest) { m.SchemaVersion++ }},
		{"major", func(m *packageManifest) { m.PostgreSQLMajor++ }},
		{"mode", func(m *packageManifest) { m.Files[3].Mode = 0o700 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneManifest(syntheticManifest())
			test.edit(&manifest)
			id, err := packageID(manifest)
			if id != "" || err != ErrInvalidPayload || errors.Unwrap(err) != nil {
				t.Fatalf("packageID() = %q, %v; want empty ID and exact invalid sentinel", id, err)
			}
		})
	}
}

func TestPayloadManifestIdentityIncludesRejectedFields(t *testing.T) {
	base := syntheticManifest()
	baseIdentity := manifestIdentity(base)
	tests := []struct {
		name string
		edit func(*packageManifest)
	}{
		{"unsupported schema", func(m *packageManifest) { m.SchemaVersion++ }},
		{"unsupported major", func(m *packageManifest) { m.PostgreSQLMajor++ }},
		{"unsupported operating system", func(m *packageManifest) { m.Target.OS = "linux" }},
		{"invalid file mode", func(m *packageManifest) { m.Files[3].Mode = 0o700 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneManifest(base)
			test.edit(&manifest)
			if manifestIdentity(manifest) == baseIdentity {
				t.Fatal("canonical identity did not include rejected field")
			}
			id, err := packageID(manifest)
			if id != "" || err != ErrInvalidPayload || errors.Unwrap(err) != nil {
				t.Fatalf("packageID() = %q, %v; want empty ID and exact invalid sentinel", id, err)
			}
		})
	}
}

func TestPayloadSelectionValidatesSelectedCompiledManifest(t *testing.T) {
	valid := syntheticManifest()
	selected, err := selectPayload([]packageManifest{valid}, PGDump, supportedPostgreSQLMajor, valid.Target)
	if err != nil || selected.Executables[PGDump] != valid.Executables[PGDump] {
		t.Fatalf("selectPayload() = %#v, %v", selected, err)
	}

	invalidCases := []struct {
		name string
		edit func(*packageManifest)
	}{
		{"schema", func(m *packageManifest) { m.SchemaVersion++ }},
		{"digest", func(m *packageManifest) { m.Files[0].SHA256 = [sha256.Size]byte{} }},
		{"mapping", func(m *packageManifest) { delete(m.Executables, PSQL) }},
	}
	for _, test := range invalidCases {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneManifest(valid)
			test.edit(&manifest)
			selected, err := selectPayload([]packageManifest{manifest}, PGDump, supportedPostgreSQLMajor, valid.Target)
			if selected.SchemaVersion != 0 || selected.Files != nil || selected.Executables != nil || err != ErrInvalidPayload || errors.Unwrap(err) != nil {
				t.Fatalf("selectPayload() = %#v, %v; want zero manifest and exact invalid sentinel", selected, err)
			}
		})
	}

	other := cloneManifest(valid)
	other.Target.Architecture = "amd64"
	invalidUnselected := cloneManifest(other)
	invalidUnselected.SchemaVersion++
	selected, err = selectPayload([]packageManifest{invalidUnselected, valid}, PGDump, supportedPostgreSQLMajor, valid.Target)
	if err != nil || selected.Target != valid.Target {
		t.Fatalf("unselected entry affected lookup: %#v, %v", selected, err)
	}

	invalidMatch := cloneManifest(valid)
	invalidMatch.SchemaVersion++
	for _, inventory := range [][]packageManifest{
		{valid, cloneManifest(valid)},
		{valid, invalidMatch},
		{invalidMatch, valid},
	} {
		selected, err := selectPayload(inventory, PGDump, supportedPostgreSQLMajor, valid.Target)
		if !reflect.DeepEqual(selected, packageManifest{}) || err != ErrInvalidPayload || errors.Unwrap(err) != nil {
			t.Fatalf("duplicate selection = %#v, %v; want zero manifest and exact invalid sentinel", selected, err)
		}
	}
}

func TestPayloadProductionInventoryIsEmptyAndHasNoFallback(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"pg_dump", "pg_restore", "psql"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("secret-canary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory)
	t.Setenv("SPARC_PG_DUMP", filepath.Join(directory, "pg_dump"))
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	for _, target := range []payloadTarget{{OS: "darwin", Architecture: "arm64"}, {OS: "darwin", Architecture: "amd64"}, {OS: "windows", Architecture: "amd64"}} {
		for _, tool := range []Tool{PGDump, PGRestore, PSQL} {
			_, err := lookupProductionPayload(tool, supportedPostgreSQLMajor, target)
			if err != ErrPayloadUnavailable || errors.Unwrap(err) != nil || err.Error() != "tool payload unavailable" {
				t.Fatalf("lookupProductionPayload() error = %v", err)
			}
		}
	}
	if _, err := lookupProductionPayload(Tool(99), supportedPostgreSQLMajor, payloadTarget{OS: "darwin", Architecture: "arm64"}); err != ErrInvalidPayload {
		t.Fatalf("unknown tool error = %v", err)
	}
	if _, err := lookupProductionPayload(PGDump, 99, payloadTarget{OS: "darwin", Architecture: "arm64"}); err != ErrInvalidPayload {
		t.Fatalf("unknown major error = %v", err)
	}
}
