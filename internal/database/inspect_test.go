package database

import (
	"context"
	"testing"
)

func TestObserveCatalogRejectsInvalidInputsBeforeConnecting(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*ConnectionParams) []string
		want error
	}{
		{"invalid schema", func(p *ConnectionParams) []string { return []string{"bad\nname"} }, ErrInvalidSchemaSelection},
		{"unsupported transaction route", func(p *ConnectionParams) []string {
			p.Host = "aws-0-us-east-1.pooler.supabase.com"
			p.Port = transactionPort
			p.User = "postgres." + testProjectRef
			return nil
		}, ErrUnsupportedRoute},
		{"session route ref mismatch", func(p *ConnectionParams) []string {
			p.Host = "aws-0-us-east-1.pooler.supabase.com"
			p.User = "postgres.otherprojectref1234"
			return nil
		}, ErrConnectionParameters},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearDatabaseEnvironment(t)
			params := validConnectionParams(t)
			params.SSLRootCert = "system"
			names := test.edit(&params)
			_, err := ObserveCatalog(context.Background(), params, []byte("explicit-password"), names)
			if err != test.want {
				t.Fatalf("ObserveCatalog() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestObserveCatalogRefusesAmbientPostgresEnvironmentBeforeConnecting(t *testing.T) {
	clearDatabaseEnvironment(t)
	t.Setenv("PGOPTIONS", "-c search_path=pg_catalog")
	params := validConnectionParams(t)
	params.SSLRootCert = "system"
	if _, err := ObserveCatalog(context.Background(), params, []byte("explicit-password"), nil); err != ErrAmbientConfiguration {
		t.Fatalf("ObserveCatalog() error = %v, want ambient-configuration refusal", err)
	}
}

func TestObserveCatalogRejectsNilContext(t *testing.T) {
	if _, err := ObserveCatalog(nil, ConnectionParams{}, nil, nil); err != ErrCatalogObservation {
		t.Fatalf("ObserveCatalog(nil) error = %v, want fixed inspection error", err)
	}
}
