package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ponytail: bound catalog output at 256 extensions, 256 version bytes and 10,000 relations; raise only with reviewed report limits.
const (
	supportedPostgresMajor = 17
	maxObservedExtensions  = 256
	maxExtensionVersion    = 256
	maxObservedRelations   = 10000
	inspectionTimeout      = 15 * time.Second
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

type RelationObservation struct {
	Schema      string
	Name        string
	Kind        string
	Persistence string
	IsPartition bool
}

type CatalogObservation struct {
	ServerVersionNum int
	ServerMajor      int
	TLS              bool
	ReadOnly         bool
	Schemas          []SchemaObservation
	Extensions       []ExtensionObservation
	Relations        []RelationObservation
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
			       relation.relkind::text,
			       relation.relpersistence::text,
			       relation.relispartition
			FROM unnest($1::text[]) WITH ORDINALITY AS requested(name, ordinal)
			JOIN pg_catalog.pg_namespace AS namespace
			  ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
			JOIN pg_catalog.pg_class AS relation
			  ON relation.relnamespace = namespace.oid
			ORDER BY requested.ordinal, relation.relname::text COLLATE "C"
			LIMIT $2
		`, schemaNames, maxObservedRelations+1)
		if err != nil {
			return observation, contextOr(ctx, ErrCatalogObservation)
		}
		for relationRows.Next() {
			var relation RelationObservation
			if err := relationRows.Scan(&relation.Schema, &relation.Name, &relation.Kind, &relation.Persistence, &relation.IsPartition); err != nil {
				relationRows.Close()
				return observation, contextOr(ctx, ErrCatalogObservation)
			}
			if !validIdentifier(relation.Schema) || !validIdentifier(relation.Name) || len(relation.Kind) != 1 || len(relation.Persistence) != 1 {
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
