# Task 04 Private Boundaries Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Add native private storage, strict non-secret profiles, and bounded credential input for macOS and Windows without enabling backup, verify, or restore.

**Architecture:** Keep three narrow packages: `internal/platform` owns native private files and locations, `internal/config` parses a fixed v1 profile schema, and `internal/credentials` resolves one explicit secret source or reads a hidden terminal. Public errors are fixed sentinels; native and parser details never cross those boundaries. Use the already cached, upstream-approved candidates `golang.org/x/term v0.39.0` and `golang.org/x/sys v0.40.0`; perform no network download.

**Tech Stack:** Go 1.25, standard library, `golang.org/x/term`, `golang.org/x/sys/unix`, `golang.org/x/sys/windows`.

---

## Scope and proof limits

- Supported implementation targets are macOS and Windows AMD64.
- Private storage protects against ordinary other users, not root/SYSTEM, administrators, or a compromised same-user process.
- Path validation is not race-free confinement against a malicious same-user process. Final-object exclusive/no-follow creation and handle validation are still required.
- macOS native tests prove POSIX owner/mode/link/type behavior on the local test filesystem. Windows DACL, reparse-point, console, and second-user behavior require native Windows evidence; cross-compilation is compile evidence only.
- No Keychain/Credential Manager persistence, recursive directory creation, config writer/migration, generic filesystem abstraction, operational command, or secret-valued CLI flag.
- Unknown CLI arguments use fixed diagnostics so argument contents cannot be reflected.

## Task 1: Pin qualified terminal and native-system dependencies

**Files:**
- Modify: `go.mod`
- Create: `go.sum`
- Modify: `docs/credentials.md` (created in Task 6)

**Steps:**
1. Confirm `golang.org/x/term v0.39.0` requires `golang.org/x/sys v0.40.0` from the local module cache.
2. Add those exact versions with `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`; a cache miss is a blocker, never a download fallback.
3. Run `go mod verify` offline and record BSD-3-Clause provenance in `docs/credentials.md` when that file is added.

## Task 2: Stop CLI argument disclosure

**Files:**
- Modify: `internal/cli/run.go`
- Modify: `internal/cli/run_test.go`

**Steps:**
1. Add a failing test passing a secret canary and terminal controls as an unknown argument.
2. Assert exact fixed stderr, empty stdout, exit 2, and absence of the canary/control bytes.
3. Replace reflected unknown-command output with a fixed diagnostic.
4. Run `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/cli -run TestRun -count=1 -v`.

## Task 3: Implement private native storage

**Files:**
- Create: `internal/platform/private.go`
- Create: `internal/platform/private_darwin.go`
- Create: `internal/platform/private_windows.go`
- Create: `internal/platform/private_test.go`
- Create: `internal/platform/private_darwin_test.go`
- Create: `internal/platform/private_windows_test.go`

**API:**

```go
type Locations struct {
    ConfigFile string
    DataDir    string
    CacheDir   string
}

var ErrPrivateStorage = errors.New("private storage unavailable")

func NativeLocations() (Locations, error)
func CreatePrivateDir(path string) error
func CheckPrivateDir(path string) error
func WritePrivateFile(path string, data []byte) error
func ReadPrivateFile(path string, maxBytes int64) ([]byte, error)
```

**Tests first:** native locations are absolute and create nothing; relative/traversal/control/NUL paths fail; linked ancestors and final symlink/reparse points fail; directory/file creation is exclusive; concurrent creation has one winner; existing objects are unchanged; special files and hard-linked files fail; reads are bounded; all public errors equal the fixed sentinel and contain no path/canary.

**macOS implementation:** parents must exist; directories are created `0700`; files use `O_CREAT|O_EXCL|O_NOFOLLOW` at `0600`; validate opened descriptor owner UID, regular/directory type, link count, and exact private mode before reading or writing. Reject unsafe or unverifiable ancestors rather than repairing them.

**Windows implementation:** reject UNC/device/drive-relative paths, alternate data streams, reserved names, trailing dots/spaces, traversal, controls, and reparse components. Build a protected DACL for current-user and LocalSystem only and pass it in `SECURITY_ATTRIBUTES` to `CreateDirectory`/`CreateFile(CREATE_NEW)` so protection exists at creation. Open validation handles with `FILE_FLAG_OPEN_REPARSE_POINT`, then inspect owner, DACL, reparse status, type, and link count. Never use chmod or create-then-repair as evidence.

**Verification:** run native package tests on macOS; cross-compile `windows/amd64` tests with `CGO_ENABLED=0`. Do not claim Windows runtime qualification from cross-compilation.

## Task 4: Implement explicit bounded credential input

**Files:**
- Create: `internal/credentials/input.go`
- Create: `internal/credentials/input_darwin.go`
- Create: `internal/credentials/input_windows.go`
- Create: `internal/credentials/input_test.go`
- Create: native test files as needed

**API:**

```go
type Reference struct {
    Env  string `json:"env,omitempty"`
    File string `json:"file,omitempty"`
}

var ErrInput = errors.New("credential input unavailable")
var ErrCancelled = errors.New("credential input cancelled")

func (Reference) Validate() error
func Input(ref *Reference, stdin *os.File, stderr io.Writer) ([]byte, error)
```

**Tests first:** exactly one source; bounded environment names; explicit source only; no ambient fallback; private-file reads; non-TTY prompt refusal without consuming input; 16 KiB bound; nonempty valid UTF-8; Unicode allowed; control/newline rejection; one file LF/CRLF terminator accepted; environment bytes are exact; fixed prompt; fixed non-wrapping errors; canaries absent after nested formatting, logging, and JSON error envelopes.

**Implementation:** a non-nil reference selects only that environment variable or private file. A nil reference requires `term.IsTerminal` on the supplied handle and uses bounded hidden input with terminal state restored on every ordinary return. Do not open another terminal device, accept argv values, trim spaces, serialize secrets, or silently truncate. Native console tests remain required for the platform they qualify.

## Task 5: Implement strict v1 profile parsing

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Schema:**

```go
type Config struct {
    Version  int       `json:"version"`
    Profiles []Profile `json:"profiles"`
}

type Profile struct {
    Name            string                 `json:"name"`
    ProjectRef      string                 `json:"project_ref,omitempty"`
    Destination     string                 `json:"destination,omitempty"`
    ManagementToken *credentials.Reference `json:"management_token,omitempty"`
}

var ErrConfig = errors.New("invalid configuration")
func Load(path string) (Config, error)
```

**Tests first:** valid metadata/references; 64 KiB total bound; depth 8; 32 profiles; duplicate keys at every object level including escaped aliases; unknown/case-variant keys; malformed UTF-8; BOM; unpaired surrogate escapes; decoded controls; wrong/null types; version other than integer `1`; duplicate profile names; oversized values; trailing JSON; invalid references; `password`/`token` value fields rejected; failure returns zero config and fixed non-wrapping error without canaries.

**Implementation:** read only through `platform.ReadPrivateFile`; perform one schema-specific token walk with `json.Decoder.UseNumber` to detect duplicates, depth, exact keys, and string constraints, then decode concrete structs and validate semantics. Keep helpers private; `DisallowUnknownFields` alone is insufficient.

## Task 6: Document, verify, review, and integrate

**Files:**
- Create: `docs/credentials.md`
- Modify: `docs/progress.md`

**Documentation:** native paths, explicit reference syntax, size limits, environment exposure, no argv secrets, same-user/privileged-user limits, no secure-erasure promise, local-filesystem scope, no plaintext persistence fallback, deferred native credential stores, dependency versions/licenses, and separate Windows runtime gate.

**Guarded verification:**

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off go test ./internal/cli ./internal/platform ./internal/config ./internal/credentials -count=1 -v
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off go test ./... -count=1
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off go vet ./...
# Compile each affected package separately for windows/amd64 into a private temporary directory.
git diff --check
```

Run an independent specification review and code-quality/security review. Fix all findings, repeat verification, commit the reviewed milestone directly on `main`, and push `main`. Record Windows native runtime and second-user checks as open qualification gates unless actual evidence is obtained.
