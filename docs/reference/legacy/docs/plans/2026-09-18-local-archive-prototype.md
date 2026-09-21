# Local Encrypted Archive Prototype Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Prove that a portable encrypted artifact folder can be created, verified and restored locally without the original source folder.

**Architecture:** One Rust package exposes a small library and a thin CLI. Each file is streamed into a standard age-encrypted payload; an encrypted JSON manifest describes the original relative paths, sizes and SHA-256 checksums. This is a local artifact-packaging proof, not yet a Supabase backup tool or production archive format.

**Tech Stack:** Rust, the age crate, serde/serde_json, sha2, tempfile, and anyhow. Rust's built-in test runner; no frontend or async runtime yet.

**Workspace:** Work directly on `main`, without worktrees, as explicitly requested by the user on September 18, 2026.

---

## Boundaries and choices

- Input is a quiet local folder of regular files and directories, standing in for future database/configuration/Storage/function exports. No network calls or Supabase credentials.
- Use native age X25519 recipient keys first. The private recovery key is written with owner-only permissions, outside the archive. Passphrase UX and Keychain integration follow later; the prototype must clearly warn that the key file is unencrypted.
- Use standard age files rather than custom encryption or a tar extractor. Payload filenames are opaque numbered identifiers. The manifest itself is encrypted and published last.
- Verify every payload by consuming its complete authenticated stream and comparing its size and hash. Do not call an archive verified merely because its manifest decrypts.
- Refuse existing archive/restore/key destinations, including symlinks. Reject unsafe relative paths, malformed or unsupported manifests, missing/unlisted payloads, symlinks and special input files.
- Bound manifest size, entry count, nesting, and declared total bytes. Preserve empty directories and regular-file bytes, not POSIX ownership, executable bits, ACLs, extended attributes or symlinks.
- Archive/restore outputs use owner-only directories and files on macOS. Source folders must remain stable while packing; this is not a filesystem snapshot or protection against a concurrent malicious local process.
- A failed pack may leave an incomplete ciphertext directory without a manifest. A failed restore may leave an incomplete, private output directory; only verified files are published there. Neither operation may overwrite existing user data or report success.
- Full-file sequential transfers first. No compression, deduplication, partial encryption, resumable jobs, remote destinations, GUI, project creation or live restore in this slice.
- Initial size-support goals remain 10 GB database / 100 GB Storage, but this prototype does not claim those benchmarks have passed.

## Task 1: Package and failing round-trip check

**Files:** Create `Cargo.toml`, `src/lib.rs`, `tests/archive.rs`; update `.gitignore` to exclude `/target/` and development recovery-key files.

1. Create the package configuration with a reusable library; publishing to crates.io is disabled.
2. Add public API signatures with explicit not-implemented errors so the regression test compiles and fails because the behavior is absent.
3. Add a built-in Rust integration test that creates synthetic SQL, JSON, a binary object larger than one encryption chunk, an empty file and an empty directory, then packs them.
4. Remove the source folder. Verify and restore with the recovery identity, and compare every byte and directory.
5. Run `cargo test --test archive round_trip_without_source`; confirm the not-implemented failure before writing the implementation.

The test exercises the real library and real age encryption, not mock APIs:

```rust
let identity = age::x25519::Identity::generate();
let manifest = sparc::pack(&source, &archive, &identity.to_public())?;
std::fs::remove_dir_all(&source)?;
assert_eq!(sparc::verify(&archive, &identity)?, manifest);
sparc::unpack(&archive, &destination, &identity)?;
assert_eq!(std::fs::read(destination.join("database/data.sql"))?, sql);
```

## Task 2: Streamed encryption and verification

**Files:** Implement `src/lib.rs`; extend `tests/archive.rs`.

1. Define a versioned manifest with explicit format identity, artifact records and empty-directory records. Reject unknown schema fields/versions rather than silently misinterpreting them.
2. Traverse regular source files with a depth/entry limit. Reject symlinks and unsafe/non-UTF-8 paths, and reject archive destinations inside the source.
3. Hash while copying fixed-size buffers into age stream writers. Finish and synchronize ciphertext files before recording them.
4. Serialize the bounded manifest through an age writer to a private temporary file. Publish it without replacing any existing manifest, after all payloads have completed.
5. Implement manifest loading/validation and complete-stream payload verification. Payload filenames must be derived from validated identifiers, not arbitrary paths supplied by an archive.
6. Restore into a newly created private directory. Write each file to a private temporary file, verify it, then publish without clobbering. Preserve empty directories; do not execute anything from the archive.
7. Run the round-trip test again and confirm it passes.

## Task 3: Safety regressions and thin CLI

**Files:** Extend `tests/archive.rs`; create `src/main.rs` and `tests/cli.rs`.

Write each relevant failing check before implementing its guard:

- Wrong key and truncated or modified ciphertext do not verify.
- Missing payload, unlisted file, invalid version and invalid manifest do not verify.
- Authenticated but malicious manifests with traversal/absolute paths, duplicate paths/IDs, invalid hashes or excess resource declarations are rejected.
- Source/archive symlinks and special files are rejected; no output path can escape the selected destination.
- Existing archive, recovery-key and restore destinations remain untouched.
- Case/normalization collisions on the destination filesystem must error rather than overwrite.
- An absent encrypted manifest means incomplete, not empty-and-valid.
- Decrypted filenames/content are not written into the archive, and output permissions are private on Unix.

Expose four deliberately small developer commands:

```text
sparc keygen IDENTITY_FILE
sparc pack SOURCE_DIR ARCHIVE_DIR RECIPIENT
sparc verify ARCHIVE_DIR IDENTITY_FILE
sparc unpack ARCHIVE_DIR DESTINATION_DIR IDENTITY_FILE
```

`keygen` prints only the public recipient, never the private identity. Identity files are read from a path, not accepted as secret command-line arguments. Errors are nonzero and must not echo key contents. The CLI prints only counts/bytes and explicitly identifies local verification/restoration, not a Supabase project restore.

Use standard argument matching rather than adding a CLI framework for four fixed commands. Later user-facing CLI requirements can justify one.

## Task 4: Independent verification and documentation

**Files:** Update `README.md`; create `docs/archive-prototype.md`; commit `Cargo.lock`.

1. Run `cargo fmt --check`, `cargo test --locked`, and `cargo clippy --locked --all-targets -- -D warnings`.
2. Build the executable and run a complete CLI round trip with synthetic data in a temporary directory outside the repository.
3. Use the separately installed Go `age` CLI to decrypt the Rust-created manifest and a payload. Compare the payload with the synthetic input. Use an age CLI-created ciphertext to exercise library verification of the same declared file.
4. Check wrong-key failure, pre-existing-target refusal and that private keys never appear in captured command output.
5. Document the exact format, recovery-key handling, metadata leakage (ciphertext sizes/file count), resource bounds, partial-output behavior, supported file types, and lack of sender authentication.
6. Record test results and explicitly leave macOS app packaging, Docker-free Supabase export parity, Storage ownership, function dependency completeness and live round-trip tests as unproven next steps.

## Acceptance gate

A local archive survives removal of its input folder; restores identical supported contents; rejects the specified corrupt/unsafe inputs; interoperates with the independent age implementation; and never overwrites existing destinations. Passing this gate proves artifact packaging only, not Supabase completeness or production readiness.

## Execution results — September 18, 2026

Implemented directly on `main`, without worktrees, as requested. The initial round-trip and CLI tests were observed failing against not-implemented entry points before implementation.

Verified on macOS 27.0 / Apple Silicon, Rust 1.91.1, using Go age 1.3.2 for independent interoperability:

- `cargo test --locked`: **14 passed**; the external-tool test is opt-in.
- `cargo test --locked --test age_interop -- --ignored`: **1 passed**, testing Rust-to-Go and Go-to-Rust encryption/decryption.
- `cargo clippy --locked --all-targets -- -D warnings`: **passed**.
- `cargo fmt --check`: **passed**.

The expanded filename-collision check exposed a cleanup issue: `tempfile::PersistError` retains its temporary file while the error is held. All publication callers now convert that error to `io::Error` before propagation, immediately dropping the private staging file. The regression remains in the suite.

On this macOS filesystem, invalid UTF-8 filenames are rejected by the OS before packing; the explicit non-UTF-8-source test is conditional on other Unix targets and was not run here. Case-folding and Unicode-normalization collision checks ran on the actual destination filesystem.

No live Supabase resources, credentials or paid services were used. No 10 GB / 100 GB benchmark, independent security audit, GUI, job resumption or signed application packaging is claimed. Those remain subsequent work.
