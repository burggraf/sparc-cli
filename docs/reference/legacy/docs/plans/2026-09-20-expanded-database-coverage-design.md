# Expanded database coverage — proposed next milestone

Status: the owner-authorized combined synthetic schema, roles and migration-history rehearsal passed locally and on the two hosted test projects. See [observed results](2026-09-20-expanded-database-implementation.md#combined-hosted-result--2026-09-20). This document describes the design; the specific operator authorization was separately recorded and has been consumed.

## Selected scope

Deliver richer application schema, custom roles/grants/default privileges and migration history together in one milestone. Keep separate acceptance gates within the same rehearsal so failures remain diagnosable. Do not postpone roles/history to a later delivery. The existing local desktop and archive format remain unchanged.

Alternatives:
- Application schema first (initial recommendation, not selected): small dependency/behavior test, no new cluster-wide role authority or managed-schema writes.
- **Schema, roles and migration history together (owner selected):** more realistic; needs distinct safety checks for three failure domains and exact broader hosted authorization.
- Desktop integration now: improves usability but would expose a recovery procedure whose database coverage is still narrow; defer.

## What we actually have

The published native recipe restores one fixed schema with three tables, an owned sequence, a foreign key, grants and forced RLS. Exact source/destination checks deliberately reject other objects. `hosted_rehearsal.guard`, `assertions`, `SEED`, and `PROBE` are fixture-specific; `native_recipe.dump` fixes the schema name and capture/restore call those guards directly. Simply adding objects or accepting another schema name would not extend the safety contract.

Preserve the existing recipe and regression tests. Do not weaken its guard to accept a larger arbitrary catalog, add a generic SQL execution interface, or invent a plugin/profile framework. Decide the smallest shared change only after reading the expanded fixture and tests end to end.

## Coverage and acceptance matrix (baseline at planning)

| Area | Evidence now | Next required evidence |
| --- | --- | --- |
| Tables, exact values, FK, sequence | Hosted synthetic round trip passed | Retain all checks |
| Enum and check constraint | Not tested | Exact enum labels; valid insert accepted; invalid input rejected |
| Secondary index | Not tested | Definition, uniqueness where applicable, predicate and validity match |
| View | Not tested | Definition and results match; invoker access does not bypass underlying restrictions |
| SQL function | Not tested | Schema-qualified, security-invoker function; exact definition, owner, ACL and result checks |
| Dependencies | Only current table/sequence/FK fixture | Dump/restore ordering works for the added type, table, function and view |
| Custom roles and memberships | Not tested | Separate allowlist; attributes/membership/ownership and actual denied/allowed access; reserved roles unchanged |
| Default privileges | Existing fixture configures them; broader future-object behavior unproven | New disposable target object gets exactly intended grants, including negative checks |
| Migration history | Not tested | Inspect versioned format; archive rows as metadata, restore compatible history without executing recorded SQL or replaying migrations; reject conflicting target history |
| Managed services and scale | Not tested | Separate future milestones; no claim from this work |

## Gate A: application schema dependencies

Proposed fixed fixture adds one enum, one constrained table using that enum, one secondary index, one security-invoker SQL function and one security-invoker view. Use synthetic values only. Keep existing data/grants/RLS probes. Avoid SECURITY DEFINER, event triggers, extensions and outbound calls. Integrate the explicit role ownership/grants from Gate B into this same fixture.

Start with offline tests for exact inventory, malformed metadata, changed script pins, mismatched artifact hashes, used-target refusal and bounded/redacted failures. These tests establish control flow, not PostgreSQL behavior. Real SQL behavior must later pass in a database rehearsal; string assertions alone are insufficient.

Capture remains pinned native schema/data scripts -> private artifacts -> encrypted SPARC archive -> independent verification. Restore remains destination-only, trusted locally generated SQL, explicit transaction, guards and behavioral probes followed by an independent check. Preserve client verify-full, scoped credentials, no-overwrite outputs and single-attempt fencing. Do not claim concurrent-write snapshot consistency from separate dump invocations; this remains a quiescent fixture experiment.

The exact new object names, definitions, expected catalogs, archive version and source/target preparation SQL must be fixed in the implementation plan before any live authorization request. Reject unknown objects/dependencies rather than automatically dropping them. Catalog inspection should compare expected definitions and effective permissions, not just object counts.

## Gates B and C: required in this milestone

**Roles:** roles are cluster-wide. Do not replay a whole roles dump. First inspect the pinned upstream role recipe and supported hosted permissions. Define a tiny NOLOGIN-role allowlist with no elevated attributes/passwords and explicit permitted membership edges. Name collisions and unexpected dependencies stop the procedure. The fixed fixture keeps application objects owned by postgres: arbitrary custom ownership recovery is explicitly unproven. Default-privilege behavior requires actual checks, not only creation success. PostgreSQL-created administrative membership from each new role to its creator must be inspected separately from the one application membership; it must not be confused with granting a privileged provider role to a fixture role.

**Migration history:** first inspect the pinned tool's actual schema and semantics. Archive history separately from executable schema SQL. Restore only to a compatible explicitly approved destination, with exact empty/conflict guards; do not run recorded statements. Writing the migration-history schema is outside current hosted authorization.

## Pinned-source findings and design consequences

Inspected Supabase CLI commit `21db855916f2c2b12f61cde923a27094b8528b23`, files `apps/cli-go/pkg/migration/scripts/dump_role.sh` and `apps/cli-go/pkg/migration/history.go`:

- Role export uses `pg_dumpall --roles-only --role postgres --quote-all-identifier --no-role-passwords --no-comments`, then sed transformations controlled by reserved-role/config patterns. It is not a fixture-role allowlist. Do not execute its entire output for this rehearsal or describe a custom role manifest as upstream role-script parity.
- Migration table creation defines `version text NOT NULL PRIMARY KEY`, then adds nullable `statements text[]` and `name text`. History restore must preserve NULL versus empty names/arrays, statement ordering, Unicode and quotes. The upstream read query coalesces NULL names; exact archival capture must not copy that lossy projection.
- The same source defines `seed_files(path text PRIMARY KEY, hash text NOT NULL)`. This milestone will explicitly report seed-history coverage separately; discovering it unexpectedly must not silently permit overwriting it.
- Use a synthetic history record containing SQL that would fail if executed as an acceptance trap. Restoring that record as data must succeed without running it. Do not use CLI migration apply/repair as a substitute for exact history recovery.
- For this bounded milestone, prefer an exact allowlisted role manifest and fixed role-creation statements over a parser for arbitrary `pg_dumpall` output. Preserve metadata and validate role attributes/memberships against the fixture contract before generating any SQL. This is a proposed implementation choice, not existing functionality.

## Hosted authorization boundary

At planning time both projects held the old fixture and the prior cleanup permission was consumed. The owner subsequently authorized the exact combined source additions and destination fixture-only cleanup/restore. That single rehearsal has now completed; both projects hold the expanded fixture. This design does not authorize another cleanup, retry or broader mutation.

Before the combined rehearsal runs live, present the exact source fixture extension, custom roles/membership/default privileges, migration-history objects/rows, and destination preparation plan. Include explicit RESTRICT drops, validated previous recovery archives and the failure/quarantine procedure. Ask approval for those named operations only, explicitly identifying cluster-wide role effects and migration-schema writes. Neither project will be deleted/reset; no unrelated schema/service or paid resource is involved. A failure does not authorize retry or broader cleanup.

## Offline preparation evidence

Added `scripts/expanded_metadata.py`: a bounded duplicate-key-rejecting JSON contract, exactly two nonprivileged NOLOGIN role declarations, one explicit membership, and nullable migration history. SQL generation refuses role/history collisions and encodes migration content as hex-encoded JSON data. It neither connects to a database nor runs archive SQL. No arbitrary role dump parser was added.

Six offline unit tests passed after observed RED failures for the missing validator, missing SQL generator and missing pre-creation history guard. An opt-in native PostgreSQL 17 test also passed using a disposable socket-only cluster and a non-superuser role creator. Exact NULL/empty/Unicode/ordered-history values, role attributes, membership flags, inert `SELECT 1/0`/psql-command text and collision refusals passed. The local server was terminated by test teardown. Full regression passed: 30 Python tests with the local PG17 test enabled, all root Rust tests and explicit independent age interoperability, rustfmt, Clippy and diff hygiene.

Reproduce the local test with an explicitly chosen PG17 directory:

```sh
SPARC_TEST_PG17_BIN=/path/to/postgresql17/bin PYTHONDONTWRITEBYTECODE=1 \
  python3 -m unittest discover -s tests -p 'expanded_metadata*_test.py' -v
```

That initial preparation was metadata-only and touched neither hosted project. It was followed by the independently reviewed full fixture implementation and successful combined hosted rehearsal linked above. Normal test discovery skips server tests unless explicitly enabled.

## Done means

A gate is complete only after runnable offline checks, actual database behavior checks, independent restored-state verification and published bounded evidence. Keep local-only/offline-tested/hosted-verified states distinct in the report. Desktop database integration waits for a deliberately chosen supported coverage contract, not merely another successful fixture.
