package archive

import (
	"strings"
	"testing"
)

func TestManifestValidateAcceptsMinimalManifest(t *testing.T) {
	m := Manifest{Format: "sparc-archive", Version: 1, Components: []Component{{ID: "00000000", Key: "database/schema.sql", Scope: "database", Status: "complete", Length: 3, SHA256: strings.Repeat("a", 64)}}}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestManifestValidateRejectsDuplicateComponentIDs(t *testing.T) {
	m := Manifest{Format: "sparc-archive", Version: 1, Components: []Component{{ID: "00000000"}, {ID: "00000000"}}}
	if err := m.Validate(); err == nil {
		t.Fatal("Validate() accepted duplicate component IDs")
	}
}

func TestManifestValidateRejectsDuplicateLogicalKeys(t *testing.T) {
	m := Manifest{Format: Format, Version: Version, Components: []Component{
		{ID: "00000000", Key: "database/schema.sql", Scope: "database", Status: "complete", SHA256: strings.Repeat("a", 64)},
		{ID: "00000001", Key: "database/schema.sql", Scope: "database", Status: "complete", SHA256: strings.Repeat("a", 64)},
	}}
	if err := m.Validate(); err == nil {
		t.Fatal("Validate() accepted duplicate logical keys")
	}
}

func TestManifestValidateRejectsUnsupportedVersionAndInvalidDigest(t *testing.T) {
	m := Manifest{Format: "sparc-archive", Version: 2, Components: []Component{{ID: "00000000", Key: "database/schema.sql", Scope: "database", Status: "complete", SHA256: "not-a-digest"}}}
	if err := m.Validate(); err == nil {
		t.Fatal("Validate() accepted unsupported version and invalid digest")
	}
}

func TestParseManifestRejectsDuplicateAndUnknownFields(t *testing.T) {
	for _, encoded := range []string{
		`{"format":"sparc-archive","format":"sparc-archive","version":1,"components":[{"id":"00000000","key":"database/schema.sql","scope":"database","status":"complete","length":0,"sha256":"` + strings.Repeat("a", 64) + `"}]}`,
		`{"format":"sparc-archive","version":1,"extra":true,"components":[{"id":"00000000","key":"database/schema.sql","scope":"database","status":"complete","length":0,"sha256":"` + strings.Repeat("a", 64) + `"}]}`,
	} {
		if _, err := ParseManifest(strings.NewReader(encoded)); err == nil {
			t.Fatal("ParseManifest() accepted invalid object fields")
		}
	}
}

func TestParseManifestPreservesUnicodeLogicalKey(t *testing.T) {
	encoded := `{"format":"sparc-archive","version":1,"components":[{"id":"00000000","key":"storage/Café/資料.txt","scope":"storage","status":"complete","length":0,"sha256":"` + strings.Repeat("a", 64) + `"}]}`
	m, err := ParseManifest(strings.NewReader(encoded))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if got := m.Components[0].Key; got != "storage/Café/資料.txt" {
		t.Fatalf("key = %q", got)
	}
}

func TestParseManifestRejectsOversizeInput(t *testing.T) {
	if _, err := ParseManifest(strings.NewReader(strings.Repeat(" ", maxManifestBytes+1))); err == nil {
		t.Fatal("ParseManifest() accepted oversize input")
	}
}
