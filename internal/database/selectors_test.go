package database

import (
	"strconv"
	"strings"
	"testing"
)

func TestValidateSchemaNamesAcceptsLiteralNames(t *testing.T) {
	names := []string{
		"public", "space name", `quoted"name`, `back\slash`, "regex.[x]$", "雪schema",
		strings.Repeat("n", maxIdentifier), "Foo", "foo",
	}
	if err := validateSchemaNames(names); err != nil {
		t.Fatalf("literal schema names rejected: %v", err)
	}
	if err := validateSchemaNames(nil); err != nil {
		t.Fatalf("empty selection rejected: %v", err)
	}
}

func TestValidateSchemaNamesRejectsInvalidOrAmbiguousInputs(t *testing.T) {
	for _, test := range []struct {
		name string
		list []string
	}{
		{"empty name", []string{""}},
		{"too many bytes", []string{strings.Repeat("n", maxIdentifier+1)}},
		{"invalid UTF-8", []string{string([]byte{0xff})}},
		{"control", []string{"bad\nname"}},
		{"NUL", []string{"bad\x00name"}},
		{"duplicate", []string{"same", "same"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateSchemaNames(test.list); err != ErrInvalidSchemaSelection {
				t.Fatalf("validateSchemaNames() = %v, want fixed selector error", err)
			}
		})
	}

	tooMany := make([]string, maxSchemaSelections+1)
	for i := range tooMany {
		tooMany[i] = "schema_" + strconv.Itoa(i)
	}
	if err := validateSchemaNames(tooMany); err != ErrInvalidSchemaSelection {
		t.Fatalf("too many schema names returned %v", err)
	}
}

func TestPostgresVersionGate(t *testing.T) {
	for _, test := range []struct {
		versionNum int
		major      int
		wantErr    error
	}{
		{170009, 17, nil},
		{160011, 16, ErrUnsupportedServerVersion},
		{180001, 18, ErrUnsupportedServerVersion},
		{90624, 9, ErrUnsupportedServerVersion},
		{0, 0, ErrUnsupportedServerVersion},
		{-1, 0, ErrUnsupportedServerVersion},
	} {
		if got := serverMajor(test.versionNum); got != test.major {
			t.Errorf("serverMajor(%d) = %d, want %d", test.versionNum, got, test.major)
		}
		if err := validatePostgresVersion(test.versionNum); err != test.wantErr {
			t.Errorf("validatePostgresVersion(%d) = %v, want %v", test.versionNum, err, test.wantErr)
		}
	}
}
