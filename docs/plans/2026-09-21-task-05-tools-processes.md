# Task 05 Trusted Tools and Bounded Processes Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Add synthetic-only trusted-payload extraction, bounded subprocess execution, native process-tree ownership, and scoped PostgreSQL passfile primitives without enabling backup, verify, or restore.

**Architecture:** `internal/tools` owns a compiled typed payload inventory, streaming extraction, full pre-launch revalidation, and bounded execution. `internal/platform` owns private executable files, no-replace directory publication, and native process-tree lifecycle. `internal/credentials` owns one-operation PostgreSQL passfiles. Production inventory remains empty until separately approved client artifacts exist.

**Tech Stack:** Go 1.25 standard library plus already-pinned `golang.org/x/sys v0.40.0`; no new dependencies, downloads, PostgreSQL installation, or hosted services.

---

## Scope and proof limits

- Synthetic Go test executables prove mechanisms only; they do not prove PostgreSQL provenance, redistribution, dependency loading, TLS, signing, or Supabase compatibility.
- macOS process groups do not contain deliberately detached descendants. Windows Job Objects require native runtime proof; cross-compilation is compile evidence only.
- Task 04 macOS extended-ACL and native Windows private-storage gates remain open. No real credentials may rely on unresolved protection.
- Same-user replacement between final revalidation and execution remains outside the threat boundary and must be disclosed.
- Do not import or promote `experiments/packaging`, parse `build/clients/manifest.json` at runtime, use PATH fallback, invoke a shell, buffer complete process output, or add operational CLI commands.

## Task 1: Define a typed, empty production payload inventory

**Files:**
- Create: `internal/tools/payload.go`
- Create: `internal/tools/payload_test.go`

**Contract:** Define only `PGDump`, `PGRestore`, and `PSQL`; supported targets are Darwin arm64/amd64 and Windows amd64. Production inventory is empty and returns a fixed unavailable sentinel. Unexported test constructors may build synthetic manifests.

**Tests first (`TestPayload`):** reject unknown tool/major/OS/architecture/schema/purpose; duplicate and case-colliding paths; file/directory collisions; absolute/traversal/nonportable names; invalid SHA-256/length/mode; arithmetic overflow; missing executable mappings. Assert hostile PATH/environment/adjacent executables cannot satisfy an absent payload. Prove deterministic package identity changes for platform, major, inventory, mapping, mode, length, or digest changes.

**Bounds:** 1,024 files; portable ASCII paths up to 240 bytes; 256 MiB/file; 512 MiB compressed; 1 GiB expanded. These are parser ceilings, not product-size claims.

## Task 2: Add private executable files and no-replace publication

**Files:**
- Modify: `internal/platform/private.go`
- Modify: `internal/platform/private_darwin.go`
- Modify: `internal/platform/private_windows.go`
- Modify corresponding platform tests

**API:**

```go
func CreatePrivateFile(path string) (*os.File, error)
func OpenPrivatePayloadFile(path string, executable bool) (*os.File, error)
func SealPrivateExecutable(file *os.File) error
func PublishPrivateDir(staging, destination string) (bool, error)
```

**Tests first (`TestPrivatePayload`, `TestPrivatePublish`):** exclusive private creation; legacy credential files still require `0600`; macOS executable sealing yields descriptor-validated `0700`; Windows retains the exact protected DACL and treats chmod as irrelevant; reject wrong type/mode/owner/DACL/link/symlink/reparse/unsafe ancestor. Publication requires safe sibling paths on one qualified local filesystem, never replaces, and has one winner under concurrency. Inject write/sync/close/rename failures.

**Implementation:** create files private from the first handle. Seal through the opened descriptor. Publish with Darwin exclusive rename and Windows no-replace move; no copy fallback and no repair of a losing/invalid destination.

## Task 3: Implement bounded streaming extraction and complete-package validation

**Files:**
- Create: `internal/tools/extract.go`
- Create: `internal/tools/extract_test.go`
- Create: `internal/tools/helper_test.go`

**Tests first (`TestExtract`, `TestCache`):** exact synthetic inventory succeeds; reject missing/extra/duplicate/truncated/altered entries, traversal and platform path ambiguities, links/devices/sparse/unsupported metadata, malformed tar/gzip, trailing compressed members/data, bad checksums, and every declared bound. Verify executable format/CPU with `debug/macho` or `debug/pe`. Inject read/write/short-write/sync/close/publication failures. Test valid reuse, receipt forgery, tamper, mode/DACL changes, independent-process first-run races, killed publishers, and invalid winners.

**Implementation:** use one narrow gzip/USTAR format. Verify compressed digest, extract to an exclusive random private sibling staging directory with fixed-size buffers, validate every length/hash/header, seal/sync/close, write a bounded receipt containing schema/package ID, then publish the complete directory without replacement. A losing contender removes only its own staging directory and validates the winner. Before every launch, revalidate the entire inventory and selected executable. Never execute staging, repair/delete invalid published packages, or add lock files/cache eviction.

**Layout:** `<NativeLocations.CacheDir>/tools-v1/<package-id>/` plus `.staging-<random-id>/` siblings.

## Task 4: Implement native process-tree ownership

**Files:**
- Create: `internal/platform/process.go`
- Create: `internal/platform/process_darwin.go`
- Create: `internal/platform/process_windows.go`
- Create corresponding native tests

**API:**

```go
type ProcessSpec struct {
    Path string
    Args, Env []string
    Dir string
    Stdin, Stdout, Stderr *os.File
}

type Process struct { /* native ownership */ }
func StartProcess(ProcessSpec) (*Process, error)
func (*Process) Wait() (int, error)
func (*Process) Terminate(context.Context) error
func (*Process) Close() error
```

**Tests first (`TestProcess`):** absolute-path direct launch, successful/nonzero/failed start, child+grandchild ownership, descendants retaining pipes, endless child, timeout/cancel, termination failure, reaping, and close failures. Use helper modes and bounded handshakes, never sleeps or shell scripts.

**Darwin:** launch directly with a new process group; terminate the owned group, reap the direct child, and bound cleanup. Document detached-session escape and abnormal-parent-death limits.

**Windows:** create a kill-on-close Job Object, create the process suspended with explicit application path/argv/environment/cwd and only intended inherited handles, assign it to the job, then resume. Assignment/resume failure must terminate/reap/close. Fail closed when existing-job constraints prevent ownership. Do not use start-then-assign, `taskkill`, breakaway flags, shell command lines, or parent-only `CommandContext`.

## Task 5: Implement bounded streaming execution

**Files:**
- Create: `internal/tools/run.go`
- Create: `internal/tools/run_test.go`
- Extend: `internal/tools/helper_test.go`

**Contract:** callers choose only a typed tool and structured request; no executable path, cwd, environment, arbitrary stdin, stderr forwarding, or shell option. Initially allow only `--version` without a connection, or no free-form arguments for a structured connected invocation.

Use positive overflow-safe timeout, cleanup timeout (maximum five seconds), stdout, and stderr limits. Stdout goes to a context-aware sink; stderr is counted and discarded. Public results contain exit code and byte counts only. Convert native/sink errors to fixed sentinels before any joining.

**Tests first (`TestRun`):** exact Unicode/space/quote/backslash argv; hostile ambient PATH/PG/loader/proxy/credential/HOME/cwd; concurrent huge stdout+stderr; exact limit and one-byte excess; synchronous backpressure; short/write/close failures; blocked cancellation-aware sink; pre-start/during-run cancellation; timeout; parent exit while descendants hold pipes; failed start/nonzero/cleanup failure; secret canaries in args/stderr/paths/errors.

**Implementation:** explicit pipes, one fixed buffer per concurrently pumped stream, no unbounded queues, nil stdin/EOF, bounded cleanup context after operation cancellation, tree termination on every failure, direct-child reaping, sink closed exactly once, and all owned goroutines joined before success. Build environment from scratch with private operation HOME/temp/config paths, fixed locale, no ambient PATH/PG/DYLD/proxy/credential values, and only required native Windows OS variables.

## Task 6: Add scoped PostgreSQL passfiles and structured invocation

**Files:**
- Create: `internal/credentials/passfile.go`
- Create: `internal/credentials/passfile_test.go`
- Extend: `internal/tools/run.go`, `run_test.go`

**API:**

```go
type PGPassEntry struct {
    Host, Database, User string
    Port uint16
    Password []byte
}

type PGPassfile struct { /* private lifecycle */ }
func NewPGPassfile(privateDir string, entry PGPassEntry) (*PGPassfile, error)
func (*PGPassfile) Path() string
func (*PGPassfile) Close() error
```

**Tests first (`TestPGPassfile`, `TestPGInvocation`):** exact escaped line; `:` and `\` escaping; reject wildcards, empties, controls, CR/LF/NUL, invalid UTF-8, zero port, oversized values/passwords; preserve spaces; no password in argv/`PGPASSWORD`; private exclusive file; unique concurrent scopes; removal after success/start failure/timeout/cancel/output/nonzero paths; fixed cleanup failure. Prove ambient `.pgpass`, service files, `.psqlrc`, and password variables are not selected.

**Implementation:** create one private passfile inside the runner-owned operation directory, close before launch, set only its `PGPASSFILE`, use explicit host/port/user/database arguments, `--no-password`/`-w`, and `psql -X --set=ON_ERROR_STOP=1` where applicable. Remove only after descendants and streams are closed. No secure-erasure claim; SQL/script input and real dump/restore recipes remain deferred.

## Task 7: Document, verify, review, and integrate

**Files:**
- Create: `docs/tools.md`
- Modify: `docs/credentials.md`
- Modify: `docs/progress.md`

Document cache layout/limits/publication, full revalidation, fixed diagnostics, sink cancellation contract, environment boundary, passfile lifecycle, same-user TOCTOU, process-tree escape limits, Task 04 blockers, empty production inventory, synthetic-only proof, and native versus cross-compile evidence.

**Guarded verification:**

```sh
unset SPARC_PACKAGING_SMOKE SPARC_TEST_PG_BIN SPARC_HOSTED_TEST_CONFIG GOOS GOARCH
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off GOENV=off GOFLAGS= GOTELEMETRY=off
go mod verify
go test -mod=readonly ./internal/tools ./internal/platform ./internal/credentials -count=1 -v -timeout=180s
go test -mod=readonly ./... -count=1 -timeout=180s
go test -mod=readonly -race ./internal/tools ./internal/platform ./internal/credentials -count=1 -timeout=180s
go vet -mod=readonly ./...
git diff --check
```

Compile each affected package separately for Windows amd64 and Darwin arm64/amd64 into one private temporary directory, then remove it. Do not execute cross-built binaries or call compilation native proof.

Run independent specification/security and code-quality reviews. Fix all findings, rerun verification, commit reviewed milestones on feature branches, fast-forward each into `main`, verify again on `main`, and push `main`.

## Owner and qualification gates

No owner decision is required to implement synthetic mechanisms. Owner authorization is required later for real payload acquisition/build, redistribution/provenance, signing/notarization costs, release publication, local PostgreSQL installation, native machine provisioning, or hosted tests.

Engineering gates that cannot be waived: resolve macOS extended-ACL privacy before real credentials; obtain native Windows DACL/reparse/Job Object evidence; qualify real payload dependency loading/signatures/TLS/noninteractive behavior; prove approved macOS helpers remain in their process group. Completion wording is limited to synthetic local mechanisms on named tested targets.
