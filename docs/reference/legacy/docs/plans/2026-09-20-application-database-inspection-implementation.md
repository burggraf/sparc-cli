# Application Database Inspection Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Inspect arbitrary explicitly selected application schemas without reading application data, exporting SQL or modifying a database, and return honest structural inventory plus review blockers.

**Architecture:** One developer-only Python module reuses `hosted_rehearsal` configuration/transport/private-output primitives, but none of its fixture assertions. Catalog SQL is fixed; selected names are safely encoded data. Strict bounded response validation and a pure deterministic assessment keep observation separate from readiness.

**Tech Stack:** Python standard library/unittest, native PostgreSQL17, existing private native transport; no new dependencies.

---

Direct main, no worktrees, one writer. Implementation delegated to Terra/Sol; Astra designs/reviews. Exact contract is in `2026-09-20-application-database-inspection-design.md`.

## Task 1: Schema selection and fixed read-only query

Create `scripts/application_inventory.py`, `tests/application_inventory_test.py`.

1. RED tests: list-only, 1–32 unique strings, UTF-8 1–63 bytes, NUL/surrogate rejection, protected schemas, injection/quotes/wildcards treated literally. Invalid inputs must fail before any database/tool call.
2. Implement `inspection_sql(schemas)` with validated sorted names encoded as JSON hex in the fixed SELECT. Use catalog-only queries inside repeatable-read/read-only BEGIN, pg_catalog search_path, ROLLBACK. Require PG17 and exact selected-schema presence. Never call a user function or emit user input as an identifier/pattern.
3. GREEN. Inspect query to confirm no dynamic SQL, role/sequence/application data reads or mutations.

## Task 2: Bounded response validation and findings

Same files.

1. RED tests for duplicate JSON fields, invalid constants/encoding/depth, missing/extra keys, wrong scalar/collection types, schema/output mismatch, unknown relation kinds (retained with blocker), invalid flags, limits and redacted errors.
2. Implement strict validation for actual returned structural families. Maximum raw inventory2MiB, maximum10000 records per family; no silent truncation. Exact field sets/types and selected-schema membership must be validated. SQL and validator schemas must match.
3. Implement deterministic findings with private object references; always emit execution/export/restore flags false and dependency-analysis-incomplete. Keep mandatory unknowns even when no feature finding exists. Ordinary kinds are observations, not verified supported coverage.
4. GREEN with all findings listed in design, including column grants and cross-schema FK detection.

## Task 3: Protected collection and optional persistence

Same files.

1. RED: invalid selection/private work/output must fail before connection; harness transport used without ambient secrets; query result truncated/malformed fails; no overwrite/symlink output; errors never echo supplied values. No broad CLI entrypoint.
2. Implement `inspect(cfg, work, schemas)` returning validated assessment; config is already validated by parent. Use existing native version check and psql transport, no fixture guards. Optional `inspect_to_file(cfg, work, schemas, output)` verifies private parent/new destination before inspection and uses exclusive0600 write. Preserve bounded output; no stdout JSON.
3. GREEN and existing regression suite.

## Task 4: Real local catalog semantics

Create `tests/application_inventory_pg_test.py` (opt-in via existing SPARC_TEST_PG17_BIN pattern).

1. Disposable private socket-only cluster with sanitized environment and teardown; no hosted configs or connections. Set up arbitrary schema names, representative features and unsupported features; secret canary in a routine body must not be returned. Source application rows need not be read to inventory.
2. Test literal schema matching, owner/column/constraint/index/routine/trigger/type/policy/default-ACL/extension observations, exact managed-schema refusal, cross-schema FK findings, missing schema errors, role-free read-only execution and all readiness flags false.
3. Observe RED for missing behavior; implement minimal correction; GREEN. Maintain original v1/v2 tests unchanged.

## Task 5: Independent review and parent evidence

Fresh Astra review: review security boundaries, omission risks, patterns/injection, never-ready flags, output/privacy limits and meaningful tests. Parent fixes design issues through lower-model implementation where needed. Run all Python tests with local PG17 enabled, Rust tests/age/fmt/Clippy, staged private-artifact scan and diff checks. Parent may run read-only inspection of the retained synthetic projects; no hosted mutations. Add observed results distinguishing local/hosted inventory from export/recovery evidence, then commit/push and refresh handoff. General capture/restore remains a separately designed gate; no claimed product support from inventory alone.

## Offline implementation evidence (2026-09-20)

- Added developer-only `scripts/application_inventory.py`: `validate_schemas`, `inspection_sql`, `validate_response`, `assess`, `inspect`, and `inspect_to_file`. It emits one fixed catalog-only PG17 repeatable-read/read-only transaction, uses selected schema names solely as hex-encoded JSON data, and returns raw observed inventory separately from deterministic review findings.
- The validated response is bounded to 2 MiB and 10,000 records per family, rejects duplicate JSON keys/records, malformed types, invalid catalog constants, unexpected fields, and missing selected schemas. Output is only written through exclusive 0600 creation in an existing private directory; existing paths and symlinks are refused before transport.
- Unit RED evidence: `python3 -m unittest tests/application_inventory_test.py` initially failed because `application_inventory` did not exist; the later invalid-catalog-kind test failed before strict constant validation was added; the portable-OID truthfulness test failed before its false flag was added. GREEN: the same command reports 8 tests passing.
- Local-only PG17 evidence: `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/application_inventory_pg_test.py` passed against a disposable socket-only UTF-8 cluster. It exercised quoted/wildcard/Unicode literal selection, missing-schema refusal, FK outside selection, owner/column ACL/RLS/type/routine/trigger/materialized-view/unlogged/default-ACL observation, and confirmed a routine-body canary is absent. All readiness flags remained false.
- Regression evidence: `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest discover -s tests -p '*_test.py'` passed (46 tests). No hosted connection, credential/config/key/passfile access, browser/MCP operation, export, restore, data read, or mutation was performed.

## Review-correction evidence (2026-09-20)

- Astra review found server-version, identity, extension-membership, ownership-finding, and acceptance-fixture gaps. `application_inventory.py` now includes `server_version_num` from `current_setting('server_version_num')` in the same read-only catalog result and rejects any server outside PostgreSQL 17.x.
- Response validation now rejects conflicting catalog identities rather than only byte-identical records. Constraint identity includes its relation or domain; routine identity includes kind and argument OID vector; extension identity includes built-in `pg_identify_object` kind/schema/identity. The extension query maps `pg_identify_object`'s display schema safely back to exact selected namespace data and includes the schema-object null-schema case. It intentionally reports unidentifiable extension namespace membership as an explicit unknown rather than treating it as absent.
- Findings now use structured object references, distinguish security-definer routines, and cover non-`postgres` schema, relation, routine, and type owners. PG17-only generated-column constants are enforced.
- Review RED/GREEN evidence: server-version/conflicting-identity and security-definer/owner unit tests failed before their fixes. The expanded local fixture initially failed on real OID-vector JSON representation and quoted namespace display output from `pg_identify_object`; those observations produced minimal SQL fixes. GREEN: `python3 -m unittest tests/application_inventory_test.py` reports 14 tests; `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/application_inventory_pg_test.py` reports one real socket-only PG17 test.
- The local PG17 fixture now observes hstore extension operator/operator-class/operator-family/type/function membership via `pg_identify_object`, overload-safe identities, duplicate-named domain constraints, a partitioned relation, a foreign table, and an empty custom-owned selected schema. Unit tests additionally exercise schema-count/UTF-8/raw-byte limits, malformed UTF-8/depth, nonprivate and symlink work/output paths, redacted transport failure, conflicting identities, and schema/collation extension member shapes.

## Re-review edge-case correction evidence (2026-09-20)

- The previous display-schema join was not canonical when two selected raw schemas could match the same quoted display form. Extension membership now compares only `pg_identify_object.schema = quote_ident(raw_namespace_name)` for normal namespace-bearing objects. A separate OID-based branch handles extension-owned namespace objects whose `schema` field is null; it does not compare a raw namespace name to a display string.
- Family identity now follows PG17 catalog uniqueness: routine identity is schema/name/argument-OID-vector (not routine kind), and index identity is schema/name (not parent relation). OID vectors are emitted through `bigint` and validation accepts the full unsigned OID range 0–4294967295.
- RED/GREEN: the new routine-kind/index-parent conflict unit test failed against the previous identity keys; after correction, `python3 -m unittest tests/application_inventory_test.py` passed 15 tests. The local fixture uses `SELECT '4294967295'::oid::bigint` to observe the unsigned conversion without catalog growth.
- The real disposable PG17 fixture creates schemas `a b` and `"a b"`, marks `a b` as an hstore extension-owned schema using `ALTER EXTENSION hstore ADD SCHEMA`, and inventories both together and each independently. It verifies schema-object membership is attributed only to `a b`, exercising the null-schema/OID branch and preventing quoted-display misattribution. `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/application_inventory_pg_test.py` passed one test; full local-PG17-enabled Python discovery passed 53 tests.

## Quoted extension-member regression evidence (2026-09-20)

- The prior fixture proved only the namespace-object OID case. It did not contain an ordinary extension member in `a b`, so the old mixed raw/display join could still have passed. The fixture now creates `"a b".extension_probe()` and adds it to hstore using `ALTER EXTENSION hstore ADD FUNCTION "a b".extension_probe()`.
- Correct production SQL observes that exact function identity only with raw schema `a b`, both when selected alone and alongside the distinct raw schema `"a b"`; selecting only the literal-quote schema observes no probe member. The namespace-object OID assertion remains in the same fixture.
- The test temporarily patches only its local `inspection_sql` call to the previous mixed join (`x.schema=raw_name OR x.schema=quote_ident(raw_name)`) without editing production code. That deliberately misattributes the probe to the literal-quote schema for both combined and independent selection, proving this regression would fail under the previous query. The final production query passes. Fresh local PG17 test and full local-PG17-enabled discovery both passed; discovery remains 53 tests because this is additional coverage within the existing PG test.

## Parent-verified result — 2026-09-20

Terra implemented and corrected the inspector; Astra designed and independently reviewed it. Review caught server-version, catalog identity, extension-membership and boundary-test gaps; retained review cleared production corrections, and the parent verified the final quoted-display regression described above. No review claim substitutes for execution evidence.

Parent verification passed **53 Python tests**, including all three opt-in disposable PostgreSQL tests, plus root Rust tests, explicit independent age interoperability, rustfmt, Clippy and diff hygiene. Existing archive/planner/rehearsal modules and tests were unchanged.

A parent-only read-only inspection of `public` and the retained synthetic application schema succeeded on **both hosted PostgreSQL 17.6 test projects**, using native PG17.9 and the production scoped passfile/verify-full transport. No schema/data/role/service mutations were performed. Reserved-role/settings/membership/default-ACL baseline checks passed before and after. Reports were written to new private files, not stdout or Git.

Each report observed 2 schemas, 11 relations, 21 column catalog records (including index/sequence attributes, not merely user table columns), 6 constraints, 5 indexes, 1 policy, 1 routine, 1 standalone type and 7 schema-scoped default-ACL records. The selected schemas had zero noninternal triggers and zero directly identified extension members; these are scoped observations, not whole-project absence claims. Fourteen findings flagged routine/type/policy/default-expression/default-privilege/ownership review. In particular, provider-owned `public` and its defaults are observed, not assumed empty or postgres-owned.

All export/execution/restore-readiness flags remain **false**, and dependency analysis remains explicitly incomplete. This establishes structural read-only inspection, not application-database capture, recovery, compatibility of arbitrary features, or a coherent data snapshot. Namespace-unidentifiable extension dependencies and other mandatory unknowns remain visible.

Next gate: design the supported application capture/restore profile using these observations. Do not substitute variable names into the synthetic assertions, copy provider defaults blindly, treat fixture acceptance as general permission equivalence, or silently change the upstream/native capture recipe. No new hosted mutation authorization is implied.
