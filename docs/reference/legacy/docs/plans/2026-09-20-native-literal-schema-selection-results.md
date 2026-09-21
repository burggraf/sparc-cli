# Native literal schema selection: implementation results

Status: local synthetic PG17 evidence only. This implements a bounded `pg_dump` selector-argument helper and proves its literal-pattern behavior in one disposable local fixture. It is not a general capture route, restore API, or hosted/provider support claim.

## Implemented boundary

`scripts/native_schema_selection.py` contains the pure `pg_dump_schema_args(schemas)` helper. It calls `application_inventory.validate_schemas` directly, so its accepted selection is the existing 1--32, unique UTF-8, 1--63-byte, non-NUL, non-protected schema set in deterministic order. It returns only:

```text
--strict-names
--schema="<PG17 double-quoted literal pattern>"
```

for each selected name. The complete name is double-quoted and embedded double quotes are doubled. It does not invoke a shell, subprocess, database, archive executor, or framework.

`tests/native_schema_selection_test.py` has three offline checks: argument construction including a double quote/backslash/pattern-punctuation name, invalid selections refusing before arguments can be returned, and direct reuse of the application-inventory validator.

`tests/native_schema_selection_pg_test.py` is opt-in only through `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin`. It checks `pg_dump`, `pg_restore`, and server major 17. The inspected approved location is a valid symlink to `/opt/homebrew/Cellar/postgresql@17/17.9`; `pg_dump`, `pg_restore`, `postgres`, `initdb`, `pg_ctl`, and `psql` each reported PostgreSQL 17.9 (Homebrew), exited zero, and produced no stderr.

The test starts one explicitly owned, `0700` private, socket-only cluster with a private `HOME` and sanitized environment. `listen_addresses` is empty and the connected server reports no `inet_server_addr()`. It reuses the test-only `cleanup_owned_cluster` helper from `destination_permissions_pg_test.py`; bounded fast/immediate `pg_ctl` shutdown, process exit, pid removal, and socket unavailability are required before deletion, otherwise the private root is quarantined.

## Observed literal-selection result

The fixture selected 19 schemas, within the 32-name limit: ordinary, mixed case, spaces, literal surrounding and embedded quotes, Unicode, a 63-byte UTF-8 name, dots, backslashes, a backslash/quote combination, `*`, `?`, brackets, parentheses, pipe, plus, dollar, comma, a leading hyphen, and a newline. It also seeded five similarly named decoys.

One custom-format `pg_dump` used `pg_dump_schema_args` including `--strict-names`; one `pg_restore --single-transaction --exit-on-error` restored it into a fresh target. A JSON catalog/data comparison found exactly the expected additions: the 19 selected schemas and 19 `items` tables, plus the target's preexisting `public.preexisting` table. It compared all 20 expected marker rows (19 selected plus the preserved public marker). None of the five decoys appeared.

The double-quoted/doubled-interior-quote selector candidate therefore passed against the real PG17.9 clients for the complete matrix, including literal backslash/quote and regular-expression/wildcard punctuation; this is client evidence, not merely string construction.

The isolated RED diagnostic used the deliberately naive direct argv selector `--schema=star*`. Its custom dump/restore included both the intended literal `star*` schema and the unwanted `star-decoy` schema. Thus native schema selectors are patterns without a shell, and quoting remains required.

A third custom dump supplied one existing selection and `missing_schema` through the helper. It returned nonzero because of `--strict-names`. No `pg_restore` was invoked after that failed capture; any failure output/archive remained under the private test root and was removed only after confirmed cluster shutdown.

## Commands and exact outcomes

All commands used `PYTHONDONTWRITEBYTECODE=1`.

| Phase | Command | Result |
| --- | --- | --- |
| GREEN | `python3 -m unittest tests/native_schema_selection_test.py -v` | Passed: 3 tests, 0 failures. |
| GREEN | `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest tests/native_schema_selection_pg_test.py -v` | Passed: 1 test in 2.131s, 0 failures/skips. It made 2 successful custom captures/restores, 1 expected strict-name capture failure, 0 restores after that failure, and confirmed cleanup of 1 owned cluster with 0 quarantines. |
| Full opt-in regression | `SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest discover -s tests -p '*_test.py' -v` | Passed: 69 tests in 27.557s, 0 failures, 0 skips. |
| Static | `python3 -m py_compile scripts/native_schema_selection.py tests/native_schema_selection_test.py tests/native_schema_selection_pg_test.py && git diff --check` | Passed. |

The RED above is an observed focused counterexample, not fabricated TDD history: the test passes by asserting the naive selector's unwanted inclusion. The strict-name nonzero result is also an observed expected negative, not a successful capture.

## Scope and limitations

The helper supplies selector argv only. It does not establish version safety outside the PG17 fixture, transport, credentials, TLS, permissions, dependency closure, restore readiness, archive safety, provider behavior, concurrent snapshots, or scale. The fixture uses bootstrap privileges only for local synthetic setup/capture and does not retest restricted-role permission equivalence or role restoration. Captured subprocess output is size-checked only after collection; that is diagnostic checking, not an allocation bound. No hosted service, credentials, private operator/configuration file, browser/MCP, Docker, installation, shell command, or publication action was used.

## Review and independent verification

Astra reviewer `reviewer9ee865fa` accepted this selection-only milestone. The initial independent verifier output was truncated and therefore inconclusive; an authorized repeat by Terra `cb93f63d` completed the full opt-in Python suite with exit 0: 69 tests, 0 skips, 0 failures, in 27.539s. This verification covers the retained reviewed helper/test hashes and does not extend the milestone beyond literal schema selection. The next gate is dependency handling; it remains unimplemented, and no hosted authority is granted.
