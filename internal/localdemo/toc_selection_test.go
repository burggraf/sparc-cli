//go:build localdemo

package localdemo

import (
	"reflect"
	"testing"
)

func TestSelectArchiveTOCSeparatesApplicationDDLFromEligibleData(t *testing.T) {
	input := `; header
1; 2615 100 SCHEMA - sparc_app postgres
2; 1259 101 TABLE public items postgres
3; 1259 102 TABLE sparc_app "odd table" postgres
4; 2606 103 FK CONSTRAINT sparc_app "odd table"_fk postgres
5; 0 101 TABLE DATA public items postgres
6; 0 102 TABLE DATA sparc_app "odd table" postgres
7; 0 104 SEQUENCE SET sparc_app items_id_seq postgres
8; 2615 105 SCHEMA - auth supabase_admin
9; 1259 106 TABLE auth users supabase_admin
10; 0 106 TABLE DATA auth users supabase_admin
11; 0 107 TABLE DATA auth schema_migrations supabase_admin
12; 0 108 TABLE DATA storage objects supabase_admin
13; 0 109 TABLE DATA storage migrations supabase_admin
14; 0 110 TABLE DATA storage buckets_vectors supabase_admin
15; 0 111 TABLE DATA storage vector_indexes supabase_admin
16; 3079 112 EXTENSION - pgcrypto
`
	plan, err := selectArchiveTOC([]byte(input))
	if err != nil {
		t.Fatalf("selectArchiveTOC() error = %v", err)
	}
	if got, want := plan.schemaIDs, []uint32{1, 2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("schema IDs = %v, want %v", got, want)
	}
	if got, want := plan.dataIDs, []uint32{5, 6, 7, 10, 12}; !reflect.DeepEqual(got, want) {
		t.Errorf("data IDs = %v, want %v", got, want)
	}
	if plan.summary.Total != 16 || plan.summary.ManagedDDL != 2 || plan.summary.ExcludedData != 4 || plan.summary.Unsupported != 1 || plan.summary.Unknown != 0 {
		t.Errorf("selection summary = %#v", plan.summary)
	}
	if !plan.Blocked() {
		t.Fatal("managed DDL/extension entry did not block selection")
	}
}

func TestSelectArchiveTOCPreservesDependentDataOrderAndRefusesUnknowns(t *testing.T) {
	input := `1; 1259 1 TABLE public parent postgres
2; 1259 2 TABLE public child postgres
3; 0 1 TABLE DATA public parent postgres
4; 0 2 TABLE DATA public child postgres
5; 2606 2 FK CONSTRAINT public child child_parent_fkey postgres
6; 0 3 TABLE DATA cron job_scheduler postgres
7; 0 4 TABLE DATA storage vector_indexes supabase admin
8; 9999 5 FUTURE OBJECT public opaque postgres
`
	plan, err := selectArchiveTOC([]byte(input))
	if err != nil {
		t.Fatalf("selectArchiveTOC() error = %v", err)
	}
	if got, want := plan.schemaIDs, []uint32{1, 2, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("schema IDs = %v, want parent/child tables and FK %v", got, want)
	}
	if got, want := plan.dataIDs, []uint32{3, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("data IDs = %v, want original FK-dependent order %v", got, want)
	}
	if plan.summary.Unsupported != 1 || plan.summary.Unknown != 1 || plan.summary.ExcludedData != 1 || !plan.Blocked() {
		t.Errorf("unsupported/unknown entries did not block selection: %#v", plan.summary)
	}
}

func TestSelectArchiveTOCRejectsMalformedOrAmbiguousEntries(t *testing.T) {
	for _, input := range []string{
		"1; 0 1 TABLE DATA public \"unterminated postgres\n",
		"1; 0 1 TABLE DATA public item postgres\n1; 0 2 TABLE DATA public other postgres\n",
		"1; not-an-oid 1 TABLE DATA public item postgres\n",
		"1; 4294967296 1 TABLE DATA public item postgres\n",
		"1; 0 1 TABLE DATA public item\n",
		"not a TOC entry\n",
	} {
		if _, err := selectArchiveTOC([]byte(input)); err == nil {
			t.Errorf("selectArchiveTOC() accepted malformed TOC %q", input)
		}
	}
}

func TestSelectArchiveTOCHandlesQuotesInsideDisplayTokens(t *testing.T) {
	plan, err := selectArchiveTOC([]byte("1; 0 1 ACL public FUNCTION f(\"char\") postgres\n"))
	if err != nil {
		t.Fatalf("selectArchiveTOC() error = %v", err)
	}
	if plan.Blocked() || !reflect.DeepEqual(plan.schemaIDs, []uint32{1}) {
		t.Fatalf("quoted signature token was not classified: %#v", plan)
	}
}

func TestSelectArchiveTOCRefusesWhitespaceSchemaNames(t *testing.T) {
	input := `1; 2615 1 SCHEMA - "quoted schema" postgres
2; 1259 2 TABLE "quoted" thing postgres
`
	plan, err := selectArchiveTOC([]byte(input))
	if err != nil {
		t.Fatalf("selectArchiveTOC() error = %v", err)
	}
	if !plan.Blocked() || plan.summary.Unsupported != 1 || plan.summary.Unknown != 1 || len(plan.schemaIDs) != 0 {
		t.Fatalf("ambiguous schema name was not refused: %#v", plan)
	}
}

func TestSelectArchiveTOCRejectsOversizedInput(t *testing.T) {
	input := make([]byte, maxTOCBytes+1)
	if _, err := selectArchiveTOC(input); err == nil {
		t.Fatal("selectArchiveTOC() accepted oversized TOC")
	}
}
