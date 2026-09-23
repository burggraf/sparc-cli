//go:build integration

package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestLocalFixtureReproducesGlobalDefaultPublicSelect(t *testing.T) {
	fixture := newPostgresFixture(t)
	sql := "CREATE ROLE sparc_creator NOLOGIN;\n" +
		"CREATE ROLE sparc_unapproved LOGIN PASSWORD '" + fixturePassword + "';\n" +
		"CREATE TABLE public.sparc_column_canary (value text NOT NULL);\n" +
		"GRANT SELECT (value) ON public.sparc_column_canary TO PUBLIC;\n" +
		"GRANT CREATE ON SCHEMA public TO sparc_creator;\n" +
		"GRANT USAGE ON SCHEMA public TO PUBLIC;\n" +
		"ALTER DEFAULT PRIVILEGES FOR ROLE sparc_creator GRANT SELECT ON TABLES TO PUBLIC;\n" +
		"SET ROLE sparc_creator;\n" +
		"CREATE TABLE public.sparc_global_default_canary (value text NOT NULL);\n" +
		"INSERT INTO public.sparc_global_default_canary VALUES ('synthetic-public-canary');\n" +
		"RESET ROLE;\n"
	cleanHome := filepath.Join(filepath.Dir(fixture.caPath), "psql-home")
	if err := os.Mkdir(cleanHome, 0700); err != nil {
		t.Fatal("unable to create local fixture psql home")
	}
	t.Setenv("HOME", cleanHome)
	t.Setenv("USERPROFILE", cleanHome)
	t.Setenv("APPDATA", cleanHome)
	runFixtureCommandWithInput(t, []byte(sql), fixture.binDir, "psql", fmt.Sprintf("host=%s hostaddr=127.0.0.1 port=%d user=postgres dbname=postgres sslmode=verify-full sslrootcert='%s'", fixture.params.Host, fixture.port, fixture.caPath), "-v", "ON_ERROR_STOP=1")

	observation, err := observeCatalog(context.Background(), fixture.config(t, fixture.caPath), []string{"public"})
	if err != nil {
		t.Fatalf("local preflight observation failed: %v", err)
	}
	if !observation.Security.Observed || !observation.Security.PublicSelectDefaults || !observation.Security.PublicSelectObjects || !observation.Security.PublicSelectColumns {
		t.Fatalf("public default exposure was not fully observed: %+v", observation.Security)
	}
	if err := CheckTargetSecurityV1(observation); !errors.Is(err, ErrUnsafeTargetSecurityProfile) {
		t.Fatalf("preflight error = %v, want unsafe-target refusal", err)
	}

	conn, err := pgx.ConnectConfig(context.Background(), fixture.configForUser(t, fixture.caPath, "sparc_unapproved"))
	if err != nil {
		t.Fatalf("local non-superuser probe could not connect: %v", err)
	}
	defer conn.Close(context.Background())
	var currentUser, owner, value string
	var isSuperuser, isCreatorMember, hasPublicSelect bool
	const query = `
		SELECT current_user::text,
		       role.rolsuper,
		       pg_catalog.pg_has_role(current_user, 'sparc_creator', 'MEMBER'),
		       pg_catalog.pg_get_userbyid(relation.relowner),
		       EXISTS (
		         SELECT 1
		         FROM pg_catalog.aclexplode(relation.relacl) AS acl
		         WHERE acl.grantee = 0 AND acl.privilege_type = 'SELECT'
		       ),
		       canary.value
		FROM public.sparc_global_default_canary AS canary
		JOIN pg_catalog.pg_class AS relation
		  ON relation.oid = 'public.sparc_global_default_canary'::pg_catalog.regclass
		JOIN pg_catalog.pg_roles AS role
		  ON role.rolname = current_user
	`
	if err := conn.QueryRow(context.Background(), query).Scan(&currentUser, &isSuperuser, &isCreatorMember, &owner, &hasPublicSelect, &value); err != nil {
		t.Fatalf("local default-grant exposure probe failed: %v", err)
	}
	if currentUser != "sparc_unapproved" || isSuperuser || isCreatorMember || owner != "sparc_creator" || !hasPublicSelect || value != "synthetic-public-canary" {
		t.Fatalf("counterexample did not prove public exposure: user=%q superuser=%t creator_member=%t owner=%q public_select=%t value=%q", currentUser, isSuperuser, isCreatorMember, owner, hasPublicSelect, value)
	}
}

func TestObserveCatalogRejectsDirectPublicRelationSelect(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local fixture as administrator")
	}
	defer admin.Close(ctx)
	for _, statement := range []string{
		"CREATE ROLE sparc_direct_reader LOGIN PASSWORD '" + fixturePassword + "'",
		"CREATE TABLE public.sparc_direct_grant_canary (value text NOT NULL)",
		"INSERT INTO public.sparc_direct_grant_canary VALUES ('synthetic-direct-public-canary')",
		"GRANT SELECT ON TABLE public.sparc_direct_grant_canary TO PUBLIC",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal("unable to seed direct-grant fixture")
		}
	}

	observation, err := observeCatalog(ctx, fixture.config(t, fixture.caPath), []string{"public"})
	if err != nil {
		t.Fatal("unable to observe direct-grant fixture")
	}
	if !observation.Security.Observed || !observation.Security.PublicSelectObjects || observation.Security.PublicSelectDefaults || observation.Security.PublicSelectColumns {
		t.Fatalf("direct relation grant was not isolated in the observation: %+v", observation.Security)
	}
	if err := CheckTargetSecurityV1(observation); !errors.Is(err, ErrUnsafeTargetSecurityProfile) {
		t.Fatalf("preflight error = %v, want unsafe-target refusal", err)
	}

	reader, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "sparc_direct_reader"))
	if err != nil {
		t.Fatal("unable to connect as the unrelated local reader")
	}
	defer reader.Close(ctx)
	var value string
	if err := reader.QueryRow(ctx, "SELECT value FROM public.sparc_direct_grant_canary").Scan(&value); err != nil || value != "synthetic-direct-public-canary" {
		t.Fatal("direct PUBLIC SELECT grant did not reproduce the synthetic exposure")
	}
}

func TestObserveCatalogReportsSelectedRelationOwner(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local fixture as administrator")
	}
	defer admin.Close(ctx)
	for _, statement := range []string{
		"CREATE ROLE sparc_catalog_owner NOLOGIN",
		"CREATE TABLE public.sparc_owner_canary (value text NOT NULL)",
		"ALTER TABLE public.sparc_owner_canary OWNER TO sparc_catalog_owner",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal("unable to seed relation-owner fixture")
		}
	}

	observation, err := observeCatalog(ctx, fixture.config(t, fixture.caPath), []string{"public"})
	if err != nil {
		t.Fatal("unable to observe relation-owner fixture")
	}
	for _, relation := range observation.Relations {
		if relation.Name == "sparc_owner_canary" {
			if relation.Owner != "sparc_catalog_owner" {
				t.Fatalf("observed relation owner = %q, want sparc_catalog_owner", relation.Owner)
			}
			return
		}
	}
	t.Fatal("owned relation missing from catalog observation")
}

func TestObserveCatalogReportsRelationAndColumnACLs(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local fixture as administrator")
	}
	defer admin.Close(ctx)
	for _, statement := range []string{
		"CREATE ROLE sparc_acl_owner NOLOGIN",
		"CREATE ROLE sparc_acl_reader NOLOGIN",
		"CREATE TABLE public.sparc_acl_null_canary (value text NOT NULL)",
		"CREATE TABLE public.sparc_acl_empty_canary (value text NOT NULL)",
		"GRANT SELECT ON TABLE public.sparc_acl_empty_canary TO PUBLIC",
		"REVOKE ALL PRIVILEGES ON TABLE public.sparc_acl_empty_canary FROM PUBLIC",
		"REVOKE ALL PRIVILEGES ON TABLE public.sparc_acl_empty_canary FROM postgres",
		"CREATE TABLE public.sparc_acl_granted_canary (id bigint, value text NOT NULL)",
		"ALTER TABLE public.sparc_acl_granted_canary OWNER TO sparc_acl_owner",
		"SET ROLE sparc_acl_owner",
		"GRANT SELECT ON TABLE public.sparc_acl_granted_canary TO sparc_acl_reader WITH GRANT OPTION",
		"GRANT SELECT (value) ON TABLE public.sparc_acl_granted_canary TO PUBLIC",
		"RESET ROLE",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal("unable to seed ACL fixture")
		}
	}

	observation, err := observeCatalog(ctx, fixture.config(t, fixture.caPath), []string{"public"})
	if err != nil {
		t.Fatal("unable to observe ACL fixture")
	}
	findACL := func(schema, relation, column string) (ACLObservation, bool) {
		for _, acl := range observation.ACLs {
			if acl.Schema == schema && acl.Relation == relation && acl.Column == column {
				return acl, true
			}
		}
		return ACLObservation{}, false
	}

	nullACL, found := findACL("public", "sparc_acl_null_canary", "")
	if !found || !nullACL.ACLIsNull || len(nullACL.Grants) != 0 {
		t.Fatalf("NULL relation ACL observation = %+v, found=%t", nullACL, found)
	}
	nullColumnACL, found := findACL("public", "sparc_acl_null_canary", "value")
	if !found || !nullColumnACL.ACLIsNull || len(nullColumnACL.Grants) != 0 {
		t.Fatalf("NULL column ACL observation = %+v, found=%t", nullColumnACL, found)
	}
	emptyACL, found := findACL("public", "sparc_acl_empty_canary", "")
	if !found || emptyACL.ACLIsNull || len(emptyACL.Grants) != 0 {
		t.Fatalf("explicit-empty relation ACL observation = %+v, found=%t", emptyACL, found)
	}

	relationACL, found := findACL("public", "sparc_acl_granted_canary", "")
	if !found || relationACL.ACLIsNull {
		t.Fatalf("explicit relation ACL observation = %+v, found=%t", relationACL, found)
	}
	foundNamedGrant := false
	for _, grant := range relationACL.Grants {
		if grant.Grantee == "sparc_acl_reader" && grant.Grantor == "sparc_acl_owner" && grant.Privilege == "SELECT" && grant.Grantable {
			foundNamedGrant = true
		}
	}
	if !foundNamedGrant {
		t.Fatalf("named relation grant identity/options missing: %+v", relationACL.Grants)
	}

	columnACL, found := findACL("public", "sparc_acl_granted_canary", "value")
	if !found || columnACL.ACLIsNull {
		t.Fatalf("explicit column ACL observation = %+v, found=%t", columnACL, found)
	}
	foundPublicGrant := false
	for _, grant := range columnACL.Grants {
		if grant.Grantee == "PUBLIC" && grant.Grantor == "sparc_acl_owner" && grant.Privilege == "SELECT" && !grant.Grantable {
			foundPublicGrant = true
		}
	}
	if !foundPublicGrant {
		t.Fatalf("column-level PUBLIC grant identity missing: %+v", columnACL.Grants)
	}
}

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
	if err := CheckTargetSecurityV1(observation); err != nil {
		t.Fatalf("conservative local profile rejected the clean fixture: %v", err)
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
		{Schema: "literal schema", Name: "base table", Owner: "postgres", Kind: "r", Persistence: "p", TriggerCount: 1, UserTriggerCount: 1},
		{Schema: "literal schema", Name: "base table_pkey", Owner: "postgres", Kind: "i", Persistence: "p"},
		{Schema: "literal schema", Name: "partitioned child", Owner: "postgres", Kind: "r", Persistence: "p", IsPartition: true},
		{Schema: "literal schema", Name: "partitioned table", Owner: "postgres", Kind: "p", Persistence: "p"},
		{Schema: "literal schema", Name: "rls table", Owner: "postgres", Kind: "r", Persistence: "p", RowSecurityEnabled: true, ForceRowSecurity: true, PolicyCount: 1},
		{Schema: "literal schema", Name: "sample index", Owner: "postgres", Kind: "i", Persistence: "p"},
		{Schema: "literal schema", Name: "sample matview", Owner: "postgres", Kind: "m", Persistence: "p"},
		{Schema: "literal schema", Name: "sample sequence", Owner: "postgres", Kind: "S", Persistence: "p"},
		{Schema: "literal schema", Name: "sample view", Owner: "postgres", Kind: "v", Persistence: "p"},
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
