# R05 — Destination permissions and ordinary `public` applications

**Status: open; public documentation reviewed, a local version-1 `PUBLIC SELECT` guard is implemented, and direct relation-grant detection now has an isolated local fixture. The full permission baseline and hosted preflight remain unqualified.** This record defines the hazards and local evidence gates for Task 10. It does not choose a universal Supabase ACL profile or claim that a matched catalog snapshot is safe.

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

## Current local guard (partial; not a complete baseline)

`CheckTargetSecurityV1` uses catalog facts gathered by `ObserveCatalog` inside its existing read-only, TLS-verified PostgreSQL 17 transaction. It refuses when a global or selected-schema table default, selected row-bearing object's ACL, or selected column ACL grants `SELECT` to `PUBLIC`; it also refuses if the required observation is missing or unqualified. It is deliberately conservative and may reject a grant that RLS would otherwise contain. A clean local PostgreSQL fixture passes, and the global-default exposure fixture is rejected.

`ObserveCatalog` now reports selected relation owner role names and bounded relation/column ACL facts (NULL versus explicit ACL state, grantee/grantor names, privileges, and grant options). This first guard does **not** compare these facts to an expected profile. It also does not evaluate grants to named Supabase roles such as `anon`, `authenticated`, or `service_role`; memberships, default ACLs beyond the narrow `PUBLIC SELECT` case, policy behavior, security-definer bodies, application collisions, target identity, or changes after observation. A pass means only “no observed `PUBLIC SELECT` in this narrow scope,” not “safe target,” “permission-equivalent,” or restore-ready. No restore command calls this guard.

## Task 10 implementation constraints

1. Define an independently reviewed expected destination profile. Never copy observed source permissions and call them safe; source state may already expose data.
2. Observe owners, database/schema/object/column ACLs, grantors and grant options, PUBLIC versus named roles, role attributes/memberships, global and per-schema defaults, RLS/forced-RLS/policies, views, and security-definer routines as separate facts. Preserve NULL versus explicit ACL state. Translate local OIDs to names only within the live observation; do not persist OIDs as portable identities.
3. Do not read policy expression trees, routine bodies, passwords, or other secret-bearing values into ordinary diagnostics. If the supported recipe cannot faithfully capture/review a security-relevant fact, classify it unknown and block completeness rather than infer safety.
4. Refuse before mutation on an unexpected owner, grant, default, membership option, RLS state, or unsupported managed role. Do not auto-revoke, normalize, or strip ownership/ACLs to make a restore succeed.
5. Test the negative case where a global default `SELECT` grant to `PUBLIC` causes a newly created non-RLS table with synthetic data to be readable by an otherwise unauthorized role. A successful SQL/restore exit must not pass preflight when this exposure exists.

## Local evidence and remaining plan

**Executed 2026-09-23:** `SPARC_TEST_PG_BIN=/opt/homebrew/opt/postgresql@17/bin go test -mod=readonly -tags=integration ./internal/database -run 'TestLocalFixtureReproducesGlobalDefaultPublicSelect|TestObserveCatalogOnDisposablePostgres' -count=1 -v` passed with PostgreSQL 17.9. The disposable TLS loopback fixture created a `NOLOGIN` owner with global default `SELECT TO PUBLIC`, then a non-RLS table and synthetic row. A separate ordinary login role, not a member of the owner role, read the canary. A second table has a column-only `PUBLIC SELECT` grant. The catalog check detected the default, resulting relation, and column grants; `CheckTargetSecurityV1` refused them. The clean fixture passed the narrow guard. This is local database evidence only; no actual SPARC restore or hosted project was used.

**Verification 2026-09-23:** `go test -mod=readonly ./... -count=1 -timeout=240s`, tagged PostgreSQL integration tests, serial database race tests, `go vet`, CLI build, Windows AMD64 and Darwin arm64 integration-test cross-compiles, formatting, and `git diff --check` passed. A new isolated PostgreSQL 17.9 case also passes:

```sh
SPARC_TEST_PG_BIN=/opt/homebrew/opt/postgresql@17/bin go test -mod=readonly -tags=integration ./internal/database -run '^TestObserveCatalogRejectsDirectPublicRelationSelect$' -count=1 -v
```

The direct-grant test grants `SELECT` on one public table to `PUBLIC`, observes only the object-level grant (no default or column grants), and confirms `CheckTargetSecurityV1` refuses. A separate ordinary login role reads the synthetic canary, reproducing the exposure. The ACL observation test distinguishes a NULL ACL from an explicit-empty ACL, and records a named grantee, grantor, privilege, and grant option plus a column-level `PUBLIC` grant. Another case transfers a synthetic relation to a `NOLOGIN` role and confirms `ObserveCatalog` reports the role name rather than a local OID; the guard does not compare that observation to an expected owner profile. This is read-only preflight evidence after fixture seeding; it does **not** prove refusal before an actual restore mutation because no product restore path calls the guard.

- Next, observe global/per-schema default ACLs, role attributes/membership options, and policy role/effect details; then define a reviewed expected profile rather than allowing from source state. Do not treat the current guard as a complete profile. Wire it to a restore gate only when a real restore pipeline exists; do not add a mock mutation wrapper.
- Continue local cases for owner/BYPASSRLS behavior, RLS enabled/forced and missing-policy semantics, and security-definer/view effects. Do not treat this matrix as hosted Supabase qualification.
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
