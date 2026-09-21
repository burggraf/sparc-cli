# SPARC Go CLI — packaging and distribution plan

**Date:** 2026-09-21. **Status:** required product work, not a solved build recipe. See research R01/R02/R17 in [the ledger](02-RESEARCH-AND-DECISIONS.md).

## 1. End-user contract

A supported user downloads an authentic SPARC release, extracts it if the transport is a ZIP, opens a terminal and runs:

```sh
# macOS, from the downloaded/extracted directory
./sparc backup
```

```powershell
# Windows PowerShell, from the downloaded/extracted directory
.\sparc.exe backup
```

The program prompts for project/access/destination/encryption choices and performs the backup. No compiler, language runtime, PostgreSQL server/client installation, Docker, Homebrew, Node, Python, Deno, Supabase CLI, age CLI or C++ runtime installer is required. If a feature ultimately requires a helper, it must be qualified and shipped or the feature must be explicitly unsupported; never spring a dependency installer on the user.

Plain `sparc backup` from any directory requires PATH configuration. Offer an optional user-scoped install/PATH helper only after the portable flow works, with explicit approval before changing shell profiles or Windows environment values. Terminal use and obtaining project credentials remain unavoidable; dependency-free does not mean credential-free.

No administrator/root privileges for the portable baseline. Do not silently request elevation, disable security software, strip quarantine, relax execution policy, change firewall/TLS settings, or install system services. Do not require Rosetta for the supported Apple Silicon build.

### What self-contained means

All executable code needed for supported operations is in the release. Contacting Supabase and the chosen backup destination is expected; fetching PostgreSQL or a language runtime on first use is **not** self-contained. The CLI may create private support/cache/data files. There is no promise of zero disk writes or a portable executable that never extracts helpers.

App first-run dependency extraction must work with outbound Internet blocked, before any actual Supabase operation. Certificate/notarization validation may have separate OS network behavior; document/test that distinction rather than claiming fully offline macOS trust evaluation without evidence.

## 2. Distribution options and early proof

### Preferred: one executable containing signed helper payloads

Embed a per-platform payload with the qualified `pg_dump`, `pg_restore`, optional `psql`, and every non-system dependency required by those clients. Use Go `embed` for resources. Extract into a private versioned directory, verify contents, invoke exact absolute paths, and do not mutate PATH globally.

Benefits: user keeps one file, no missing adjacent tools, one update download.

Costs/questions to prove:

- Apple notarization may not inspect compressed embedded executable content. Determine how each helper/library must be signed/notarized before embedding and how its trust survives extraction. Signing the outer Go binary is not enough evidence.
- Windows may scrutinize self-extracting/child-executing binaries; all executable payloads need provenance and an appropriate signing/trust strategy. SmartScreen reputation is not guaranteed.
- Larger downloads when multiple PG majors/architectures are included; extra disk for embedded + extracted payloads.
- Cache permissions, races, interrupted extraction, malicious replacement, locked files, quarantine/MOTW and executable policy.

### Fallback: self-contained ZIP with adjacent `tools/`

One download containing `sparc`/`sparc.exe`, all client tools/dependencies and notices. Nothing else is installed; tools remain next to the executable. This is simpler to inspect and notarize but users must preserve the directory. Test this as a control in R01. If the single-file path cannot meet OS trust/security requirements, present this evidence and obtain owner approval before making it the release default.

### Optional later conveniences

Signed per-user installer, `.pkg` where appropriate, Winget/Homebrew/Scoop, Microsoft Store distribution, shell completion and PATH setup. These must remain optional; package-manager availability is not the product's primary success criterion. No automatic self-updater initially; manual replacement is sufficient and avoids a second privileged downloader/executor.

## 3. Proposed platform matrix

| Target | Initial plan | Qualification requirement |
| --- | --- | --- |
| macOS arm64 | Required | Native Go core AND native PostgreSQL helpers/libraries, minimum macOS version, signed/notarized browser download on standard-user clean host. |
| macOS amd64 | Required | Native Intel test host/VM on supported OS; no assumptions based on cross-compile success. |
| macOS universal | Optional consolidation | If used, all helper dependencies must run on both CPUs. Building a fat Go executable alone is insufficient; compare doubled payload/download cost. Separate downloads are acceptable initially. |
| Windows amd64 | Required | Supported Windows versions chosen explicitly, standard-user clean VM, no Visual Studio/PG/Go/VC redistributable preinstallation dependence. |
| Windows arm64 | Research/later | Qualify native client availability or explicitly supported emulation; do not label x64-only tests native ARM support. |
| Linux | Later/CI convenience | Not a substitute for macOS/Windows acceptance; release support is a separate decision. |

Do not freeze an OS minimum until Go, PostgreSQL, TLS/compression libraries and signing tools' minimums are checked together. Publish the intersection and test it. Avoid "works on Windows" when only one developer machine was exercised.

## 4. PostgreSQL client provenance and dependency closure

Select either reproducible builds from official PostgreSQL source or a trusted redistributable vendor artifact with clear license/build provenance. Homebrew-installed clients are useful developer research inputs, not portable release payloads. Audit current security patches before pinning.

Create `build/clients/manifest.json` (schema versioned) with:

- Platform/CPU/minimum OS, PostgreSQL version and supported source/target matrix.
- Upstream URL/tag/source digest, build recipe/toolchain, compiler flags and provenance reference.
- Every shipped relative file, byte length, SHA-256, purpose, architecture and mode.
- Dependency relationships, TLS/compression/runtime versions, license/notice paths.
- Signing identity/status and post-signing digests (signing changes bytes).
- Package ID derived from final payload contents; no real credentials/endpoints.

Investigate `pg_dump`, `pg_restore`, `psql` and possibly a constrained role-export need; do not include a server/initdb/pg_ctl in end-user payloads unless separately justified. Test harnesses may need servers in developer CI only. Do not add `pg_dumpall` merely by habit; global roles/password privileges differ on hosted Supabase.

### macOS dependency audit

Inspect Mach-O architectures and linked libraries (`file`, `otool -L`, codesign inspection in release CI). Remove Homebrew/Cellar/build-machine absolute load paths via a controlled reproducible build/relocation recipe. Use approved relative loader paths where needed. Apply relocations before signing. Avoid broad `DYLD_*` environment overrides and unsafe writable search paths. Determine TLS roots and compression dependencies explicitly.

### Windows dependency audit

Inspect PE imports using trusted CI tooling, recursively include needed app-local DLLs or use an appropriate build linkage strategy. Check libpq, SSL/crypto, zlib/lz4/zstd and compiler runtime dependencies as applicable; do not assume their exact names or presence without inspecting the pinned build. Ensure safe DLL resolution does not search attacker-controlled current directories/PATH before the intended payload. No prerequisite VC++ redistributable installer. Test actual connection, TLS, dump and restore: `--version` cannot exercise all delay-loaded paths.

### Versions and offline completeness

One release may carry multiple **qualified** client majors, or explicitly support a limited major set. Runtime selects only from bundled signed inventory. An unsupported project gets an actionable compatibility refusal and instructions to obtain a newer complete SPARC release—not an insecure fallback or automatic download of a random pg_dump. Document future ability to read older archive formats and which restore client is selected for each dump/server combination.

## 5. Safe extraction and helper execution

Implement in small platform-specific files; don't build a general package manager.

1. Determine app support directory in user-owned native location (for example a SPARC subdirectory under Application Support / LocalAppData; settle exact paths in tests). Distinguish disposable tool cache from persistent credentials and operation journals.
2. Check trusted ownership/ACLs and reject unsafe symlink/reparse-point redirection or attacker-writable roots. POSIX `0700`/`0600` is not an adequate Windows DACL implementation.
3. Use process coordination to prevent concurrent extraction/use of a partial payload. Prefer exclusive creation/OS lock behavior; stale locks must not cause unsafe deletion.
4. Extract into a new private staging directory, with strict entry allowlist, bounded sizes/counts, no traversal/absolute paths/symlinks/device entries or decompression bombs.
5. Verify every file against the embedded final manifest, apply exact executable modes/native ACLs, validate architecture/signature as required, and only then publish the completed package directory atomically where supported.
6. Store a completion receipt bound to package digest. Receipt alone is not sufficient if files can be replaced. Define and test revalidation before execution and the remaining same-user attack boundary.
7. Invoke exact absolute executables with argument arrays and sanitized environment, private cwd and scoped credentials. Avoid ambient executable/DLL/plugin/rc resolution.
8. Clean only SPARC-owned incomplete extraction directories. Never recursively delete arbitrary user-selected paths. Active tools/Windows locks postpone cleanup; retain the previous valid package until no operation uses it.
9. On failure, report a bounded actionable category, preserve no executable partial package as ready, and do not run unverified contents.

An embedded hash is trustworthy only to the extent the outer distribution is authentic. It detects cache corruption/tampering relative to that distribution, not replacement of both executable and manifest. Use OS code signatures plus release provenance; same-user malicious code is not fully sandboxed by directory permissions.

Package extraction takes place without hosted credentials. Test simultaneous first runs, disk full, cache deletion, read-only parent, path spaces/Unicode, junction replacement, interruption and tamper. Do not put unencrypted project data in the helper cache.

## 6. macOS signing and notarization workstream

Owner needs an appropriate Apple Developer identity/budget. Keep signing credentials in protected release CI, never source or normal PR workflows.

Proposed sequence to qualify (not an asserted already-working command recipe):

1. Build/relocate each helper and library for intended CPU/minimum OS.
2. Sign each relevant code object with the right Developer ID identity, timestamp and required hardened-runtime settings/entitlements; minimize entitlements rather than disabling library validation indiscriminately.
3. Establish how helper payloads receive notarization assessment independently if embedded/compressed assets are not discovered in the outer submission. Test whether separate submission is required and how runtime Gatekeeper locates trust tickets.
4. Embed the **final signed** payload into the Go app, build the outer executable and sign it.
5. Submit a supported distribution container using the current `notarytool` workflow, wait for actual accepted result, inspect logs and fail release on issues. Do not rely on deprecated `altool` instructions.
6. Use ticket stapling only for formats Apple supports; bare executables/ZIPs do not share the same stapling workflow as `.app`/`.pkg`/`.dmg`. Research exact artifact choice and offline trust behavior rather than claiming all can be stapled.
7. Verify signatures/notarization and download the exact released artifact in a browser on a clean machine. Launch both outer CLI and extracted helpers under normal quarantine/Gatekeeper behavior.

A locally ad-hoc-signed binary or a copied file without quarantine is not public-distribution evidence. Do not make `xattr -d com.apple.quarantine` the installation guide or instruct users to disable Gatekeeper. A signed/notarized archive fallback may be preferable if single-file trust is brittle; document measured results.

## 7. Windows signing and reputation workstream

Use Authenticode signing with trusted identity, appropriate SHA-256 algorithms and trusted timestamps. Evaluate Microsoft Artifact Signing or an appropriate certificate provider with owner-approved cost/eligibility; never assume an account is available.

- Sign executable helpers/DLLs and outer `sparc.exe` as appropriate; preserve valid third-party signatures where required and record signer provenance. The trust model must work under Smart App Control as well as ordinary launches.
- Verify signatures after embedding/extraction and on the final distributed bytes. Do not modify signed files afterward.
- Test browser download/ZIP extraction with Mark-of-the-Web preserved, Windows Defender, SmartScreen and Smart App Control where applicable. Test both PowerShell and cmd execution.
- New signed executables can still show reputation warnings. Microsoft explicitly says EV certificates no longer automatically bypass SmartScreen. Do not promise warning-free first release or pay for EV solely on that assumption.
- Enterprise policy can disallow execution; provide signed publisher information and supported IT deployment options rather than bypass instructions.
- Use documented publisher verification guidance, false-positive reporting and safe support diagnostics. No packed/obfuscated executable or UPX by default; such compression can complicate trust/AV and debugging.

## 8. Release pipeline and public repository safety

Proposed files: `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `build/clients/`, `build/package/`, `docs/install.md`, `docs/releasing.md`, `THIRD_PARTY_NOTICES.md`, `SECURITY.md`. GoReleaser configuration only if chosen. Keep source/payload generation steps reviewable; do not commit downloaded binaries casually.

Pipeline stages:

1. Protected tagged source revision; clean tree, reviewed version/compatibility changes.
2. Pin Go toolchain, dependencies and Actions by reviewed versions/commit SHAs; fetch verified client source/artifacts from approved provenance only.
3. Offline unit/security tests; isolated local PG tests; platform-specific tool-extraction/ACL/process tests.
4. Build reproducible unsigned core/payload where possible; record compiler/flags/SBOM. Signing/timestamps mean final signed bytes may not be bit-for-bit reproducible; distinguish unsigned reproducibility from signed provenance.
5. Native packaging and signing in isolated protected jobs, no secrets for fork PRs and no signing untrusted pull-request code.
6. Notarization/signature checks and clean-machine smoke tests of exact distribution artifacts, including actual PG operations and zero dependency downloads.
7. Produce platform downloads, SHA-256 checksums over **final signed artifacts**, third-party notices, build provenance/SBOM, minimum OS/architecture/client support, release notes and recovery limitations.
8. Publish draft release; human approval before public upload. No registry/release publication implicitly authorized by local build success.

A checksum file served alongside a maliciously replaced download is not independent authenticity. Use OS signing plus authenticated release provenance/signatures appropriate to the project. Do not require end users to install verification tooling just to run the app; expose verification options for advanced users.

### CI test tiers

Ordinary PR jobs: no credentials, no public network beyond declared dependency fetches, no hosted project access. Local PG integration jobs own disposable clusters. Hosted matrix jobs are manually dispatched with fresh scoped credentials/explicit budget and protected environment; they must never run against production by default. Public logs contain only redacted evidence and synthetic identifiers.

Signing certificates, notary keys, service tokens and package-provider credentials have separate scope. Prevent uploading plaintext temp data, passfiles, private keys, crash dumps or real project archives as CI artifacts. Secret-scan tracked changes AND artifacts before publication.

## 9. Clean-machine release acceptance

For every advertised OS/CPU/minimum-version combination:

- Fresh standard-user account with no Go/Rust/PG/Docker/Homebrew/Node/Python/Deno/age installation; confirm no accidental PATH/developer dependency.
- Download in browser, preserve OS quarantine/MOTW; no trust bypass.
- Run help/version/doctor; first-run extraction with dependency-download network blocked.
- Execute bundled PG clients against an approved synthetic endpoint with valid TLS; reject wrong CA/hostname.
- Complete synthetic backup → offline verify → new-target restore → target verification using the exact release binary. Hosted tests need explicit authorization; local controlled services may supply packaging checks but not hosted support evidence.
- Demonstrate user-space permissions, no elevation, no global PATH/system changes, optional credential-store behavior and correct missing-input prompts.
- Test path spaces/non-ASCII, long Windows paths, non-system drive, Downloads/read-only directory behavior, low disk, concurrent runs, AV scan and operation cancellation.
- Inspect executable imports/runtime loading and outbound traffic: no hidden package/runtime download, analytics or arbitrary dependency fetch.
- Stop/restart mid-extraction and mid-transfer; no corrupted ready cache, no surviving untracked helper, clear private-state cleanup instructions.
- Test manual update, old-package cleanup, downgrade/archive compatibility refusal and uninstall instructions that do not delete archives/recovery material.

Publish results with exact artifact digest, OS/CPU, signing/notary status and exclusions. A cross-build success is not a release acceptance result.

## 10. Updates, support and costs

Start with manual replacement downloads. Archive format/recipe versions and readable historical formats are part of release policy. Never silently auto-upgrade pg tools mid-backup or retire the only client capable of restoring an existing supported archive.

Track security updates for Go, pgx, age, AWS SDK, PostgreSQL/libpq, TLS/compression libraries and signing toolchain. Maintain a process for rebuilding bundles, revoking compromised releases, publishing advisories and notifying users. Signing identity continuity and certificate renewal need an owner.

Budget items to decide: Apple Developer membership, Windows signing service/certificate, macOS/Windows runners or test machines, artifact bandwidth, any GoReleaser Pro usage, hosted synthetic projects and benchmark egress/storage. Do not hardcode temporary advertised prices; verify before approval. The old $0 fixture-test budget is historical, not authorization for new charges.

Support docs explain where helper cache, journals and credentials live, what can be safely removed, how to retain recovery keys and how to generate a redacted diagnostic report. Backups and keys must never be deleted as part of uninstall/cache cleanup.
