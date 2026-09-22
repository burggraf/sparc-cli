# R03 — PostgreSQL TLS, route, and project identity

**Status: open.** This is an offline qualification specification, not a finding
that a connection route or driver is safe. No current upstream page was fetched,
no database client was installed or run as part of this work, and no PostgreSQL
server or hosted project was contacted. The exact driver settings, provider endpoints, and
identity checks must be verified before implementation or support claims.

## Repository-local facts

- [Task 09](../03-IMPLEMENTATION-PLAN.md) requires strict connection parameters,
  expected-project routing, refusal of unsupported pooling, version reporting,
  pgx without ambient fallbacks, and independent TLS tests for pgx and bundled
  native clients.
- [R04](R04-database-route.md) sets the **qualification target**, not a support
  claim: PostgreSQL 17 source/target/client majors match; direct or separately
  qualified session-pooler routes only; transaction pooling is blocked. It says
  TLS and project identity must be proved for both client paths.
- The product/security contract requires explicit hostname and trust
  verification, no insecure fallback, no secret in argv/URLs/diagnostics, and
  source/target identity checks ([architecture](../01-PRODUCT-AND-ARCHITECTURE.md),
  [security acceptance](../05-VERIFICATION-AND-SECURITY.md#4-required-acceptance-matrix)).
- Configuration stores non-secret project-reference metadata and explicit
  credential references; it does not yet model a database endpoint or username.
  `credentials.Input` resolves one explicitly selected source. The passfile
  helper creates a private file; the trusted-tool runner places it in the
  operation directory and scopes `PGPASSFILE`. This is not permission to inherit
  other `PG*` settings.
- Task 08's Management API client is an offline candidate. It implements neither
  a verified project-detail endpoint nor trusted project-ref-to-database-host
  mapping. Its project list is not evidence that a database connection reaches
  that project.
- `pgx/v5` is a research candidate, not yet a module dependency or configured
  driver. The packaged libpq/native-client payload is also unselected and
  unqualified (R02). No existing code proves either client's TLS defaults.

## Required connection contract

These are SPARC safeguards and acceptance requirements, not assertions about
current Supabase endpoint syntax:

1. The caller supplies an explicit expected project reference and a typed route
   description. Host, port, database, user, TLS policy and route kind must not be
   inferred from ambient environment, local service files, shell startup files,
   or fallback credentials.
2. The connection must authenticate both the TLS peer and the intended project.
   Certificate-chain trust plus hostname verification proves the endpoint name;
   it does **not** by itself prove that a shared pooler routed to the expected
   backend project. A server version, database name, username, or successful
   password authentication alone is not project identity evidence.
3. Bind the expected project reference to the database host/route using a
   provider-documented, independently checked mapping or a separately reviewed
   explicit confirmation contract. Task 08's unverified list response is not
   sufficient. If a shared pooler cannot provide a qualified project binding,
   refuse the connection rather than guess from a hostname or username pattern.
4. Require chain validation and hostname matching for both pgx and libpq. A
   missing, wrong, expired, or untrusted CA; hostname mismatch; plaintext
   connection; or verification-disabled mode must fail closed. Do not silently
   downgrade or retry with weaker TLS. Do not replace a hostname with a resolved
   IP for certificate checks.
5. Do not assume pgx and libpq use the same trust store or interpret connection
   settings identically. Select a trust-store/provisioning policy only after
   testing both native clients on macOS and Windows. Do not ship a single stale
   provider CA as the only trust source.
6. Direct and session-pooler routes remain candidates pending current provider
   documentation and tests. Transaction pooling stays blocked under R04. TLS to
   a pooler authenticates/encrypts the client-to-pooler leg only; make no claim
   about pooler-to-database encryption or backend identity without separate
   evidence.
7. Preserve the DNS hostname for TLS/SNI while testing IPv4 and IPv6 reachability
   independently. Never treat successful resolution, a reachable IP, or a valid
   certificate for a pooler hostname as project binding.
8. After connection and identity checks, catalog observation must use a bounded
   read-only transaction, explicit safe search path, statement/lock deadlines,
   and a fixed application name. These controls do not replace TLS or routing
   checks.

## Ambient configuration and secret handling

For pgx, construct connection settings from validated explicit fields; do not
pass a partial DSN to a parser that might fill missing values from the process
environment. Audit the selected pgx version before relying on any parser or TLS
helper. No password belongs in argv, a URL logged to diagnostics, a dry-run
string, or public errors.

For native clients, launch only with an allowlisted environment built for that
operation. Remove inherited `PG*` variables and service-file selectors, then set
each approved host/port/database/user/TLS parameter explicitly and select only
the scoped `PGPASSFILE`. Never use `PGPASSWORD`, ambient `.pgpass`,
`PGSERVICE`, `PGSERVICEFILE`, or implicit home-directory connection settings.
The exact required variable names and precedence must be rechecked against the
pinned libpq version before implementation. Keep the passfile alive only for
that operation and report cleanup failure without exposing its path or contents.

A read-only SQL transaction is a secondary safeguard, not a substitute for using
a least-privileged account. No connection check may change source settings,
roles, network bans, passwords, or project state. Avoid probes that read secrets
or rely on provider-internal tables/settings.

## Qualification matrix — all evidence still open

| Check | Required positive case | Required negative case | Current evidence |
| --- | --- | --- | --- |
| pgx TLS | Local controlled PostgreSQL endpoint with a trusted CA and matching SAN | Wrong CA, untrusted/expired certificate, wrong hostname, and plaintext/verification disabled | None for SPARC |
| libpq TLS | Same fixture and policy through the exact pinned native client | Same negative cases, tested independently from pgx | No client payload selected; none for SPARC |
| Endpoint binding | Approved expected ref maps to the exact direct or qualified session route | Same-version, same-database decoy project/endpoint and mismatched ref refused before work | No verified mapping available |
| Pooler routing | Documented session route preserves the tested behavior and project binding | Transaction route and unknown/shared route refused | Current route semantics not rechecked |
| Network families | Matching hostname and TLS work on supported IPv4 and IPv6 paths | DNS/address failure is reported without weakening verification or changing identity | Not tested |
| Ambient settings | Hostile `PG*`, service-file, proxy, and default-database settings do not alter selected route or TLS | Remove one explicit required setting and verify fail-closed behavior | No database client integration exists |
| Version | Report actual server major and compare with the qualified PG17 policy | Wrong major blocks before capture/restore | No connection implementation exists |

A fake SQL runner or an `httptest` server cannot establish PostgreSQL wire-protocol
TLS, libpq behavior, provider routing, or tenant identity. Unit tests should first
cover strict field parsing, route classification, config defaults, error
redaction, and refusal behavior. Only later, with approved tools, can an
owned local TLS-enabled PostgreSQL fixture exercise the protocol. The pgx and
native-client cases must be separate; a passing pgx test says nothing about
libpq.

## Evidence still required to close R03

1. Recheck and cite the exact PostgreSQL 17/libpq TLS and connection-environment
   rules, the exact pinned pgx version's TLS/configuration behavior, and current
   Supabase direct/session/transaction connection guidance. The existing source
   register identifies PostgreSQL P4 and Go G2 as leads; these pages were not
   refreshed for this record.
2. Define the canonical database endpoint/project-ref binding for direct and
   session routes, including credential/user routing, shared-pooler behavior,
   hostname/SAN expectations, and any verified provider-side identity check.
3. Choose and test a CA/trust provisioning policy across supported macOS and
   Windows versions for both drivers; prove wrong/missing trust fails closed.
4. Run the matrix above against an owned local disposable PG17 TLS fixture and
   exact native client build. Installation/build of PostgreSQL tools or
   provisioning machines requires separate owner approval.
5. Keep hosted endpoint, source/target identity, or network-denial tests blocked
   until fresh exact project/scope/timing authorization. This document does not
   authorize a live connection.

## Source leads to recheck (not fetched for this record)

- PostgreSQL 17 [libpq SSL support](https://www.postgresql.org/docs/17/libpq-ssl.html),
  [connection parameters](https://www.postgresql.org/docs/17/libpq-connect.html),
  and [environment variables](https://www.postgresql.org/docs/17/libpq-envars.html)
  (the existing source register's P4 points to the current SSL and passfile
  pages; version-specific behavior must be confirmed).
- [pgx v5 package/source](https://pkg.go.dev/github.com/jackc/pgx/v5), listed as
  G2 in the research ledger; first select and pin the exact module version.
- Supabase [connecting to Postgres](https://supabase.com/docs/guides/database/connecting-to-postgres)
  and its current pooler/TLS guidance. These are source leads only; no page was
  fetched or treated as evidence here.
- [R02](R02-client-provenance.md) for the still-unresolved native-client source,
  license, dependency closure and platform trust work.
