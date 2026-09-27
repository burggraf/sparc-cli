# SPARC CLI

SPARC (Supabase Project ARChiver) is a Go command-line project for a future Supabase project backup and recovery tool.

> **Current usable slice:** `sparc backup` creates an encrypted local PostgreSQL database archive using an explicitly selected local PostgreSQL 17 macOS arm64 client. It is deliberately marked incomplete: it does not include Auth, Storage, Functions, Vault, project configuration, cluster roles/globals, or provider-managed services. `restore` remains unavailable and hosted Supabase recovery support is not claimed.

## Commands

```sh
sparc help
sparc version
sparc verify --help
sparc backup --help
sparc restore # unavailable
```

`help`, `version`, and `verify` do not access the network. Verification passphrases are requested without terminal echo, or can be read from an explicitly selected private file.

## Create a local encrypted database archive

`backup` needs four explicit inputs: a project ref, a private file containing a qualified Supavisor **session-pooler** URL, a new archive directory below an existing private parent, and the exact `bin` directory of an unbundled PostgreSQL 17 macOS arm64 client. It never accepts a database URL/password in argv and never searches `PATH`. The six client files are privately copied and validated before execution; this is a developer path, not bundled-client or cross-platform support.

```sh
mkdir -m 700 "$HOME/sparc-backups"
./sparc backup \
  --project-ref YOUR_PROJECT_REF \
  --database-url-file "$HOME/Library/Application Support/sparc/hosted-session-url" \
  --archive "$HOME/sparc-backups/database-2026-09-26" \
  --pg-client-bin /absolute/path/to/pg17/bin
```

The command prompts for an archive passphrase without echo unless you add `--passphrase-file /absolute/private/file`. It prints `Database archive created.` and exits **3**, meaning the encrypted archive is intact but deliberately incomplete. Run `sparc verify` with the same passphrase to verify it offline. This command needs separate authorization before it is pointed at any hosted project; it has not been run as a hosted backup in this repository. Remote folders are not supported yet.

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

The separate integration test is also available: `SPARC_TEST_PG_BIN=/absolute/path/to/postgresql-17/bin go test -tags=integration ./internal/database -run '^TestLocalRecoveryRehearsal$' -count=1 -v`. If the variable is unset, it skips. Native Windows runtime for this demo remains unqualified. This workflow does not prove hosted Supabase capture, restore, production client packaging, arbitrary database connections, remote folders, or full-project coverage.

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
