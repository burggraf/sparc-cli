//go:build integration

package database

import (
	"context"
	"strings"
	"testing"
)

func TestObserveCatalogRejectsWrongTrust(t *testing.T) {
	fixture := newPostgresFixture(t)
	if _, err := observeCatalog(context.Background(), fixture.config(t, fixture.wrongCAPath), nil); err != ErrDatabaseConnect {
		t.Fatalf("wrong fixture CA error = %v, want database connection failure", err)
	}
}

func TestObserveCatalogRejectsUntrustedSystemRoot(t *testing.T) {
	fixture := newPostgresFixture(t)
	if _, err := observeCatalog(context.Background(), fixture.config(t, "system"), nil); err != ErrDatabaseConnect {
		t.Fatalf("system-root fixture error = %v, want database connection failure", err)
	}
}

func TestObserveCatalogRejectsWrongHostname(t *testing.T) {
	fixture := newPostgresFixture(t)
	config := fixture.config(t, fixture.caPath)
	config.TLSConfig.ServerName = "wrong.example.test"
	if _, err := observeCatalog(context.Background(), config, nil); err != ErrDatabaseConnect {
		t.Fatalf("wrong fixture hostname error = %v, want database connection failure", err)
	}
}

func TestObserveCatalogRefusesNonTLSFixture(t *testing.T) {
	fixture := newPostgresFixture(t)
	config := fixture.config(t, fixture.caPath)
	config.TLSConfig = nil
	if _, err := observeCatalog(context.Background(), config, nil); err != ErrDatabaseConnect {
		t.Fatalf("non-TLS fixture error = %v, want database connection failure", err)
	}
}

func TestObserveCatalogOnDisposablePostgres(t *testing.T) {
	fixture := newPostgresFixture(t)
	config := fixture.config(t, fixture.caPath)

	names := []string{
		"public", "literal schema", "literalXschema", `quote"schema`,
		`back\slash`, "regex.[x]$", "雪schema",
		strings.Repeat("a", maxIdentifier), strings.Repeat("雪", maxIdentifier/len("雪")),
		"missing schema",
	}
	observation, err := observeCatalog(context.Background(), config, names)
	if err != nil {
		t.Fatalf("observeCatalog() = %v", err)
	}
	if observation.ServerVersionNum/10000 != supportedPostgresMajor || !observation.TLS || !observation.ReadOnly {
		t.Fatalf("unexpected server observation: %+v", observation)
	}
	if len(observation.Extensions) == 0 {
		t.Fatal("extension inventory is empty")
	}
	foundPLpgSQL := false
	for _, extension := range observation.Extensions {
		if extension.Name == "plpgsql" {
			foundPLpgSQL = true
			if extension.Version == "" || extension.Schema != "pg_catalog" {
				t.Fatalf("unexpected plpgsql observation: %+v", extension)
			}
		}
	}
	if !foundPLpgSQL {
		t.Fatal("default plpgsql extension missing from catalog observation")
	}
	if len(observation.Schemas) != len(names) {
		t.Fatalf("got %d schema observations, want %d", len(observation.Schemas), len(names))
	}
	for i, wantPresent := range []bool{true, true, true, true, true, true, true, true, true, false} {
		if observation.Schemas[i].Name != names[i] || observation.Schemas[i].Present != wantPresent {
			t.Fatalf("schema observation %d = %+v, want name %q present=%t", i, observation.Schemas[i], names[i], wantPresent)
		}
	}

	relationObservation, err := observeCatalog(context.Background(), config, []string{"literal schema"})
	if err != nil {
		t.Fatalf("relation observeCatalog() = %v", err)
	}
	wantRelations := []RelationObservation{
		{Schema: "literal schema", Name: "base table", Kind: "r", Persistence: "p", TriggerCount: 1, UserTriggerCount: 1},
		{Schema: "literal schema", Name: "base table_pkey", Kind: "i", Persistence: "p"},
		{Schema: "literal schema", Name: "partitioned child", Kind: "r", Persistence: "p", IsPartition: true},
		{Schema: "literal schema", Name: "partitioned table", Kind: "p", Persistence: "p"},
		{Schema: "literal schema", Name: "rls table", Kind: "r", Persistence: "p", RowSecurityEnabled: true, ForceRowSecurity: true, PolicyCount: 1},
		{Schema: "literal schema", Name: "sample index", Kind: "i", Persistence: "p"},
		{Schema: "literal schema", Name: "sample matview", Kind: "m", Persistence: "p"},
		{Schema: "literal schema", Name: "sample sequence", Kind: "S", Persistence: "p"},
		{Schema: "literal schema", Name: "sample view", Kind: "v", Persistence: "p"},
	}
	if len(relationObservation.Relations) != len(wantRelations) {
		t.Fatalf("got %d relations, want %d: %+v", len(relationObservation.Relations), len(wantRelations), relationObservation.Relations)
	}
	for i, want := range wantRelations {
		if got := relationObservation.Relations[i]; got != want {
			t.Errorf("relation %d = %+v, want %+v", i, got, want)
		}
	}

	wantRoutines := []RoutineObservation{
		{Schema: "literal schema", Name: "noop trigger", Kind: "f", Language: "plpgsql"},
		{Schema: "literal schema", Name: "secure sample", Kind: "f", Language: "sql", SecurityDefiner: true, HasConfiguration: true},
	}
	if len(relationObservation.Routines) != len(wantRoutines) {
		t.Fatalf("got %d routines, want %d: %+v", len(relationObservation.Routines), len(wantRoutines), relationObservation.Routines)
	}
	for i, want := range wantRoutines {
		got := relationObservation.Routines[i]
		if got.Schema != want.Schema || got.Name != want.Name || got.Kind != want.Kind || got.Language != want.Language || got.SecurityDefiner != want.SecurityDefiner || got.HasConfiguration != want.HasConfiguration {
			t.Errorf("routine %d = %+v, want metadata %+v", i, got, want)
		}
		if got.Name == "secure sample" && got.IdentityArguments == "" {
			t.Error("secure sample identity arguments were not observed")
		}
	}
}
