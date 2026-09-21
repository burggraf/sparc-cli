# Destination permission preflight: implementation results

Status: local synthetic PG17 implementation evidence only. This is a bounded catalog prerequisite comparison, not restore authorization, a learned baseline, a restore interface, or hosted/provider evidence.

## Implemented boundary

`scripts/destination_permissions.py` accepts explicit application schemas and creating roles, carries them into one PG17 `REPEATABLE READ READ ONLY` transaction as UTF-8 JSON hex data, and reads only the specified system-catalog metadata. It validates a versioned JSON envelope with duplicate-key rejection, bounded names/counts/output, semantic ACL expansion for database and namespace NULL ACLs, and names rather than OIDs. ACL grantees are structurally tagged as `public` or `role`, so a real quoted role named `PUBLIC` is distinct from the pseudo-grantee.

The observed envelope includes the connected database owner/ACL and direct database-wide-settings presence; selected schema presence/owner/ACL (including expected-absent schemas); all public role attributes and role-settings presence; all membership edges; and selected creators' global and selected-schema altered default-ACL records. Empty altered-default ACL records remain records. Global scope is JSON `null`, never a sentinel schema name. No role-setting values, relation rows, expressions, routine bodies, passwords, or application data are queried.

`compare` requires the independently supplied expected envelope to pass the same shape checks, requires selections and PG17 version to agree, and returns only a negative/positive prerequisite comparison plus fixed false `export_ready`, `restore_verified`, and `execution_supported` flags. It does not generate remediation, bless a current target, normalize defaults, or authorize restoration.

## Review corrections

The follow-up review found and this revision fixes seven bounded defects. Both expected and observed envelopes now require every database/schema owner, ACL grantor/named grantee, membership role/member/grantor, default creator, and scoped-default present namespace to resolve in the full role/schema inventory; the one deliberate exception is an observed absent selected creator, which remains a negative report rather than a malformed observation. Membership identity is `(role, member, grantor)`, matching PG17. ACL privileges are restricted by database, namespace, and default-object context.

Direct dictionary APIs now reject non-exact dictionaries/lists, non-string keys, tuples, cycles, and invalid scalars before JSON serialization; they serialize and UTF-8 encode a fresh JSON copy, then enforce the 2MiB envelope bound. This bounds accepted envelopes, not serialization allocation, and `compare` does not mutate either caller input. Expected-contract provenance/review is an explicit mandatory unknown distinct from archive provenance.

The local fixture now includes row, routine-body, expression, and setting-value canaries; snapshots its bounded fixture/catalog state before and after observation; and checks the transaction's read-only/repeatable-read assertion embedded in the observed SQL. It proves public ACL as well as owner drift. Shutdown uses cluster-owned `pg_ctl -m fast -w`, bounded immediate escalation, and confirmation of postmaster exit, pid removal, and socket unavailability. Lifecycle ownership is explicit: only a confirmed stopped cluster (or a pre-start fixture) is removed; an unconfirmed server leaves its owned directory quarantined. Focused fake-runner tests exercise the fast-failure/immediate branch and the real lifecycle cleanup helper: a failed shutdown leaves a sentinel directory present, and only that synthetic no-server fixture is explicitly removed in test teardown.

## TDD and commands

All commands used `PYTHONDONTWRITEBYTECODE=1`.

| Phase | Command | Result |
| --- | --- | --- |
| RED | `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/destination_permissions_pg_test.py -v` | Failed while developing the fixture because PG17 accepts one membership-option clause per `GRANT`, not two. No comparison result was claimed. |
| RED | same command | Failed on the first catalog query due to an ambiguous `value` alias in the JSON selection CTE. The query was corrected to name its JSON table-function column. |
| GREEN | `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tests/destination_permissions_test.py -v` | Passed: 6 tests. |
| GREEN | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/destination_permissions_pg_test.py -v` | Passed: 1 test in 1.451s. |
| Regression | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest discover -s tests -p '*_test.py' -v` | Passed: 61 tests in 24.637s. |
| Review RED | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/destination_permissions_pg_test.py -v` | Failed first on an incorrect `TemporaryDirectory` context binding, then on an explicit test snapshot needing the `defaclobjtype::text` cast. Both failures occurred in the disposable local fixture; neither is an observation/restore claim. |
| Review GREEN | `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tests/destination_permissions_test.py -v` | Passed: 8 tests. |
| Review GREEN | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/destination_permissions_pg_test.py -v` | Passed: 2 tests in 2.025s. |
| Review regression | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest discover -s tests -p '*_test.py' -v` | Passed: 64 tests in 26.861s. |
| Review30308 RED | `PYTHONDONTWRITEBYTECODE=1 python3 -` (temporary-directory reproduction) | Passed the reproduction assertion: detaching the finalizer did not prevent `TemporaryDirectory.__exit__` cleanup, confirming the blocked lifecycle bug without starting a server. |
| Review30308 GREEN | `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tests/destination_permissions_test.py -v` | Passed: 8 tests, including reversed expected/observed records and ACLs with both inputs unchanged. |
| Review30308 GREEN | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/destination_permissions_pg_test.py -v` | Passed: 3 tests in 1.675s, including the sentinel quarantine lifecycle path. |
| Review30308 regression | `PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest discover -s tests -p '*_test.py' -v` | Passed: 65 tests in 25.117s. |

The opt-in integration creates a new `0700` temporary cluster, `0700` Unix socket directory and temporary HOME, with a sanitized environment. It uses only `/opt/homebrew/opt/postgresql@17/bin`, `initdb -A trust`, direct argument vectors, `listen_addresses=`, and a private socket. It uses explicit `mkdtemp` lifecycle ownership and cluster-aware bounded shutdown in `finally`; it removes cluster/socket/log state only after postmaster exit, pid removal, and socket unavailability are confirmed. An unconfirmed shutdown leaves the owned temporary directory quarantined for manual inspection; a pre-start failure is removed because no server was launched. No TCP listener, hosted connection, credentials, Docker, installation, or retained data was used.

The integration's compatible expectation is explicitly specified for a stock initialized PostgreSQL 17 cluster plus synthetic roles/schemas; it is not copied from the observed target. It proves the expected contract matches, then independently proves refusal for: a global default `SELECT` grant to the real role `"PUBLIC"`; a changed schema default grant option; membership `SET` drift; `public` ownership drift; database ownership drift while `public` remains owned by `pg_database_owner`; and role-setting presence. It also uses quoted/spaced/Unicode selected names plus similarly named unselected schema/default-ACL decoys, and asserts application-value canaries never appear in the observation.

## Limitations retained

A match says only that these caller-selected bounded catalog fields equal caller-supplied data. It does **not** make a database safe or authorize a restore. Mandatory unknowns remain dependency closure, collisions, archive provenance, role/session configuration values, effective privilege behavior, provider baseline compatibility, snapshot concurrency, and managed services. Namespace/default ACLs do not verify object/column/routine ACLs, effective access, managed-provider behavior, or any hosted target. The local fixture is not an independent-cluster, production-scale, or real-data test.

## Independent clearance and publication readiness

Astra reviewer `ca97eb4c` accepted this bounded milestone. Independent Terra verification `820e650f` ran the complete opt-in Python suite with the reviewed code unchanged before and after: `65` tests passed, `0` skipped, in `25.022s`. The reviewed implementation hashes were:

- `scripts/destination_permissions.py`: `c2d32b0c25a4d281225fe3e9d1dd0310b9bd8c54f7be937b822141814f1699a6`
- `tests/destination_permissions_pg_test.py`: `2db30db1e827ae2c1785d8d9942b65edb0a7f330a487c4caab8a3f12f3096d56`
- `tests/destination_permissions_test.py`: `3c562ca378da69b496e32a0b8b046edb00a8dac7f18159f4f6eb5f9445a0210f`

This clearance is evidence for the local bounded observer only. A match still does not make a destination safe or authorize a restore.
