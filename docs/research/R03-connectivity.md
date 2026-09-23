# R03 — PostgreSQL TLS, route, and project identity

**Status: open.** Public PostgreSQL 17, pgx, and Supabase connectivity documentation
was reviewed on 2026-09-22, and a temporary loopback TLS fixture passed on the
available macOS machine. This qualifies only a local PostgreSQL/TLS mechanism;
it is not Supabase connectivity, provider routing, project identity, or production
client-payload qualification. No Supabase project, credential, or hosted endpoint
was used.

## Repository-local facts

- [Task 09](../03-IMPLEMENTATION-PLAN.md) requires strict connection parameters,
  expected-project routing, refusal of unsupported pooling, version reporting,
  pgx without ambient fallbacks, and independent TLS tests for pgx and bundled
  native clients.
- [R04](R04-database-route.md) sets the **qualification target**, not a support
  claim: PostgreSQL 17 source/target/client majors match; direct or separately
  qualified session-pooler routes only; transaction pooling is blocked. It says
  TLS and project identity must be proved for both client paths.
- Project-level configuration still does not model a database endpoint or
  username. Task 09 adds an in-memory `ConnectionParams` contract and pgx config
  builder; neither is wired into the CLI or persisted. `credentials.Input`
  resolves one explicitly selected source. The passfile helper creates a private
  file; the trusted-tool runner scopes `PGPASSFILE`. This is not permission to
  inherit other `PG*` settings.
- Task 08's Management API client is an offline candidate. It implements neither
  a verified project-detail endpoint nor trusted project-ref-to-database-host
  mapping. Its project list is not evidence that a database connection reaches
  that project.
- `pgx/v5.11.0` is pinned as a Go dependency and used by the Task 09 config
  builder, but no database operation is wired into the CLI and no pgx client
  runtime/platform support is qualified. The packaged libpq/native-client
  payload is also unselected and unqualified (R02).

## Documentation findings (accessed 2026-09-22)

### PostgreSQL 17 / libpq

The [PostgreSQL 17 SSL documentation](https://www.postgresql.org/docs/17/libpq-ssl.html)
confirms that `sslmode=prefer` is the libpq default and can fall back to plaintext;
`require` requires encryption but is not a hostname-verification policy;
`verify-ca` checks the certificate chain without checking the hostname; and
`verify-full` checks both the chain and hostname. SPARC must explicitly require
`verify-full`, not rely on a driver default or use `require`/`verify-ca` as an
identity check.

PostgreSQL 17 also documents `sslrootcert=system` for trust roots supplied by
the SSL implementation. The implementation and locations are platform-dependent.
Otherwise libpq uses its documented root-certificate file behavior. Therefore a
root certificate or system trust source must be selected explicitly and tested;
libpq and Go's certificate pool must not be assumed identical. Supabase documents
a downloadable database root certificate and `verify-full` configuration. The
project-specific certificate source, rotation procedure, and supported-platform
trust policy remain open.

The [connection parameter](https://www.postgresql.org/docs/17/libpq-connect.html)
and [environment variable](https://www.postgresql.org/docs/17/libpq-envars.html)
docs confirm that connection options can come from environment and service
files. For libpq, `host` is the server name used for TLS identity when a separate
`hostaddr` is supplied; replacing the provider hostname with a resolved IP loses
that DNS identity check. A native child must receive a purpose-built allowlisted
environment with route, TLS, and credential-file settings explicit. In
particular, do not inherit `PGSERVICE`, `PGSERVICEFILE`, `PGHOSTADDR`,
`PGPASSWORD`, TLS overrides, or an ambient `.pgpass`.

PostgreSQL also supports GSS encryption, which can be preferred over TLS when
available. Native-client tests intended to qualify TLS must explicitly disable
GSS encryption (for libpq, `gssencmode=disable`) and verify `pg_stat_ssl.ssl`,
not merely observe a successful connection.

### pgx

The current [pgx v5 package documentation](https://pkg.go.dev/github.com/jackc/pgx/v5)
and [v5.11.0 configuration source](https://github.com/jackc/pgx/blob/v5.11.0/pgconn/config.go)
were reviewed. pgx documents the libpq-like `prefer` default and TLS fallback;
its config documentation warns that manually changing `TLSConfig` can leave an
unencrypted fallback in `Fallbacks`. `sslrootcert=system` uses Go's system
certificate pool and selects full verification; explicit CA files are also
supported. The local v5.11.0 checks below cover only the reported config and
loopback cases; platform trust behavior remains open.

`ParseConfig` reads defaults and `PG*` environment settings, can read a selected
service file, and merges service settings with connection-string values. The
parser accepts `servicefile` in its connection string. Its allowed-key option
constrains connection-string keys, not the environment it imports. Explicit
connection fields therefore do not make arbitrary ambient service settings safe.
SPARC must reject or isolate ambient `PG*` inputs and must not accept
caller-supplied `service`/`servicefile` selectors; it must explicitly set route,
TLS, database, user, and credential handling. Keep passwords out of DSNs and
errors; pgx's parse errors are not a substitute for SPARC's fixed public errors.

The module now pins pgx v5.11.0, and offline tests exercise its parser through
`database.NewConnConfig`. A separate disposable PostgreSQL 17.9 loopback fixture
on 2026-09-22 exercised that exact config builder: the trusted direct-route DNS
name was resolved and dialed only to IPv4 loopback, `verify-full` succeeded,
and wrong-CA and wrong-hostname attempts failed. A temporary home directory
contained a mismatched `.pgpass` plus invalid default client-cert/key/root files;
the explicit credential and CA still worked. One preliminary probe set only
`DialFunc`; pgx performed system DNS resolution first and returned NXDOMAIN
without opening a TCP connection. The successful probe set both `LookupFunc`
and `DialFunc` to force loopback, preventing external resolution or connection.
This is local mechanism evidence, not hosted support, project identity, or
platform trust qualification.

A second disposable PostgreSQL 17.9 fixture exercises the bounded inspection
transaction using a config from `NewConnConfig` plus a test-only loopback
resolver/dialer. It observes `server_version_num=170009`, TLS active, and
`transaction_read_only=on`; exact schema presence/missing results cover spaces,
quotes, backslashes, regex punctuation, Unicode, a decoy, and a missing name.
The durable opt-in test also rejects a wrong CA, hostname mismatch, and a
non-TLS connection rejected by the fixture's `pg_hba.conf`. It requires an
approved local `SPARC_TEST_PG_BIN` directory, creates/removes its own cluster
and private certs, and is currently excluded from Windows runtime because it
uses a Unix-domain socket for bootstrap. This is not a broad schema/dependency
inventory or production support evidence.

### Supabase connection and pooler guidance

The current Supabase [Postgres connection guide](https://supabase.com/docs/guides/database/connecting-to-postgres)
and [pooler guide](https://supabase.com/docs/guides/database/connecting-to-postgres#connection-pooler)
were reviewed. The documented route characteristics are:

- The direct database endpoint uses the project reference in a hostname of the
  form `db.<project-ref>.supabase.co`, normally on port 5432. Supabase documents
  direct connections as IPv6-capable by default; its IPv4 add-on changes the
  endpoint's address support and is not a promise of dual-stack behavior.
- The shared session pooler is IPv4-capable and uses a dashboard-provided
  regional/indexed pooler hostname and a project-qualified database username
  (commonly `postgres.<project-ref>`), normally on port 5432. Do not synthesize
  the host from a region or project ref; use the exact project Connect details.
- The transaction pooler is a distinct route (commonly port 6543) with different
  session semantics. It remains blocked by R04. A TLS connection to a pooler
  authenticates the client-to-pooler leg; these docs and this local test do not
  prove pooler-to-database encryption or backend tenant identity.
- Supabase documents `sslmode=require` as an encryption setting and describes
  using the database root certificate with `sslmode=verify-full` for server
  certificate verification. SPARC's stricter policy is `verify-full` with an
  explicit trust source.

These are provider-documented general forms, not a verified endpoint for any
particular project. Exact hosts, usernames, certificates, plan/network
availability, and backend routing still require the project's own current
Connect details and authorized qualification.

## Required SPARC connection contract

These are safeguards and acceptance requirements, not live support claims:

1. The caller supplies an explicit expected project reference and typed route.
   Host, port, database, user, TLS policy, and route kind must not be inferred
   from ambient environment, local service files, shell startup files, or
   fallback credentials. For a pooler, accept the dashboard-provided hostname
   and qualified username; do not construct a regional/indexed hostname.
2. Require `sslmode=verify-full` for pgx and libpq, with an explicitly selected
   CA/system trust source. A missing, wrong, expired, or untrusted CA; hostname
   mismatch; plaintext connection; or weaker verification mode must fail closed.
   Preserve the provider DNS hostname for TLS verification/SNI even when a
   separately qualified address is used. Do not silently downgrade or retry
   with weaker TLS.
3. Explicitly handle ambient settings. For pgx, audit the pinned parser, reject
   or isolate all relevant `PG*` settings (especially service selectors), and
   set every security-sensitive connection option. Do not accept caller-supplied
   `service`/`servicefile`; the builder uses a controlled empty `servicefile`
   value to suppress defaults. For native clients, use an allowlisted child
   environment and a scoped private passfile. Set client-cert behavior explicitly;
   do not load a user's implicit `.postgresql` files.
4. Bind the expected project ref to route data. Direct-route host/ref matching
   and the pooler's dashboard-provided host plus project-qualified user are
   documented mapping candidates, not yet tested proof of backend identity.
   Server version, database name, username alone, TLS hostname alone, or
   successful password authentication cannot establish the intended project.
   A shared-pooler route with no independently checked project binding must be
   refused rather than guessed.
5. Direct and session-pooler routes remain candidates; transaction pooling stays
   blocked. TLS to a pooler alone makes no claim about the pooler-to-database
   leg. Candidate catalog observation must be bounded and read-only, with safe
   search path, statement/lock deadlines, and a fixed application name. Such an
   observation can report version/TLS/schema facts but does not prove project
   identity; no backup/restore may proceed until route and identity checks pass.

## Local disposable fixture evidence

On 2026-09-22, an ephemeral PostgreSQL 17.9 (Homebrew) cluster and private test
CA/server certificate were created under a mode-0700 temporary directory. The
server listened only on loopback, required TLS/SCRAM for TCP, and rejected
non-TLS TCP in `pg_hba.conf`. The certificate covered `localhost`, `127.0.0.1`,
and `::1`. A synthetic role/password and private passfile were used only inside
the fixture. The temporary cluster, certificates, passfile, and logs were removed
on exit. For that original v5.8.0 run, module resolution was forced offline;
no PostgreSQL or Go dependency was installed or added. pgx v5.11.0 was fetched
and pinned later under separate approval, as noted above.

Results:

- Native `psql` 17.9 and cached pgx v5.8.0 each connected with
  `sslmode=verify-full`, the fixture CA, and a matching DNS name; the query
  observed PostgreSQL `server_version_num=170009` and `pg_stat_ssl.ssl=true`.
- Both clients rejected a wrong CA and a hostname mismatch under `verify-full`.
  Both accepted a wrong hostname under `verify-ca`, and both accepted a wrong
  hostname under `require` with no root certificate. This confirms why SPARC
  must use `verify-full` rather than weaker modes.
- For native libpq, `verify-full` with no configured root certificate failed;
  pgx's wrong-root case also failed under `verify-full`. The server rejected
  `sslmode=disable` under its `hostnossl ... reject` rule; this is fixture-side
  refusal, not a claim that an unconfigured client alone enforces TLS.
- Both clients succeeded over loopback IPv4 and IPv6 while retaining `localhost`
  as the TLS identity (`hostaddr` for libpq; a test resolver for pgx).
- In pgx v5.8.0, explicit connection fields kept the selected host, port,
  database, user, TLS mode/root, passfile, and application name despite poisoned
  `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGSSLMODE`, `PGSSLROOTCERT`,
  `PGPASSFILE`, and `PGAPPNAME`. However, `PGOPTIONS="-c search_path=pg_catalog"`
  remained active and changed the server's `search_path` to `pg_catalog` even
  with the other fields explicit. `PGOPTIONS` must be rejected/isolated, not
  assumed neutralized by an explicit DSN. `PGSERVICE`/`PGSERVICEFILE` were not
  exercised; source review confirms they can load a service file and they must
  also be rejected/isolated.

The v5.8.0 native/driver matrix and v5.11.0 local-builder probe are mechanism
results on one macOS host. They do not qualify the selected packaged libpq
payload, system-root behavior, Windows, Supabase routing, pooler semantics, or
project identity. They are not production support claims.

## Qualification status and remaining evidence

| Area | Evidence now available | Still required |
| --- | --- | --- |
| pgx TLS | v5.8.0 local `verify-full` over IPv4/IPv6; v5.11.0 durable local fixture passes explicit-CA `verify-full`, wrong-CA, hostname-mismatch, and non-TLS refusal checks | Qualify system-root behavior and supported-platform trust provisioning; add native Windows runtime coverage |
| libpq TLS | psql 17.9 local `verify-full` positive/negative cases over IPv4/IPv6 | Test the exact selected/signed client payload and supported Windows/macOS runtime behavior |
| Environment | v5.11.0 builder refuses all `PG*`, `SSL_CERT_FILE`, and `SSL_CERT_DIR` variables; parser key allowlist, explicit route/TLS fields, empty passfile/servicefile/client-cert settings, and fixed runtime params are unit-tested. The local fixture also ignored poisoned home passfile/client-cert/root files. | Reproduce as durable opt-in test; qualify native-child environment separately; review process-environment mutation assumptions |
| Route mapping | Current public docs describe direct and shared-pooler forms | Verify actual project Connect values, credential/user binding, and backend identity under separately authorized hosted test |
| Pooler | Public docs distinguish direct/session/transaction routes and their constraints | Test qualified session routing; transaction mode remains refused |
| Catalog/selection | v5.11.0 durable local read-only transaction observes TLS/version and exact literal schema presence/missing names for punctuation, quotes, backslashes and Unicode | Expand only reviewed structural/security/dependency inventory; no general closure claim |
| Trust policy | Explicit private CA works with local pgx v5.8.0 and v5.11.0 fixtures; v5.11.0 system-root parsing is unit-tested only; `require`/`verify-ca` are insufficient | Choose and test CA/system trust provisioning, rotation, and failure behavior on all supported platforms |
| Version | Durable local fixture reports `server_version_num=170009`; unit gate accepts only major 17 | Add durable wrong-major integration refusal and prove gate before capture/restore |

A fake SQL runner or `httptest` cannot establish PostgreSQL wire-protocol TLS,
libpq behavior, provider routing, or tenant identity. No database connection is
yet wired into the CLI, and no public backup/verify/restore command is enabled.

## Sources reviewed

- PostgreSQL 17: [SSL support](https://www.postgresql.org/docs/17/libpq-ssl.html),
  [connection parameters](https://www.postgresql.org/docs/17/libpq-connect.html),
  and [environment variables](https://www.postgresql.org/docs/17/libpq-envars.html).
- pgx: [v5 package docs](https://pkg.go.dev/github.com/jackc/pgx/v5) and
  [v5.11.0 `pgconn/config.go`](https://github.com/jackc/pgx/blob/v5.11.0/pgconn/config.go).
- Supabase: [connecting to Postgres](https://supabase.com/docs/guides/database/connecting-to-postgres)
  and [pooler](https://supabase.com/docs/guides/database/connecting-to-postgres#connection-pooler).
- [R02](R02-client-provenance.md) remains open for the native-client source,
  license, dependency closure, exact payload, and platform trust behavior.

Public documentation review and this local fixture do not authorize hosted
connections. Any project endpoint, credential, network-denial, or source/target
identity test still requires separate exact owner authorization.
