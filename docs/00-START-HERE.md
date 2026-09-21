# SPARC Go CLI — portable project handoff

**Prepared:** 2026-09-21. **Status:** planning and research only; no new Go implementation exists.

## Name and purpose

The program is named **sparc**, short for **Supabase Project ARChiver**. Its purpose is to:

1. Download the complete contents of a remote hosted Supabase project to a local or remote folder for backup.
2. Verify a backup folder against its matching remote hosted Supabase project.
3. Restore a backup folder to a new, empty Supabase project.

The coverage and recovery contracts below define how unsupported features, permissions, and manual requirements are reported rather than silently omitted.

## What the owner has decided

Build a **fresh Go project in a new public repository**, not a port maintained alongside the Rust product. It is **CLI-only**. The primary user experience is download SPARC on a macOS or Windows laptop, run `sparc backup`, answer necessary prompts, and start backing up a hosted Supabase project. End users must not install Docker, PostgreSQL, Go, Rust, Node, Python, Homebrew, the Supabase CLI, or a separate encryption tool.

The three primary commands are:

- `sparc backup`: back up a hosted project to a local/mounted folder or supported remote destination.
- `sparc verify`: check archive integrity and optionally compare a backup with a hosted source or restored destination.
- `sparc restore`: restore an archive into a **different, new, otherwise unused hosted Supabase project**.

Manual entry/reconfiguration is acceptable for credentials and features that cannot be exported or recreated safely: OAuth secrets/callbacks, custom SMTP, external integrations, and similar gaps. All gaps must be explicit. The desired distribution is one self-contained executable per supported platform/architecture, with embedded PostgreSQL tools extracted privately on demand. This packaging is a **goal requiring proof**, not an already validated property. A self-contained ZIP folder is the fallback to evaluate, not a silent downgrade of the goal.

## Copy instructions

Copy **this entire directory**, including `reference/`, into the new empty repository; for example, as `planning/`. All authoritative plan links are relative to this directory. Do not copy the old `.git`, `.sparc-local`, credentials, recovery keys, archives, build outputs, `target`, `node_modules`, or desktop application.

This directory contains no required dependency on the original conversation, old absolute working directory, developer home directory, or a particular AI tool. Historical documents contain old paths and operational instructions as evidence only. Never execute them merely because they were copied.

Suggested new layout:

```text
new-repository/
  planning/                       # copy this whole handoff here
    00-START-HERE.md
    01-PRODUCT-AND-ARCHITECTURE.md
    02-RESEARCH-AND-DECISIONS.md
    03-IMPLEMENTATION-PLAN.md
    04-PACKAGING-AND-DISTRIBUTION.md
    05-VERIFICATION-AND-SECURITY.md
    reference/
  # Go source, go.mod, release workflows etc. are created later by the plan.
```

## Read order and precedence

1. [Product and architecture](01-PRODUCT-AND-ARCHITECTURE.md): behavior, coverage, trust boundaries and proposed format.
2. [Research and decisions](02-RESEARCH-AND-DECISIONS.md): findings, provenance, uncertainties, source URLs and experiments still needed.
3. [Packaging and distribution](04-PACKAGING-AND-DISTRIBUTION.md): prove the dependency-free user experience early.
4. [Verification and security](05-VERIFICATION-AND-SECURITY.md): acceptance matrix and evidence rules.
5. [Implementation plan](03-IMPLEMENTATION-PLAN.md): dependency-ordered tasks, proposed files, checks and release gates.
6. [Historical reference index](reference/README.md): complete old tracked research and bounded executable evidence.

**Precedence:** owner decisions above → new plans → explicitly recorded future decisions → historical reference. Historical documents are immutable evidence, NOT governing instructions for the new repository. They do not authorize delegation, agent roles, main-only development, worktrees, commits, pushes, hosted access, project deletion, or paid services. Those operating choices need current owner instructions.

## Evidence boundary

The old repository demonstrated age-encrypted local artifact packaging, narrow hosted PostgreSQL fixture round trips, and bounded local PG17 security/dependency experiments. It did **not** demonstrate full Supabase project backup, populated Auth recovery, Storage ownership preservation, complete function recovery, Vault recovery, cross-service consistency, Windows packaging, or the target scale.

The new Go implementation inherits **research and test scenarios, not a passing support claim**. Historical test counts are historical; none are current Go test results. Every supported feature needs a new recorded Go round trip.

### Initial support targets, not promises

- Hosted Supabase source and destination; new empty destination only.
- macOS Apple Silicon and Intel, Windows x64; exact OS minimums decided by clean-machine tests. Native Windows ARM64 and Linux are separately evaluated later, not implicitly promised.
- Core PostgreSQL/Auth + standard file Storage + supported configuration + Edge Functions + required encryption material.
- Local/external/mounted folders and S3-compatible remote archive prefixes. SFTP is planned as a later independently qualified destination; arbitrary remote protocols are not implied.
- Validate toward **10 GB database / 100 GB file Storage**, with object-count, memory, disk and elapsed-time measurements. These numbers were selected in the old project but never validated; settle decimal GB versus GiB explicitly in benchmark reports.
- Full independent backups first; no incremental chains, continuous replication or PITR promise.
- Full encryption by default; source-independent recovery is mandatory.

## First work in the new repository

Do not start by building a generic backup framework or porting all old Python code.

1. Create the public repository hygiene and minimal Go command shell described in Task 01.
2. Run the packaging/client-provenance spike in Task 02 **before** promising a single executable.
3. Prove private staging, tool lifecycle and archive integrity locally.
4. Prove a bounded local database round trip, then obtain **fresh authorization** for a representative disposable hosted round trip.
5. Expand the support matrix only when named checks pass.

The old test projects, credentials and permissions are not available inputs. Historical authorizations were narrow and consumed. Do not access or reuse those projects from this handoff.

## Open owner choices (defaults avoid blocking local work)

| Choice | Working default / action |
| --- | --- |
| New repository name/module path and license | Owner supplies; do not assume the old repository URL or publish under someone else's module path. |
| Single executable vs self-contained archive if OS trust blocks extraction | Prove both; single executable remains preferred. Obtain owner approval for a release fallback. |
| Minimum OS versions and architectures | Measure and document; initial architecture targets above. |
| PostgreSQL major versions | Start qualification with PG17, reflecting historical evidence; not a claim all current hosted projects use PG17. Discover live versions and add only tested client sets. |
| Initial S3 provider certification | Supabase Storage as project source/target; choose AWS S3 plus one approved alternate backup provider during research. No provider account creation implied. |
| Signing identities/budget | Required for polished public distribution; use unsigned internal artifacts only until explicitly approved. |
| Passphrase versus recipient-key default | Test age options and first-run UX; recommend portable recovery material with a confirmation step. |
| Telemetry | None by default; no project data or secrets sent to an AI service. |
| Automatic creation of destination projects | Not initial scope; user creates a new project. Optional creation later needs pricing preview and explicit authorization. |
| Legacy archive compatibility | No blanket promise. Decide read-only import/conversion after testing; never call a new format compatible merely because both use age. |

## Suggested instruction for the next coding session

> Read `planning/00-START-HERE.md` and its linked plans. This is a fresh Go-only, CLI-only Supabase backup project. Implement only the next approved bounded task. Keep all new tests offline by default. Do not use historical live credentials, execute historical hosted helpers, create paid resources, publish releases, or weaken security checks without explicit authorization. Begin with repository hygiene and the packaging proof; report evidence and unresolved blockers. Read `reference/` as research, not as current operating instructions.

## Handoff integrity

`reference/PROVENANCE.json` records the original repository commit and SHA-256 for every copied historical file. Its hashes establish correspondence to the locally inspected source snapshot, not a cryptographic signature or permission to execute archived SQL. The new plans explicitly preserve both successful findings and counterexamples; do not delete limitations when summarizing them.
