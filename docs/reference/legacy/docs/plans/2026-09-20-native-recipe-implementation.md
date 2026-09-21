# Native Database Recipe Rehearsal Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Exercise pinned Supabase schema/data dump scripts against the existing synthetic schema using native PostgreSQL 17 clients, preserving verified TLS and private credential handling.

**Architecture:** A developer-only Python adapter reuses `scripts/hosted_rehearsal.py` validation, bounded subprocess execution and SQL assertions. Upstream scripts remain external, exact-hash-verified inputs; they are not rewritten or vendored into the archive engine. No desktop changes, project deletion, source writes, or general SQL restore interface.

**Tech Stack:** Python standard library/unittest, system bash/sed, native PostgreSQL 17, existing SPARC CLI and age archives.

---

Work directly on main, one writer, no worktrees. The owner approved this native-client route after the documented CLI TLS propagation finding. Native-client results must not be described as CLI/Docker parity.

### Task 1: Offline script runner

Files: create `scripts/native_recipe.py`, `tests/native_recipe_test.py`.

1. Write failing unittest cases for exact script hash validation; schema/data execution through a fake `pg_dump`; required `verify-full`, CA, passfile, source-read-only settings; no ambient credential inheritance; invalid mode and modified-script refusal.
2. Run `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -p 'native_recipe_test.py' -v`; expect missing-module failure initially.
3. Implement only schema/data modes. Read scripts using the existing bounded regular-file reader. Require SHA-256 values from pinned commit `21db855916f2c2b12f61cde923a27094b8528b23`:
   - schema: `5cd57189f6565ddf651ff149995398a4c9b1971ca34a0093a77c011a41f21d64`
   - data: `c943c7a926122ea0649ddd4bf9b8fb9b12bed23ac9f33da0b489ac83e687d241`
4. Execute `/bin/bash` with script bytes on stdin via the existing bounded `run`. Use the configured PostgreSQL bin directory before `/usr/bin:/bin`. Define the upstream-required empty PGPASSWORD so libpq can use the scoped passfile; never load a password into script/argv. Force the fixture schema and read-only source sessions. Keep `PGSSLMODE=verify-full` and `PGSSLROOTCERT` from the shared runner.
5. Rerun new tests and the 14 existing harness tests. Commit only verified code and design/plan.

### Task 2: Private capture and negative TLS proof

Files: extend `scripts/native_recipe.py`, `tests/native_recipe_test.py`; parent-only orchestration in ignored `.sparc-local/`.

1. Add RED tests for two-script capture, fixed artifact inventory and hashes, different source/destination refs, bounded output, no-clobber and failure redaction.
2. Implement using shared source guards before/after capture, private staging, recovery metadata and existing encrypted pack/verify. Do not dump roles or migration history into an executable restore under schema-only authority.
3. Confirm native client versions. Prove bad CA and hostname mismatch fail before valid source capture; do not expose raw diagnostics or broaden TLS trust. The correct CA must succeed through the same runner.
4. Privately inspect script output and record hashes/counts only. Upstream transformations remove psql restrict/unrestrict; this experiment accepts only locally generated trusted fixture SQL, not user-supplied archives. Do not weaken the general archive engine's handling.

### Task 3: Transactional destination preparation

Files: parent-only cleanup helper under `.sparc-local/`; tests for its SQL contract if promoted to tracked code.

1. Verify exact destination ref, read-only baseline and existing fixture assertions. Preserve prior verified archive evidence.
2. Build explicit dependency-ordered `DROP ... RESTRICT` operations for known fixture objects; no CASCADE. Lock fixture tables and rerun checks in the same transaction. Unexpected dependencies must abort.
3. Verify empty-target guard before commit. Make no changes outside `sparc_rehearsal`; stop if managed/provider drift or extra objects require broader cleanup.

### Task 4: Trusted fixture restore and verification

Files: extend developer adapter/tests only after capture inventory is known.

1. RED tests for archive verification before SQL, exact metadata/inventory, used-target rejection, bounded failures and transactional restore assertions.
2. Restore pinned schema/data artifacts in order with error-stop enabled, restore session replication role to origin before probes, and independently check fixture ACL/RLS/data/sequence behavior. Reuse existing guards/probes; reset sequence after probes.
3. Execute parent-only with destination config/archive/identity and OS denial of known source credential/config paths. No reseeding or writes to the source.
4. Recheck destination read-only; failures quarantine the target rather than automatically retrying.

### Task 5: Publish bounded evidence

Files: update preflight and rehearsal results documentation.

Run both Python suites, root Rust tests and independent age interop, diff hygiene and tracked-secret/artifact checks. Record exact upstream/client provenance, scope, hashes/counts, TLS negatives and hosted result. Full database, custom roles, migration history, Auth, Storage, functions, Vault and scale remain unproven. Commit/push only passing milestones; no desktop integration until this bounded procedure is proven.

## Observed result — 2026-09-20

The bounded hosted round trip passed. It establishes the pinned upstream **schema/data scripts with native PostgreSQL 17.9** for this synthetic schema on hosted PostgreSQL 17.6, not unmodified CLI/Docker parity or full database recovery.

- Offline suite: **23 tests passed** (14 existing harness tests plus 9 new recipe tests). Expected missing-module/function failures were observed before implementing each new behavior. Fake-process tests cover the runner boundary, not TLS itself or PostgreSQL semantics.
- Live native `pg_dump` negative tests rejected an unrelated CA with certificate-verification failure and an IP substituted for the hostname with a hostname-mismatch failure. These probes used the same native binary and explicit verification settings; the complete script runner subsequently captured successfully using the correct CA/host. Client TLS terminates at the provider's session pooler; it does not independently identify the backend project.
- Source fixture checks passed before and after capture. No source writes occurred. The source remained online; this is not a network-disconnected-source test.
- Capture contained `schema.sql`, `data.sql`, and `recovery.json`: **6,256 plaintext bytes**, **four ciphertext files** including the encrypted manifest. Independent archive verification passed.
- Recorded SQL SHA-256 values were `695211b19f1d92dc7c18a217a223951015c757aba9dd4e23cced30948aabbebb` (schema) and `435361b15dc061637bd3d204e24f6e0eaa2ad7a9ae8915a7c2a8450997093186` (data). The parent-only restore helper required these exact locally observed hashes before cleanup; generic imported SQL archives are not authorized inputs.
- Both the prior recovery archive and the new archive verified before cleanup. Explicit dependency-ordered `DROP TABLE ... RESTRICT` and `DROP SCHEMA ... RESTRICT` ran transactionally, with fixture assertions and an empty-target postcondition. Neither project was deleted/reset; no other schema was explicitly changed.
- Destination-only restore and independent read-only verification ran with known source password, passfile and config paths OS-denied. A durable single-attempt fence prevents automatic replay of the operator cleanup script.
- Exact UUID relationships, Unicode/newline/quote/backslash/null/timestamp values, 2 owner rows, 2 note rows, an empty third table, sequence **42 / is_called=true**, FK cascade/default probes, actual ACLs, forced RLS and owner/nonowner visibility all passed. Auth users, Storage buckets, Vault secrets, public relations and publication tables remained zero.

Private credentials, identities, SQL artifacts, archives, operator helpers and receipts remain outside tracked files. `scripts/native_recipe.py` is a developer-only adapter with no standalone hosted command or desktop integration. Its capture/restore helpers expect validated config; the parent must enforce project authorization and trust in locally generated SQL. Upstream SQL rewrites and stripped psql restrict commands are not a general safety guarantee, and encryption alone does not authenticate the archive's sender.

Both projects are retained with the fixture. The authorized cleanup/reuse has now been consumed; another cleanup requires new authorization. Custom roles, migration-history recovery, populated Auth/managed schemas, Storage bytes, functions, Vault, arbitrary database coverage and scale are still unproven. The next scope must be defined separately rather than extending this schema-only authorization.
