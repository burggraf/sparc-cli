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
		       COALESCE(access.grantee = 0, false),
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
		var aclIsNull, grantable, granteeIsPublic bool
		if err := rows.Scan(&schema, &relation, &column, &aclIsNull, &grantee, &granteeIsPublic, &grantor, &privilege, &grantable); err != nil {
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
			if grantable || granteeIsPublic {
				rows.Close()
				return nil, ErrCatalogObservation
			}
			continue
		}
		if !validIdentifier(grantee) || !validIdentifier(grantor) || !validIdentifier(privilege) || granteeIsPublic && grantee != "PUBLIC" {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		observations[len(observations)-1].Grants = append(observations[len(observations)-1].Grants, ACLGrantObservation{
			Grantee: grantee, GranteeIsPublic: granteeIsPublic, Grantor: grantor, Privilege: privilege, Grantable: grantable,
		})
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	return observations, nil
}

func observeDefaultACLs(ctx context.Context, tx pgx.Tx, schemaNames []string) ([]DefaultACLObservation, error) {
	observations := make([]DefaultACLObservation, 0)
	rows, err := tx.Query(ctx, `
		SELECT access.creator,
		       access.schema_name,
		       access.object_type,
		       access.acl_is_null,
		       CASE
		         WHEN access.grantee IS NULL THEN ''::text
		         WHEN access.grantee = 0 THEN 'PUBLIC'
		         ELSE pg_catalog.pg_get_userbyid(access.grantee)::text
		       END,
		       COALESCE(access.grantee = 0, false),
		       COALESCE(pg_catalog.pg_get_userbyid(access.grantor)::text, ''),
		       COALESCE(access.privilege_type::text, ''),
		       COALESCE(access.is_grantable, false)
		FROM (
		  SELECT COALESCE(pg_catalog.pg_get_userbyid(defaults.defaclrole)::text, '') AS creator,
		         COALESCE(namespace.nspname::text, '') AS schema_name,
		         defaults.defaclobjtype::text AS object_type,
		         defaults.defaclacl IS NULL AS acl_is_null,
		         exploded.grantee,
		         exploded.grantor,
		         exploded.privilege_type,
		         exploded.is_grantable
		  FROM pg_catalog.pg_default_acl AS defaults
		  LEFT JOIN pg_catalog.pg_namespace AS namespace
		    ON namespace.oid = defaults.defaclnamespace
		  LEFT JOIN LATERAL pg_catalog.aclexplode(defaults.defaclacl) AS exploded ON true
		  WHERE defaults.defaclnamespace = 0
		     OR EXISTS (
		       SELECT 1
		       FROM unnest($1::text[]) AS requested(name)
		       WHERE namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
		     )
		  LIMIT $2
		) AS access
		ORDER BY access.creator COLLATE "C",
		         access.schema_name COLLATE "C",
		         access.object_type COLLATE "C",
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
		var creator, schema, objectType, grantee, grantor, privilege string
		var aclIsNull, grantable, granteeIsPublic bool
		if err := rows.Scan(&creator, &schema, &objectType, &aclIsNull, &grantee, &granteeIsPublic, &grantor, &privilege, &grantable); err != nil {
			rows.Close()
			return nil, err
		}
		if !validIdentifier(creator) || schema != "" && !validIdentifier(schema) || len(objectType) != 1 || !validText(objectType) {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		newACL := len(observations) == 0 || observations[len(observations)-1].Creator != creator || observations[len(observations)-1].Schema != schema || observations[len(observations)-1].ObjectType != objectType
		if newACL {
			observations = append(observations, DefaultACLObservation{Creator: creator, Schema: schema, ObjectType: objectType, ACLIsNull: aclIsNull})
		} else if observations[len(observations)-1].ACLIsNull != aclIsNull {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		if grantee == "" && grantor == "" && privilege == "" {
			if grantable || granteeIsPublic {
				rows.Close()
				return nil, ErrCatalogObservation
			}
			continue
		}
		if !validIdentifier(grantee) || !validIdentifier(grantor) || !validIdentifier(privilege) || granteeIsPublic && grantee != "PUBLIC" {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		observations[len(observations)-1].Grants = append(observations[len(observations)-1].Grants, ACLGrantObservation{
			Grantee: grantee, GranteeIsPublic: granteeIsPublic, Grantor: grantor, Privilege: privilege, Grantable: grantable,
		})
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	return observations, nil
}

func observeRoles(ctx context.Context, tx pgx.Tx) ([]RoleObservation, error) {
	observations := make([]RoleObservation, 0)
	rows, err := tx.Query(ctx, `
		SELECT roles.name,
		       roles.superuser,
		       roles.inherit,
		       roles.create_role,
		       roles.create_database,
		       roles.can_login,
		       roles.replication,
		       roles.bypass_rls
		FROM (
		  SELECT role.rolname::text AS name,
		         role.rolsuper AS superuser,
		         role.rolinherit AS inherit,
		         role.rolcreaterole AS create_role,
		         role.rolcreatedb AS create_database,
		         role.rolcanlogin AS can_login,
		         role.rolreplication AS replication,
		         role.rolbypassrls AS bypass_rls
		  FROM pg_catalog.pg_roles AS role
		  LIMIT $1
		) AS roles
		ORDER BY roles.name COLLATE "C"
	`, maxObservedRoles+1)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		if len(observations) == maxObservedRoles {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		var role RoleObservation
		if err := rows.Scan(&role.Name, &role.Superuser, &role.Inherit, &role.CreateRole, &role.CreateDatabase, &role.CanLogin, &role.Replication, &role.BypassRLS); err != nil {
			rows.Close()
			return nil, err
		}
		if !validIdentifier(role.Name) {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		observations = append(observations, role)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	return observations, nil
}

func observeMemberships(ctx context.Context, tx pgx.Tx) ([]MembershipObservation, error) {
	observations := make([]MembershipObservation, 0)
	rows, err := tx.Query(ctx, `
		SELECT memberships.role_name,
		       memberships.member_name,
		       memberships.grantor_name,
		       memberships.admin_option,
		       memberships.inherit_option,
		       memberships.set_option
		FROM (
		  SELECT COALESCE(pg_catalog.pg_get_userbyid(membership.roleid)::text, '') AS role_name,
		         COALESCE(pg_catalog.pg_get_userbyid(membership.member)::text, '') AS member_name,
		         COALESCE(pg_catalog.pg_get_userbyid(membership.grantor)::text, '') AS grantor_name,
		         membership.admin_option,
		         membership.inherit_option,
		         membership.set_option
		  FROM pg_catalog.pg_auth_members AS membership
		  LIMIT $1
		) AS memberships
		ORDER BY memberships.role_name COLLATE "C",
		         memberships.member_name COLLATE "C",
		         memberships.grantor_name COLLATE "C"
	`, maxObservedMemberships+1)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		if len(observations) == maxObservedMemberships {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		var membership MembershipObservation
		if err := rows.Scan(&membership.Role, &membership.Member, &membership.Grantor, &membership.AdminOption, &membership.InheritOption, &membership.SetOption); err != nil {
			rows.Close()
			return nil, err
		}
		if !validIdentifier(membership.Role) || !validIdentifier(membership.Member) || !validIdentifier(membership.Grantor) {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		observations = append(observations, membership)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	return observations, nil
}

func observePolicies(ctx context.Context, tx pgx.Tx, schemaNames []string) ([]PolicyObservation, error) {
	observations := make([]PolicyObservation, 0)
	if len(schemaNames) == 0 {
		return observations, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT policy.schema_name, policy.relation_name, policy.policy_name,
		       policy.command, policy.permissive, policy.has_using,
		       policy.has_with_check, policy.role_present, policy.is_public,
		       policy.role_name
		FROM (
		  SELECT namespace.nspname::text AS schema_name,
		         relation.relname::text AS relation_name,
		         rule.polname::text AS policy_name,
		         rule.polcmd::text AS command,
		         rule.polpermissive AS permissive,
		         rule.polqual IS NOT NULL AS has_using,
		         rule.polwithcheck IS NOT NULL AS has_with_check,
		         assigned.role_oid IS NOT NULL AS role_present,
		         COALESCE(assigned.role_oid = 0, false) AS is_public,
		         CASE WHEN assigned.role_oid = 0 THEN 'PUBLIC'::text
		              ELSE COALESCE(role.rolname::text, '') END AS role_name
		  FROM unnest($1::text[]) AS requested(name)
		  JOIN pg_catalog.pg_namespace AS namespace
		    ON namespace.nspname::text COLLATE "C" = requested.name COLLATE "C"
		  JOIN pg_catalog.pg_class AS relation
		    ON relation.relnamespace = namespace.oid
		  JOIN pg_catalog.pg_policy AS rule
		    ON rule.polrelid = relation.oid
		  LEFT JOIN LATERAL unnest(rule.polroles) AS assigned(role_oid) ON true
		  LEFT JOIN pg_catalog.pg_roles AS role
		    ON role.oid = assigned.role_oid
		  LIMIT $2
		) AS policy
		ORDER BY policy.schema_name COLLATE "C",
		         policy.relation_name COLLATE "C",
		         policy.policy_name COLLATE "C",
		         policy.role_name COLLATE "C",
		         policy.is_public
	`, schemaNames, maxObservedPolicyRows+1)
	if err != nil {
		return nil, err
	}
	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > maxObservedPolicyRows {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		var schema, relation, name, command, roleName string
		var permissive, hasUsing, hasWithCheck, rolePresent, isPublic bool
		if err := rows.Scan(&schema, &relation, &name, &command, &permissive, &hasUsing, &hasWithCheck, &rolePresent, &isPublic, &roleName); err != nil {
			rows.Close()
			return nil, err
		}
		if !validIdentifier(schema) || !validIdentifier(relation) || !validIdentifier(name) || len(command) != 1 || !validText(command) ||
			rolePresent && (!validIdentifier(roleName) || isPublic && roleName != "PUBLIC") ||
			!rolePresent && (roleName != "" || isPublic) {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		newPolicy := len(observations) == 0 || observations[len(observations)-1].Schema != schema || observations[len(observations)-1].Relation != relation || observations[len(observations)-1].Name != name
		if newPolicy {
			observations = append(observations, PolicyObservation{
				Schema: schema, Relation: relation, Name: name, Command: command,
				Permissive: permissive, HasUsing: hasUsing, HasWithCheck: hasWithCheck,
				Roles: make([]PolicyRoleObservation, 0),
			})
		} else if prior := observations[len(observations)-1]; prior.Command != command || prior.Permissive != permissive || prior.HasUsing != hasUsing || prior.HasWithCheck != hasWithCheck {
			rows.Close()
			return nil, ErrCatalogObservation
		}
		if rolePresent {
			observations[len(observations)-1].Roles = append(observations[len(observations)-1].Roles, PolicyRoleObservation{Name: roleName, IsPublic: isPublic})
		}
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
