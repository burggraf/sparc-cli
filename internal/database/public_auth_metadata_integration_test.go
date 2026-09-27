//go:build integration

package database

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestObservePublicAuthMetadataRefusesOversizedColumnInventory(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local fixture administrator")
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA auth"); err != nil {
		t.Fatal("unable to create local auth schema")
	}
	for table := 0; table < 7; table++ {
		var ddl strings.Builder
		fmt.Fprintf(&ddl, "CREATE TABLE public.sparc_wide_%d (", table)
		for column := 0; column < 1450; column++ {
			if column > 0 {
				ddl.WriteByte(',')
			}
			fmt.Fprintf(&ddl, "c%d integer", column)
		}
		ddl.WriteByte(')')
		if _, err := admin.Exec(ctx, ddl.String()); err != nil {
			t.Fatalf("unable to create synthetic wide table %d", table)
		}
	}

	metadata, err := observePublicAuthMetadata(ctx, fixture.config(t, fixture.caPath))
	if err != ErrCatalogObservation {
		t.Fatalf("oversized column observation error = %v, want catalog refusal", err)
	}
	if len(metadata.Catalog.Schemas) != 0 || len(metadata.Columns) != 0 || len(metadata.Constraints) != 0 {
		t.Fatal("oversized observation returned a truncated or partial inventory")
	}
}

func TestObservePublicAuthMetadataIsBoundedCatalogOnly(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()

	missing, err := observePublicAuthMetadata(ctx, fixture.config(t, fixture.caPath))
	if err != ErrCatalogObservation {
		t.Fatalf("observation without auth schema error = %v, want catalog refusal", err)
	}
	if len(missing.Catalog.Schemas) != 0 || len(missing.Columns) != 0 || len(missing.Constraints) != 0 {
		t.Fatal("missing-schema refusal returned partial metadata")
	}

	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local fixture administrator")
	}
	defer admin.Close(ctx)
	for _, statement := range []string{
		"CREATE SCHEMA auth",
		"CREATE TABLE auth.users (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, email text NOT NULL, encrypted_password text)",
		"CREATE TABLE public.profiles (user_id bigint PRIMARY KEY REFERENCES auth.users(id), display_name text NOT NULL)",
		"CREATE TABLE public.audit (id bigint, note text CHECK (note <> 'constraint-expression-canary'))",
		"ALTER TABLE public.profiles ENABLE ROW LEVEL SECURITY",
		"CREATE POLICY profile_read ON public.profiles FOR SELECT USING (display_name <> 'policy-expression-canary')",
		"INSERT INTO auth.users (email, encrypted_password) VALUES ('row-email-canary@example.test', 'password-hash-canary')",
		"INSERT INTO public.profiles VALUES (1, 'row-value-canary')",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatalf("unable to seed local synthetic catalog fixture (%s)", statement[:strings.Index(statement, " ")])
		}
	}

	metadata, err := observePublicAuthMetadata(ctx, fixture.config(t, fixture.caPath))
	if err != nil {
		t.Fatalf("local public/auth metadata observation failed: %v", err)
	}
	if metadata.Catalog.ServerMajor != supportedPostgresMajor || !metadata.Catalog.TLS || !metadata.Catalog.ReadOnly {
		t.Fatalf("unexpected connection assurance result: major=%d tls=%t read-only=%t", metadata.Catalog.ServerMajor, metadata.Catalog.TLS, metadata.Catalog.ReadOnly)
	}
	if len(metadata.Catalog.Schemas) != 2 || metadata.Catalog.Schemas[0].Name != "public" || !metadata.Catalog.Schemas[0].Present || metadata.Catalog.Schemas[1].Name != "auth" || !metadata.Catalog.Schemas[1].Present {
		t.Fatalf("unexpected selected schema observations: %+v", metadata.Catalog.Schemas)
	}

	columns := make(map[string]PublicAuthColumnObservation)
	for _, column := range metadata.Columns {
		if column.Schema != "public" && column.Schema != "auth" {
			t.Fatalf("column observation escaped selected schemas: %q", column.Schema)
		}
		columns[column.Schema+"."+column.Relation+"."+column.Name] = column
	}
	if column := columns["auth.users.encrypted_password"]; column.DataType != "text" || column.NotNull {
		t.Fatalf("auth.users.encrypted_password metadata = %+v", column)
	}
	if column := columns["public.profiles.display_name"]; column.DataType != "text" || !column.NotNull {
		t.Fatalf("public.profiles.display_name metadata = %+v", column)
	}
	if column := columns["auth.users.id"]; column.DataType != "bigint" || column.Identity != "a" || column.Ordinal != 1 {
		t.Fatalf("auth.users.id metadata = %+v", column)
	}

	foundForeignKey := false
	foundCheck := false
	for _, constraint := range metadata.Constraints {
		if constraint.Schema == "public" && constraint.Relation == "profiles" && constraint.Kind == "f" {
			foundForeignKey = constraint.Column == "user_id" && constraint.ReferencedSchema == "auth" && constraint.ReferencedTable == "users" && constraint.ReferencedColumn == "id"
		}
		if constraint.Schema == "public" && constraint.Relation == "audit" && constraint.Kind == "c" {
			foundCheck = constraint.Column == "note" && constraint.ReferencedSchema == "" && constraint.ReferencedTable == ""
		}
	}
	if !foundForeignKey || !foundCheck {
		t.Fatalf("constraint metadata omitted expected FK/check shape: foreign-key=%t check=%t", foundForeignKey, foundCheck)
	}

	serialized := fmt.Sprintf("%+v", metadata)
	for _, canary := range []string{
		"row-email-canary@example.test",
		"password-hash-canary",
		"row-value-canary",
		"constraint-expression-canary",
		"policy-expression-canary",
	} {
		if strings.Contains(serialized, canary) {
			t.Fatalf("catalog metadata exposed non-catalog canary %q", canary)
		}
	}
}
