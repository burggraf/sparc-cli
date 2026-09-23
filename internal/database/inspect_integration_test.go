//go:build integration && !windows

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
}
