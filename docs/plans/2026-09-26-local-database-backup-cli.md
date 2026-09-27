# Local Database Backup CLI Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Make `sparc backup` create an encrypted local PostgreSQL database archive from a Supabase session-pooler connection using an explicitly supplied PostgreSQL 17 client directory.

**Architecture:** Keep the current signed/bundled payload boundary intact: `tools.Run` remains the release-path runner with an empty production inventory. Add a separate, explicit unbundled-client runner for the current macOS arm64 developer environment; it validates and privately re-packages the selected local PostgreSQL client before executing the same closed dump operation. The CLI reads the session URL and archive passphrase from private files or an interactive passphrase prompt, parses only the qualified Supavisor session route, then streams the dump through the existing encrypted local destination.

**Tech Stack:** Go standard library, pgx route validation, existing encrypted archive/destination packages, PostgreSQL 17 `pg_dump` supplied by the caller.

---

## User contract

```text
sparc backup \
  --project-ref <Supabase project ref> \
  --database-url-file /absolute/private/session-url \
  --archive /absolute/private-parent/new-archive-dir \
  --pg-client-bin /absolute/path/to/pg17/bin \
  [--passphrase-file /absolute/private/passphrase]
```

- `--database-url-file` is a private file containing only a session-pooler URL; neither its contents nor a password are accepted in argv.
- `--pg-client-bin` is explicit opt-in to a caller-supplied PostgreSQL 17 macOS arm64 client directory. It is not `PATH` discovery, a bundled payload, or portable/release support.
- The archive destination must be new and have an existing private parent directory. It is a local archive directory, not remote storage.
- A successful dump writes one encrypted **database** component with capture status `incomplete`; Auth, Storage, project config, roles/globals and provider-managed services are excluded. The command returns exit code `3` after archive creation to preserve the existing intact-but-incomplete contract.
- The command is ready for a separately authorized hosted source; this plan does not perform a hosted connection or mutation.

### Task 1: Move the qualified session URL parser into production code

**Files:**
- Create: `internal/database/session_url.go`
- Create: `internal/database/session_url_test.go`
- Modify: `internal/database/hosted_probe_test.go`

**Step 1: Write failing tests**

Test exported `database.ParseSupavisorSessionURL` accepts only a `postgres`/`postgresql` session-pooler URL for a caller-provided project ref and maps it to `verify-full` plus the embedded Supabase CA. Cover passwords with encoded punctuation and reject direct routes, port 6543, wrong tenant username/database, unknown query parameters, fragments and a malformed/missing password. Assert errors never contain a synthetic password.

**Step 2: Run the focused unit test**

Run: `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -mod=readonly ./internal/database -run '^TestParseSupavisorSessionURL' -count=1`

Expected: failure because the production parser does not exist.

**Step 3: Implement minimal parser**

Move the closed parser from the hosted test into `session_url.go`. Do not accept direct or transaction-pooler routes. Retain the strict URL shape, known `sslmode` options only, explicit port 5432/database `postgres`, exact project-bound username, `verify-full`, `SSLRootCert: "supabase"`, and passfile-compatible password validation. Keep public errors at `ErrConnectionParameters`.

**Step 4: Update hosted test**

Make the hosted probe call the exported parser; remove its duplicate private parser and imports that are no longer needed.

**Step 5: Run focused tests**

Run the unit test and `go test -mod=readonly -tags=hosted ./internal/database -run '^TestParseHosted' -count=1` with hosted environment variables unset.

**Step 6: Commit**

```sh
git add internal/database/session_url.go internal/database/session_url_test.go internal/database/hosted_probe_test.go
git commit -m "feat: expose qualified Supavisor session parsing"
```

### Task 2: Add an explicit unbundled PostgreSQL client runner

**Files:**
- Create: `internal/tools/external.go`
- Create: `internal/tools/external_test.go`
- Modify: `internal/tools/candidate_integration.go`
- Modify: `internal/tools/candidate_integration_test.go`

**Step 1: Write failing tests**

Test `tools.RunExternal` refuses relative/non-`bin` paths, invalid run requests and unsupported runtime before executing. Under the existing integration fixture, prove a qualified TLS dump can use an explicit macOS arm64 PostgreSQL 17 client directory without `PGHOSTADDR` loopback override; preserve the separate loopback-only integration candidate behavior.

**Step 2: Run the focused test**

Run the focused default test first (expected missing API), then the integration test with the approved local candidate path.

**Step 3: Implement minimal runner**

Factor the current candidate archive/extraction mechanism into a reusable private helper. `RunExternal` accepts the exact client `bin` path and executes the existing closed `RunRequest` through the private extracted package without setting `testHostAddr`. Retain macOS arm64 restriction, exact six-file inventory, hash validation, private cache/extraction, explicit CA/passfile, fixed arguments and no `PATH` fallback. Keep `RunCandidate` integration-only and retain its loopback pin.

**Step 4: Run tools tests**

Run default and integration-tagged tools tests.

**Step 5: Commit**

```sh
git add internal/tools/external.go internal/tools/external_test.go internal/tools/candidate_integration.go internal/tools/candidate_integration_test.go
git commit -m "feat: add explicit unbundled postgres runner"
```

### Task 3: Connect explicit client capture to the CLI

**Files:**
- Modify: `internal/database/capture.go`
- Modify: `internal/database/recovery_test.go`
- Create: `internal/cli/backup.go`
- Create: `internal/cli/backup_test.go`
- Modify: `internal/cli/run.go`
- Modify: `internal/cli/run_test.go`

**Step 1: Write failing CLI tests**

Add a `runBackupWith` seam and test valid flags build a `database.CaptureRequest` with parsed source data, archive path and passphrase while invoking the explicit-client capture callback. Test missing/extra/malformed flags, non-terminal stdin without a passphrase file, unreadable URL file, invalid URL, capture failure, and output failures return redacted diagnostics with no URL/password/passphrase echo. Test success prints the archive’s incomplete declaration and exits `3`.

**Step 2: Run the focused CLI test**

Run: `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -mod=readonly ./internal/cli -run '^TestRunBackup' -count=1`

Expected: failure because backup is unavailable.

**Step 3: Add explicit-client capture API**

Add `database.CaptureWithExternalClient(ctx, clientBin, request)`, implemented by replacing only the `run` operation in the existing `Capture` pipeline with `tools.RunExternal`. `Capture` keeps its existing bundled-client behavior. Test that the external-client path retains source observation, stream abort and archive-manifest checks.

**Step 4: Implement CLI**

Implement `backup.go` with fixed flags from the user contract. Read the database URL only through `credentials.Input` from the private file; parse it through `database.ParseSupavisorSessionURL`; prompt for or privately read the archive passphrase; clear both buffers after use. Call `CaptureWithExternalClient(context.Background(), ...)`. On archive creation print only `Database archive created. Capture declaration: incomplete.` and return `3`. Do not print selected paths, refs, URLs, or error internals.

**Step 5: Update help and command dispatch**

Describe backup as an explicit local encrypted database archive command; keep restore unavailable and do not imply full-project recovery.

**Step 6: Run focused tests**

Run default database and CLI tests.

**Step 7: Commit**

```sh
git add internal/database/capture.go internal/database/recovery_test.go internal/cli/backup.go internal/cli/backup_test.go internal/cli/run.go internal/cli/run_test.go
git commit -m "feat: add local encrypted database backup command"
```

### Task 4: Demonstrate the local database vertical slice and document its limit

**Files:**
- Modify: `docs/progress.md`
- Modify: `README.md`
- Modify: `docs/coverage.md`

**Step 1: Update documentation**

State the exact command and its narrow status: operational only with an explicitly supplied macOS arm64 PostgreSQL 17 client and an authorized qualified Supavisor session URL; output is one encrypted local database archive marked incomplete. State plainly that it does not back up Auth, Storage, Functions, Vault, project configuration, roles/globals, remote folders, or restore.

**Step 2: Execute the local demo**

Use `sparc-localdemo` with `SPARC_TEST_PG_BIN` and a new private archive folder to produce and verify an encrypted archive from its disposable PostgreSQL 17 fixture. This proves the local archive path, not Supabase capture. Do not use a hosted connection in this task.

**Step 3: Full verification**

Run:

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -mod=readonly ./... -count=1 -timeout=240s
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -mod=readonly -race -p=1 ./... -count=1 -timeout=600s
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  SPARC_TEST_PG_BIN=/opt/homebrew/opt/postgresql@17/bin \
  SPARC_TEST_PG_CLIENT_BIN=/tmp/sparc-r02-openssl35.lXTpLX/relocated-pg-client/bin \
  go test -mod=readonly -tags=integration ./internal/tools ./internal/database -count=1 -timeout=600s
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go vet ./...
git diff --check
```

**Step 4: Commit**

```sh
git add README.md docs/coverage.md docs/progress.md
git commit -m "docs: describe local database backup slice"
```
