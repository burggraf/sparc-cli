# R05 — Destination permissions and ordinary `public` applications

**Status: open; public documentation reviewed, no hosted baseline or permission-preflight behavior qualified.** This record defines the hazards and local evidence gates for Task 10. It does not choose a universal Supabase ACL profile or claim that a matched catalog snapshot is safe.

**Reviewed:** 2026-09-23. PostgreSQL 17 privilege, GRANT, default-privilege, role, membership, RLS, and catalog documentation; current public Supabase API-security, RLS, and role guidance. No project, database endpoint, credential, or hosted resource was used.

## PostgreSQL facts that affect the preflight

- Object ownership, ACL grants, role membership, and RLS are distinct controls. Owners normally control their objects and can grant/revoke object privileges; `PUBLIC` is a special grantee applying to all roles. Table and column grants are separate, as are grantor, grant option, and privilege identity. A table-level grant can authorize access even when an observer notices a separate column ACL.
- ACL representation is not binary. A NULL ACL represents the object's built-in default privileges; an explicit empty ACL represents a different state. `pg_default_acl` describes defaults used for future object creation; it is not retroactively applied when observing an existing NULL ACL. Never collapse absent, NULL, empty, or explicit ACL values into one state.
- Default privileges are attached to the role creating future objects. Global defaults and per-schema defaults are distinct; per-schema grants add to global/built-in defaults and cannot revoke a global grant. A current object ACL and the target's defaults both matter: a later restore can create an exposed object even if existing-object ACL checks pass.
- `pg_auth_members` includes membership grantor and PG17 `admin_option`, `inherit_option`, and `set_option`. Role-level attributes also affect effective access. A membership edge alone is not a sufficient authorization model; local OIDs are not portable role identities.
- When RLS is enabled, policies apply to row operations; absence of policies is default-deny for those operations. Table owners normally bypass policies unless `FORCE ROW LEVEL SECURITY` is set; superusers and `BYPASSRLS` roles always bypass. Table/column grants still decide whether the role can reach the object at all. A security-definer routine executes with its owner's authority, so routine presence/metadata is not policy-equivalence evidence.
- `pg_authid` contains password material and is not publicly readable; `pg_roles` masks passwords. SPARC must not query, record, compare, or export role password hashes as part of a permission baseline.

## Supabase-specific boundary

- Supabase documents Data API access as two gates: SQL grants determine which API roles can reach an object; RLS policies determine which rows those roles can see or change. Both must be checked. A policy does not remove a broad grant, and a grant alone does not establish intended row visibility.
- Current documentation describes roles including `anon`, `authenticated`, `service_role`, `postgres`, and service-managed roles. It describes `service_role` as elevated and able to bypass RLS. Never broaden grants to it or infer that a role is harmless from its name.
- Current API-security guidance says existing projects may have default grants on new `public` tables/functions and also describes a platform-default change intended to revoke those automatic grants. This is explicit evidence against copying a single public-schema baseline across all projects. Project age, managed roles, migrations, custom defaults, and current platform behavior require qualification.
- Supabase documentation is mutable and describes general behavior, not the observed owner/default/ACL state of a particular source or destination. Hosted ordinary-`public` support remains blocked until exact disposable-project authorization and targeted qualification.

## Task 10 implementation constraints

1. Define an independently reviewed expected destination profile. Never copy observed source permissions and call them safe; source state may already expose data.
2. Observe owners, database/schema/object/column ACLs, grantors and grant options, PUBLIC versus named roles, role attributes/memberships, global and per-schema defaults, RLS/forced-RLS/policies, views, and security-definer routines as separate facts. Preserve NULL versus explicit ACL state. Translate local OIDs to names only within the live observation; do not persist OIDs as portable identities.
3. Do not read policy expression trees, routine bodies, passwords, or other secret-bearing values into ordinary diagnostics. If the supported recipe cannot faithfully capture/review a security-relevant fact, classify it unknown and block completeness rather than infer safety.
4. Refuse before mutation on an unexpected owner, grant, default, membership option, RLS state, or unsupported managed role. Do not auto-revoke, normalize, or strip ownership/ACLs to make a restore succeed.
5. Test the negative case where a global default `SELECT` grant to `PUBLIC` causes a newly created non-RLS table with synthetic data to be readable by an otherwise unauthorized role. A successful SQL/restore exit must not pass preflight when this exposure exists.

## Local evidence plan; not yet run for R05

- Use only the approved disposable PostgreSQL 17 loopback fixture and synthetic roles/data. Create separate owner, intended reader, and unauthorized reader roles.
- Reproduce the global default-grant exposure above and prove the unauthorized read succeeds before the preflight is added. Then test that preflight rejects before any target sentinel changes.
- Add focused local cases for NULL versus empty/current ACLs, column-only grants, `PUBLIC`, grant options/grantors, global versus per-schema defaults, PG17 membership flags, RLS enabled/forced and missing-policy behavior, owners/BYPASSRLS, and security-definer/view behavior. Do not treat this matrix as hosted Supabase qualification.
- No native client payload, hosted connection, or restore command is authorized by this record.

## Sources

### PostgreSQL 17

- [Privileges](https://www.postgresql.org/docs/17/ddl-priv.html)
- [`GRANT`](https://www.postgresql.org/docs/17/sql-grant.html)
- [`ALTER DEFAULT PRIVILEGES`](https://www.postgresql.org/docs/17/sql-alterdefaultprivileges.html)
- [`pg_default_acl`](https://www.postgresql.org/docs/17/catalog-pg-default-acl.html)
- [Role attributes](https://www.postgresql.org/docs/17/role-attributes.html)
- [`pg_authid`](https://www.postgresql.org/docs/17/catalog-pg-authid.html) and [`pg_auth_members`](https://www.postgresql.org/docs/17/catalog-pg-auth-members.html)
- [Row security policies](https://www.postgresql.org/docs/17/ddl-rowsecurity.html) and [`pg_policy`](https://www.postgresql.org/docs/17/catalog-pg-policy.html)

### Supabase

- [Securing your API](https://supabase.com/docs/guides/api/securing-your-api)
- [Row Level Security](https://supabase.com/docs/guides/database/postgres/row-level-security)
- [Postgres roles](https://supabase.com/docs/guides/database/postgres/roles)

Recheck mutable Supabase guidance before any hosted test or support claim. Public documentation and local tests do not authorize a hosted connection.
