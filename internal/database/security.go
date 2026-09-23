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

func observeACLs(ctx context.Context, tx pgx.Tx, schemaNames []string) ([]ACLObservation, error) {
	observations := make([]ACLObservation, 0)
	if len(schemaNames) == 0 {
		return observations, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT access.schema_name,
		       access.relation_name,
		       access.column_name,
		       access.acl_is_null,
		       CASE
		         WHEN access.grantee IS NULL THEN ''::text
		         WHEN access.grantee = 0 THEN 'PUBLIC'
		         ELSE pg_catalog.pg_get_userbyid(access.grantee)::text
		       END,
		       COALESCE(pg_catalog.pg_get_userbyid(access.grantor)::text, ''),
		       COALESCE(access.privilege_type::text, ''),
		       COALESCE(access.is_grantable, false)
		FROM (
		  SELECT *
		  FROM (
		    SELECT namespace.nspname::text AS schema_name,
		         relation.relname::text AS relation_name,
		         ''::text AS column_name,
		         relation.relacl IS NULL AS acl_is_null,
		         exploded.grantee,
		         exploded.grantor,
		         exploded.privilege_type,
		         exploded.is_grantable
		  FROM unnest($1::text[]) AS requested(name)
		  JOIN pg_catalog.pg_namespace AS namespace
		    ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
		  JOIN pg_catalog.pg_class AS relation
		    ON relation.relnamespace = namespace.oid
		  LEFT JOIN LATERAL pg_catalog.aclexplode(relation.relacl) AS exploded ON true
		  UNION ALL
		  SELECT namespace.nspname::text,
		         relation.relname::text,
		         attribute.attname::text,
		         attribute.attacl IS NULL,
		         exploded.grantee,
		         exploded.grantor,
		         exploded.privilege_type,
		         exploded.is_grantable
		  FROM unnest($1::text[]) AS requested(name)
		  JOIN pg_catalog.pg_namespace AS namespace
		    ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
		  JOIN pg_catalog.pg_class AS relation
		    ON relation.relnamespace = namespace.oid
		  JOIN pg_catalog.pg_attribute AS attribute
		    ON attribute.attrelid = relation.oid
		   AND attribute.attnum > 0
		   AND NOT attribute.attisdropped
		    LEFT JOIN LATERAL pg_catalog.aclexplode(attribute.attacl) AS exploded ON true
		  ) AS all_access
		  LIMIT $2
		) AS access
		ORDER BY access.schema_name COLLATE "C",
		         access.relation_name COLLATE "C",
		         access.column_name COLLATE "C",
		         access.grantor,
		         access.grantee,
		         access.privilege_type COLLATE "C",
		         access.is_grantable
	`, schemaNames, maxObservedACLRows+1)
	if err != nil {
		return nil, err
	}
	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > maxObservedACLRows {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		var schema, relation, column, grantee, grantor, privilege string
		var aclIsNull, grantable bool
		if err := rows.Scan(&schema, &relation, &column, &aclIsNull, &grantee, &grantor, &privilege, &grantable); err != nil {
			rows.Close()
			return nil, err
		}
		if !validIdentifier(schema) || !validIdentifier(relation) || column != "" && !validIdentifier(column) {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		newObject := len(observations) == 0 || observations[len(observations)-1].Schema != schema || observations[len(observations)-1].Relation != relation || observations[len(observations)-1].Column != column
		if newObject {
			observations = append(observations, ACLObservation{Schema: schema, Relation: relation, Column: column, ACLIsNull: aclIsNull})
		} else if observations[len(observations)-1].ACLIsNull != aclIsNull {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		if grantee == "" && grantor == "" && privilege == "" {
			if grantable {
				rows.Close()
				return nil, ErrCatalogObservation
			}
			continue
		}
		if !validIdentifier(grantee) || !validIdentifier(grantor) || !validIdentifier(privilege) {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		observations[len(observations)-1].Grants = append(observations[len(observations)-1].Grants, ACLGrantObservation{
			Grantee: grantee, Grantor: grantor, Privilege: privilege, Grantable: grantable,
		})
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	return observations, nil
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
