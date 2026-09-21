# Isolated packaging experiment (non-production)

This directory is a Task 02 mechanics proof, not CLI code or reusable
extraction infrastructure. It contains no PostgreSQL payload and makes no
network request or tool installation.

Normal tests exercise hash mismatch/tamper refusal and no-clobber extraction:

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./experiments/packaging -count=1 -v
```

The macOS arm64 smoke is intentionally opt-in; the guarded full suite skips it
and therefore does not invoke `codesign`, `xattr`, or `file`:

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./...
```

Run the macOS arm64 smoke explicitly:

```sh
SPARC_PACKAGING_SMOKE=1 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./experiments/packaging -run TestOptInEmbeddedSmoke -count=1 -v
```

When smoke is enabled, those three settings are required and every child `go
build` receives a bounded environment with the same settings, private cache
locations, and no inherited Go configuration. Missing settings or a missing
local toolchain fail rather than downloading or installing anything.

The smoke builds and ad-hoc-signs a temporary synthetic Go helper, compares
adjacent absolute-path execution with an outer Go executable using `embed`,
and checks the extracted helper bytes/signature. It extracts only into a new
private test directory, verifies SHA-256 before extraction and execution, and
uses a sanitized helper environment. The signed helper is copied briefly into
an ignored repository-local `.smoke-outer-*/` directory for `go:embed`
compilation; the source helper, outer binary, and extraction artifacts use OS
test temporary storage. Cleanup removes the ignored directory and all test
temporary binaries.

Ad-hoc signatures demonstrate byte preservation only. They do not demonstrate
Developer ID, notarization, Gatekeeper, or public-release trust. Quarantine and
denied-write observations remain pending rather than synthetic proof.
