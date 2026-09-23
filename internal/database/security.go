package database

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var (
	ErrTargetSecurityUnknown       = errors.New("target security profile is unknown")
	ErrUnsafeTargetSecurityProfile = errors.New("target exceeds the conservative security profile")
)

type SecurityObservation struct {
	Observed             bool
	PublicSelectDefaults bool
	PublicSelectObjects  bool
	PublicSelectColumns  bool
}

// CheckTargetSecurityV1 is a deliberately conservative first gate, not a
// complete Supabase permission baseline. It rejects PUBLIC SELECT on selected
// row-bearing objects or their future table defaults. A nil result is not
// restore authorization or proof of overall target safety.
func CheckTargetSecurityV1(observation CatalogObservation) error {
	if observation.ServerMajor != supportedPostgresMajor || !observation.TLS || !observation.ReadOnly || len(observation.Schemas) == 0 || !observation.Security.Observed {
		return ErrTargetSecurityUnknown
	}
	if observation.Security.PublicSelectDefaults || observation.Security.PublicSelectObjects || observation.Security.PublicSelectColumns {
		return ErrUnsafeTargetSecurityProfile
	}
	return nil
}

func observeSecurity(ctx context.Context, tx pgx.Tx, schemaNames []string) (SecurityObservation, error) {
	var observation SecurityObservation
	err := tx.QueryRow(ctx, `
		SELECT
			EXISTS (
				SELECT 1
				FROM pg_catalog.pg_default_acl AS defaults
				CROSS JOIN LATERAL pg_catalog.aclexplode(defaults.defaclacl) AS acl
				WHERE defaults.defaclobjtype = 'r'
				  AND (
					defaults.defaclnamespace = 0
					OR EXISTS (
						SELECT 1
						FROM unnest($1::text[]) AS requested(name)
						JOIN pg_catalog.pg_namespace AS namespace
						  ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
						WHERE namespace.oid = defaults.defaclnamespace
					)
				  )
				  AND acl.grantee = 0
				  AND acl.privilege_type = 'SELECT'
			),
			EXISTS (
				SELECT 1
				FROM unnest($1::text[]) AS requested(name)
				JOIN pg_catalog.pg_namespace AS namespace
				  ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
				JOIN pg_catalog.pg_class AS relation
				  ON relation.relnamespace = namespace.oid
				CROSS JOIN LATERAL pg_catalog.aclexplode(relation.relacl) AS acl
				WHERE relation.relkind IN ('r', 'p', 'v', 'm', 'f')
				  AND acl.grantee = 0
				  AND acl.privilege_type = 'SELECT'
			),
			EXISTS (
				SELECT 1
				FROM unnest($1::text[]) AS requested(name)
				JOIN pg_catalog.pg_namespace AS namespace
				  ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
				JOIN pg_catalog.pg_class AS relation
				  ON relation.relnamespace = namespace.oid
			JOIN pg_catalog.pg_attribute AS attribute
			  ON attribute.attrelid = relation.oid
			CROSS JOIN LATERAL pg_catalog.aclexplode(attribute.attacl) AS acl
			WHERE relation.relkind IN ('r', 'p', 'v', 'm', 'f')
			  AND attribute.attnum > 0
			  AND NOT attribute.attisdropped
			  AND acl.grantee = 0
			  AND acl.privilege_type = 'SELECT'
		)
	`, schemaNames).Scan(&observation.PublicSelectDefaults, &observation.PublicSelectObjects, &observation.PublicSelectColumns)
	if err != nil {
		return SecurityObservation{}, err
	}
	observation.Observed = true
	return observation, nil
}
