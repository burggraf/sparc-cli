package database

import (
	"context"

	"github.com/jackc/pgx/v5"
)

const (
	// ponytail: cap each public/Auth shape query at 10,000 rows; raise only after measuring memory.
	maxObservedPublicAuthRows = 10000
	maxObservedDatabaseType   = 512
)

// PublicAuthMetadata contains the existing catalog observation plus bounded
// relation-column and constraint metadata for exactly public and auth.
type PublicAuthMetadata struct {
	Catalog     CatalogObservation
	Columns     []PublicAuthColumnObservation
	Constraints []PublicAuthConstraintObservation
}

type PublicAuthColumnObservation struct {
	Schema    string
	Relation  string
	Kind      string
	Owner     string
	Ordinal   int
	Name      string
	DataType  string
	NotNull   bool
	Identity  string
	Generated string
}

type PublicAuthConstraintObservation struct {
	Schema           string
	Relation         string
	Name             string
	Kind             string
	Validated        bool
	Deferrable       bool
	Deferred         bool
	ReferencedSchema string
	ReferencedTable  string
	ColumnOrdinal    int
	Column           string
	ReferencedColumn string
}

// ObservePublicAuthMetadata observes fixed public/auth catalog metadata over a
// validated TLS route. It never reads relation rows or Auth user values.
func ObservePublicAuthMetadata(ctx context.Context, params ConnectionParams, password []byte) (PublicAuthMetadata, error) {
	if ctx == nil {
		return PublicAuthMetadata{}, ErrCatalogObservation
	}
	config, err := NewConnConfig(params, password)
	if err != nil {
		return PublicAuthMetadata{}, err
	}
	return observePublicAuthMetadata(ctx, config)
}

func observePublicAuthMetadata(ctx context.Context, config *pgx.ConnConfig) (PublicAuthMetadata, error) {
	metadata := PublicAuthMetadata{
		Columns:     make([]PublicAuthColumnObservation, 0),
		Constraints: make([]PublicAuthConstraintObservation, 0),
	}
	catalog, err := observeCatalogWithShape(ctx, config, []string{"public", "auth"}, &metadata)
	if err != nil {
		return PublicAuthMetadata{}, err
	}
	metadata.Catalog = catalog
	return metadata, nil
}

func isPublicAuthSchemaSelection(names []string) bool {
	return len(names) == 2 && names[0] == "public" && names[1] == "auth"
}

func observePublicAuthShape(ctx context.Context, tx pgx.Tx, schemaNames []string, metadata *PublicAuthMetadata) error {
	if tx == nil || metadata == nil || !isPublicAuthSchemaSelection(schemaNames) {
		return ErrCatalogObservation
	}

	columnRows, err := tx.Query(ctx, `
		SELECT namespace.nspname::text,
		       relation.relname::text,
		       relation.relkind::text,
		       pg_catalog.pg_get_userbyid(relation.relowner)::text,
		       attribute.attnum,
		       attribute.attname::text,
		       pg_catalog.format_type(attribute.atttypid, attribute.atttypmod),
		       attribute.attnotnull,
		       attribute.attidentity::text,
		       attribute.attgenerated::text
		FROM pg_catalog.pg_namespace AS namespace
		JOIN pg_catalog.pg_class AS relation ON relation.relnamespace = namespace.oid
		JOIN pg_catalog.pg_attribute AS attribute
		  ON attribute.attrelid = relation.oid
		 AND attribute.attnum > 0
		 AND NOT attribute.attisdropped
		WHERE namespace.nspname::text = ANY ($1::text[])
		  AND relation.relkind IN ('r','p','v','m','f','S')
		ORDER BY namespace.nspname COLLATE "C", relation.relname COLLATE "C", attribute.attnum
		LIMIT $2
	`, schemaNames, maxObservedPublicAuthRows+1)
	if err != nil {
		return contextOr(ctx, ErrCatalogObservation)
	}
	for columnRows.Next() {
		var column PublicAuthColumnObservation
		var ordinal int16
		if err := columnRows.Scan(&column.Schema, &column.Relation, &column.Kind, &column.Owner, &ordinal, &column.Name, &column.DataType, &column.NotNull, &column.Identity, &column.Generated); err != nil {
			columnRows.Close()
			return contextOr(ctx, ErrCatalogObservation)
		}
		column.Ordinal = int(ordinal)
		if !validIdentifier(column.Schema) || !validIdentifier(column.Relation) || !validIdentifier(column.Owner) || !validIdentifier(column.Name) || column.DataType == "" || !validText(column.DataType) || len(column.DataType) > maxObservedDatabaseType || column.Ordinal <= 0 || len(column.Kind) != 1 || !validIdentityMarker(column.Identity) || !validGeneratedMarker(column.Generated) {
			columnRows.Close()
			return ErrCatalogObservation
		}
		metadata.Columns = append(metadata.Columns, column)
	}
	columnErr := columnRows.Err()
	columnRows.Close()
	if columnErr != nil || len(metadata.Columns) > maxObservedPublicAuthRows {
		return contextOr(ctx, ErrCatalogObservation)
	}

	constraintRows, err := tx.Query(ctx, `
		SELECT source_namespace.nspname::text,
		       source_relation.relname::text,
		       constraint_row.conname::text,
		       constraint_row.contype::text,
		       constraint_row.convalidated,
		       constraint_row.condeferrable,
		       constraint_row.condeferred,
		       COALESCE(target_namespace.nspname::text, ''),
		       COALESCE(target_relation.relname::text, ''),
		       COALESCE(key_pair.ordinality, 0)::int,
		       COALESCE(source_column.attname::text, ''),
		       COALESCE(target_column.attname::text, '')
		FROM pg_catalog.pg_constraint AS constraint_row
		JOIN pg_catalog.pg_class AS source_relation ON source_relation.oid = constraint_row.conrelid
		JOIN pg_catalog.pg_namespace AS source_namespace ON source_namespace.oid = source_relation.relnamespace
		LEFT JOIN pg_catalog.pg_class AS target_relation ON target_relation.oid = constraint_row.confrelid
		LEFT JOIN pg_catalog.pg_namespace AS target_namespace ON target_namespace.oid = target_relation.relnamespace
		LEFT JOIN LATERAL unnest(constraint_row.conkey, constraint_row.confkey)
		     WITH ORDINALITY AS key_pair(source_attnum, target_attnum, ordinality) ON true
		LEFT JOIN pg_catalog.pg_attribute AS source_column
		  ON source_column.attrelid = source_relation.oid AND source_column.attnum = key_pair.source_attnum
		LEFT JOIN pg_catalog.pg_attribute AS target_column
		  ON target_column.attrelid = target_relation.oid AND target_column.attnum = key_pair.target_attnum
		WHERE constraint_row.contype IN ('p','u','f','c','x')
		  AND (source_namespace.nspname::text = ANY ($1::text[])
		       OR target_namespace.nspname::text = ANY ($1::text[]))
		ORDER BY source_namespace.nspname COLLATE "C", source_relation.relname COLLATE "C",
		         constraint_row.conname COLLATE "C", key_pair.ordinality
		LIMIT $2
	`, schemaNames, maxObservedPublicAuthRows+1)
	if err != nil {
		return contextOr(ctx, ErrCatalogObservation)
	}
	for constraintRows.Next() {
		var constraint PublicAuthConstraintObservation
		if err := constraintRows.Scan(&constraint.Schema, &constraint.Relation, &constraint.Name, &constraint.Kind, &constraint.Validated, &constraint.Deferrable, &constraint.Deferred, &constraint.ReferencedSchema, &constraint.ReferencedTable, &constraint.ColumnOrdinal, &constraint.Column, &constraint.ReferencedColumn); err != nil {
			constraintRows.Close()
			return contextOr(ctx, ErrCatalogObservation)
		}
		if !validIdentifier(constraint.Schema) || !validIdentifier(constraint.Relation) || !validIdentifier(constraint.Name) || !validConstraintKind(constraint.Kind) || constraint.ColumnOrdinal < 0 {
			constraintRows.Close()
			return ErrCatalogObservation
		}
		if constraint.ColumnOrdinal == 0 {
			if constraint.Column != "" || constraint.ReferencedColumn != "" {
				constraintRows.Close()
				return ErrCatalogObservation
			}
		} else if !validIdentifier(constraint.Column) {
			constraintRows.Close()
			return ErrCatalogObservation
		}
		if constraint.Kind == "f" {
			if !validIdentifier(constraint.ReferencedSchema) || !validIdentifier(constraint.ReferencedTable) || constraint.ColumnOrdinal == 0 || !validIdentifier(constraint.ReferencedColumn) {
				constraintRows.Close()
				return ErrCatalogObservation
			}
		} else if constraint.ReferencedSchema != "" || constraint.ReferencedTable != "" || constraint.ReferencedColumn != "" {
			constraintRows.Close()
			return ErrCatalogObservation
		}
		metadata.Constraints = append(metadata.Constraints, constraint)
	}
	constraintErr := constraintRows.Err()
	constraintRows.Close()
	if constraintErr != nil || len(metadata.Constraints) > maxObservedPublicAuthRows {
		return contextOr(ctx, ErrCatalogObservation)
	}
	return nil
}

func validIdentityMarker(value string) bool {
	return value == "" || value == "a" || value == "d"
}

func validGeneratedMarker(value string) bool {
	return value == "" || value == "s"
}

func validConstraintKind(value string) bool {
	switch value {
	case "p", "u", "f", "c", "x":
		return true
	default:
		return false
	}
}
