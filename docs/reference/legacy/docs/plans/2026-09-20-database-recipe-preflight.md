# Database recipe compatibility preflight

Status: the owner-approved pinned upstream-script/native-client fixture rehearsal passed. Implementation and observed results are recorded in [the bounded plan](2026-09-20-native-recipe-implementation.md). Source inspection itself performed no hosted changes; the subsequent authorized rehearsal cleared and restored only the destination fixture.

## Scope and authorization

The next milestone is a bounded database-recipe experiment, not a general Supabase backup claim or desktop integration. Retain both Free-tier test projects and the $0 ceiling. The owner authorized removal of only the synthetic `sparc_rehearsal` schema from the restore project, followed by reuse for the next test. This is not permission to delete/reset a project, modify the source, or change other schemas/services.

Cleanup stays deferred until capture and restore safety gates are ready. Never run an unchecked `DROP SCHEMA ... CASCADE`: dependencies outside the schema must not be removed under this authorization. Any required role, public-schema default-privilege, migration-history, or managed-service changes need separately defined scope before live execution.

## Observations

- Installed Supabase CLI reports `2.117.0`.
- Docker client and daemon report `29.4.0`; no containers or images were created by this preflight.
- Upstream tag `v2.117.0` was cloned into ignored local research storage and its commit verified as `21db855916f2c2b12f61cde923a27094b8528b23`.
- `apps/cli/src/command-internal/legacy-pg-dump.env.ts`, function `legacyToDumpEnv`, passes only PGHOST, PGPORT, PGUSER, PGPASSWORD and PGDATABASE as connection settings. Mode-specific builders add dump filters, not PGSSLMODE or PGSSLROOTCERT.
- `apps/cli/src/commands/db/dump/dump.handler.ts` passes those mode environments to `legacyStreamPgDump`.
- `apps/cli/src/command-internal/legacy-pg-dump.run.ts` forwards that environment and uses `binds: []`. The inspected path does not propagate our required `verify-full` setting and pinned CA into the dump container. Parsing TLS options in the host-side connection resolver does not establish that the separate dump process uses them.
- This is a source-level finding, not a live TLS downgrade test. Image defaults and installed-binary behavior have not been independently exercised. Do not connect using real credentials merely to investigate it.
- Upstream scripts rewrite SQL and comment out psql restrict/unrestrict commands. Their data script changes session replication role. These are security-relevant recipe characteristics, not changes to copy silently into the archive engine.
- CLI `--dry-run` expands passwords into stdout and is not an approved diagnostic with hosted credentials. CLI file output can create/truncate a 0644 file; SPARC must retain private, no-clobber staging and bounded diagnostics.

## Alternatives

1. **Recommended: pinned upstream scripts with native PostgreSQL 17 clients.** Keep upstream SQL transformations identifiable and hash-pinned, while controlling TLS, credentials, output permissions and errors in the parent runner. First use only the existing synthetic schema and trusted locally generated SQL. Label this an upstream-script/native-client experiment, not CLI/Docker parity. Do not remove psql safety protections in SPARC's general restore path.
2. **Explicit container runner.** Run the same pinned scripts in a pinned image with read-only credential/CA mounts and explicit verification settings. This requires validating image provenance, container networking, secret cleanup and negative TLS cases; Docker remains a developer prerequisite. It is also not an unmodified CLI execution.
3. **Wait for an upstream CLI path that demonstrably preserves required TLS settings.** Least custom integration but blocks the live compatibility milestone for now.

## Required next gates

- Approve the execution route and write its bounded implementation plan before adding code.
- Exercise credential/CA transport with synthetic inputs first; prove that a wrong CA and wrong hostname fail without printing secrets.
- Reuse the existing harness validation and independent archive verification where possible. Never pass passwords in command arguments, dry-run output or renderer state.
- Inspect captured role/schema/data SQL privately before any restore; inventory migration history without assuming its absence or replaying migrations.
- Refuse any restore requiring mutations outside the authorized schema until separately authorized. Capturing broader metadata does not authorize restoring it.
- Before schema cleanup, verify destination identity, unchanged fixture inventory, dependency boundaries and managed-service baselines. Preserve the previous verified encrypted archive. Execute cleanup transactionally with timeouts and postconditions.
- Restore from destination inputs plus archive/key only, with known source credential paths denied. Verify exact data, constraints, grants, positive/negative RLS and sequence state independently.
- Record supported and unsupported coverage. Auth, Storage bytes, functions, Vault, full-project recovery and target scale remain unproven.

## Sources

All code references above are relative to [the pinned upstream commit](https://github.com/supabase/cli/tree/21db855916f2c2b12f61cde923a27094b8528b23). The upstream command side-effect inventory is `apps/cli/src/commands/db/dump/SIDE_EFFECTS.md`; SQL scripts are in `apps/cli-go/pkg/migration/scripts/` with verbatim TypeScript copies in `legacy-pg-dump.scripts.ts`.
