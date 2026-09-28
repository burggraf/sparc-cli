# SPARC CLI

SPARC (Supabase Project ARChiver) is a Go CLI for encrypted Supabase database backups. Full-project recovery is not yet supported.

> **Current usable slice:** On macOS arm64, `sparc backup` creates three separately encrypted archive-v1 components: schema-only SQL for `public`, data-only SQL for `public`/`auth`/`storage`, and a versioned incomplete recovery profile. PostgreSQL 17.11 is bundled; no separate PostgreSQL or OpenSSL installation is needed. The profile records source identity/public-schema owner, fixed exclusions, extension inventory, and archive-derived table fingerprints; public schema creation is made idempotent. Non-public application schemas, managed Auth/Storage DDL, custom roles, a shared source snapshot, complete archive-derived fidelity, Storage object bytes, project settings, and provider services remain unqualified. The experimental split restore is not exposed; `restore` remains unavailable, and other platform payloads are not yet bundled.

## Commands

```sh
sparc help
sparc version
sparc verify --help
sparc backup --help
sparc licenses
sparc restore # unavailable
```

`help`, `version`, `verify`, and `licenses` do not access the network. Verification and backup passphrases are requested without terminal echo, or can be read from an explicitly selected file.

## Create a local encrypted database archive

`backup` needs a project ref, a readable file containing the qualified Supavisor **session-pooler** URL (port 5432), and a new archive directory below an existing local directory you can write to. Paths may be relative to the current directory. The macOS arm64 executable contains PostgreSQL 17.11 `pg_dump` and its required runtime libraries; SPARC extracts and validates them in its private cache. No Homebrew PostgreSQL, OpenSSL install, or `PATH` lookup is required. The URL/password never enter argv.

```sh
mkdir -p backups
ARCHIVE="./backups/database-$(date +%Y%m%d-%H%M%S)"
./sparc backup \
  --project-ref YOUR_PROJECT_REF \
  --database-url-file .env.db \
  --archive "$ARCHIVE"
```

The command prompts twice for an archive-encryption passphrase without echo unless you add `--passphrase-file .passphrase`. It prints `Database archive created.` and exits **3**, meaning the encrypted archive is intact but deliberately incomplete. Eligible Auth/Storage table rows are included; fixed migration and Storage vector tables are excluded. The profile states that custom roles, non-public application schemas, common snapshot consistency, and several service-level requirements are missing or unqualified. Run `./sparc verify --archive "$ARCHIVE"` with the same passphrase to verify it offline. A hosted backup reads database contents and needs the operator's authorization. Remote folders are not supported yet. Run `./sparc licenses` to print the bundled PostgreSQL and OpenSSL notices.

## Rehearse restoring a legacy archive locally

Homebrew PostgreSQL 17 server tools can test whether a legacy custom-format SPARC database component such as `./tmp/test`
restores on this Mac. Split-profile archives are not yet accepted by this developer-only restore path. The Homebrew service need not be running: this developer-only
command starts a fresh temporary cluster on a private Unix socket, verifies the
archive, restores into a new database, checks the catalog is readable, and stops
and removes the cluster. It does **not** alter an existing PostgreSQL service,
connect to Supabase, compare the hosted source, or establish full-project coverage.
A restore executes SQL from the archive as the local OS user; use only an archive
you trust. Role/extension prerequisites may cause the strict restore to fail.

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  go run -tags=localdemo ./cmd/sparc-localrestore --archive ./tmp/test
```

Enter the **existing archive passphrase** when prompted; never put it in the
command line or chat. The command uses `/opt/homebrew/opt/postgresql@17/bin` and
requires it to be installed. A passing synthetic test does not mean your archive
was restored: run the command above on your actual archive to learn that result.
For a read-only inventory of the encrypted dump (no server or network), run
`go run -tags=localdemo ./cmd/sparc-localrestore --archive ./tmp/test --inspect`
and enter the archive passphrase. It prints only TOC counts; these are not proof
that the target can accept every object. If PostgreSQL rejects a restore and the
fixed error category is inconclusive, rerun with `--show-postgres-error` to see
the first 64 KiB of raw diagnostic on
your terminal. It may contain SQL, identifiers, or other sensitive data: **do
not paste that output into chat or logs**. This option refuses redirected stderr.

## Disposable hosted restore experiment

`go run -tags=localdemo ./cmd/sparc-hostedtry` is a separate developer-only
command for an explicitly disposable Supabase project. It verifies the archive,
checks that `public` is empty, and defaults to an atomic, stop-on-first-error
`pg_restore`. Fresh Supabase projects already contain managed schemas, so this
normally rejects `CREATE SCHEMA auth`. With `--continue-on-error`, `pg_restore`
continues past errors **without a transaction**: failures can leave the target
partially changed or its managed services inconsistent. This is a destructive
experiment, not `sparc restore`; delete and recreate a failed target before
retrying. No mode uses `--clean` or drops schemas. Neither mode proves complete
project recovery, Storage bytes, or source fidelity. Never put database or
archive secrets in argv or chat. `--show-postgres-error` shows bounded raw SQL
errors only on the local terminal; do not paste them. For diagnosing a
partially populated disposable target, `--auth-table sessions` selects only
one allowlisted Auth table's data in a single transaction and refuses a
nonempty destination table. Other supported names are `identities`,
`refresh_tokens`, `mfa_amr_claims`, and `one_time_tokens`. It does not
resolve cross-table dependencies, restore Auth settings/root keys, or claim
recovery when any required table remains absent.

## Try developer-only backup/restore

`sparc-localdemo` is a separate developer-only build target; it is not linked into normal `sparc` release builds. It creates a fresh temporary PostgreSQL 17 cluster listening only on `127.0.0.1`, creates fixed synthetic source/target databases, streams one schema into a private encrypted archive, deletes the source, and restores/checks rows and sequence state in the fresh target. No database URL is accepted, and no hosted project is contacted. The demo cluster uses trust authentication and TLS off only because it contains synthetic data and is loopback-only; this is not a connection mode for external databases. The encrypted archive remains at the requested path and is declared incomplete.

You need Go and an explicitly selected local PostgreSQL 17 `bin` directory containing `initdb`, `postgres`, `pg_ctl`, `psql`, `pg_dump`, and `pg_restore`. Run as a normal user, not root. Create a private output parent first; the command prompts for the passphrase without echo and refuses to overwrite an existing archive:

```sh
mkdir -m 700 "$HOME/sparc-demo"
SPARC_TEST_PG_BIN=/absolute/path/to/postgresql-17/bin \
  go run -tags=localdemo ./cmd/sparc-localdemo --archive "$HOME/sparc-demo/archive"
```

On macOS with Homebrew PostgreSQL 17, use `/opt/homebrew/opt/postgresql@17/bin` if that path exists. Then verify the retained archive offline (it prompts for the passphrase again and exits 3 because the capture is intentionally incomplete):

```sh
go build -o ./sparc ./cmd/sparc
./sparc verify --archive "$HOME/sparc-demo/archive"
```

The separate integration test is also available: `SPARC_TEST_PG_BIN=/absolute/path/to/postgresql-17/bin go test -tags=integration ./internal/database -run '^TestLocalRecoveryRehearsal$' -count=1 -v`. This developer-only fixture needs local PostgreSQL server tools; the normal `sparc backup` command does not. If the variable is unset, the test skips. Native Windows runtime remains unqualified. This rehearsal does not prove hosted Supabase capture, restore, arbitrary database connections, remote folders, or full-project coverage.

## Development

This scaffold requires Go 1.25 or later:

```sh
go test ./...
go vet ./...
go build ./cmd/sparc
```

## Contribution hygiene

Before opening a pull request:

- [ ] Run `go test ./...` and `go vet ./...`.
- [ ] Do not commit credentials, `.env` files, passfiles, keys, archives, project metadata, or raw diagnostics.
- [ ] Keep fixtures synthetic and sanitized; inspect the diff for secrets and generated build output.

## License

SPARC CLI is licensed under the [MIT License](LICENSE).
