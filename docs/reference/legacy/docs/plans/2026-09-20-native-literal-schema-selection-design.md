# Native PostgreSQL literal schema selection

Status: authorized local-only next gate. Astra designs/reviews; Terra implements, executes tests and (after clearance) publishes. No hosted mutation or general recovery approval.

## Goal and smallest implementation

Prove that native PG17 schema filters select exactly the caller's explicitly named application schemas, not patterns or similarly named neighbors. Add one small pure helper returning native pg_dump schema-selection arguments, plus offline tests and a real disposable local round-trip test. Do not build a dump/restore executor, subprocess framework, archive format, dependency scanner or GUI integration in this slice.

Reuse application_inventory.validate_schemas for 1–32 unique UTF-8 schema names, 1–63 bytes, no NUL, and existing protected-schema exclusions. Do not loosen that validator or copy it. Fail before any invocation on invalid selection. Returned selector arguments include --strict-names and one --schema=<literal pattern> per selected name in deterministic order. Pass them through a direct argv array, never shell expansion. PostgreSQL selectors remain patterns even without a shell: quote each entire name according to PG17 pattern semantics, including embedded double quotes, rather than using shell quoting. Wrapping names in double quotes and doubling interior quotes is a candidate implementation, not evidence; verify against real clients, especially backslashes and regex punctuation. Use a fixed bounded error if needed, not input echoing.

The helper produces selector arguments only; it does not establish version, transport, credential safety, dependency completeness or restore readiness. No readiness flags need be added to a pure argument helper. Existing inspector/preflight flags remain unchanged and false. Missing-schema refusal is supplied by the client via --strict-names, and must be proved for a mixed list containing both existing and nonexistent names. Do not claim no partial output on client failure; the test must keep failed outputs private and never publish them.

## Local qualification matrix

Use an explicitly opted-in PG17 installation at /opt/homebrew/opt/postgresql@17/bin. Assert actual pg_dump, pg_restore and server major versions. One new private socket-only temporary cluster, synthetic source database, fresh target(s), sanitized environment, private HOME/socket paths, no TCP. Local bootstrap privileges may seed/capture/restore this selection-only experiment; disclose that it does not retest restricted-role permission equivalence or role restoration. Existing permission experiment is separate evidence.

Seed small selected namespaces with one ordinary table and a unique synthetic marker each, plus unselected decoy namespaces with similar names and separate markers. Cover at least ordinary names, mixed case, spaces, embedded and literal surrounding double quotes, Unicode, dots, backslashes, *, ?, brackets, parentheses, |, +, $, commas, leading hyphens/option-looking names, a line break, and exact 63-byte UTF-8 name boundaries. Keep batches within 32. Names containing shell-like syntax must remain ordinary data; no shell is invoked. Use safe SQL identifier/data construction for synthetic setup.

Run a single schema-and-data custom-format pg_dump per supported test batch using the helper's actual arguments and --strict-names; restore that archive into a fresh disposable target with --single-transaction --exit-on-error. No --no-owner/--no-acl/--clean/trigger disabling or permissive error handling. Verify exact schema-qualified table names and marker rows from destination catalogs/query output using JSON or another unambiguous structured encoding, not pg_restore TOC text/line parsing (identifiers can contain newlines). Compare the full application namespace/table additions relative to the fresh target's baseline; asserting only that selected objects are present is insufficient because decoys could also be included. Preserve and verify the preexisting public namespace rather than mistaking it for leakage.

Required negatives:
- Invalid identifier selection never produces invocation arguments or touches a database.
- One missing name among existing selections causes a nonzero dump result, not a seemingly complete archive. No restore follows failed capture.
- An unescaped wildcard selector demonstrably includes an unwanted decoy in a deliberately isolated diagnostic run, or another focused RED case establishes why exact quoting matters. Record real observations; don't fabricate TDD history.
- No similarly named decoy appears after a successful literal-selection round trip. Test embedded double quote and backslash combinations against real PG17, not only string equality.

A schema with cross-schema dependencies is not promised to be restorable by this gate. Do not add incomplete dependency checks to make the helper look ready. Dependency inventory/refusal is the next distinct gate; Auth/Storage/provider/public behavior, concurrent snapshots, endpoint TLS, encrypted archive integration and large scale remain unproven.

## Lifecycle and scope

Reuse the already-reviewed stop_owned_cluster/cleanup_owned_cluster from tests/destination_permissions_pg_test.py for test shutdown (a test-only import is acceptable). No production dependency on test helpers, no general abstraction extraction or old-file modifications. Use explicit ownership, not unconditional TemporaryDirectory removal around a potentially live cluster. Confirm fast/immediate pg_ctl shutdown before removal; quarantine on uncertainty. Test commands have deadlines and bounded displayed diagnostics. If subprocess output is size-checked only after collection, document that honestly rather than claiming allocation bounds. Archives stay inside the private root and are removed only with known-stopped test state.

No hosted credentials/configuration or private operator files, browser/MCP, Docker, installs, external services, old fixture execution, project resets or global configuration. No edits to existing modules/tests or desktop. Source checkout on main; no worktrees; one writer.

## Allowed files and acceptance

Terra may create scripts/native_schema_selection.py, tests/native_schema_selection_test.py, tests/native_schema_selection_pg_test.py, and docs/plans/2026-09-20-native-literal-schema-selection-results.md. This design is parent-owned; do not edit it. Keep the helper to the smallest correct implementation and document its supported PG17-only evidence.

Implement focused offline tests and real matrix, run all Python tests with SPARC_TEST_PG17_BIN and PYTHONDONTWRITEBYTECODE=1, report counts/skips/cleanup and actual RED/GREEN evidence. Fresh Astra review checks PostgreSQL pattern correctness, exact restored-inventory assertions, strict missing-name behavior and isolation. Separate Terra verification runs the full suite and checks reviewed source hashes unchanged. Publication only after acceptance, assigned to Terra in a separately bounded handoff. Do not promote this gate to general database support.
