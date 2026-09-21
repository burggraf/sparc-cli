# Native PostgreSQL permission-preservation experiment: results

Status: local synthetic PG17 experiment completed. This is evidence for a destination-privilege preflight requirement, not a general exporter, restore API, or hosted-Supabase support declaration.

## Environment and boundaries

- Client and server: PostgreSQL 17.9 (Homebrew), `/opt/homebrew/opt/postgresql@17/bin`.
- The opt-in test requires `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin` and rejects no version itself beyond its PG17 server assertion.
- It creates a new `0700` temporary `/tmp/spnp-*` directory, `0700` private Unix socket directory, temporary `HOME`, and a trust-authenticated PG cluster. `postgres` is started with `listen_addresses=` and a private `-k` socket path: no TCP listener is created.
- The process environment is a sanitized allowlist. Commands are direct argument vectors with 30-second timeouts. This fixed tiny-fixture test captures subprocess pipes and rejects output above 200KB **after** collection; that is diagnostic-size checking, not a live output bound and is not a general runner claim. There is no shell, network, hosted database, browser/MCP, secrets, private config, installation, or upstream-CLI/script execution.
- The server is terminated and waited (then killed only if termination exceeds 15 seconds) in `finally`; `TemporaryDirectory` removes the cluster, socket, archive, and log even on an assertion failure. The successful run left no reusable cluster or archive.

The pinned source was read only as a compatibility reference. In particular, `apps/cli/src/command-internal/db-bootstrap/db-setup.ts` and `.../templates/db-initial-schema-13.sql.ts` show explicit `ALTER DEFAULT PRIVILEGES` rules, including both schema-scoped rules and provider baseline rules. No conclusion about those provider baselines is drawn from this synthetic `native_app` schema.

## Fixture and commands

The fixed schema has an `app_owner` non-superuser, an `app_reader`, and `unapproved`; all are synthetic login roles. `bootstrap` creates the four temporary databases and grants only database `CREATE` to `app_owner`. The fixture covers:

- table and column grants, identity sequence `USAGE` and `SELECT` as distinct grants, schema `PUBLIC` identity, and explicit routine `EXECUTE` (with `PUBLIC` revoked);
- `app_owner` ownership of schema, table, sequence, and SECURITY DEFINER routine with fixed `pg_catalog` search path;
- forced RLS and a deterministic `app_reader` policy (`id = 1`);
- schema-scoped `app_owner` table default privileges for `app_reader`.

Capture is intentionally performed by local `bootstrap`, which is the cluster bootstrap administrator, so forced RLS cannot hide source rows. This is a limitation of the experiment. The archive is one native schema-and-data custom dump:

```text
pg_dump --format=custom --schema=native_app --file=<private temporary archive> native_source
```

Restore is authenticated as the real restricted `app_owner` and uses exactly:

```text
pg_restore --dbname=<temporary target> --single-transaction --exit-on-error <private temporary archive>
```

The test does not use `--no-owner`, `--no-acl`, `--disable-triggers`, `--clean`, target normalization, destructive retry, or permissive restore error handling. Roles are precreated prerequisites and are not in the archive.

## TDD record

All commands below set `PYTHONDONTWRITEBYTECODE=1`.

| Phase | Command | Result |
| --- | --- | --- |
| RED | `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/native_permissions_pg_test.py -v` | Failed while the initial local lifecycle setup was incomplete: first the failure diagnostic attempted `read_text` on its open log writer; after that was corrected, the literal empty host form caused PostgreSQL to attempt a TCP host named `''`; then the temporary socket path exceeded macOS's Unix-socket limit; finally, the first catalog assertion exposed the needed explicit `polcmd::text` cast. No restore result was claimed from these failing runs. |
| GREEN | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/native_permissions_pg_test.py -v` | Passed: `Ran 1 test in 2.359s`, `OK`. |
| Regression | `PYTHONDONTWRITEBYTECODE=1 env -u SPARC_TEST_PG17_BIN python3 -m unittest discover -s tests -p '*_test.py' -v` | Passed: `Ran 54 tests in 14.229s`, `OK (skipped=4)`. The skipped tests are the repository's opt-in PG17 tests, including this new test after its opt-in environment variable was deliberately removed. |
| Follow-up RED | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/native_permissions_pg_test.py -v` | Failed after semantic default-ACL comparison was introduced because the comparator cast absent target fixture relations to `regclass` before restore. This was a test-comparator defect, not a restore claim; it was corrected to use `to_regclass`/`to_regprocedure`, preserving absent-object semantics. |
| Follow-up GREEN | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/native_permissions_pg_test.py -v` | Passed: `Ran 1 test in 2.629s`, `OK`. This run includes the semantic ACL/default comparison, negative ACL mutations, target routine denial, separate sequence operations, and owner FORCE-RLS checks. |

## Observations

### Compatible target

The restore succeeded under `app_owner`, not bootstrap's effective superuser context. Its canonical role-name metadata matched the source for schema/table/sequence/routine owners, table RLS and FORCE flags, and the deterministic policy expression.

The test also compares a sorted semantic ACL list rather than privilege booleans. Every entry records object kind and fixed schema-qualified object (including all fixture table columns), owner role, grantor role, grantee role (`PUBLIC` is rendered by name rather than OID), privilege, and grant-option flag. For object ACLs, a NULL catalog ACL is expanded with PostgreSQL's `acldefault` for that object type; for default ACLs, absence/NULL remains no altered default entry rather than being synthesized as an object ACL. The source and compatible lists were exactly equal and included schema `PUBLIC` USAGE, table/column grants, separate sequence `USAGE` and `SELECT`, routine `EXECUTE`, and the schema-scoped default table grant.

The test then makes bounded, fixed-fixture negative mutations on the compatible target: an extra table grant to `unapproved`, adding the default-grant option to the reader's default SELECT, and revoking only sequence SELECT. Each produces a semantic ACL difference; the latter specifically removes the SELECT entry while USAGE remains, proving the comparison does not treat `USAGE,SELECT` as an ANY-style boolean. The target is restored to the source semantic ACL between the first two checks; no mutation is mistaken for permission equivalence.

Effective principal checks also matched the source: `app_reader` saw only row `id=1`, could read `id` and `reader_note`, could not read `owner_note`, could execute the routine, and could perform both `nextval` (USAGE) and `last_value` (SELECT). `unapproved` was denied routine execution and both sequence operations on **both source and compatible destination**. With no owner policy, `app_owner` saw zero rows on source and compatible target, demonstrating FORCE RLS rather than only ordinary-reader filtering. A post-restore table created by `app_owner` was readable by `app_reader` under the restored schema-scoped default privilege.

### Incompatible target: actual privilege drift

Before restore, the target was deliberately given this **global** default privilege for `app_owner`:

```sql
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT SELECT ON TABLES TO unapproved;
```

Before attempting restore, the semantic default-ACL list confirms the global entry with owner and grantor `app_owner`, grantee `unapproved`, privilege `SELECT`, and no grant option. `pg_restore` **succeeded**; it did not reject or remove this destination state. The resulting `unapproved` principal had `SELECT` on both restored tables. Forced RLS made `SELECT count(*) FROM native_app.documents` return `0`, so the ordinary non-RLS `native_app.access_probe` was used to prove the grant was effective: `unapproved` read `ordinary-table-probe` successfully.

Therefore a successful native restore is not permission-equivalent in the presence of incompatible destination global defaults. A future general route must preflight and fail closed on an explicit destination privilege/default-ACL contract. It must not auto-revoke destination grants or defaults just to make restore pass.

### Collision rollback

The collision target starts with an independent `public.rollback_sentinel` row. A temporary target-only event trigger injects an already-existing `native_app.documents` relation after restore creates `native_app.access_probe`; this causes a mid-restore `already exists` error. `pg_restore --single-transaction --exit-on-error` returns nonzero, the newly created `native_app` namespace is absent afterward, and the preexisting sentinel remains exactly `1:unchanged`. The event trigger and its function are removed after the assertion. There is no retry or cleanup restore.

## Parent verification and review

After the ACL-comparison and effective-access corrections, the retained independent Astra reviewer accepted this bounded experiment with no remaining findings. The parent inspected the corrected code and independently ran:

```sh
PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin \
  python3 -m unittest discover -s tests -p '*_test.py' -v
```

Result: **54 tests passed, no skips**, including all four opt-in local PG17 tests (23.052 seconds). `git diff --check` also passed. No existing production or fixture implementation was changed. These are local regression results, not new hosted recovery evidence.

## Limitations retained

This uses source and targets in one local stock PG17 cluster and proves neither independent clusters, global-role restoration, unavailable-source recovery, provider-managed `public` baselines, hosted Supabase behavior, TLS, literal arbitrary schema patterns, concurrent snapshots, dependencies outside the fixed schema, large archives, encryption integration, migration/history restoration, or role memberships. Capture's local bootstrap privilege is specifically not evidence that an ordinary application principal can dump forced-RLS data. Custom archives remain trusted executable database input; this experiment does not sandbox them.
