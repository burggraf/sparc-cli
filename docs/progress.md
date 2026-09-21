# Progress

## Task 01 — Public repository and minimal command contract

**Status:** complete for the scaffold-only command shell (working tree, uncommitted).

**Scope:** `sparc help` and `sparc version` are offline command-shell behavior only. `backup`, `verify`, and `restore` are recognized only with no arguments and return a non-success unavailable status; they perform no backup work. Extra arguments/options return usage status 2 without echoing those values. Help/version return operational status 1 if stdout cannot be written, with a redacted diagnostic. No Supabase support or recovery capability is claimed.

### TDD evidence

1. **Red (initial shell):** After adding `internal/cli/run_test.go` and before `internal/cli/run.go` existed, `go test ./internal/cli -count=1 -v` exited 1 with `undefined: Run`. This was the expected compile failure for the new command-contract test.
2. **Green (initial shell):** After adding `Run`, `go test ./internal/cli -count=1 -v` passed help, version, unknown-command, and unavailable backup/verify/restore cases.
3. **Red (arity):** After adding `TestRunRejectsExtraArguments` and before changing `Run`, `go test ./internal/cli -run TestRunRejectsExtraArguments -count=1 -v` exited 1. The five cases returned success or unavailable status instead of usage status 2, and did not produce the required generic usage error.
4. **Green (arity and side effects):** After adding the arity guard, `go test ./internal/cli -run 'TestRunRejectsExtraArguments|TestRunHelpAndVersionHaveNoObservedSideEffects' -count=1 -v` passed.
5. **Red (stdout delivery):** After adding `TestRunReturnsFailureWhenStdoutWriteFails` and before changing `Run`, `go test ./internal/cli -run TestRunReturnsFailureWhenStdoutWriteFails -count=1 -v` exited 1. No-args help, explicit help, and version returned 0 and produced no diagnostic when the test writer returned its sentinel error.
6. **Green (stdout delivery):** After adding checked stdout writes, `go test ./internal/cli -run TestRunReturnsFailureWhenStdoutWriteFails -count=1 -v` passed. Each case returns 1 and writes only `sparc: unable to write command output` to stderr; the sentinel error text is absent.

### Validation

- `gofmt -d cmd/sparc/main.go internal/cli/run.go internal/cli/run_test.go` — no diff.
- `go test ./... -count=1` — passed.
- `go vet ./...` — passed.
- `go build ./cmd/sparc` — passed; local build output was removed after checks.
- `GOOS=windows GOARCH=amd64 go build -o /tmp/sparc-task01.exe ./cmd/sparc` — passed; temporary output was removed.
- Native command checks — `sparc help` and `sparc version` exited 0; `sparc backup`, `sparc verify`, and `sparc restore` each exited 1 with the explicit scaffold-unavailable message; unknown `sparc archive` exited 2; `sparc help extra` exited 2 with the generic no-arguments usage error.
- Ignore/add dry run — `cmd/sparc/main.go` is not ignored and `git add -n cmd/sparc/main.go` reports `add 'cmd/sparc/main.go'`; `/sparc` and `/sparc.exe` are ignored by root-anchored rules.
- CI actions — checkout is pinned to `11d5960a326750d5838078e36cf38b85af677262` (`v4`) and setup-go to `40f1582b2485089dde7abd97c1529aa768e1baff` (`v5`), with version comments retained.
- Hygiene — `git diff --check` passed, no staged files, no local build outputs, and the production runner contains no test sentinel text.

### Self-review

The production command shell imports only `fmt`, `io`, and `os`; help/version have no filesystem or network calls. It handles stdout write failure with a fixed diagnostic and operational exit 1 without exposing the underlying error. `TestRunHelpAndVersionHaveNoObservedSideEffects` observes that help/version preserve a sentinel-only current directory and would panic on an HTTP request using the standard default transport. It does not prove the absence of filesystem writes outside that directory or non-default/direct network activity; the production source review supplies that narrower assurance. Repository documentation and ignore rules contain no credentials, archives, private configuration, or raw diagnostics.

### Owner decisions

- **License:** MIT, recorded in `LICENSE`.

### Next approved work

Task 02 — dependency-free packaging/provenance spike; do not make backup capability claims before its evidence exists.

## Task 02 — Packaging/provenance research slice (R01/R02/R17)

**Status:** research slice implemented; Task 02 remains incomplete and no distribution format or client payload is approved.

**Scope:** Added only research records, an unmistakably research-only empty
client manifest, package/client notes, and an isolated synthetic macOS arm64
experiment. No production CLI behavior, PostgreSQL/EDB payload, signing
identity, hosted service, download, installation, account, or release artifact
was used.

**Historical command note:** bare `go test` commands in the Task 02 TDD record
below are historical observations run with the already-installed local Go
1.25.6 toolchain. Current Task 02 test and smoke commands use
`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`.

### TDD evidence

1. **Red:** After writing the extraction tests and before implementation,
   `go test ./experiments/packaging -count=1 -v` failed to build because
   `Extract` and `RunVerified` were undefined.
2. **Green:** After the minimum extraction implementation, the current guarded
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./experiments/packaging -count=1 -v` passed the hash-mismatch,
   existing-destination no-clobber, and tamper-before-execution tests; the
   opt-in smoke is skipped in this ordinary run.
3. **Smoke correction:** The first opt-in smoke attempt failed only because
   the generated temporary outer source was outside the module (`go.mod file
   not found`). The test was corrected to create its temporary source under a
   removed dot-directory in the module; a byte-identical signed helper is
   temporarily copied there for `go:embed`, while the source helper and outer
   executable remain in OS test temporary storage. The next attempt exposed a
   test-only CDHash parser limitation (`codesign -dvv` did not print `CDHash`);
   it was minimally changed to `codesign -dvvv`. The final smoke passed.
4. **Red/green (offline Go contract):** New focused tests initially failed to
   build because `offlineGoEnvironment` was undefined. After the minimal helper,
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./experiments/packaging -run 'TestOfflineGoEnvironment|TestRunVerifiedRefusesTamperedHelper' -count=1 -v` passed. Enabling smoke without the contract fails clearly before a child build: `SPARC_PACKAGING_SMOKE=1 requires GOTOOLCHAIN=local; refusing child Go build`.
5. **Mutation sensitivity (execution hash guard):** The tamper test now asserts
   exactly `synthetic helper hash mismatch` without paths or helper contents.
   Temporarily changing the execution guard to `false && hash(...)` made
   `TestRunVerifiedRefusesTamperedHelper` fail because it executed the tampered
   helper; restoring the real comparison made the focused test pass.
6. **Red/green (nil environment):** Before the guard,
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./experiments/packaging -run TestRunVerifiedRejectsNilEnvironment -count=1 -v` failed with the test's redacted wrong-category assertion. After adding the first-line guard, the focused guarded test passed and asserts exactly `sanitized helper environment required` before any helper read or execution. A non-nil empty environment remains explicit and allowed.

### Validation

- `SPARC_PACKAGING_SMOKE=1 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./experiments/packaging -run TestOptInEmbeddedSmoke -count=1 -v` — passed on `Darwin 27.0.0 arm64`, macOS `27.0` build `26A428`, Go `go1.25.6 darwin/arm64`. The smoke requires those three values and passes them with bounded private Go caches to every child `go build`.
- The smoke used `/opt/homebrew/bin/go`, `/usr/bin/codesign`, `/usr/bin/xattr`, and `/usr/bin/file` (`file-5.41`). It ad-hoc-signed a temporary synthetic helper; adjacent and embedded absolute-path executions both returned `synthetic-helper 1` with a sanitized helper environment.
- `GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -c -o /tmp/sparc-packaging-windows.test.exe ./experiments/packaging` — passed; the temporary test binary was removed without execution. This establishes only cross-target test compilation, not Windows runtime or trust behavior.
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./experiments/packaging -count=1 -v` and `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./... -count=1` passed. The focused nil-environment test proves `RunVerified` returns exactly `sanitized helper environment required` before attempting to read the helper.
- `file` reported both files `Mach-O 64-bit executable arm64`; SHA-256 was `eee58c0739bbc3c7f758ba0777eaf3b48d0b55ec2ed9ee57069f5624a8c5ca31` for original and extracted bytes; CDHash was `61761f6338e95d199298f538e6898ec37b2c08e4` for both. `codesign --verify --strict` passed for both. `xattr -l` recorded only `com.apple.provenance: \x01\x02` on each.
- The signed helper is briefly copied with generated outer source into ignored
  `experiments/packaging/.smoke-outer-*/` for `go:embed` compilation; cleanup
  removes it. The source helper, outer binary, and extraction output use OS test
  temporary storage. `docs/research/R01-packaging.md` has the full commands,
  observations, limitations, separate online/first-ever-offline/cached-assessment
  test plan, and blockers. `docs/research/R02-client-provenance.md` records the verified
  PostgreSQL 17.11 source URL and SHA-256
  `dd27f2b3c59e73ed14aa3324901242bf69a032a6347805f274e6260322d42979`;
  EDB is explicitly uninspected only.

### Open blockers

Ad-hoc signing proves byte/signature preservation only—not Developer ID,
notarization, Gatekeeper, or public trust. Denied-write and browser-quarantine
observations are pending. PostgreSQL/EDB file inventories, imports, licenses,
minimum OS, signed digests, native TLS dump/restore, macOS trusted-notary
behavior, and Windows trust behavior need separate owner approval. Adjacent
tools remain control/fallback only and require owner approval before selection.

## Task 03 — Support profile and coverage report contract

**Status:** complete for the offline contract and research/profile documentation
(working tree, uncommitted). This is a qualification target only; it makes no hosted,
payload, or historical recovery-success claim.

**Scope:** Added a JSON-ready operation report with distinct observation, read permission,
capture, byte integrity, recovery prerequisite, restore, behavior-verification, and
structured/manual requirement-ID fields. Validation rejects unknown states and duplicate or
empty component IDs, requires reasons for `not_used`/excluded components, and derives a non-success summary from
all component gates. No backup/restore engine or manual-recovery structure was added.

### TDD evidence

1. **Red:** After writing `internal/operation/report_test.go` and before
   `internal/operation/report.go` existed, `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run TestReport -count=1 -v` exited 1 with a package build failure (the report contract types and `Report` methods were undefined).
2. **Green:** After the minimal report contract, `gofmt -w internal/operation/report.go internal/operation/report_test.go && GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run TestReport -count=1 -v` passed 20 tests. The table-driven cases prevent a complete/success summary for `unknown`, `permission_denied`, `unsupported`, `partially_captured`, `manual_required`, `missing_critical_material`, `failed`, and `inconclusive`; a separate test prevents permission denial being relabeled `not_used`.

### Profile and coverage decisions

- Initial database qualification target: PostgreSQL 17 source, target, and client matching
  major only; direct or qualified session-pooler routes only; transaction pooler blocked.
  The PG17 client payload is not selected (R02), and R03/R04/R05 remain open.
- Ordinary provider-owned `public` is a required hosted-support gate. Real Auth, standard
  Storage ownership, dependency-complete Function packages, and Vault when detected are
  product gates. Vector/Iceberg/external specialty features are unsupported only with
  reliable detection and explicit incomplete/refusal behavior.
- `docs/coverage.md` maps every product inventory family to required, manual, unsupported,
  or detect-and-block handling and names its Rxx blocker. `docs/compatibility.md` and
  `docs/research/R04-database-route.md` distinguish qualification targets from support.

### Validation

- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./... -count=1` — passed (3 packages).
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go vet ./...` — passed.
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go build -o /tmp/sparc-task03 ./cmd/sparc` — passed; temporary binary removed.
- `GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -c -o /tmp/sparc-operation-windows.test.exe ./internal/operation` — passed; temporary binary removed.
- `gofmt -d internal/operation/report.go internal/operation/report_test.go` and `git diff --check` — no output. `git status --porcelain=v1` showed only the six Task 03 paths; `git diff --cached --name-status` was empty. A scan found no key, `.env`, archive, or certificate artifacts.

No hosted services, downloads, installations, commits, or payloads were used.

### Next approved work

Finish Task 02 gates or proceed only according to the approved plan; R03–R15 remain
research blockers for service behavior.

### Task 03 follow-up — independent coverage/restore and UTF-8 report IDs

**Status:** complete for the offline report contract correction (working tree,
uncommitted). No backup/restore operation engine was added.

#### TDD evidence

1. **Red:** After adding the independent coverage/restore, unsupported absence, UTF-8, and
   JSON identity tests but before changing `report.go`,
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run 'TestReportSummarySeparatesCoverageAndRestore|TestReportUnsupportedAndDetectComponentsRemainExplicit|TestReportValidateRejectsInvalidUTF8IDsAndReason|TestReportJSONRoundTripPreservesIDs' -count=1 -v`
   failed: 3 tests passed and 15 failed. The prior summary coupled coverage to restore and
   behavior, accepted invalid UTF-8, and blocked reliably absent unsupported components.
2. **Green:** After the minimal summary/validation correction, the same guarded focused
   command passed 17 tests. The final guarded
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run TestReport -count=1 -v`
   passed 38 tests.

#### Contract correction

Coverage now derives only from observation, read permission, capture, byte integrity, and
recovery prerequisites. Restore derives separately: untouched/absent components yield
`not_run`; complete capture plus later restore failure remains coverage-complete and restore
`failed`; mixed work is `blocked`; sampled/inconclusive behavior is `inconclusive`.
Reliably absent unsupported/detect-and-block components use the complete `not_used` tuple
with a reason and do not block coverage. Observed, unknown, denied, or otherwise non-absent
cases remain explicit incomplete/blocked. Explicit exclusions require a reason and remain
conservatively coverage-incomplete/restore-blocked. Component IDs, requirement IDs, and
non-secret reasons reject invalid UTF-8 before duplicate identity checks; JSON round-trip
coverage proves valid IDs retain identity.

#### Follow-up validation

- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./... -count=1` — passed (3 packages).
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go vet ./...` — passed.
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go build -o /tmp/sparc-task03-followup ./cmd/sparc` — passed; temporary binary removed.
- `GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -c -o /tmp/sparc-operation-followup-windows.test.exe ./internal/operation` — passed; temporary binary removed.
- `gofmt -d internal/operation/report.go internal/operation/report_test.go` and `git diff --check` — no output. `git diff --cached --name-status` was empty; status contained only the six Task 03 paths. A secret/artifact scan found no `.env`, key, certificate, or archive files.

No hosted services, downloads, installations, commits, or payloads were used.

### Task 03 follow-up — pre-restore capture/integrity outcome

**Status:** complete for the offline summary correction (working tree, uncommitted).

#### TDD evidence

1. **Red:** After adding three no-restore cases (capture failed, integrity failed, and
   integrity inconclusive) to `TestReportSummarySeparatesCoverageAndRestore` and before
   changing summary derivation,
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run TestReportSummarySeparatesCoverageAndRestore -count=1 -v`
   failed: 6 tests passed and 7 failed. Capture/integrity states were incorrectly treated
   as restore failures or restore inconclusive even though restore was `not_attempted`.
2. **Green:** After limiting restore failure to `RestoreFailed`/behavior failure and restore
   inconclusive to sampled/inconclusive behavior, the guarded focused command passed 12
   tests; the subsequent guarded
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run TestReport -count=1 -v`
   passed 41 tests.

Capture and byte integrity now affect only coverage. When coverage is incomplete before a
restore starts, restore is `blocked`; actual restore/behavior evidence alone yields
`failed` or `inconclusive`.

#### Follow-up validation

- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./... -count=1` — passed (3 packages).
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go vet ./...` — passed.
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go build -o /tmp/sparc-task03-restore-outcome ./cmd/sparc` — passed; temporary binary removed.
- `GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -c -o /tmp/sparc-operation-restore-outcome-windows.test.exe ./internal/operation` — passed; temporary binary removed.
- `gofmt -d internal/operation/report.go internal/operation/report_test.go` and `git diff --check` — no output. `git diff --cached --name-status` was empty; status contained only the six Task 03 paths. A secret/artifact scan found no `.env`, key, certificate, or archive files.

No hosted services, downloads, installations, commits, or payloads were used.

### Task 03 follow-up — restore precedence, coherence, and reason codes

**Status:** complete for the offline report-contract correction (working tree,
uncommitted). No operation engine or dependency was added.

#### TDD evidence

1. **Red:** After adding restore-precedence, coherence, typed-reason, and exact JSON-shape
   tests but before implementation,
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run 'TestReportRestorePrecedence|TestReportValidateCoherence|TestReportValidateReasonCodes|TestReportJSONV1Shape' -count=1 -v`
   exited 1 with a package build failure because the new `NotUsedReason` type/codes did not
   exist. The added tests also specify the prior precedence defect: sampled behavior must
   not outrank incomplete coverage or untouched components.
2. **Green:** After the minimal contract changes, the same guarded focused command passed
   27 tests. `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run TestReport -count=1 -v`
   passed 64 tests.

Restore failure/behavior failure now has highest precedence; incomplete coverage is blocked
before sampled/inconclusive behavior is considered; all applicable components must be
restored before behavior determines inconclusive/success. Direct validation rejects the
specified observation/integrity/restore/behavior contradictions. `not_used_reason` is now
the allowlisted `NotUsedReason` JSON code (`feature_not_observed` or
`outside_declared_scope`), never free-form public text. The v1 JSON test semantically
compares complete decoded shapes and verifies empty optional requirement/reason fields are
omitted; Unicode ID round-trip remains separately tested.

#### Follow-up validation

- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./... -count=1` — passed (3 packages).
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go vet ./...` — passed.
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go build -o /tmp/sparc-task03-quality ./cmd/sparc` — passed; temporary binary removed.
- `GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -c -o /tmp/sparc-operation-quality-windows.test.exe ./internal/operation` — passed; temporary binary removed.
- `gofmt -d internal/operation/report.go internal/operation/report_test.go` and `git diff --check` — no output. `git diff --cached --name-status` was empty; status contained only the six Task 03 paths. A secret/artifact scan found no `.env`, key, certificate, or archive files.

No hosted services, downloads, installations, commits, or payloads were used.

### Task 03 follow-up — order-independent behavior and bound reason codes

**Status:** complete for the offline report-contract correction (working tree,
uncommitted). No operation engine or dependency was added.

#### TDD evidence

1. **Red:** After adding order-independent behavior reduction, reason-code binding, and
   blocking-`not_used` guard tests but before implementation,
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run 'TestReportRestoreBehaviorReductionIsOrderIndependent|TestReportValidateReasonCodes|TestReportValidateRejectsBlockingStateLabeledNotUsed' -count=1 -v`
   failed: 8 tests passed and 8 failed. Sampled behavior could mask unchecked behavior by
   component order, and valid-looking but extraneous/misbound reason codes were accepted.
2. **Green:** After the minimal reduction and reason binding changes, the same guarded
   focused command passed 15 tests. The guarded
   `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/operation -run TestReport -count=1 -v`
   passed 69 tests.

Behavior reduction now scans all applicable restored components: non-passed/non-sampled
behavior blocks; sampled/inconclusive behavior is reported only after that full scan.
`feature_not_observed` requires the reliable-absence tuple;
`outside_declared_scope` requires excluded support; extraneous reason codes are rejected.
The blocking-`not_used` regression now reaches and asserts its intended guard with an
otherwise coherent excluded/permission-denied tuple and a valid reason code.

#### Follow-up validation

- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./... -count=1` — passed (3 packages).
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go vet ./...` — passed.
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go build -o /tmp/sparc-task03-rereview ./cmd/sparc` — passed; temporary binary removed.
- `GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -c -o /tmp/sparc-operation-rereview-windows.test.exe ./internal/operation` — passed; temporary binary removed.
- `gofmt -d internal/operation/report.go internal/operation/report_test.go` and `git diff --check` — no output. `git diff --cached --name-status` was empty; status contained only the six Task 03 paths. A secret/artifact scan found no `.env`, key, certificate, or archive files.

No hosted services, downloads, installations, commits, or payloads were used.

## Task 04, storage slice — implementation candidate (Tasks 1–3)

Pinned cached `golang.org/x/term v0.39.0` and `golang.org/x/sys v0.40.0`
without network resolution. CLI unknown-command diagnostics are fixed and never
reflect argument values. Added `internal/platform` native location lookup,
exclusive directory/file creation, bounded private-file reads and fixed sentinel
errors. No config parser, credential-input implementation or operational command
has been enabled in this slice.

macOS enforces POSIX UID/type/link/exact-mode checks and rejects unsafe ancestors,
except named root-owned sticky `/private/tmp` and `/private/var/tmp` anchors.
Windows attaches the protected user+SYSTEM DACL at creation, validates handles,
and inspects untrusted ancestor DACLs. Only user/SYSTEM/Administrators may have
non-read ancestor rights; unsupported ACE forms fail closed. The local volume
root is the documented trusted ACL anchor. Details are in `docs/credentials.md`.

### Observed RED → GREEN

Commands use `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off` throughout.

- RED: `go test ./internal/cli -run 'TestRun(CommandContract|UnknownCommandDoesNotDiscloseInput)' -count=1`
  failed against reflected unknown-command output after the fixed-output and
  canary/control tests were written.
- GREEN: `go test ./internal/cli -run TestRun -count=1` passed after the single
  diagnostic replacement.
- RED: `go test ./internal/platform -count=1` failed to build after writing the
  common/macOS tests and before adding the platform API/implementation.
- GREEN: `go test ./internal/platform -count=1 -v` passed after implementation.
  Windows-specific tests were authored before Windows implementation, but their
  runtime RED/GREEN has **not** been observed on this macOS host.

### Fresh verification

- `go get golang.org/x/term@v0.39.0 golang.org/x/sys@v0.40.0` — resolved offline
  from cache; `go mod verify` passed.
- `go test ./internal/cli ./internal/platform -count=1 -v` — passed, including
  exclusive/concurrent creation, no-clobber, modes, unsafe ancestors, symlinks,
  hard links, FIFO refusal, bounds, locations and redacted errors.
- `go test ./... -count=1` and `go vet ./...` — passed.
- `go test -race ./internal/cli ./internal/platform -count=1` — passed.
- Native `go build ./cmd/sparc` — passed, binary placed in a private temporary
  `/tmp/sparc-task04-builds.*` directory and removed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c` separately for
  `./internal/cli` and `./internal/platform` — passed, outputs removed.
- Windows AMD64 `go vet ./internal/cli ./internal/platform` and CLI build — passed.
- `gofmt` and `git diff --check` — clean. No files staged; no commit/push.

### Explicit qualification gates

Supervisor-approved scope: macOS extended/inherited ACL handling is not implemented
or proven; POSIX modes alone must not be presented as effective ACL privacy. This
is a security/release blocker before using real credentials. Windows native DACL,
reparse and second-user behavior likewise remain unexecuted runtime gates;
cross-compilation is not proof. The native reparse test deliberately fails, rather
than skips, if its Windows runner lacks symlink privilege. This slice and Task 04
remain incompletely qualified, pending these gates and independent review.

## Task 04, config and credential slice — implementation candidate (Tasks 4–5)

Added `internal/credentials` explicit environment/private-file references and
bounded hidden-terminal input, plus `internal/config` strict private-file v1
profile loading. No operational CLI path, save/migration API, credential-store
persistence, dependency update, or plaintext fallback was added. The pinned
`x/term` already implements native mode handling, so this slice reuses it directly
instead of adding redundant Darwin/Windows wrappers.

Supervisor-approved details: names/project refs are bounded to 256 UTF-8 bytes,
destinations/file refs to 4096, environment names to 128 ASCII grammar bytes, and
profiles to 0–32 entries. Present metadata is nonblank; secret spaces remain
unchanged. Hierarchical URI-looking destinations reject userinfo/parse errors,
while local `%`/`@`/Windows-drive paths remain inert metadata. Config key-presence
checks reject two reference keys even if one value is empty. Hidden input drains
an overlong/ASCII-invalid line through Enter, ignoring later cancellation/editing
bytes, before restoring mode and returning input failure; ordinary Ctrl-C/D on
an otherwise valid line returns the fixed cancellation sentinel.

### Observed RED → GREEN

All Go commands use `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off`.

- RED: `go test ./internal/credentials -count=1` failed to build after the
  input/reference/terminal/error tests were written, before implementation.
- GREEN: the focused credential suite passed after implementation and correcting
  a test fixture that accidentally described an allowed single CRLF terminator.
- RED: `go test ./internal/config -count=1` failed to build after strict schema,
  duplicate/type/Unicode/bound/redaction tests were written, before implementation.
- GREEN: the focused config suite passed after the schema-specific parser was added.
- RED: added U+2028/U+2029 newline/paragraph regressions failed against both initial
  validators. GREEN: both passed after rejecting those separators explicitly.
- RED: overlong/invalid-line cancellation regressions failed against immediate
  Ctrl-C/D handling. GREEN: they passed after making drain state ignore those
  bytes through Enter; fixed-output and restoration checks also passed.

### Verification and remaining gates

- Focused new-package tests, full offline suite, new-package race tests, and
  `go vet ./...` passed.
- Native CLI build, separate Windows AMD64 test compilation for each new package,
  Windows vet for both packages, and Windows CLI build passed. Temporary build
  outputs were removed.
- `gofmt` and `git diff --check` passed; no staged files, commits, or pushes.
- Canary checks exercise fixed/non-wrapping failures through nested formatting,
  logging, and JSON error envelopes. Pipe/non-TTY tests prove refusal without
  consuming input; explicit-source failures do not prompt or fall back.

Terminal parser and injected raw-mode tests are not native console qualification.
Native macOS/Windows echo, Unicode, console cancellation, and abnormal termination
remain open gates. macOS extended ACL protection and Windows native DACL/reparse/
second-user checks remain the previously recorded security blockers, including
for config and credential-file reads. No real credentials, network requests,
downloads, installations, or hosted services were used. Independent review remains
required before this slice is accepted or merged.

### Final P2 follow-up — restore terminal before final output

`readHidden` now restores terminal state immediately after its read attempt,
before emitting the final newline. A deferred fallback handles early exits and
unsuccessful immediate restoration; successful immediate restoration is not
repeated. Any immediate restoration failure discards the input, suppresses the
newline, and remains `ErrInput` even if the fallback subsequently succeeds.

RED: event-order regressions and the updated restoration-failure output assertion
failed against the prior newline-before-restore implementation. GREEN: focused
hidden-input tests passed after the correction. The event checks observe file
position to prove prompt-before-read and completed-read-before-restore, followed
by restore-before-newline. They also cover prompt/read/newline failures,
cancellation, successful fallback, failed fallback, and no redundant restore.
These remain injected-mode tests, not native console qualification.

Fresh guarded verification (`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off`):
focused hidden-input tests, full suite, credentials/config race tests, full vet,
and separate Windows AMD64 credentials/config test compilation and vet passed.
Temporary compilation outputs were removed. Formatting and diff checks passed;
no files staged, no network access, and no commits or pushes. Existing native
console and filesystem-security qualification gates remain open.

## Task 05, payload inventory slice — implementation candidate (Task 1)

Added `internal/tools` typed payload metadata for `pg_dump`, `pg_restore`, and
`psql`. The compiled production inventory is intentionally empty; lookup returns
the fixed `tool payload unavailable` sentinel and never consults PATH,
environment variables, adjacent files, or `build/clients/manifest.json`.
Synthetic manifests are test-only and do not enable extraction or execution.

The v1 validator accepts only PostgreSQL 17 payloads for Darwin arm64/amd64 and
Windows amd64. It enforces the 1,024-file, 240-byte portable-path, 256 MiB/file,
512 MiB compressed, and 1 GiB expanded ceilings with overflow-safe addition.
It rejects unsupported schema/tool/target/purpose, zero/oversized lengths,
zero digests, wrong modes, duplicate/case/prefix collisions, nonportable names,
and incomplete/non-executable/shared tool mappings. Canonical package identity
binds schema, PostgreSQL major, target, compressed artifact, sorted file
inventory, and deterministic executable mappings.

### Observed RED → GREEN

All Go commands used `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off
GOENV=off GOTELEMETRY=off`.

- RED: `go test -mod=readonly ./internal/tools -run TestPayload -count=1`
  failed to build after `payload_test.go` was created because the payload types,
  constants, sentinels, validation, lookup, and identity functions did not exist.
- GREEN: the focused suite passed 60 cases after the minimal implementation. One
  test-only prefix-collision fixture was corrected from a sibling path to an
  actual case-folded child path; no production behavior was weakened.

Fresh guarded verification passed: `go mod verify`; focused 60-case payload
suite; full repository suite; `go test -race ./internal/tools`; full `go vet`;
format/diff/staging checks; and separate compile-only Windows AMD64 and Darwin
AMD64 `internal/tools` test binaries in a removed private `/tmp` directory.

No payload bytes, extraction, execution, platform changes, dependencies,
operational commands, downloads, installations, hosted services, commits, or
pushes were added. Real client acquisition, provenance, licensing, signatures,
runtime closure, TLS behavior, and native execution remain open gates.

### Task 05 payload follow-up — selected-inventory validation and public package ID

Review found that compiled manifests could be returned without full validation,
and identity regressions exercised the unchecked hash helper rather than the
validated `packageID` boundary. A new unexported selector accepts synthetic
inventories for tests; production lookup delegates to it while its compiled
inventory remains empty. Any matching manifest is fully validated before return,
and an invalid selected entry returns the exact non-wrapping `ErrInvalidPayload`.

RED: focused selection/package-ID tests failed to build before `selectPayload`
existed. GREEN: 20 focused follow-up cases passed after routing production lookup
through the validating selector. Valid target, purpose, runtime path, length,
digest, compressed artifact, and unique executable-mapping changes now prove
`packageID` changes; reordered file inventory and map insertion order preserve the
ID. Unsupported schema, major, and mode return an empty ID plus the exact invalid
sentinel.

Fresh guarded verification passed: `go mod verify`; the complete 75-case focused
payload suite; full repository suite; `go test -race ./internal/tools`; full
`go vet`; gofmt/diff/staging checks; and separate compile-only Windows AMD64 and
Darwin AMD64 test binaries in a removed private `/tmp` directory. No production
payload, fallback path, dependency, network access, commit, or push was added.

### Task 05 payload follow-up — unique selection and closed executable inventory

Quality review found that lookup returned the first matching manifest, an extra
unmapped executable-purpose file validated, and the canonical encoding lacked a
fixed-vector regression. Selection now scans every matching major/target entry,
validates each, and succeeds only for exactly one valid match. Duplicate valid
entries and mixed valid/invalid duplicates fail with a zero manifest and the
exact non-wrapping `ErrInvalidPayload` regardless of order. Every executable file
must now be mapped exactly once by the three approved tool keys.

RED: focused tests reported seven failures: the placeholder golden package ID,
the accepted unmapped helper executable, and duplicate inventories returning the
first valid match. GREEN: the focused payload suite passed after the two minimal
validation changes and pinning the synthetic package ID to
`3e76db2acc8ac60d86de52653e491624f38a2a114eb198769a18c7688d0c764e`.
Direct identity checks cover rejected schema version, PostgreSQL major, target OS,
and file mode changes while confirming validated `packageID` rejection.

## Task 05, private payload storage slice — implementation candidate (Task 2)

Extended `internal/platform` with exclusive streaming private-file creation,
validated payload reopening, descriptor-based executable sealing, and atomic
no-replace sibling-directory publication. Existing credential/data APIs retain
their exact `0600` semantics and fixed non-wrapping error contract.

On macOS, payload files start at `0600`; executable sealing validates the open
owner/type/single-link descriptor, changes that descriptor to exact `0700`,
syncs it, and validates again. Data and executable opens require exact `0600`
and `0700` respectively, so the legacy reader refuses executable payloads.
Publication opens the already-qualified private parent no-follow as a directory,
validates that descriptor, requires `fstatfs` to report `MNT_LOCAL`, treats close
failure as fatal, and only then uses `renamex_np(RENAME_EXCL)` with no copy or
replacement fallback.

On Windows, file creation retains the existing protected current-user+SYSTEM
DACL from the first handle. Payload opening and sealing validate disk type,
single link, reparse absence, owner, and exact DACL; executable intent remains
manifest metadata rather than a chmod claim. Publication uses no-replace
`MoveFileW` between validated siblings. Native Windows behavior remains a runtime
qualification gate; cross-compilation is not DACL/reparse/rename proof.

### Observed RED → GREEN

All Go commands used `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off
GOENV=off GOTELEMETRY=off`.

- RED: focused private-payload/publication tests failed to compile after the new
  API, streaming, mode/DACL, collision, concurrency, injected-failure, hard-link,
  symlink/reparse, and unsafe-boundary cases were written.
- GREEN: the focused platform suite passed after adding the minimal common and
  native implementations. Additional native fixtures independently inspect
  Darwin modes and Windows security descriptors instead of trusting validators.

Fresh guarded verification passed: `go mod verify`; 21 top-level platform tests
executed natively on macOS arm64 (including the common and Darwin-specific
coverage); platform race tests; the full repository suite; full vet;
formatting/diff checks; and compile-only Windows AMD64 plus Darwin AMD64 platform
test binaries and vet. Windows-specific DACL/reparse/no-replace tests were not
executed here. Temporary binaries were isolated under `/tmp` and removed.
Legacy `WritePrivateFile` tests retain narrow write/close failure injection; this
slice injects executable sync and publication failures. Every deterministic fault
is passed to an unexported per-call `...With` helper; production entry points call
direct native/file operations and there are no mutable package-global function
hooks. The fault tests run safely in parallel under the race detector. Streaming
payload write/close failure ownership is deferred to Task 3 extraction tests.
Concurrent publishers produce exactly one winner; collisions leave both the
losing staging directory and even an invalid existing winner untouched.

A follow-up RED test failed to compile before the Darwin filesystem-qualification
seams existed. GREEN proves a synthetic non-local `fstatfs` result and a parent
handle close failure both return exact `ErrPrivateStorage`, leave staging and
absent destination unchanged, and therefore do not reach rename. A second RED
failed after tests switched from shared hooks to missing per-call helpers; GREEN
passed after the production paths were made direct and only the narrow helpers
accepted injected write/close/sync/statfs/fd-close/publication functions.

No extraction, process execution, payload bytes, dependency, operational command,
network/hosted access, installation, commit, or push was added. macOS extended
ACL qualification and native Windows DACL/reparse/no-replace execution remain
open gates inherited from Task 04/this slice.
