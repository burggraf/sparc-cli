# Historical research and executable evidence

**Reference only. Do not run these programs or operational commands as setup for the new project.**

This directory preserves the prior SPARC Rust/Python project's research, positive results and counterexamples so the new Go implementation can reuse test ideas without relying on the old checkout or conversation.

## Provenance and contents

[PROVENANCE.json](PROVENANCE.json) identifies original commit `858f3643fc08183c269defe1e7b8e638a1aa5e54`, each source path, byte length and SHA-256. All 60 copied files were compared byte-for-byte with the tracked working-tree files at handoff preparation. Hashes are correspondence evidence, not a signature or permission to run archived code.

Included under `legacy/`:

- Every previously tracked Markdown document under `docs/` (24 documents, including superseded desktop plans).
- Original root [README](legacy/README.md), `.gitignore`, Cargo manifests/lockfile.
- Original `src/` Rust archive/CLI/planner implementation.
- Original `scripts/` developer-only Python database inspectors/rehearsal helpers.
- Original `tests/` Rust and Python synthetic/opt-in integration fixtures.

Excluded: `.git`, `.sparc-local`, all untracked/operator state, credentials, project URLs/configs, keys, real data/archives, private test receipts/sandbox profiles, binaries/build outputs and desktop implementation source. The snapshot is not a complete runnable old desktop repository. Documentation references to omitted desktop source and private operator paths are historical context, not missing new-project dependencies.

All copied files are regular non-executable-mode reference files. They can still contain executable code/commands: **do not execute them without separate review and authorization**. Historical source references/import paths are preserved rather than edited to look like new Go code.

## Governing instructions

The [new handoff](../00-START-HERE.md) and its plans override this snapshot. In particular:

- Go CLI-only replaces the old Rust/Tauri/React product direction.
- Old agent names/delegation rules, main-only/worktree choices, commit/push instructions and model references are not new-repository instructions.
- Old live-operation authorizations were exact, limited and consumed. They authorize nothing now.
- Old project IDs/credentials are not required and were not copied. Do not seek or reuse them.
- Old local paths/tool versions are evidence, not recommended installations or current support promises.
- Historical passing tests do not establish the new code works. Port the regression scenario, run it in Go and publish new results.
- A public reference snapshot does not mean real backups/keys/diagnostics may be committed to the new public repository.

## High-value reading map

| Topic | Historical evidence |
| --- | --- |
| Exhaustive Supabase surfaces/manual recovery/risks | [Full original product design](legacy/docs/plans/2026-09-18-supabase-project-archive-design.md) |
| Archive wire format/bounds/security | [Archive prototype](legacy/docs/archive-prototype.md), [implementation plan](legacy/docs/plans/2026-09-18-local-archive-prototype.md), `legacy/src/lib.rs`, `legacy/tests/archive.rs`, `legacy/tests/age_interop.rs` |
| Offline planning/CLI hazards | [Database feasibility](legacy/docs/plans/2026-09-18-database-feasibility.md), `legacy/src/database.rs` |
| First tiny hosted round trip | [Hosted rehearsal](legacy/docs/plans/2026-09-18-hosted-database-rehearsal.md), `legacy/scripts/hosted_rehearsal.py`, matching tests |
| TLS/upstream script findings | [Recipe preflight](legacy/docs/plans/2026-09-20-database-recipe-preflight.md), [native recipe](legacy/docs/plans/2026-09-20-native-recipe-implementation.md) |
| Roles/security/history as inert data | [Expanded design](legacy/docs/plans/2026-09-20-expanded-database-coverage-design.md), [implementation/results](legacy/docs/plans/2026-09-20-expanded-database-implementation.md), `legacy/scripts/expanded_metadata.py`, `legacy/scripts/expanded_rehearsal.py` |
| Scoped structural catalog observation | [Inspector design](legacy/docs/plans/2026-09-20-application-database-inspection-design.md), [implementation/results](legacy/docs/plans/2026-09-20-application-database-inspection-implementation.md), `legacy/scripts/application_inventory.py` |
| Direct dependency limits | [Dependency design](legacy/docs/plans/2026-09-20-application-dependency-inspection-design.md), [results](legacy/docs/plans/2026-09-20-application-dependency-inspection-results.md), `legacy/scripts/application_dependencies.py` |
| Route decision and unsafe alternatives | [Capture route vetting](legacy/docs/plans/2026-09-20-database-capture-route-vetting.md) |
| Successful restore can broaden privileges | [Permission experiment design](legacy/docs/plans/2026-09-20-native-permissions-experiment-design.md), [results/counterexample](legacy/docs/plans/2026-09-20-native-permissions-experiment-results.md), `legacy/tests/native_permissions_pg_test.py` |
| Independent target prerequisite contract | [Preflight design](legacy/docs/plans/2026-09-20-destination-permission-preflight-design.md), [results](legacy/docs/plans/2026-09-20-destination-permission-preflight-results.md), `legacy/scripts/destination_permissions.py` |
| Native selectors still match patterns | [Selector design](legacy/docs/plans/2026-09-20-native-literal-schema-selection-design.md), [results](legacy/docs/plans/2026-09-20-native-literal-schema-selection-results.md), `legacy/scripts/native_schema_selection.py` |
| Composed bounded local recovery and rollback | [Profile design](legacy/docs/plans/2026-09-20-native-recovery-profile-design.md), [final result](legacy/docs/plans/2026-09-20-native-recovery-profile-results.md), `legacy/tests/native_recovery_profile_pg_test.py` |
| Superseded desktop history | [Desktop design](legacy/docs/plans/2026-09-18-local-desktop-alpha-design.md), [desktop implementation plan](legacy/docs/plans/2026-09-18-local-desktop-alpha-implementation.md); retained for history, not work to do |

## What is intentionally NOT claimed

The historical tests were tiny trusted quiescent fixtures, mostly PostgreSQL 17-specific. They did not establish general provider-owned `public` restore, arbitrary schema dependencies, real Auth/MFA/session recovery, Storage bytes/ownership, functions, Vault, active side-effect suppression, cross-service atomic snapshots, Windows distribution or the 10 GB/100 GB validation envelope.

The snapshot's synthetic fixtures can guide new tests. Do not generalize by replacing fixed fixture names with user input. Old subprocess output bounds and lifecycle helpers have documented limitations; adopt the corrected requirements in the new plans rather than copying the old implementation unquestioningly.

The source/third-party license choice for the new public repository still needs the owner. Preserve provenance and applicable notices when reusing code; the inclusion of evidence is not a new license grant over third-party material.
