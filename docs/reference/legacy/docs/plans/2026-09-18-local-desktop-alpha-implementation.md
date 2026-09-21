# Local Desktop Archive Alpha Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Build an ad-hoc-signed, locally testable macOS Tauri app—with no Developer ID signing or notarization—that creates, verifies and restores the existing encrypted local artifact archives through two guided UI flows.

**Architecture:** Add an isolated `desktop/` React/Vite/Tauri project whose Rust crate depends directly on the root `sparc` library. Move recovery-identity file handling from the CLI into the shared library, then expose three narrow Tauri commands returning bounded summaries and safe errors. Keep frontend state in memory and grant only native open/save dialog permissions.

**Tech Stack:** Rust 1.91, Tauri 2, React 19, TypeScript, Vite 8, Tailwind CSS 4, selected shadcn/ui source components, Vitest 5 and Testing Library.

**Workspace:** Work directly on `main`, without worktrees, as explicitly requested by the owner. One writer only.

---

## Non-goals and invariants

- No Supabase connection, export or restore.
- No shell, sidecar, unrestricted filesystem plugin, history, browser storage, telemetry, cancellation, byte progress, Keychain, Developer ID signing or notarization.
- The root `sparc` library remains the archive implementation. The UI never executes the CLI.
- Existing destinations remain no-clobber. Partial output may remain after failure.
- Private identities are never returned to JavaScript, printed, logged or placed in command arguments.
- The current root test suite and archive format must remain compatible.
- Configuration and generated app/icon scaffolding contain no behavioral logic; all application behavior follows red-green TDD.

## Task 1: Share recovery-identity file handling

**Files:**
- Create: `tests/identity.rs`
- Modify: `src/lib.rs`
- Modify: `src/main.rs`

### Step 1: Write the failing library tests

Add tests that describe the shared API before it exists:

```rust
use anyhow::Result;

#[test]
fn identity_file_round_trips_without_exposing_the_secret() -> Result<()> {
    let work = tempfile::tempdir()?;
    let path = work.path().join("recovery.agekey");

    let recipient = sparc::create_identity_file(&path)?;
    let identity = sparc::read_identity_file(&path)?;

    assert_eq!(identity.to_public().to_string(), recipient.to_string());
    assert!(recipient.to_string().starts_with("age1"));
    #[cfg(unix)] {
        use std::os::unix::fs::PermissionsExt;
        assert_eq!(std::fs::metadata(path)?.permissions().mode() & 0o777, 0o600);
    }
    Ok(())
}

#[test]
fn identity_file_refuses_overwrite_and_unsafe_inputs() -> Result<()> {
    // Create once, confirm a second creation fails without changing bytes.
    // Confirm an identity file with public permissions is rejected on Unix.
    // Confirm malformed, multiple-key and oversized files are rejected.
    Ok(())
}
```

### Step 2: Run the focused test and observe RED

Run:

```sh
cargo test --locked --test identity
```

Expected: compilation fails because `create_identity_file` and `read_identity_file` do not exist.

### Step 3: Implement the smallest shared API

Move the already-tested CLI logic, without changing its limits or messages, into:

```rust
pub fn create_identity_file(path: &Path) -> Result<Recipient>;
pub fn read_identity_file(path: &Path) -> Result<Identity>;
```

`create_identity_file` uses a private temporary file, `sync_all`, and `persist_noclobber(...).map_err(io::Error::from)`. `read_identity_file` retains regular-file, symlink, Unix `0600`, UTF-8, 16 KiB and exactly-one-native-identity checks.

Update `src/main.rs` to call the library functions and remove its duplicated imports/helper. The CLI continues printing only the public recipient.

### Step 4: Run GREEN and regression checks

```sh
cargo test --locked --test identity
cargo test --locked --test cli
```

Expected: all focused tests pass.

### Step 5: Commit

```sh
git add src/lib.rs src/main.rs tests/identity.rs
git commit -m "refactor: share recovery identity handling"
```

## Task 2: Scaffold the isolated desktop project

**Files:**
- Modify: `.gitignore`
- Create: `desktop/package.json`
- Create: `desktop/package-lock.json`
- Create: `desktop/index.html`
- Create: `desktop/tsconfig.json`
- Create: `desktop/tsconfig.app.json`
- Create: `desktop/tsconfig.node.json`
- Create: `desktop/vite.config.ts`
- Create: `desktop/vitest.config.ts`
- Create: `desktop/components.json`
- Create: `desktop/src/index.css`
- Create: `desktop/src/main.tsx`
- Create: `desktop/src/test/setup.ts`
- Create: `desktop/src-tauri/Cargo.toml`
- Create: `desktop/src-tauri/Cargo.lock`
- Create: `desktop/src-tauri/build.rs`
- Create: `desktop/src-tauri/tauri.conf.json`
- Create: `desktop/src-tauri/capabilities/default.json`
- Create: `desktop/src-tauri/src/main.rs`
- Create: `desktop/src-tauri/src/lib.rs`
- Create: Tauri-generated files under `desktop/src-tauri/icons/`

### Step 1: Add configuration-only scaffolding

Use current stable Tauri 2 packages, not the Tauri 3 alpha. Pin direct JavaScript dependencies in `package-lock.json`; use compatible Cargo `2` requirements resolved into the desktop lockfile.

Required scripts:

```json
{
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "test": "vitest run",
    "tauri": "tauri"
  }
}
```

The Tauri crate is named `sparc-desktop`, uses `tauri = "2"`, `tauri-build = "2"`, `tauri-plugin-dialog = "2"`, `serde`, and `sparc = { path = "../.." }`.

Configure Vite on a fixed local port with `clearScreen: false`, `strictPort: true`, and `src-tauri` excluded from frontend watching. Configure `frontendDist: "../dist"` and the matching dev URL.

Configure one `main` window, product name `SPARC`, bundle identifier `com.burggraf.sparc`, and a restrictive local CSP. The sole capability is scoped to `main` and contains only `core:default`, `dialog:allow-open`, and `dialog:allow-save`. Register `tauri_plugin_dialog`; do not register shell or filesystem plugins.

Add `/desktop/node_modules/`, `/desktop/dist/`, and `/desktop/src-tauri/target/` to `.gitignore`.

### Step 2: Install exactly the needed frontend dependencies

Install React/Tauri runtime dependencies and TypeScript/Vite/Vitest/Testing Library development dependencies. Initialize Tailwind CSS 4 through its Vite plugin. Add only the shadcn/ui Button, Card, Alert, Badge and Separator source components and their actual generated dependencies. Do not add a router, form package, state store or animation package beyond what those components require.

### Step 3: Verify the empty shell builds

```sh
cd desktop
npm test
npm run build
cargo check --manifest-path src-tauri/Cargo.toml --locked
```

Expected: zero tests are acceptable only for this configuration scaffold; TypeScript/Vite and the Tauri crate compile without warnings.

### Step 4: Commit

```sh
git add .gitignore desktop
git commit -m "build: scaffold Tauri desktop app"
```

## Task 3: Implement the tested Rust desktop command layer

**Files:**
- Create: `desktop/src-tauri/src/archive.rs`
- Modify: `desktop/src-tauri/src/lib.rs`

### Step 1: Write failing Rust tests first

Inside `archive.rs`, define tests against the wished-for pure functions and state:

```rust
#[test]
fn creates_verifies_and_restores_without_the_source() {
    // Create a synthetic source with text, binary bytes and an empty directory.
    // Call create_archive, remove source, call verify_archive and restore_archive.
    // Assert summary counts and byte-identical output.
}

#[test]
fn refuses_existing_outputs_without_changing_them() {
    // Existing key/archive/restore paths return destination_exists.
}

#[test]
fn wrong_key_and_corruption_are_safe_errors() {
    // Verify with another key and a truncated payload.
    // Assert code only; serialized error must contain no key contents.
}

#[test]
fn operation_state_refuses_overlap() {
    let state = OperationState::default();
    let first = state.start().unwrap();
    assert_eq!(state.start().unwrap_err().code, "busy");
    drop(first);
    assert!(state.start().is_ok());
}
```

Define the expected public data shapes in the tests:

```rust
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ArchiveSummary {
    pub files: usize,
    pub directories: usize,
    pub plaintext_bytes: u64,
    pub ciphertext_files: usize,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct UiError {
    pub code: &'static str,
    pub message: &'static str,
    pub outputs_may_remain: Vec<&'static str>,
}
```

### Step 2: Observe RED

```sh
cargo test --manifest-path desktop/src-tauri/Cargo.toml --locked archive::tests
```

Expected: compilation fails because the command functions/state do not exist.

### Step 3: Implement minimal synchronous operations

Implement pure path-based functions:

```rust
pub fn create_archive(source: &Path, archive: &Path, identity: &Path) -> Result<ArchiveSummary, UiError>;
pub fn verify_archive(archive: &Path, identity: &Path) -> Result<ArchiveSummary, UiError>;
pub fn restore_archive(archive: &Path, identity: &Path, destination: &Path) -> Result<ArchiveSummary, UiError>;
```

- Preflight new outputs with `symlink_metadata` so existing paths map to `destination_exists`.
- Creation calls `sparc::create_identity_file`, `sparc::pack`, reloads the identity, then `sparc::verify`.
- Existing-archive verification maps all decrypt/authentication/format failures to `wrong_key_or_corrupt_archive` without forwarding internal text.
- Restore verifies first, then calls `sparc::unpack` into a new destination.
- `ciphertext_files` equals payload count plus the encrypted manifest.
- Errors have fixed public messages. Do not parse or serialize underlying error strings.
- `OperationState` is an `Arc<AtomicBool>` with an RAII guard so panic/error paths release the busy flag.

### Step 4: Add narrow async Tauri wrappers

Register three commands with `tauri::generate_handler!`. Each wrapper acquires the shared operation guard, moves owned paths and the guard into `tauri::async_runtime::spawn_blocking`, and maps join failure to `internal_failure`. JavaScript never receives the recovery identity value.

### Step 5: Run GREEN and Clippy

```sh
cargo test --manifest-path desktop/src-tauri/Cargo.toml --locked
cargo clippy --manifest-path desktop/src-tauri/Cargo.toml --locked --all-targets -- -D warnings
```

Expected: all desktop Rust tests pass; Clippy reports no warnings.

### Step 6: Commit

```sh
git add desktop/src-tauri
git commit -m "feat: expose safe desktop archive commands"
```

## Task 4: Build the home screen and navigation with frontend TDD

**Files:**
- Create: `desktop/src/App.test.tsx`
- Create: `desktop/src/App.tsx`
- Create: `desktop/src/lib/tauri.ts`
- Create: `desktop/src/lib/utils.ts`
- Create: selected files under `desktop/src/components/ui/`
- Modify: `desktop/src/main.tsx`
- Modify: `desktop/src/index.css`

### Step 1: Write the failing home-screen test

Test that the initial screen contains:

- the SPARC heading;
- the exact local-only scope notice;
- Create archive and Open archive task buttons;
- no Supabase-connect action;
- clicking each task reveals its heading and a Back action.

Use accessible roles and names rather than CSS selectors.

### Step 2: Observe RED

```sh
cd desktop
npm test -- --run src/App.test.tsx
```

Expected: failure because `App` and the task UI do not exist.

### Step 3: Implement the minimal shell

Create a single `App` component using local state (`home | create | open`). Use only the selected shadcn primitives and semantic HTML. Add the persistent scope alert, two task cards and back navigation. No router or shared state package.

### Step 4: Run GREEN and production build

```sh
npm test -- --run src/App.test.tsx
npm run build
```

Expected: test and build pass.

### Step 5: Commit

```sh
git add desktop/src desktop/package.json desktop/package-lock.json
git commit -m "feat: add desktop archive task shell"
```

## Task 5: Implement the Create archive flow with frontend TDD

**Files:**
- Modify: `desktop/src/App.test.tsx`
- Modify: `desktop/src/App.tsx`
- Modify: `desktop/src/lib/tauri.ts`

### Step 1: Write failing interaction tests

Mock only the two external boundaries: Tauri native dialogs and the typed `invoke` bridge. Test:

1. Create stays disabled until source, key and archive paths are selected.
2. The unencrypted-key warning is visible before start.
3. Starting creation disables all selection/start controls and announces `Creating encrypted archive`.
4. The invocation sends only `{ source, archive, identity }` paths.
5. Success focuses a summary showing files, directories, plaintext bytes and ciphertext files, plus the archive location and a separate-key reminder.
6. A structured safe failure receives focus, displays its message and lists possible remaining outputs without rendering a raw error chain.
7. Returning home clears all selected paths and results.

### Step 2: Observe RED

```sh
npm test -- --run src/App.test.tsx
```

Expected: the new Create-flow assertions fail against the shell.

### Step 3: Implement minimal behavior

Use Tauri dialog `open({ directory: true, multiple: false })` for the source and `save(...)` for new key/archive paths. Use `.agekey` as the suggested key suffix. Store only current-session values in component state. Call the typed bridge and update an `aria-live` status region through selecting, creating, verifying and complete wording without inventing byte progress.

Do not print to the browser console or use `localStorage`, `sessionStorage` or IndexedDB.

### Step 4: Run GREEN

```sh
npm test -- --run src/App.test.tsx
npm run build
```

Expected: all frontend tests and build pass.

### Step 5: Commit

```sh
git add desktop/src
git commit -m "feat: add guided archive creation flow"
```

## Task 6: Implement Verify and Restore with frontend TDD

**Files:**
- Modify: `desktop/src/App.test.tsx`
- Modify: `desktop/src/App.tsx`
- Modify: `desktop/src/lib/tauri.ts`

### Step 1: Write failing interaction tests

Test:

1. Verify requires an archive folder and recovery-key file.
2. Restore controls do not exist or remain disabled until verification succeeds.
3. Verification displays bounded archive counts and the local-only limitation.
4. Choosing a new destination and restoring sends only archive, identity and destination paths.
5. Busy state disables all controls and announces the active phase.
6. Restore completion says `Local files restored` and never `Supabase restored`.
7. Changing archive or key selection invalidates prior verification.
8. Wrong-key/corruption and destination-exists errors display safe next actions and receive focus.

### Step 2: Observe RED

```sh
npm test -- --run src/App.test.tsx
```

Expected: the new Open-flow assertions fail.

### Step 3: Implement minimal behavior

Use native open dialogs for the archive directory and identity file, then the save dialog for a new destination-folder path. Call `verifyArchive` before rendering the restore section. Invalidate verification whenever either input path changes. Keep one operation in flight and clear task state on Back.

### Step 4: Run GREEN and accessibility-oriented checks

```sh
npm test -- --run src/App.test.tsx
npm run build
```

Expected: all tests pass with no React act warnings or accessibility-query failures.

### Step 5: Commit

```sh
git add desktop/src
git commit -m "feat: add guided archive verification and restore"
```

## Task 7: Build the macOS app and document testing

**Files:**
- Modify: `README.md`
- Modify: `docs/archive-prototype.md`
- Modify: `docs/plans/2026-09-18-local-desktop-alpha-design.md` only to append observed results
- Modify: this plan only to append execution results

### Step 1: Write documentation assertions before prose

Add a small repository check script invocation (inline Python is sufficient) that initially fails unless documentation contains all of:

- `desktop/` development command;
- ad-hoc-signed local `.app` build command and output location;
- local-artifacts-only warning;
- unencrypted recovery-key warning;
- no Supabase export/restore claim;
- no Developer ID signing/notarization claim.

Run it before editing docs and observe failure.

### Step 2: Update documentation minimally

Document:

```sh
cd desktop
npm ci
npm run tauri dev
npm run tauri build -- --bundles app
```

State the exact generated `.app` location observed on this machine. Explain macOS ad-hoc-signed app behavior without suggesting users bypass system security for an untrusted download. Keep the CLI instructions.

### Step 3: Run full fresh verification

From the repository root:

```sh
cargo fmt --check
cargo build --locked --offline
cargo test --locked --offline
cargo test --locked --offline --test age_interop -- --ignored
cargo clippy --locked --offline --all-targets -- -D warnings
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -p 'hosted_rehearsal_test.py' -v
cd desktop
npm ci
npm test
npm run build
cargo fmt --manifest-path src-tauri/Cargo.toml --check
cargo test --manifest-path src-tauri/Cargo.toml --locked
cargo clippy --manifest-path src-tauri/Cargo.toml --locked --all-targets -- -D warnings
npm run tauri build -- --bundles app
```

Return to the repository root and run `git diff --check`. Inspect the complete diff and ensure generated archives, keys, `node_modules`, `dist` and Rust targets are not tracked.

### Step 4: Perform integration smoke evidence

Use the desktop Rust command tests for the real encrypted create/verify/remove-source/restore path and the frontend tests for the renderer workflow. Launch the produced `.app` binary and verify it remains running without startup failure, then stop it cleanly. Because this harness cannot automate a Tauri WKWebView, record interactive clicking as **owner smoke test pending** rather than claiming it passed. Provide the `.app` path and a five-step owner test checklist.

### Step 5: Commit and publish only after verification

```sh
git add README.md docs .gitignore Cargo.lock src tests desktop
git status --short
git commit -m "feat: add local desktop archive alpha"
```

Push through the existing authenticated HTTPS fallback without changing `origin`, refresh `origin/main`, and verify local/tracking/GitHub commit equality. Do not mark the alpha fully accepted until the owner reports the interactive create/open flow result.

## Final acceptance checklist

- [x] Every behavioral test was observed failing for the expected reason before implementation.
- [x] The desktop invokes the root Rust library directly.
- [x] Create performs keygen, pack and independent verify.
- [x] Open requires verify before restore.
- [x] Existing destinations are never overwritten.
- [x] Errors are structured, bounded and secret-free.
- [x] No generic shell/filesystem power is granted to the renderer.
- [x] No frontend persistence or telemetry exists.
- [x] Root and desktop checks pass from clean installs/locks.
- [x] An ad-hoc-signed local `.app` with no Developer ID signature or notarization ticket is produced.
- [x] Automated integration evidence passes.
- [x] User-authorized interactive smoke result is recorded separately.
- [x] Supabase/full-backup/scale exclusions remain prominent.

## Observed execution result — 2026-09-19

Expected RED states were observed before implementation for shared identity APIs, the desktop task shell, Create controls, Open/Verify/Restore controls, and the local ad-hoc packaging contract. The implemented desktop suite now has four archive-command tests and one configuration/capability contract test. The frontend suite has nine workflow tests.

The real command-layer integration test creates a recovery identity and encrypted archive, verifies it independently, removes the source, restores to a new destination, and compares recovered bytes. Separate tests cover existing outputs, wrong keys, ciphertext corruption, and overlapping operations. The built app executable also remained running during a startup smoke check and stopped cleanly.

The final bundle is `desktop/src-tauri/target/release/bundle/macos/SPARC.app`. Its code signature is ad-hoc, has no Team ID, and has no stapled notarization ticket. One earlier local attempt inherited installed Developer ID/notarization credentials and was automatically signed and notarized by Tauri; it was not published. The checked-in configuration now forces ad-hoc signing, and acceptance builds explicitly remove Apple signing/notarization environment variables.

Automated browser rendering was unavailable because the installed browser wrapper was below its supported version and Chrome's remote-debugging connection required interactive approval. No global browser tooling was changed. The user then authorized direct interaction with the already-running Tauri window. Using its actual macOS dialogs, the smoke flow selected the synthetic source, saved a recovery key and archive, created and verified the archive, reopened and verified it, and restored to a new destination. The UI reported 2 files, 1 directory, 46 plaintext bytes, and 3 encrypted files. An independent recursive diff confirmed the restored files match the source.
