# Database capture route: vetted decision

Status: decision review completed; only a bounded local evaluation is justified. No general capture/restore implementation, desktop integration, installation, or hosted mutation was performed by this review.

## Decision

Keep the existing native/pinned-script route as fixed-fixture regression evidence. Do not generalize its schema selection or SQL transformations. Do not adopt unmodified Supabase CLI 2.117.0 under SPARC's current verified-TLS requirements. Evaluate direct native PostgreSQL custom-format capture/restore next, with a deliberately limited application-database contract and explicit Supabase compatibility checks.

This selects the next experiment, not a production-ready recovery engine. If the contract cannot preserve permissions, dependencies and provider boundaries without weakening safeguards, the experiment fails. There is no automatic fallback to a permissive dump or destructive retry.

Two independent Astra reviews covered security/runtime behavior and recovery semantics. Both reached this bounded recommendation. The parent inspected source and independently reproduced four relevant behaviors locally using synthetic inputs. No database connection was made during these new checks.

## Compared routes

| Route | Benefit | Blocking limitation | Disposition |
| --- | --- | --- | --- |
| A: Unmodified Supabase CLI container dump + psql | Upstream Supabase filtering and SQL transformations; packaged PostgreSQL tools | Required TLS/CA settings are lost at the inspected dump-container boundary; adds daemon/image/configuration dependencies | Do not adopt this pinned route under current requirements |
| B: Exact pinned upstream scripts + native PG17 | Existing fixture-specific recovery evidence; explicit native TLS/passfile boundary; no Docker | Fixed identifiers, shell argument splitting, SQL rewriting, separate snapshots, and incomplete pipeline timeout cleanup | Preserve regression evidence; not general application support |
| C: Direct native pg_dump custom archive + pg_restore | Direct argument arrays, no Bash/sed rewrite pipeline, one schema-and-data invocation, inspectable archive contents | No automatic Supabase compatibility policy, dependency closure, global-role capture or permission equivalence | Preferred next local experiment only |

C is not inherently safe because it is native or uses custom format. Its advantage is a smaller execution boundary that can be tested explicitly. Upstream scripts remain valuable specifications and regression references, not a reason to copy every transformation.

## Source and local evidence

Pinned CLI source: version 2.117.0, commit `21db855916f2c2b12f61cde923a27094b8528b23`, independently confirmed locally. Native `pg_dump` and `pg_restore` both report PostgreSQL 17.9 (Homebrew).

Upstream paths below are relative to that checkout.

### 1. Verified TLS does not cross the CLI dump boundary

`apps/cli/src/command-internal/legacy-db-config.parse.ts` retains SSL mode and root-certificate configuration for the host-side connection. But `legacy-pg-dump.env.ts:120–127` forwards only host, port, user, password and database. `legacy-pg-dump.run.ts:68–74` supplies that environment to the container with no mounts.

Parent check: executed the actual pure schema-environment builder with synthetic connection values, `sslmode=verify-full` and a synthetic CA path. Its keys were `EXTRA_FLAGS`, `PGDATABASE`, `PGHOST`, `PGPASSWORD`, `PGPORT`, `PGUSER`; neither TLS mode nor CA path was forwarded.

This proves loss at this source boundary, not a live plaintext downgrade or behavior of every Supabase CLI release. Image defaults were not tested. A successful verified host-side connection cannot certify the separate container connection. A custom container launcher with explicit certificate mounts would be a different route needing qualification.

### 2. A literal schema containing a space splits into separate arguments

The same builder produces `EXTRA_FLAGS=--schema=a b` for schema `a b`. `apps/cli-go/pkg/migration/scripts/dump_schema.sh` expands that variable unquoted.

Parent check: executed the exact pinned schema script with a shell-function stub replacing `pg_dump`; no database client was invoked. The stub received separate `--schema=a` and `b` arguments. Assertions passed.

This is argument splitting, not proof of arbitrary shell command execution. Native argument arrays avoid this problem, but `pg_dump` still interprets schema arguments as patterns. Literal quoting and decoy-schema exclusion require real PostgreSQL tests for C.

### 3. Dry-run expansion can contain a password

Parent check: invoked the actual `legacyExpandScript` with a synthetic, non-secret password canary. It appeared in the expanded script, as expected from the dry-run implementation. No real credential was used or displayed.

Do not use real credentials in CLI dry-run diagnostics. This does not establish telemetry exfiltration. The pinned CLI has flag-redaction logic; telemetry and ambient configuration would nevertheless need explicit control in a SPARC integration.

### 4. The existing fixture timeout does not terminate the whole pipeline

`scripts/hosted_rehearsal.py:129–132` uses `subprocess.run(timeout=60)` without descendant lifecycle management. B invokes Bash, which launches `pg_dump`/`sed` children.

Parent check: exercised the real shared runner with a synthetic `sleep | cat` pipeline, shortened its timeout to 0.25 seconds, and gave the test a dedicated session solely for safe cleanup. The runner raised its timeout error and reaped Bash, but the synthetic process group still existed. The test then sent SIGKILL to that isolated group. Temporary files were removed; no database/network was involved.

This is a reproduced operational limitation, not evidence that prior successful captures failed. Before reusing B for additional execution, correct and regression-test complete cancellation. A future C runner also needs cancellation, private staging, bounded diagnostics/output, and partial-output handling; native execution alone does not establish those controls.

## Recovery semantics that remain mandatory

- **Schema, data and roles have different exclusions.** Default CLI schema dumps exclude managed definitions, while data-only dumps can include Auth/Storage rows. Explicit schema selection replaces some default exclusion behavior. Do not describe either route as a complete project backup.
- **Ownership and effective access must survive.** Dropping owners or ACLs to make restoration succeed is not permission-equivalent recovery. Destination global/schema defaults, column grants, memberships, grantors, RLS and SECURITY DEFINER behavior matter. Existing fixtures only demonstrate fixed postgres ownership and restricted NOLOGIN roles.
- **History is data.** Preserve migration-history records without executing their statement contents. Conflicts and unrecognized layouts require explicit refusal or a separately approved policy.
- **Snapshot consistency belongs to the operation.** One full pg_dump invocation provides its database snapshot in either plain or custom format. Separate inventory/history calls need coordination or an explicit quiescence policy. Sequence state, global roles and concurrent DDL need stated limitations; before/after equality is not a universal concurrency guarantee.
- **Selected schemas are not necessarily a closed recovery unit.** Cross-schema FKs, types, functions, policies, extensions and runtime SQL dependencies require included dependencies, verified prerequisites or refusal. Archive ordering and selective-restore switches do not compute complete dependency closure.
- **Archives can execute database code.** Encryption and internal hashes are not sender authentication or a sandbox. Upstream scripts strip psql restriction markers and modify SQL semantics; custom archives still carry executable definitions. Restore only within a defined trusted-producer/target boundary.
- **Managed services remain separate.** Database metadata is not Storage object bytes, Auth configuration, Vault root keys, Edge Function deployment or complete project configuration.

## Installation and maintenance consequences

A requires the CLI, a running compatible container runtime and PostgreSQL images; its documented SQL restoration also needs psql locally or in a container. Node/npm is installation-method dependent, not required for standalone/Homebrew CLI binaries. CLI version alone does not pin the effective image.

B currently needs native PG17 tools, Bash/sed, Unix Python runner facilities and the SPARC archive executable. It is developer rehearsal tooling, not a cross-platform desktop distribution solution.

C needs compatible native PostgreSQL clients and their libraries, but no Supabase CLI, Docker or running local PostgreSQL server for remote operations. Supported binaries, provenance, updates and packaging still need an explicit policy. On macOS, Homebrew offers `libpq@17`; no installation was performed in this review.

## Gates for the next local experiment

1. Astra specifies one closed application-only support contract: exact owners, privileges, roles, managed prerequisites, dependencies and refusals. Provider-owned `public` must be addressed explicitly, not silently excluded while claiming general support.
2. Prove literal selection with real PG17 schemas containing spaces, quotes, Unicode and pattern punctuation, plus similarly named decoys; missing selections must fail.
3. Exercise valid TLS, wrong CA, hostname mismatch and missing required TLS using isolated test certificates/endpoints. Prevent credential canaries from appearing in argv, diagnostics or published artifacts.
4. Capture the supported schema/data set in one invocation; coordinate auxiliary data or enforce the stated quiescence policy. Do not promise concurrent consistency without evidence.
5. Round-trip through custom dump, encrypted archive and independent destination restoration; compare contents, owners, ACL/default ACLs, RLS behavior, sequences and constraints. Preserve history as inert data. Refuse unsupported external dependencies and target collisions.
6. Inject cancellation, output/disk failure and mid-restore errors. Prove no published partial archive, appropriate rollback/quarantine, no surviving child processes and no automatic destructive retry.
7. Keep existing v1/v2 regressions and inspector readiness flags unchanged. Local stock PostgreSQL cannot prove hosted provider behavior. Any later hosted mutation needs new precise authorization; previous permissions remain consumed.

Implementation belongs to Terra/Sol (Luna only for easy work), with fresh Astra review and parent verification. This decision does not authorize bundled-client installation, paid services, hosted changes or general-support claims.

## References

- https://supabase.com/docs/reference/cli/supabase-db-dump
- https://supabase.com/docs/guides/local-development/cli/getting-started
- https://supabase.com/docs/guides/platform/migrating-within-supabase/backup-restore
- https://github.com/supabase/cli/tree/v2.117.0
- https://www.postgresql.org/docs/17/app-pgdump.html
- https://www.postgresql.org/docs/17/app-pgrestore.html
- https://formulae.brew.sh/formula/libpq@17
- Existing SPARC native-recipe, expanded-database and application-inspection plans dated 2026-09-20.
