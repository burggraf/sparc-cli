# SPARC Go CLI — research ledger and decisions

**Prepared/refreshed:** 2026-09-21. This file distinguishes owner decisions, documentation-backed facts, historical experiments, proposed choices and unanswered questions. Mutable upstream pages must be rechecked and pinned before implementation. Tool search snippets are leads, not compatibility proofs.

## 1. Decisions carried into the new project

| ID | Decision | Reason / boundary |
| --- | --- | --- |
| D01 | Fresh Go-only CLI, public repository | Explicit owner request. Rust/Tauri is superseded, not maintained as a parallel engine. |
| D02 | Download-and-run macOS/Windows | No end-user development tools, database server, Docker, Supabase CLI or package-manager dependency. |
| D03 | Prefer one executable with embedded tools | Fits owner UX. Requires OS trust and extraction proof; a bundled folder remains a fallback requiring approval. |
| D04 | Official PostgreSQL client tools | Do not spend the project reimplementing PostgreSQL's dumper. Client tools are bundled, not assumed on PATH. |
| D05 | Documented HTTP + S3 interfaces | No dashboard scraping, MCP or AI runtime dependency. Native requests from Go are sufficient. |
| D06 | New, different, empty destination only | Avoid merging/overwriting production; empty means provider baseline with no user resources. |
| D07 | Manual settings/secrets are acceptable | Preserve explicit missing requirements and guided input; do not mislabel a secret digest as a recoverable credential. |
| D08 | Full encrypted independent archives first | Portable recovery without source access, no incremental-chain dependency. |
| D09 | Local/mounted and S3 destinations | Remote S3 is a prefix/object protocol, not a filesystem directory. SFTP follows separately. |
| D10 | Verification separates integrity, comparison and restore proof | Successful tools/counts do not prove content, security or recoverability. |
| D11 | Honest bounded support | Detect unsupported features rather than silently omit them; full project promise requires service-specific proof. |
| D12 | Fresh authorization for live work | Old projects/credentials/permissions are not inputs; no new paid service or mutation permission is implied. |

## 2. Why Go, and what that does not solve

This is an I/O/orchestration application. Go's standard HTTP/JSON/process/stream APIs, straightforward concurrency and release tooling make it a reasonable fresh CLI choice. This is a maintainability judgment, not a benchmark showing Go backups are faster or safer than Rust. Rust could offer the same end-user experience.

Go's pure-Go core can usually be built with `CGO_ENABLED=0`; verify the selected module graph and platform integrations. PostgreSQL client binaries still have platform-specific native libraries and licenses. Cross-compiling SPARC alone is not proof that the release is self-contained.

### Libraries and alternatives

| Area | Candidate / finding | Decision status |
| --- | --- | --- |
| PostgreSQL SQL client | `github.com/jackc/pgx/v5`, pure-Go PostgreSQL driver/toolkit [G2] | Recommended for observation/verification; qualify TLS/env behavior. Not a pg_dump implementation. |
| PostgreSQL subprocess | Go `os/exec` [G1] | Recommended directly; a wrapper crate/module adds little to the strict execution boundary. |
| Go pg command wrappers | `habx/pg-commands` [G8] | Exists but still requires native tools; do not add solely to run two executables. |
| Rust alternatives investigated | `elefant-tools`, `pg_plumbing`, `libpgdump` [G9] | `elefant-tools` does schema/data copying/SQL; `pg_plumbing` advertises early development; `libpgdump` manipulates dump files. None established full hosted-Supabase recovery. Archived context only, not new implementation dependencies. |
| AWS S3 | AWS SDK for Go v2 service S3, custom endpoint/path-style configuration [G3] | Preferred initial evaluation; configure signing, region, checksums and retries for actual provider support. |
| Transfer helper update | `feature/s3/manager` Uploader/Downloader are deprecated in favor of `feature/s3/transfermanager` [G4] | **Correction to earlier conversation examples.** Use current API after evaluation, not copied old manager examples. New transfermanager provides automatic multipart reads/writes and sequential-reader output; restart-safe resume is not thereby proven. |
| Alternative S3 SDK | `github.com/minio/minio-go/v7` [G5] | Suitable evaluation alternative. Choose one SDK, not both by default; validate target endpoints and license. |
| Encryption | `filippo.io/age` [G6] | Reference Go library, streaming authenticated encryption. Test Close/EOF/error handling, passphrase UX and old X25519 interop. |
| CLI | stdlib flag + line prompts; `x/term` for hidden input | Sufficient starting point. Cobra/terminal UI optional only if proven useful. |
| Packaging | stdlib `embed`, native signing tools, optional GoReleaser [G1,G7] | Embedding is easy; signed extracted native clients are the hard proof. GoReleaser notarization features may require Pro; a paid dependency is not assumed. |

Documentation lookup used Context7 first for Go, pgx, age, AWS SDKs and GoReleaser. Some broad Go snippets were not specific enough; direct package docs were consulted. The age Context7 request failed, so direct `pkg.go.dev/filippo.io/age` was used. Do not interpret index availability as release qualification.

## 3. Supabase findings retained

### S01 — A SQL dump is not a full project [S1,S2,S3]

Storage bytes, service configuration, functions and encryption root keys need separate capture. Roles/global state, migration history, managed customizations and extension-owned data need deliberate handling. A default Supabase schema dump has managed exclusions; data-only behavior differs. The migration guide explicitly excludes vector-storage metadata in its example; this is not evidence specialty data is backed up elsewhere automatically.

### S02 — Much control-plane configuration is automatable [S4,S5]

Management APIs cover Auth (including many SMTP/provider fields), Realtime, Storage, Data API/PostgREST, database/pooler settings and other resources. APIs differ in stability/permission requirements and which fields are writable. Do not PATCH a GET response wholesale. Preserve original encrypted response plus a normalized allowed-field model and classified unknowns.

### S03 — Readable configuration is not exportable secret material [S5,S6]

The aggregate `/v2/projects/{ref}/config` endpoint is alpha in the indexed reference and explicitly returns Auth secrets as HMACs. Historical research found Edge Function secret lists expose digests rather than usable values. Reconfirm exact behavior of each chosen endpoint/version; a field named `value` is not evidence of plaintext. Missing secrets require user-supplied originals or replacement plans. Never deploy a secret-exfiltration function as a workaround.

### S04 — Vault and column encryption require the source root key [S1,S7]

Supabase documents GET/PUT `/v1/projects/{ref}/pgsodium` for root-key transfer. The guide describes a 64-character hex key, available only while the source project is active, and warns backups contain ciphertext rather than this key. Replacing a target key strands data encrypted with the former key; this is only acceptable under a validated unused-target workflow and explicit approval. The endpoint is beta/experimental in prior research; recheck permissions/stability and pg/Vault versions. This is NOT the JWT signing key.

### S05 — New JWT signing private keys are non-exportable [S8]

Only the legacy JWT secret is described as extractable; newer signing private keys/shared secrets cannot be retrieved from Supabase. Inventory rather than promise export. Prefer destination keys, application updates, supported session invalidation and reauthentication. Outstanding email links/signed URLs may need replacement; restoring rows does not preserve all sessions. Imported keys retained externally are a distinct case, not a default capability.

### S06 — Standard Storage has usable S3 endpoints [S9,S10]

Supabase documents ListBuckets, ListObjectsV2 pagination, Get/Head/PutObject, conditional operations and multipart operations. It does NOT implement every S3 ACL/checksum/encryption/versioning/lifecycle feature. Generated project S3 keys bypass RLS across buckets and are privileged server-side credentials. Session-token RLS access also exists but needs completeness qualification. Preserve bucket-specific settings via Storage APIs where S3 does not expose them.

### S07 — Ownership is a critical unsolved restore gate [S11]

Supabase documents owner IDs derived from user JWT `sub`, and service-key creation does not automatically preserve the original owner. Ordinary admin re-upload cannot establish equivalent owner-based RLS. Need supported API/metadata reconciliation proof for owners, object IDs, timestamps and application references. Do not repair Storage by directly manipulating provider tables without explicit documented support and a tested contract. If no supported fidelity path exists, detect affected projects and refuse an equivalent-restore claim.

### S08 — Functions may need original dependency files [S1,S12]

The migration guide warns function downloads omit import maps and `deno.json`. Save shared code, assets, lockfiles/import config and a redeployable package, not just an inventory. Remote dependency availability and function startup effects need investigation. Management download/deploy APIs may remove a Supabase CLI/Deno dependency, but their exact body/bundling contract must be proved on Windows/macOS. Do not quietly require Node/Deno/Docker if source bundling needs them; choose a supported server-side path, bundle an approved tool, or block that configuration explicitly.

### S09 — Native clone is not the portable archive solution [S3]

Supabase's native restore-to-new-project is database-focused and can automatically copy encryption keys in that distinct flow. It does not establish this CLI's independent logical recovery or copy every service/file. Copied scheduled/external operations can begin acting; native clone is an optional complementary service, not an implementation shortcut for SPARC's contract.

### S10 — Management integration OAuth is not project Auth OAuth [S13]

The currently read integration guide recommends PKCE but still requires client ID AND client secret at token exchange. Do not embed a confidential integration client secret in a public executable. Start with user-provided access tokens. Research an approved public-client/device/loopback flow before promising browser login; otherwise a broker adds hosting/security scope and needs a separate decision. This restriction does not refer to a project's own OAuth server clients.

### S11 — New project identity is unavoidable

Project refs/URLs/API keys and callback configuration differ; custom domains need new ownership validation and approved cutover. External hosting, billing, CI/CD and OAuth provider accounts are outside the database. Some configuration is manual by design. Organization membership/billing and history of backups/logs are not automatically cloneable project data.

### S12 — No assumed global snapshot

A PostgreSQL logical snapshot is not an atomic snapshot of Storage, functions, config and external writers. Multiple dump passes must share a proven compatible snapshot or use a quiet window. Stop app writes/uploads/signups/jobs/consumers and external writers operationally; pausing Supabase is not an export-compatible write freeze. Live mode needs precise drift detection and weaker guarantees.

## 4. PostgreSQL findings and hazards

Official PostgreSQL docs [P1-P4] remain the primary dump/restore specification. Archive execution can run source-defined code. Custom format improves inspection/selection but is not a sandbox.

- `pg_dump` clients refuse newer server majors; restoration to an older server is not a supported portability assumption. SPARC starts with a stricter tested matching-major policy, not a claim PostgreSQL universally requires exact majors.
- One full dump provides a database snapshot; sequences/global roles and separately observed state have concurrency caveats. Separate dump files are not automatically one snapshot.
- Schema selectors are patterns **even without a shell**. Literal selection needs correct client-version-specific quoting plus `--strict-names` and decoy tests.
- Selected schemas are not necessarily a complete dependency set. Dynamic SQL/string-bodied routines can refer to objects without catalog dependency edges. Extension membership, partitions and managed references complicate closure.
- Roles are global; `pg_dump` is not enough. `pg_dumpall --roles-only` may expose unavailable privileged fields/passwords/reserved roles; evaluate restricted capture rather than blindly replaying it.
- Dropping owners/privileges to make restore work changes the security model. Destination defaults can add privileges even when dump ACL replay succeeds.
- PostgreSQL table-level ACL checks miss column-level grants; PUBLIC pseudo-grantee is distinct from a quoted role named `PUBLIC`. NULL and empty ACL/default records are not interchangeable. Membership identities/flags are version-specific.
- `session_replication_role=replica` affects triggers/FKs and may interact with encryption. It is not universal side-effect suppression. Constraints and security need post-load proof.
- No generic ignore-errors restore, unrestricted `--clean`, CASCADE cleanup, or automatic retry after ambiguous commit.
- `psql -X` and fixed environment prevent ambient rc behavior. Retain current psql safety mechanisms; never strip restriction markers casually. Credentials do not belong in argv/dry-run logs.
- Go pgx and libpq have different TLS/configuration defaults. Test both, including host mismatch/wrong CA/missing TLS; no fallback to insecure transport.

## 5. Historical evidence preserved (not new support claims)

Exact copies and source provenance are in [reference](reference/README.md). Historical source commit: `858f3643fc08183c269defe1e7b8e638a1aa5e54`. Old Supabase CLI reference: v2.117.0, commit `21db855916f2c2b12f61cde923a27094b8528b23`. Neither version is an instruction to freeze all new dependencies forever.

| Evidence | What was actually established | What remains unproven |
| --- | --- | --- |
| Local Rust archive prototype | Separate age-encrypted artifacts, manifest-last, hashes, private local output, source-folder-independent unpack; independent Go age interoperability. | Hosted export, production archive format, resume, Windows ACLs, target scale and authentic sender identity. |
| Declaration-only planner | Strict bounded inputs, version/pooler blocking, no network/tool effects; readiness always false. | Observed connectivity, live compatibility and executable recipe. |
| 2026-09-18 hosted fixture | Tiny synthetic non-public schema, native PG17.9 clients against hosted PG17.6, exact rows/FK/sequence/ACL/RLS checks; known source credential files OS-denied during restore. | Source remained online; not network-outage proof, no populated Auth/Storage/Vault/services. |
| Pinned native-script rehearsal | Exact upstream schema/data scripts through controlled native clients; real bad-CA/hostname rejection, encrypted round trip, fixed fixture verification. | Not unmodified Supabase CLI/Docker parity; separate passes require quiescence. |
| Expanded hosted fixture | Enum/view/function/partial index, two fixed NOLOGIN roles, scoped defaults and migration-history arrays restored as inert data. Column-grant counterexample caught in review and fixed. | Arbitrary owner graphs, all roles, seed history, ordinary public apps, managed services or scale. |
| Structural observer | Scoped catalog inspection, actual PG17 version, raw versus rendered identifier care, overload-safe identities, extension membership, read-only hosted observations. | Not exporter/admission proof; output intentionally excluded data/routine bodies, completeness flags stayed false. |
| Direct dependency observer | Named direct PG17 edges, partitions, extensions, unresolved local-only addresses; string-bodied routine lacked an expected catalog edge. | No recursive/semantic closure, dynamic SQL proof, runtime effect analysis or general restore readiness. |
| Permission experiment | Non-superuser round trip retained fixed ACL/RLS behavior on compatible stock PG17; actual negative default-grant case reproduced. | Hosted defaults/security model or arbitrary profiles. |
| Destination permission preflight | Compare explicit reviewed expected contract with observed database/schema/roles/membership/default ACLs, detect drift without normalization. | A match alone is not safe-target or permission-equivalence proof. |
| Literal selector experiment | PG17.9: 19 tricky schema names restored exactly, 5 decoys excluded; naive `star*` selector demonstrably overselected; missing schema failed. | Other client versions, hosted permissions, dependency closure. |
| Final native local recovery profile | Two independent PG17.9 clusters, trusted quiet non-public fixture, one custom dump/transactional non-superuser restore, source stopped, exact rows/ACL/default/sequence/constraints, meaningful late-DDL rollback; historical suite 79/79 passed. | Provider-owned `public`, arbitrary expressions/features, Auth/Storage/functions/Vault, concurrency, Windows and scale. |

### Upstream/native-route counterexamples that must survive the restart

These were source-inspected or locally reproduced **for the pinned CLI**, not allegations about every current release:

1. Dump container environment forwarded host/port/user/password/database but not required `PGSSLMODE`/`PGSSLROOTCERT`; no CA bind mounts at the inspected boundary. Host-side verified TLS did not establish dump-process TLS. Image defaults were not proved.
2. `--dry-run` resolved connections and expanded passwords; linked routes could create login roles or clear network bans. Not a safe offline diagnostic.
3. A schema name containing a space split through unquoted shell `EXTRA_FLAGS`; direct argv avoids shell splitting but not pg_dump patterns.
4. Output-file creation/truncation could use permissive mode; stdout retry could retain partial SQL. Need private exclusive staging and no concatenated retry output.
5. Script SQL rewrites affected ownership/ACLs/publications/extensions and stripped psql restrict/unrestrict controls. Do not import them as a generic engine.
6. Python timeout killed the shell but left a descendant pipeline alive. Go `CommandContext` alone likewise must not be assumed to terminate every child; qualify process-tree cancellation on both OSes.
7. Successful restore retained an unwanted global default SELECT grant on a non-RLS table, allowing unapproved reads. Success exit status is not security equivalence.
8. Column grants can authorize operations missed by table ACL tests; owners/FORCE RLS and sequence USAGE versus SELECT need separate probes.
9. Catalog internal `"char"` concatenation required explicit casts; OIDs are unsigned and database-local. Rendered identifier strings cannot safely be split back into canonical names.
10. `to_regnamespace` with an unquoted text name containing spaces can report NULL even while the exact namespace exists. Compare exact catalog names where intended.
11. Cleanup of owned test clusters must confirm shutdown before directory removal. Unconfirmed shutdown leaves a quarantined directory, not deleted live files.
12. Capturing subprocess output and checking its size afterward does not bound peak memory. The new runner must bound while reading.

## 6. Research/experiment backlog with decision gates

Each experiment produces a short committed `docs/research/Rxx-*.md` containing date, source links/commits, tested versions, exact sanitized commands, fixture definition, expected/actual checks, limits and next decision. Passing a fake test cannot close a live-service question.

| ID | Question / experiment | Evidence required and fallback |
| --- | --- | --- |
| R01 | Can a signed single executable extract/run signed PostgreSQL helpers on macOS/Windows without dependency installs? | Clean machine standard-user/browser-download tests including quarantine/MOTW, actual TLS dump/restore and non-ASCII paths. Investigate nested signature/notary discoverability. If blocked, self-contained ZIP fallback needs owner approval. |
| R02 | Where do redistributable PostgreSQL clients come from? | License/provenance/build recipe, complete dynamic dependency tree (libpq/TLS/compression/runtime), version/digest manifest, supported CPU/OS baselines and updates. No reliance on Homebrew paths or a VC++ installer. |
| R03 | TLS and project identity for native clients and pgx | Good CA succeeds; wrong CA/hostname/expired/untrusted cert and insecure fallback fail; direct/session and IPv4/IPv6 cases. Explicit credential/tenant routing binding and pooler TLS limits. |
| R04 | Supabase-aware native capture/restore recipe | Compare upstream pinned recipe with native profiles; cover public, Auth data, roles/history, managed exclusions/customizations/extensions. One coherent snapshot or quiet-window evidence. No permissive SQL rewriting fallback. |
| R05 | Destination permissions and ordinary public-schema apps | Hosted tests of owners/defaults/membership/column grants/RLS/security-definer behavior; block unknown baselines. Prove no unintended exposure, not merely matched names. |
| R06 | Auth recovery | Actual users/identities/password hashes, FK IDs, login, session policy, provider records; separately MFA/passkeys/SSO/OAuth apps/schema drift. Document recoverable versus reenrollment-required records. |
| R07 | Storage owner/ID/metadata fidelity | Full standard bucket round trip with owner-only RLS, public/private access, arbitrary keys, many pages, multipart, app object-ID references and SQL metadata reconciliation. Supported API path or explicit blocker, not silent admin ownership. |
| R08 | Vault/pgsodium | Obtain key while active, encrypt it, restore into unused target, prove target decryption without source. Test missing/wrong key and nonempty-target refusal. Do not reveal plaintext secret in report. |
| R09 | Functions without external user tools | Download/deploy API contracts, complete dependency package and any ESZIP/bundling requirements; verify no dependency on source repo, source APIs or unpinned remote code. Missing files/secret must block deploy-ready state. |
| R10 | Settings/secret availability by endpoint | Read/write/redaction/null/default/immutable/permission behavior for Auth/SMTP/SSO/hooks/Data API/Realtime/Storage/network/etc. Safe field allowlist and manual collection for unsupported fields. Alpha API not sole dependency. |
| R11 | Deep verification | Deterministic qualified DB encodings with duplicates/no PK/types/collations, remote byte hashing and expected target differences; quick/deep/sample labels and cost bounds. No dump-file hashes as semantic equality. |
| R12 | Consistency and side effects | Quiet-window procedure; separate snapshot coordination if live mode proposed; source changes during scan/download; cron/hooks/queues/triggers disabled target sequence. No global atomicity claim. |
| R13 | Archive/key/resume design | age round trip, independent decrypt, truncation/finalization errors, key loss/wrong key, bounded manifests, disk staging, chunk versus whole-object restart, app crash journal binding, sender-trust policy and legacy import choice. |
| R14 | Destination behavior | Local/APFS/NTFS/exFAT/mounted folders and chosen S3 providers; collision/no-clobber/publication, streaming retries, checksums, eventual/provider-specific listing behavior, quotas, multipart cleanup and egress. SFTP host-key/resume semantics later. |
| R15 | Specialty/extension durable data | Vectors/Iceberg, queues/cron, extension config tables, large objects, FDWs/external rows, publications/replication. Support only with independent export/reconstruction proof; otherwise inventory and block complete coverage. |
| R16 | Management login UX | PAT first. Recheck confidential-client requirement, fine-grained scopes, token expiry/revocation, OS stores; public-client OAuth/device flow only if officially supported. No embedded client secret. |
| R17 | Release maintenance and public repo | Choose license/module path, dependency SBOM/notices, signing service budget, CI trust boundaries, archive compatibility policy, offline first run, support escalation and CVE response. |
| R18 | Supported scale and reliability | 10 GB DB / 100 GB files plus small-file-heavy corpus; memory/disk/time/egress/timeout baselines, rate limits, concurrent reads/writes, restart and cancellation. No production/user data. |

### Blocking decisions before public alpha

R01/R02 (actual dependency-free packages), R03/R04/R05 (database/security), R06/R07/R08/R09 for any advertised features, R10 minimum settings and manual handling, R11 honest verification, R12 recovery safety, R13 archive integrity and R17 signing/repository hygiene. R15 may remain unsupported only with reliable detection, explicit refusal/incomplete scope and documented manual export options. Unsupported features cannot disappear from the inventory.

## 7. Source register

Sources are public, no credentials required. Dates here describe research, not an immutable API promise. Preserve exact endpoint contracts or sanitized fixtures in the new research records when implementing. Do not vendor entire upstream repositories unnecessarily.

### Supabase

- **S1** [Logical backup/restore and migration](https://supabase.com/docs/guides/platform/migrating-within-supabase/backup-restore); [upstream MDX](https://raw.githubusercontent.com/supabase/supabase/master/apps/docs/content/guides/platform/migrating-within-supabase/backup-restore.mdx). Covers keys, migration history, managed customizations, functions and file transfer; example scripts are not a production completeness guarantee (audit pagination!).
- **S2** [Database backups](https://supabase.com/docs/guides/platform/backups); [dashboard restore](https://supabase.com/docs/guides/platform/migrating-within-supabase/dashboard-restore).
- **S3** [Restore to a new project](https://supabase.com/docs/guides/platform/clone-project).
- **S4** [Management API introduction](https://supabase.com/docs/reference/api/introduction); [OpenAPI v1](https://api.supabase.com/api/v1-json); [Auth config](https://supabase.com/docs/reference/api/v1-get-auth-service-config).
- **S5** [Aggregate configuration](https://supabase.com/docs/reference/api/v2-get-project-config), explicitly HMAC-redacted Auth secrets.
- **S6** [Function secret inventory](https://supabase.com/docs/reference/api/v1-list-all-secrets); [function environment/secrets](https://supabase.com/docs/guides/functions/secrets).
- **S7** [Vault](https://supabase.com/docs/guides/database/vault); [pgsodium](https://supabase.com/docs/guides/database/extensions/pgsodium).
- **S8** [JWT signing keys](https://supabase.com/docs/guides/auth/signing-keys).
- **S9** [S3 compatibility](https://supabase.com/docs/guides/storage/s3/compatibility).
- **S10** [S3 authentication](https://supabase.com/docs/guides/storage/s3/authentication).
- **S11** [Storage ownership](https://supabase.com/docs/guides/storage/security/ownership); [access control](https://supabase.com/docs/guides/storage/security/access-control).
- **S12** [Function dependencies](https://supabase.com/docs/guides/functions/dependencies); [get function API](https://supabase.com/docs/reference/api/v1-get-a-function).
- **S13** [Management OAuth integration](https://supabase.com/docs/guides/integrations/build-a-supabase-oauth-integration).
- **S14** [SMTP](https://supabase.com/docs/guides/auth/auth-smtp); [custom OAuth/OIDC](https://supabase.com/docs/guides/auth/custom-oauth-providers); [OAuth server](https://supabase.com/docs/guides/auth/oauth-server/getting-started); [OAuth Admin reference](https://supabase.com/docs/reference/javascript/oauth-admin).
- **S15** [Storage overview](https://supabase.com/docs/guides/storage); [vector indexes](https://supabase.com/docs/guides/storage/vector/working-with-indexes); [analytics buckets](https://supabase.com/docs/guides/storage/analytics/creating-analytics-buckets).
- **S16** [Custom domains](https://supabase.com/docs/guides/platform/custom-domains); [Cron](https://supabase.com/docs/guides/cron); [Queues](https://supabase.com/docs/guides/queues); [Wrappers](https://supabase.com/docs/guides/database/extensions/wrappers/overview); [Log drains](https://supabase.com/docs/guides/observability/log-drains).
- **S17** [Pinned legacy CLI source](https://github.com/supabase/cli/tree/21db855916f2c2b12f61cde923a27094b8528b23); [current CLI repo](https://github.com/supabase/cli); [dump reference](https://supabase.com/docs/reference/cli/supabase-db-dump). Current repository has TypeScript/Bun and legacy Go code; do not assume a stable importable Go recovery library.

### PostgreSQL

- **P1** [PG17 pg_dump](https://www.postgresql.org/docs/17/app-pgdump.html); [current pg_dump](https://www.postgresql.org/docs/current/app-pgdump.html).
- **P2** [pg_restore](https://www.postgresql.org/docs/17/app-pgrestore.html).
- **P3** [pg_dumpall](https://www.postgresql.org/docs/17/app-pg-dumpall.html).
- **P4** [libpq TLS](https://www.postgresql.org/docs/current/libpq-ssl.html); [passfiles](https://www.postgresql.org/docs/current/libpq-pgpass.html); [license](https://www.postgresql.org/about/licence/).

### Go/libraries

- **G1** [Go embed](https://pkg.go.dev/embed); [os/exec](https://pkg.go.dev/os/exec); [Go release policy](https://go.dev/doc/devel/release).
- **G2** [pgx v5](https://pkg.go.dev/github.com/jackc/pgx/v5); [source](https://github.com/jackc/pgx).
- **G3** [AWS SDK Go v2](https://github.com/aws/aws-sdk-go-v2); [endpoint configuration](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-endpoints.html).
- **G4** [old manager docs/deprecations](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/feature/s3/manager); [transfermanager](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager); [migration discussion](https://github.com/aws/aws-sdk-go-v2/discussions/3306).
- **G5** [MinIO Go client](https://github.com/minio/minio-go).
- **G6** [age package](https://pkg.go.dev/filippo.io/age); [age project](https://github.com/FiloSottile/age).
- **G7** [GoReleaser](https://goreleaser.com/); [notarization](https://goreleaser.com/customization/sign/notarize/); [binary signing](https://goreleaser.com/customization/sign/binary_sign/); [universal binaries](https://goreleaser.com/customization/builds/universalbinaries/).
- **G8** [pg-commands wrapper](https://github.com/habx/pg-commands).
- **G9** [elefant-tools](https://docs.rs/elefant-tools/latest/elefant_tools/); [pg_plumbing](https://github.com/NikolayS/pg_plumbing); [libpgdump](https://docs.rs/libpgdump/latest/libpgdump/); [Rust AWS SDK](https://github.com/awslabs/aws-sdk-rust).

### Platform distribution

- **O1** [Apple Developer ID](https://developer.apple.com/developer-id/); [notarization overview](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution); [custom workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow); [common issues](https://developer.apple.com/documentation/security/resolving-common-notarization-issues). Apple pages may require JS; public search excerpts and GoReleaser docs were inspected, but the single-file/helper notarization path was NOT tested. Read the full current Apple workflow during R01.
- **O2** [Microsoft SignTool](https://learn.microsoft.com/en-us/windows/win32/seccrypto/using-signtool-to-sign-a-file); [Authenticode timestamps](https://learn.microsoft.com/en-us/windows/win32/seccrypto/time-stamping-authenticode-signatures).
- **O3** [SmartScreen developer guidance](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation). Signed new binaries can still warn; EV certificates no longer guarantee a reputation bypass; Smart App Control/enterprise policy may add restrictions.

## 8. Research hygiene

Record successful and failed experiments together. Never turn the age interop result into archive-schema compatibility, the fixed PG17 fixture into arbitrary database support, a configured endpoint into S3 compatibility, a code signature into guaranteed warning-free execution, or a native dump into full Supabase coverage. The copied reference records deliberately retain these limitations.
