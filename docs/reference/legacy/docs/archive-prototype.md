# Local archive prototype (experimental)

This is the first artifact-packaging experiment for SPARC. It is **not** a Supabase project exporter, database restore tool, production backup, or stable archive-format promise. Never use it as the only copy of real data.

## Commands

```text
sparc keygen IDENTITY_FILE
sparc pack SOURCE_DIR ARCHIVE_DIR RECIPIENT
sparc verify ARCHIVE_DIR IDENTITY_FILE
sparc unpack ARCHIVE_DIR DESTINATION_DIR IDENTITY_FILE
```

`SOURCE_DIR` is a quiet folder of already-exported artifacts. Packing does not contact Supabase or execute SQL, source code or scripts. Unpacking restores local files only. The original source is unnecessary for verification and unpacking.

All output destinations must be new. Existing empty directories also count as existing: the prototype will not overwrite or merge them.

## Local desktop alpha

A macOS Tauri alpha now wraps these same Rust operations in two guided flows. It can create a new unencrypted recovery key, pack and independently verify a local source folder, or verify an existing archive before restoring its files locally. **Local artifact archives only — it is not a Supabase exporter, Supabase restore tool or production backup application.**

```sh
cd desktop
npm ci
npm run tauri dev
# Build an ad-hoc-signed local application bundle:
npm run tauri build -- --bundles app
```

The local bundle is `desktop/src-tauri/target/release/bundle/macos/SPARC.app` relative to the repository. It is ad-hoc signed for local execution; it is **not Developer ID signed or notarized** and is not for redistribution. The renderer receives only bounded counts and safe errors from three named Rust commands. It has native open/save dialogs but no generic shell or unrestricted filesystem plugin. Selected paths live only in current React memory; there is no history, browser storage, Keychain integration, telemetry, cancellation or detailed progress.

The desktop alpha does not change the archive format, limits or partial-output behavior below. A failed create can leave the unencrypted recovery key and a partial archive; a failed restore can leave a partial private destination. Retry only with new output paths.

## Recovery key

The prototype uses native age X25519 identities and recipients. `keygen` writes the private identity with owner-only permissions and prints only the public `age1...` recipient. Only the recipient belongs in a command argument.

**The private identity file is unencrypted.** Keep it outside the input folder, outside the archive, outside Git, and in separately protected storage. A copied plaintext recovery key grants access to the archive. Passphrase-protected identities, friendly recovery-key UX and macOS Keychain integration are not implemented in this slice. Losing the key means losing recovery access.

The `.agekey` filename suffix is Git-ignored as a secondary safeguard, not a substitute for storing keys outside this public repository. Do not put private keys in shell environment variables, command arguments, screenshots, issue reports or logs.

## Folder format

```text
archive/
  00000000.age
  00000001.age
  ...
  manifest.age
```

Each artifact is a separate standard, binary age file encrypted to the selected recipient. There is no custom cryptographic primitive or container. The encrypted manifest is published last; an absent manifest means the archive is incomplete.

After decryption, the manifest is JSON:

```json
{
  "format": "sparc-local-artifacts",
  "version": 1,
  "directories": ["database"],
  "artifacts": [
    {
      "id": 0,
      "path": "database/data.sql",
      "bytes": 0,
      "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    }
  ]
}
```

The example describes an empty file. Artifact IDs must be contiguous and ordered from zero. Payload filenames are the ID encoded as eight lowercase hexadecimal digits followed by `.age`. Every parent directory appears in `directories`, including empty directories. Source path bytes are never used as payload filenames.

Verification validates the manifest, requires exactly the listed payloads plus `manifest.age`, reads every decrypted stream through its authenticated end, and compares plaintext byte counts and SHA-256 hashes. This establishes consistency with the decrypted manifest, not the completeness of a Supabase backup.

## Bounds and file semantics

Prototype bounds:

- 16 MiB decrypted manifest; ciphertext manifest capped at 32 MiB.
- 100,000 combined file/directory entries. Packing also uses a conservative 16 MiB inventory budget (twice each path's byte length plus 256 bytes per entry), which can be reached before that entry count.
- 64 relative path components; 4,096 UTF-8 bytes per relative path.
- 128 GiB declared total plaintext file bytes.

These bound parsing and extraction; they are **not** measured performance or supported-scale claims. File contents are streamed with a fixed-size buffer. The bounded manifest and file inventory are held in memory.

Only regular files and directories are supported. Symlinks, special files, non-UTF-8 paths, absolute paths, dot/parent components, backslashes, colons and control characters are rejected. A source must remain unchanged during packing; this is not a filesystem snapshot. Detected changes cause failure, but metadata comparisons cannot prove that an adversarial writer did not change and replace bytes during a read.

Unpacking preserves supported relative paths and bytes, including empty files/directories. It intentionally does not restore executable bits, timestamps, ownership, ACLs, xattrs or hard-link relationships. macOS filesystems may fold case or normalize Unicode; collisions fail rather than silently replacing a file. This experiment is not a general-purpose filesystem backup.

## Security and failure behavior

- Archive/restore directories are private (`0700`) and files are private (`0600`) on Unix. Original source permissions are not widened or changed.
- Full encryption hides original paths, JSON, SQL and file bytes. It does **not** hide ciphertext lengths, payload count, filesystem timestamps or the age format/recipient type. The archive is not padded.
- Age authenticates ciphertext, but does not authenticate its sender. Anyone with the public recipient can construct an entirely new encrypted archive. Restore only trusted archives; there is no sender-signature feature yet.
- A failed pack can leave a partial ciphertext directory, without a final manifest. Retry in a new destination; resumable packing is not implemented.
- A failed unpack can leave a partial private output directory. Files are first written to private temporary files and only published after their complete size/hash/authentication checks. Discard an incomplete destination or inspect it knowingly; retry in a new destination. There is no whole-restore transaction.
- Temporary decrypted files are part of unpacking. They are cleaned up on normal error paths, but sudden termination or machine failure may leave them behind. SSD/snapshot secure erasure is not promised.
- The prototype rejects static symlinks and malformed paths, but does not defend against another process with equivalent local permissions concurrently replacing files/directories. Use stable, user-owned locations, not attacker-writable directories.
- No compression, deduplication, partial encryption, remote transport, resumable transfer, passphrase prompt or GUI is included yet.

## Independent recovery with age

The separate Go `age` implementation can decrypt the manifest for inspection:

```sh
age --decrypt --identity /secure/location/recovery.agekey archive/manifest.age
```

That prints potentially sensitive names and metadata: do not run it in recorded terminals or paste the result into issues. Individual payloads can likewise be decrypted using age; consult the decrypted manifest to map payload IDs to original paths. SPARC verification adds manifest schema/path checks and cross-payload size/hash checks.

## Still unproven

- Live Supabase database/Auth/Storage/function export and restoration.
- Vault root-key migration, Storage ownership and complete function dependencies.
- Docker-free parity with Supabase's dump/restore behavior.
- Packaging PostgreSQL/Supabase tools in a signed and notarized macOS app.
- Interrupted-job resumption and the 10 GB database / 100 GB Storage validation target.

Live tests require explicitly authorized disposable projects and a cost ceiling. Do not create or modify hosted projects just to run the local tests.
