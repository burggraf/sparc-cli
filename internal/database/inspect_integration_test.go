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

func TestObserveCatalogViaSessionPoolerShapeUsesTLSAndReadOnlyTransaction(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local TLS fixture")
	}
	defer admin.Close(ctx)
	poolerUser := "postgres." + testProjectRef
	if _, err := admin.Exec(ctx, `CREATE ROLE "`+poolerUser+`" LOGIN PASSWORD '`+fixturePassword+`'`); err != nil {
		t.Fatal("unable to create synthetic pooler user")
	}
	params := fixture.params
	params.Host = "aws-1-us-east-2.pooler.supabase.com"
	params.User = poolerUser
	config := fixture.configForParams(t, fixture.caPath, params)
	observation, err := observeCatalog(ctx, config, []string{"public"})
	if err != nil {
		t.Fatalf("local PostgreSQL session-route-shaped probe failed: %v", err)
	}
	if !observation.TLS || !observation.ReadOnly || observation.ServerMajor != supportedPostgresMajor {
		t.Fatalf("session-route-shaped observation = %+v", observation)
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
		"CREATE ROLE \"PUBLIC\" NOLOGIN",
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
		"GRANT UPDATE ON TABLE public.sparc_acl_granted_canary TO \"PUBLIC\"",
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
	foundNamedPublic := false
	for _, grant := range relationACL.Grants {
		if grant.Grantee == "PUBLIC" && grant.Privilege == "UPDATE" && !grant.GranteeIsPublic {
			foundNamedPublic = true
		}
	}
	if !foundNamedPublic {
		t.Fatal("quoted named role PUBLIC was confused with the PUBLIC pseudo-role")
	}

	columnACL, found := findACL("public", "sparc_acl_granted_canary", "value")
	if !found || columnACL.ACLIsNull {
		t.Fatalf("explicit column ACL observation = %+v, found=%t", columnACL, found)
	}
	foundPublicGrant := false
	for _, grant := range columnACL.Grants {
		if grant.Grantee == "PUBLIC" && grant.GranteeIsPublic && grant.Grantor == "sparc_acl_owner" && grant.Privilege == "SELECT" && !grant.Grantable {
			foundPublicGrant = true
		}
	}
	if !foundPublicGrant {
		t.Fatalf("column-level PUBLIC grant identity missing: %+v", columnACL.Grants)
	}
}

func TestObserveCatalogReportsDefaultACLs(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local fixture as administrator")
	}
	defer admin.Close(ctx)
	for _, statement := range []string{
		"CREATE ROLE sparc_default_creator NOLOGIN",
		"CREATE ROLE sparc_default_reader NOLOGIN",
		"CREATE SCHEMA sparc_default_schema",
		"CREATE SCHEMA sparc_unselected_default_schema",
		"GRANT USAGE, CREATE ON SCHEMA sparc_default_schema TO sparc_default_creator",
		"GRANT USAGE, CREATE ON SCHEMA sparc_unselected_default_schema TO sparc_default_creator",
		"SET ROLE sparc_default_creator",
		"ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO PUBLIC",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA sparc_default_schema GRANT INSERT ON TABLES TO sparc_default_reader WITH GRANT OPTION",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA sparc_unselected_default_schema GRANT UPDATE ON TABLES TO PUBLIC",
		"RESET ROLE",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal("unable to seed default-ACL fixture")
		}
	}

	observation, err := observeCatalog(ctx, fixture.config(t, fixture.caPath), []string{"public", "sparc_default_schema"})
	if err != nil {
		t.Fatal("unable to observe default-ACL fixture")
	}
	findDefault := func(creator, schema, objectType string) (DefaultACLObservation, bool) {
		for _, acl := range observation.DefaultACLs {
			if acl.Creator == creator && acl.Schema == schema && acl.ObjectType == objectType {
				return acl, true
			}
		}
		return DefaultACLObservation{}, false
	}
	hasGrant := func(acl DefaultACLObservation, expected ACLGrantObservation) bool {
		for _, grant := range acl.Grants {
			if grant == expected {
				return true
			}
		}
		return false
	}
	global, found := findDefault("sparc_default_creator", "", "r")
	if !found || global.ACLIsNull || !hasGrant(global, ACLGrantObservation{
		Grantee: "PUBLIC", GranteeIsPublic: true, Grantor: "sparc_default_creator", Privilege: "SELECT",
	}) || !hasGrant(global, ACLGrantObservation{
		Grantee: "sparc_default_creator", Grantor: "sparc_default_creator", Privilege: "SELECT",
	}) {
		t.Fatalf("global table default ACL = %+v, found=%t", global, found)
	}
	schema, found := findDefault("sparc_default_creator", "sparc_default_schema", "r")
	if !found || schema.ACLIsNull || !hasGrant(schema, ACLGrantObservation{
		Grantee: "sparc_default_reader", Grantor: "sparc_default_creator", Privilege: "INSERT", Grantable: true,
	}) {
		t.Fatalf("schema table default ACL = %+v, found=%t", schema, found)
	}
	if _, found := findDefault("sparc_default_creator", "sparc_unselected_default_schema", "r"); found {
		t.Fatal("default ACL for an unselected schema was observed")
	}
}

func TestObserveCatalogReportsRoleAttributesAndMembershipOptions(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local fixture as administrator")
	}
	defer admin.Close(ctx)
	for _, statement := range []string{
		"CREATE ROLE sparc_attribute_role LOGIN CREATEDB CREATEROLE NOINHERIT BYPASSRLS REPLICATION",
		"CREATE ROLE sparc_membership_parent NOLOGIN",
		"CREATE ROLE sparc_membership_child NOLOGIN",
		"GRANT sparc_membership_parent TO sparc_membership_child WITH ADMIN TRUE, INHERIT FALSE, SET TRUE",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal("unable to seed role-membership fixture")
		}
	}

	observation, err := observeCatalog(ctx, fixture.config(t, fixture.caPath), []string{"public"})
	if err != nil {
		t.Fatal("unable to observe role-membership fixture")
	}
	foundRole := false
	for _, role := range observation.Roles {
		if role.Name == "sparc_attribute_role" {
			foundRole = role.CanLogin && !role.Superuser && !role.Inherit && role.CreateRole && role.CreateDatabase && role.Replication && role.BypassRLS
		}
	}
	if !foundRole {
		t.Fatal("role attributes were not observed by name")
	}
	foundMembership := false
	for _, membership := range observation.Memberships {
		if membership.Role == "sparc_membership_parent" && membership.Member == "sparc_membership_child" {
			foundMembership = membership.Grantor == "postgres" && membership.AdminOption && !membership.InheritOption && membership.SetOption
		}
	}
	if !foundMembership {
		t.Fatal("role membership identity/options were not observed by name")
	}
}

func TestObserveCatalogReportsPolicyRolesAndRLSBehavior(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local policy fixture")
	}
	defer admin.Close(ctx)
	for _, statement := range []string{
		"CREATE ROLE sparc_policy_owner LOGIN PASSWORD '" + fixturePassword + "'",
		"CREATE ROLE sparc_policy_reader LOGIN PASSWORD '" + fixturePassword + "'",
		"CREATE ROLE sparc_policy_bypass LOGIN BYPASSRLS PASSWORD '" + fixturePassword + "'",
		"CREATE ROLE \"PUBLIC\" NOLOGIN",
		"CREATE TABLE public.sparc_policy_canary (value text NOT NULL)",
		"INSERT INTO public.sparc_policy_canary VALUES ('visible'), ('hidden')",
		"ALTER TABLE public.sparc_policy_canary OWNER TO sparc_policy_owner",
		"GRANT USAGE ON SCHEMA public TO sparc_policy_reader, sparc_policy_bypass",
		"GRANT SELECT ON public.sparc_policy_canary TO sparc_policy_reader, sparc_policy_bypass",
		"ALTER TABLE public.sparc_policy_canary ENABLE ROW LEVEL SECURITY",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal("unable to seed synthetic RLS fixture")
		}
	}
	countAs := func(role string) int {
		t.Helper()
		conn, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, role))
		if err != nil {
			t.Fatal("unable to connect to local policy fixture role")
		}
		defer conn.Close(ctx)
		var count int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM public.sparc_policy_canary").Scan(&count); err != nil {
			t.Fatal("unable to read synthetic RLS canary")
		}
		return count
	}
	if owner, reader, bypass := countAs("sparc_policy_owner"), countAs("sparc_policy_reader"), countAs("sparc_policy_bypass"); owner != 2 || reader != 0 || bypass != 2 {
		t.Fatalf("default-deny/owner/BYPASSRLS counts = %d/%d/%d, want 2/0/2", owner, reader, bypass)
	}
	for _, statement := range []string{
		"CREATE POLICY sparc_reader_policy ON public.sparc_policy_canary FOR SELECT TO sparc_policy_reader USING (value = 'visible')",
		"CREATE POLICY sparc_public_policy ON public.sparc_policy_canary FOR SELECT TO PUBLIC USING (false)",
		"CREATE POLICY sparc_named_public_policy ON public.sparc_policy_canary FOR SELECT TO \"PUBLIC\" USING (false)",
		"CREATE POLICY sparc_restrictive_policy ON public.sparc_policy_canary AS RESTRICTIVE FOR ALL TO sparc_policy_reader USING (value = 'visible') WITH CHECK (value = 'visible')",
		"ALTER TABLE public.sparc_policy_canary FORCE ROW LEVEL SECURITY",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal("unable to seed selected RLS policies")
		}
	}
	if owner, reader, bypass := countAs("sparc_policy_owner"), countAs("sparc_policy_reader"), countAs("sparc_policy_bypass"); owner != 0 || reader != 1 || bypass != 2 {
		t.Fatalf("forced-RLS/policy/BYPASSRLS counts = %d/%d/%d, want 0/1/2", owner, reader, bypass)
	}
	observation, err := observeCatalog(ctx, fixture.config(t, fixture.caPath), []string{"public"})
	if err != nil {
		t.Fatal("unable to observe policy facts")
	}
	found := map[string]bool{}
	for _, policy := range observation.Policies {
		if policy.Schema != "public" || policy.Relation != "sparc_policy_canary" {
			continue
		}
		switch policy.Name {
		case "sparc_reader_policy":
			found[policy.Name] = policy.Command == "r" && policy.Permissive && policy.HasUsing && !policy.HasWithCheck && len(policy.Roles) == 1 && policy.Roles[0] == (PolicyRoleObservation{Name: "sparc_policy_reader"})
		case "sparc_public_policy":
			found[policy.Name] = policy.Command == "r" && policy.Permissive && len(policy.Roles) == 1 && policy.Roles[0] == (PolicyRoleObservation{Name: "PUBLIC", IsPublic: true})
		case "sparc_restrictive_policy":
			found[policy.Name] = policy.Command == "*" && !policy.Permissive && policy.HasUsing && policy.HasWithCheck && len(policy.Roles) == 1 && policy.Roles[0] == (PolicyRoleObservation{Name: "sparc_policy_reader"})
		case "sparc_named_public_policy":
			found[policy.Name] = len(policy.Roles) == 1 && policy.Roles[0] == (PolicyRoleObservation{Name: "PUBLIC"})
		}
	}
	for _, name := range []string{"sparc_reader_policy", "sparc_public_policy", "sparc_restrictive_policy", "sparc_named_public_policy"} {
		if !found[name] {
			t.Fatalf("policy %s role/effect facts missing", name)
		}
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
