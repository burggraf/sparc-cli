# Native application recovery profile — final local results

Status: **accepted local experiment**. Independent source review found no issues, and an independent full-suite run passed. This remains a constrained fixture result, not a general recovery interface.

## Verified scope

The opt-in test composed existing catalog observers, literal schema selectors, destination prerequisite comparison, native PostgreSQL tools, and the existing encrypted SPARC archive CLI. It used PostgreSQL **17.9** clients and two independently initialized, disposable local clusters: one source and one target.

The trusted, quiescent, exclusively owned source fixture had two fixed non-public schemas, including spaces and Unicode. It contained:

- one non-superuser owner and one restricted reader, precreated independently on the target;
- ordinary logged tables, an enum, identity sequences, primary/unique/check constraints, an ordinary index, and a cross-schema foreign key;
- representative Unicode, null, text, and sequence state;
- explicit schema, table, column, and sequence grants; and
- a schema-scoped default `SELECT` grant for the reader.

Roles were prerequisites, not dump contents. Capture used one custom-format `pg_dump` with exact literal schema selectors. Restore authenticated directly as the non-superuser owner and used one `pg_restore --single-transaction --exit-on-error` attempt.

## Evidence and provenance

Before capture, real `application_inventory` and `application_dependencies` SQL and validators ran through test-local transports. Fixture-specific assertions checked the reviewed schemas, relations, owners, types, columns, built-in `pg_catalog.int4`/`text` prerequisites, and absence of unsupported routines, user triggers, policies, extension members, and rejected dependency classes.

The catalog observations remain incomplete by design. Both observers retained all four conservative flags as `false`: `execution_supported`, `export_ready`, `restore_verified`, and `dependency_analysis_complete`. Empty direct PG17 `pg_depend` edges for the pinned built-ins were recorded as a fixture observation, not dependency closure or a system-schema allowlist.

The dump and bounded profile metadata were packed, verified, and unpacked with the existing age-encrypted SPARC CLI. An independently held receipt bound both unpacked files to the trusted pre-capture bytes before the normal recovery path accessed the destination. This establishes correspondence to a trusted locally produced artifact; encryption and self-contained hashes do not authenticate an arbitrary sender or archive.

The source cluster was then confirmed stopped. The test counted source calls and proved no later source wrapper call while restoration and verification ran against the independently initialized target cluster.

## Restored state and behavior

The compatible target first matched an independently specified prerequisite contract for database ownership and ACL, role attributes and memberships, absent selected schemas, and no altered defaults. The test then verified exact structured source-to-target state for:

- selected schema, table, enum, index, constraint, and identity-sequence inventory;
- named ownership, ordered columns, qualified types, nullability, identity mode, and defaults;
- constraint definitions and validation/deferrability state;
- index definitions and state flags;
- sequence configuration, ownership, owned-column identity, `last_value`, and `is_called`;
- row values; and
- semantic schema/table/column/sequence/default ACLs, including grantor, named-role or tagged `PUBLIC` grantee, privilege, grant option, null ACL, and explicit empty ACL distinctions.

The explicit `parents` table `SELECT` grant and the `items` column grant were preserved. Separate authenticated probes established the owner and reader identities and non-superuser attributes. Reader table access succeeded, access to the ungranted column failed, and reader writes failed.

A new owner-created table received the restored schema-scoped default `SELECT` grant. Its PG17 owner privilege set included `MAINTAIN`; reader `SELECT` succeeded and reader `INSERT` failed.

A rolled-back behavior probe proved a valid cross-schema FK insert succeeds and an invalid insert reports SQLSTATE `23503`. A second exact snapshot comparison proved the probe did not mutate recovered state.

The preexisting stock PG17 `public` namespace owner and semantic grants were unchanged before and after restore. This is **public preservation**, not support for capturing or restoring application objects into provider-owned `public`.

## Fail-closed cases

The test refused each of these before an additional dump or restore primitive call, as applicable:

- source external `public` FK;
- source user routine;
- missing selected schema;
- damaged dump or mismatched external receipt;
- destination global default table-`SELECT` grant;
- preexisting selected schema/table collision;
- unexpected `public` table;
- unexpected `public` composite relation/type;
- unexpected `public` collation;
- destination user event trigger on the normal path; and
- repeated normal restore attempt.

Sentinels survived all relevant refusals. Error assertions used fixed categories, and a non-opt-in canary test proved metadata, state, and native diagnostic failures did not disclose a captured value.

A fixture-only fault path directly invoked the same one-attempt primitive after separately proving normal admission refusal. A `ddl_command_end` trigger failed late, only after the `items` table, both selected schema catalog rows, and enum catalog row existed in the active transaction. The intended fixed marker was observed, the precondition marker was absent, the sentinel survived, and both selected schemas were absent afterward: meaningful late-DDL transactional rollback.

The final late-fault fix replaced `to_regnamespace` checks for unquoted schema names containing spaces with exact `pg_namespace.nspname` checks, plus an exact `pg_type`/`pg_namespace` join for the enum. The successful target separately records the PG17 behavior: both catalog schema rows exist while those unquoted textual `to_regnamespace` calls return null. No dynamic identifier acceptance or general name parser was added.

Startup ownership was registered before readiness assertions. A unit test directly exercised registered cleanup failure and confirmed root quarantine; startup/readiness exception routing was reviewed in source, not exercised by that injected unit test. Successful runs confirmed both owned clusters stopped before deleting their roots.

## Independent verification

Exact command:

```sh
PYTHONDONTWRITEBYTECODE=1 SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin python3 -m unittest discover -s tests -p "*_test.py" -v
```

Result: **79 passed, 0 failures, 0 errors, 0 skips**, `Ran 79 tests in 34.479s`, exit **0**. The accepted test and design hashes matched before and after, imported helper hashes were unchanged, repository status bytes were unchanged, and no owned cluster root or PostgreSQL process remained.

## Limits

This does not establish arbitrary SQL or archive safety, archive authenticity, exhaustive catalog admission, provider-owned `public` restoration, hosted behavior, concurrent capture consistency, scale, or production support. It does not cover managed Auth/Storage/Vault/services, arbitrary expressions or objects, role export, cross-major restore, or automatic destination repair. No hosted operation or credential use occurred.
