# Hosted database fixture rehearsal — implementation plan

## Goal and approved boundary

A developer-only, fixed **schema-scoped native PostgreSQL fixture experiment** around the existing SPARC encrypted archive CLI. This is NOT the Supabase backup recipe, native/Supabase dump parity, a full database export, Auth recovery, or production readiness. The offline planner and archive format remain unchanged. Only the parent operator may connect to the two specifically authorized newly created Free disposable projects, with synthetic fixtures and a $0 ceiling. No deletion, password resets, paid features, existing-project changes, email/SMS/webhooks, or Docker setup.

Architecture: Python 3 stdlib runner invokes explicitly configured native PostgreSQL 17 binaries and existing SPARC CLI with rebuilt environments; fixed seed/capture/restore modes, no generic adapter/framework. Restore takes destination configuration, archive and recovery identity only. Source configuration/password are neither accepted nor needed. No dependencies or Rust product changes.

Exact files: this plan; `scripts/hosted_rehearsal.py`; `tests/hosted_rehearsal_test.py`; README pointer.

## Research and recipe choice

Context7 `/supabase/cli` documentation and current [official backup/restore guide](https://supabase.com/docs/guides/platform/migrating-within-supabase/backup-restore), including its upstream MDX, were inspected before edits. The official recipe uses Docker-backed Supabase role/schema/data dumps and transactional psql restoration, with deliberate Auth/Storage/Vault handling. Tagged CLI v2.117.0 [handler](https://github.com/supabase/cli/blob/v2.117.0/apps/cli/src/commands/db/dump/dump.handler.ts), environment/script builders, and retained [hazards](2026-09-18-database-feasibility.md) were inspected. Dry-run resolves connections and prints expanded secrets; linked mode may mutate roles/bans; file output can truncate; stdout retries can concatenate partial dumps. We do not invoke that CLI or reimplement its rewrites. Parent explicitly approved the narrower native fixture experiment instead. PostgreSQL 17.9 clients match the observed hosted 17.6 major; PATH's 18.x clients are not used.

## Fixed fixture and proof

Dedicated, unexposed `sparc_rehearsal` schema only: owners with UUID PK; notes with non-default sequence-backed integer PK, owner UUID FK ON DELETE CASCADE, text/Unicode/newline, nullable and timestamp/default values; one empty table. Existing `authenticated` gets schema USAGE and notes SELECT only; forced RLS owner policy uses synthetic request JWT sub. Synthetic UUIDs are NOT Auth identities. No auth.users inserts. Validate exact data, constraints, defaults, sequence state, object catalog and ACLs, positive and negative RLS. Capture verification is read-only. Restore mutation probes must account for nontransactional sequence behavior: explicitly reset sequence to archived state before committing. Auth remains zero and unexercised.

Source is quiet during capture. Archive contains fixed SQL and bounded recovery metadata identifying the source tenant and SQL digest. Capture must fail without publishing an archive on nonzero/incomplete dump. Restore uses no source access and checks the archived fixture after executing trusted SQL. SQL/archives are executable code: only restore this runner's trusted synthetic archive, never untrusted input.

## Safety contract

Strict bounded duplicate-rejecting JSON configs, exact keys, no passwords/URLs/arbitrary flags. Explicit absolute tool paths, private exactly-one-entry PGPASSFILE matching that config's host/port/database/user (no wildcards), pinned CA, verify-full, session port 5432 only. Validate host and postgres.<expected-ref> tenant routing. This authenticates the shared pooler and relies on Supabase tenant routing plus unique credentials: it is NOT independent backend identity proof or backend TLS. Parent observed backend pg_stat_ssl false; do not claim otherwise.

Private 0700 temporary directories and 0600 plaintext SQL files, short-lived and removed on ordinary exit. No secure erase promise; crashes may leave plaintext. No retries/fallback, no raw subprocess diagnostics, fixed redacted errors, bounded outputs/timeouts. Archive and local workspace publication are no-clobber. Trusted stable local filesystem/tools required; no concurrent path replacement defense. Parent must prevent concurrent target writers.

Refuse source==target, nonempty/unsupported/reused target BEFORE mutation, and repeat guard inside transaction. Target/source-empty guard uses the exact parent-observed baseline: schemas auth/extensions/graphql/graphql_public/pgbouncer/public/realtime/storage/vault; extensions pg_stat_statements 1.11, pgcrypto 1.3, plpgsql 1.0, supabase_vault 0.3.1, uuid-ossp 1.1 in their observed namespaces; managed event triggers and empty supabase_realtime publication. Reject public objects, foreign servers, subscriptions, global default ACLs, Auth users, unexpected schemas/extensions/event triggers/publications. This is a fixed observed baseline, not universal Supabase compatibility. Managed schemas/Storage/Edge Functions are not exhaustively inventoried; operator must independently ensure new unused projects and no automation/customization.

Create schema without PUBLIC access, revoke schema-scoped defaults and validate actual ACLs including defaults/inherited PUBLIC visibility. Do not alter unrelated global defaults or activate publications/automation. Restore and seed use ON_ERROR_STOP, one transaction, bounded locks/statements. On unknown failure stop; no automatic cleanup of hosted objects, retry, reset or deletion. Quarantine target (do not use/activate it), ask owner before remedial writes; failed commits can be ambiguous.

## Runnable red/green checks

1. Add Python unittest fake-process regression first; run it and observe missing-runner failure before implementation.
2. Run `python3 -m unittest discover -s tests -p 'hosted_rehearsal_test.py' -v`. Fake PostgreSQL tools only; archive round trip uses real local SPARC. Cover strict malformed/duplicate/oversize configs, unsafe/reused/same-source targets, wrong versions/CA, hostile env/secrets, partial export, redacted failures, cleanup and independent restore inputs.
3. `python3 -m py_compile scripts/hosted_rehearsal.py tests/hosted_rehearsal_test.py`; `cargo fmt --check`; `cargo test --locked --offline`; `cargo clippy --locked --offline --all-targets -- -D warnings`; `git diff --check`. Normal cargo tests never connect.

## Parent-only live acceptance

After fresh review, parent supplies private external source/target configs, CA and separate exactly-scoped pgpass files, build binary and recovery identity. Run explicit seed, capture and restore commands documented below after implementation. Capture source-independent proof by making source config/passfile unavailable to the restore process (without deleting/pausing either project). Verify archive independently before restore; record only redacted summaries, versions and assertion success. Do not claim live success until parent actually observes it. No second target or retry requiring project deletion is authorized.

Exclusions: full Supabase managed schema/data, Auth users/identities/passwords/sessions/MFA/providers, Storage bytes/ownership, migration history, Vault/root keys/encrypted values, extensions/custom roles/functions/views/trigger recovery, replication/cron/hooks/queues/Edge Functions, project settings, coherent cross-service snapshots, large-database performance, crash recovery/secure erase. These remain later gates.

## Operator configuration and exact commands (parent only)

Never run these merely because the offline tests passed. Fresh review and owner authorization for the exact two projects are prerequisites. The parent independently rechecks new/unused status, public catalog, zero Auth, Storage/Edge Functions and absence of custom automation before seed/restore. These are not automated project-isolation guarantees. Keep the source quiet throughout capture and do not allow target writers.

Create separate external private JSON files with this exact shape (replace placeholders; no password in JSON):

```json
{
  "version": 1,
  "project_ref": "<EXPECTED_SOURCE_OR_TARGET_REF>",
  "host": "<DASHBOARD_SESSION_POOLER_HOST>",
  "pg_bin": "/opt/homebrew/opt/postgresql@17/bin",
  "sparc": "/absolute/repository/target/debug/sparc",
  "pgpass": "/absolute/private/tenant-specific.pgpass",
  "ca": "/absolute/private/verified-supabase-root-2021.crt"
}
```

The host must match `aws-N-xx-region-N.pooler.supabase.com`; expected ref is exactly 20 lowercase letters. Port/database/user are fixed as 5432/postgres/postgres.<ref>. Unexpected provider endpoint shapes require review, not an automatic fallback. Each passfile must be a regular 0600 file in a private directory, exactly one host:5432:postgres:postgres.<ref>:password record (optional final newline); libpq colon/backslash escaping is supported. No comments, wildcard records, unrelated entries, source credentials in destination file, or shared passwords. Parent verifies the authentic CA provenance; the runner checks certificate framing and native libpq performs verify-full certificate/hostname validation. No system-root fallback. All paths and tools are operator-trusted and stable; parent-directory symlinks and concurrent local file replacement are not defended against.

`PYTHON` below is the absolute already-installed Python 3 interpreter; `REPO`, `SOURCE_CONFIG`, `TARGET_CONFIG`, `ARCHIVE`, `IDENTITY` and `SOURCE_DENY_PROFILE` are private operator-side paths, not committed artifacts. `ARCHIVE` and `IDENTITY` must not already exist. No new installation is needed.

```sh
# Offline build and checks first.
cargo build --locked --offline
PYTHONDONTWRITEBYTECODE=1 "$PYTHON" -m unittest discover -s tests -p 'hosted_rehearsal_test.py' -v

# Parent-only live writes: fixed source fixture, after independent empty baseline check.
/usr/bin/env -i PATH=/usr/bin:/bin "$PYTHON" "$REPO/scripts/hosted_rehearsal.py" seed "$SOURCE_CONFIG"
RECIPIENT="$("$REPO/target/debug/sparc" keygen "$IDENTITY")"
/usr/bin/env -i PATH=/usr/bin:/bin "$PYTHON" "$REPO/scripts/hosted_rehearsal.py" capture "$SOURCE_CONFIG" "$ARCHIVE" "$RECIPIENT"
"$REPO/target/debug/sparc" verify "$ARCHIVE" "$IDENTITY"

# Parent prepares/independently probes the macOS sandbox profile to deny reading
# known SOURCE password, pgpass and config paths, while allowing target inputs.
# Source remains ONLINE; this is source-input denial, not a source outage.
/usr/bin/sandbox-exec -f "$SOURCE_DENY_PROFILE" /usr/bin/env -i PATH=/usr/bin:/bin \
  "$PYTHON" "$REPO/scripts/hosted_rehearsal.py" restore "$TARGET_CONFIG" "$ARCHIVE" "$IDENTITY"
```

Do not publish sandbox files, connection configs, archive metadata, temporary paths or raw database/tool diagnostics. The archive privately contains the source ref for same-source refusal; age encryption does not establish who authored SQL. Restore **trusted code only**. Restore reads no source config and receives no source password: the parent additionally denies those known paths at the OS level to the process and descendants. This macOS-specific proof is not a product sandbox or an independent backend-identity proof.

Native execution details: pg_dump plain format, exactly `--schema=sparc_rehearsal --strict-names --no-owner --no-comments --no-security-labels --lock-wait-timeout=5s --no-password`; there is no unrestricted export. Native psql uses `-X -w -qAt --set ON_ERROR_STOP=1 --file -`, no rc files, password prompts or raw connection URL. SQL travels over stdin; connection settings/credential file paths use a rebuilt libpq environment. A 60-second wall deadline, 2 MiB per-process output/file limit, no core dumps, 16 KiB configuration and metadata bounds, and a three-file bounded encrypted archive gate apply. Libpq has a 10-second connection deadline. Initial SQL statement/lock limits are 15/5 seconds, but pg_dump SQL can reset these: the outer wall deadline remains authoritative. No retry is attempted.

Seed and restore each preflight read-only, then repeat baseline checks inside one transaction before CREATE; ordinary psql failures roll back that transaction on disconnect. Target is a dedicated unexposed schema; PUBLIC/default table/sequence grants are removed during seed and dumped, global defaults must be absent, and restored actual ACLs/role capabilities are checked. No custom user functions, publication tables or automation are created. Managed provider event triggers remain active baseline behavior (catalog/REST notifications), not claimed to be disabled. Row/default/FK cascade probes execute only on target, then explicitly set sequence to archived 42/is_called=true and recheck the fixture before commit. Verification under `authenticated` tests owner A, owner B, and a missing owner; these are simulated SQL claims, not Auth sign-in tests.

All temporary SQL is plaintext in an OS temporary 0700 directory with 0600 files, deleted on ordinary success/failure; crashes/SIGKILL can leave it. There is no secure-erase guarantee. An interrupted `pack` can leave partial ciphertext: do not reuse that archive path. A dump failure/incomplete marker does not invoke pack. Output/error text is deliberately generic; it does not echo credentials, SQL or subprocess stderr.

## Failure, rollback and evidence rules

Any guard/version/CA/credential/output assertion failure stops. Before mutation: no hosted changes intended. After seed/restore invocation: quarantine the destination and stop; network loss or timeout near COMMIT is ambiguous. Sequence operations are not ordinarily transactional, and no general rollback guarantee is claimed. Do not retry, manually drop/reset anything, alter unrelated defaults, broaden the baseline, activate automation, delete projects or change credentials without new approval. A successful second restore to the same target is forbidden by its nonempty guard.

Parent acceptance requires: exact redacted tool/server versions, successful authentic-CA verify-full connection, source/target routing distinction, source fixture seed, encrypted pack + verify, successful sandbox-denied-source-input restore into the sole empty target, all row/catalog/default/sequence/FK/ACL/RLS checks, then independent read-only target inventory. Record failure cases without raw diagnostics. Project is still not fully recoverable: Auth remains zero/unexercised and all exclusions above remain open.

## Offline implementation evidence

Observed initial red: `python3 -m unittest discover -s tests -p 'hosted_rehearsal_test.py' -v` failed before production code with `FileNotFoundError` for the missing runner (1 test). Expanded green suite exercises fake native processes and real existing SPARC local encryption; it cannot validate PostgreSQL SQL syntax/permissions/provider behavior. Parent must not treat fake SQL execution as a database round trip. No live operation was performed by the implementation child.

Final child offline checks: Python fake-process suite **13 passed**; Python byte-compilation passed; `cargo fmt --check` passed; `cargo test --locked --offline` **19 passed, 1 optional age test ignored**; `cargo clippy --locked --offline --all-targets -- -D warnings` passed. No Rust source/archive format changed. At that point, fresh independent review and parent-only live acceptance were still pending. Provider SQL execution/permissions are not validated by fake-process tests; the subsequent live evidence is recorded below.

### Parent read-only compatibility check

Fresh spec and safety reviews initially passed. Parent independently reran the offline checks, including the independent age interoperability test. The first actual read-only hosted guard then failed before fixture writes with PostgreSQL SQLSTATE `42725`: concatenating catalog internal `"char"` columns had ambiguous operator resolution. Minimal read-only reproductions confirmed both `evtenabled` and `relkind` failures; explicit `::text` casts resolved them without weakening the guard or broadening the provider baseline. A SQL-text regression failed for both before the two-line correction. It is not a substitute for real PostgreSQL execution.

After correction, **14 Python tests passed**, both actual hosted empty-database guards passed, and an OS-level preflight denied opening all three known source credential/config files while allowing destination inputs. This established only read-only compatibility and source-input denial preparation. The retained independent safety reviewer rechecked the correction, scanned remaining catalog concatenations, reran all 14 offline tests, and returned OK before live writes.

## Observed hosted result

**2026-09-18: the fixed synthetic fixture round trip passed.** This is a small feasibility result, not full database or Supabase project recovery.

Environment: two newly provisioned owner-authorized Free projects in Oregon; both PostgreSQL **17.6**; native `psql`/`pg_dump` **17.9**; Python **3.14.7** on macOS. Client connections used `verify-full` with the authentic dashboard-linked Supabase CA. No paid feature, Docker installation, existing-project changes, outbound fixture integrations, or project deletion occurred. Both projects were retained.

| Gate | Observed result |
| --- | --- |
| Initial scope | Both database guards passed; Auth users, Storage buckets and Vault secrets absent; no user publication tables. Both dashboards showed no deployed Edge Functions. |
| Source seed | Fixed schema created transactionally; all data/catalog/ACL/RLS assertions passed. |
| Capture | Schema-scoped native dump completed; source assertions passed before and after capture. |
| Encrypted archive | Two artifacts totaling **4,508 plaintext bytes**, stored as three age ciphertext files totaling **5,798 bytes**, including encrypted manifest. Independent `sparc verify` passed. |
| Source-independent restore | Destination-only config plus archive/identity; fresh controlled environment; macOS process sandbox denied reads of the known source password, pgpass and config files to the process and descendants. Source project remained online. |
| Live restore assertions | Schema/data import, defaults, generated ID, FK delete cascade, restricted ACLs and positive/negative RLS passed before commit. Sequence explicitly returned to **42 / is_called=true** after probes. |
| Independent destination check | Read-only queries confirmed **3 tables**, **2 owners**, **2 notes**, empty third table, exact UUID relationships and Unicode/newline/quote/backslash/null/timestamp values. Sequence state and forced RLS matched; simulated owners saw only IDs 41 and 42 respectively; a missing owner saw no rows. Auth/Storage/Vault and public relations remained empty. |
| Reused-target safety | After restore, the empty-target guard was separately tested in a read-only transaction and rejected the used target with expected SQLSTATE `P0001`. No second restore was attempted. |

The independent destination verifier also rechecked OS denial of all three known source input paths. This proves this execution did not need source credentials/configuration; it does **not** prove a source network outage, protection against malicious local code, or a general product sandbox. Shared-pooler TLS is not backend TLS or independent cryptographic backend identity.

All credentials, recovery identity, SQL staging, encrypted archives and private connection metadata stayed outside Git. The identity is an **unencrypted private file**, stored separately from the archive; losing it prevents recovery. Normal temporary SQL cleanup completed; secure erasure and crash cleanup are not claimed.

**Still unproven:** Auth accounts/login/session recovery; supported Supabase roles/schema/data recipe parity; managed/customized schemas; Storage bytes; Vault/root keys; functions/configuration; coherent cross-service snapshots; and **10 GB database / 100 GB Storage** scale. The two retained projects now contain the fixture and are no longer empty restore targets. Do not reset or reuse them for another restore without new owner authorization.
