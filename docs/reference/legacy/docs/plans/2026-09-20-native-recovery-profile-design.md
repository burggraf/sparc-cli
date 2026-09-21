# Native application recovery profile — first composed local rehearsal

Status: design for a supervised local experiment, not a general restore interface. Astra designs/reviews; Terra implements, executes and publishes after acceptance. Existing production helpers and readiness flags stay unchanged.

## Decision

Combine the proven selectors, catalog observers, prerequisite comparison and encrypted archive engine in one end-to-end native custom-format rehearsal. Do not add another declaration-only planner or a general restore framework. The deliverable is a runnable opt-in integration test plus actual results, using a deliberately constrained synthetic application profile and two separate disposable clusters.

The experiment proves composition and source-independent restoration for that fixture. It does NOT convert the existing incomplete catalog observers into complete safety checks, certify arbitrary archives, or expose database operations in the desktop app.

## Profile v0: what this experiment permits

- Native PostgreSQL17 clients/source/target; all versions observed. No cross-major compatibility promise.
- One or two explicitly selected, non-public application schemas with literal-name handling (include a spaced/quoted/Unicode name). The source fixture is created by this test from reviewed SQL, not supplied by an arbitrary database owner.
- One non-superuser application owner and one restricted reader, with identical explicitly precreated role prerequisites on destination. Roles are NOT exported/restored by this profile; source capture may use the local bootstrap administrator, target restoration authenticates directly as the application owner.
- Ordinary logged tables, enum types, owned sequences/identity columns, simple PK/unique/FK/check constraints, ordinary indexes and explicit table/column/sequence/schema privileges, plus a schema-scoped reader default SELECT grant. Include an FK between the two selected schemas and representative Unicode/null/text/sequence values.
- No user routines, user triggers, views/materialized views, RLS, partitions/inheritance, foreign tables, extension-owned objects, custom operators/opclasses/collations, unlogged tables, migration-history or managed-service objects in the fixture. Internal FK triggers are expected structural artifacts, not user trigger support.
- Dependencies among the selected fixture objects and their directly referenced built-in prerequisites are known from the reviewed fixture. External application/protected dependencies, extensions and unresolved catalog references must prevent this test's restore path. Do not create a blanket system-schema allowlist: explicitly state which concrete fixture prerequisites are expected. If ordinary fixture internals produce unresolved direct references, stop and discuss rather than adding a permissive exception.
- Destination is an independently initialized, exclusively owned test cluster. Selected schemas must be absent, expected roles/database owner/default privileges must match independently specified prerequisites, and no user event triggers or unexpected database objects are allowed. The preexisting public namespace is preserved and checked unchanged.

This intentionally leaves `public` out of the first composed capture. Most real Supabase applications use public; supporting restoration into its provider-owned, already-present namespace needs a separate explicit compatibility/ownership/ACL contract. This experiment must not be advertised as general Supabase application recovery.

## Explicit refusals and limits

For the fixed profile, show at least a source external FK/reference refusal, a source unsupported user-routine or RLS refusal, and a destination extra GLOBAL default grant refusal BEFORE any dump restoration is invoked. A missing requested schema also fails via existing selection/inspection validation. Refusal is not a request to auto-drop, revoke, remap ownership, disable triggers, remove ACLs, expand schemas or retry. No `--no-owner`, `--no-acl`, `--disable-triggers`, `--clean` or permissive restore errors.

The fixture-specific checks must be named/documented as such. They do not constitute a reusable policy for arbitrary SQL expressions, operators, dependencies or object catalogs. All old observer readiness/dependency-complete flags remain false. If a small generic API seems necessary, ask first; prefer test-local composition over a premature production API.

## Capture, archive and trust

1. Seed source from fixed reviewed synthetic SQL. Record canonical source values, sequence state, object identities and semantic ACL/default-ACL expectations as explicit verification evidence. Do not compare database-local OIDs between clusters.
2. Invoke existing application_inventory and application_dependencies through a test-local transport adapter only, preserving their real SQL/validators/assessment. Do not mock the observed catalog contents or turn their readiness flags true. Confirm fixture-level source conditions separately against its explicitly reviewed expected shape.
3. Capture selected schema AND data with ONE native custom-format pg_dump invocation using native_schema_selection.pg_dump_schema_args. Do not adapt the upstream Bash/sed scripts. The source is quiescent and exclusively owned throughout inspection/capture; this is not a concurrent snapshot-coordination claim. Roles/sequence/auxiliary metadata concurrency remains outside this gate.
4. Stage the custom dump and small bounded profile metadata privately and no-clobber. Use the EXISTING SPARC pack/verify/unpack CLI and age key handling; no new crypto or archive format. Keep recovery key separate from the encrypted archive. Do not log raw dumps, keys or application values.
5. Keep an independently trusted test-side receipt/hash of the dump and profile metadata OUTSIDE the archive. After decrypt/unpack, require exact integrity/provenance correspondence to that receipt before accessing destination. An archive's self-contained hashes do not authenticate its producer. This is trusted locally produced artifact recovery, not import of arbitrary user archives.
6. Stop the SOURCE cluster completely using reviewed cluster-aware cleanup/confirmation before starting restoration. Do not retain a way for restore code to use the source connection; count/assert no source calls after shutdown. Do not claim global network isolation or hosted outage, but a stopped independent source is stronger evidence than two databases sharing one running cluster.

## Destination and restore

Destination's expected prerequisite contract must be independently specified from the fixture/setup contract, not copied from the destination just before acceptance. Observe through destination_permissions using its real SQL and strict comparison. Explicitly require absent selected schemas, matching named roles/memberships/database ownership, expected schema ACLs for any inspected prerequisites, and no altered defaults for the creating role before restore. Account for pg_database_owner implicit database ownership. Matching is one prerequisite, not an authorization shortcut.

Use separate fresh disposable destination clusters/instances for incompatible-state and rollback cases, or owned databases within the separate destination cluster where the role baseline remains explicit. Never clean/retry an already mutated target. Read-only refusal cases can be examined in-place without a restore attempt; a failed restore target is quarantined until test-owned cluster teardown.

Restore the trusted unpacked custom dump as the actual non-superuser application owner, using direct argv `pg_restore --single-transaction --exit-on-error`. A test-local one-attempt latch prevents automatic re-entry. Expected nonempty/mismatching destination is rejected before pg_restore; no writes to fix defaults/roles/schemas. A deliberate collision/error inside an authorized fresh failure-test target must fail transactionally, preserve its preexisting sentinel and leave no partially restored application schemas. The failure fixture may use a deliberately installed test-only event trigger as fault injection ONLY in a separate fixture-only branch. First prove the normal gate rejects that target for its event-trigger prerequisite and that no restore primitive was invoked. Never label it prerequisite-compatible. Under explicit authority limited to this newly owned synthetic failure fixture, the test may then call the same one-attempt, non-superuser transactional restore primitive directly to verify rollback and sentinel survival. That direct test call is fault-injection authority, not normal admission: no exported bypass API, skip-check flag, automatic retry or general exception is allowed. The normal gate remains unchanged; this test does not prove an unexpected trigger is safe or that a production restore may proceed after refusal.

## Verification, not just successful import

On the independent compatible target compare exact schema/table/type/sequence/constraint/index inventory appropriate to this fixed profile, row values and sequence last_value/is_called, named ownership, semantic table/column/sequence/schema/default ACLs, and expected FK behavior. Probe reader allow/deny operations and defaults for a newly created table. All comparisons use structured encodings, not psql line splitting or TOC text. Source shutdown precedes these checks; expected values/metadata come from the trusted pre-capture evidence.

Negative evidence also includes a damaged/mismatching unpacked dump or receipt refused before destination access (no restore command), source unsupported/dependency refusal, destination extra global default refusal, repeated attempt refusal and rollback. Keep test fixture small; not one giant parameterized generic recovery engine. Green tests mean this trusted constrained fixture recovered and those failures were detected, not that all application databases are supported.

## Execution boundaries and files

Terra may create tests/native_recovery_profile_pg_test.py and docs/plans/2026-09-20-native-recovery-profile-results.md only. No existing production/test modules or desktop edits. Reuse test-only cleanup_owned_cluster; explicit temporary ownership, fast/immediate verified shutdown, quarantine if not confirmed. Two socket roots must remain short enough for macOS paths. No TCP, ambient credentials, hosted config, browser/MCP, Docker, installation, global paths or real data. Only `/opt/homebrew/opt/postgresql@17/bin`, `SPARC_TEST_PG17_BIN`, `PYTHONDONTWRITEBYTECODE=1` and the existing local SPARC build/CLI are used. Building the existing Rust archive CLI is allowed if needed; no dependency changes or downloads deliberately introduced. Stop if tools are unavailable rather than substitute/install.

Use bounded commands and private unique retained test logs/exit receipts. Document post-collection output checks honestly; no new allocation/scale promises. No old Bash fixture pipelines. Do not stage private archives, keys, SQL, receipts or logs. Test cleanup must preserve potentially live state on failure rather than deleting it.

## Acceptance sequence

Astra independently reviews this design before implementation; a blocking design finding stops execution for parent resolution. Terra implements the small experiment and reports real failures/results, with focused and full opt-in suite runs. Fresh Astra source review then a separate Terra full-suite verification with retained full log, immediate exit receipt and before/after hashes. Publication only after parent accepts both reports, assigned to Terra under separately bounded exact-file authority.

After this experiment, decide which limitations to address for the first actual application profile—particularly provider-owned public, safe expression/object coverage, capture consistency, real transport and a reviewed trusted-archive interface. New hosted changes still require new precise owner authorization; all prior approvals are consumed.
