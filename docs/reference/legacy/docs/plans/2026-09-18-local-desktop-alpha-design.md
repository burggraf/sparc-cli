# Local Desktop Archive Alpha — Design

**Status:** Approved for implementation on September 18, 2026.

**Goal:** Provide a real, locally testable macOS Tauri application for the existing encrypted artifact archive engine. A non-developer can create and verify an archive, or verify and restore one locally, without using the command line.

**Product boundary:** This alpha packages local files only. It is not a Supabase exporter, Supabase restore tool, production backup product, or proof of the 10 GB database / 100 GB Storage target.

## Decisions

- Tauri 2 on macOS, with React, TypeScript, Vite, Tailwind CSS and a minimal set of shadcn/ui components.
- Local development launch plus an ad-hoc-signed, double-clickable `.app` build for this Mac, with no Developer ID signing or notarization.
- Two guided tasks rather than exposing four low-level CLI commands:
  - **Create archive:** choose a source, save a new recovery key, choose a new archive path, pack, then verify.
  - **Open archive:** choose an archive and recovery key, verify, then optionally restore to a new local folder.
- Phase-level status only: packing, verifying, restoring and complete. No byte progress, cancellation or resumption yet.
- No recent paths, history, browser storage, Keychain storage, analytics or telemetry.
- Work directly on `main`, without worktrees, as requested by the owner.

## Architecture

Add a self-contained `desktop/` project. Its frontend contains the React/Vite application and its Tauri crate depends on the existing root `sparc` crate by local path. The root library and CLI remain the single archive implementation; the desktop app does not execute or parse the CLI.

The Tauri backend exposes only narrow, typed operations:

- `create_archive(source, archive, identity)`
- `verify_archive(archive, identity)`
- `restore_archive(archive, identity, destination)`

The command implementation calls the root Rust library directly. Long operations run away from the UI thread. A small process-local guard permits one archive operation at a time.

Native dialogs select source folders, archive folders, recovery-key files and new output paths. The main window receives dialog access and the named archive commands only. Do not add a shell plugin, generic command runner, unrestricted filesystem plugin, sidecar, updater or application server.

The frontend uses a thin typed bridge around Tauri `invoke`. Rust returns bounded summary objects and safe error objects, never manifests, file contents, private-key contents or raw error chains.

This separate desktop project is preferred over converting the repository into a workspace because the stable root package does not need restructuring. It is preferred over a CLI sidecar because direct library calls avoid process arguments, output parsing and duplicate packaging.

## User flow

The home screen presents two equal cards and a persistent scope notice: **Local artifact archives only — this does not back up Supabase yet.**

### Create archive

1. Choose an existing source folder.
2. Choose a new recovery-key filename, defaulting to `.agekey`.
3. Choose a new archive-folder path.
4. Review a warning that the key is unencrypted, must be stored separately and cannot be recovered if lost.
5. Start creation.

The backend generates the key, packs the source and verifies the completed archive. The result reports file count, directory count, plaintext bytes, ciphertext file count and archive location. It does not display or log the private identity or decrypted manifest paths.

If an operation fails after creating the identity or a partial archive, the result identifies which selected outputs may remain. It does not claim secure erasure or complete cleanup. Retry requires new output paths because existing destinations are never overwritten.

### Open archive

1. Choose an existing archive folder.
2. Choose its recovery-key file.
3. Verify the archive.
4. Show verified counts and prototype limitations.
5. Optionally choose a new destination-folder path and restore locally.

Restore remains disabled until verification succeeds. Existing destinations are refused. Completion says **restored local files**, never **restored Supabase**.

Selections and results live only in current React memory. Relaunching starts clean.

## Interface and accessibility

Use one window and local React state, with no router or state-management package. Selecting a home card opens a focused task panel with a short step list, path rows, one primary action and a clear return action.

Use only the shadcn/ui components justified by the screen: Button, Card, Alert, Badge and Separator. Native semantic HTML covers headings, forms and status. Avoid dashboard chrome, animation frameworks and custom component abstractions.

The visual style is a calm utility: neutral macOS-compatible colors, strong hierarchy, generous spacing and readable status. Status uses text and icons as well as color. Controls have visible labels and focus indicators. Operation state uses `aria-live`; completion or failure receives focus.

During a backend operation, selection controls and start buttons are disabled. The user can still close the application after being warned that interruption may leave private partial output. Cancellation is omitted because the engine cannot yet guarantee safe cancellation semantics.

## Errors and security boundary

Map backend failures into a small, stable UI-safe taxonomy:

- `destination_exists`
- `invalid_input`
- `wrong_key_or_corrupt_archive`
- `filesystem_failure`
- `busy`
- `internal_failure`

Each response supplies an actionable public message. The renderer does not receive raw `anyhow` chains, key text, decrypted paths or subprocess diagnostics. Selected paths can be displayed back to the user but are not logged or persisted.

The existing archive rules remain authoritative: outputs must not exist, keys are unencrypted private files, archive contents are untrusted until fully verified, and restore is local only. Encryption authenticates ciphertext but not its sender.

No generic JavaScript filesystem API or shell execution is exposed. Tauri capabilities are scoped to the main window and required dialog operations. Keep a restrictive content security policy compatible with the local Vite bundle.

## Testing

Implementation is test-driven.

Desktop Rust tests use temporary directories and the real root archive engine to prove:

- create generates a key, packs and verifies an archive;
- the source can be removed before byte-identical restoration;
- existing key, archive and destination paths are refused;
- wrong keys and damaged archives fail safely;
- UI errors are bounded and sanitized;
- overlapping operations are rejected.

The React layer isolates Tauri calls behind a typed bridge. Focused Vitest and Testing Library checks cover:

- both task flows and required selections;
- disabled controls while busy;
- verification before restoration;
- safe errors and completion summaries;
- keyboard/focus behavior for results;
- the persistent local-only scope notice.

Do not add a browser E2E framework, visual snapshot suite or mocked duplicate of archive logic.

## Acceptance gate

1. Existing root formatting, tests, independent age interoperability and Clippy remain green.
2. Desktop Rust command tests pass.
3. Frontend tests, strict TypeScript checking and Vite production build pass.
4. Tauri produces an ad-hoc-signed local macOS `.app` with no Developer ID signature or notarization ticket.
5. A smoke run through the app creates and verifies a synthetic archive, removes the source, restores it and confirms matching bytes.
6. No private key appears in the UI, captured output, frontend storage or logs.
7. Git contains no generated archives, recovery identities, build output or private fixtures.

## Deferred work

- Live Supabase discovery, export or restore.
- Auth, Storage, functions, settings, secrets and coverage reports for real projects.
- Detailed transfer progress, cancellation, checkpoints and resumption.
- Existing-key reuse, passphrase identities and Keychain integration.
- History, recent archives and persistent application state.
- Developer ID signing, notarization, DMG packaging, auto-update and cross-architecture builds.
- Release-quality branding and visual polish.
- Scale validation.

## Documentation checked

The implementation should follow current official guidance checked during design:

- [Tauri 2 with Vite](https://v2.tauri.app/start/frontend/vite/)
- [Tauri dialog plugin](https://v2.tauri.app/plugin/dialog/)
- [Tauri capabilities](https://v2.tauri.app/security/capabilities/)
- [Tauri macOS application bundles](https://v2.tauri.app/distribute/macos-application-bundle/)
- [shadcn/ui Vite installation](https://ui.shadcn.com/docs/installation/vite)
- [Vite documentation](https://vite.dev/)

## Observed implementation result — 2026-09-19

The alpha is implemented as designed. The Tauri shell calls the shared Rust library directly through three bounded commands. Automated command tests exercise a real create, independent verify, source removal, and restore round trip. The React tests cover both guided flows, verification invalidation, busy states, safe errors, and the local-only wording.

The generated arm64 bundle is at `desktop/src-tauri/target/release/bundle/macos/SPARC.app`. It is ad-hoc signed (`Signature=adhoc`, no Team ID) and has no stapled notarization ticket. A durable configuration test locks the main-window capability set to core plus native open/save dialogs and locks local packaging to ad-hoc signing.

One initial packaging attempt inherited Developer ID and notarization credentials from the local environment and Tauri automatically signed, submitted, and stapled that build. No artifact was published. The repository now fails closed with `bundle.macOS.signingIdentity` set to `-`, and acceptance builds remove Apple signing/notarization environment variables. The replacement bundle was confirmed ad-hoc and unstapled.

The built executable remained running during an automated startup smoke check and was then stopped cleanly. On 2026-09-19, the user authorized direct interaction with the running Tauri window. The actual macOS dialogs selected the synthetic source, saved a new key and archive, created and verified the archive, reopened and verified it, and restored to a new location. The UI reported 2 files, 1 directory, 46 plaintext bytes, and 3 encrypted files; an independent recursive diff confirmed the restored bytes match the source. All Supabase export/restore, full-project recovery, signing for distribution, scale, cancellation, and deferred service coverage exclusions remain unchanged.
