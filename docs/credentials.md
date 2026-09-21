# Private storage and credential boundaries

Task 04 provides private-storage primitives, strict v1 profile loading, and
explicit credential input. No operational command consumes these APIs and no
native credential store or plaintext-persistence fallback is enabled.

## Native locations (lookup only)

- macOS: config `~/Library/Application Support/sparc/config.json`, data
  `~/Library/Application Support/sparc`, cache `~/Library/Caches/sparc`.
- Windows: config under the native RoamingAppData known folder at
  `sparc\config.json`; data and cache under LocalAppData at `sparc\data` and
  `sparc\cache`. Known-folder lookup does not request directory creation.

Callers must provision existing parent directories explicitly. Creation is
exclusive, never recursive, and never repairs or overwrites an existing object.
Paths must be absolute, clean, valid UTF-8, and free of control characters.
Symlink/reparse ancestors and final objects are rejected. Files must be regular
and have one hard link. Reads have an explicit positive byte limit (less than
`MaxInt64`); oversized/error results return no data. Public errors are the exact
`private storage unavailable` sentinel, without native errors or paths.

## Security boundary

macOS checks opened descriptors for current UID, type, link count and exact
`0700` directory / `0600` file mode. File creation uses exclusive/no-follow flags.
Ancestors must be directories owned by the current user or root, without
other-user write permission. Only the named root-owned sticky shared-temp
anchors `/private/tmp` and `/private/var/tmp` permit group/world write access.
Paths through `/tmp` or `/var` symlink aliases must be resolved by the caller
before use; the storage API does not silently follow them.

**Open macOS security gate:** POSIX modes do not prove effective privacy when
extended/inherited ACLs grant additional access. This slice does not inspect or
remove those ACLs. Effective macOS ACL privacy and inherited-ACL tests remain a
release/qualification blocker. No real credentials should rely on this storage
slice as qualified protection before that gate is resolved.

Windows creates objects with an explicit owner and protected DACL attached to
`CreateDirectory` / `CreateFile(CREATE_NEW)`: current user and LocalSystem only,
full control, no inherited entries. Handles are checked for disk object type,
reparse status, link count, owner, and exact DACL policy. There is no chmod or
create-then-repair security claim. Ancestor ACLs are inspected: only current user,
LocalSystem and built-in Administrators may have write/create/delete or other
non-read rights. Other principals may have ordinary read/traverse rights.
Unknown ACE forms, unreadable ACLs and null DACLs fail closed. The local fixed or
removable volume root alone is a trusted ACL anchor (but still cannot be a
reparse point). UNC/network/device paths, alternate data streams, drive-relative
paths and reserved/ambiguous Windows names are refused.

These checks protect neither against an already-compromised same-user process
nor privileged/root/SYSTEM/administrator access. Ancestor checks are not
race-free confinement against same-user path replacement. Non-local filesystem
semantics are not qualified. Partial failed writes are removed on a best-effort
basis; there is no secure-erasure or crash-durability promise.

**Open Windows security gate:** cross-compilation is not runtime evidence.
Protected-DACL creation, ancestor policy, reparse behavior and second-user denial
require native Windows execution. The reparse test fails (not skips) if its
runner lacks symlink privilege/Developer Mode. Native console tests remain open on both macOS and Windows. Task 04 remains
incompletely qualified.

## Dependency provenance

Pinned from the existing local Go module cache, with network resolution disabled:

- `golang.org/x/term v0.39.0` (BSD-3-Clause), reserved for hidden terminal input.
- `golang.org/x/sys v0.40.0` (BSD-3-Clause), native filesystem/security APIs;
  this is also the exact version required by the pinned `x/term` module.

`go.sum` records module checksums; offline `go mod verify` checks cached contents.
This is not a current vulnerability audit. Future version changes require review
of upstream security notices, license/checksum changes, and native regression
checks on both platforms; no runtime download or tool installation is used.

## Explicit credential input

`credentials.Input` accepts either one `Reference` or a nil reference for hidden
input on the supplied terminal. `{"env":"SPARC_MANAGEMENT_TOKEN"}` selects only
that variable; `{"file":"/absolute/private/credential"}` selects only that file.
Reference environment names use ASCII `[A-Za-z_][A-Za-z0-9_]*`, at most 128 bytes.
File references are at most 4096 UTF-8 bytes, absolute, clean, and control-free;
the private-storage API additionally enforces native safety when reading them.
Missing/invalid sources fail without trying other variables, files, or prompts.
No secret value is accepted through this API's arguments as a command-line flag.
Environment variables are explicit opt-in inputs, not secure persistence: the
calling process and inherited child environments may expose them. This package
does not launch subprocesses, alter the environment, or provide an ambient chain.

Secrets must contain 1–16384 UTF-8 bytes, with no Unicode control characters or
Unicode line/paragraph separators. Spaces are preserved, not trimmed. A file may
end with exactly one LF or CRLF terminator, which is removed before validation;
environment values are exact (a terminal newline is invalid). File input uses
`platform.ReadPrivateFile` with a 16386-byte bound to accommodate that terminator.

A nil reference requires the supplied `*os.File` to pass `term.IsTerminal`;
redirected input and missing handles fail without reading or printing a prompt.
The pinned `x/term` provides native raw-mode entry/restoration on both platforms;
no additional per-platform terminal wrapper or unbounded `ReadPassword` buffer is
used. Hidden input has a fixed `Secret: ` prompt and one newline after the read
attempt and successful terminal restoration. CR/LF submits, Ctrl-C/Ctrl-D cancels,
and BS/DEL deletes the last UTF-8
rune. Overlong or ASCII-control-invalid input is drained through Enter while echo
is disabled, without retaining additional bytes. After entering this drain state,
Ctrl-C/Ctrl-D and editing bytes are ignored until Enter, and the result is input
failure rather than cancellation. Restoration is attempted immediately after the
read, before the final newline; a deferred fallback covers early returns and a
failed immediate restoration. Successful immediate restoration is not repeated.
Restoration/output errors discard the result and fail. An immediate restoration
failure suppresses the newline and remains an input failure even if the deferred
retry succeeds.
Process termination, console host behavior, and native Unicode/echo behavior
remain separate qualification work, not claims supplied by injected-mode tests.

Public failures are only `credential input unavailable` or
`credential input cancelled`, with no wrapped file, terminal, or environment
errors. Callers receive secret bytes only on success and must never log or
serialize them. There is no secure-memory/erasure guarantee.

## Strict v1 profiles

`config.Load` reads only a private file, bounded to 64 KiB including whitespace.
It never resolves the credential references or performs network requests. Example
with synthetic non-secret identifiers:

```json
{"version":1,"profiles":[{"name":"example","project_ref":"source-ref","destination":"s3://backups/prefix","management_token":{"env":"SPARC_MANAGEMENT_TOKEN"}}]}
```

The exact root keys `version` and `profiles` are required. Version must be the
integer token `1`, not a float, exponent, or string. Profiles may contain 0–32
entries. Each requires a unique, nonblank `name` (up to 256 UTF-8 bytes); optional
`project_ref` (256 bytes) and `destination` (4096 bytes) must also be nonblank when
present. `management_token`, if present, contains exactly one nonempty `env` or
`file` key; even an explicitly empty second key is rejected. Nothing is inferred
from a missing reference, and unknown password/token-value fields are refused.

All keys/types/case are exact. Nulls, duplicate decoded keys (including escaped
aliases), malformed UTF-8, BOMs, unpaired surrogate escapes, decoded controls,
line/paragraph separators, and trailing JSON are rejected. Container depth is
bounded to eight; the fixed schema itself admits only four nested containers.
Local destination paths, including Windows drive paths, `%`, and `@`, remain
inert metadata. Hierarchical URI-looking destinations containing `://` or starting
`//` are parsed to reject malformed URI syntax and userinfo; this is not scheme,
endpoint, or destination authorization. Profile metadata is user-designated
non-secret text, not a mechanism that can identify every possible secret value.

Every load failure returns zero `Config` and the fixed, non-wrapping
`invalid configuration` error. Partial profiles and input text are never attached
to errors. There is no config writer, migration, credential persistence, or
plaintext fallback. The macOS ACL and Windows runtime gates above still apply to
both config and credential file reads; real credentials remain unqualified.
