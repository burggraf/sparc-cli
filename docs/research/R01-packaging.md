# R01 — packaging mechanics and trust boundary (Task 02)

**Status:** research incomplete. The single-file strategy is not selected and
no product support is implied.

## Approved-now synthetic evidence

`experiments/packaging` is isolated, stdlib-only, non-production code. It uses
only already-installed `go`, `codesign`, `xattr`, and `file`; it installs and
downloads nothing. Its opt-in smoke builds a synthetic macOS arm64 Go helper,
ad-hoc-signs it, runs it adjacent by absolute path, builds a separate Go outer
executable with `embed`, extracts only into a new private directory, verifies
SHA-256 before extraction and again before execution, then runs the extracted
helper by absolute path with only `PATH`, `HOME`, and `TMPDIR` supplied.

The extraction unit tests were added before implementation. **Historical local
Go 1.25.6 observation:** the initial bare command
`go test ./experiments/packaging -count=1 -v` failed to build because `Extract`
and `RunVerified` were undefined. The current guarded command
`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./experiments/packaging -count=1 -v`
passes: hash mismatch is refused before destination creation, an existing
destination is not clobbered, and a tampered helper is refused before execution.

Run the opt-in mechanics smoke only on macOS arm64:

```sh
SPARC_PACKAGING_SMOKE=1 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./experiments/packaging -run TestOptInEmbeddedSmoke -count=1 -v
```

When smoke is enabled, the three environment values are mandatory. Each child
`go build` receives a bounded environment with those values plus private Go
cache locations; missing settings or a missing compatible local toolchain fail
instead of downloading or installing one.

### Exact local observation

Latest local observation recorded `2026-09-21T14:45:16Z`:

- Host: `Darwin 27.0.0 arm64`; macOS `27.0` (`26A428`)
- Go: `go version go1.25.6 darwin/arm64` at `/opt/homebrew/bin/go`
- Tools: `/usr/bin/codesign`, `/usr/bin/xattr`, `/usr/bin/file`
  (`file-5.41`; `codesign` and `xattr` expose usage text rather than a version)
- Smoke result: PASS; adjacent and embedded helpers both printed
  `synthetic-helper 1`.
- `file` reported both original and extracted helpers as `Mach-O 64-bit
  executable arm64`.
- SHA-256 original/extracted:
  `eee58c0739bbc3c7f758ba0777eaf3b48d0b55ec2ed9ee57069f5624a8c5ca31`
- CDHash original/extracted:
  `61761f6338e95d199298f538e6898ec37b2c08e4`
- `codesign --verify --strict` passed for both. `xattr -l` returned only
  `com.apple.provenance: \x01\x02` for each; no quarantine claim was made.

The source signed helper, outer executable, and extracted helper were in OS
test temporary directories and removed by the test. For `go:embed` compilation,
a byte-identical signed helper copy and generated outer source briefly occupy
an ignored repository-local `experiments/packaging/.smoke-outer-*/` directory;
cleanup removes it. None is tracked.

## What this proves—and does not

Ad-hoc signing proves only that the synthetic Mach-O bytes and its ad-hoc
signature/CDHash survived embedding and extraction. It does **not** prove
Developer ID signing, secure timestamps, notarization, Gatekeeper behavior,
public trust, or any PostgreSQL client dependency closure.

Denied-write behavior and quarantine/Gatekeeper behavior are pending: this run
did not manufacture a denial or a browser-origin quarantined artifact. No
`spctl` result is presented as trust evidence.

The tamper test asserts the fixed category `synthetic helper hash mismatch`
without reporting a path or helper content. A deliberate temporary bypass of
the execution hash comparison made that focused test fail because the tampered
script executed; restoring the comparison made it pass. This is sensitivity
evidence for the guard, not a production extraction claim.

## Separate offline and OS-trust tests

Dependency-offline extraction is a separate property from OS trust evaluation.
Future clean-machine evidence must record all three states independently:

1. normal online validation with normal OS trust connectivity;
2. first-ever offline validation from fresh OS assessment state, with no
   dependency or trust network; and
3. offline validation after an earlier online/cached assessment.

A cached assessment cannot satisfy first-ever offline evidence. The current
synthetic smoke supports only the narrower fact that extraction makes no
runtime dependency download.

Adjacent tools remain a control/fallback and require owner approval before any
release selection. A ZIP with adjacent bare tools is not assumed to solve
offline OS trust.

## Blockers

A release decision remains blocked on approved PostgreSQL artifact inspection
and native TLS dump/restore, macOS Developer ID/notary evidence and
clean-machine assessment, Windows signing/MOTW/SAC/App Control evidence, and
an owner-approved release shape. No signing identity, notary service, hosted
service, client payload, or account was used in this task.
