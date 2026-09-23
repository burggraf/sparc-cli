# SPARC CLI

SPARC (Supabase Project ARChiver) is a Go command-line project for a future Supabase project backup and recovery tool.

> **Limited local milestone:** `sparc verify` checks encrypted archive integrity offline. It does not prove complete project coverage or recoverability. `backup` and `restore` remain unavailable, and hosted Supabase support is not claimed.

## Commands

```sh
sparc help
sparc version
sparc verify --help
sparc backup  # unavailable
sparc restore # unavailable
```

`help`, `version`, and `verify` do not access the network. Verification passphrases are requested without terminal echo, or can be read from an explicitly selected private file.

## Try the local recovery rehearsal

This **developer-only integration workflow**, not the product backup command, creates two disposable databases in a new local PostgreSQL 17 cluster, dumps a synthetic schema, encrypts and verifies an archive, deletes the source database, then restores rows and sequence state into the fresh target. It contacts no hosted project. You need Go and trusted local PostgreSQL 17 binaries (`initdb`, `postgres`, `pg_ctl`, `psql`, `pg_dump`, `pg_restore`). Run as a normal user, not root:

```sh
SPARC_TEST_PG_BIN=/absolute/path/to/postgresql-17/bin go test -tags=integration ./internal/database -run '^TestLocalRecoveryRehearsal$' -count=1 -v
```

On macOS with Homebrew PostgreSQL 17, use `/opt/homebrew/opt/postgresql@17/bin` if that path exists. On Windows PowerShell, set `$env:SPARC_TEST_PG_BIN` to your approved PostgreSQL 17 `bin` directory, then run the same `go test` command. **If `SPARC_TEST_PG_BIN` is unset, the test skips; a green skipped test proves nothing.** By default it uses temporary files and removes the archive. On macOS/Unix, to keep a synthetic encrypted archive for the offline CLI check, create a private directory and set `SPARC_TEST_ARCHIVE_OUT` to a new path inside it:

```sh
mkdir -m 700 "$HOME/sparc-demo"
umask 077
printf '%s' 'synthetic-rehearsal-passphrase' > "$HOME/sparc-demo/passphrase"
SPARC_TEST_PG_BIN=/absolute/path/to/postgresql-17/bin \
SPARC_TEST_ARCHIVE_OUT="$HOME/sparc-demo/archive" \
  go test -tags=integration ./internal/database -run '^TestLocalRecoveryRehearsal$' -count=1 -v
go build -o ./sparc ./cmd/sparc
./sparc verify --archive "$HOME/sparc-demo/archive" --passphrase-file "$HOME/sparc-demo/passphrase"
```

The demo archive is synthetic and explicitly **incomplete**; `verify` prints `Integrity: passed` and exits 3 to signal incomplete capture. Never reuse the demo passphrase. This narrow local proof does not establish Supabase support, production client packaging, safe arbitrary restores, or full-project coverage.

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
