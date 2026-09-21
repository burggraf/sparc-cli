# Offline Database Feasibility Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Add a declaration-only database planner, not export, restore, tool discovery or project recoverability validation.

**Architecture:** A pure shared Rust function and a thin `sparc plan-database INPUT.json` command. Existing archive format and behavior remain unchanged; no runner or adapter framework.

**Tech Stack:** Rust stdlib and existing anyhow/serde/serde_json dependencies. No new dependencies.

**Workspace:** Directly on `main`, without worktrees, as requested by the project owner.

Exact files: this plan, `src/database.rs`, `src/lib.rs` (module export), `src/main.rs` (branch/help), `tests/database.rs`, `README.md`.

## Contract

- Shared `database::plan_database(&[u8]) -> Result<DatabasePlan>` plus `plan_database_file(&Path)` using the existing regular-file helper with redacted errors. Read at most 16 KiB + 1; reject files/JSON above 16 KiB, final-component symlinks and special files. Like the archive prototype, assumes a trusted local filesystem without concurrent path replacement; parent directory symlinks are not forbidden.
- Strict JSON v1: `version: 1`, required `connection_mode` (`direct`, `session`, `transaction`, `unknown`), optional/null `source_major`, `client_major`, `destination_major`. Numeric majors 10..=18 are the prototype's accepted declaration range, **not a support guarantee**. No free text, feature absence assertions, URLs, passwords, tool paths, SQL or arbitrary flags. Unknown fields fail without echoing values or parser chains.
- Deterministic JSON contains declared (not observed) inputs, fixed proposed artifact descriptions, provenance, blockers and unknowns. `offline_plan_only: true`, `execution_supported: false`, `export_ready: false`, `restore_verified: false` always. Valid JSON plans exit successfully even when blocked.
- Older client than source is a PostgreSQL restriction; newer client than source is blocked by SPARC's deliberately conservative matching-major policy. Destination below either declared source or client major is blocked. Missing/null declarations remain unknown. Transaction pooling is blocked; direct/session declarations do not verify connectivity, TLS or permissions.
- Every plan remains blocked on unimplemented execution and unproven Supabase-aware coverage/parity. Feature usage is always unknown. Artifact names are proposals, not created files or an executable recipe.

## Red / green checks

1. First add a CLI behavioral test: valid synthetic source 17/client 15 declarations must produce a blocked JSON plan instead of rejecting the unknown command. Observe failure before implementation.
2. Implement minimally, then test equal majors still blocked; older client; newer client/older destination; destination downgrade; missing/null versions; transaction/direct/session/unknown mode; deterministic artifact order.
3. Test malformed/unknown/credential fields, duplicate fields, invalid enum/version/major, exact 16 KiB acceptance and 16 KiB + 1 rejection, symlinks/nonregular input, redacted full library error chains and CLI errors. Run CLI with empty PATH and hostile synthetic credential environment; no tool or configuration inspection.
4. Run `cargo fmt --check`, `cargo test --locked`, `cargo clippy --locked --all-targets -- -D warnings`, and a synthetic CLI example. Existing archive tests stay unchanged. Record final counts and limitations in the implementation report.

## Retained research / future gates

Documentation/source analysis only; no live verification. Fixed reference: Supabase CLI **v2.117.0**, commit **21db855916f2c2b12f61cde923a27094b8528b23** (the annotated [tag](https://api.github.com/repos/supabase/cli/git/tags/cdcb8e4a8bc8d10658f194186d39739f43c04d1b)); [PostgreSQL 18 pg_dump](https://www.postgresql.org/docs/18/app-pgdump.html); [Supabase migration guide](https://supabase.com/docs/guides/platform/migrating-within-supabase/backup-restore).

- The tagged [dump handler](https://github.com/supabase/cli/blob/v2.117.0/apps/cli/src/commands/db/dump/dump.handler.ts) resolves connections before dry-run, which prints expanded `PGPASSWORD`. [Side effects](https://github.com/supabase/cli/blob/v2.117.0/apps/cli/src/commands/db/dump/SIDE_EFFECTS.md) include linked login-role creation and possible network-ban clearing. **Do not run dry-run as offline preflight.** It can inspect credentials and cause network/cache/telemetry work. Linked transaction-pooler fallback conflicts with SPARC policy.
- Docker-backed CLI version alone does not pin the PostgreSQL client: [image resolution](https://github.com/supabase/cli/blob/v2.117.0/apps/cli/src/command-internal/legacy-db-image.ts) uses configuration/cached pins. Eventual authorized tests need exact binaries/images/digests, source/destination versions and signed/notarized distribution evidence.
- [Environment builders](https://github.com/supabase/cli/blob/v2.117.0/apps/cli/src/command-internal/legacy-pg-dump.env.ts) distinguish default schema exclusions (including managed schemas) from data exclusions. Auth and Storage data are not generally excluded; managed migration tables and Vault/pgsodium data have gaps. The migration guide's `storage.buckets_vectors`/`storage.vector_indexes` exclusions are recipe additions, not universal CLI defaults. Specialty datasets remain unsupported.
- [Dump scripts](https://github.com/supabase/cli/blob/v2.117.0/apps/cli/src/command-internal/legacy-pg-dump.scripts.ts) rewrite SQL, roles, owners, grants, extensions and publications. Native tools are not proven parity. Roles omit passwords/reserved roles; grants/default privileges/RLS need positive and negative round trips. Never copy SQL rewrites or weakened `\\restrict` protection speculatively.
- File output may truncate/create with mode 0644; retried stdout may retain partial SQL. Future execution needs private bounded staging, no-clobber publication, credential channels and redacted diagnostics. No secret collection is implemented here.
- Separately prove managed-schema customizations, Auth identity/session/MFA compatibility, Storage bytes/ownership, migration history without replay, large objects, extension-owned data and Vault data/key metadata plus source root-key recovery while source remains active. SQL backups omit the root key; replacing a target key can strand ciphertext.
- Independent dump passes and Storage are not a shared snapshot. Require an approved quiet window or proven coordination. Restore SQL is executable; cron/webhooks/queues/hooks need tested inactive restoration and explicit activation. TLS, privileges, resource bounds, empty destination and recovery without source access remain live gates requiring authorization.

## Execution results — September 18, 2026

Implemented on `main`, without worktrees. The initial CLI test failed on the missing command before implementation. Fresh spec and correctness/security reviews were performed.

The correctness review found that derived Serde deserialization also accepted positional arrays and object-valued unit enums. Added library and CLI regressions, observed the library test fail, and fixed the shared parser to require an object with a string-valued connection mode. A subsequent typed parse of the original bounded bytes preserves duplicate-field rejection. The reviewer independently rechecked the fix and found no remaining actionable issue.

Final parent verification on macOS / Apple Silicon:

- `cargo fmt --check`: passed.
- `cargo test --locked --offline`: **19 passed**, with the external-tool test ignored by default.
- `cargo test --locked --offline --test age_interop -- --ignored`: **1 passed** (independent Go age interoperability).
- `cargo clippy --locked --offline --all-targets -- -D warnings`: passed.
- `cargo build --locked --offline`, executable `--help`, and `git diff --check`: passed.
- Synthetic CLI checks: matching majors produce a valid but blocked declaration-only plan; positional arrays, object-valued connection modes and duplicate nullable fields fail with empty stdout and a fixed redacted error.

No hosted projects, credentials, database connections or Docker services were used. No export/restore execution, Supabase compatibility, target-scale performance or project recoverability is claimed. Those Phase 0 gates remain open.
