package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ponytail: cap ACL/membership/policy facts at 65,536 rows and roles at 4,096;
// raise only after measuring worst-case memory. Other caps: 256 extensions,
// 1,024 routines, 10,000 relations, and 8 KiB per routine signature.
const (
	supportedPostgresMajor      = 17
	maxObservedExtensions       = 256
	maxExtensionVersion         = 256
	maxObservedRoutines         = 1024
	maxRoutineIdentityArguments = 8192
	maxObservedRelations        = 10000
	maxObservedACLRows          = 65536
	maxObservedRoles            = 4096
	maxObservedMemberships      = 65536
	maxObservedPolicyRows       = 65536
	inspectionTimeout           = 15 * time.Second
)

var (
	ErrDatabaseConnect          = errors.New("database connection failed")
	ErrCatalogObservation       = errors.New("database catalog observation failed")
	ErrDatabaseTLSRequired      = errors.New("database TLS is not active")
	ErrReadOnlyTransaction      = errors.New("read-only database transaction is not active")
	ErrUnsupportedServerVersion = errors.New("database server major version is not qualified")
)

type SchemaObservation struct {
	Name    string
	Present bool
}

type ExtensionObservation struct {
	Name    string
	Version string
	Schema  string
}

type RoutineObservation struct {
	Schema            string
	Name              string
	IdentityArguments string
	Kind              string
	Language          string
	SecurityDefiner   bool
	HasConfiguration  bool
}

type RelationObservation struct {
	Schema             string
	Name               string
	Owner              string
	Kind               string
	Persistence        string
	IsPartition        bool
	RowSecurityEnabled bool
	ForceRowSecurity   bool
	PolicyCount        int64
	TriggerCount       int64
	UserTriggerCount   int64
}

// ACLObservation records a relation ACL when Column is empty, otherwise a
// column ACL. ACLIsNull distinguishes the catalog's NULL default from an
// explicit ACL array, including an explicit array with no grants.
type ACLObservation struct {
	Schema    string
	Relation  string
	Column    string
	ACLIsNull bool
	Grants    []ACLGrantObservation
}

// ACLGrantObservation keeps the PUBLIC pseudo-grantee distinct from a quoted
// role named "PUBLIC"; Grantee alone is not a safe identity.
type ACLGrantObservation struct {
	Grantee         string
	GranteeIsPublic bool
	Grantor         string
	Privilege       string
	Grantable       bool
}

// DefaultACLObservation records global defaults with an empty Schema and
// selected-schema defaults by name. ObjectType is PostgreSQL's catalog code.
// ACLIsNull preserves NULL versus explicit-empty ACL values.
type DefaultACLObservation struct {
	Creator    string
	Schema     string
	ObjectType string
	ACLIsNull  bool
	Grants     []ACLGrantObservation
}

type RoleObservation struct {
	Name           string
	Superuser      bool
	Inherit        bool
	CreateRole     bool
	CreateDatabase bool
	CanLogin       bool
	Replication    bool
	BypassRLS      bool
}

// MembershipObservation records Role's grant to Member, issued by Grantor.
type MembershipObservation struct {
	Role          string
	Member        string
	Grantor       string
	AdminOption   bool
	InheritOption bool
	SetOption     bool
}

// PolicyRoleObservation distinguishes the PUBLIC pseudo-role from a named
// role (including a quoted role literally named "PUBLIC").
type PolicyRoleObservation struct {
	Name     string
	IsPublic bool
}

// PolicyObservation deliberately excludes pg_node_tree expressions: presence
// flags are not evidence that two policies enforce equivalent conditions.
type PolicyObservation struct {
	Schema       string
	Relation     string
	Name         string
	Command      string
	Permissive   bool
	HasUsing     bool
	HasWithCheck bool
	Roles        []PolicyRoleObservation
}

type CatalogObservation struct {
	ServerVersionNum int
	ServerMajor      int
	TLS              bool
	ReadOnly         bool
	Schemas          []SchemaObservation
	Extensions       []ExtensionObservation
	Relations        []RelationObservation
	Routines         []RoutineObservation
	ACLs             []ACLObservation
	DefaultACLs      []DefaultACLObservation
	Roles            []RoleObservation
	Memberships      []MembershipObservation
	Policies         []PolicyObservation
	Security         SecurityObservation
}

// ObserveCatalog performs bounded, read-only observation over the validated
// direct route. It does not prove project identity or authorize backup/restore.
func ObserveCatalog(ctx context.Context, params ConnectionParams, password []byte, schemaNames []string) (CatalogObservation, error) {
	if ctx == nil {
		return CatalogObservation{}, ErrCatalogObservation
	}
	if err := validateSchemaNames(schemaNames); err != nil {
		return CatalogObservation{}, err
	}
	config, err := NewConnConfig(params, password)
	if err != nil {
		return CatalogObservation{}, err
	}
	return observeCatalog(ctx, config, schemaNames)
}

func observeCatalog(ctx context.Context, config *pgx.ConnConfig, schemaNames []string) (CatalogObservation, error) {
	if ctx == nil || config == nil {
		return CatalogObservation{}, ErrCatalogObservation
	}

	ctx, cancel := context.WithTimeout(ctx, inspectionTimeout)
	defer cancel()

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return CatalogObservation{}, contextOr(ctx, ErrDatabaseConnect)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	}()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return CatalogObservation{}, contextOr(ctx, ErrCatalogObservation)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, statement := range [...]string{
		"SET LOCAL search_path TO pg_catalog",
		"SET LOCAL statement_timeout TO '8000ms'",
		"SET LOCAL lock_timeout TO '2000ms'",
		"SET LOCAL idle_in_transaction_session_timeout TO '12000ms'",
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return CatalogObservation{}, contextOr(ctx, ErrCatalogObservation)
		}
	}

	var versionNum int
	var tlsActive, readOnly bool
	if err := tx.QueryRow(ctx, `
		SELECT pg_catalog.current_setting('server_version_num')::int,
		       COALESCE((
			       SELECT ssl FROM pg_catalog.pg_stat_ssl
			       WHERE pid = pg_catalog.pg_backend_pid()
		       ), false),
		       pg_catalog.current_setting('transaction_read_only') = 'on'
	`).Scan(&versionNum, &tlsActive, &readOnly); err != nil {
		return CatalogObservation{}, contextOr(ctx, ErrCatalogObservation)
	}

	observation := CatalogObservation{
		ServerVersionNum: versionNum,
		ServerMajor:      serverMajor(versionNum),
		TLS:              tlsActive,
		ReadOnly:         readOnly,
		Schemas:          make([]SchemaObservation, 0, len(schemaNames)),
		Extensions:       make([]ExtensionObservation, 0),
		Relations:        make([]RelationObservation, 0),
		Routines:         make([]RoutineObservation, 0),
		ACLs:             make([]ACLObservation, 0),
		DefaultACLs:      make([]DefaultACLObservation, 0),
		Roles:            make([]RoleObservation, 0),
		Memberships:      make([]MembershipObservation, 0),
		Policies:         make([]PolicyObservation, 0),
	}
	if !observation.ReadOnly {
		return observation, ErrReadOnlyTransaction
	}
	if !observation.TLS {
		return observation, ErrDatabaseTLSRequired
	}
	if err := validatePostgresVersion(versionNum); err != nil {
		return observation, err
	}

	if len(schemaNames) > 0 {
		rows, err := tx.Query(ctx, `
			SELECT requested.name, namespace.nspname IS NOT NULL
			FROM unnest($1::text[]) WITH ORDINALITY AS requested(name, ordinal)
			LEFT JOIN pg_catalog.pg_namespace AS namespace
			  ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
			ORDER BY requested.ordinal
		`, schemaNames)
		if err != nil {
			return observation, contextOr(ctx, ErrCatalogObservation)
		}
		for rows.Next() {
			var schema SchemaObservation
			if err := rows.Scan(&schema.Name, &schema.Present); err != nil {
				rows.Close()
				return observation, contextOr(ctx, ErrCatalogObservation)
			}
			observation.Schemas = append(observation.Schemas, schema)
		}
		rowErr := rows.Err()
		rows.Close()
		if rowErr != nil || len(observation.Schemas) != len(schemaNames) {
			return observation, contextOr(ctx, ErrCatalogObservation)
		}
	}

	if len(schemaNames) > 0 {
		relationRows, err := tx.Query(ctx, `
			SELECT namespace.nspname::text,
			       relation.relname::text,
			       pg_catalog.pg_get_userbyid(relation.relowner)::text,
			       relation.relkind::text,
			       relation.relpersistence::text,
			       relation.relispartition,
			       relation.relrowsecurity,
			       relation.relforcerowsecurity,
			       (SELECT pg_catalog.count(*)
			        FROM pg_catalog.pg_policy AS rls_policy
			        WHERE rls_policy.polrelid = relation.oid),
			       trigger_summary.trigger_count,
			       trigger_summary.user_trigger_count
			FROM unnest($1::text[]) WITH ORDINALITY AS requested(name, ordinal)
			JOIN pg_catalog.pg_namespace AS namespace
			  ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
			JOIN pg_catalog.pg_class AS relation
			  ON relation.relnamespace = namespace.oid
			CROSS JOIN LATERAL (
			  SELECT pg_catalog.count(*) AS trigger_count,
			         pg_catalog.count(*) FILTER (WHERE NOT trigger_row.tgisinternal) AS user_trigger_count
			  FROM pg_catalog.pg_trigger AS trigger_row
			  WHERE trigger_row.tgrelid = relation.oid
			) AS trigger_summary
			ORDER BY requested.ordinal, relation.relname::text COLLATE "C"
			LIMIT $2
		`, schemaNames, maxObservedRelations+1)
		if err != nil {
			return observation, contextOr(ctx, ErrCatalogObservation)
		}
		for relationRows.Next() {
			var relation RelationObservation
			if err := relationRows.Scan(&relation.Schema, &relation.Name, &relation.Owner, &relation.Kind, &relation.Persistence, &relation.IsPartition, &relation.RowSecurityEnabled, &relation.ForceRowSecurity, &relation.PolicyCount, &relation.TriggerCount, &relation.UserTriggerCount); err != nil {
				relationRows.Close()
				return observation, contextOr(ctx, ErrCatalogObservation)
			}
			if !validIdentifier(relation.Schema) || !validIdentifier(relation.Name) || !validIdentifier(relation.Owner) || len(relation.Kind) != 1 || len(relation.Persistence) != 1 {
				relationRows.Close()
				return observation, ErrCatalogObservation
			}
			observation.Relations = append(observation.Relations, relation)
		}
		relationErr := relationRows.Err()
		relationRows.Close()
		if relationErr != nil || len(observation.Relations) > maxObservedRelations {
			return observation, contextOr(ctx, ErrCatalogObservation)
		}
	}

	if len(schemaNames) > 0 {
		routineRows, err := tx.Query(ctx, `
			SELECT namespace.nspname::text,
			       routine.proname::text,
			       CASE WHEN pg_catalog.octet_length(identity.arguments) <= $1
			            THEN identity.arguments ELSE '' END,
			       routine.prokind::text,
			       language.lanname::text,
			       routine.prosecdef,
			       routine.proconfig IS NOT NULL,
			       pg_catalog.octet_length(identity.arguments)
			FROM unnest($2::text[]) WITH ORDINALITY AS requested(name, ordinal)
			JOIN pg_catalog.pg_namespace AS namespace
			  ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
			JOIN pg_catalog.pg_proc AS routine
			  ON routine.pronamespace = namespace.oid
			JOIN pg_catalog.pg_language AS language
			  ON language.oid = routine.prolang
			CROSS JOIN LATERAL (
			  SELECT pg_catalog.pg_get_function_identity_arguments(routine.oid) AS arguments
			) AS identity
			ORDER BY requested.ordinal, routine.proname::text COLLATE "C", identity.arguments COLLATE "C"
			LIMIT $3
		`, maxRoutineIdentityArguments, schemaNames, maxObservedRoutines+1)
		if err != nil {
			return observation, contextOr(ctx, ErrCatalogObservation)
		}
		for routineRows.Next() {
			var routine RoutineObservation
			var argumentBytes int
			if err := routineRows.Scan(&routine.Schema, &routine.Name, &routine.IdentityArguments, &routine.Kind, &routine.Language, &routine.SecurityDefiner, &routine.HasConfiguration, &argumentBytes); err != nil {
				routineRows.Close()
				return observation, contextOr(ctx, ErrCatalogObservation)
			}
			if argumentBytes > maxRoutineIdentityArguments || !validIdentifier(routine.Schema) || !validIdentifier(routine.Name) || !validText(routine.IdentityArguments) || len(routine.Kind) != 1 || !validIdentifier(routine.Language) {
				routineRows.Close()
				return observation, ErrCatalogObservation
			}
			observation.Routines = append(observation.Routines, routine)
		}
		routineErr := routineRows.Err()
		routineRows.Close()
		if routineErr != nil || len(observation.Routines) > maxObservedRoutines {
			return observation, contextOr(ctx, ErrCatalogObservation)
		}
	}

	acls, err := observeACLs(ctx, tx, schemaNames)
	if err != nil {
		return observation, contextOr(ctx, ErrCatalogObservation)
	}
	observation.ACLs = acls

	defaultACLs, err := observeDefaultACLs(ctx, tx, schemaNames)
	if err != nil {
		return observation, contextOr(ctx, ErrCatalogObservation)
	}
	observation.DefaultACLs = defaultACLs

	roles, err := observeRoles(ctx, tx)
	if err != nil {
		return observation, contextOr(ctx, ErrCatalogObservation)
	}
	observation.Roles = roles

	memberships, err := observeMemberships(ctx, tx)
	if err != nil {
		return observation, contextOr(ctx, ErrCatalogObservation)
	}
	observation.Memberships = memberships

	policies, err := observePolicies(ctx, tx, schemaNames)
	if err != nil {
		return observation, contextOr(ctx, ErrCatalogObservation)
	}
	observation.Policies = policies

	security, err := observeSecurity(ctx, tx, schemaNames)
	if err != nil {
		return observation, contextOr(ctx, ErrCatalogObservation)
	}
	observation.Security = security

	extensionRows, err := tx.Query(ctx, `
		SELECT extension.extname::text,
		       CASE WHEN pg_catalog.octet_length(extension.extversion) <= $1
		            THEN extension.extversion ELSE '' END,
		       namespace.nspname::text,
		       pg_catalog.octet_length(extension.extversion)
		FROM pg_catalog.pg_extension AS extension
		JOIN pg_catalog.pg_namespace AS namespace
		  ON namespace.oid = extension.extnamespace
		ORDER BY extension.extname::text COLLATE "C"
		LIMIT $2
	`, maxExtensionVersion, maxObservedExtensions+1)
	if err != nil {
		return observation, contextOr(ctx, ErrCatalogObservation)
	}
	for extensionRows.Next() {
		var extension ExtensionObservation
		var versionBytes int
		if err := extensionRows.Scan(&extension.Name, &extension.Version, &extension.Schema, &versionBytes); err != nil {
			extensionRows.Close()
			return observation, contextOr(ctx, ErrCatalogObservation)
		}
		if versionBytes > maxExtensionVersion || !validIdentifier(extension.Name) || !validText(extension.Version) || !validIdentifier(extension.Schema) {
			extensionRows.Close()
			return observation, ErrCatalogObservation
		}
		observation.Extensions = append(observation.Extensions, extension)
	}
	extensionErr := extensionRows.Err()
	extensionRows.Close()
	if extensionErr != nil || len(observation.Extensions) > maxObservedExtensions {
		return observation, contextOr(ctx, ErrCatalogObservation)
	}

	if err := tx.Commit(ctx); err != nil {
		return observation, contextOr(ctx, ErrCatalogObservation)
	}
	return observation, nil
}

func serverMajor(versionNum int) int {
	if versionNum <= 0 {
		return 0
	}
	return versionNum / 10000
}

func validatePostgresVersion(versionNum int) error {
	if serverMajor(versionNum) != supportedPostgresMajor {
		return ErrUnsupportedServerVersion
	}
	return nil
}

func contextOr(ctx context.Context, fallback error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fallback
}
