# sparc

A local-first Supabase project archive and restore assistant.

Archive as much of a project as supported, collect missing settings through guided forms, and restore into a new project with explicit coverage and verification reports.

## Status

Early feasibility prototype: a shared Rust library, developer CLI and local macOS desktop alpha can pack, verify and unpack local artifact folders using age encryption. The CLI also produces declaration-only offline database plans. **This is not a Supabase exporter/restorer or a production backup app. Do not use it as your only backup.**

See the [high-level product plan](docs/plans/2026-09-18-supabase-project-archive-design.md), [prototype implementation plan](docs/plans/2026-09-18-local-archive-prototype.md), and [experimental archive format and security limits](docs/archive-prototype.md).

## Direction

- **Desktop:** Tauri 2, macOS first.
- **UI:** React + TypeScript + Vite SPA with shadcn/ui.
- **Engine:** Shared Rust core with a thin CLI.
- **Initial validation target:** Databases up to 10 GB and stored files up to 100 GB; not yet benchmarked or verified.
- **Privacy:** Local-first execution and full archive encryption by default.
- **Safety:** Restore to new, empty projects; explicitly report missing or non-exportable material.

## Try the local prototype

Requires Rust 1.91 or later. No Docker, Node, Supabase account, or separate age executable is needed for these commands.

```sh
cargo build --locked
DEMO="$(mktemp -d)"
mkdir "$DEMO/source"
printf '%s\n' '-- synthetic SQL fixture; never executed' > "$DEMO/source/data.sql"
RECIPIENT="$(target/debug/sparc keygen "$DEMO/recovery.agekey")"
target/debug/sparc pack "$DEMO/source" "$DEMO/archive" "$RECIPIENT"
target/debug/sparc verify "$DEMO/archive" "$DEMO/recovery.agekey"
target/debug/sparc unpack "$DEMO/archive" "$DEMO/restored" "$DEMO/recovery.agekey"
```

The recovery key file is **unencrypted**: protect it separately. Output directories must not already exist. Files are restored locally, not to Supabase. An interrupted operation can leave partial output; resumable transfers are not implemented yet.

## Try the local macOS desktop alpha

The desktop alpha provides two guided flows over the same Rust engine: create and verify an encrypted archive, or verify and restore one locally. **Local artifact archives only — this does not back up Supabase yet.**

Development requires Node.js, npm, Rust and Xcode Command Line Tools:

```sh
cd desktop
npm ci
npm run tauri dev
```

Build a local `.app` for this Mac:

```sh
npm run tauri build -- --bundles app
```

The app is written to `desktop/src-tauri/target/release/bundle/macos/SPARC.app` when the command is run from the repository root, or `src-tauri/target/release/bundle/macos/SPARC.app` from `desktop/`. This developer build is ad-hoc signed for local execution; it is **not Developer ID signed or notarized** and is not intended for redistribution. Do not bypass macOS security warnings for an app obtained from an untrusted source.

The recovery key is an **unencrypted recovery key** stored separately from the archive. The UI keeps paths only in memory, never displays private-key contents, refuses existing destinations and reports that partial private output may remain after failure or interruption. It has no Supabase connection, history, cancellation, resumable operations, Keychain integration or telemetry.

## Try the offline database planner

```sh
PLAN_DEMO="$(mktemp -d)"
cat > "$PLAN_DEMO/declarations.json" <<'JSON'
{"version":1,"connection_mode":"direct","source_major":17,"client_major":17,"destination_major":17}
JSON
target/debug/sparc plan-database "$PLAN_DEMO/declarations.json"
```

This prints a deterministic JSON plan, not SQL or an executable recipe. It reads only the supplied non-secret JSON file; it does not inspect tools, environment credentials or project configuration, connect anywhere, invoke Supabase/PostgreSQL/Docker, create artifacts, or perform export/restore. **A successful exit means valid declarations, not readiness:** every plan has `execution_supported`, `export_ready` and `restore_verified` set to `false`, even with matching majors. Archive integrity is not project recoverability.

Input is strict JSON v1, at most 16 KiB, in a regular non-symlink file. Only the fields shown are allowed. `version` and `connection_mode` are required; mode is `direct`, `session`, `transaction` or `unknown`. Majors may be omitted/null (unknown), or integers 10..=18 (the prototype's accepted range, not a Supabase support guarantee). Do not put credentials, connection strings or real project data in this file. Errors do not echo input values or parser chains. Use stable trusted local paths: concurrent path replacement is not defended against; parent-directory symlinks are allowed.

An older dump client than source violates PostgreSQL's restriction; a newer client is blocked by SPARC's conservative matching-major policy. A destination below source or client is blocked, as is transaction pooling. Direct/session mode and all versions are **declared, never observed**. Feature use remains unknown, never inferred absent. Connectivity/TLS/permissions, Auth/Storage/extension compatibility, Vault keys, side-effect suppression and coherent snapshots remain unverified.

Shared API: `sparc::database::plan_database(&[u8])` and `plan_database_file(&Path)`. See the [bounded implementation plan and retained upstream hazards](docs/plans/2026-09-18-database-feasibility.md) for fixed source provenance, proposed artifacts, exclusions and future live-test gates. In particular, Supabase CLI dry-run is **not** a safe offline probe: it can resolve connections, cause side effects and print passwords.

## Developer-only hosted fixture experiment

An explicitly authorized operator can rehearse one synthetic, schema-scoped native PostgreSQL 17 fixture through the existing encrypted archive CLI. See the [bounded rehearsal plan, safety requirements, and parent-only commands](docs/plans/2026-09-18-hosted-database-rehearsal.md). This is **not** the Supabase dump recipe, native/CLI parity, full database recovery, or Auth recovery. Its Python fake-process tests are offline; no hosted operation runs by default. A [hosted round trip was verified](docs/plans/2026-09-18-hosted-database-rehearsal.md#observed-hosted-result) for this tiny fixture, with source-credential reads OS-denied during restore. That evidence does not extend to Auth, other Supabase services, or the target size envelope. A subsequent [pinned upstream-script/native-client rehearsal](docs/plans/2026-09-20-native-recipe-implementation.md#observed-result--2026-09-20) also passed for the same tiny schema, including live TLS rejection checks and source-input-denied restoration. This is not unmodified Supabase CLI/Docker parity or general database support. The subsequent [combined schema/roles/migration-history rehearsal](docs/plans/2026-09-20-expanded-database-implementation.md#combined-hosted-result--2026-09-20) also passed: additional schema dependencies, two restricted NOLOGIN roles, default privileges, and migration history restored as data without replaying statements. This remains a fixed synthetic fixture; arbitrary databases, managed services and scale are unproven. Desktop remains local-archive-only.

General application-database work now includes a [read-only schema inspector](docs/plans/2026-09-20-application-database-inspection-implementation.md#parent-verified-result--2026-09-20). It collects structural catalogs for explicitly selected application schemas and reports review blockers without reading application rows or routine bodies. Local PG17 tests and read-only inspection of both hosted test projects passed. This developer helper does **not** export or restore general databases; readiness flags remain false.

A [read-only PG17 direct catalog dependency observer](docs/plans/2026-09-20-application-dependency-inspection-results.md) covers named selected roots, internal/external/managed references, extension requirements, and unresolved local-only addresses. It is not dependency closure or a dynamic-SQL guarantee; execution, export, restore, and dependency-completeness readiness remain false.

The [native capture-route review](docs/plans/2026-09-20-database-capture-route-vetting.md) favors a bounded native PostgreSQL evaluation, not an approved general recovery engine. A [local permission-preservation experiment](docs/plans/2026-09-20-native-permissions-experiment-results.md) verified the fixed fixture's ownership/ACL/RLS behavior on a compatible target and reproduced a critical counterexample: a successful restore can retain unwanted destination default grants and expose restored data. The [destination permission preflight](docs/plans/2026-09-20-destination-permission-preflight-results.md) is a read-only PG17 prerequisite observer that compares an explicit expected catalog contract: database/schema ownership and ACLs, roles/memberships, and selected creators' global/scoped defaults. It detects mismatches without normalizing destination state; all readiness flags remain false. It is not hosted support or a general restore authorization. No automatic revocation or hosted changes are authorized by these experiments.

A [native PG17 literal-selector experiment](docs/plans/2026-09-20-native-literal-schema-selection-results.md) proved exact selectors for 19 tricky schema names with complete restored-inventory comparison and decoy exclusion; a missing selection fails. It does not establish dependency completeness or general recovery.

A subsequent [native recovery-profile design](docs/plans/2026-09-20-native-recovery-profile-design.md) and [verified local result](docs/plans/2026-09-20-native-recovery-profile-results.md) compose those observers and selectors with one native custom dump, encrypted archive round trip, independently initialized target, non-superuser transactional restore, exact inventory/ownership/ACL/default/FK checks, fail-closed cases, and late-DDL rollback. The independent PG17.9 suite passed all 79 tests. This proves only a trusted, quiescent, fixed non-public fixture; catalog coverage remains incomplete and it does not establish arbitrary archive authenticity, provider-owned `public` restoration, hosted behavior, concurrency, or scale.

## Checks

```sh
cargo fmt --check
cargo test --locked
cargo clippy --locked --all-targets -- -D warnings
# Optional independent interoperability check: requires the Go age CLI on PATH.
cargo test --locked --test age_interop -- --ignored
# Developer harness: Python 3 on POSIX, fake PostgreSQL, no live services.
cargo build --locked
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -p '*_test.py' -v
# Desktop frontend and native command layer.
(cd desktop && npm ci && npm test && npm run build)
cargo test --manifest-path desktop/src-tauri/Cargo.toml --locked
cargo clippy --manifest-path desktop/src-tauri/Cargo.toml --locked --all-targets -- -D warnings
```

Tests use synthetic temporary files, including restoration after removal of the source folder. They make no network calls. Passing them does not prove Supabase completeness, live recovery, or the target size envelope.

Keep real credentials, archives, and recovery keys outside this public repository. Live Supabase testing requires explicitly authorized disposable projects and a cost ceiling. Work directly on `main`, without worktrees, per the project owner's instruction.
