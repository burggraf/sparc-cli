# Real Client Extraction Smoke Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Prove the existing trusted payload extractor can privately extract and run the locally built, relocated PostgreSQL 17.11 client closure on the current macOS arm64 host.

**Architecture:** Keep the production inventory empty. Add one opt-in `integration` test in `internal/tools` that builds a temporary manifest from an explicitly supplied private client directory, archives only the three required executables and their adjacent runtime libraries, calls the existing extractor, verifies the extracted files, and executes only `--version` through the existing `runWith` bounded-runner path. The test does not connect to a hosted project, dump data, or make a payload selectable in the product.

**Tech Stack:** Go standard library, existing `internal/tools` extraction/run code, local PostgreSQL 17.11 candidate, macOS native tools.

---

### Task 1: Complete the private candidate closure

**Files:**
- No repository files.
- Private temporary candidate only: `pg17-shared/candidate` under the verified local build root.

**Step 1: Build missing `psql` from the already verified PostgreSQL 17.11 source.**

Run its existing source-tree target and install it into the private candidate.

**Step 2: Rewrite only its build-prefix library load commands.**

Use the same `@rpath`/`@loader_path/../lib` layout already proven for `pg_dump` and `pg_restore`.

**Step 3: Relocate the complete private candidate.**

Hide the original build paths, then run `pg_dump --version`, `pg_restore --version`, and `psql --version` with a bounded environment. Confirm dyld loads only the copied adjacent non-system libraries.

### Task 2: Add an opt-in real-client extraction test

**Files:**
- Create: `internal/tools/candidate_integration_test.go`

**Step 1: Write the failing test.**

Create an `integration`-tagged test requiring an explicit absolute `SPARC_TEST_PG_CLIENT_BIN`. It should construct a manifest for `bin/pg_dump`, `bin/pg_restore`, `bin/psql`, and the adjacent `lib/libpq.5.dylib`, `lib/libssl.3.dylib`, `lib/libcrypto.3.dylib`; package them in a deterministic gzip+USTAR stream; then call `preparePayload`, validate the extracted package, and call `runWith` with only `--version`.

**Step 2: Run the focused test and confirm RED.**

Run:
```sh
SPARC_TEST_PG_CLIENT_BIN=<private-relocated-client>/bin \
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
go test -mod=readonly -tags=integration ./internal/tools \\
  -run '^TestOptInCandidatePayloadExtraction$' -count=1 -v
```
Expected: compile failure because the helper used to construct the bounded archive/manifest does not exist.

**Step 3: Implement the smallest test-only helpers.**

Use standard-library tar/gzip, fixed timestamps/ownership/modes, SHA-256, and the existing manifest types. Refuse symlinked/non-regular input and reject any client layout other than the expected adjacent `bin`/`lib` tree. Keep files inside `t.TempDir`; do not write to the repository or modify `productionPayloads`.

**Step 4: Re-run the focused test and confirm GREEN.**

Require all three extracted `--version` commands to identify PostgreSQL 17.11 and run with a bounded environment lacking `PATH`, `DYLD_*`, and ambient PostgreSQL credentials.

### Task 3: Verify and record limits

**Files:**
- Modify: `docs/research/R02-client-provenance.md`
- Modify: `docs/progress.md`

**Step 1: Run fresh verification.**

Run default tests, `internal/tools` integration test, full database integration/race suites with the private candidate, and default/integration `go vet` using `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`.

**Step 2: Record only demonstrated evidence.**

State that the private macOS 27 experiment proved extractor mechanics for the local candidate, not signing, release payload approval, clean-machine support, cross-version compatibility, hosted use, or redistribution.

**Step 3: Commit and push only documentation and test code.**

Do not commit a client binary, archive, key, credential, or test dump.
