# SPARC Go CLI — verification, security and acceptance

**Date:** 2026-09-21. This is a test/acceptance specification, not a record of executed Go tests. The new repository starts with zero implementation evidence.

## 1. Evidence levels

Use these labels in reports and release notes:

1. **Documented:** upstream describes an API/behavior; we have not executed it.
2. **Offline contract tested:** parser, fake process, mock HTTP/S3 or archive tests passed.
3. **Local PostgreSQL tested:** actual SQL/tool behavior on owned disposable clusters; not Supabase service proof.
4. **Hosted synthetic tested:** exact authorized disposable Supabase versions/configurations passed.
5. **Source-independent recovery tested:** archive + target inputs only, source credentials and network routes denied; named checks passed.
6. **Release/scale tested:** exact signed downloads passed clean-machine OS matrix and measured workloads.

Never substitute levels 1–3 for 4–6. Every result records scope, source/target/tool versions, commit/artifact digests, test command and exit status, omissions and residual risk. Avoid "100% complete" scores that conceal missing critical keys or policies.

## 2. Threat and trust model

Protect against accidental data loss, partial/corrupt archives, malicious input paths/metadata, unsafe subprocess/SQL arguments, credential leaks, incompatible targets, broadened permissions, unintended outbound effects and release supply-chain substitution.

Boundaries:

- Management/DB/Storage/backup-destination credentials are distinct high-value secrets.
- Remote API bodies, project/object names, config and archive metadata are untrusted input even when accessed with trusted credentials.
- SQL dumps/function packages are executable content from a trusted source; encryption does not authenticate their author or sandbox them.
- OS private directories/ACLs reduce access by other users; do not claim defense from an already-compromised same-user process.
- A library context cancellation is not a process-tree termination proof, an upload return is not stored-byte verification, and a DB transaction is not cross-service rollback.

### Mandatory safeguards

- No secret literals in argv, URLs printed to console, JSON reports, process stderr forwarding, trace logs or issue templates. Password/token prompts hidden; terminal control characters escaped.
- No secret persistence without consent; protect private files on both OSes. Windows DACLs are tested rather than assuming chmod semantics.
- Validate bounds/UTF-8/types/unknown versions/duplicates for security-critical formats. Go JSON decoding needs deliberate duplicate-field handling and schema/version policy; `DisallowUnknownFields` alone is not duplicate-key rejection.
- No arbitrary executable path, shell flags, SQL, endpoint override or plugin loaded from a backup. Validated source metadata cannot choose where credentials are sent.
- HTTP redirect/proxy/TLS policy, SSRF/endpoint checks and credential-scoped clients: do not forward bearer/S3/DB secrets across unapproved origins. Custom S3 endpoints are explicit user inputs; local/insecure endpoints allowed only in isolated test mode, not production defaults.
- Temporary plaintext staging is private, bounded, minimized and documented; no secure-erasure promise. Logs/journals containing object names or settings are sensitive too.
- Finalize immutable archives only after authenticated EOF, checksum and write/close success. No clobber/merge of existing final backup or nonempty restore target.
- Source backup operations must not create roles, reset passwords, clear bans, pause/delete the project or change source config silently. Non-mutating verification never activates jobs/functions or sends messages.
- Restore preview/identity/empty-state/capacity/compatibility checks precede mutation, followed by immediate recheck. No platform gives global locking across every API; target exclusivity remains an explicit operator requirement.
- Quarantine failed/ambiguous target; no automatic destructive recovery. Any changed destination root key, created role or service object must be listed in protected operation evidence.

## 3. Runnable test conventions for the new repository

Use Go's `testing`, `httptest`, temporary directories, helper processes and narrow fakes. Add meaningful regression tests, not a framework for its own sake. Local integration tests must own new cluster directories and verify shutdown before cleanup; Windows lifecycle differs from historical Unix-socket fixtures.

Commands after the corresponding files exist:

```sh
go test ./...
go vet ./...
go test -race ./...
go build ./cmd/sparc
```

Native race builds may require a C toolchain **in CI/development**, which is not an end-user dependency. Test every advertised OS natively, not just cross-compile. For targeted packages use `go test ./internal/PACKAGE -run TestNAME -count=1 -v`.

Opt-in integration conventions to implement:

```sh
# Developer-owned local PG only; never a hosted connection string.
SPARC_TEST_PG_BIN=/approved/local/pg/bin go test -tags=integration ./tests/integration -count=1 -v

# Manual hosted test job only, with an external private configuration file.
# Requires fresh exact project/scope/cost authorization, not just the env var.
SPARC_HOSTED_TEST_CONFIG=/private/test-contract.json go test -tags=hosted ./tests/hosted -count=1 -v
```

Provide native PowerShell equivalents using `$env:...` in developer docs. Hosted config identifies explicitly authorized disposable source/target, permitted mutations and spending cap; code independently refuses same source/target and unknown scope. Config must not allow production merely because an environment variable exists. No real credentials, project archives or reports in public fixtures.

Negative tests must genuinely fail if their protection is removed. Include intentional unsafe-selector/default-grant variants in isolated test fixtures to demonstrate test sensitivity. Fake SQL runners cannot prove query semantics or provider permissions.

## 4. Required acceptance matrix

| ID | Area | Required positive and negative checks |
| --- | --- | --- |
| A01 | CLI | No-flags interactive backup; headless no-prompt behavior; malformed args; help/version offline; JSON not mixed with progress; explicit exit-code precedence; prompt cancellation/hidden secrets/no ANSI injection. |
| A02 | Credentials | File/TTY/native-store paths; denied/unavailable store; strict bounds/private ACLs; token expiry; hostile ambient env/rc/passfiles; passwords absent from argv/all diagnostic chains; partial config never printed. |
| A03 | Tool packaging | Exact bundled digest/version/architecture; no PATH fallback or runtime install; interrupted/concurrent extraction; tamper, traversal, symlink/reparse, wrong ACL, disk full, locked cache; actual signed helper execution on clean OS. |
| A04 | Process lifecycle | Bounded stdout/stderr during reading, streaming backpressure, input/output errors, deadlines, cancellation, child/grandchild cleanup, no stale handles/pipes, exit status and Close errors propagated. |
| A05 | TLS/routing | Native and pgx valid trust succeeds; wrong CA/hostname/untrusted/expired cert fails; no insecure fallback; explicit direct/session versus transaction refusal; expected tenant/source/target identity; IPv4/IPv6 reachability handled. |
| A06 | Archive parser | Malformed UTF-8/JSON, duplicate keys/IDs/paths, unknown required version/feature, excessive depth/length/count/size, overflow, absent indexes/payloads, extra payload policy, path traversal/drive/UNC/device names/case and normalization collisions. |
| A07 | Encryption | Correct/wrong key, passphrase bounds, truncated/tampered streams, failed final writer Close, corrupt final manifest, independent age decrypt, missing key refuses before target access, public recipient not mistaken for sender identity. |
| A08 | Publication/resume | No final manifest on incomplete writes; finalized incomplete coverage accurately labeled; collision/no-clobber; crash at every phase boundary; journal bound to archive/project/recipe/config; invalid or changed snapshot cannot resume. |
| A09 | Catalog/selection | Weird literal schema names with decoys; `--strict-names`; selected versus protected schemas; owner/overload/type/extension/partition/dependency identity; exact raw names not parsed display strings; unsupported/dynamic dependencies stay visible. |
| A10 | Database content | NULL/empty/Unicode/newlines/quotes/binary; empty tables, duplicate rows, composite keys/no PK, arrays/JSON/timestamps/floats, sequences incl is_called/identity; constraints/indexes/materialized views/large objects and extension types according to qualified scope. |
| A11 | Security | Owner/column/table/routine/schema/sequence/default ACLs and grantors/options; PUBLIC versus named role; membership flags; RLS/FORCE and security-definer behavior; unwanted destination global default-grant counterexample rejected; deny anonymous/other-user writes/reads. |
| A12 | Managed public/Auth | Hosted public schema baseline preserved; real Auth IDs/FKs/password login restored; managed base not clobbered; custom auth/storage policies/triggers restored; incompatible managed schema rejected; MFA/passkey/session limitations separately reported. |
| A13 | Migration history | NULL versus empty array/name, ordered/null statements, Unicode/metacommand/malicious SQL text preserved as inert rows; original statements never executed; version/seed history conflicts explicitly handled or refused. |
| A14 | Storage enumeration | Multiple pages (>1,000 objects), nested-looking flat keys, unusual Unicode/case/encoded separators/control bytes allowed by service, empty and large objects, bucket pagination; list failures/permission errors not absence. |
| A15 | Storage fidelity | Exact bytes/content/cache metadata/settings, owner-ID RLS, object-ID app references, target public/private access, API uploads plus SQL metadata without duplicate/corrupt records; inability to preserve required identity blocks equivalence. |
| A16 | Storage races/transfers | Changed/deleted source object during download, conditional GET failure, multipart failures/retries/ranges, sizes above basic PUT threshold, transient/429/permanent errors, EOF/authentication failure, bounded concurrency/memory; ETags not accepted as universal hashes. |
| A17 | Remote destination | Encrypted bytes before upload; independent read-back; unique prefixes/final manifest publication/collision; interrupted remote transfer and resume; no public ACL or source credentials reused; endpoint/path-style/region/checksum provider matrix. |
| A18 | Vault | Actual encrypted value recoverable on new target with archived root key and no source access; wrong/missing key blocks readiness; target with existing encrypted data refused; no plaintext values in reports. |
| A19 | Functions | All deployed functions enumerated, missing import map/deno.json/shared asset detected, secret digest not value, redeploy without source access, destination-managed env replaced correctly, JWT behavior checked; no unapproved smoke-test side effects. |
| A20 | Config/manual | Field read/write/secret/default/immutable classification; 401/403/404/429/service drift distinct; unsafe GET→PATCH roundtrip prevented; unknown fields reported; SMTP/OAuth missing-secret prompts produce usable recovery tasks and external callback changes. |
| A21 | Side effects | Cron/webhooks/queues/hooks/function startup and provider triggers reviewed; synthetic outbound collector sees no unexpected actions during restore; automation activates only on explicit approval; no emails/SMS to restored users. |
| A22 | Target safety | Same project via alias refused; preexisting tables/data/users/buckets/functions/custom roles/config conflicts refused; target drift between checks stops; disk/plan/extension downgrade unsupported; no retry/clean/CASCADE bypass. |
| A23 | Restore failure | Late DDL rollback test after real objects created; data-load failure/constraint failure; mid-upload/API failure; uncertain COMMIT; partial target documented and quarantined; sequences/external effects not falsely described as rolled back. |
| A24 | Verify | Offline no network; quick not mislabeled deep; same row counts/different content detected by deep; stable duplicate/no-PK encoding; full Storage byte mismatch; source live drift vs corruption; expected destination key/URL changes distinguished from security regression. |
| A25 | Source independence | Actual source DB/API/Storage credential and route denial while target/backup destination allowed; all needed secrets/dependencies in archive or explicit approved replacement inputs; source project stays intact. |
| A26 | Specialty/unknown | Vector/Iceberg/FDW/external/extension data/branch/SSO unknown feature detected; missing permission retained; fail/incomplete rather than silent omission; uploaded manual export validated before calling it recoverable. |
| A27 | OS/filesystems | Native macOS/Windows ACLs, path limits/Unicode/case, removable/mounted folders/disconnect, sleep/wake, disk full/quota, read-only files, antivirus locks, cache cleanup and concurrent runs. |
| A28 | Supply chain | Final release signatures/checksums/provenance/licenses; no PR secrets; dependency/client CVE audit; authentic download on clean minimum OS; no runtime installation or security bypass. |
| A29 | Scale | Target GB workloads plus many-small-file case, measured object counts/elapsed time/throughput/peak RSS/temp disk/egress; cancellation/restart mid-load; correctness/security rechecked after scale run. |
| A30 | Public hygiene | Tracked-file and release-artifact scan; no private keys/passfiles/.env/real archives/project metadata/raw diagnostics; safe support report includes categories/versions rather than secret-bearing dumps. |

## 5. Hosted synthetic fixture design

Do not create this fixture until authorized. Prefer two new dedicated projects and separate private credentials, with a reviewed cost cap and a cleanup decision. Preserve the source until restore is proven; deletion is unnecessary to simulate source loss.

Representative fixture:

- An ordinary `public` application with profiles referencing real `auth.users`, plus a second custom schema and qualified cross-schema dependencies.
- Two designated test users with password sign-in and private owner-specific rows/files; actual IDs/identity relationships preserved. Email delivery disabled or controlled; no real people.
- Custom restricted roles/defaults, table/column/routine grants, forced RLS, security-definer function with safe search path, sequences/identity and a view/type/index/constraint matrix.
- Standard public/private buckets, restrictive MIME/size settings, >1,000 synthetic objects for pagination, unusual keys, empty and multipart-sized files, ownership policies and one app object-ID reference.
- One complete function with dependency file/shared asset and user-supplied secret; controlled safe endpoint for explicitly authorized tests.
- A Vault secret with a canary checked for successful decryption without printing it.
- A disabled cron/webhook/queue fixture with a controlled outbound collector, used only after side-effect suppression is reviewed.
- Non-default supported Auth/Data API/Realtime/Storage settings, one manual SMTP/OAuth requirement, migration history with intentionally failing SQL text that must never execute.
- Separately seeded unsupported specialty/external dependencies to test refusal, not silently extend the successful profile.

Start narrower if needed, but name exactly which subset was tested. Do not call a two-table synthetic schema full application recovery.

### Source-independent run protocol

1. Confirm exact authorized project refs, versions, emptiness and no production data.
2. Seed only the approved synthetic scope; record expected source state and security behavior independently from the code being tested.
3. Establish quiet window; capture archive; verify with a separate reader/check process where possible.
4. Remove source credentials from the restore environment and deny source-specific DB/API/Storage routes using a tested harness. Shared pooler host routing requires care: don't inadvertently deny target or claim host-only blocking proves tenant isolation. Audit outbound calls and fail on source-directed requests.
5. Restore using archive, portable recovery material, destination credentials and explicitly logged manual replacement inputs only. Dependencies hosted outside source must be declared; ideally archive them for reproducibility.
6. Run independent target structural/content/security checks and user-authorized behavioral tests. Do not use source reads as the restoration oracle after the barrier; use pre-capture expected evidence.
7. Confirm no source calls, no unintended effects, correct target keys/endpoints and persistent unresolved task reporting.
8. Publish only redacted version/count/check results. Keep raw credentials/archives/private receipts outside the public repository. Any cleanup/new attempt requires its own authority.

Historical source-file denial with the source still online was weaker evidence than this new target. Retain that distinction.

## 6. Performance and reliability measurements

Define datasets in code, with deterministic non-sensitive values and a recorded seed. Target both throughput and object-count extremes; a 100 GB bucket with ten files does not test a million tiny objects. Set approved synthetic object counts before a costly run, not after.

Measure capture/verification/restore times separately, peak memory, sustained throughput, peak plaintext/encrypted temporary disk, local disk writes, retries, S3 requests, service throttling and estimated/actual egress cost. Record compute/region/network placement so results are interpretable. Test laptop sleep/network interruption and a server close to Supabase; do not promise laptop throughput based on a datacenter-only benchmark.

Bound every collection/queue and document resource knobs: transfer concurrency, part size, retries/timeouts, staging directory and available space. Defaults should be conservative and evidence-based. A single successful run is not a retention/durability guarantee; recommend multiple verified backup copies and periodic restoration.

## 7. Release gates and sign-off

- **Gate A — local foundation:** A01–A08, local portions of A27/A30; no network requirement to run help or inspect a local archive.
- **Gate B — database:** A05/A09–A13/A22–A23 plus exact local fixture evidence; hosted public/security behavior separately required before hosted support.
- **Gate C — core hosted:** A12/A14–A21/A25 plus all advertised feature checks; no source dependency or silent data/security loss.
- **Gate D — remote/reliability:** A08/A16–A17/A24/A27 with real supported providers and clean retry semantics.
- **Gate E — public release:** A28/A30 and [packaging acceptance](04-PACKAGING-AND-DISTRIBUTION.md), user docs/limits, signed exact artifacts and minimum OS matrix.
- **Gate F — supported scale:** A29 and repeatable correctness/security checks at claimed sizes. Do not advertise 10/100 GB support before this gate.

Independent review of restore safety, archive parsing and release chain is recommended before public alpha; who performs it is an owner decision, not permission to launch agents. Findings require regression evidence, not just a reviewer approval label. Publish a clearly bounded alpha if scale or specialty support is incomplete, without weakening core safety.

## 8. Required operator docs before release

`docs/install.md`, `docs/usage.md`, `docs/credentials.md`, `docs/coverage.md`, `docs/archive-format.md`, `docs/restore.md`, `docs/verification.md`, `docs/troubleshooting.md`, `docs/compatibility.md`, `docs/releasing.md`, `SECURITY.md`.

They must explain: recovery-key loss, missing secret/data consequences, file verification versus restore proof, consistency/live drift, required privileges, new API keys/callback URLs, quarantine/activation, egress/temp-space costs, platform support, exact private-file locations, interrupted recovery options, and how to report issues without uploading project data or credentials.
