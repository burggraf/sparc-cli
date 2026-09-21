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
