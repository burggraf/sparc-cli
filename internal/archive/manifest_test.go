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

func TestManifestValidateRejectsCaseFoldedKeyCollisions(t *testing.T) {
	m := Manifest{Format: Format, Version: Version, Components: []Component{
		{ID: "00000000", Key: "database/Foo.sql", Scope: "database", Status: "complete", SHA256: strings.Repeat("a", 64)},
		{ID: "00000001", Key: "database/foo.sql", Scope: "database", Status: "complete", SHA256: strings.Repeat("b", 64)},
	}}
	if err := m.Validate(); err == nil {
		t.Fatal("Validate() accepted a case-folded key collision")
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

func TestParseManifestRejectsMalformedUnicode(t *testing.T) {
	base := `{"format":"sparc-archive","version":1,"components":[{"id":"00000000","key":"storage/`
	suffix := `","scope":"storage","status":"complete","length":0,"sha256":"` + strings.Repeat("a", 64) + `"}]}`
	invalidUTF8 := append([]byte(base), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(suffix)...)
	unpairedSurrogate := base + `\ud800` + suffix
	for _, data := range [][]byte{invalidUTF8, []byte(unpairedSurrogate)} {
		if _, err := ParseManifest(strings.NewReader(string(data))); err == nil {
			t.Fatal("ParseManifest() accepted malformed Unicode")
		}
	}
}

func TestParseManifestPreservesUnicodeLogicalKey(t *testing.T) {
	encoded := `{"format":"sparc-archive","version":1,"components":[{"id":"00000000","key":"storage/Café/資料/\ud83d\ude80.txt","scope":"storage","status":"complete","length":0,"sha256":"` + strings.Repeat("a", 64) + `"}]}`
	m, err := ParseManifest(strings.NewReader(encoded))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if got := m.Components[0].Key; got != "storage/Café/資料/🚀.txt" {
		t.Fatalf("key = %q", got)
	}
}

func TestManifestValidateRejectsUnsafePortableKeys(t *testing.T) {
	for _, key := range []string{"../escape", "/absolute", `C:/drive`, `a\\b`, "database/CON", "database/trailing.", "a//b"} {
		m := Manifest{Format: Format, Version: Version, Components: []Component{{ID: "00000000", Key: key, Scope: "database", Status: "complete", SHA256: strings.Repeat("a", 64)}}}
		if err := m.Validate(); err == nil {
			t.Errorf("Validate() accepted unsafe key %q", key)
		}
	}
}

func TestParseManifestRejectsExcessiveDepth(t *testing.T) {
	deep := strings.Repeat("[", maxManifestDepth+1) + strings.Repeat("]", maxManifestDepth+1)
	encoded := `{"format":"sparc-archive","version":1,"components":[],"nested":` + deep + `}`
	if _, err := ParseManifest(strings.NewReader(encoded)); err == nil {
		t.Fatal("ParseManifest() accepted excessive nesting")
	}
}

func TestManifestValidateRejectsUnicodeLineSeparatorsInKeys(t *testing.T) {
	m := Manifest{Format: Format, Version: Version, Components: []Component{{ID: "00000000", Key: "database/line\u2028break", Scope: "database", Status: "complete", SHA256: strings.Repeat("a", 64)}}}
	if err := m.Validate(); err == nil {
		t.Fatal("Validate() accepted a Unicode line separator")
	}
}

func TestParseManifestRejectsOversizeInput(t *testing.T) {
	if _, err := ParseManifest(strings.NewReader(strings.Repeat(" ", maxManifestBytes+1))); err == nil {
		t.Fatal("ParseManifest() accepted oversize input")
	}
}
