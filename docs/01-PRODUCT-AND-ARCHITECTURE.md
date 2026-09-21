# SPARC Go CLI — product and architecture

**Date:** 2026-09-21. **Status:** approved product direction plus proposed technical contracts; unproven details are explicitly gated. Read [Start here](00-START-HERE.md) first.

## 1. Product promise

Create a portable encrypted recovery package for a hosted Supabase project, report exactly what was captured, and reconstruct supported functionality in a new hosted project without accessing the original project.

Do not promise a perfect infrastructure clone, continuous availability, preserved live sessions, physical disaster recovery/PITR history, or support for arbitrary PostgreSQL features. Unknown or inaccessible components must remain visible; missing permissions are not evidence a feature is absent.

User experience: download SPARC, run `sparc backup`, select the project/destination, provide credentials and encryption recovery information, review consistency/coverage, then copy and verify. Manual prompts are a normal recovery mechanism, not unstructured notes that disappear after the session.

### Non-goals for the initial product

No GUI/Tauri/React, hosted SPARC service, mandatory browser automation, mandatory account with SPARC, Docker dependency, mandatory Supabase CLI, plugin system, generic workflow engine, incremental/deduplicated backup chains, source deletion, destructive target merge, in-place restore, automatic retention deletion, replication migration, or scheduler daemon. OS schedulers can run the eventual headless CLI. Preserve omitted features in coverage and the roadmap, not in speculative scaffolding.

## 2. Commands and interaction contract

These are proposed public commands; finalize syntax in Task 01 and keep it stable once released. `sparc`, not `spark`, is the executable name.

```text
sparc backup
sparc backup --project SOURCE_REF --to /path/to/new-backup
sparc backup --project SOURCE_REF --to s3://archive-bucket/new-prefix
sparc verify /path/to/backup
sparc verify /path/to/backup --against SOURCE_OR_TARGET_REF
sparc verify /path/to/backup --against TARGET_REF --deep
sparc restore /path/to/backup --project NEW_TARGET_REF
```

Supporting commands/options are allowed only when useful: `--help`, `--version`, `doctor`, `keygen`, and non-mutating restore preview. They must not obscure the three core commands. `doctor` performs local checks by default; network checks require explicit project selection and clear consent. It must never use upstream `supabase db dump --dry-run` as an offline probe.

### Interactive mode

- `sparc backup` with a terminal and no flags starts a line-oriented guided flow; no full-screen TUI required.
- Ask for a Management API token, discover authorized projects where possible, confirm name/ref/organization, request database access and Storage access as needed, then choose destination and encryption.
- Clearly separate source, restored project and remote backup storage. Do not reuse credentials across them implicitly.
- Hidden password/secret entry, confirmation on destructive or expensive actions, useful progress on stderr, stable summary on stdout.
- Prompts support spaces/Unicode, paste, cancellation, redirected streams, narrow terminals and color-disabled/accessibility use. Do not print raw server errors or object names containing terminal control sequences.
- Show estimated bytes, temporary-space needs, request/egress cost implications and consistency mode before copying. Do not start paid project creation or benchmarks implicitly.
- Collect only missing values. Allow `not used`, `provide later`, `external reference` and `cannot recover`, each with an honest status.
- Print recovery requirements even if data export completed. A verified ciphertext archive is not necessarily a recoverable application.

### Automation mode

- Explicit `--non-interactive`, `--json`, config path, credential-file paths or approved environment references; no secret values in argv.
- Missing required inputs fail promptly, never block on a password prompt or silently downgrade coverage.
- A generic `--yes` must not bypass same-source refusal, nonempty target, corrupt archive, incompatible versions, missing critical keys or unsupported active side effects.
- JSON on stdout is a versioned report, not progress mixed with messages. Reports avoid raw data/secret values. Human reports and prompts go to stderr as appropriate.
- Exit-code proposal: 0 requested scope passed; 1 operational failure; 2 usage/invalid input; 3 completed with missing coverage/manual requirements; 4 comparison drift; 5 safety/compatibility refusal; 130 user cancellation where supported. Freeze precedence in tests (safety/operational failure outranks drift/incomplete). Every report contains per-component outcomes, so exit codes do not carry all semantics.
- Offline verification has **no source credential requirement and no network calls**. Remote-archive verification naturally needs destination-storage access, not source-project access.

### Configuration and secrets

Profiles contain non-secret identifiers, user-chosen destinations and references to credentials, not plaintext secrets by default. Resolve config/cache/data paths using native platform conventions. Persisted credentials are opt-in via macOS Keychain/Windows Credential Manager after qualification; keychain access denial falls back to prompting, not plaintext saving.

Support a private secret file for automation; document environment-variable tradeoffs. Validate files, permissions and bounds; suppress content in all error chains. Recovery keys/passphrases must remain portable and separately backed up. A password manager reference is an external recovery dependency, not embedded backup material.

## 3. Capability and coverage model

Keep separate fields for: feature **observed**, read permission, capture result, byte integrity, recoverability prerequisites, restore result, behavior verification, and manual actions. Suggested capture states: `not_used`, `captured`, `partially_captured`, `manual_required`, `unsupported`, `permission_denied`, `unknown`, `failed`. Suggested verification states: `not_checked`, `sampled`, `full`, `passed`, `failed`, `inconclusive`, with scope details rather than one overloaded state.

A project-wide success requires all components in the declared support scope to meet their gates. Explicitly excluded features remain listed. Never quietly narrow the requested scope to obtain a green check. An incomplete but useful archive may be finalized as **incomplete**; corrupt/interrupted output must not appear finalized at all.

### Complete coverage inventory

The historical [full product plan](reference/legacy/docs/plans/2026-09-18-supabase-project-archive-design.md) preserves the expanded original tables. The following is the new CLI coverage checklist; each row needs discovery, capture, restore, verification and missing-data behavior.

| Surface | Required handling / limits |
| --- | --- |
| Application schemas/data | Tables, partitions, inheritance, rows, enums/domains/composite types, sequences/identity state, indexes, constraints, comments, views/materialized views, routines, triggers, generated/default expressions and supported large objects. Prove dependency completeness for the supported profile; reject unsupported cases. |
| Application security | Owners, database/schema/table/column/routine/sequence grants, grantors/grant options, role attributes/membership including version-specific flags, altered default privileges, RLS enable/force/policies, security-definer semantics. Do not drop ownership/ACLs just to make restore succeed. |
| Provider-owned `public` | Explicit hosted support gate. Ordinary Supabase apps use it; a non-public-only fixture is not an adequate general alpha. Preserve managed owner/baseline and restore application objects safely. |
| Auth records | User IDs, identities, password hashes, metadata, relationships and supported auxiliary records. Data export through a tested database recipe, not create-user APIs. Test actual password login in target; distinguish sessions, MFA/passkeys and provider-specific records. |
| Auth/session limitations | New signing keys and project endpoints; planned reauthentication and supported session invalidation. MFA encrypted factors and passkey RP/origin binding require targeted tests, possibly reenrollment. Do not promise session continuity. |
| Managed schema modifications | Explicit custom policies, triggers, routines and supported modifications in `auth`, `storage` and other managed namespaces; do not restore their base DDL wholesale. |
| Migration history | Preserve versions, names, nullable statement arrays and seed history where supported. Statements remain data, never replayed after snapshot restoration. Original migration files are optional separate source artifacts. |
| Extensions | Name/version/schema, configuration, dependencies and extension-managed durable data. Check target support before restore. Postgres `pgvector` data is ordinary database data; specialty Storage vectors are different. |
| Cron/webhooks/queues | Job definitions, schedules, owners/timezones, enabled state; webhook triggers/headers; durable queue/message/archive state and consumer dependencies. Restore inactive; never replay pending outbound requests blindly; explain duplicate delivery/visibility-timeout limits. |
| Vault/pgsodium | Ciphertext, metadata/definitions plus source root key exported while source active, encrypted in archive. Key transplant to confirmed unused target only; missing key is a recovery blocker. Distinct from JWT signing keys. |
| Foreign data/external connections | Servers, mappings/options, credentials as sensitive data and references; foreign rows are external, not automatically backed up. Explicit separate export or a gap. Dynamic SQL/runtime links remain uncertain. |
| Replication/Realtime DB | Publications and membership, replica identity, relevant policies. Replication slots, subscriptions, CDC positions and replicas require deliberate recreation; do not copy live positions blindly. |
| Standard Storage buckets | Names/IDs, public/private setting, size/MIME restrictions and service settings; keep target private until release/activation plan. |
| Standard Storage objects | Every page and every original byte, exact key, size, strong digest, content/cache headers, supported user metadata, ownership and identity inventory. Restore via supported Storage/S3 APIs. Reconcile SQL metadata and uploads exactly once. |
| Storage identity | Prove supported owner/ID/timestamp preservation or mapping; identify application references and policies affected by any loss. Service-key uploads alone do not preserve user ownership. Unsupported loss can block restored-app readiness. |
| Storage history | Detect supported current/versioned state. Label current-state-only if historical/deleted versions cannot be exported. CDN caches and unfinished multipart uploads are not durable app data. |
| Vector Storage | Dedicated bucket/index/schema/dimension/distance/vector/metadata export and reconstruction proof; otherwise an explicit blocker/manual external export. |
| Analytics/Iceberg Storage | Catalog, namespaces, schemas, data/metadata files, location remapping and snapshot/history semantics need separate proof. Ordinary file copy is not sufficient. |
| Edge Functions | Inventory, deployed body/package, slug/entrypoint, import/dependency files, shared code/assets, JWT verification setting and required secrets. Prove redeployment without original project/repository access. Downloads may omit `deno.json`/import maps. |
| Function secrets | Names/digests are inventory, not values. Collect actual values from user-approved sources. Regenerate destination-managed `SUPABASE_*` values instead of replaying source credentials. |
| Auth settings | Site/redirect URLs, signup/password/session/rate-limit/linking/CAPTCHA/email/phone/anonymous policy, templates/notifications; preserve supported effective defaults with version mapping. |
| OAuth/OIDC providers | Built-in and custom providers, IDs/options/scopes/endpoints/claims, actual credentials where available; otherwise prompts. External callback updates required. |
| SAML/SSO/third-party Auth | Metadata/XML, domains/mappings, integration config; reconcile restored DB identities with API resources, avoid duplicates; changed ACS/entity IDs/external provider registration may require manual steps. |
| Supabase OAuth server/apps | Server configuration, registered clients/apps, redirects and credential inventory using supported Auth Admin interfaces; preserve/reconcile IDs or report replacements. Not the Management API integration OAuth flow. |
| SMTP/email/SMS | Host/port/user/sender/templates and provider credentials; missing passwords supplied manually. Capture external image/template dependencies. Test delivery only to explicitly designated recipients with consent. |
| Auth hooks/MFA policy | SQL/HTTP definitions, signing secrets, dependencies and enabled flags; restore disabled while unresolved. |
| API/JWT keys | Inventory metadata/key types. New signing private keys are non-exportable; use destination keys, update clients, invalidate sessions/links using tested supported paths. Legacy extractability must not become a blanket cloning promise. |
| Data API/Realtime/Storage service | Exposed schemas/search path/limits; Realtime settings/private-channel authorization; Storage service config. Apply writable allowlists, verify unprivileged behavior. Live connections/presence/broadcasts are ephemeral. |
| Project infrastructure | Source ref/org/region/PG version, compute/disk/pooler/important settings and compatibility requirements. Record/recreate; destination gets new ref/URLs. Capacity/plan restrictions may block restore. |
| Network security | CIDRs, TLS enforcement, pooler/direct routing, IPv6/IPv4 needs, certificate trust. Restore destination-appropriate restrictions without locking worker out; never weaken TLS. |
| Custom domains | Hostnames/DNS/certification/dependencies; new verification challenges, approved cutover and callback changes. No automatic source-domain release. |
| Backups/PITR/add-ons | Record retention/feature intent, not historical physical backup/WAL timeline portability. Enabling paid options requires consent. |
| External integrations | Hosting/CI/GitHub, payment/provider webhooks, env variable names and accountable external owners; app repositories/assets optional explicitly supplied inputs. No external account cloning claim. |
| Logs/monitoring | Log drains/alerts/config and required credentials. Optional bounded log export with retention limits; not replayable state or complete history. |
| Ancillary project assets | Accessible SQL snippets, notes, diagrams, source attachments; visibility may be user-scoped. Never execute archived snippets automatically. |
| Organizations/branches | Membership/policies/access/billing reference only; not automatic cloning. Archive each selected branch separately; unknown/new features remain visible gaps. |

## 4. Go architecture: direct and small

One executable with ordinary internal packages. Start with only the modules needed for each task; the list is a map, not a scaffolding checklist.

```text
cmd/sparc/                  process entrypoint only
internal/cli/               args, prompts, output and exit-code contract
internal/config/            profiles and bounded config parsing
internal/credentials/       hidden input, files, optional native stores
internal/tools/             embedded client inventory, extraction, trusted execution
internal/platform/          Windows/macOS file/process/security primitives
internal/supabase/          explicit HTTP API calls and capability discovery
internal/database/          pgx inspection, recipe, roles/security/history, dump/restore
internal/storage/           bucket/object metadata and source/target file transfers
internal/functions/         deployed functions, dependencies and secrets inventory
internal/archive/           versioned manifest, age streams, integrity and readers
internal/destination/       local/mounted and S3 archive artifact I/O
internal/operation/         fixed backup/restore sequence and durable journals
internal/verify/            offline checks, normalized remote comparison, reports
internal/recovery/          structured manual requirements and activation checklist
```

Use `context.Context` through operations; bounded worker groups/queues for transfers, not a generic workflow engine. Introduce tiny interfaces only at real test boundaries (HTTP, object store, tool process, filesystem), not an interface for every struct. Avoid an app database unless manifest/journal scale proves JSON/JSONL insufficient.

### Candidate stack

- Go standard library: `flag`, `net/http`, `encoding/json`, `io`, `crypto/sha256`, `os/exec`, `embed`, `context`, native path utilities.
- `github.com/jackc/pgx/v5`: catalog inspection and verification, not a replacement dumper.
- `filippo.io/age`: streaming encryption; no external age executable for users.
- AWS SDK for Go v2 S3; evaluate current `feature/s3/transfermanager` against Supabase and chosen remote providers. Older `feature/s3/manager` Uploader/Downloader APIs are deprecated. MinIO Go SDK is an alternative, not a second mandatory SDK.
- `golang.org/x/term` for hidden terminal inputs; `x/sys` where native ACL/process support requires it; dependency choice/pinning follows investigation.
- Native credential-store wrapper only after tests; no mandatory credential persistence.
- GoReleaser optional for CI packaging, never an end-user dependency; signing may use native CI scripts without Pro features.

Pin selected Go/module/client versions and document licenses/security update policy. Do not hardcode tutorial versions or import a whole Supabase CLI as a library. No Rust or Python runtime in the shipped app; old implementations are reference fixtures only.

## 5. Database route and compatibility

Preferred evaluation: official native `pg_dump` custom-format archive and `pg_restore`, plus `psql` only for a reviewed SQL recipe that actually needs it. Custom roles/global state require explicit capture; `pg_dump` is not `pg_dumpall`. A custom archive is inspectable and dependency-ordered but not inherently safe or Supabase-aware.

Determine which managed data/customizations can join the main dump and which need coordinated separate exports. Full-project capture may need multiple artifacts; coordinate a common supported snapshot or enforce a quiet window. Do not automatically translate the upstream multi-script recipe into an unfiltered native dump or fragile SQL regex rewrites.

Detect actual source, client and destination versions. Initially require qualified matching majors; PostgreSQL's own compatibility rules are broader than this conservative support policy. Reject client older than source and destination downgrade. Verify extension and managed Auth/Storage schema compatibility. New server versions require a qualified client payload already bundled in a release; do not silently download executables on first use.

Connect directly or through a qualified session pooler, never transaction pooling. Verify TLS hostname and trust for both pgx **and the subprocess**. Pooler TLS authenticates the pooler; it is not proof of encrypted pooler-to-backend traffic or independent backend identity. Bind expected project ref, route and credentials using tested provider identity observations; record confidence/limits rather than inventing certainty.

Use a direct argv array, fixed executable path, sanitized environment, scoped private passfile and verified trust material. No shell, command-line passwords, ambient `.pgpass`/service/rc configuration, automatic source password reset, upstream dry-run or raw stderr echo.

## 6. Archive contract (design to qualify)

An open, documented, versioned **folder/prefix of encrypted artifacts**, not a private database. Preserve human-inspectable logical SQL/custom dump + JSON/JSONL + function/source + file bytes behind standard encryption.

Example encrypted layout (proposed, not a frozen file format):

```text
backup/
  FORMAT.txt                     minimal public identification/recovery instructions
  payloads/00000001.age           opaque names, never raw Storage keys
  payloads/00000002.age
  ...
  manifest.age                   final authenticated manifest, published last
```

Logical decrypted sections: `database/`, `configuration/`, `storage/`, `functions/`, `manual/`, `secrets/`, `reports/`. Manifest includes format and recipe versions, archive UUID, source identity, capture interval and consistency mode, component scopes/statuses, tool versions/digests, artifact identities/lengths/plaintext hashes, dependencies, required destination capabilities and manual recovery items. Source identifiers and sensitive filenames belong inside encryption.

For large object inventories, use bounded authenticated index shards/JSONL referenced by the root manifest; select actual limits using measurement. Do not hold all file bytes or unbounded lists in RAM. Exact remote object keys are data, not paths: preserve case, Unicode and raw supported key spelling independently of macOS/Windows filename semantics.

### Publication and resume

- New local directory or unique S3 prefix only. Never merge/overwrite a completed archive.
- Private partial-operation state lives separately from the finalized immutable archive; protect its secrets too.
- Finish and close all age writers (check finalization errors), verify stored artifacts, then publish the final manifest exclusively/atomically where supported.
- Manifest encodes `coverage_complete` separately from physical artifact completion. No arbitrary plaintext marker can upgrade incomplete coverage.
- Final remote publication is a protocol across objects, not a POSIX rename. Define collision detection/conditional creation and behavior for providers without those guarantees. Partial prefixes remain incomplete.
- Resume only verified artifacts with matching operation/source/config identity. Never splice a new database snapshot into a prior dump. Restart its phase if snapshot continuity is lost.
- Prefer restart of an interrupted encrypted object over unsafe appending. Evaluate authenticated chunking only if giant-object restart cost justifies it; age streams cannot be naively concatenated and called resumed.
- Support full local staging first where necessary; disclose required disk space. Streaming to remote storage can reduce disk but may conflict with SDK retry/replay and restore seek requirements. Prove bounded encrypted staging and private plaintext exceptions, do not promise zero plaintext everywhere.

### Cryptographic and format guarantees

Encryption is authenticated confidentiality, **not proof of who authored an archive**. A person with the public recipient can create a new encrypted archive. SQL/functions are executable code; accept trusted-origin archives only. Evaluate optional signed archive provenance without making bespoke cryptography or a cloud identity service a prerequisite. Checksums in the same archive are integrity references, not independent authorship receipts.

Use portable recovery material; keychain-only storage is not recovery. Do not promise secure erasure from SSDs, snapshots, backups, cloud-synced directories or memory. Full encryption does not hide total size, object count, timing or recipient-format metadata.

Legacy `sparc-local-artifacts` v1 used individual X25519 age payloads plus `manifest.age`, 16 MiB manifest/100k entry bounds and was not a stable Supabase format. Decide import-only compatibility or a clean new format explicitly; keep age interoperability tests either way.

## 7. Fixed backup sequence

1. Parse inputs and validate local destination/credential boundaries without network side effects.
2. Discover actual project capabilities, sizes, permissions, versions and recovery gaps; reject unsupported critical scope or ask for explicit partial-backup acceptance.
3. Select encrypted destination and portable key material; verify recovery material saved separately.
4. Collect unavailable secrets/source dependencies or record precise unresolved requirements.
5. Explain consistency and costs; operator establishes quiet window where required. Do not pause Supabase as a write-freeze mechanism.
6. Capture compatible database state and auxiliary catalogs under the selected consistency contract. Capture required root key while source active.
7. Copy standard Storage bytes with inventory/metadata/owner mapping, conditional requests/change detection and bounded pagination/concurrency.
8. Capture functions, settings, manual evidence and remaining supported components; record capture times and inter-service drift.
9. Verify archive artifacts, required dependencies, cryptographic completeness and coverage. Publish final manifest last.
10. Report data captured, unresolved requirements, actual checks, costs/size and recommended restore rehearsal. Never offer automatic source deletion.

## 8. Verification semantics

### Offline integrity

No source access; decrypt to authenticated EOF, validate bounded manifest/indexes, check exact required payload inventory, lengths/hashes, format/recipe support and dependency references. Corruption/truncation/wrong key/unknown required version fails. Remote artifact read-back must inspect actual stored bytes rather than trusting upload return values. Archive integrity does not prove capture completeness, original source truth or executable safety.

### Remote comparison

`--against` selects a read-only source or restored target comparison. Report comparisons per scope and actual consistency interval. Normalize only known differences (project ref/endpoints, target-managed keys, supported identity mappings); never broad text replacement or silently ignore security differences.

- Quick: inventory, normalized definitions/security/settings, counts and sizes. Counts/sizes are not content equality.
- Deep: deterministic database-content comparison for qualified types/relations, sequence state, full file-byte hashes where remote trusted hashes unavailable, config and redeployable function contents where comparable. Sampled checks must say sampled.
- Define stable encoding/order, null/duplicate/no-primary-key handling, collation/float/timestamp/JSON/array/binary/extension type behavior and cost budgets before advertising deep DB equivalence.
- Do not hash dump files as the equality test: order/tool headers/compression/internal IDs can differ while logical state agrees.
- S3 ETags are not universally file hashes. Full object re-download may be necessary and incurs egress.
- Live drift is not necessarily bad backup; a new snapshot cannot prove past-state equality. Return `drift` or `inconclusive` honestly.
- Read-only verification must not call arbitrary archived functions, consume sequences or send messages. Behavior tests with writes belong to separately authorized restore validation.

### Recovery rehearsal

Only a real restore with independently recorded checks earns `restore_tested`. Bind results to archive identity, target versions/capabilities, recipe, time and check set. A test against one fixture is not support for every discovered feature. Source access must be removed/denied during this test, including source credentials, APIs, Storage and project-specific dependency downloads.

## 9. Fixed restore sequence

1. Unlock and fully validate archive and executable-origin trust before target mutation. Show scope/gaps.
2. Select destination, refuse same source (including alternate endpoint aliases) and validate new unused project across database/Auth/Storage/functions. Expected managed baseline is independently qualified, not learned from the target and called safe.
3. Check actual versions/extensions/plan limits/disk/quota/roles/owners/default grants and resolve critical missing keys. Recheck immediately before mutation; user prevents concurrent target writers.
4. Produce a recovery preview and old-to-new identity/URL/key map. No source access needed. Decide activation quarantine; abort features whose side effects cannot be suppressed safely.
5. Prepare compatible extensions and reviewed supported roles; transplant Vault root key only under explicit approved unused-target conditions.
6. Restore database application/Auth data, schema, security, sequences, indexes/constraints, managed customizations, migration history and extension data in a tested dependency order. Use fail-fast transactions where possible; validate constraints if loading bypasses triggers/FKs. Never ignore restore errors wholesale.
7. Restore buckets/files/ownership/metadata using the proven reconciliation strategy, with public exposure deferred as needed.
8. Restore actual user secrets and deploy complete functions with destination-managed environment values. Review startup/import side effects and external dependency fetching.
9. Apply supported configuration with writable allowlists, safe ordering and unresolved hooks/providers disabled. Produce external OAuth/SMTP/DNS/hosting tasks.
10. Verify data and permissions with structural and explicitly authorized behavioral checks; keep failed targets quarantined (not exposed/activated).
11. Obtain explicit activation approval for jobs/consumers/hooks/public exposure and external cutover. Activation may need to be manual if the platform offers no safe programmatic quarantine.
12. Write a separate target-bound restore journal/report. Do not modify immutable backup contents. `data restored` is distinct from `application ready`.

There is no global rollback covering SQL, Storage and APIs. Failure around COMMIT can be ambiguous; do not blindly retry. Transaction rollback does not automatically undo sequences or external effects. Preserve diagnostics safely and require an explicit recovery decision; never delete/reset the failed project automatically. After writes begin on target, routing back to source is not lossless rollback.

## 10. Structured manual recovery

Each item has stable ID, component, source (`api`, `database`, `user`, `repository`), sensitivity, capture time, state, why required, permitted replacement strategy, secret reference and exact external action. Examples: `auth.oauth.google.client_secret`, `auth.smtp.password`, `functions.billing.STRIPE_SECRET`, `domain.dns_verification`.

Keep actual secrets in encrypted artifacts or private operation storage, not ordinary report text. `provided` is not `tested`; do not send emails/SMS or invoke functions just to test a credential without permission. Manual exports for inaccessible **data** must be actual recoverable payloads, not a checklist saying someone should reconstruct them later.

Re-run targeted prompts at restore; allow supplied replacement credentials without altering immutable source history. Authentication using the new project should use new keys and explicit provider callback configuration. Resolve source URLs only in known structured fields; scan code/SQL/text for review but never global-replace user data.

## 11. Support and maintenance contract

Maintain a machine-readable supported-version/feature matrix, archive migration policy and clear release notes. New Supabase fields/endpoints become unknowns until classified; GET responses cannot simply be replayed as PATCH payloads. Preserve encrypted raw responses where safe/legal plus normalized replayable fields for later recovery, with size bounds and secret classification.

Network handling: explicit timeouts, bounded retries/backoff for idempotent reads, `Retry-After`, token expiry, per-operation cancellation, pagination exhaustion detection, no credential forwarding to untrusted redirects. Permission/service failures remain distinct from absence. Retry uncertain mutations only after checking actual target state with a qualified idempotency rule.

Long runs handle disk full, quota, Windows locks, network mount removal, sleep/wake, rate limiting and interruption. Never disable OS sleep/security or change source settings silently. Report how to resume or restart a phase and what private state remains.
