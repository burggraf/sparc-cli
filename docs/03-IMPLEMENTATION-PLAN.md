# SPARC Go CLI Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task if that skill is available. Portable equivalent: execute one bounded task at a time, run its failing/passing checks, inspect the diff, record evidence and stop at approval gates. No particular AI harness or subagent is required or authorized.

**Goal:** Build a dependency-free-to-install Go CLI that backs up, verifies and restores supported hosted Supabase projects into new empty projects on macOS and Windows.

**Architecture:** One Go executable orchestrates bundled official PostgreSQL clients, documented Supabase HTTP APIs, S3-compatible transfers and age-encrypted versioned archives. A fixed, testable sequence owns capture/restore state; explicit coverage and recovery requirements prevent partial backups from masquerading as complete recovery.

**Tech Stack:** Go standard library, pgx v5, filippo.io/age, one qualified S3 SDK, minimal terminal/platform helpers, bundled version-compatible PostgreSQL clients, native release signing and optional GoReleaser.

---

**Date:** 2026-09-21. **Repository assumption:** fresh empty public repository; source paths below are relative to its root. The copied planning directory can be named `planning/` or another directory without changing these source paths. **Status:** none of these Go tasks have been implemented or run.

## How to execute this plan

1. Read [product](01-PRODUCT-AND-ARCHITECTURE.md), [research](02-RESEARCH-AND-DECISIONS.md), [release](04-PACKAGING-AND-DISTRIBUTION.md) and [acceptance](05-VERIFICATION-AND-SECURITY.md) contracts before selecting a task.
2. Resolve the task's unknowns with the named Rxx experiment; do not invent API contracts or generalize old fixtures. Commit sanitized findings to `docs/research/` after approval under repository policy.
3. For implementation tasks, use a small red/green cycle: add the named failing test → run it and confirm the relevant failure → minimal implementation → focused test → regression tests → review diff and secret hygiene → checkpoint/commit when authorized. Use Go's testing library, not a new test framework.
4. Every task lists proposed files and acceptance. Create only files actually needed; combine small helpers if that reduces complexity without mixing trust boundaries. No empty scaffolding for future tasks.
5. Re-read current versions/docs when implementing. Module versions/tool payloads must be pinned based on then-current supported releases, not this document's examples.
6. Stop at blockers. A failed test or unsupported feature does not authorize dropping ACLs, ignoring restore errors, downgrading TLS, accepting unknown dependencies or silently narrowing requested scope.
7. Keep a `docs/progress.md` ledger: task status, code revision, exact tests/exit status, support scope, unresolved requirements and next approved work. Never mark success from a plan or historical test count.

No hosted credentials/resources are needed for initial work. Native tools, SDKs and test servers may be developer/CI prerequisites; they are not end-user prerequisites. Obtain approval before installing local tools or incurring costs in an operator environment. Do not copy old main-only/worktree/agent/push instructions into the new repository's policy.

## Phase map and dependencies

| Phase | Tasks | Exit criterion |
| --- | --- | --- |
| P0: prove feasibility boundaries | 01–03 | Minimal CLI/public hygiene, packaging/provenance spike and explicit supported-profile decisions. |
| P1: safe local foundation | 04–08 | Private credentials/processes and encrypted archive round trip on macOS/Windows; no Supabase support claim. |
| P2: database recovery | 09–13 | Native local proof, hosted public/Auth/security/Vault proof for explicitly authorized supported profile. |
| P3: project completeness | 14–18 | Standard Storage ownership, config/manual tasks, functions, side-effect strategy and whole-project orchestration. |
| P4: useful commands and remote destinations | 19–23 | Offline/remote verify, S3 backup destination, safe journals/resume, reliable guided/headless workflows. |
| P5: release readiness | 24–27 | Source-independent hosted acceptance, scale evidence, signed clean-machine downloads and public docs. |
| P6: demand-driven coverage | 28 | SFTP/specialty/advanced integrations independently qualified; no catch-all support claim. |

Within a phase, follow explicit dependencies. Packaging feasibility starts early; final signing/release work cannot be postponed until after all service code is written. No automatic delegation is implied by independent tasks.

## Task 01 — Public repository and minimal command contract

**Prerequisites:** owner supplies repo/module name and license decision; public hygiene can start without live access.

**Create:** `go.mod`, `cmd/sparc/main.go`, `internal/cli/run.go`, `internal/cli/run_test.go`, `.gitignore`, `README.md`, `SECURITY.md`, `docs/progress.md`, `.github/workflows/ci.yml`.

1. Choose a supported Go version and exact module path. Set `go`/toolchain policy; no tutorial import path or old repo path. No runtime dependencies initially.
2. Test `Run(args, stdin, stdout, stderr)` help/version/unknown command behavior, deterministic exit categories and no network/filesystem side effects for help. Test `backup`, `verify`, `restore` are recognized but **explicitly unavailable**, not false-success placeholders.
3. Run `go test ./internal/cli -count=1 -v`; observe failure before implementing behavior.
4. Implement thin main calling the testable runner; stdlib flags/subcommand dispatch, no full-screen TUI. Keep names and example output in `docs/usage.md` when command behavior grows.
5. Ignore secrets/keys/passfiles/.env/private configs/archives/tool caches/build outputs; ignore rules are a secondary control, not permission to keep real data in repo. Add a PR checklist for secret/fixture hygiene.
6. CI runs `go test ./...`, `go vet ./...`, build on macOS/Windows, no live credentials. Add race checks on supported developer runners when concurrency arrives.
7. `go build ./cmd/sparc` and native help/version runs pass. README clearly says scaffold only, not a backup tool.

**Exit:** new public repo has no copied executable legacy source in active packages; reference snapshot remains documentation only. A passing help command does not claim recovery support.

## Task 02 — Dependency-free packaging/provenance spike (R01/R02/R17)

**Depends on:** 01. Run before committing to a distribution format.

**Create as needed:** `docs/research/R01-packaging.md`, `docs/research/R02-client-provenance.md`, `build/clients/manifest.json`, `build/clients/README.md`, `build/package/README.md`, an isolated `experiments/packaging/` proof (not production recovery).

1. Compare trusted native client sources/build recipes for macOS arm64/amd64 and Windows amd64; inspect complete dependency trees and license obligations. Pin one research payload per platform with digest and provenance.
2. Build a minimal Go executable that embeds and privately extracts the payload; separately test a ZIP-with-tools control. It executes only approved version checks and synthetic TLS database operations, never arbitrary downloaded code.
3. Prove no Homebrew/VC redistributable/PG/Go/runtime installation needed on clean standard-user machines. Test actual dump/restore, not only `--version`.
4. Investigate/sign/notarize with owner-provided credentials where authorized. Without them, record unsigned proof only and keep public-distribution gate open. Evaluate helper signatures before embedding, notary scanning and quarantine/MOTW.
5. Test first-run offline extraction, denied writes, architecture mismatch and cache tamper; measure package/extracted size for one versus multiple PG majors.
6. Record minimum OS candidates and unresolved signing issues. Choose one-file strategy only when demonstrated; if blocked, seek owner approval for ZIP fallback. No GoReleaser Pro or signing-service purchase without approval.

**Exit:** documented feasible package path or explicit blocker. No product support implied by a packaging prototype. Test maps to A03/A28.

## Task 03 — Define support profile and pin research decisions

**Depends on:** 01–02 research. **Create:** `docs/research/R04-database-route.md`, `docs/compatibility.md`, `docs/coverage.md`, `internal/operation/report.go`, `internal/operation/report_test.go`.

1. Review preserved historical route findings and current upstream sources; choose initial supported PG/source/target versions and native tools. PG17 is a research starting point, not universal hosted coverage.
2. Write per-component capability/coverage/verification states and required unknowns. Test permission denial/unknown/unsupported cannot become `not_used` or success; incomplete coverage remains explicit.
3. Define initial component support requirements including ordinary `public`, real Auth, standard Storage with ownership, function packages and Vault if detected. Smaller experiments allowed, smaller unannounced product claims not allowed.
4. Define feature detection and manual/unsupported paths for every inventory row in product plan. Specify which omissions block archive completeness, restoration or application-ready status.
5. Run `go test ./internal/operation -run TestReport -count=1 -v` and regression suite. Record which Rxx questions block each later task.

**Exit:** explicit version/feature matrix and report contract, not a declaration-only planner presented as recovery.

## Task 04 — Native private storage, config and credential boundaries

**Depends on:** 01. **Create:** `internal/platform/private_darwin.go`, `private_windows.go`, corresponding tests; `internal/config/config.go`, `config_test.go`; `internal/credentials/input.go`, `input_test.go`; `docs/credentials.md`.

1. Add tests for exclusive private file/dir creation, unsafe paths/symlinks/reparse points, permission failures, oversized/duplicate/unknown JSON, malformed UTF-8, embedded control characters, environment leakage and fixed redacted errors.
2. Implement native path locations, POSIX modes and Windows DACL policy. Do not use `os.Chmod(0600)` as a Windows security claim. Set explicit threat boundary for same-user path races.
3. Add hidden terminal secret entry using a qualified minimal helper; non-TTY input requires explicit file/env reference or fails. No secret values in argv.
4. Profiles contain non-secret metadata and secret references only. Keep optional Keychain/Credential Manager persistence deferred until explicitly implemented/tested; no silent plaintext fallback.
5. Test redaction through nested errors, logs and JSON output using canaries; escaping terminal controls does not reveal hidden values.
6. Run `go test ./internal/platform ./internal/config ./internal/credentials -count=1 -v` natively on both platforms.

**Exit:** A02/A27/A30 local protections proved; no real project credentials needed.

## Task 05 — Trusted tool extraction and bounded process lifecycle

**Depends on:** 02,04. **Create:** `internal/tools/payload.go`, `extract.go`, `run.go`, tests; `internal/platform/process_darwin.go`, `process_windows.go`, tests.

1. Add extraction tests: allowlisted inventory, hashes, incomplete cache, concurrent first run, traversal/decompression bounds, modified helper, wrong platform, no PATH fallback.
2. Implement private versioned extraction and atomic-ready publication exactly as packaging contract; use approved final signed payload manifest when available.
3. Add Go helper-process tests for huge stderr/stdout, never-ending child, child holding pipe, grandchild, output sink error, cancellation, failed start, wrong exit and secret canaries.
4. Implement direct argv, sanitized environment/cwd, streaming limits, bounded diagnostics, deadlines and process-tree ownership. macOS process groups and Windows Job Objects or equivalent need native proof. `CommandContext` alone is insufficient evidence.
5. Credentials via scoped private passfiles; explicit `psql -X`/no password prompts where used. Disable unwanted inherited PG/DYLD/credential config without breaking required OS execution variables.
6. `go test ./internal/tools ./internal/platform -count=1 -v`; native integration proves no surviving children after failure/cancellation and no secret arguments.

**Exit:** A03/A04. No shell pipeline from the old Python harness is adopted.

## Task 06 — Archive schema and streaming encrypted artifacts

**Depends on:** 03–04. **Create:** `docs/archive-format.md`, `internal/archive/manifest.go`, `codec.go`, `writer.go`, tests; synthetic `internal/archive/testdata/` only if needed.

1. Resolve R13 basics: new format name/version, X25519/passphrase decision, recovery material, bounds/index sharding and legacy import policy. Do not accidentally claim `sparc-local-artifacts` compatibility.
2. Test strict bounded manifests, deterministic IDs, duplicate keys/paths, unsupported required versions, integer/depth limits, Unicode and cross-platform logical-key preservation.
3. Add real age encryption test for zero/small/multiple/large-stream artifacts, closing/finalization failures and source mutation. Implement bounded streaming; no `io.ReadAll` for payloads.
4. Use opaque filenames and encrypted manifest/indexes; record component scope/status and lengths/digests. Add only existing artifact fields needed for recovery; don't invent crypto/chunk formats.
5. Test wrong key/truncation/corruption and independent Go age interoperability. Preserve disclosure that encryption does not authenticate sender.
6. Run `go test ./internal/archive -count=1 -v`; measure allocations on a streamed large synthetic reader, not a huge byte slice.

**Exit:** A06/A07; local archive primitive only.

## Task 07 — Local destination publication and offline verification

**Depends on:** 04,06. **Create:** `internal/destination/local.go`, `local_test.go`, `internal/archive/reader.go`, `reader_test.go`, `internal/verify/offline.go`, `offline_test.go`.

1. Test no-clobber archive directory and partial writes; complete encrypted payload checks precede exclusive final manifest publication. Interrupted output has no complete marker.
2. Implement local/external/mounted folder destination using native safe file semantics; document where network filesystem guarantees are weaker. Do not promise atomic multi-file transactions.
3. Reader validates authenticated EOF, exact required files/index references, hashes and bounds without executing content. Extra files policy is explicit and tested.
4. Offline verify distinguishes byte integrity from missing recovery components. It makes zero network requests and reads no source config.
5. Check missing payload, bad hash, final-manifest failure, wrong recovery key, disk-full/read failure and case/path collision behavior. Test restored local artifacts remain private, without making local unpack a primary public command unless needed.
6. Run `go test ./internal/destination ./internal/archive ./internal/verify -count=1 -v`.

**Exit:** encrypted artifact round trip/source-folder removal proof; A08 foundation. Still not a Supabase backup.

## Task 08 — Supabase HTTP transport and project discovery

**Depends on:** 03–04. **Create:** `internal/supabase/client.go`, `projects.go`, `capabilities.go`, tests; `docs/research/R10-api-contracts.md`, `R16-login.md`.

1. Use `httptest` tests for auth scope, custom endpoint refusal, redirect credential leak, request timeout, body bounds, pagination, 401/403/404/429/5xx, Retry-After and malformed/unknown responses.
2. Implement explicit control-plane client using net/http; read-only discovery first. Token-based flow only. No embedded OAuth client secret or mandatory SPARC backend.
3. Model permissions separately from absence. Discover authorized projects/versions/features without resetting passwords, creating roles or changing settings.
4. Distinguish management token, DB credential, project service/S3 credential and backup-destination credential. No ambient AWS credential chain accidentally selecting the source for destination writes.
5. Record sanitized endpoint fixtures and raw-response encryption/retention policy. Keep alpha aggregate config optional with qualified endpoint fallbacks.
6. `go test ./internal/supabase -count=1 -v`; all tests offline.

**Exit:** A02/A20 HTTP contract foundation, not live API support.

## Task 09 — Database connection/TLS and catalog observation

**Depends on:** 05,08, R03. **Create:** `internal/database/connect.go`, `inspect.go`, `selectors.go`, tests; `tests/integration/connection_test.go`, `catalog_test.go`; `docs/research/R03-connectivity.md`.

1. Test strict connection parameters, expected project routing, unsupported pooling, version/capability reporting, pgx configuration without unsafe ambient fallbacks.
2. Implement pgx read-only catalog observation in explicit transactions with safe search path/timeout. Input names are data/quoted identifiers, not interpolated patterns.
3. Native local tests prove good TLS and wrong CA/hostname/missing TLS rejection for pgx and bundled clients independently. Qualify trust-store/certificate provisioning per platform; don't ship a stale single CA forever.
4. Reproduce literal schema matrix with exact names, quotes, spaces, backslashes, regex punctuation, Unicode, max supported identifier bytes, decoys and missing names. Native argv does not solve pg_dump's pattern semantics.
5. Inventory structural/security/dependency features; preserve unknown dynamic/string-body references and local-only OIDs. Port tested ideas, not old hard-coded provider baselines.
6. Run unit tests and opt-in local `go test -tags=integration ./tests/integration -run 'TestConnection|TestCatalog|TestSelector' -count=1 -v` with approved test tool directory.

**Exit:** A05/A09 observed facts, not general dependency closure.

## Task 10 — Destination baseline and security preflight

**Depends on:** 09,R05. **Create:** `internal/database/security.go`, `target.go`, tests; `tests/integration/security_test.go`; `docs/research/R05-permissions.md`.

1. Define independently reviewed baseline/profile for supported targets; don't copy observed target state and call it expected. Include roles/memberships/settings/defaults and application collisions.
2. Test owners and semantic ACLs including column grants, grantor/options, NULL/empty distinction, PUBLIC versus named role, version-specific membership flags and forced RLS.
3. Reproduce the historical unwanted global-default SELECT counterexample in an owned local cluster: restore can succeed while exposing data. Preflight must reject before mutation.
4. Refuse existing application/Auth/Storage/function resources using combined DB/API checks; validate source != target even through alias connection paths. Target-wide service emptiness needs later live qualification.
5. No auto-revocation/normalization of unexpected defaults, no broad privilege flags. Missing role credentials become explicit input requirements.
6. Unit + local integration tests prove unchanged sentinels on refusals and read-only preflight. Hosted `public` baseline waits for Task 12.

**Exit:** A11/A22; report remains a prerequisite result, not blanket safe-restore authorization.

## Task 11 — Bounded native database capture/restore proof

**Depends on:** 06–07,09–10,R04. **Create:** `internal/database/capture.go`, `restore.go`, `roles.go`, `history.go`, tests; `tests/integration/recovery_test.go`, `docs/research/R04-database-route.md` updates.

1. Start with a trusted quiet fixed fixture and independently initialized source/target. Define exactly supported schemas/features and refusal cases.
2. Test one native custom dump, archive round trip, source stopped, target-only restore with error-stop/transaction semantics. No new generic engine for SQL rewrite adapters.
3. Handle custom restricted roles explicitly; globals not implicitly in pg_dump. Preserve migration arrays/nulls/order as parameterized inert data; trap SQL/metacommand history must not execute.
4. Verify full object inventory/rows/types/indexes/constraints/sequences/owners/ACLs/defaults and positive/negative permissions. Test source-inaccessible recovery, not merely successful commands.
5. Inject late DDL failure after meaningful progress and prove transactional rollback for supported objects; preserve preexisting sentinels and document sequence/external-effect exceptions.
6. Test bad dump, changed manifest/receipt, cross-schema missing dependency, role/history collisions, version downgrade and repeat restore refusal.
7. Run `go test ./internal/database ./internal/archive -count=1 -v` and opt-in `go test -tags=integration ./tests/integration -run TestRecovery -count=1 -v`.

**Exit:** bounded local Go recovery evidence replacing, not inheriting, old Rust/Python success.

## Task 12 — Hosted public/Auth/managed-schema recipe qualification

**Depends on:** 08–11; **fresh owner approval required before hosted activity**.

**Create:** `tests/hosted/database_test.go`, `auth_test.go`, private-config contract in test code, `docs/research/R04-hosted-recipe.md`, `R06-auth.md`; extend database/supabase modules only for proven behavior.

1. Specify exact disposable project refs, permitted fixture/actions, cost cap, no production data and cleanup authority. Default test suite must not discover/use hosted credentials.
2. Inspect actual source/target PG/Auth/Storage versions and `public` owner/default baselines. Establish native recipe for managed data/customizations; compare with current pinned upstream recipe without assuming Docker parity.
3. Seed representative public app, real Auth accounts/password hashes/identities and dependent rows, selected managed customizations, roles/history and supported extensions.
4. Capture under a quiet window/coordinated snapshot; restore with source access denied and destination-only inputs. Verify real password login, IDs/FKs and private access using designated synthetic users.
5. Record session invalidation/reauth policy; MFA/passkeys/SSO/OAuth apps require additional targeted cases, not blanket support from password login.
6. Verify no changes to reserved provider roles/baseline or unintended grants; distinguish event-trigger baseline behavior from user automation.
7. Unknown managed layout, nonempty destination, absent privilege or extension incompatibility stops. Never ignore "already exists" errors globally.

**Exit:** ordinary hosted public/Auth support only for actually qualified versions/features. Historical non-public-only evidence is insufficient.

## Task 13 — Vault/encrypted-column recovery prerequisite

**Depends on:** 08,11–12,R08 and approved hosted scope.

**Create:** `internal/database/vault.go`, `vault_test.go`, `tests/hosted/vault_test.go`, `docs/research/R08-vault.md`.

1. Offline tests classify required key capture and refuse missing/invalid key without echoing it. Key data encrypted in archive; management token not archived by default.
2. Verify documented active-source key endpoint, permission/stability/version contract and unused-target protection. Root-key replacement is a distinct explicitly acknowledged action.
3. Hosted synthetic round trip captures ciphertext + root key, denies source access, transplants key and validates target decryption using a non-disclosing assertion.
4. Test wrong/missing key, source unavailable during capture, incompatible extension/schema and target already containing encrypted data. Never try a random replacement key until decrypt happens to work.
5. Record key ordering relative to data/trigger restore, no double encryption and recovery limitations.

**Exit:** detected Vault cannot be called recoverable without A18. If unsupported, mark archive/restore blocked for affected projects.

## Task 14 — Standard Storage capture and supported restore fidelity

**Depends on:** 06–08,12,R07; implement offline portions before live work.

**Create:** `internal/storage/client.go`, `inventory.go`, `capture.go`, `restore.go`, tests; `tests/hosted/storage_test.go`, `docs/research/R07-storage.md`.

1. Evaluate one Go S3 SDK against required Supabase operations/checksum behavior; select/pin it. Test endpoint/region/path-style/signing and no accidental AWS ambient source/destination credential crossover.
2. Enumerate all buckets/pages and object keys with bounded memory, paginate nested-looking flat keys correctly; bucket settings via explicit supported REST where S3 lacks fields.
3. Stream downloads to encrypted artifacts with digest/size/content headers/user metadata and owner/ID inventory. Conditional requests detect changed/deleted objects; retry only under stated consistency contract.
4. Prove restore metadata/owner/ID reconciliation via supported interfaces. Do NOT finish this task with admin uploads and defer unnoticed owner loss.
5. Hosted fixture includes two users/private owner-RLS, app object-ID reference, public bucket setting, >1,000 objects, empty/Unicode/case/reserved-local-name keys and multipart size. Verify exact bytes and negative access.
6. Never use remote keys as local filesystem paths. Tests cover encoding/double-encoding, terminal controls and stable metadata maps.
7. Explicitly detect vector/analytics buckets and exclude/block them with component reports until dedicated support exists.

**Exit:** A14–A16/A26 standard-file fidelity. Unsupported ownership path is a product blocker for affected apps, not a successful partial equivalence claim.

## Task 15 — Configuration capture and structured manual recovery

**Depends on:** 03,06,08,R10/R16. **Create:** `internal/supabase/settings.go`, `settings_test.go`, `internal/recovery/requirements.go`, `requirements_test.go`, `prompts.go`, tests; `docs/research/R10-api-contracts.md` updates.

1. Define per-endpoint readable/writable/secret/digest/immutable/default/unknown field contracts, versioned with sanitized fixtures. Model config GET and PATCH separately.
2. Capture supported Auth/SMTP/templates/providers/hooks/Data API/Realtime/Storage/database/network/infrastructure settings and encrypted original responses with bounds.
3. Required secret values unavailable from APIs become typed recovery items. Test HMAC/digest/masked/null responses cannot be replayed as credentials.
4. Prompt for actual values or replacements using hidden input; preserve external owner/action/reference metadata. "Provided" is not "tested" and external reference is not self-contained data.
5. Restore allowlisted supported fields in safe dependency order; report new project URLs/keys/callback/DNS/hosting tasks. Do not globally rewrite code/SQL/rows.
6. Test unknown fields/permissions/plan restrictions and explicit unresolved results. Sending email/SMS or activating hooks requires separate approval.

**Exit:** A20; configuration and missing-information workflow covers every product inventory family, with explicit unsupported classifications where necessary.

## Task 16 — Edge Functions and dependency-complete deployment

**Depends on:** 06,08,15,R09. **Create:** `internal/functions/inventory.go`, `package.go`, `restore.go`, tests; `tests/hosted/functions_test.go`, `docs/research/R09-functions.md`.

1. Prove actual Management API download/deploy body format and server-side bundling capability; identify any required local ESZIP/Deno tool before committing to implementation.
2. If an extra runtime/tool is required, resolve packaging under Task 02 before advertising dependency-free function support. No surprise user installation.
3. Capture deployed bodies/packages, entrypoint/slug/JWT settings, import maps/deno.json/lockfiles/shared assets. User repository import is explicit, filtered for traversal/symlinks and sensitive files; not a whole-home-folder scan.
4. Inventory secret names/digests, accept approved values via Task 15; do not extract values via deployed exfiltration code. Target uses its own managed `SUPABASE_*` environment.
5. Re-deploy from archive with source and original repository access denied. Pin/package dependencies so transient upstream source changes don't silently alter recovery. Identify any declared external dependency still required.
6. Test missing package/dependency/secret/JWT config; safe authorized smoke test must validate expected auth behavior, not only HTTP deployment success.

**Exit:** A19 with complete redeployable package or explicit blocking gap.

## Task 17 — Side-effect suppression and extension/automation inventory

**Depends on:** 09–16,R12/R15. **Create:** `internal/database/automation.go`, tests; `internal/recovery/activation.go`, tests; `tests/hosted/activation_test.go`, `docs/research/R12-consistency-and-effects.md`.

1. Inventory cron/jobs/queues/webhooks/pg_net/hooks/publications/FDWs/external routine effects and relevant extension-managed durable state. Dynamic code cannot be exhaustively inferred; require reviewed supported profiles and manual declarations.
2. Determine which definitions/data can be captured, restored inactive and later activated. A flag named "quarantine" is not proof of API/network isolation.
3. Build explicit inactive restore/activation checklist using documented operations; permission/version uncertainty blocks dependent work. Do not globally change source settings or pretend session_replication_role disables every effect.
4. Test with controlled outbound collectors and stopped consumers, no real email/SMS/webhooks. Verify no unexpected calls during schema/data/function/config restore.
5. Preserve queue message semantics/history limitations; unsupported extension-owned data is visible, not silently missing from database dump.
6. Record operator quiet-window procedure and snapshot boundaries across database/Storage/config; live mode may remain limited with explicit drift report.

**Exit:** A21/A26; no activation before explicit user approval and required validation.

## Task 18 — Fixed whole-project backup and restore sequence

**Depends on:** 03–17 for advertised components. **Create:** `internal/operation/backup.go`, `restore.go`, `plan.go`, tests; `internal/cli/backup.go`, `restore.go`, tests.

1. First add fake-component sequence tests asserting preflight-before-mutation, correct dependency ordering, no source handle supplied to restore, final manifest last, and stop on failed prerequisites.
2. Implement the fixed sequences in the product plan. Use real modules without a plugin/workflow framework. Carry context/cancellation and bounded progress events.
3. Backup distinguishes immutable captured artifacts from incomplete manual prerequisites; final report may be incomplete without being corrupt. No percentage hiding missing root keys/files.
4. Restore validates archive/trusted origin before target mutation, refuses source aliases/nonempty target, previews changes, rechecks baseline and applies reviewed ordering.
5. Keep target-bound restore state outside the archive. Jobs/public access/external cutover remain disabled until explicit approved activation.
6. Fault-inject each phase and assert later phases never execute. Unknown commit/API outcome marks target ambiguous/quarantined, not retryable success.
7. `go test ./internal/operation ./internal/cli -count=1 -v`; one authorized small whole-project round trip when all prerequisites are proven.

**Exit:** actual three-command operation path begins, but remote comparison/destinations/resume and release gates remain.

## Task 19 — Read-only remote verification and honest deep comparison

**Depends on:** 07,09–18,R11. **Create:** `internal/verify/compare.go`, `database.go`, `storage.go`, tests; `internal/cli/verify.go`, tests; `docs/research/R11-verification.md`, `docs/verification.md`.

1. Define quick versus deep versus sampled checks and expected source/target normalization. Read-only verify cannot send messages, execute user routines or modify sequences.
2. Test equal counts/different values, duplicate/no-PK rows, nulls/types/collation/time encoding, transformed target identifiers and permission drift. Choose deterministic supported DB-content representation with explicit unsupported-type behavior.
3. Avoid dump-byte equality; store pre-capture verification evidence within the consistency contract. New live reads can establish current comparison, not prove historical equality to a past snapshot.
4. Stream strong Storage hashes or verify provider-qualified checksums; ETag alone insufficient. Show cost/egress warnings for full re-download.
5. Compare supported normalized config/function/security metadata; secret digest comparisons are limited evidence, not a usability/decryption test.
6. Test network drift/inconclusive vs corruption and stable exit/report semantics. Offline command remains available without source or target credentials.

**Exit:** A24 and named-check reports; only real rehearsal produces `restore_tested`.

## Task 20 — S3-compatible remote archive destinations

**Depends on:** 06–08,14,18,R14. **Create:** `internal/destination/s3.go`, `s3_test.go`, `tests/integration/s3_test.go`, qualified provider tests; `docs/research/R14-destinations.md`.

1. Reuse the selected S3 SDK, with separate explicit destination credentials/endpoints and no project-Storage coupling.
2. Test encrypted-only upload, bucket/prefix validation, pagination, multipart/cancellation, retries, collision/no-clobber and failed-final-manifest publication. Define a safe protocol when provider lacks conditional create/atomic rename.
3. Stream or stage encrypted artifacts based on SDK replay requirements; prove bounded RAM/disk. End-to-end restored bytes must match despite retries; do not reuse half-finished age streams.
4. Independent stored-byte read-back before finalized success, including manifest after publication. Remote verify/restore accepts the same location abstraction without writing over archive.
5. Qualify Supabase source/target and selected backup providers separately; a local fake S3 server proves requests, not provider compatibility. No user account creation/egress without approval.
6. Document prefix permissions, lifecycle/retention risk, optional provider object-lock/versioning support and interrupted multipart cost/cleanup. Delete only owned in-progress upload state with clear policy, never a completed backup automatically.

**Exit:** A17 and remote destination requirement, with named supported providers/features.

## Task 21 — Durable operation journals and bounded resume

**Depends on:** 18–20,R13/R14. **Create:** `internal/operation/journal.go`, `resume.go`, tests; extend destination/storage tool interruption tests.

1. Version journal records bound to operation/archive/source/target IDs, recipe/client/config hashes, phases, artifact digests and consistency state; keep secrets protected.
2. Test crash before/after each durable write/publication/API request and corrupted/stale/cross-target journals. Atomically update state; no mutable journal inside finalized archive.
3. Resume only verified immutable artifacts and proven idempotent mutations. Lost DB snapshot restarts its capture phase; uncertain COMMIT requires investigation/new target, not replay.
4. Define object restart versus multipart encrypted resume policy. Existing verified work can survive but no concatenated snapshots/cryptographic stream fragments.
5. Cancellation terminates all owned children, aborts/records owned multipart state where appropriate, stops future work and reports remaining private files/target effects. No automatic source or failed-target deletion.
6. Test sleep/wake/network removal/disk-full/Windows locks and concurrent process access. Record exact safe cleanup/resume instructions.

**Exit:** A08/A16/A23/A27 interruption semantics. Do not call simple transient SDK retry durable resume.

## Task 22 — Final guided CLI and unattended contract

**Depends on:** 15,18–21. **Create/extend:** `internal/cli/prompts.go`, `output.go`, tests; optional native credential-store files under `internal/credentials/`; `docs/usage.md`, `docs/credentials.md`.

1. No-flags `backup` prompts only necessary inputs; discover project, choose destination, explain privileges/cost/consistency/encryption, collect gaps and show progress.
2. Restore walks preview/target identity/manual replacements/quarantine/activation with clear source vs target badges in plain text. Keys never printed by default; key saving includes independent recovery confirmation.
3. Implement stable `--non-interactive`/`--json` and input-reference precedence; missing inputs fail, `--yes` never bypasses hard safety gates.
4. Optional Keychain/Credential Manager persistence tested natively; denial/corruption falls back to safe prompt or fails unattended, no plaintext fallback.
5. Terminal transcript tests with sanitized canaries, narrow/no-color/non-TTY controls and cancellation; don't implement a full-screen dashboard.
6. Plain `sparc` PATH convenience remains optional per packaging docs; no hidden shell/system changes.

**Exit:** owner-requested download/run/prompt UX demonstrated, with all remaining incompatibilities plainly reported.

## Task 23 — Threat-model review, parsers and fault regressions

**Depends on:** 04–22. **Create:** `docs/security-model.md`, targeted fuzz tests in archive/config/HTTP modules, redacted `internal/cli/diagnostics.go` if needed, tests.

1. Walk every trust boundary in verification/security document; map each Axx case to a real test or explicit open issue. No assertion that old test counts satisfy it.
2. Fuzz manifest/index/config parsers and key-to-path handling with bounded corpora; test duplicates, overflow, deep nesting, path/reparse tricks and error-chain disclosure.
3. Exercise filesystem/tool/API fault injection, credential leaks, process descendants, SSRF/redirects, unsafe SQL origins and default-grant exposure.
4. Review archive sender-trust policy and safe user acknowledgment; do not suggest encryption makes arbitrary dumps safe.
5. Produce support diagnostics containing versions/categories/check IDs, not raw SQL/data/URLs/passwords. Upload requires explicit user action; no telemetry by default.
6. Run `go test ./...`, `go vet ./...`, native race tests where supported, local integration, secret/artifact scans and diff review. Obtain independent review if authorized and fix findings with regressions.

**Exit:** explicit risk/coverage ledger; safety blockers remain blockers even if all ordinary happy-path tests pass.

## Task 24 — Representative source-independent hosted acceptance

**Depends on:** 12–23 and fresh exact authorization. **Create:** `tests/hosted/project_recovery_test.go`, `docs/research/hosted-recovery-result.md`.

1. Use the fixture/protocol in [verification](05-VERIFICATION-AND-SECURITY.md), including public app/real Auth/private owner Storage/function/Vault/manual requirement and controlled side effects.
2. Record exact source/target/tool/recipe versions and independently expected state. Back up in approved quiet window and verify actual stored archive.
3. Deny source DB/API/Storage credentials/routes during restore, while permitting target and independent backup location. Prove the deny rule works; don't equate source-file denial with network denial.
4. Restore from archive/portable key/destination credentials and explicit manual replacements; verify content, security, actual Auth login, Storage owner behavior, function config and Vault decrypt without leaking values.
5. Exercise missing critical material and target reuse refusal. Do not reset/delete projects or retry ambiguous mutations without approval.
6. Publish sanitized evidence and limitations; keep private reports/archives outside repo. Source remains intact; cleanup is separately authorized.

**Exit:** Gate C and A25. This is the key product proof, not a UI milestone.

## Task 25 — Scale, recovery-time and interruption qualification

**Depends on:** 21,24 and explicit benchmark cost/resource approval.

**Create:** `tests/benchmark/` generators/harness, `docs/benchmarks.md`, `docs/limits.md`.

1. Define deterministic synthetic 10 GB DB/100 GB Storage target and small-file-heavy corpus, with exact units, object counts and expected restore state.
2. Measure capture/quick/deep verify/restore independently: time, CPU/RSS, disk peaks/plaintext staging, requests/retries and actual egress/storage costs.
3. Include low-bandwidth/laptop and near-provider runs; document network/compute settings. Do not publish only fastest result as universal expectation.
4. Interrupt at worst points, sleep/disconnect/removable-drive removal, disk quota/throttling; verify bounded resume/restart behavior and exact final integrity/security.
5. Tune conservative worker/part/timeouts using evidence; no speculative cache/dedup engine. If envelope fails, reduce advertised support explicitly and record upgrade path.

**Exit:** A29/Gate F only for measured matrix, not arbitrary database sizes/object counts.

## Task 26 — Signed distribution and clean-machine release gates

**Depends on:** 02,05,23–25 for advertised scope. **Create/finalize:** `build/clients/`, native packaging/signing scripts, `.github/workflows/release.yml`, optional `.goreleaser.yaml`, `THIRD_PARTY_NOTICES.md`, `docs/install.md`, `docs/releasing.md`.

1. Rebuild/verify pinned patched client payloads and transitive native dependencies; SBOM/licenses/source provenance.
2. Follow full [packaging plan](04-PACKAGING-AND-DISTRIBUTION.md), including signed helpers before embedding, final binary signing/notarization, correct Windows timestamps and supported-container stapling investigation.
3. Exact release artifacts on clean native macOS Apple Silicon/Intel and Windows x64 at advertised minimums; real browser download/quarantine/MOTW, no developer tools/admin/security bypass.
4. Verify no dependency downloads, real bundled TLS/PG operations, no PATH/DLL accidental fallback, actual backup/verify/restore smoke flow, optional credential store and cancellation.
5. Publish checksums AFTER signing; independent signature/provenance, notices, supported client/OS matrix and honest SmartScreen caveat. No signing secrets in forks/public logs.
6. Draft release only until owner approves publication and signing/service costs. A compiled unsigned dev binary is not completion of this task.

**Exit:** Gate E, one-file UX proved or explicit owner-approved self-contained-folder fallback documented.

## Task 27 — Operator documentation and versioned release support

**Depends on:** 19,22–26. **Create/finalize:** all operator docs named in verification plan; `CHANGELOG.md`, version/archive compatibility policy and security advisory process.

1. Write a first-run guide for both OSes with no dependency setup; how to obtain each credential, least privilege limits, scopes and source/target distinction.
2. Explain quiet/live semantics, encryption/recovery key protection, full versus partial backup, verification levels, missing secret/data behavior, new API keys/callbacks/DNS and activation.
3. Document private temp/cache/journal paths, disk/egress estimates, failed-target quarantine, safe resume/new-target recovery, manual update/uninstall without deleting backups/keys.
4. Publish supported versions/features/providers/limits and unsupported specialty data. Record every open Rxx/Axx gap; never substitute a reassuring percentage.
5. Define archive readers/migrations/deprecations, bundled client security updates, signing identity renewal, release revocation and redacted support reporting.
6. Run an operator walkthrough from docs on clean machines; fix misleading examples. Source-independent recovery instructions must not ask for old project access.

**Exit:** public alpha/release description exactly matches tested capabilities; no deployment or publication without approval.

## Task 28 — Later coverage and deliberate exclusions

**Depends on:** actual demand and explicit bounded design for each item. No speculative code now.

- SFTP destination: maintained Go SSH/SFTP library, host-key verification, private remote dirs, resumable/atomic publication semantics, no shell dependency. Test untrusted host key/rename limitations before support.
- Specialty Storage vectors/Iceberg: dedicated APIs/export files, complete vectors/indexes/catalog/location remapping and history contracts. Manual attachment is not enough without restoration proof.
- Advanced Auth SAML/custom OIDC/OAuth-server/MFA/passkeys: explicit IDs/metadata/credentials/binding changes and supported admin operations; no session continuity assumption.
- More extensions/cron/queues/FDWs/replication and additional PostgreSQL majors: profile-specific export/restore/side-effect/security tests.
- Custom domains/network controls/log drains/team/organization/branches/snippets/external integrations: automate safe subsets, retain manual checklist and cost/cutover approval.
- Optional auto-create new project: price/plan/region preview, authorization, asynchronous provisioning and cleanup responsibilities; never production reset.
- Native Windows ARM64/Linux/macOS universal/optional installers/package managers: new packaging matrix and trust tests.
- Scheduled backups/retention/multiple copies: start with documented OS scheduler; deletion/retention is separately authorized and must not destroy last known-good recovery set.
- Incremental/dedup/PITR/continuous replication: different consistency/recovery product, not hidden scope in v1.
- Selective/no encryption, sender signatures and team keys: threat-model/format change with explicit warnings and compatibility plan, not a shortcut to ship sensitive data unprotected.

## Handoff checkpoints

After each phase, summarize only what is implemented/tested, exact commands/results, remaining blockers and next bounded task. Keep new evidence distinct from archived old evidence. The final core gate is: **download on a clean supported laptop, capture a representative hosted project, verify the archive, restore with the source inaccessible, and demonstrate equivalent supported data/access behavior without installing dependencies.**
