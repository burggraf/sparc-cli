# Application dependency inspection — direct catalog evidence

Status: authorized local-only dependency gate. Astra owns design and review. Terra owns implementation, all test execution, and publication after clearance. No worktrees, hosted actions, installs or general restore authorization.

## Goal and bounded interpretation

Observe concrete catalog dependencies of objects in explicitly selected application schemas. Identify external application-schema references, references to protected/managed schemas, extension requirements and unresolved references. Do not silently omit them or automatically expand the selected schema set.

This is direct catalog dependency evidence, not dependency closure or a SQL-body analyzer. A report with no external edges is NOT proof that an archive is self-contained. Runtime SQL, string-bodied routines, external effects and unsupported object families remain mandatory unknowns. All execution/export/restore/dependency-complete flags remain false. The next recovery layer must explicitly refuse unresolved blockers or verify a separately authorized prerequisite; this helper never grants that authority.

## Minimal implementation

Add scripts/application_dependencies.py, two test files and an evidence document. Reuse application_inventory.validate_schemas and the existing protected direct-psql transport; do not modify old modules/tests. No graph framework, recursive closure engine, general restore API, auto remediation, package dependency or file-persistence API.

Public APIs should be small: SQL generation, bounded strict response validation, assessment and observation. Literal input names travel only as encoded JSON hex data, never SQL interpolation as identifiers/patterns. Observation is one PG17 REPEATABLE READ READ ONLY transaction with fixed pg_catalog search_path. No rows, bodies, constraint/default expressions, role-setting values, foreign-server options or secrets are read. Diagnostics never echo untrusted catalogs/inputs. Use the existing native transport only for direct psql, not old Bash fixture pipelines.

## Covered sources and edges

Define the catalog coverage explicitly in the report. For this gate, include dependencies originating from selected:

- relations and their columns (pg_class, including pg_depend objsubid), including tables/views/materialized views/sequences/partitions;
- routines (pg_proc), standalone/custom types and domains (pg_type), constraints (pg_constraint);
- column defaults/generated expressions (pg_attrdef), view rewrite rules (pg_rewrite), policies (pg_policy), triggers (pg_trigger), attributed to their owning selected relation;
- selected schema objects (pg_namespace).

Use pg_depend for recorded direct references, retaining the dependency type. Also inspect pg_inherits explicitly for cross-schema table inheritance/partition parent references, rather than assuming every semantic parent edge is represented as the same pg_depend record. Include extension membership for covered selected objects AND covered reference targets via pg_depend-to-pg_extension; an extension-owned object in pg_catalog is not automatically a safe builtin. Relations' columns and auxiliary objects must resolve to their actual namespace, not be dropped merely because pg_identify_object reports no schema for that catalog class.

Known catalog dependencies can include a view's rewrite rule, a default's sequence, a parsed SQL routine's references, policy/helper function, trigger function, external enum/domain, foreign key and partition parent. Preserve references to unselected application schemas and protected schemas as findings, even though protected schemas cannot themselves be selected as application inputs.

Dependencies among selected schemas are internal observations, not blockers solely due to crossing those selected namespaces. Do not recurse through external schemas, read their data, execute their functions or automatically include them. Core catalog references may be classified as system prerequisites, NOT silently authorized. Missing schema attribution or unsupported catalog identities remain explicit unresolved findings. Keep dependency direction clear (dependent -> referenced); omit neither source nor target because the other lies outside selection.

## Identity and response integrity

Report stable structured metadata: source and target catalog kind/object kind, actual namespace name where attributable, canonical object identity (including routine argument identity or column subidentity), dependency type and extension association where applicable. pg_identify_object may help with canonical identity but its schema field is quoted SQL text; do NOT compare it directly with raw namespace names. Use catalog joins/quote_ident-aware attribution and test both `a b` and a distinct namespace whose literal name includes surrounding quotes. Do not split human-readable identity strings on dots/newlines. Include reference targets that are namespace or extension objects even when the generic schema field is null.

OIDs may be used inside SQL for joins; do not use database-local OIDs as portable report identities. If an unsupported identity must retain opaque diagnostic information, label it unresolved/local-only explicitly, never claim portability. Decide the smallest concrete JSON structure in implementation and document it. Do not include identity text capable of exposing bodies/expressions/options; constrain supported identification paths and mark unsupported paths unresolved.

Validate explicit version, PG17 server version, exact envelope/record keys and scalar/container types, names as valid UTF-8 and bounded identity strings, supported dependency tags/catalog-kind values (or an explicit unresolved representation), duplicate/conflicting identities and source selection attribution. Validate JSON duplicate keys, invalid encoding/constants/depth, maximum 2MiB raw response and maximum10000 records per family. Keep selected schema presence explicit and fail closed when a requested schema is absent. Invalid input fails before transport. If accepting dictionaries as well as bytes, apply exact type validation, fixed errors, consistent size checking and fresh copies; simplest is bytes-only at the validation boundary. Post-serialization bounds must not be described as allocation bounds.

Assessment produces deterministic findings and mandatory unknowns. It does not mutate caller inputs, invent an allowlist from the destination, treat external dependencies as warning-only readiness, or convert absence of evidence into proof of completeness.

## Local tests and acceptance

Use only owned private socket-only synthetic PG17 clusters and the already-reviewed cleanup_owned_cluster helper from destination_permissions_pg_test.py (test-only import). No TCP, ambient credentials or shell. Verify PG17 tools/server, run with SPARC_TEST_PG17_BIN=/opt/homebrew/opt/postgresql@17/bin and PYTHONDONTWRITEBYTECODE=1. Confirm shutdown before removing owned data; quarantine on uncertainty. No modification of global PostgreSQL/Homebrew installation.

Offline tests: invalid selections rejected before transport; exact malformed/duplicate/type/size/unresolved identity cases; dependency direction and deterministic assessment; internal vs external vs protected classification; extension ownership never overridden merely by pg_catalog location; false readiness and mandatory unknowns always retained.

Real synthetic fixture must establish:

1. Selected A table references unselected B parent (FK); A column uses B enum/domain; A default calls B sequence; A view/rewrite and parsed SQL routine reference B; A policy and trigger use B helper functions; A partition/inheritance child depends on B parent. Assert actual observed edges/identities, not only number of findings. Helpers remain synthetic and are never invoked by inspection.
2. Selecting both A and B reclassifies their mutual edges as internal, but does NOT make the report ready/complete. Namespace names with spaces, quotes/Unicode and similarly named decoys prove correct attribution, including literal quote-bearing namespace identity.
3. A synthetic auth schema/table reference is flagged as managed/protected external dependency. Label it a local stand-in, not hosted Auth evidence. A local installed extension (e.g. hstore, already provided by the approved PG17 installation) supplies a reference/member case; do not install OS packages/download extensions. Include an extension-owned test object in pg_catalog if locally legal to prove namespace alone does not suppress its requirement, or escalate a concrete alternative if not legal.
4. A string-bodied SQL/PLpgSQL routine referring to B need not expose its runtime reference in pg_depend. Demonstrate that limitation and retain unknowns; do not parse/search body text or claim completeness from its missing edge.
5. Application-row/body/expression/option canaries do not appear in observed output. Test actual read-only/repeatable-read settings in the observation transaction and compare bounded fixture/catalog snapshots before/after observation. Do not use a regex over emitted SQL as the only nonmutation proof.
6. Missing selected schema fails explicitly. Report catalog coverage, known unsupported/dynamic dependencies and exact actual results. Keep old69-test regressions; full new total determined from real execution, not guessed.

No general capture/restore attempt in this milestone. No dependency closure, effective permission verification, arbitrary roles, provider compatibility, TLS handshake, snapshot coordination, encryption integration or scale claim. Dependency observation is necessary but not sufficient for a supported native recovery profile.

## Files, review and execution

Terra may create only scripts/application_dependencies.py, tests/application_dependencies_test.py, tests/application_dependencies_pg_test.py and docs/plans/2026-09-20-application-dependency-inspection-results.md. Parent design is read-only. Implement focused TDD and real local tests, then report evidence. Fresh Astra reviews semantics/coverage and safety without executing tests. A separate Terra verification run captures the full unittest log plus immediate exit-status receipt in newly owned private retained files so output truncation cannot erase completion evidence; compare source hashes before/after. No publication until both accepted. Commits/README/handoff/push are later assigned to Terra under bounded authority. No other private files, hosted/database/browser/MCP calls, credential access, installations, resets or global changes.
