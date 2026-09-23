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

## Task 05, bounded extraction/cache slice — implementation candidate (Task 3)

Added synthetic-only `internal/tools` extraction and cache validation. A seekable
source is fully length/digest verified before an exclusive crypto-random staging
directory exists, then re-hashed during extraction. Only one narrow gzip/USTAR
regular-file stream is accepted. Files are streamed through fixed 32 KiB buffers,
checked against the compiled manifest, synced/closed, executable headers are
matched to the declared Mach-O/PE target, and an exact receipt is written last.
No staging path is launched. Atomic no-replace publication is followed by a full
receipt, recursive inventory, private mode/DACL, byte/hash, and executable-header
validation on every return and cache reuse.

### Observed RED → GREEN

- RED: `go test -mod=readonly ./internal/tools -run TestExtractSyntheticPayload
  -count=1` failed to compile before extraction/cache types and functions existed.
- GREEN: the first synthetic executable round trip passed after the bounded
  implementation. Focused follow-up cases then drove strict USTAR epoch handling,
  second-pass compressed hashing, exact source seek offsets, extra-directory
  rejection, and close-sensitive full validation.

The focused suite covers missing/extra/duplicate/tampered/truncated entries;
malformed/checksum-failed/multistream/trailing gzip and tar data; PAX/GNU/link/
device metadata; wrong executable header/CPU; manifest reorder stability;
source read/seek/mutation and cancellation; injected write/short-write/sync/close/
random/publication failures through per-call operations; forged receipts, extra
files/directories, invalid winners, abandoned staging, eight concurrent goroutine
contenders, and three independently synchronized helper processes. Failures are
the fixed non-wrapping `tool extraction failed` sentinel and never repair/delete
a published invalid winner or another process's staging directory.

Fresh guarded verification passed: offline module verification; focused extraction
and cache tests; full repository tests; `internal/tools` race tests (including
helper processes); full vet; formatting/diff checks; and compile-only Windows
AMD64 plus Darwin AMD64 tools test binaries. Temporary outputs were removed.
No real client payload, execution path, CLI command, PostgreSQL tool, network,
download, installation, hosted service, commit, or push was used. Native Windows
runtime and existing macOS extended-ACL qualification gates remain open.

### Task 05 extraction follow-up — strict source, executable, tar, crash and metadata gates

Specification review found that second-pass compressed validation stopped at the
manifest length without probing the retained source, executable parsing accepted
non-executable Mach-O/PE containers, regular USTAR device fields were unchecked,
tar termination relied on `archive/tar` EOF semantics, crash boundaries lacked
kill-point proof, and native metadata-only tamper fixtures were incomplete.

RED-first focused fixtures now cover an archive that changes after verification
to `archive || extra`, Mach-O object/dylib/wrong-CPU mutations, regular USTAR
nonzero device metadata, zero/one/two/three zero-block terminators, deterministic
helper kills immediately before and after atomic publication, and Darwin mode-only
executable/receipt/directory tamper. A Windows-only test mutates the protected
payload DACL for future native qualification and compiles here without being
claimed as runtime evidence. Windows parser fixtures mutate PE machine,
executable/DLL characteristics, and PE32 versus PE32+ optional-header identity.

The second pass now retains the context-aware underlying reader, verifies exact
limited gzip/hash/tar termination, then requires a direct one-byte probe to return
`0, io.EOF`. Mach-O requires `TypeExec` plus the declared CPU. Windows requires
AMD64, `OptionalHeader64`, `IMAGE_FILE_EXECUTABLE_IMAGE`, and no `IMAGE_FILE_DLL`.
Regular USTAR headers require zero device numbers. Overflow-safe structural
accounting requires exactly one 512-byte header and padded body per manifest file,
followed by exactly two zero 512-byte terminator blocks; any shorter or longer
decompressed stream fails.

Kill helpers use stdin/stdout pipe handshakes with 30-second command contexts and
per-test cleanup that kills and waits any started child. No sleeps, locks, mutable
production hooks, or repair paths were added. Before-publication kill leaves no
destination and one complete private abandoned staging directory; later prepare
creates a separate valid winner without touching it. After-publication kill leaves
a fully valid package that reuses without reading source bytes. The focused kill
test passed under an explicit 60-second Go timeout, and process inspection found
no surviving helper/test process. The earlier >4-minute combined validation had
completed; it was broad full/race/cross-compile work rather than a stuck helper.

## Task 05, native process ownership slice — implementation candidate (Task 4)

Added a fixed-error `internal/platform` process lifecycle with direct absolute-path
launch, explicit argument/environment/cwd/stdio inputs, asynchronous direct-child
reaping, repeatable `Wait`, bounded `Terminate`, and idempotent bounded `Close`.
Invalid paths, controls, nonportable environment keys, case-colliding environment
entries, nil stdio, and native failures return only the non-wrapping
`process unavailable` sentinel.

### Observed RED → GREEN

- RED: `go test -mod=readonly ./internal/platform -run '^TestProcess' -count=1`
  failed to compile before `ProcessSpec`, `Process`, `ErrProcess`, and the lifecycle
  methods existed.
- GREEN: focused native macOS tests pass after direct `exec.Cmd` launch with
  `Setpgid`, negative-group `SIGKILL`, direct-child wait/reap, and bounded common
  lifecycle coordination. Follow-up RED fixtures drove cancellation/timeout,
  exact environment isolation, concurrent/repeated lifecycle calls, descriptor
  accounting, and injected wait/terminate/close failure mapping.

The native Darwin suite launches the test executable directly, preserves exact
Unicode/space/quote/backslash arguments, proves the supplied environment and cwd
without ambient inheritance, covers zero/nonzero/failed starts, and uses pipe
handshakes for endless child+grandchild ownership. Group termination while the
direct child remains unreaped yields pipe EOF, direct children are no longer
waitable after `Wait`, and sixteen launch/wait/close cycles preserve `/dev/fd`
count. No shell, sleep, PATH lookup, mutable global hook, or unbounded process-
output buffering is used.

Windows code creates a kill-on-close unnamed Job Object first, duplicates only the
three intended standard handles, creates the explicit application suspended with
an extended handle allowlist and explicit Unicode environment/cwd, assigns it to
the job, and only then resumes it. Assignment, resume-count, resume, or thread
close failures terminate with bounded wait and close every owned handle. Pure
per-call lifecycle seams compile tests for existing-job assignment, resume, and
close failure cleanup ordering; they are not native runtime evidence.

Fresh guarded verification passed: offline module verification; 59 native macOS
platform test/subtest results; ten repeated focused process runs; focused and full
platform race tests; the full repository suite; full vet; formatting/diff checks;
and compile/vet-only Windows AMD64 plus compile/vet-only Darwin AMD64 platform
artifacts in a removed private temporary directory. Native Windows Job Object,
handle-inheritance, argv, environment, and descendant behavior remain qualification
gates. Deliberately detached Darwin descendants and abnormal parent death remain
outside the process-group mechanism. No runner, passfile, CLI command, real client,
network, download, install, hosted access, commit, or push was added.

A specification-review follow-up made the Windows fault fixtures compare the
complete assignment/resume/termination/wait/close sequence instead of only a
cleanup suffix. The assignment-failure case separately asserts that resume is
never reached, while resume error/count failures remain ordering-sensitive. All
direct test calls to `Process.Wait`, including Darwin reap loops and concurrent
waiters, now use one local timeout-bounded helper; wait-group completion has the
same bounded test contract.

A quality follow-up found that publishing reap state in common code still left a
gap after native wait returned. Darwin now launches with `os.StartProcess`, polls
kqueue `NOTE_EXIT`, and uses Sysctl zombie state as the registration-race fallback.
One native mutex encloses the group-signal and reap decision, while a stopped ticker
bounds polling CPU and is always released. After exit is observed, Darwin sends a
negative-group `SIGKILL` while the direct leader remains unreaped, then performs the
blocking `wait4`, marks the child reaped, releases the process handle, and closes the
kqueue descriptor. A gated regression pauses after `wait4` has reaped but before
state publication and starts termination concurrently; no later signal is sent after
`ECHILD`. A deterministic helper proves a parent may exit after its grandchild is
ready and the owned group still closes the retained pipe; injected residual-group
kill failure fails closed. Live termination remains covered and late termination
cannot signal a reaped numeric PGID. Windows continues to terminate its stable Job Object after direct-child wait
completion. Its suspended-start failure path now retries direct termination and,
after successful assignment, Job termination plus a paced wait until direct-child
exit is confirmed before any child or Job handle closes. Native Windows runtime
proof, deliberately detached Darwin descendants, and abnormal-parent-death handling
remain qualification gates.

## Task 05, bounded streaming execution slice — implementation candidate (Task 5)

Added synthetic-only `internal/tools` typed version execution. Public callers supply
only `Tool`, positive operation/cleanup timeouts, byte limits, and a cancellation-
aware stdout sink; arbitrary executable paths, args, cwd, environment, and stdin
are not accepted. The public production route selects a compiled manifest for the
native target and full-revalidates its private cached package and mapped executable
before launch. Production inventory is still empty, so this cannot run a real
client or claim PostgreSQL support.

Only `--version` is currently admitted. Connected invocation is deliberately
rejected until Task 6 can supply its scoped passfile and structured connection
boundary. Each run receives a crypto-random private operation directory with
private home/temp/config subdirectories, an explicit fixed locale, no inherited
PATH/PG/loader/proxy/credential/HOME values, a direct executable path, explicit
private cwd, and EOF stdin. No ambient `SystemRoot` is forwarded on Windows; the
direct native launch uses no shell or PATH lookup.

Stdout and discarded stderr each stream through one fixed 32 KiB buffer with
separate exact byte ceilings. Exact limits succeed; one-byte excess, partial or
failed sink writes, read failures, and sink close failure use the fixed
non-wrapping `tool output failed` or `tool run failed` errors without native,
path, argument, or secret text. Successful results expose exact final counts only
after all workers join; every error returns a zero result, while stdout may already
contain only its bounded permitted prefix. Once a valid request is accepted, one outer lifecycle owner context-closes the
sink exactly once even when production inventory lookup, validation, setup, or
pre-start cancellation fails. On cancellation, timeout, output failure, nonzero
exit, or lifecycle failure, finalization cancels the run, closes parent pipe
handles, terminates and context-closes the owned process, joins the waiter and both
pumps, and closes every owned descriptor once. `CleanupTimeout`, capped at five
seconds, bounds normal cleanup and sink close. If
an owned process tree is not terminated and reaped at expiration, cleanup fails
closed and explicitly overruns that budget while retrying hard termination with
bounded polling until process ownership, the waiter, native handles, and pumps are
resolved. `OutputSink` requires both `WriteContext` and `CloseContext`, so blocked
output and close operations honor the normal cancellation boundary without an
unkillable close goroutine.

### Observed RED → GREEN

- RED: `go test -mod=readonly ./internal/tools -run '^TestRun' -count=1` failed
  before the typed request/result, bounded pumps, and runner existed.
- GREEN: focused tests prove exact `--version` argv, hostile ambient-environment
  exclusion, explicit operation cwd and standard streams, empty production
  inventory behavior, invalid/pre-start cancellation rejection, nonzero/wait/start/
  termination/close failure mapping, exact successful counts, one-byte output
  ceilings with bounded stdout prefixes and zero failure results, short/failed/synchronously blocked cancellation-aware
  sinks, caller cancellation, setup timeout, descriptor/pipe/read/close failures,
  simultaneous large stdout+stderr, unique concurrent operation directories with
  removal, and an actual copied synthetic test executable invoked directly.

Fresh guarded checks passed: focused runner tests; focused tools race tests; tools,
platform, and credentials tests; full suite; full vet; format/diff checks; and
Windows AMD64 plus Darwin AMD64 tools compile checks (Windows vet too). No real
payload, PostgreSQL client, operational CLI command, passfile, network/download,
installation, hosted service, commit, or push was used. Native Windows Job Object
and existing macOS extended-ACL qualification gates remain open; synthetic helper
execution proves mechanisms only, not PostgreSQL provenance, dependency loading,
or Supabase compatibility.

A narrow P0 cleanup repair removed one-shot termination poisoning. Process
termination is serialized but retryable; Darwin keeps direct-child reap and
negative-group kill ownership under one native mutex and retries transient group
kill failures with bounded polling. `CloseContext` records normal-deadline expiry
as failure but does not return while its waiter or native ownership remains live.
Runner finalization defensively repeats hard close without a deadline after a
bounded close failure, then joins all waiter/pump result channels. Deterministic
fail-once Darwin and runner regressions prove the first failed kill/close cannot
release the result early, and the retained-pipe descendant regression remains the
end-to-end ownership check. This security-first exceptional cleanup can exceed
`CleanupTimeout`; a permanently unresolvable native kill intentionally fail-stops
rather than returning with an owned process tree.

### Task 05 P1/P2 follow-up — deterministic failed results and runner documentation

Runner failures now return a zero `RunResult`, so concurrent stdout/stderr completion
order cannot expose speculative sibling-stream counts. The stdout pump still reads
at most `remaining+1` and forwards exactly its permitted prefix before its own
overflow; a failed run cannot retract a prefix already accepted by the sink.
Repeated concurrent one-stream and two-stream overflow regressions assert the same
zero result and exact bounded stdout prefix. Partial-positive sink writes with nil
or error outcomes assert fixed `ErrOutput`, zero public results, and their actual
accepted prefix. Every stdout/stderr read-only or write-only pipe-plus-error setup
owns and closes each returned descriptor exactly once and returns fixed `ErrRun`.

RED: the new focused runner cases failed against retained partial byte counts on
errors. GREEN: after changing only final result publication, the focused accounting,
partial-write, and one-sided-pipe cases passed, followed by the complete `TestRun`
suite. No process retry behavior was changed.

Fresh guarded validation used offline/local module settings: the complete focused
`TestRun` suite passed; tools/platform/credentials compile-only tests and targeted
vet passed; the CLI built natively; tools test binaries compiled for Windows AMD64
and Darwin AMD64 without execution; formatting and `git diff --check` passed. No
full-suite or race loop was run in this follow-up.

The complete Task 05 mechanism boundary, layouts, diagnostics, output/lifecycle
policy, environment isolation, TOCTOU boundary, and proof limits are now summarized
in [`docs/tools.md`](tools.md).

Task 6 is implemented as a synthetic mechanism candidate; qualification still requires
native Windows Job Object, handle-inheritance, argv/environment, DACL/reparse, and
descendant runtime evidence; macOS extended/inherited ACL evidence; detached-Darwin
and abnormal-parent-death handling decisions/evidence; and real PostgreSQL payload
provenance, redistribution, signing/notarization, dependency loading, TLS,
noninteractive, and Supabase compatibility evidence. Current execution proof remains
synthetic only.

## Task 05, scoped PostgreSQL passfiles and typed connection slice — implementation candidate (Task 6)

Added synthetic-only `credentials.PGPassfile` and a typed connected runner request. `PGPassfile` accepts any verified private directory; the runner specifically chooses its private operation directory. It emits exactly one escaped PostgreSQL line, is closed before launch, and is supplied only through scoped `PGPASSFILE`. After process-tree, pump, descriptor, and output-sink closure, the runner makes one owned passfile deletion attempt and one operation-directory removal attempt. Either cleanup failure returns fixed `ErrRun`; abnormal termination can leave private plaintext and there is no secure-erasure claim. Inputs are bounded UTF-8 and reject empty fields, zero ports, controls, CR/LF/NUL, oversize values, standalone pgpass selector `*` host/database/user values, and database conninfo/URI selectors. Literal `*`, `?`, `:`, and `\` remain valid password characters; spaces are retained and only `:`/`\` are escaped. Public passfile errors are the fixed non-wrapping `credential passfile unavailable` sentinel; neither paths nor password bytes are returned.

Connected requests choose a typed client and explicit host/port/user/database/password only. They build fixed direct client flags (`--no-password`, host, port, user, database; `psql` also `-X --set=ON_ERROR_STOP=1`) and accept no SQL, scripts, stdin, arbitrary argv, environment, path, cwd, or shell. `PGPASSWORD`, ambient passfiles/services/psqlrc, PATH, loader, proxy, and credential variables are absent. Production payload inventory remains empty, so this does not enable a real client, backup, verify, or restore operation.

Focused tests cover exact escaping, spaces, invalid/oversize credentials, private exclusive creation, concurrent uniqueness, idempotent removal, redacted write/removal failures; typed argv/environment for every tool; no secret in argv/public errors; and passfile retention through sink close plus removal on success, failed start, nonzero exit, output failure, and cancellation. No real PostgreSQL client, payload, connection, download, installation, network, hosted project, CLI command, commit, or push was added. Native Windows passfile/process behavior and existing macOS extended-ACL qualification remain open gates.

## Task 05, documentation and integration audit (Task 7)

**Status:** implementation documentation is complete for the synthetic local mechanism
scope. It is not a PostgreSQL, Supabase, backup, verify, restore, release, or
real-credential support claim.

The Task 05 slices are integrated on `main` as `b8f0d92` (typed empty payload
inventory), `23f51b2` (private payload publication), `7594a4f` (bounded
extraction), `8a39b2d` (owned process execution), `d3405c7` (bounded execution),
and `e5b6843` (scoped passfiles). [`docs/tools.md`](tools.md) records the cache
layout and parser bounds; private staging/no-replace publication; full cache and
pre-launch revalidation; invalid-winner refusal; typed-only requests; fixed
diagnostics; isolated environment/cwd; bounded streams; zero failed results;
fail-stop ownership cleanup; and scoped passfile lifecycle. [`docs/credentials.md`](credentials.md)
owns private-storage and passfile validation, best-effort deletion, and plaintext
residual limits. [`SECURITY.md`](../SECURITY.md) limits all current evidence to
local synthetic mechanisms.

Remaining non-waivable qualification gates are effective macOS extended/inherited
ACL privacy; native Windows DACL/reparse/console, Job Object, handle-inheritance,
argv/environment, and descendant evidence; Darwin deliberately detached-descendant
and abnormal-parent-death limits; same-user replacement between final revalidation
and execution; and real PostgreSQL payload provenance, redistribution, signing or
notarization, dependency closure, TLS, noninteractive behavior, and Supabase
compatibility. Production payload inventory remains empty. No backup, verify, or
restore command has been enabled.

Owner authorization remains required before real payload acquisition or build,
redistribution/provenance work, signing/notarization costs, release publication,
local PostgreSQL installation, native-machine provisioning, or hosted Supabase
testing. No such access, download, installation, or cost was incurred by Task 05.

### Final guarded verification

All commands below exited 0 under `GOTOOLCHAIN=local`, `GOPROXY=off`,
`GOSUMDB=off`, `GOWORK=off`, `GOENV=off`, `GOFLAGS=`, and `GOTELEMETRY=off`:

- `go mod verify`
- `go test -mod=readonly ./internal/tools ./internal/platform ./internal/credentials -count=1 -timeout=180s`
- `go test -mod=readonly ./... -count=1 -timeout=180s`
- `go test -mod=readonly -race ./internal/tools ./internal/platform ./internal/credentials -count=1 -timeout=180s`
- `go vet -mod=readonly ./...`, `gofmt` checks, `git diff --check`, and local
  Markdown-link checks
- Compile-only credentials/tools/platform test binaries for Windows AMD64 and
  Darwin AMD64/arm64; those binaries were removed and never executed.

## Task 06, passphrase-only local archive primitive — implementation candidate

Added the versioned `sparc-archive` local format and `internal/archive` package.
Each stream is encrypted independently with `filippo.io/age` scrypt encryption.
Payload filenames are opaque contiguous IDs (`00000000.age`, and so on); the
encrypted `manifest.age` is exclusive and published only after every payload
stream completed. Its absence means incomplete output, not an empty archive.
The encrypted manifest records an opaque ID, Unicode-preserved logical key,
scope, status, exact plaintext length, and SHA-256 digest. It rejects duplicate
or unknown JSON fields, duplicate keys/IDs, unsupported versions, non-integer
lengths, invalid IDs/keys/digests, and bounded-size/count/depth violations.

The selected recovery model is passphrase-only. Passphrases are nonempty,
valid UTF-8, control-free, and at most 4,096 bytes; they are never added to
filenames, manifests, argv, or normal diagnostics. The archive primitive has
no sender-authentication claim. It encrypts but does not establish artifact
provenance or author identity.

Tests cover zero-byte, one-byte, multi-chunk, and generated 8 MiB streams;
wrong/unsafe passphrases; truncation/corruption; finalization-write failure;
direct age-library decryption; duplicate/unknown/oversize manifest input;
Unicode logical-key preservation; payload and manifest round trips; an absent
manifest after source failure; and refusal before writing into an existing
manifest directory. The allocation benchmark uses a generated 8 MiB stream
rather than a large plaintext slice: on Apple M1/Darwin arm64,
`BenchmarkEncryptLargeStream` ran once in 472,342,708 ns with 268,715,704 bytes
and 127 allocations. This measures scrypt setup plus encryption; it is not a
scale/support claim.

Final guarded validation exited 0 under `GOTOOLCHAIN=local`, `GOPROXY=off`,
`GOSUMDB=off`, `GOWORK=off`, `GOENV=off`, `GOFLAGS=`, and `GOTELEMETRY=off`:
`go mod verify`; `go test -mod=readonly ./internal/archive
-count=1 -v`; `go test -mod=readonly -race ./internal/archive -count=1`;
`go test -mod=readonly ./... -count=1 -timeout=180s`; `go vet -mod=readonly
./...`; `go build -mod=readonly ./cmd/sparc`; `gofmt` and `git diff --check`;
and compile-only archive test binaries for Windows AMD64 and Darwin AMD64/arm64.
Those cross-compiled test binaries were removed without execution.

No Task 06 real payload, PostgreSQL connection, network service, hosted
project, or Supabase operation was added. Owner authorization remains required
before real payload work, release/signing costs, PostgreSQL installation,
native-machine provisioning, or hosted testing.

## Task 07, private local publication and offline archive verification

Implemented `internal/destination`, `internal/archive/reader.go`, and
`internal/verify`. Following the owner's choice, destination publication
requires an already-existing private parent directory on a supported local
volume; ordinary shared folders and network paths are refused. `Create` writes
into a random private sibling staging folder, checks every encrypted payload
by reopening/decrypting through authenticated EOF and comparing plaintext size
and SHA-256 **before** writing the encrypted final manifest, then atomically
publishes the finished directory with native no-replace semantics. Existing
final paths remain unchanged. Normal error cleanup removes only owned files in
the staging directory, best-effort; a crash may leave an unpublished private
staging folder. This is not a multi-file transaction or a durability guarantee.

`archive.Verify` validates the private directory, authenticated encrypted
manifest, strict bounded manifest schema and portable logical keys, exact
payload inventory, each age-authenticated EOF, length, and digest. Unknown or
extra files, missing payloads, symlinks, corruption, truncation, wrong
passphrases, and digest mismatches fail with fixed errors. Passphrase KDF work
is capped at age scrypt work factor 18. No archived content is executed.
`verify.Offline` requires only archive path and passphrase, does not read source
configuration or use networking, and reports byte integrity separately from
the manifest-declared complete/incomplete capture status. Passing integrity is
not proof of full Supabase recovery coverage. Source-folder deletion before
offline verification is covered by a test.

Focused tests passed: `go test -mod=readonly ./internal/destination
./internal/archive ./internal/verify -count=1 -v -timeout=240s`; source-redaction,
no-clobber, partial-write cleanup, non-private-parent refusal,
exact inventory, malformed UTF-8/surrogates, path/case collisions, and size/depth
limits are included.

Final guarded checks exited 0 with `GOTOOLCHAIN=local`, `GOPROXY=off`,
`GOSUMDB=off`, `GOWORK=off`, `GOENV=off`, `GOFLAGS=`, and `GOTELEMETRY=off`:
`go mod verify`; targeted Task 07 package tests; targeted package race tests;
`go test -mod=readonly ./... -count=1 -timeout=240s`; `go vet -mod=readonly
./...`; `go build -mod=readonly ./cmd/sparc`; `gofmt`; and `git diff --check`.
Compile-only archive/destination/verify test binaries succeeded for Windows AMD64
and Darwin AMD64/arm64; they were removed without execution. The generated-stream
8 MiB encryption benchmark ran once on Apple M1/Darwin arm64 in 502,363,792 ns
with 268,710,440 bytes and 122 allocations, mostly age scrypt setup; this is not
a scale/support claim.

Native Windows runtime and effective macOS extended/inherited ACL qualification
remain open. Unicode logical keys are preserved without normalization; later
extraction must detect platform normalization collisions. Source-mutation
detection, resume, remote destinations, public backup/verify/restore commands,
real payload provenance, PostgreSQL/Supabase behavior, and production inventory
remain unimplemented. No network, hosted service, real payload, local PostgreSQL
installation, or cost was involved in Task 07.

## Task 08 — Supabase HTTP transport and project-discovery foundation

**Status:** offline transport/fixture foundation implemented; **not live API
support**. No Supabase project, token, or network request was used.

`internal/supabase` adds a fixed-endpoint, GET-only client using an explicit
Management API token. It does not accept a public custom endpoint, disables
ambient proxies, rejects redirects, uses standard HTTPS verification and a
30-second timeout, caps response/pagination sizes, returns fixed redacted errors,
and exposes bounded `Retry-After` without automatically retrying. Its candidate
`GET /projects?limit=100&offset=N` fixture parses required project identity fields
and an optional candidate `db_version`; unknown keys are surfaced by name only.
Missing database version and feature inventory remain explicitly unknown. A
successful empty list is distinct from 403 permission denial. No API body is
persisted. The transport is not connected to the CLI and no project features are
claimed as discovered.

`docs/research/R10-api-contracts.md` records that the endpoint, pagination, field
schema, version field, permission scope, and feature discovery have **not** been
revalidated against current official documentation. The feature endpoint and
feature schema remain unimplemented rather than inferred from unknown fields.
`docs/research/R16-login.md` documents token-only intent, separate credential
roles, no token persistence, and no embedded OAuth client secret/browser flow.
Fresh owner approval is required before any public-doc network lookup or hosted
read test; hosted access is not needed for these offline tests.

### TDD and offline contract evidence

- **Red:** Before implementation, `go test ./internal/supabase -count=1` failed
to build because the tested `NewClient`, endpoint constants, error types, and
project/capability types were not defined.
- **Green:** The offline `httptest` cases cover fixed route and Bearer header,
no ambient proxy, redirect refusal/no cross-origin credential forwarding,
timeout, response and pagination bounds, 401/403/404/429/503 mapping,
`Retry-After` parsing/capping, malformed JSON/duplicate keys/control characters/
unpaired surrogates, unknown-field-name-only handling, and error redaction.
Empty successful discovery and permission denial are asserted separately.

### Guarded validation

All checks used `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOWORK=off`,
`GOENV=off`, `GOFLAGS=`, and `GOTELEMETRY=off`:

- `go mod verify` — passed.
- `go test -mod=readonly ./internal/supabase -count=1 -v` — passed.
- `go test -mod=readonly -race ./internal/supabase -count=1` — passed.
- `go test -mod=readonly ./... -count=1 -timeout=240s` — passed.
- `go test -mod=readonly -race ./... -count=1 -timeout=300s` — passed.
- `go vet -mod=readonly ./...` and `go build -mod=readonly -o /tmp/sparc-task08 ./cmd/sparc` — passed; temporary output removed.
- Supabase package test binaries cross-compiled for Windows AMD64 and Darwin AMD64/arm64; compile evidence only, binaries removed without execution.
- `gofmt` and `git diff --check` — clean.

**Remaining gate:** verify the candidate GET route, actual pagination and response
schema, token permissions, version discovery, and any feature endpoint against
current official documentation under owner authorization before describing this
client as compatible with Supabase. No password reset, role creation, setting
change, OAuth, database connection, service credential, or project mutation was
implemented.

## R03 — PostgreSQL connectivity and TLS

**Status: open; documentation refreshed and local TLS mechanisms tested, but no
Supabase support or project identity qualified.** On 2026-09-22, public
PostgreSQL 17/libpq, pgx, and Supabase connectivity/pooler documentation was
reviewed. Disposable loopback PostgreSQL 17.9 fixtures tested native `psql`
17.9 and cached pgx v5.8.0; a separate fixture exercised the pinned pgx
v5.11.0 config builder. `docs/research/R03-connectivity.md` records
source-backed route/TLS findings, fixture configuration/results, and limits. No
hosted project, credential, or endpoint was used. No PostgreSQL native client
was installed or payload added; pgx v5.11.0 was fetched and pinned after
approval.

The v5.8.0 fixture confirmed `verify-full` with a private CA and matching DNS
identity over IPv4/IPv6, rejection of wrong CA/hostname, and the weakness of
`verify-ca`/`require`. A separate v5.11.0 local probe verified the new builder's
TLS success, wrong-CA and hostname rejection, and explicit password/CA use with
poisoned files in a temporary home. Task 09 now has an offline config builder;
broader catalog/version qualification, supported-platform trust behavior,
native payload, Supabase routes, and project/backend identity remain open.
Separate exact owner authorization is still required for any hosted endpoint,
credential, or source/target identity test.

## Task 09 — Explicit pgx config and bounded catalog observation

**Status: in progress; candidate read-only observation only.** Added
`internal/database/connect.go`, `connect_test.go`, `pgx_config_test.go`,
`inspect.go`, `inspect_test.go`, `selectors.go`, and `selectors_test.go`.
The explicit parameter contract accepts only a canonical direct host bound to
the expected project ref, port 5432, `sslmode=verify-full`, and an explicit
`system` or absolute, normalized CA path. It bounds database/user identifiers
and rejects control text. Route classification distinguishes direct, shared
session-pooler, and transaction-pooler forms; `Validate` refuses both pooler
routes because neither has hosted project-binding qualification.

The module now pins pgx v5.11.0. `NewConnConfig` builds a parsed config with an
allowlisted DSN, explicit route/database/user/TLS settings, no DSN password, no
TLS fallback, fixed `application_name`, and empty passfile, servicefile,
client-cert, and key selectors. It refuses process environment variables
beginning with `PG` plus `SSL_CERT_FILE`/`SSL_CERT_DIR`; only after config
validation is the explicitly supplied password copied into memory. The builder is not wired into the CLI and is not project-identity proof.

`ObserveCatalog` bounds connection/transaction work to 15 seconds, begins an
explicit read-only transaction, sets `search_path=pg_catalog` and statement,
lock, and idle-transaction timeouts, then observes the PostgreSQL version, TLS,
and read-only transaction status. It accepts at most 256 unique literal schema
names (63 UTF-8 bytes each), passes them as a `text[]` parameter, and returns
exact presence results in caller order; it never interpolates names as SQL or
patterns. It reports installed extension name/version/schema with fixed bounds
(256 entries, 256 version bytes), plus exact `pg_class` relation facts for the
selected schemas: name, raw `relkind`, persistence, and partition status (at
most 10,000 rows). Unknown relation-kind codes remain raw observations. It does
not traverse dependencies or capture owners, ACLs, RLS, columns, or data. Only
PostgreSQL major 17 is accepted; this is not a full inventory, tenant-identity
proof, or hosted-support claim.

### TDD and verification

- **Red:** New `NewConnConfig` tests first failed to compile because the builder
  and ambient-configuration error were undefined. A route-propagation regression
  then failed because an unsupported pooler was collapsed into a generic
  parameter error; the builder now preserves the fixed refusal.
- **Green:** Focused tests cover URI escaping, TLS server-name/root config,
  absent fallbacks, fixed runtime params, password exclusion from the parsed
  DSN, invalid credentials, and rejection of `PGOPTIONS`, service selectors,
  route/password/TLS environment, and system-root overrides.
- A disposable PostgreSQL 17.9 loopback fixture exercised pgx v5.11.0 through
  `NewConnConfig`: TLS and major 17 were observed; wrong CA and hostname were
  rejected. A custom resolver/dialer forced the synthetic direct hostname to
  IPv4 loopback only. A temporary home contained a wrong `.pgpass` and invalid
  default client TLS files, which did not interfere with the explicit config.
  This probe was removed and is not a durable integration test.
- `go mod tidy` pinned pgx v5.11.0 and required checksums after network-fetch
  approval; no Supabase endpoint or credential was used.
- **Red:** Schema-selector and version-gate tests first failed to compile with
  undefined selector validation, bounds, and PostgreSQL-version gate symbols.
  The implementation then passed exact literal-name validation cases and the
  PG17-only major gate.
- **Local PostgreSQL:** The opt-in disposable PostgreSQL 17.9 loopback fixture
  exercises `observeCatalog`: TLS and read-only transaction are true, version is
  170009, and exact schema presence/missing results cover spaces, quotes,
  backslashes, regex punctuation, Unicode, exact 63-byte ASCII/UTF-8 identifier
  boundaries, a decoy, and a missing name. The bounded extension inventory
  includes the default `plpgsql` extension. Selected-schema relation facts cover
  tables, indexes, views, materialized views, sequences, partitions, and a
  same-name decoy in an unselected schema. It also
  rejects a wrong CA, hostname mismatch, and non-TLS connection. A custom
  resolver/dialer force the synthetic direct-route hostname to IPv4 loopback.
  It requires `SPARC_TEST_PG_BIN` naming an approved local PostgreSQL bin
  directory; it creates/removes its cluster and private certificates. It is
  currently excluded from Windows runtime because bootstrap uses a Unix socket.
- **Red:** The initial focused package test failed to compile with undefined
  `ConnectionParams`/`RouteKind` and route constants, as expected before the
  implementation.
- **Red/green (CA path controls):** After adding tests for ESC and Unicode
  control characters in CA paths, the focused test failed because `Validate`
  accepted them. Reusing a single strict UTF-8/control-text check fixed the
  boundary; the focused package suite then passed.
- `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off GOENV=off GOFLAGS= GOTELEMETRY=off go test -mod=readonly ./... -count=1 -timeout=240s` — passed.
- The same guarded environment with `go vet -mod=readonly ./...` and
  `go build -mod=readonly -o /tmp/sparc-task09-route ./cmd/sparc` — passed;
  temporary binary removed.
- `internal/database` tests cross-compiled for Windows AMD64 and Darwin arm64;
  compile evidence only, binaries removed without execution.
- `gofmt -d internal/database/connect.go internal/database/connect_test.go` and
  `git diff --check` — clean for the initial route slice.
- After pinning pgx v5.11.0: `go mod verify`, focused database tests, full
  `go test -mod=readonly ./... -count=1 -timeout=240s`, `go vet -mod=readonly ./...`,
  CLI build, Windows AMD64 and Darwin arm64 database test cross-compiles, `gofmt`,
  and `git diff --check` all passed.
- The default-parallel full race run hit its 5-minute package timeout in the
  archive scrypt test under concurrent load. The serial full rerun
  `go test -mod=readonly -race -p=1 ./... -count=1 -timeout=600s` passed.
- `SPARC_TEST_PG_BIN=/opt/homebrew/opt/postgresql@17/bin GOTOOLCHAIN=local
  GOPROXY=off GOSUMDB=off GOWORK=off GOENV=off GOFLAGS= GOTELEMETRY=off
  go test -mod=readonly -tags=integration ./internal/database -run '^TestObserveCatalog' -count=1 -v`
  — passed on local macOS. Omitting `SPARC_TEST_PG_BIN` skips the fixture; the
  integration-tagged database package cross-compiled for Windows AMD64 but was
  not executed.
- After adding inspection/selectors: `go mod verify`, full
  `go test -mod=readonly ./... -count=1 -timeout=240s`,
  `go test -mod=readonly -race ./internal/database -count=1 -timeout=120s`,
  `go vet -mod=readonly ./...`, CLI build, Windows AMD64/Darwin arm64 database
  test cross-compiles, `gofmt -d`, and `git diff --check` passed.

**Still open:** expand catalog/security/dependency observation and selector
coverage; add native Windows runtime coverage and durable wrong-major refusal;
qualify system roots on supported platforms and the selected native payload; and
separately authorize any hosted route/identity test. No backup/verify/restore
command is enabled.
