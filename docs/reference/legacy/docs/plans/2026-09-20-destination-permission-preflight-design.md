# Destination permission prerequisites: read-only comparison

Status: approved direction, bounded implementation specification. Astra owns design/review; Terra owns implementation and execution. This is not a restore executor or a complete recovery authorization gate.

## Why this slice

The native-permissions experiment proved that pg_restore can succeed while destination global default grants expose data. Implement a catalog-only prerequisite comparison before broader recovery work. No destination normalization, owner remapping, automatic revocation, role creation or restore is permitted.

One new developer-only Python module, unit tests, and a disposable local PG17 integration test suffice. Reuse existing strict identifier selection and transport where safe, without changing existing inspector/fixture code or adding dependencies. Do not build a generic policy framework, CLI, desktop integration or automatically learned baseline.

## Authority and meaning

Inputs are explicit application schema names, explicit creating-role names, and an independently supplied expected destination catalog contract. The expected contract is data, not proof of authorization; its provenance/review is caller responsibility. There is NO API that automatically blesses a current destination as safe. A match only means the observed, bounded prerequisite fields match the supplied contract. export_ready, restore_verified and execution_supported remain false even on a perfect match. Mandatory unknowns cover dependency closure, object collisions, archive provenance, role/session configuration values, effective privilege behavior, provider baseline compatibility, snapshot concurrency and managed services. Matching an unsafe caller-supplied expectation cannot authorize a restore.

For public, require an explicit expected presence, owner and semantic namespace ACL. Never assume postgres ownership or silently treat public as empty/default. Fresh application namespaces can be expected absent. Missing creating roles are blockers. There is no generic exemption for provider roles, provider defaults or mismatching schemas.

## Observation contract

Use one REPEATABLE READ READ ONLY transaction, fixed pg_catalog search_path, PG17 enforcement, literal schema/role selections encoded as JSON hex data, never SQL identifiers, patterns or shell arguments. Read no rows, bodies, expressions, passwords, credentials or role-setting values. No DDL or application-function execution.

Inventory only the following bounded prerequisites:

- Connected database named owner and semantic database ACL (NULL expanded with acldefault), plus existence of applicable database-wide settings without their values. Database ownership matters because pg_database_owner has implicit membership not represented by pg_auth_members. Database name may be reported but is not evidence of endpoint identity; no identity authorization is provided by this helper.
- Explicit selected schemas: presence, named owner and semantic namespace ACL, with NULL namespace ACL expanded using acldefault. Preserve schemas with empty ACLs as distinct records.
- All cluster role names and public security attributes: superuser, inherit, create-role, create-database, login, replication, bypass-RLS. Full bounded role/membership observation avoids claiming completeness from only edges adjacent to selected roles. Do not read pg_authid/passwords. Flag existence of role/database settings without reading their values; values remain explicitly unobserved.
- All pg_auth_members edges: named role/member/grantor plus ADMIN, INHERIT and SET booleans. No OIDs in portable comparisons.
- For each explicitly selected creating role: ALL global default-ACL records and ALL defaults in explicitly selected existing schemas, for every catalog object type. Preserve an explicit empty ACL record: it is not equivalent to no altered-default record. Represent global scope structurally (null schema), not a reserved schema-name string. A missing global record means built-in defaults, not an empty permission set. Compare altered-default records exactly rather than inventing a global-default normalization that could hide revocations. Schema-scoped defaults are additive to globals; do not let a scoped record conceal global extra grants.

ACL entries carry named grantor, a tagged grantee (PUBLIC vs named role), privilege and grant-option flag. A real role named "PUBLIC" must not alias the pseudo-grantee. Ownership and membership/default identities are names, not local OIDs. Canonicalize order; reject duplicate/conflicting identities. Preserve empty containers and all relevant records, including grants to roles outside the creating-role list. Namespace ACL and default ACL are not table/column/routine ACL verification; that remains later restore verification.

Use an explicit versioned JSON envelope, exact keys/types, duplicate JSON-key rejection, validated UTF-8 names (1–63 bytes, no NUL), max32 unique schemas/creating roles, bounded raw input/output (2MiB) and max10000 records per family/aggregate ACL entries. Reuse application_inventory.validate_schemas to retain protected-schema restrictions; public remains allowed. Roles may include provider role names because observation is not mutation. Missing/unresolved role identities and unknown unsupported catalog types fail closed. Validate client/server17. Diagnostics must not echo malformed input/catalog content. No file persistence is needed for this slice; avoid new filesystem APIs.

## Comparison/report

Provide small direct functions for SQL generation, strict bounded response validation, observation through existing protected psql transport, and pure comparison with the independently supplied expected contract. Expected and observed selections/version must agree exactly. Compare database owner/ACL/settings-presence, namespace owners/ACLs, full role attributes/settings-presence, membership edges and selected creators' altered-default records independently; stable mismatch categories suffice, not a generic diff framework or SQL remediation generator. Missing selected creator or required schema never passes. Expected-absent schema is represented explicitly, not silently dropped by an inner join.

A report states prerequisite_catalog_matches, fixed false readiness/execution fields, mismatch categories and mandatory unknowns. Invalid inputs throw bounded fixed errors before transport; valid incompatible observations return a negative report. No automatic retries or caller-selected exceptions. Expected and actual shape validation must be equally strict, so a malicious expected payload cannot bypass validation.

The existing hosted transport is reused ONLY for direct psql if an observation wrapper is needed; do not invoke its Bash pipeline. Its native TLS/passfile/no-ambient-secret controls remain unchanged. Do not claim complete process-tree lifecycle or live output bounds from that helper. No hosted call is permitted in this milestone. Local tests patch only the endpoint/transport boundary, never catalog/policy logic.

## Required tests

1. Offline malformed/duplicate/oversized/type/selection tests fail before database access; mismatched expected scope cannot be accepted. Canonical order permutations match.
2. Semantic mutations: namespace owner/ACL grantor/grantee/grant option, role privilege bits, membership grantor/ADMIN/INHERIT/SET, extra global default, scoped extra default, empty-global override versus absent global, creator omission and expected-absent schema unexpectedly present all mismatch. PUBLIC pseudo-grantee differs from a quoted role named PUBLIC.
3. Real disposable socket-only PG17: independently specified compatible baseline matches; the already-proven unapproved GLOBAL SELECT default produces refusal. A schema-scoped expected reader grant must not hide that global grant. Also demonstrate owner/default grant-option/membership drift and empty-default preservation using real catalogs. Do not use a baseline freshly copied from the mismatching target as the only oracle.
4. Public namespace uses explicit stock-PG baseline expectations; changing its owner or ACL produces refusal. Changing the database owner while public remains owned by pg_database_owner must also produce refusal. Label this stock PostgreSQL, not hosted Supabase evidence.
5. Quoted/spaced/Unicode schema and creator names are literal data; similarly named decoys are excluded. Inspect catalogs without application-row/routine-body/role-setting-value canaries appearing in output; defaults in unselected schemas are out of scope by design.
6. Confirm READ ONLY transaction properties and unchanged catalogs/application fixture before/after observation; all readiness flags remain false. Run existing Python regressions including opt-in local PG17 tests.

All test roles/objects are synthetic in newly owned private temporary clusters with no TCP and sanitized environments. No hosted credentials, MCP/browser, Docker, installation, global configuration, resets or existing database access. No mutation of retained fixtures. Cleanup test processes/temp state even on failure. Shell-free execution and bounded test operations; do not claim production safety from a tiny fixture.

## Files and sequence

Terra may create scripts/destination_permissions.py, tests/destination_permissions_test.py, tests/destination_permissions_pg_test.py, and docs/plans/2026-09-20-destination-permission-preflight-results.md. Existing production/tests stay unchanged. Implement TDD, keep APIs minimal, report actual commands/failures/results. Astra independently reviews code/semantics and scope. A lower-model verification pass executes all tests and reports evidence; Astra assesses that evidence without doing implementation/execution. Fixes remain with Terra. No commits/pushes until review clearance; publication must also be assigned to a lower model under the owner's latest routing instruction and separately bounded when ready.
