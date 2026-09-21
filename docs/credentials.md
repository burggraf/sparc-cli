# Private storage and credential boundaries

This page currently documents Task 04's storage slice only. Profile parsing and
credential input are not yet implemented. No operational command consumes these
APIs and no native credential store or plaintext-persistence fallback is enabled.

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
runner lacks symlink privilege/Developer Mode. Console testing belongs to the
later credential-input slice. Task 04 remains incompletely qualified.

## Dependency provenance

Pinned from the existing local Go module cache, with network resolution disabled:

- `golang.org/x/term v0.39.0` (BSD-3-Clause), reserved for hidden terminal input.
- `golang.org/x/sys v0.40.0` (BSD-3-Clause), native filesystem/security APIs;
  this is also the exact version required by the pinned `x/term` module.

`go.sum` records module checksums; offline `go mod verify` checks cached contents.
This is not a current vulnerability audit. Future version changes require review
of upstream security notices, license/checksum changes, and native regression
checks on both platforms; no runtime download or tool installation is used.
