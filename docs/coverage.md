# Coverage contract

SPARC records every inventory component even when it is absent, excluded, manual,
unsupported, denied, or unknown. This is an initial classification, **not a support
claim**. The report contract is `internal/operation/report.go`; reports contain IDs,
states, and allowlisted non-secret reason codes only—never payloads, credentials,
tokens, or secret values.

## Report states

Each component has separate JSON fields: `observation`, `read_permission`, `capture`,
`byte_integrity`, `recoverability_prerequisite`, `restore`, and
`behavior_verification`. `structured_requirement_ids` and `manual_requirement_ids`
reference future structured recovery records without embedding their data. The
`not_used_reason` field is an optional stable code, not public diagnostic text:
`feature_not_observed` and `outside_declared_scope` are the only v1 codes. The former
requires the reliable-absence tuple; the latter requires `excluded` support. A reason is
invalid when neither `not_used` nor exclusion requires one.

- Capture states: `not_used`, `captured`, `partially_captured`, `manual_required`,
  `unsupported`, `permission_denied`, `unknown`, `failed`.
- Integrity states: `not_used`, `not_checked`, `passed`, `failed`, `inconclusive`.
- Recovery states: `not_used`, `ready`, `missing_critical_material`,
  `manual_required`, `unknown`.
- Restore states: `not_used`, `not_attempted`, `restored`, `manual_required`,
  `unsupported`, `failed`, `unknown`.
- Behavior states: `not_used`, `not_checked`, `sampled`, `passed`, `failed`,
  `inconclusive`.

`coverage: complete` is derived only from observation, read permission, capture, byte
integrity, and recovery-prerequisite gates. It deliberately does not require a restore or
behavior check: a complete backup before restore reports `restore: not_run`; a later failed
restore preserves `coverage: complete` and reports `restore: failed`. Restore is derived
separately: actual restore plus behavior-passed evidence for every applicable component is
`success`; all untouched or reliably absent components are `not_run`; mixed/blocked work is
`blocked`; only restore or behavior failures are `failed`. Failure has highest precedence,
then incomplete coverage is `blocked`. Only after every applicable component is restored
can sampled or inconclusive behavior be `inconclusive`; unchecked/non-passed behavior is
otherwise `blocked`. That behavior reduction scans every applicable component, so sampled
results never mask unchecked results based on component order. Capture or byte-integrity
failure before a restore is
coverage-incomplete and restore-`blocked`, not a restore failure.

`permission_denied`, `unknown`, `unsupported`, `partially_captured`,
`manual_required`, `failed`, `missing_critical_material`, and failed or inconclusive
integrity make coverage incomplete or restore non-success as applicable. A `not_used`
component must remain listed with a reason; it cannot hide one of those blocking
conditions. A reliably absent component has `not_observed`, `not_required`, every other
gate `not_used`, and an allowlisted reason code; it may leave coverage complete but never
makes restore successful. `not_observed` is invalid with any other tuple. Passed integrity
requires captured or partially captured bytes; a fully captured component cannot call
integrity `not_used`. Restore/behavior pairs are likewise direct: `not_attempted` requires
`not_checked`, `not_used` requires `not_used`, and active behavior evidence requires
`restored`. `unsupported` and `detect_and_block` use that tuple as their only non-blocking
absence path; observed, unknown, denied, or otherwise non-absent cases remain explicit and
incomplete/blocked. `excluded` is more conservative: it requires a reason and remains an
incomplete declared scope even if absent. File integrity is not restore or behavior proof.
A real restore rehearsal is separately required for `restore_tested` evidence.

## Initial inventory handling

`Required` means a detected/in-scope component blocks completeness until its named
evidence exists. `Manual` means an explicit replacement/action requirement remains in
the report. `Unsupported` means refuse/incomplete if present. `Detect-and-block` means
reliably discover it first, then refuse/incomplete if found. Every row below names the
research blocker(s); no row silently disappears.

| Inventory family | Initial handling | Blocking research / required outcome |
| --- | --- | --- |
| Application schemas and data | Required | R04 recipe; R15 specialty durable data; prove dependency-complete supported scope. |
| Application security (owners, ACLs, RLS, defaults) | Required | R05; verify no broadened access. |
| Provider-owned `public` | Required | R04/R05; no hosted claim before ordinary `public` is qualified. |
| Auth records | Required | R04/R06; use a tested database recipe and real login behavior. |
| Auth sessions, MFA, passkeys and bindings | Manual | R06; report reauthentication/reenrollment limits. |
| Managed schema modifications | Required | R04/R05/R06/R07; do not replay provider base DDL. |
| Migration and seed history | Required | R04; preserve as inert data, never replay statements. |
| Extensions and extension-managed data | Detect-and-block | R04/R15; qualify each supported extension/version or refuse. |
| Cron, webhooks and queues | Detect-and-block | R12/R15; restore only through a reviewed inactive/activation path. |
| Vault/pgsodium | Required when detected | R08; missing root/recovery material is `missing_critical_material`. |
| FDWs, foreign rows and external connections | Detect-and-block | R15; external rows/credentials are not silently copied. |
| Replication, Realtime DB, publications and slots | Detect-and-block | R15; do not copy live positions. |
| Standard Storage buckets/settings | Required | R07; preserve qualified settings and safe exposure state. |
| Standard Storage object bytes/metadata | Required | R07; every page/byte and qualified digest/metadata handling. |
| Storage identity, ownership and RLS | Required | R07; ownership/ID loss blocks application-ready recovery. |
| Storage history/versioning | Manual | R07; label current-state-only until history is qualified. |
| Vector Storage | Unsupported | R15; reliable detection plus explicit incomplete/refusal or qualified export/rebuild. |
| Analytics/Iceberg Storage | Unsupported | R15; reliable detection plus explicit incomplete/refusal or qualified export/rebuild. |
| Edge Function bodies/packages | Required | R09; package must include dependencies, imports/assets and deployment requirements. |
| Function secrets | Manual | R09/R10; record names/digests and collect approved replacements, never values. |
| Auth settings | Required | R10; endpoint field classification and safe writable allowlist. |
| OAuth/OIDC providers | Manual | R06/R10; credentials and external callbacks are explicit requirements. |
| SAML/SSO/third-party Auth | Detect-and-block | R06/R10; refuse/incomplete until IDs, metadata and bindings are qualified. |
| Supabase OAuth server/apps | Detect-and-block | R06/R10; do not confuse with management integration OAuth. |
| SMTP/email/SMS | Manual | R10; external credentials and delivery/callback tasks remain explicit. |
| Auth hooks and MFA policy | Detect-and-block | R06/R10/R12; keep unresolved hooks inactive. |
| API/JWT keys | Manual | R10; destination-managed keys replace non-exportable source keys. |
| Data API, Realtime and Storage service settings | Manual | R10; classify writable versus immutable/unknown fields. |
| Project infrastructure/plan/capacity | Manual | R03/R10; record destination compatibility and operator action. |
| Network security and routing | Required | R03/R10; direct/session only, strict TLS, no transaction pooler. |
| Custom domains | Manual | R10; external DNS/callback cutover is not automatic. |
| Backups, PITR and add-ons | Manual | R10; record intent only, no physical-history portability claim. |
| External integrations | Manual | R10/R12; external owner/action/dependency requirements remain explicit. |
| Logs and monitoring | Manual | R10; not replayable state or complete history. |
| Ancillary project assets | Manual | R10; only explicitly supplied accessible artifacts, never execute snippets. |
| Organizations and branches | Manual | R10/R15; reference only; selected branches are separate archives. |

## Evidence gates

The contract supports, but does not satisfy, A01–A30. In particular: A05/A09–A13
cover the database route and managed/Auth behavior; A14–A16 Storage; A18 Vault; A19
Functions; A20 config/manual requirements; A21 side effects; A24 verification; A25
source independence; A26 specialty/unknown features; and A30 safe public reporting.
A finalized archive may be physically intact but coverage-incomplete. A corrupt or
interrupted archive must not appear finalized.
