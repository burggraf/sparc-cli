# Supabase-Aware Database Recovery Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task. Work directly on `main` under the existing owner authorization; do not delegate unless the owner explicitly requests it.

**Goal:** Restore a SPARC encrypted PostgreSQL database archive into a *fresh Supabase project* without replaying Supabase-managed schema definitions, and prove the declared database scope from the archive rather than from a live source.

**Architecture:** Keep the existing one-component archive as a legacy, incomplete input. First prove a guarded restore selection from its custom-format TOC; only then add a versioned split-capture profile (application schema, eligible data, custom roles/plan metadata). Use bounded encrypted streams and fixed PostgreSQL client operations. A restore must stop/rollback on errors; never adopt the developer-only `--continue-on-error` trial as the product.

**Tech Stack:** Go, current `internal/archive` + `internal/tools` bundled PostgreSQL 17.11/psql, pgx read-only observations, disposable PostgreSQL 17 fixtures, one explicitly authorized fresh Supabase target.

---

## Durable state and evidence (2026-09-28)

- Repo: `main`, pushed commit `a29af91`, clean when this plan was written. `./tmp/test` is the user-created encrypted archive; `.env.db` is the source URL, `.env.target` points to the **already modified** disposable target `wxqganvfxvpzqdyzmpmv`. These files are ignored/private. `.env.passphrase` was removed after testing. Never put credentials in chat, argv, logs, or Git.
- Archive: one full custom-format PostgreSQL dump, 1,081 TOC entries: `auth` 281, `public` 479, `storage` 118. `verify` passed encrypted integrity only; manifest says `incomplete`. It contains rows for all five Auth tables that the first restore missed. It lacks cluster globals/roles and non-database resources.
- Strict hosted `pg_restore -1 --exit-on-error` hit `CREATE SCHEMA auth` / already exists. Non-atomic continue-on-error imported data but returned failure (`permission denied`) and left a partial target. Five empty Auth tables were subsequently loaded with separate transactional data-only trials. One initial client-stream false negative was fixed: selected `pg_restore` closes stdin early; the runner now drains/authenticates the remaining verified input. **Neither trial constitutes a successful end-to-end restore.**
- Read-only comparison *after manual repair* with the **current live source**: all 25 `public` base tables / 620 rows and all 27 `auth` base tables have matching per-table row-content fingerprints and row counts; Storage table counts match, but `storage.migrations` content differs. The target has four additional Auth relations, a differing Storage ACL object, and source-only reserved role `cli_login_postgres`. `cron` and `net` transient counts differ. This is useful evidence, **not archive-time fidelity, complete schema equivalence, Auth service health, or project recovery**.
- In a READ ONLY transaction on the target, `SET LOCAL session_replication_role = replica` succeeded and `current_setting` returned `replica`. `has_parameter_privilege` had returned false, so do not use that catalog boolean as the sole capability test.
- Supabase CLI **v2.117.0** (the version installed here) source: [schema/data exclusion lists](https://github.com/supabase/cli/blob/v2.117.0/apps/cli-go/pkg/migration/dump.go), [schema script](https://github.com/supabase/cli/blob/v2.117.0/apps/cli-go/pkg/migration/scripts/dump_schema.sh), [data script](https://github.com/supabase/cli/blob/v2.117.0/apps/cli-go/pkg/migration/scripts/dump_data.sh), [role script](https://github.com/supabase/cli/blob/v2.117.0/apps/cli-go/pkg/migration/scripts/dump_role.sh), [migration guide](https://supabase.com/docs/guides/platform/migrating-within-supabase/backup-restore). Default schema dump excludes managed `auth`/`storage` and extension schemas. Default data dump includes Auth/Storage but excludes managed migration tables and several service/extension schemas; it emits `SET session_replication_role = replica`. Roles require a separate filtered dump. Explicit `--schema` changes the default exclusion behavior. Treat this as a version-pinned reference, **not** a claim that Supabase CLI alone is a complete project backup or that its `sed` rewrites are safe to copy wholesale.

## Safety and scope gates

- Do not reuse the modified target for a clean proof, delete it, create a billable replacement, or fetch/overwrite a project encryption root key without explicit owner approval. A fresh target is required for the final hosted gate. No hosted writes while designing/tests remain red.
- First deliverable is **database-only recovery**, not full Supabase recovery. Storage object bytes, Edge Functions, Auth settings/API keys, provider configuration, Vault/pgsodium root key, external integrations and service consistency remain separate components/manual requirements. Keep capture status `incomplete` until their coverage is actually qualified.
- No `--clean`, indiscriminate dropping of managed schemas, ambient PostgreSQL credentials, plaintext dump staging, permissive retry against a dirty project, or inference that row counts prove full fidelity. Preserve app ACL/RLS/owners where possible; do not blanket `--no-acl`/`--no-owner` without a reviewed scope decision.

## Tasks (TDD: failing check → minimum implementation → fresh check → commit)

### 1. Pin the database recovery profile

**Files:** Create `docs/research/R17-supabase-cli-dump-profile.md`; update `docs/coverage.md` and `docs/archive-format.md` only for facts established here.

Write the explicit table of **included schema DDL**, **included table data**, **excluded migration/service data**, **custom roles**, **managed-schema customizations**, and unknowns. Distinguish the legacy full dump from a new split archive; specify whether versioning uses a new encrypted restore-plan component or archive version 2 before changing parsers. Record why source-only `cli_login_postgres` is not a custom role to replay. Pin source URLs above and explain that `--schema` bypasses defaults. Keep the existing archive readable; do not reinterpret its manifest silently. Commit the contract alone.

### 2. Classify the existing archive offline; no target writes

**Files:** `internal/localdemo/toc.go` or a small new archive-plan module, `internal/archive/*_test.go`, `internal/database/recovery_test.go`.

Add tests using a synthetic custom dump with application objects, managed Auth/Storage objects, migration tables, quoted names, extensions, FK-dependent data, and unknown TOC entries. RED: a naive full-dump replay or text-token scan cannot produce an accepted plan. Implement a bounded TOC selection/explicit refusal that separates app schema from eligible data and flags managed-schema customizations/unknown entries for manual review. `pg_restore -L` may use a **private identifiers-only list file**, never plaintext rows/SQL on disk. Archive authentication/format checks precede any target connection. Run `go test -tags=localdemo ./internal/localdemo ./internal/database -count=1`; expect PASS and a count-only local plan for `./tmp/test`. Commit.

### 3. Prove one atomic schema + data execution path locally

**Files:** `internal/tools/run.go`, `internal/tools/run_test.go`, `internal/database/restore.go` (or a separate qualified restore file), `internal/database/recovery_integration_test.go`.

Test a target prepopulated with Supabase-shaped `auth`/`storage` schemas, Auth FK dependencies, and migration-history rows. Failing test must show full-dump conflict and that a late data error leaves **no changes**. Implement fixed, typed streams: application schema only; eligible data only; scoped `SET LOCAL session_replication_role = replica` in the **same transaction/session** as data loading, with `ON_ERROR_STOP` and no ignored errors. Bundled `psql` is available; do not assume `pg_restore -1` and a separate connection's `SET` share state. Preserve timeout, verified TLS, private passfile, process ownership and bounded diagnostics. Test real PG17 fixture success, rollback, input truncation, early child close, and side-effect-sensitive triggers. If the scoped setting is unavailable on a target, fail closed. Commit after integration/race/vet pass.

### 4. Add split encrypted capture only after Task 3 proves the recipe

**Files:** `internal/database/capture.go`, `internal/tools/run.go`, `internal/archive/manifest.go`/format tests if versioning is required, `internal/cli/backup.go`, `docs/archive-format.md`, `docs/coverage.md`.

RED tests: separate typed schema-only and data-only capture modes follow the pinned profile; managed schema DDL and managed migration data are absent; Auth/Storage **eligible** rows are present; failed capture publishes no archive; no secret reaches argv/logs. Stream each component straight into the encrypted archive. Add filtered custom-role capture only when a tested bundled `pg_dumpall` or equivalent qualified path exists; until then record role coverage as missing and keep `incomplete`. Capture and encrypt a machine-readable profile/selection manifest with source PG major, client version, filters, excluded items and unknowns. Do not claim a complete project. Commit in small steps.

### 5. Guard the fresh target and verify from the archive

**Files:** `internal/database/inspect.go`, `internal/database/security.go`, qualified restore code/tests, `internal/verify/*`, CLI tests.

RED tests: refuse same source/target, wrong ref or TLS route, unsupported PG/extension versions, dirty application schema, missing required custom roles, unresolved managed-schema customizations, unsupported objects, and unknown post-failure state. Observe target read-only immediately before write. Verify **archive-derived** per-table row counts/content fingerprints (not solely current live source), sequences, columns/constraints/indexes, functions, ACL/RLS and relevant Auth/Storage metadata after restore; record what cannot be checked. Do not mark success merely because SQL ran or the target matches today's live source. Only surface a production `sparc restore` once the qualified scope passes. Commit.

### 6. Clean hosted rehearsal and release gate

**Prerequisites:** owner explicitly authorizes fresh project creation/deletion and any charges, or supplies a newly created disposable ref plus a separate local URL file; supplies the archive passphrase **via local private file or terminal**, not chat. Current `.env.target` is dirty; `.env.passphrase` no longer exists.

First run the complete recipe against disposable **local** PG17 with managed baseline and exact rollback/fidelity checks. Then run **one** guarded hosted rehearsal against a newly created Supabase project, capturing fixed redacted errors locally; do not ignore failures or silently retry mutations. Validate database scope from archive, then separately test Auth service usability, Storage **object-byte** recovery/access, functions/settings/key dependencies if claiming project scope. If any component is unavailable, report `incomplete` with an exact checklist. Run fresh default/tagged Go tests, race and vet; check `git diff --check`, private files and `git status`, push verified commits to `main`.

## First action after context compaction

Read this plan and the four pinned Supabase CLI source files. Start **Task 1 only**: pin the profile and compatibility decisions; no hosted write, no secret request. The owner can then authorize implementation and a fresh target when the local recipe is green. No delegation unless explicitly requested.
