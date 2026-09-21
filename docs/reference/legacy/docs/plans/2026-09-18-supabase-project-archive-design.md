# Supabase Project Archive & Restore — High-Level Product Plan

**Status:** High-level design draft; Tauri and macOS-first delivery approved. React SPA with shadcn/ui is the working UI choice. A local Rust artifact-packaging prototype is implemented; the GUI and live Supabase export/restore are not yet implemented.

**Research date:** September 18, 2026.

**Goal:** Help a non-developer preserve as much of a Supabase project as possible in a portable folder, then reconstruct it in a new Supabase project without needing the original project to remain available.

## 1. Product direction

Build a **local-first archive-and-recovery assistant**, not merely a database-dump wrapper. Its main actions are:

1. **Create an archive.** Discover the project, copy everything supported, and guide the user through collecting missing information.
2. **Check an archive.** Verify integrity, explain what it contains, and identify anything still needed for recovery.
3. **Restore an archive.** Prepare a new project, restore in a safe order, help reconnect external services, and verify the result.

**Selected direction:** A Tauri desktop GUI, initially for **macOS**, with a small shared Rust engine that can also power a CLI. Use a **React + TypeScript + Vite SPA with shadcn/ui** for the interface. The GUI is the primary product for non-developers; the CLI is useful for large projects, remote servers, testing, and eventual scheduling. Do not build two independent implementations.

The product promise should be **“a portable recovery package with explicit coverage and verified restoration”**, not “a perfect clone of every Supabase system.” Some keys cannot be extracted, external services are outside Supabase, and a new project necessarily has new infrastructure identifiers.

### Initial assumptions

- Hosted Supabase is the source and destination. Self-hosted Supabase is a future compatibility target, not an implicit promise.
- A restore targets a newly created, otherwise unused project, possibly in another organization or region.
- Each archive covers one project/branch. Additional branches are separately selectable archives, not silently assumed to be included.
- The archive can be inspected offline. Restoration still requires connectivity to Supabase and any external services being reconnected.
- Full independent archives come first. Incremental backups, continuous replication, zero-downtime migration, and point-in-time recovery are not initial requirements.
- The initial validated size target is a database up to **10 GB** and stored files up to **100 GB**, selected by the user. This is a test/support envelope, not a hard archive-format limit or a performance claim; object-count limits and recovery-time measurements remain to be established.
- The source is not deleted, paused, reset, or otherwise changed by default.

## 2. Important findings from current documentation

1. **Much configuration is automatable.** The Management API exposes Auth, SMTP fields, PostgREST/Data API, database/pooler, Realtime, Storage, network controls, functions, SSO, custom-domain operations, and more. A separate universal “Supabase admin CLI” is not necessary. The official CLI, Management API, Postgres tools, Storage APIs, and selected Auth Admin APIs cover different parts of the job. [S2]
2. **Configuration is not necessarily recoverable credentials.** The new aggregate `/v2/projects/{ref}/config` endpoint is alpha and explicitly returns Auth secrets as HMACs, not plaintext. Edge Function secret listing exposes digests rather than usable secret values. Readable names or fields named `value` must not be mistaken for exported secrets. The precise behavior of each supported endpoint/version needs a tested contract. [S3, S8]
3. **Vault and encrypted columns need an additional key.** The CLI migration guide documents exporting and importing the project’s pgsodium root key. SQL backups do not contain it. Retrieve it while the source is active; encrypt it in the archive. This is distinct from a JWT signing key. The root-key endpoint is beta/experimental. [S1, S2]
4. **A default database schema dump is not the whole project.** Managed schemas, extension-owned objects, data, roles, migration history, and user modifications to `auth`/`storage` need deliberate handling. Auth user records can be migrated, but Auth service configuration is separate. [S1, S4]
5. **Storage requires actual object bytes.** Database metadata alone is not a file backup. Storage ownership matters to RLS; uploading everything with an administrator credential is not by itself proof of equivalent access control. [S1, S9, S10]
6. **Function download is not necessarily a complete source export.** The migration guide warns that downloads omit import maps and `deno.json`; dependency and source recovery must be checked, not assumed. [S1]
7. **New JWT signing private keys are non-exportable.** Inventory their configuration, but normally generate/use destination keys. Plan for client updates and reauthentication instead of promising session continuity. [S7]
8. **Supabase’s native clone is not a portable archive.** It remains useful as a complementary recovery option, but it does not copy all service configuration or Storage files. Its physical restore can also start copied cron/external-operation jobs immediately. A controlled logical restore is a better basis for this product. [S5]
9. **New Storage products need separate support.** Vector and analytics/Iceberg buckets are not ordinary file buckets. The current migration example explicitly excludes vector bucket/index metadata; a generic dump-and-file-copy process must not claim to cover those datasets. [S1, S15]

These findings are documentation-backed, not results of a live project migration. A feasibility phase must establish supported, round-trip-tested versions before advertising full coverage.

## 3. What the archive should cover

Legend:

- **Automatic:** A documented API/tool can perform the main operation; still subject to permissions, compatibility, and validation.
- **Hybrid:** Automatic inventory/export plus supplied credentials, external action, or special restore handling.
- **Record/recreate:** Preserve configuration and instructions, but do not claim an exact portable copy.

### 3.1 Database and database-powered features

| Surface | Capture approach | Restore approach / limits |
|---|---|---|
| Application schemas and data | Supabase-aware logical export: tables, partitions, views, materialized views, routines, types, sequences, indexes, constraints, comments and large objects where supported. | **Automatic.** Restore dependency order, refresh derived data where necessary, and verify sequence values and constraints. |
| Security and custom database roles | Roles, membership, grants, owners, default privileges, RLS enabled/forced flags, policies, and security-definer routines. | **Hybrid.** Do not recreate reserved platform roles or temporary CLI roles. Set new custom LOGIN-role passwords; verify that target defaults did not broaden access. |
| `auth` data | Explicitly include supported Auth user, identity, password-hash, metadata, and related records through the tested Supabase logical migration path. | **Hybrid.** Preserve user IDs and application foreign keys. Avoid rebuilding users solely with create-user APIs, which is not equivalent. Auth schema/version compatibility is a gate. |
| Custom changes to managed schemas | Separately inventory/export user-added policies, triggers, functions and other supported changes in `auth`, `storage`, and relevant managed schemas. | **Hybrid.** Reapply user changes against destination-owned base schemas; do not overwrite managed schemas wholesale. |
| CLI migration history | Separate `supabase_migrations` history export; optional import of original migration files from a repository/folder. | **Automatic/history; hybrid/source.** History records do not guarantee recovery of the original repository. Reconcile history without rerunning already-restored schema migrations. |
| Extensions | Names, versions, schemas, configuration, dependencies, and supported extension-owned data. | **Hybrid.** Install destination-supported versions before dependent objects; unsupported extensions or downgrades are blockers, not ignored warnings. |
| Cron and scheduled jobs | Explicit job definitions, schedules, enabled state, owner, timezone assumptions and secret/endpoint references. | **Hybrid.** Restore inactive. Review destinations and enable only after approval. Job execution history is optional evidence. |
| Database webhooks / `pg_net` | Triggers, functions, request targets, headers, credentials and necessary extension configuration. | **Hybrid.** Preserve definitions, but suppress outbound effects during recovery. Pending network requests are not blindly replayed. |
| Queues / `pgmq` and similar modules | Queue definitions, durable messages, archive tables, configuration and consumer dependencies, explicitly accounting for extension dump exclusions. | **Hybrid.** Restore messages with consumers stopped; explain duplicate-delivery and visibility-timeout implications. |
| Vault / `pgsodium` encryption | Encrypted records, key metadata, encrypted-column definitions, and encrypted export of the source root key. | **Hybrid, critical.** Install the matching root key in an unused destination before encrypted data is consumed. Missing keys block a fully recoverable verdict. |
| Foreign data wrappers and external databases | Wrapper extensions, servers, user mappings, foreign-table definitions and external credential references. | **Hybrid.** External rows are not local database rows and are not automatically archived. Offer an explicit separate dataset export or record them as external dependencies. |
| Realtime publications / other replication | Publication definitions, table membership, replica identity, and supported replication configuration. | **Hybrid.** Recreate publications; recreate subscriptions, replication slots, CDC connections and read replicas deliberately. Live replication positions are not portable logical-backup state. |

### 3.2 Authentication, authorization, and credentials

| Surface | Capture approach | Restore approach / limits |
|---|---|---|
| General Auth configuration | Management API: signup rules, site URL, redirects, email/phone/anonymous sign-in, password rules, token/session settings, rate limits, account linking and CAPTCHA settings. | **Automatic for readable/writable fields.** Reconcile against destination capabilities and effective defaults. |
| Built-in OAuth providers | Enabled providers, client IDs, provider-specific URLs/options and secret availability. | **Hybrid.** Collect unavailable secrets from the user’s original secret store or provider console. Update external callback URLs before enabling. |
| Custom OAuth/OIDC providers | Provider identifiers, issuer/discovery or endpoint URLs, scopes, claim mappings/options, accepted client IDs and secret references. | **Hybrid.** Use supported Auth Admin interfaces where available; otherwise a guided dashboard card. Preserve identifiers used by clients. |
| SAML SSO / third-party Auth | Provider metadata/XML, domains, mappings, issuers and supported integration configuration. | **Hybrid.** Reconcile DB-restored identities with management objects rather than creating duplicates. New SAML entity IDs/ACS URLs and provider IDs may require external changes. |
| Supabase as an OAuth server | Server settings, authorization path, registered apps/clients, redirect URIs, client types and credential inventory. | **Hybrid.** Use supported Auth Admin APIs. Preserve/reconcile IDs where supported; regenerate unavailable client secrets and update dependent apps. |
| SMTP | Host, port, username, sender name/address, frequency controls and credential availability via Auth configuration. | **Hybrid.** Collect missing password separately; verify sender-domain setup at the mail provider. Sending a test email requires consent. |
| Email templates and notifications | Subjects, HTML/text where exposed, links and enabled notification types. | **Automatic/hybrid.** Store template content as data, not executable UI HTML. Referenced images/assets hosted elsewhere need separate capture or dependency records. |
| SMS, MFA and passkeys | SMS provider config, templates, MFA/passkey policy, relying-party/origin settings and available Auth records. | **Hybrid.** Verify encrypted factor data and origin binding compatibility. Do not promise seamless MFA/passkey migration; document possible reenrollment. |
| Auth hooks | SQL/HTTP hook definitions, enabled flags, functions, endpoint URLs and signing-secret references. | **Hybrid.** Restore dependencies first; do not enable a hook whose destination or secret is unresolved. |
| API keys / JWT signing | Inventory key type, label, purpose, algorithm, status and public information; distinguish legacy and newer key models. | **Record/recreate by default.** Destination gets its own API and signing keys. Existing sessions, signed URLs and outstanding email links may become invalid. Explicitly invalidate restored session state using a supported path before exposure. |
| Non-exportable credentials | Track each missing/redacted/digest-only value and the exact recovery action. | **Manual.** “We found a secret named X” is not “we backed up X.” Do not deploy a secret-exfiltration function or weaken source security to recover values. |

### 3.3 Storage, Edge Functions, and runtime services

| Surface | Capture approach | Restore approach / limits |
|---|---|---|
| File buckets and settings | Bucket names/IDs, intended public/private state, MIME restrictions, size limits and applicable service settings. | **Automatic.** Keep public exposure off until validation; restore intended settings during activation. |
| File objects | Enumerate every page; stream original bytes with key, size, checksum, content/cache headers, supported user metadata and ownership inventory. | **Automatic/hybrid.** Upload via Storage API or supported S3 protocol, not raw database inserts or backend filesystem copying. Reconcile metadata exactly once. |
| Object identity and ownership | Preserve object IDs, `owner_id`, timestamps and any application references as recovery metadata. | **Hybrid.** Prove which values survive the chosen upload/metadata strategy. If ownership or IDs cannot be retained through a supported path, report the exact loss and block affected RLS/application checks. |
| Versions and special object state | Detect versioning capability, deleted versions, and any non-current state exposed by the service. | **Conditional.** If only current objects can be exported, label the archive as current-state-only. Incomplete multipart uploads and CDN caches are not normal recovery targets. |
| Vector buckets | Dedicated API discovery, bucket/index settings, dimensions, distance metric, vectors and metadata. | **Conditional adapter.** Prove enumeration and reconstruction. Otherwise collect an external export with instructions and mark coverage incomplete. Postgres `pgvector` tables remain ordinary database data. |
| Analytics / Iceberg buckets | Catalog, namespace, table schema, data and metadata-file inventory, supported snapshot/history information and external locations. | **Conditional adapter.** Use compatible Iceberg tooling and validate location/catalog remapping. File copying alone does not establish a usable restored catalog. |
| Edge Function deployments | List and download every deployed function; save entrypoint, slug, verification settings, imports, deployment metadata and content fingerprints. | **Automatic/hybrid.** Deploy to the destination and compare intended authentication behavior, not merely deployment success. Historical deployment versions are not promised. |
| Function source and dependencies | Prompt for repository/folder or missing import maps, `deno.json`, lockfiles, shared code, static assets and build instructions. | **Hybrid.** Verify a redeployable package. A deployed bundle is not necessarily the original source tree; unavailable dependencies can block later recovery. |
| Function secrets | Inventory names/digests; collect usable values from approved local files or manual entry into encrypted storage. | **Hybrid.** Recreate user secrets. Let Supabase supply destination-managed `SUPABASE_*` values; do not replay source project keys into them. |
| Data API / PostgREST | Exposed schemas, search path, row limits and other documented readable/writable configuration. | **Automatic.** Reapply only supported writable settings and validate access with non-admin clients. |
| Realtime service | Limits, private-channel policy, presence/broadcast settings and authorization policies. | **Automatic/hybrid.** Restore alongside publication/table settings. Connections, presence and in-flight broadcasts are ephemeral, not archived sessions. |

### 3.4 Platform and external dependencies

| Surface | Capture approach | Restore approach / limits |
|---|---|---|
| Project infrastructure | Name/ref, organization, region, Postgres version, compute, disk/autoscaling, pooler and relevant database configuration. | **Hybrid.** Select compatible destination settings; new project ref/hostnames are unavoidable. Pricing and availability need fresh checks. |
| Network and connection security | Allowed CIDRs, SSL enforcement, connection modes, certificates/trust requirements, IPv4 add-on and applicable access controls. | **Hybrid.** Reapply destination-appropriate restrictions without locking the restore worker out. Never disable TLS as a shortcut. |
| Custom domains / vanity names | Hostname, configuration/status, DNS observations, registrar/provider, ownership and service dependencies. | **Hybrid.** Recreate/verify with Management API or CLI where supported. Obtain new verification records and certificates; do not blindly reuse old challenges. Domain transfer requires explicit cutover approval. |
| Backups, PITR, replicas and add-ons | Inventory enabled features, retention/schedule intent, replicas and relevant plan requirements. | **Record/recreate.** Historical physical backups/WAL/PITR timelines are not exported by a logical project archive. Paid additions require approval. |
| GitHub, hosting, CI/CD, external integrations | Connection inventory, repositories, deployment settings, app environment-variable names, webhook endpoints, responsible account and documentation. | **Hybrid/manual.** Reconnect through the external service; Supabase cannot export an entire Vercel/Netlify/GitHub/payment-provider account. |
| Logs, log drains and monitoring | Config, alert destinations, credential requirements; optionally export accessible logs over a specified time range. | **Hybrid.** Logs are evidence, not replayable state; retention/API limits apply. Never claim complete historical logs. |
| Dashboard SQL snippets / ancillary assets | Export accessible project-related snippets through documented endpoints; optional notes, schema diagrams, screenshots and repository material. | **Hybrid.** Snippet visibility is user-scoped, not guaranteed project-global. Do not execute archived snippets automatically. |
| Organizations and team permissions | Reference inventory of roles/members, organization policies, project access and required plan features where authorized. | **Record/recreate.** Billing, memberships, organization SSO and ownership are not project data and are not automatically cloned. |
| Branches and unknown features | Enumerate branches and detect newly exposed configuration; ask about additional services the app cannot discover. | **Hybrid.** Archive selected branches separately. Unknown features become explicit gaps, never silently disappear. |

## 4. Automation and credential strategy

### Use the simplest reliable interface per surface

1. Documented Management API for control-plane configuration and supported project operations.
2. Official Supabase CLI for Supabase-specific export/download/deploy operations where it adds value.
3. Version-compatible `pg_dump`, `pg_dumpall`/role handling, `psql` or `pg_restore` for bulk database work through a tested Supabase-aware recipe.
4. Storage REST/S3 APIs for files; Auth Admin APIs for supported Auth administrative resources.
5. Structured manual collection when the supported interfaces are insufficient.

Do not make dashboard scraping, private Studio APIs, MCP servers, or an AI agent mandatory dependencies. Optional browser assistance can open the correct page, but deterministic exports and explicit user approval should drive the product.

### Connection wizard

Explain that these are different credentials, not interchangeable “Supabase keys”:

- A Management API token for project configuration.
- A database connection credential for complete database exports/restores.
- A suitable project-level secret/service credential or S3 credential for Storage/Auth administrative operations as needed.
- Optional destination credentials for remote archive storage.

Request the least privilege supported by each operation, distinguish source-read from target-write needs, and show why an elevated permission is necessary. A read request can still require a powerful permission: root-key retrieval is one example. A denied endpoint must be “permission missing,” not “feature absent.”

Start with a guided access-token flow and OS credential storage. Do not reset a source database password automatically: that can break the live application. Offer an explicitly approved temporary credential or manual password recovery path when necessary.

A future “Connect Supabase” OAuth experience needs separate investigation. Supabase’s documented **Management API integration** token exchange currently requires a client secret even with PKCE. Do not embed that secret in a distributed desktop binary; use an approved public-client flow if available or a minimal trusted token broker. This differs from a project’s own Auth OAuth server. [S11]

### Version and capability handling

- Pin tested tool versions and record them in the archive.
- Distinguish readable, writable, secret, redacted, immutable, derived and unknown fields.
- Keep encrypted original API responses for future recovery, plus a normalized replayable configuration.
- Apply an allowlist of supported writable fields; do not PATCH an entire GET response back blindly.
- Preserve effective defaults, but flag source defaults the destination cannot reproduce.
- Treat 403, 404, 429 and service/version mismatches differently; respect rate limits and retry transient failures with bounded backoff.
- The alpha aggregate configuration endpoint is a useful supplement, not a required single point of failure.

## 5. Archive wizard: the user experience

### Step 1 — Connect and choose a project

Show project name, organization, region and a clear source badge. Test connectivity and permissions. Display plain-language issues with an “Open the right page” action instead of terminal instructions.

### Step 2 — Scan and explain coverage

Discover features, estimated database/file sizes, object counts, credentials needed and external dependencies. Present a checklist such as:

- Database and user accounts: can be copied.
- Uploaded files: 18 GB to download.
- Google sign-in: client secret still needed.
- Custom domain: settings can be saved; DNS changes will be needed during restore.

These are illustrative UI messages, not findings about a real project. Offer “Everything we can capture” by default. Any exclusions remain prominently recorded.

### Step 3 — Choose where and how to save

Choose local disk, external disk or mounted network folder initially. Provide a friendly remote-destination form for SFTP and S3-compatible storage in a later phase, without changing the archive format. Remote object storage is represented as an archive prefix, not misleadingly described as a POSIX folder.

Check available space, filesystem limits, connectivity and estimated transfer/egress costs. Encryption takes place before remote transfer. An optional second copy should be independently verified.

### Step 4 — Choose encryption

Recommend full encryption, explain the passphrase/recovery-key requirement, and verify that the user has saved the recovery material somewhere separate from the archive. See Section 8.

### Step 5 — Fill only the gaps

Prepopulate everything already readable. Show short, conditional interview cards for unresolved settings, secrets and external systems. Allow “Save and finish later,” “I don’t know,” and “Not used,” with an honest incomplete status.

### Step 6 — Choose a consistency level

Offer:

- **Recommended for migration/decommissioning: quiet-window archive.** Guide the user to stop application writes, uploads, signups, jobs, consumers and external writers while the final export occurs.
- **Convenient live archive.** Keep the app running; explain that different services are captured at different moments. Detect changed/disappearing files and configuration drift and retry or flag them.

Postgres can provide a consistent database snapshot; there is no implied atomic snapshot across Postgres, Storage, functions and external configuration. Coordinate related database export passes with a supported snapshot strategy or a verified quiet window. Never claim consistency just because all individual commands succeeded.

Do not use “pause the Supabase project” as the write-freeze mechanism: pausing can make the project unavailable for export. Any deliberate source-side configuration change needs its own confirmation and recovery instructions.

### Step 7 — Copy with useful progress

Show phase, objects/bytes completed, estimated time, current issue and safe cancellation. Stream large payloads, use bounded concurrency, paginate all collections and checkpoint completed work. A lost connection or app restart should not discard completed verified objects.

Database snapshot work may need restarting after interruption; do not splice together chunks from different snapshots. Tell the user which phase must restart and why.

### Step 8 — Verify and produce the recovery report

Verify artifact integrity, required metadata, known gaps and secret availability. Distinguish:

1. **Files verified** — archive bytes are readable and intact.
2. **Recovery requirements satisfied** — all known required components and credentials are accounted for.
3. **Restore tested** — an actual restore into a disposable/new project passed named checks on a recorded date/version.

The result might be “Archive verified; one OAuth secret is missing.” Never reduce completeness to a reassuring percentage that hides a critical missing key. Offer a restore rehearsal before the source is retired; do not offer automatic source deletion.

## 6. Manual collection: precise, resumable forms

Each card includes:

- Why the information matters and whether it blocks recovery.
- A project-specific dashboard link plus a named menu path.
- The exact fields to collect, with examples that contain no real secrets.
- Secret inputs with deliberate reveal/copy controls and encrypted autosave.
- An external provider link when Supabase cannot reveal the required value.
- Optional notes/attachments, “Not applicable,” “Can’t find it,” and “Ask someone for help.”

Dashboard labels and routes change. Maintain versioned instructions with a last-reviewed date; use the links below as verified documentation starting points, not permanent route contracts.

| Interview card | Where to look | What to collect when not already exported |
|---|---|---|
| Sign-in providers | **Authentication → Providers**, `/auth/providers`; then Google/GitHub/etc. developer console | Enabled provider, app/client ID, missing secret, options/scopes, callback URLs, provider account owner. A masked dashboard field is not recoverable plaintext. |
| SMTP / outgoing email | **Authentication → SMTP Settings**, `/auth/smtp`; mail-service console | Host/port, username, password, sender, provider account and sending-domain verification. |
| Email appearance | **Authentication → Email Templates** | Missing subjects/template content, externally hosted images and custom links. |
| Login URLs and security | **Authentication → URL Configuration**, relevant security/rate-limit settings | Site URL, allowed redirects, CAPTCHA credential/site-key references, mobile deep links and any unsupported custom settings. |
| SMS and MFA | **Authentication → Providers / MFA** and SMS-provider console | Provider credentials, sender/service IDs, templates, recovery/re-enrollment requirements. |
| SSO, custom providers, OAuth Apps | **Authentication → SSO / Providers / OAuth Server / OAuth Apps**, depending on enabled features | IdP metadata, issuer, mapping, app settings, non-exportable secrets and who can authorize external changes. |
| Auth hooks | **Authentication → Hooks** | Hook endpoints, signing secrets, responsible system and required destination changes. |
| Function secrets | **Edge Functions → Secrets**, `/functions/secrets`; original `.env`, deployment platform or password manager | Required names and actual values. Digests cannot be reversed; offer replacement at restore time without rotating the live source automatically. |
| Function source | **Edge Functions** plus original repository/folder | Missing dependency files, shared code/assets and any build instructions; validate imported filenames and contents. |
| Custom domain | **Project Settings → General → Custom Domains**, `/settings/general`; DNS-provider console | Domain, current records, provider/account, TTL and transfer responsibilities. New validation challenges are generated during restore. |
| Network/database settings | **Connect**, **Database → Settings / Extensions / Roles / Publications** | Unavailable settings, custom-role credential replacement plan, client connectivity requirements. |
| Integrations and observability | **Integrations**, **Project Settings → Log Drains**, external hosting/CI console | Service/account, repository, webhook/monitoring config, required credentials, environment variable names and reconnection instructions. |
| Other features | Branches, organization settings, Storage specialty buckets and an “Anything else?” card | Export files for unsupported datasets, external dependencies, responsible contacts and limitations. |

Save answers as structured, schema-versioned data, not just screenshots. Each item records its source (`api`, `cli`, `database`, `user`), capture time, evidence, verification state, sensitivity and restore dependencies. An encrypted secrets file holds values; ordinary answer records hold references.

Example non-secret answer record:

```json
{
  "id": "auth.smtp.password",
  "status": "provided_unverified",
  "source": "user",
  "secret_ref": "secrets/auth.smtp.password",
  "required_for": ["auth.email_delivery"],
  "instructions_version": 1
}
```

“Provided” does not mean “tested.” A secret reference to a password manager is not itself a self-contained backup; the completion report must distinguish embedded recovery material from an external recovery dependency.

## 7. Portable archive format

Use an openly documented, versioned folder format. Favor ordinary SQL, JSON and source files over a proprietary database. A conceptual **decrypted** layout:

```text
archive/
  manifest.json
  README-restore.md
  database/
    roles.sql
    schema.sql
    data.sql
    managed-schema-customizations.sql
    migration-history/
    extension-data/
  configuration/
    project.json
    auth.json
    storage.json
    realtime.json
    data-api.json
    infrastructure.json
    original-api-responses/
  storage/
    buckets.json
    objects.jsonl
    payloads/
    specialty-exports/
  functions/
    inventory.json
    packages/
  manual/
    answers.json
    attachments/
  secrets/
    recovery-material.json
  reports/
    coverage.json
    verification.json
    external-actions.md
```

This is a logical view, not a requirement to stage plaintext files. In full-encryption mode, sensitive names, the manifest and all contents reside inside encrypted artifacts with opaque external names. Only minimal format/decryption instructions are public.

The manifest records:

- Archive UUID, format/schema version and capture start/end times.
- Source identifiers, versions and detected capabilities.
- Tool versions, export settings and consistency mode.
- Artifacts, byte sizes, cryptographic checksums and relationships.
- Per-component capture status, recovery fidelity and unresolved actions.
- Required destination features/versions and dependencies.
- Credential states: captured, user-supplied, digest-only, unavailable, regenerate, or external reference.
- Explicit exclusions and verification/rehearsal results.

Use opaque payload filenames with a metadata mapping rather than raw Storage object keys. This avoids Windows reserved names, case collisions, Unicode normalization conflicts, path length issues and path traversal. Preserve the original remote key exactly in metadata.

Write checkpoints separately while building the archive; publish a completion marker only after the manifest and all required artifacts are verified. For remote destinations, upload the final manifest/marker last. Incomplete archives remain inspectable but cannot masquerade as complete.

Keep restoration journals separate from the immutable archive, bound to its ID and the destination project. The archive can support multiple independent restores.

## 8. Encryption and security

### User-facing choices

1. **Encrypt everything — recommended.** Protect database data, file contents, names, configuration, source code, manual answers and secret material.
2. **Encrypt selected sections — advanced.** At minimum protect credentials, keys and user-entered secrets; offer whole database, Storage, functions and metadata sections as additional selections. Explain exactly what remains readable.
3. **No encryption — explicit warning.** Intended for disposable/test data or a user-managed secure environment. It is not described as safe merely because a password field is omitted.

A “secrets-only” archive can still expose personal data, password hashes, source code, webhook credentials embedded in SQL, and private uploads. Classify original API responses, user attachments and free-text notes as sensitive by default. Simple secret-name scanning is a warning aid, not a guarantee. Selective encryption should operate on complete artifacts/sections, not fragile text replacement inside SQL dumps.

### Implementation requirements

- Use an established authenticated-encryption format/library, such as age; do not invent cryptography or use weak ZIP passwords. Final packaging/key UX is a feasibility decision.
- Support a friendly passphrase flow and separately stored recovery material; advanced recipient/public-key encryption is useful for teams and headless backups.
- Compression precedes encryption where appropriate. Encrypt streams/chunks before disk or remote transmission, with bounded memory use.
- Resume must preserve authenticated chunk boundaries and safe nonce/key handling through the selected library. Verify archive metadata integrity as well as payload hashes.
- Perform a decrypt/read-back check. Be clear that losing all decryption credentials makes recovery impossible; an OS-keychain-only password is not disaster recovery.
- Store operational credentials in the OS credential store when the user opts in; otherwise use memory. Do not archive the user’s broad management token by default.
- Keep secrets out of process arguments, debug logs, telemetry, crash reports and normal support bundles. Minimize renderer exposure and avoid persistent browser storage for credentials.
- Where external tools require temporary files, use narrowly permissioned private storage, minimize lifetime, and disclose limitations. Never promise secure deletion on SSDs, snapshots or synced folders.
- Encrypt before sending to SFTP/S3, validate SSH host keys/TLS and avoid public ACLs.
- Treat archives as untrusted input: enforce extraction bounds, reject traversal/symlinks where inappropriate, and never execute bundled shell scripts. SQL and restored functions are executable code; only restore trusted archives to approved targets. Encryption does not establish trusted authorship.
- Plain checksums detect accidental damage, not malicious rewriting of an unencrypted archive. Do not conflate those guarantees.
- No project data or secrets go to an AI service by default. No cloud account for this application is required for the token-based desktop flow.

## 9. Friendly restore wizard

### Step 1 — Open and check the archive

Select the folder/remote archive, unlock it, verify artifacts and show what will be restored. Refuse corrupt, incomplete-required-component, unsupported-format or incompatible archives before making target changes. Allow inspection and a clearly limited partial-recovery plan where safe.

### Step 2 — Choose a new destination

Offer **“Create a new project for me”** through the Management API or **“Use the empty project I just created.”** Ask for organization, name, region and required paid features. Show cost implications and obtain explicit creation/purchase approval.

Compare source and target identities and reject the source project as a target. “Empty” means the known platform baseline, not literally zero tables. Detect pre-existing application data, users, functions and Storage objects; refuse non-empty targets in the initial product rather than attempting a dangerous merge.

Validate Postgres/Auth/Storage compatibility, extensions, quotas, disk, region/residency and credentials. No automatic PostgreSQL downgrade and no assumption that all custom settings are supported by the destination plan.

### Step 3 — Preview changes and finish missing information

Show:

- Automatic actions and their dependencies.
- Items requiring the user or an external administrator.
- Secrets to provide or regenerate.
- Old-to-new project IDs, URLs, keys and callback changes.
- Features intentionally kept disabled until validation.

Separate **data restored** from **application ready**. If an essential OAuth secret is missing, users might still inspect restored data, but the app must not claim the original login flow works.

### Step 4 — Prepare a safe destination

Keep clients disconnected and delay public Storage access, schedules, consumers, hooks and outbound integrations. Prepare supported extensions and prerequisites. Prevent unintended inherited privileges. Import the archived encryption root key only into a confirmed unused target and before encrypted data is relied upon; this key replacement needs a prominent explanation because overwriting another key can strand existing ciphertext.

Quarantine must be tested per component. There is no universal “disable every possible side effect” switch: fail closed when the app cannot safely suppress an enabled feature.

### Step 5 — Restore in dependency order

Use a fixed, testable sequence rather than an extensible orchestration framework:

1. Destination compatibility, baseline security and required extensions.
2. Encryption prerequisites, supported roles and schema objects.
3. Application/Auth data, sequences, constraints/indexes and explicit extension-owned data; retain inactive automation.
4. Managed-schema customizations, migration history and security reconciliation.
5. Storage buckets, object bytes and supported metadata/ownership reconciliation.
6. User secrets and redeployable Edge Functions with intended verification configuration.
7. Auth/SMTP/SSO/hooks, Data API, Realtime and other service settings, initially disabled where dependencies are unresolved.
8. External provider configuration, new API/key distribution and controlled domain cutover.
9. Verification, then explicitly approved activation of automation and public/client access.

Actual SQL ordering follows PostgreSQL dependencies and the supported Supabase recipe. Where appropriate, use fail-fast transaction-based restoration. If triggers/FK checks are bypassed during load, validate constraints and encryption-sensitive trigger behavior afterward. API and file operations cannot be part of one SQL transaction; there is no claim of global rollback.

Do not blindly restore Storage metadata and then independently recreate conflicting metadata through uploads. Likewise, reconcile SSO/OAuth resources that may exist in restored Auth data instead of creating duplicate IDs. Both need proven, version-specific ownership of each restore step.

### Step 6 — Reconnect external systems

Present one card at a time: add the new OAuth callback, update a SAML connection, update app environment variables, register a webhook, configure DNS, or reconnect a log drain.

Automatically rewrite only known structured destination-specific fields. Scan SQL, source and selected text data for source URLs/IDs and report matches for review. Never perform global search-and-replace across user data, encrypted blobs or code. Produce a human-readable change list and optional destination environment-file template, with secrets handled separately.

Domain reassignment may require releasing the source binding, incurs DNS/certificate delays, and can interrupt login. Prefer validation on the new project URL or a temporary domain before production cutover. Keep an explicit cutover checklist and rollback limitations.

### Step 7 — Verify behavior, not just commands

- Database object inventory, row counts appropriate to the dataset, sequences, constraints and extension data.
- Grants/default privileges/RLS comparison, plus positive and negative tests using unprivileged/anonymous/authenticated identities.
- Auth IDs and relationships; a designated test account login, OAuth/SSO checks and MFA/passkey validation where applicable.
- Storage object counts/bytes and cryptographic checksums where obtainable; private/public behavior and owner-based policy checks.
- Function deployment and safe user-approved smoke tests, including expected JWT behavior.
- Realtime publications/subscriptions and authorization checks.
- Vault decryption check that reveals neither the key nor secret value in reports.
- Targeted configuration comparison, with intentional differences listed.
- Optional SMTP/SMS delivery test to an explicitly chosen address/number; never message all restored users.
- Safe activation and observation of selected jobs only after user approval.

Distinguish structural, sampled and full verification; show the cost/time trade-off for large datasets. A successful HTTP status or function deployment is not proof of functional equivalence.

### Step 8 — Finish or recover from failure

Provide a named-check report, unresolved tasks, new project connection details and an application cutover checklist. If a step fails, stop dependent actions, explain the issue and preserve evidence without leaking secrets.

Resume only verified idempotent steps. An interrupted transactional database load can be rolled back/restarted; larger multi-stage loads may need a fresh target. Offer “Start again in a new project” rather than automatically dropping data or deleting the failed target. Cleanup, incurred billing and any deletion need explicit confirmation.

Keep the original project available until the user has validated the application. After clients start writing to the destination, pointing them back to the source is not a lossless rollback; reconciliation may be necessary.

## 10. Reliability and scale

- No whole-database or whole-bucket buffering in RAM; bounded disk/temp use is part of preflight.
- Paginate everything, including deeply nested Storage listings; verify more than 1,000 objects and buckets/functions exceeding a single page.
- Detect object changes between listing and download using available version/conditional-request information; record the actual captured version/time.
- Do not treat an S3 ETag as a universal file hash, especially for multipart uploads. Compute archive checksums; make the limits of remote verification explicit.
- Persist a simple per-operation journal: pending, running, completed, needs input, failed, skipped. Bind retries to source/target/manifest identity.
- Interrupted SFTP/network-share/object-store transfers leave partial state and no completion marker. Finalized immutable archives are never overwritten by a retry.
- Use supported direct or session-pooler database connections; do not use transaction pooling for dump/restore. Handle IPv6 limitations in the connection wizard. [S1]
- Pin version-compatible PostgreSQL clients. Avoid untested regex filters that strip permissions or SQL ownership statements indiscriminately.
- Provide redacted diagnostics and actionable errors for expired credentials, disk-full, quota, network, version and permission failures.
- For very large projects, run the same headless engine on a machine near Supabase or the remote destination rather than forcing all traffic through a laptop.
- Scheduling, retention and multi-copy policies can be added later without requiring incremental archive chains.

## 11. Platform evaluation and selected stack

| Option | Benefits | Costs / limitations | Recommendation |
|---|---|---|---|
| **Tauri GUI + shared Rust engine + thin CLI** | Friendly forms and file dialogs; local streaming, OS credential storage, native binaries; headless reuse. | Rust/backend skill required; cross-platform database-tool packaging and signing need work; webview behavior differs by OS. | **Selected; macOS first.** |
| **Electron GUI + shared TypeScript/Node engine + CLI** | Convenient Supabase JS ecosystem; potentially faster for a strongly TypeScript-focused team. | Larger distribution/runtime; still needs strict main/renderer separation and native-tool packaging. | Strong alternative if implementation speed/team skills favor TypeScript. |
| **CLI-first with a guided terminal experience** | Smallest delivery scope, straightforward server execution and automation. | Poorer discovery, forms, secret handling explanations and non-developer usability. | Useful prototype and secondary interface, not the primary consumer experience. |

A browser-only SaaS frontend is not the recommended first version: direct Postgres access and durable filesystem operations need another execution environment, and routing whole projects/secrets through our servers creates a much larger security and hosting responsibility.

### Working stack: macOS-first Tauri application

- **Tauri 2** shell, with macOS as the first supported desktop OS.
- **React + TypeScript + Vite**, built as a client-side SPA; no Next.js, server-side rendering or application web server.
- **shadcn/ui + Tailwind CSS**, adding only the components needed for the archive/restore wizard. Svelte with shadcn-svelte would also fit, but React is the working choice because it uses shadcn/ui directly and has an official Vite setup. [S16]
- **Rust core library** for capability discovery, operation state, file streaming, encryption, verification and narrowly scoped subprocess execution.
- **Thin CLI** using the same core for archive, inspect, verify and restore operations.
- **Documented HTTP APIs + tested CLI/Postgres binaries**, not a custom database dumper.
- **JSON/JSONL and checkpoint files** for portable metadata initially; no separate app database/server unless measured scale requires one.
- **Established crypto and OS credential-store libraries** selected during the feasibility phase.

Keep the implementation to ordinary modules for database, configuration, Storage, functions, archive/security and user guidance. A short fixed restore sequence is enough; no plugin marketplace, generic workflow engine, message broker or microservices.

Tauri supports scoped capabilities and bundled sidecar binaries. Restrict the renderer to named operations and validated arguments; do not expose a generic shell or arbitrary filesystem access. Review permissions for custom backend commands too. Sign/notarize packages, verify downloaded tools, pin versions and provide a secure update path. [S14]

### macOS-specific delivery requirements

- Use native folder/open/save dialogs and macOS Keychain for optional credential persistence. Keep recovery keys portable rather than making the Keychain the only recovery mechanism.
- Prefer a directly distributed, Developer ID-signed and notarized `.app`/DMG for public releases. Signing covers bundled executable tools too; an unsigned developer build is not a production-distribution milestone. [S14]
- Determine the minimum macOS release and Apple Silicon/Intel support during the packaging proof. Do not promise an architecture until the app and its PostgreSQL/CLI sidecars are tested together.
- Development may require Rust, Node and Xcode Command Line Tools; non-developer end users should not need to install those tools or Homebrew to use a release.
- Handle sleep, shutdown, external-drive removal and lost network mounts with checkpointed work and clear recovery instructions. Do not silently change system sleep settings.
- Keep core logic portable, but defer Windows/Linux packaging and testing until the macOS recovery path is proven.

**Important packaging decision:** The documented Supabase CLI dump path uses Docker. Docker should not become a surprising prerequisite for non-developers. Compare a guided Docker-backed compatibility mode against bundled PostgreSQL tools using a tested Supabase-aware recipe. Prove parity before recommending a Docker-free implementation; do not simply replace Supabase CLI dumping with unrestricted `pg_dump`. [S1, S4]

## 12. Phased delivery and acceptance gates

### Phase 0 — Prove recovery before building the polished UI

**Progress:** The [local archive prototype](../archive-prototype.md) proves encrypted artifact packaging, complete-payload verification, local recovery without the source folder, and interoperability with the independent age CLI. This completes only the local packaging slice, not Phase 0's Supabase or target-scale acceptance gates. See its [implementation plan and results](2026-09-18-local-archive-prototype.md).

Use deliberately configured disposable projects with representative data, Auth, Storage ownership policies, functions/dependencies, Vault, cron/webhooks and non-default settings. Begin with a small macOS headless recovery proof, not a polished UI or a large scaffold. Use synthetic local fixtures for archive/encryption checks, then explicitly authorized disposable Supabase projects for live round trips; never assume permission to create paid resources or change a production project.

Deliver:

- A tested coverage matrix and exact unsupported cases, with small-fixture round trips followed by scale measurements toward the selected **10 GB database / 100 GB stored-files** target. Record object counts, elapsed recovery time, peak memory and temporary disk needs; larger benchmarks are deferred.
- Credential/redaction behavior by endpoint and supported release.
- A round-trip database/Auth/Storage/functions procedure, including side-effect suppression.
- Root-key transfer and Vault decryption proof.
- A redeploy test for function dependency packages.
- A cross-platform tool distribution recommendation, including Docker requirements.
- Encryption/streaming/resumption feasibility and archive-format draft.

**Gate:** Restore from archive with source credentials/network access removed. Source deletion is unnecessary for the test. Verify that a missing critical key or dependency produces a failure/incomplete result, not success.

### Phase 1 — Useful private alpha

Implement connection/preflight, discovery, full archives, local/external/mounted folders, core database/Auth/file-Storage/functions coverage, manual forms, full encryption, archive verification and a new-project restore flow. Include a minimal technical CLI to exercise the engine.

Every detected surface must appear in the coverage report from the start, even if currently manual or unsupported. Do not label specialty-storage data “backed up” because its bucket name was captured.

**Gate:** A representative user can create and restore an archive without typing shell commands; a developer need not manually repair the target for supported configurations.

### Phase 2 — Broader coverage and production readiness

Add remaining automation for domains, SSO/custom OAuth, OAuth server clients, hooks, network controls, platform settings, external dependency guidance and dashboard instructions. Add selected-section encryption, signed/notarized macOS installers, recovery-key UX, robust crash recovery and macOS version/architecture testing. Other desktop operating systems remain a later expansion.

**Gate:** No unintended outbound requests/messages/jobs during restoration; security policies and known feature behavior pass end-to-end checks.

### Phase 3 — Remote storage and operational use

Add SFTP and S3-compatible destinations, headless remote execution, optional scheduling/retention, multiple verified copies and a convenient restore-rehearsal action. Implement specialty Storage adapters only after their supported export/reconstruction paths are proven.

**Gate:** Large, interrupted transfers resume without corruption; offline inspection and independent restoration work from downloaded remote archives.

### Phase 4 — Demand-driven additions

Consider incremental/deduplicated backups, organization-wide batches, branches, external-provider integrations, team key management, self-hosted targets and richer audit requirements. None should delay a reliable full-archive product.

### Essential acceptance scenarios

- Source unavailable during restoration; no hidden dependence on old keys/API access.
- Empty/no-Storage/no-functions projects and complex multi-schema projects.
- Auth identity preservation; password login; provider secret missing; MFA/passkey limitations clearly surfaced.
- More than one page of every relevant collection, large files, Unicode/case-sensitive keys and reserved local filenames.
- Private Storage owner-policy behavior and object-ID dependencies.
- Missing/non-exportable keys; incorrect passphrase; corruption/truncation; tampered metadata.
- Privilege/default-privilege differences and negative RLS tests.
- Cron, queues, hooks, triggers and external integrations do not act before activation.
- Network loss, laptop restart, disk full, permission failure, API throttling and expired credentials.
- Incompatible Postgres/extensions/Auth schema; unsupported feature/plan; source project chosen as target; non-empty destination.
- Changed source configuration/data during a live archive is detected or explicitly characterized.
- No secrets in logs, arguments, support bundles or unauthorized plaintext temp artifacts.

## 13. Confirmed direction and decisions to make next

Confirmed platform decisions and working defaults:

1. **Primary interface — approved:** Tauri desktop GUI plus shared Rust engine and thin CLI. Working frontend: React + TypeScript + Vite SPA with shadcn/ui.
2. **Initial release OS — approved:** macOS first; minimum OS version and processor architectures will follow the packaging proof. Maintain portable core logic.
3. **Connectivity:** Guided token flow first; OAuth convenience later after confirming its security/hosting model.
4. **Destinations:** Local/external/mounted folder first, SFTP and S3 next.
5. **Safety:** New empty projects only; never overwrite a live project or delete the source automatically.
6. **Protection:** Full encryption by default; selected-section mode is an advanced option.
7. **Fidelity:** Core service round trips first, all other features inventoried with explicit manual/unsupported status.
8. **Scale envelope — approved:** Initially validate databases up to **10 GB** and stored files up to **100 GB**. Establish object-count coverage, recovery time and downtime tolerance through feasibility measurements rather than promising arbitrary scale.

Next, define the Phase 0 test setup and write a bounded macOS feasibility implementation plan. Resolve Docker-free tool packaging, dependency-complete function export, Storage ownership fidelity and encrypted archive resumption before building the full wizard. The first live round trip needs explicitly authorized disposable projects and a cost ceiling; credentials and generated archives must never be committed to this public repository.

## Sources

Official documentation and live API definitions were inspected on the research date. Features marked alpha/beta and mutable CLI development documentation require release-specific verification.

- **S1:** [Backup and Restore using the CLI](https://supabase.com/docs/guides/platform/migrating-within-supabase/backup-restore) — logical restore, root key, migration history, managed-schema customizations, functions and Storage.
- **S2:** [Management API reference](https://supabase.com/docs/reference/api/introduction), [live OpenAPI v1 specification](https://api.supabase.com/api/v1-json), [Auth configuration endpoint](https://supabase.com/docs/reference/api/v1-get-auth-service-config).
- **S3:** [Get project configuration, v2 / alpha](https://supabase.com/docs/reference/api/v2-get-project-config) — aggregate effective config and HMAC-redacted Auth secrets.
- **S4:** [Supabase CLI database dump documentation](https://github.com/supabase/cli/blob/develop/apps/cli/docs/supabase/db/dump.md) — managed-schema exclusions, data/roles and default-privilege warning.
- **S5:** [Restore to a new project](https://supabase.com/docs/guides/platform/clone-project) — scope, native key copying and external-operation warning.
- **S6:** [Custom domains](https://supabase.com/docs/guides/platform/custom-domains) — DNS, verification and OAuth/SAML impacts.
- **S7:** [JWT signing keys](https://supabase.com/docs/guides/auth/signing-keys) — key lifecycle and non-exportability.
- **S8:** [Edge Function environment variables](https://supabase.com/docs/guides/functions/secrets), [secret-list endpoint](https://supabase.com/docs/reference/api/v1-list-all-secrets); corroborating [Supabase provider secret-digest behavior](https://www.pulumi.com/registry/packages/supabase/api-docs/edgefunctionsecrets/).
- **S9:** [Storage S3 compatibility](https://supabase.com/docs/guides/storage/s3/compatibility).
- **S10:** [Storage ownership](https://supabase.com/docs/guides/storage/security/ownership).
- **S11:** [Build a Supabase OAuth integration](https://supabase.com/docs/guides/integrations/build-a-supabase-oauth-integration) — Management API authorization and client-secret requirement.
- **S12:** [Custom SMTP](https://supabase.com/docs/guides/auth/auth-smtp), [custom OAuth/OIDC providers](https://supabase.com/docs/guides/auth/custom-oauth-providers), [OAuth server setup](https://supabase.com/docs/guides/auth/oauth-server/getting-started), [OAuth Admin API](https://supabase.com/docs/reference/javascript/oauth-admin).
- **S13:** [Database backups](https://supabase.com/docs/guides/platform/backups), [Log Drains](https://supabase.com/docs/guides/observability/log-drains), [Cron](https://supabase.com/docs/guides/cron), [Queues](https://supabase.com/docs/guides/queues), [Wrappers](https://supabase.com/docs/guides/database/extensions/wrappers/overview).
- **S14:** [Tauri sidecars](https://v2.tauri.app/develop/sidecar/), [Tauri capabilities](https://v2.tauri.app/security/capabilities/), [macOS code signing and notarization](https://v2.tauri.app/distribute/sign/macos/) — also checked through Context7’s official Tauri documentation index.
- **S15:** [Storage overview](https://supabase.com/docs/guides/storage), [vector indexes](https://supabase.com/docs/guides/storage/vector/working-with-indexes), [analytics buckets](https://supabase.com/docs/guides/storage/analytics/creating-analytics-buckets).
- **S16:** [shadcn/ui Vite installation](https://ui.shadcn.com/docs/installation/vite) — checked through Context7’s official shadcn/ui documentation index.
