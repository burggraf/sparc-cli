# Combined Database Rehearsal Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Prepare and verify one bounded synthetic rehearsal covering application schema dependencies, custom roles/default privileges and migration-history preservation, without treating history statements as executable migrations.

**Architecture:** Preserve the proven v1 fixture and adapter unchanged. Add a small developer-only metadata contract for two fixed NOLOGIN roles and migration rows, with offline validation before any SQL generation. Then prepare a fixed v2 fixture and parent-owned capture/restore procedure; no generic role-dump parser or desktop connection UI.

**Tech Stack:** Python standard library/unittest, native PostgreSQL 17, pinned upstream schema/data scripts, SPARC age archives.

---

Work on main, no worktrees, one writer. This plan is not hosted mutation authorization. Deliver all three coverage areas together, with separate acceptance checks.

## Task 1: Strict role/history contract (offline)

Create `scripts/expanded_metadata.py` and `tests/expanded_metadata_test.py`.

1. Add failing tests for an exact versioned JSON contract containing roles, memberships and history. Reject duplicate/unknown fields, malformed UTF-8, invalid JSON constants, booleans as versions, oversize input, unexpected role attributes, reserved roles, non-allowlisted membership edges, duplicate migration versions, NUL/surrogate text and malformed arrays.
2. Support exactly `sparc_rehearsal_reader` and `sparc_rehearsal_member`, NOLOGIN, no superuser/createdb/createrole/replication/bypassrls; INHERIT. Only reader -> member membership, no admin option; membership inherit/set flags explicit. No passwords or arbitrary settings.
3. Preserve migration `version`, nullable `name`, nullable `statements` array and nullable elements exactly. Versions are bounded decimal strings; rows must be unique and sorted. Fixed table shape is version text PK, name text nullable, statements text[] nullable. Seed-file history is not covered and must be absent for this fixture.
4. Run `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -p 'expanded_metadata_test.py' -v`; observe RED before implementation, then GREEN. Validation errors must be bounded and must not echo archive contents.

## Task 2: Non-replaying history SQL and role refusal (offline)

Same files.

1. Add failing tests proving generated SQL is derived only from validated metadata, role collisions are refused before CREATE, no reserved roles are altered, and migration statements appear only in safely encoded data.
2. Use PostgreSQL `decode(hex, 'hex')` + `convert_from(..., 'UTF8')` for a validated JSON payload and `jsonb_array_elements` to insert nullable values, preserving array element order. Do not interpolate untrusted identifiers or emit the archived statements as SQL. Explicitly preserve NULL array versus empty array.
3. Generated fragments must be transaction-only (caller owns BEGIN/COMMIT), set safe transaction-local string handling where needed, refuse an existing migration schema rather than merging history, and create only the pinned three-column history table.
4. Test a failing SQL statement, psql meta-command text, quotes, backslashes, Unicode and newline as inert history values. Textual checks prove generation boundaries only; actual PostgreSQL proof remains pending.

## Task 3: Exact live-scope proposal (no live writes)

Document fixed additions to existing `sparc_rehearsal`: enum `note_state` (draft/published), table `recovery_cases` (id PK, state enum, nonempty label), partial index `recovery_cases_published`, security-invoker SQL function `label_length(text)`, security-invoker view `published_cases`. All owned by postgres; this milestone tests custom-role grants/membership rather than custom object-owner recovery.

Add the two NOLOGIN roles above with only their mutual membership; schema USAGE and table/view SELECT to reader. No explicit membership in authenticated, anon, service_role or provider roles. PostgreSQL may automatically grant the creating postgres role administrative membership in newly created roles; capture/check these creator edges separately and do not grant privileged roles to either fixture role. No existing provider-role attributes/settings may change. Set schema-scoped default SELECT grants for future postgres-created tables to reader; deny member writes and role administration.

Create synthetic `supabase_migrations.schema_migrations` with exact pinned column types and three records: NULL name/NULL statements; empty name/empty statements; Unicode/quote/backslash name and an ordered array containing an intentionally failing SQL statement plus a NULL element. History is inserted as data, not applied. Reject any pre-existing migration schema/history or matching custom role before seeding.

Before writing operational source seed or destructive destination preparation helpers, obtain explicit permission for these source additions and destination fixture-only cleanup plus role/history restoration. Keep both projects; no project reset/deletion, provider role changes, other service writes or charges. Preserve old verified archives and stop on unexpected dependencies. Destination cleanup uses explicit RESTRICT drops and one-shot fencing; permission does not imply automatic retry.

## Task 4: Combined capture/restore after scope approval

Read all callers before changing shared helpers. Keep old v1 tests intact; use fixed v2 assertions, not permissive switches. Capture exact role attributes/membership, schema-scoped default ACL and migration rows into private bounded metadata; validate it before archive pack. Use pinned schema/data scripts for the application schema only. Name the new recipe/inventory distinctly from v1; history/roles are not upstream role-script parity.

Restore order: verify archive and exact inventory/hashes -> validate metadata and target baseline -> transaction -> role definitions -> schema/data SQL -> restore ordinary replication role/timeouts -> history data -> exact catalog/ACL/default-privilege/behavior checks -> commit -> independent check. Check role collisions and history conflicts before any mutation. Fixed custom roles do not get privileged memberships or credentials. Any failure quarantines target; no automatic cleanup/retry.

Capture uses a quiescent synthetic source; separate script calls do not prove concurrent snapshot consistency. Client TLS remains verify-full; source credential/config paths are OS-denied during destination-only restore. Preserve old evidence and add a new single-attempt fence.

## Local verification extension

`tests/expanded_metadata_pg_test.py` is opt-in via `SPARC_TEST_PG17_BIN`. It creates its own private temporary PG17 cluster, listens only on a Unix socket, uses a sanitized child environment, and terminates/removes the cluster on completion. It must never accept a hosted URL or existing data directory. Test generated fragments as a non-superuser CREATEROLE database owner, verify exact role attributes/membership/history, prove the failing statement is inert, and reject role/history collisions. This tests local SQL semantics, not Supabase provider restrictions or a full archive round trip.

## Task 5: Verification and publication

Run all Python tests plus root Rust/age regression checks; inspect staged files for private artifacts; commit only verified code/docs. Label offline contract validation separately from live PostgreSQL and hosted recovery evidence. After hosted approval/execution, publish exact row/type/function/view/index results, effective role permissions, default-privilege behavior, inert migration-statement trap, and unmodified reserved-role/service baselines. Do not claim full database, custom ownership, seed-history, Auth, Storage, scale or CLI/Docker parity.

## Tasks 3–4: offline/local implementation evidence

Added `scripts/expanded_rehearsal.py`; v1 helpers and tests remain unchanged. This
is code generation/local verification only, **not hosted execution or proof**.

Fixed v2 recipe: `supabase-2.117.0-native-pg17-fixture-v2`. Encrypted inventory is
`schema.sql`, `data.sql`, `expanded.json`, `catalog.json`, `recovery.json` (manifest
plus five encrypted artifacts). Schema/data use the existing pinned upstream
scripts. Metadata is actual catalog/history capture, validated against the exact
three-row fixture; the supplementary catalog receipt records definitions, ACLs,
default privileges and creator edges after fixed assertions, not a learned
provider baseline. Restore verifies encryption, inventory, hashes and metadata
before target access, checks empty/collision guards before mutation, and compares
the receipt inside the transaction and independently afterward.

Runnable parent-only APIs (no live command entrypoint):

- `source_extension_sql()` returns one transaction extending only the exact old
  fixture. New rows are `(1, draft, draft)` and `(2, published, 雪)`. Source probes
  exercise only added objects; old sequence/default probes are destination-only.
- `readonly_sql()` / `check(cfg, work)` assert the expanded fixture without writes.
  `check(cfg, work, expanded=False)` checks the empty destination plus role/history
  collisions. `capture_metadata(cfg, work)` and `capture_catalog(cfg, work)` are
  read-only and return bounded JSON bytes.
- `capture(cfg, work, scripts, archive, recipient, identity)` captures pinned
  native dumps into a new archive and independently verifies it using the identity.
- `restore(cfg, work, archive, identity, fence)` accepts destination configuration
  only. It exclusively creates the persistent attempt fence immediately before
  executing the transaction. A failure consumes that attempt; no cleanup/retry.

PostgreSQL 17 creates canonical administrative edges from each fixture role to
`postgres`, granted by `supabase_admin`, with ADMIN=true, INHERIT=false, SET=false.
The fixed assertions require exactly those two edges plus reader→member granted
by postgres. A non-superuser creator cannot SET ROLE using an ADMIN-only edge.
The approved bounded probe handling therefore creates a temporary **fourth**
edge (member→postgres, grantor postgres, ADMIN=false, INHERIT=false, SET=true)
only inside source-extension/restore transactions. It never changes the
supabase_admin-granted edge; it resets role and revokes only the postgres-granted
edge with RESTRICT, then requires the canonical three-edge state before commit.
Read-only capture instead checks actual ACLs/effective named-role permissions;
actual member execution occurs only in those authorized mutation transactions.
Local injected failures demonstrate complete rollback, including the temporary
edge. No privileged provider role is granted to either fixture role.

TDD evidence: the new offline test initially failed with missing
`expanded_rehearsal`; the first PG17 semantic run failed at forbidden SET ROLE,
leading to the explicitly approved temporary-edge handling above. Final checks:
37 Python tests passed with both opt-in PG17 tests enabled; root Rust tests,
explicit independent age interoperability, rustfmt and Clippy passed. New local
test performs real pinned-script schema/data dumps, encrypted pack/verify/unpack,
restore, exact history/receipt comparison, old RLS/FK/sequence checks, default-ACL
behavior, denied writes/administration and invoker-view checks. Negative mutations
cover enum labels, function body, index/predicate, invoker option, owner/table
ACLs, role flags/membership, RLS policy, history, seed_files and default privileges.

Reproduce all Python checks:

```sh
SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin PYTHONDONTWRITEBYTECODE=1 \
  python3 -m unittest discover -s tests -p '*_test.py' -v
```

The test cluster is temporary, socket-only, sanitized and terminated on teardown.
Stock PG cannot emulate managed Supabase extensions/services: **only test-local
provider guard calls are mocked**, while production guards retain the exact v1
managed baseline plus the two fixed fixture namespaces. The local roundtrip
reuses the disposable database after explicit RESTRICT cleanup; it is not hosted
source/destination isolation evidence.

Required parent safety checks before any hosted mutation: validate configs and
project identities; verify/preserve old archives; independently compare reserved
role/service catalogs and unrelated memberships against the already approved
baseline; obtain/retain exact authorization and a separate source/cleanup fence;
review generated SQL; keep source quiescent; use private work/fence directories,
scoped credentials and verify-full; deny source credential/config paths during
destination-only restore; perform destination cleanup separately with the exact
v1 RESTRICT procedure; verify final receipts and canonical creator edges. Provider
drift, unexpected dependencies, failed verification or a consumed fence means
stop/quarantine, not a fresh baseline, expanded cleanup or automatic retry.

## Independent review correction

The fresh safety reviewer found a P1: table ACL checks alone missed column grants to PUBLIC. The parent reproduced actual member UPDATE through a column grant and observed three failing rejection cases (new table UPDATE column, old notes SELECT column, migration-history SELECT column). The minimal correction rejects nonempty `pg_attribute.attacl` throughout both fixture schemas. Full regression then passed and the retained reviewer cleared the finding with no further material blocker. This demonstrates why table-level privilege checks are insufficient; no permissive baseline adjustment was used.

## Combined hosted result — 2026-09-20

The exact owner-authorized combined rehearsal **passed** on hosted PostgreSQL 17.6 using native PostgreSQL 17.9 and the pinned schema/data scripts. This is a fixed synthetic fixture, not unmodified CLI/Docker or full database recovery.

1. Read-only preflight verified both original fixtures, absence of the proposed roles/history schema, strict provider baseline and full reserved-role/membership/settings/default-ACL receipts. Both prior encrypted recovery archives verified before hosted mutations.
2. The source extension committed once, adding only the named schema objects, two NOLOGIN roles, restricted grants/default privileges and three history rows. Old fixture values/sequence and pre-existing role/settings/membership/default-ACL state remained unchanged. Source probes touched newly added objects only.
3. Read-only capture produced five encrypted artifacts (`schema.sql`, `data.sql`, `expanded.json`, `catalog.json`, `recovery.json`): **14,814 plaintext bytes**, **six ciphertext files** including the manifest. Independent verify and unpack passed. Parent-only receipts pinned exact code/artifact digests before destination preparation.
4. Destination preparation transactionally removed only the old `sparc_rehearsal` fixture using explicit RESTRICT drops and a one-shot fence. Neither project was deleted or reset. Both old archives and the new archive verified first.
5. Destination-only restore ran with known source password/passfile/config paths OS-denied. A separate restore fence prevents replay. Inside the transaction, role creation, schema/data, inert history insertion, exact assertions, behavior probes and the independent pre-existing-role baseline comparison all passed before commit.
6. A fresh read-only verification process compared actual metadata/catalog receipts with the decrypted archive. It confirmed owners=2, notes=2, empty_table=0, recovery_cases=2, published_cases=1, three exact history rows, two fixture roles and exactly three canonical membership edges. Original exact values, forced RLS visibility, FK/default behavior and sequence **42 / is_called=true** passed. Reserved roles/settings and unrelated memberships/default ACLs matched their preflight receipts.
7. Enum/check/index/function/view assertions, denied member writes/administration, invoker-view behavior and future-table default SELECT privileges passed. The transient postgres-granted SET-only probe edge was removed before commit. History preserved NULL versus empty arrays/names, Unicode, quotes, ordered statements and NULL array elements; intentionally failing SQL and psql-command text were never executed.
8. Auth users, Storage buckets, Vault secrets, public relations and publication tables remained zero. An independent read-only guard test confirmed the populated destination is refused with the expected fixture-baseline exception; no second restore was attempted.

Captured SQL SHA-256:
- schema: `0968115a8aa89be45892b883232c609aee88c6d2b8e74c8341a19f9dcf78006e`
- data: `03c8867ab94dc0b40df13f25e8ebf9a7339eb177502b7230808d8f8d529b2189`

Parent verification after the review fix: **37 Python tests passed**, including two disposable local PG17 tests, plus root Rust tests, explicit independent age interoperability, rustfmt, Clippy and diff hygiene. Live evidence is separate from mock/local test results. All private credentials, identities, archives, operator SQL/helpers, baseline receipts and attempt fences remain outside tracked files.

Both Free projects are retained with the expanded fixture; the source extension and destination cleanup/reuse authorization has been consumed. No further mutation/retry/cleanup is implied. Source remained online; denied local credential paths are not a source outage or network-isolation test. Separate dump calls require a quiescent fixture and do not prove concurrent snapshot consistency. Custom object ownership, arbitrary role graphs, seed-file history, existing/conflicting migration-history reconciliation, populated Auth/managed services, Storage bytes, functions deployment/configuration/secrets, scale and full Supabase recovery remain unproven. Desktop remains local-archive-only.
