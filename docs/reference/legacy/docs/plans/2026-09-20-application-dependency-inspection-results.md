# Application dependency inspection results

## Implemented boundary

`scripts/application_dependencies.py` observes direct PostgreSQL 17 catalog edges for explicitly selected, non-protected schemas. Literal schema names enter only as JSON encoded to hex in a fixed `REPEATABLE READ READ ONLY` transaction with `pg_catalog` as the local search path. The response contains selected-schema presence, transaction evidence, direct `pg_depend` edges, explicit `pg_inherits` edges, and extension ownership associations.

Covered dependent sources are relations and columns, routines, **all selected `pg_type` records** (including underscore-named domains and standalone composite types), constraints, defaults, rewrite rules, policies, all triggers including internal triggers, and schemas. Constraints include their owning relation/domain and constraint name. Reference identities use raw catalog namespace fields and structured name/subname/routine-argument fields; no identity is recovered by splitting rendered text. Unsupported targets retain bounded database-local `pg_depend` class/object/subobject addresses marked unresolved/local-only. Those addresses distinguish observations in one catalog only; they are not portable identities and no expression, body, option, or row rendering is read.

The response validator accepts bytes only. It rejects duplicate JSON keys/constants/invalid UTF-8/excess depth, caps raw input at 2 MiB and each record family at 10,000, validates PG17 and exact envelope/record forms, requires kind-specific names/subnames/routine arguments/constraint owners, rejects missing known attribution, requires every selected schema to be present, preserves `P` and `S` partition dependency tags, rejects conflicting extension annotations by one canonical object identity map across source and target appearances, and requires side-specific membership records to exactly match edge annotations. Assessment sorts findings deterministically, treats an extension association as a requirement before internal-schema classification, and always leaves execution, export, restore, and dependency-completeness flags false.

## Synthetic PG17 result

The opt-in socket-only owned-cluster test used `/opt/homebrew/opt/postgresql@17/bin`, private temporary state and socket paths, a sanitized environment, and `cleanup_owned_cluster` from `destination_permissions_pg_test.py`. It asserted complete structured identities for two same-named cross-schema foreign-key constraints (with distinct owner relations), enum and domain column dependencies (including `_positive`), a composite type, sequence default, normal and `ctid` view-rewrite column dependencies, overloaded parsed SQL routines, policy helper, user and catalog-derived internal trigger dependencies, partition inheritance, and concrete indexed-partition `P` and `S` dependencies with their child-index endpoints. It also asserts a synthetic `auth`-schema stand-in.

The fixture selects literal `a b` and literal quote-bearing `"a b"` schemas. It verifies that the same known FK, enum/domain, and parsed-routine edges that are external when only `a b` is selected are internal when both are selected. It also verifies selected-source extension membership, reference extension membership, and a locally legal `hstore` member in `pg_catalog`; extension associations remain requirements rather than becoming ordinary internal edges.

A string-bodied SQL routine referring to the second schema has no corresponding direct routine edge, and the dynamic/string-body unknown remains. Row, expression, routine-body, and foreign-table option canaries are absent from the observer output. A bounded snapshot of application rows, `pg_depend`, selected relation/foreign-table structure and options, and foreign-server options is unchanged before and after observation. Returned transaction settings are asserted directly.

## Validation outcome

- Focused offline and opt-in PG17 tests: **7 tests passed**.
- Complete opt-in Python unittest suite: **76 tests passed in 29.375 seconds**.
- Complete output and immediate exit-status receipt are retained in the newly created private directory `/tmp/sparc-dependency-review.RHQg2J/`; exit status was `0`.
- Aggregate SHA-256 of the four changed files was unchanged across that full run: `666d7c8ae18e8fa9e63e3ff5b9d8844633554e8bb1f3656b3c9efa9d0868ead8`.

## Independent verification and review clearance

Astra source review `b93c7468` accepted the bounded dependency milestone. Independent Terra verification `16c82f9c` completed the opt-in Python suite with **76 tests, 0 skips, 0 errors, 0 failures**, exit status `0`, in **29.371 seconds**, with the reviewed code inventory unchanged. No hosted database operation or mutation was part of this milestone.

## Limitations retained by design

This is direct catalog evidence, not recursive dependency closure, restore planning, permission verification, or a SQL/body/expression parser. Dynamic SQL, string-bodied routines, runtime external effects, unresolved/local-only catalog targets, row data, expressions, foreign-server options, credentials, and destination compatibility remain mandatory unknowns. No transport, observer, or report in this milestone grants execution, export, restore, or dependency-completeness authority.
