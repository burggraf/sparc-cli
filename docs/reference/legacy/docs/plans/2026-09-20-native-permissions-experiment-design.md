# Native PostgreSQL permission-preservation experiment

Status: authorized local-only experiment; not a general exporter or support declaration.

## Purpose and decision

Following the capture-route review and owner approval to continue, test the most consequential native recovery question first: can a restore succeed while changing effective access? Use pinned Supabase CLI source as a compatibility reference, not as executable production policy. No additional owner choices are needed for this isolated experiment.

Use native PG17 custom-format pg_dump/pg_restore in disposable, socket-only local PostgreSQL. Keep all existing production modules, fixture modules, inspector flags and desktop code unchanged. A single opt-in standard-library unittest file and a short findings document are sufficient; no dependencies or reusable export framework.

## Fixed experimental contract

Create an isolated temporary cluster, source database, compatible target database and deliberately incompatible target database. All credentials/data/roles are synthetic. Source and targets can share the cluster for this first permission experiment; this deliberately does NOT prove independent cluster/global-role restoration or unavailable-source recovery.

Use a fixed ordinary application schema, a non-superuser application owner, restricted reader and unapproved principal roles. Precreate roles as explicit prerequisites. Capture may use the local bootstrap administrator so forced RLS does not hide rows; label this limitation. Restore objects under the actual restricted application owner, not bootstrap's effective superuser privileges. Grant only the database CREATE needed by the experiment. Do not use --no-owner, --no-acl, --disable-triggers, --clean or permissive error handling. Use a direct argument vector, one schema-and-data custom dump and fail-fast transactional pg_restore. Roles are not part of the archive.

The synthetic application should exercise table/column grants, a sequence, schema-scoped default privileges, forced RLS with a deterministic policy, and a simple routine with explicit EXECUTE privileges. A SECURITY DEFINER routine, if used to probe ownership, must have a fixed safe search_path and a narrow synthetic function. Keep the fixture small; no dynamic/general schema selection implementation.

## Required evidence

1. Compatible-target restore preserves canonical owner identities, table/column/sequence/routine grants, forced-RLS flags/policy and default privileges. Compare semantic catalog data, not database-local OIDs. Test actual access under non-superuser principals, including permitted and forbidden operations, RLS visibility and a newly created table's default grants.
2. The incompatible target has an extra GLOBAL default table privilege for the creating role, granting the unapproved principal access. Verify the default exists before restore. Run the same archive and observe whether pg_restore rejects it or succeeds with additional grants; determine results experimentally. Schema-scoped defaults do not neutralize global defaults. Demonstrate effective access on an appropriate non-RLS probe if RLS masks the table grant. Do not silently fix the target or call a successful process permission equivalence. Record exact observed behavior and the consequent preflight requirement.
3. A deliberate object collision/mid-restore SQL error fails and rolls back newly created objects with --single-transaction/--exit-on-error; independently verify the preexisting sentinel survives unchanged. No destructive retry.
4. Record client/server versions and assumptions. A runnable test asserts the observations, including any diagnosed privilege drift. Green tests that demonstrate unsafe defaults mean the hazard was reproduced, NOT that general restore is safe.

## Local safety and boundaries

Follow existing opt-in PG17 tests: SPARC_TEST_PG17_BIN must select /opt/homebrew/opt/postgresql@17/bin for execution here. Sanitize environment, private temporary HOME/work/socket paths, initdb with synthetic trust authentication only on the isolated private socket, TCP disabled, psql -X -w ON_ERROR_STOP. Bound subprocess duration and diagnostics. Own/reap local server and all subprocesses in finally blocks; do not reuse the known shell-pipeline timeout limitation. No shell execution or ambient credentials. No reading hosted/private configuration, no MCP/browser/hosted calls, no installs, Docker or telemetry.

No claim about hosted Supabase behavior, public schema ownership/defaults, arbitrary owners/dependencies, TLS handshakes, literal schema patterns, concurrent snapshots, archive encryption integration, role/history restoration or large-scale recovery. These remain later gates. In particular, public's provider-managed baseline cannot be inferred from a synthetic application schema.

## Implementation and acceptance

Terra implements tests/native_permissions_pg_test.py and documents actual results in docs/plans/2026-09-20-native-permissions-experiment-results.md. Existing files remain unchanged. Begin by expressing expected invariants and observing failures against incomplete fixture/restore steps; record honest RED/GREEN evidence without manufacturing claims about production code. This is an experimental integration test, not a new product API.

Run the isolated test with native PG17, followed by the existing Python regression suite where practical. Fresh Astra review checks permission semantics, privilege context, rollback assertions and local isolation. Parent reruns verification before accepting results. Parent alone owns commits/publication and any later hosted authorization request.

Next decision depends on evidence: specify a fail-closed destination privilege contract before designing general native export/restore. Never auto-revoke destination grants or default privileges merely to make a restore pass.
