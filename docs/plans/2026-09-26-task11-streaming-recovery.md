# Task 11 — Streaming Database Recovery Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Add production-shaped typed PostgreSQL dump/restore APIs and prove one encrypted, source-independent PG17 recovery on two disposable local clusters.

**Architecture:** Extend `internal/tools` with closed, typed dump and restore operations while preserving its compiled-in payload selector; no path, free-form arguments, or environment are accepted. Stream `pg_dump` output directly into the existing encrypted archive publisher, and stream a verified/decrypted archive component to `pg_restore` stdin using fixed error-stop/single-transaction flags. The database layer performs bounded PG17/TLS/read-only observation and an empty-target check. The current production client inventory remains empty, so production operations still fail closed with `ErrPayloadUnavailable`; an explicitly selected private candidate is reachable only through an `integration`-tagged test helper and a package-private database test seam.

**Tech Stack:** Go, existing `internal/tools` process lifecycle, pgx catalog observation, encrypted `internal/archive`, private `internal/destination`, local PostgreSQL 17 fixtures.

## Scope boundaries

- Local synthetic PostgreSQL only; no hosted connection, credentials, backup, or restore.
- One full custom-format database component, marked incomplete because globals/roles and Supabase-managed services are not covered.
- No production payload approval, normal CLI wiring, automatic role provisioning, `--clean`, `--create`, or plaintext dump file.
- Capture requires an explicitly verified TLS CA. Restore request contains target and archive only—never a source connection.

---

### Task 1: Specify typed dump/restore tool operations

**Files:**
- Modify: `internal/tools/run.go`
- Modify: `internal/tools/run_test.go`

**Step 1: Add failing contract tests.**

Test that only valid mode/tool combinations are accepted: version for a selected tool, dump for `pg_dump` with a TLS root, and restore for `pg_restore` with a nonnil bounded input reader and TLS root. Verify malformed combinations, missing TLS roots, and input on dump/version are rejected before process start. Assert exact fixed dump and restore argv contains no password; restore must include `--single-transaction` and `--exit-on-error` and no cleanup/overwrite flag.

**Step 2: Run RED.**

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
go test -mod=readonly ./internal/tools -run 'Test.*(Dump|Restore)' -count=1 -v
```

Expected: compile/test failure because typed operations do not exist.

**Step 3: Implement the minimum typed requests and fixed arguments.**

Keep version execution compatible with existing callers. Do not add raw args, executable paths, schema patterns, or ambient environment. Require `verify-full` and valid explicit CA PEM for database operations; write the CA into the private per-run operation directory and use only the corresponding explicit libpq settings. Continue using private PGPASSFILE for the password.

**Step 4: Re-run focused tests.** Expected: PASS.

**Step 5: Commit.**

```sh
git add internal/tools/run.go internal/tools/run_test.go
git commit -m "feat: add typed PostgreSQL stream operations"
```

### Task 2: Stream restore input with bounded process ownership

**Files:**
- Modify: `internal/tools/run.go`
- Modify: `internal/tools/run_test.go`

**Step 1: Write failing process tests.**

Use the existing fake process seams to prove input bytes reach child stdin, EOF closes stdin, an input limit rejects excess bytes, reader/write/close errors fail with fixed sentinels, cancellation closes owned input and pipes, early child exit does not leak a pump, and all workers join before return.

**Step 2: Run focused RED.** Use the Task 1 command; expected failures for unimplemented stdin streaming/lifecycle.

**Step 3: Implement a bounded stdin pipe and pump.**

The restore request transfers ownership of its input `io.ReadCloser`; cleanup closes it and joins its pump. Preserve existing `CloseContext` process ownership rules. Add error-aware output-sink abort semantics so a failed `pg_dump` closes the archive pipe with an error; otherwise a child that writes a valid prefix then exits nonzero could publish a truncated archive.

**Step 4: Re-run focused runner tests.** Expected: PASS.

**Step 5: Commit.**

```sh
git add internal/tools/run.go internal/tools/run_test.go
git commit -m "feat: bound PostgreSQL restore input streams"
```

### Task 3: Add integration-only private candidate runner

**Files:**
- Create/modify: `internal/tools/candidate_integration.go`
- Modify: `internal/tools/candidate_integration_test.go`

**Step 1: Write failing tagged tests.**

Use an explicitly supplied clean absolute `SPARC_TEST_PG_CLIENT_BIN`; package exactly the existing six-file private closure, extract through the trusted extractor, and exercise typed dump/restore requests. No helper is compiled without the `integration` build tag.

**Step 2: Run focused RED.**

```sh
SPARC_TEST_PG_CLIENT_BIN=<private-client>/bin \
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
go test -mod=readonly -tags=integration ./internal/tools \
  -run '^TestOptInCandidate' -count=1 -v
```

Expected: failure because the integration-only typed candidate runner is missing.

**Step 3: Implement the smallest integration-only adapter.**

Reuse candidate manifest/extraction checks; allow loopback `hostaddr` only in this build-tagged test helper. Never let it populate `productionPayloads` or be callable in default builds.

**Step 4: Re-run focused tagged tests.** Expected: PASS.

**Step 5: Commit.**

```sh
git add internal/tools/candidate_integration.go internal/tools/candidate_integration_test.go
git commit -m "test: exercise typed client streams with private candidate"
```

### Task 4: Add database capture and restore APIs

**Files:**
- Create: `internal/database/capture.go`, `internal/database/restore.go`
- Test: `internal/database/recovery_test.go`
- Modify: `internal/database/recovery_integration_test.go`

**Step 1: Write failing unit tests for orchestration.**

Test source-version/TLS/read-only refusal, archive publication failure, dump failure with no final archive, archive verification before target observation, malformed/wrong-component refusal, target preflight before restore, and that restore has no source connection field. Use a package-private ops seam only; do not expose runner injection in production APIs.

**Step 2: Run RED.**

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
go test -mod=readonly ./internal/database -run 'Test(Capture|Restore)' -count=1 -v
```

Expected: compile failure because the API does not exist.

**Step 3: Implement capture.**

Observe the source using existing validated PG17 TLS/read-only catalog code. Start typed `pg_dump --format=custom` with no output filename. Pipe stdout directly to `destination.Create`; abort the pipe on process failure so a partial archive is never published. Mark the database component incomplete and use a fixed logical key/scope. No plaintext staging.

**Step 4: Implement restore.**

Verify the encrypted archive and exact single database component before target access. Observe target read-only, apply `CheckEmptyTargetV1` to the declared application scope, then decrypt the component and stream it to typed `pg_restore --single-transaction --exit-on-error`. Request data contains the target only. Return fixed non-secret errors; never normalize roles or retry ambiguous mutation.

**Step 5: Run focused tests and commit.**

```sh
git add internal/database/capture.go internal/database/restore.go internal/database/recovery_test.go
git commit -m "feat: add bounded database capture and restore APIs"
```

### Task 5: Prove encrypted cross-cluster recovery locally

**Files:**
- Modify: `internal/database/recovery_integration_test.go`
- Modify: `docs/research/R04-database-route.md`
- Modify: `docs/research/R05-permissions.md`
- Modify: `docs/progress.md`

**Step 1: Write the failing integration test.**

Create independently initialized TLS PostgreSQL 17 source and target clusters and dedicated databases. Seed only synthetic objects: a restricted owner, schema/table, row, sequence, and a migration-history canary stored as inert text. Capture to a private encrypted archive; verify it offline; stop the source; require target empty-scope preflight; prove missing owner fails with transaction rollback and no target schema; explicitly provision only the synthetic target role; restore again and verify rows, sequence, owner, and inert history text. Confirm archive integrity after both attempts. The test uses the package-private ops seam with the integration-only private candidate; no fixture dump or credentials enter Git.

**Step 2: Run RED.**

```sh
SPARC_TEST_PG_BIN=/opt/homebrew/opt/postgresql@17/bin \
SPARC_TEST_PG_CLIENT_BIN=<private-client>/bin \
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
go test -mod=readonly -tags=integration ./internal/database \
  -run '^TestEncryptedCrossClusterRecovery$' -count=1 -v
```

Expected: failure because the encrypted cross-cluster API path is not implemented.

**Step 3: Implement only the fixture needed by this proof.**

Use existing disposable fixture, CA, archive, destination, and verify helpers. The production payload manifest stays empty. No hosted endpoint or service-owned test fixture.

**Step 4: Re-run focused unit + integration tests.** Expected: PASS.

**Step 5: Record exact evidence and limits.**

State that this proves only one synthetic PostgreSQL 17 local recipe. Mark omissions: hosted `public`/managed baseline, Auth, Storage, Vault, Functions, global role recovery, broad migration-history policy, side-effect suppression, late-DDL rollback beyond the specific tested owner-failure transaction, signing, release payload, and redistribution.

**Step 6: Commit.**

```sh
git add internal/database/recovery_integration_test.go \
  docs/research/R04-database-route.md docs/research/R05-permissions.md docs/progress.md
git commit -m "test: prove encrypted local cross-cluster recovery"
```

### Task 6: Fresh guarded verification and push

Run fresh default tests, full database integration and race suites, tool integration tests with the private candidate, and default/integration vet using `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`. Confirm `productionPayloads` remains empty, `git diff --check` is clean, no candidate artifacts or credentials are tracked, then push the verified commits to `main` under existing authorization.
