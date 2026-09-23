package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	supportedPostgresMajor = 17
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

type CatalogObservation struct {
	ServerVersionNum int
	ServerMajor      int
	TLS              bool
	ReadOnly         bool
	Schemas          []SchemaObservation
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
