# General application-database support: read-only discovery first

Status: owner selected general application-database support rather than Auth recovery. This design defines its first safety gate, not an executable general restore contract. Astra owns design/review; lower-model subagents implement. No hosted mutation is authorized by this document.

## Decision

Build observed, read-only application-schema inventory before general export/restore. The declaration-only Rust planner stays unchanged: declarations cannot become detected facts. The hard-coded v1/v2 rehearsal modules remain regression evidence, not production adapters.

Alternatives rejected:
- Substitute arbitrary names into the fixed fixture: its assertions, roles, data and allowed dependencies would cease to be meaningful; upstream EXTRA_FLAGS are shell-expanded and pg_dump schema arguments are patterns, not literal identifiers.
- Jump directly to arbitrary SQL restore: successful loading does not prove dependency completeness, equivalent permissions or inactive external effects.

The first delivery is a developer-only Python inspection helper using the existing protected native-psql transport. No new credentials/config store, dependencies, general SQL execution entrypoint, Rust planner changes or desktop connection UI.

## Scope and truthfulness

Input: independently validated existing connection config, private work directory, explicit nonempty list of 1–32 unique schema names. Accept actual PostgreSQL UTF-8 identifiers up to 63 bytes, including quoted/Unicode/punctuation names; reject NUL, invalid Unicode, duplicates and empty names. Treat names as DATA through a hex-encoded JSON literal, never interpolated identifiers, SQL patterns, shell words or command arguments. Validate all input before a database call.

Allow application `public` and user schemas. Reject explicit selection of pg_* or supabase_* namespaces, information_schema, auth, storage, realtime, vault, extensions, graphql, graphql_public, pgbouncer, pgsodium, pgsodium_masks, cron and net. This conservative exclusion list is not proof that all other namespaces are application-owned. Report extension membership and unsupported objects even in otherwise selectable schemas.

One bounded `BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY` transaction, fixed pg_catalog search_path, fixed catalog-only SELECTs and rollback. Native client/server major17 only for this milestone. Reuse validated TLS/passfile transport/timeouts/output limits; no fixture/provider baseline guard, user data queries, stored routine execution, EXPLAIN, sequence reads/nextval, COUNT of application rows, DDL, GRANT or role impersonation. SQL must not fetch role passwords, function bodies, default/policy expressions, migration statements, foreign-server options or other potential embedded secrets. Relation estimates are not verified row counts; omit row counts entirely for this delivery.

Missing selected schema or oversized/truncated/malformed/ambiguous output fails with a bounded redacted error. Never silently truncate to the first N catalog rows. Use counts/limits and strict response validation; malformed records are not converted to empty lists or unknown=false.

## Required observed inventory

Schema names and owners; relation names/kinds/owners/persistence/partition flag and enabled/forced RLS; columns with type schema/name, identity/generated flags and whether default or column ACL exists; constraints with type/validation/deferrability and foreign-key target schema; indexes with validity/readiness/uniqueness and expression/predicate flags; policies with command/permissiveness and role names (not expressions); noninternal triggers with enabled flag and target function schema; routines with signature identity from argument type OIDs or type names, language, owner, security-definer flag and whether a parsed SQL body exists (not its content); nonautomatic enum/domain/composite/range type inventory; extension-owned objects detected via pg_depend; selected-schema default-ACL presence.

Preserve ordinary structural catalog records needed to distinguish observations, without attempting to serialize a complete restoration model. Include explicit relation kinds rather than assuming everything is a table. Do not omit foreign tables, materialized views, partitions or user triggers from the inventory simply because this release cannot restore them.

Known findings at minimum: external-schema FK; extension-owned object; foreign table; partitioning; materialized view; unlogged relation; noninternal trigger; routine/security-definer/dynamic dependency review; custom/domain/range type review; column ACL; RLS policy review; default-privilege review; non-postgres ownership; invalid/unvalidated indexes/constraints; generated/default expressions requiring dependency review; unknown relation/object kinds. Findings are deterministic and retain private object references. They are review blockers, not claims that all such features are permanently unsupported.

Output always states `execution_supported=false`, `export_ready=false`, `restore_verified=false`, `dependency_analysis_complete=false`. It separately says the named structural catalog families were observed in one read-only transaction. Never label an inventory complete for the whole database or an archive supported because its finding list is empty. Unobserved role graph/ACL semantics, function-body/dynamic dependencies, types/extension behavior, external effects, grants/ownership/default policy mapping, data/snapshot/export verification, destination compatibility, Auth/Storage/managed services and scale remain explicit unknowns.

Optional persistence helper writes only to a NEW private output file using the existing no-clobber helper; it validates the parent directory is private, refuses symlinks and uses bounded JSON. No raw catalog contents printed to chat/logs. Captured names and metadata are private, even when they are not credentials.

## Acceptance

- Unit RED/GREEN tests for identifier injection/pattern traps, malformed input/output, duplicates, exact bounds, missing schemas, no database access on invalid inputs, deterministic findings and flags that never imply execution readiness.
- Disposable socket-only local PG17 test under sanitized environment. Create schemas with spaces/quotes/Unicode/pattern punctuation and demonstrate exact selection, not pattern matching. Inventory ordinary table/FK/type/view/sequence/RLS/default/index plus unsupported examples; bodies containing secret canary text must never appear in the output. Prove cross-schema FK finding and missing-schema refusal. The helper executes no user routines and no application data reads.
- Fresh Astra review and parent regression check. Existing v1/v2 tests stay unchanged and pass.
- Parent-only read-only inspection of existing authorized test projects may validate transport/catalog behavior; no source extension, destination cleanup, grants or service changes. Record observations as inventory evidence, never another recovery round trip.

## Later gates, not silently included

General capture needs an explicit supported dependency/role/ownership contract, literal schema selection, native tool provenance, consistent snapshot policy and private encrypted artifacts. Decide direct native custom-format pg_dump/pg_restore versus upstream-script adaptation after inventory exposes required objects; do not silently change the recipe or vendor shell rewrites. General restore needs destination baseline/emptiness checks, trusted SQL provenance, side-effect handling, permission equivalence and source-independent test cases beyond fixed fixture values. Obtain exact additional hosted mutation authorization for those experiments. Desktop integration follows a tested supported contract, not this inspector alone.

## Sources

- PostgreSQL17 pg_dump schema selection uses patterns and does not automatically include external dependencies: https://www.postgresql.org/docs/17/app-pgdump.html
- String-literal function-body dependencies are not fully tracked; SQL-standard parsed bodies improve but do not establish all runtime dependencies: https://www.postgresql.org/docs/17/ddl-depend.html
- Existing exact-fixture evidence: `2026-09-20-expanded-database-implementation.md`.
