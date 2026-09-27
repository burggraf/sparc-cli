# Task 12 — read-only public/Auth inventory design

**Status:** offline implementation and disposable PostgreSQL 17 test passed; one separately authorized read-only hosted probe also passed. This file grants no further hosted access. Credentials and raw catalog rows were not stored.

## Goal and boundary

Prepare a bounded metadata-only observation of a specifically named PostgreSQL 17 project, limited to the `public` and `auth` schemas. It can identify schema shape, ownership, grants, RLS/policy metadata, and foreign-key links without reading application rows or Auth users. It is a discovery gate, **not** Task 12 qualification: it cannot prove password login, identity recovery, source-independent restore, Auth side effects, or compatibility with provider-managed schema behavior.

Use one caller-supplied project ref and one explicitly supplied database route. Do not discover projects through the account-wide `GET /v1/projects` endpoint. Do not connect to the previously inspected project merely because its ref or a private URL remains available. The session-pooler username convention binds the requested ref but does not independently attest backend identity; if the owner requires independent tenant attestation, stop because the present route does not provide it.

## Proposed read-only operations

The one approved probe used the bounded Go `ObservePublicAuthMetadata` path. It fixes selection to `["public", "auth"]` and runs the base catalog inspection plus the shape queries below in one read-only transaction. Any repeat or broader activity requires new approval:

- One PostgreSQL 17 connection through the qualified Supavisor session-pooler route only: port 5432 and exact `postgres.<expected-project-ref>` username. The hosted harness refuses direct and transaction-pooler routes. Use the explicit embedded Supabase CA with `sslmode=verify-full`; no ambient PostgreSQL configuration or credential fallback. Refuse wrong CA/hostname, wrong server major, missing TLS, or a transaction not confirmed read-only.
- One read-only transaction with the existing `search_path=pg_catalog`, 8-second statement timeout, 2-second lock timeout and 15-second observer deadline.
- Observe selected schema names/presence/owners; relation names/owners/kinds, RLS, policy/trigger counts and view-security flags; routine names/owners/identity signatures/kinds/languages plus security-definer and configuration-presence flags; policy names/commands/role identities and expression-presence booleans; selected object ACLs and selected/global default ACLs; database-wide extension names/versions/schemas; the narrow `PUBLIC SELECT` result flags; and bounded `pg_roles`/membership attributes. The current observer reads role/membership and extension metadata cluster-wide; this must be included explicitly in any approval.
- It does not read table rows, `auth.users` or `auth.identities` values, passwords/hashes, `pg_authid`, policy expressions, routine/trigger bodies, or user-provided SQL. It does not establish independent backend identity or a Supabase baseline.

The existing generic observer does not report column definitions or foreign-key edges. The new public/Auth observer adds closed, catalog-only queries for those facts. The statements below are design sketches; the fixed executable statements are in [`internal/database/public_auth.go`](../../internal/database/public_auth.go), bind only the exact schema array as `$1`, and bind the constant limit-plus-one (`10001`) as `$2`:

```sql
-- Relation and column shape only; $1 is exactly ARRAY['public','auth'].
SELECT n.nspname::text,
       c.relname::text,
       c.relkind::text,
       pg_catalog.pg_get_userbyid(c.relowner)::text,
       a.attnum,
       a.attname::text,
       pg_catalog.format_type(a.atttypid, a.atttypmod),
       a.attnotnull,
       a.attidentity::text,
       a.attgenerated::text
FROM pg_catalog.pg_namespace AS n
JOIN pg_catalog.pg_class AS c ON c.relnamespace = n.oid
LEFT JOIN pg_catalog.pg_attribute AS a
  ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE n.nspname::text = ANY ($1::text[])
  AND c.relkind IN ('r','p','v','m','f','S')
ORDER BY n.nspname COLLATE "C", c.relname COLLATE "C", a.attnum
LIMIT 10001;

-- Constraint shape and FK column links only; no expressions or row data.
SELECT source_ns.nspname::text,
       source.relname::text,
       constraint_row.conname::text,
       constraint_row.contype::text,
       constraint_row.convalidated,
       constraint_row.condeferrable,
       constraint_row.condeferred,
       target_ns.nspname::text,
       target.relname::text,
       key_pair.ordinality,
       source_column.attname::text,
       target_column.attname::text
FROM pg_catalog.pg_constraint AS constraint_row
JOIN pg_catalog.pg_class AS source ON source.oid = constraint_row.conrelid
JOIN pg_catalog.pg_namespace AS source_ns ON source_ns.oid = source.relnamespace
LEFT JOIN pg_catalog.pg_class AS target ON target.oid = constraint_row.confrelid
LEFT JOIN pg_catalog.pg_namespace AS target_ns ON target_ns.oid = target.relnamespace
LEFT JOIN LATERAL unnest(constraint_row.conkey, constraint_row.confkey)
     WITH ORDINALITY AS key_pair(source_attnum, target_attnum, ordinality) ON true
LEFT JOIN pg_catalog.pg_attribute AS source_column
  ON source_column.attrelid = source.oid AND source_column.attnum = key_pair.source_attnum
LEFT JOIN pg_catalog.pg_attribute AS target_column
  ON target_column.attrelid = target.oid AND target_column.attnum = key_pair.target_attnum
WHERE constraint_row.contype IN ('p','u','f','c','x')
  AND (source_ns.nspname::text = ANY ($1::text[])
       OR target_ns.nspname::text = ANY ($1::text[]))
ORDER BY source_ns.nspname COLLATE "C", source.relname COLLATE "C",
         constraint_row.conname COLLATE "C", key_pair.ordinality
LIMIT 10001;
```

The constraint query includes only edges with an endpoint in `public` or `auth`; if the other endpoint is elsewhere, it returns only that endpoint's names needed to describe the edge, not an inventory of that schema. Both query results are capped at 10,000 rows and fail closed on the 10,001st row, malformed metadata, query errors, timeout, missing schemas, or unexpected server/route state. `TestObservePublicAuthMetadataIsBoundedCatalogOnly` passed on a disposable TLS PostgreSQL 17 fixture: it verifies the required schemas, column/identity/FK/check metadata, read-only transaction, and absence of seeded row and expression canaries from the result. This is local evidence only. No raw rows are printed or persisted.

## Explicit exclusions

- No Management API request in this phase. In particular, exclude `GET /v1/projects/{ref}/config/auth`: its schema contains provider-secret-named fields, and this review did not establish whether returned values are redacted. Exclude account-wide project listing.
- No Auth Admin `listUsers` or other `/auth/v1/admin/*` route; these return user data and require privileged `service_role` access.
- No data-plane reads: no table-row `SELECT`, `COUNT(*)`, sampling, or export. The only planned `SELECT`s are the fixed `pg_catalog` metadata statements above. No login, signup, password reset, test-user creation, SMTP/provider call, Storage request, Vault/key request, write, restore, resource creation, or cleanup.
- No further use of hosted credentials, project refs, or private URL files without a new exact-scope approval; the single authorized probe is complete.

Official references: [Management API: get Auth config](https://supabase.com/docs/reference/api/v1-get-auth-service-config), [Auth Admin: list users](https://supabase.com/docs/reference/javascript/auth-admin-listusers), [Auth user management and schema boundary](https://supabase.com/docs/guides/auth/managing-user-data). These documents were consulted as public documentation only; no endpoint was called.

## Approval contract for future hosted probes

The one probe recorded in [`docs/progress.md`](../progress.md) used this scope under the owner's approval. Any future probe must be explicitly approved again, outside this repository:

```text
Source project ref: <exact ref>
Target project ref: none
Allowed action: one read-only PostgreSQL catalog observation using `ObservePublicAuthMetadata`
Allowed query scope: one Supavisor session-pooler connection, `public`/`auth` catalog metadata, cluster-wide roles/memberships/extensions, and the column/constraint queries above
Authorization scope marker: `readonly-public-auth-metadata-v1`
Management/Auth REST endpoints: none
Table rows, Auth users/identities, hashes, policy expressions, function bodies: forbidden
Mutations/resources/cost: none; $0
Execution window: <date/time and duration>
Credential source: owner-supplied private file only; never copy its value into chat/repository/logs
```

If any item is missing or a query would exceed this scope, do not connect. The hosted-tag test reads `SPARC_HOSTED_TEST_CONFIG` only when explicitly run; the private file must use the exact seven-field v1 contract below, with duplicate/unknown keys refused, a maximum 30-minute window and a zero-dollar cap. The connection URL/password remains in the separately referenced private file. Target fields are not accepted. An environment variable alone is not authorization.

```json
{
  "version": 1,
  "authorization_scope": "readonly-public-auth-metadata-v1",
  "source_project_ref": "<exact authorized project ref>",
  "connection_url_file": "/absolute/private/session-url-file",
  "not_before": "<RFC3339 start>",
  "expires_at": "<RFC3339 end, within 30 minutes>",
  "cost_cap_usd": 0
}
```

A pass only establishes the listed metadata observation on that project and date. Actual Task 12 still requires separate authorization for a disposable synthetic source/target, fixture writes, Auth password-login checks, restore, and cleanup; this read-only phase cannot be reported as Auth recovery qualification.

## Remaining Task 12 readiness gate

No hosted recovery or Auth behavior test is currently executable with the approved recovery path. `internal/tools.productionPayloads` is empty, so the normal CLI has no database clients and backup/restore remain unavailable. `RunCandidate` is integration-only and pins `PGHOSTADDR` to loopback; it cannot serve as a hosted runner. Do not treat the metadata probe as authorization to relax that boundary, select a production payload, or use a hosted dump/restore path. A reviewed hosted-test runner and separate client/payload approval must come first; only then is it useful to request exact disposable source/target refs and a new fixture/login/restore/cleanup scope.

## Stop conditions and evidence

Stop on non-PostgreSQL-17, unqualified route, failed TLS/read-only checks, unavailable schemas, missing privileges, unknown response/catalog shape, limits, timeout, or any request/response indicating access to user data. No retry or permission escalation. Record command, commit, versions, route class, result categories, omissions and exit status only after approval; keep raw metadata and credentials out of the repository.
