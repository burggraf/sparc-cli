# SPARC CLI

SPARC (Supabase Project ARChiver) is a Go command-line project for a future Supabase project backup and recovery tool.

> **Scaffold only:** this repository is not yet a backup tool. `backup`, `verify`, and `restore` are recognized commands but deliberately return an unavailable error. They do not back up, verify, or restore anything.

## Current command shell

```sh
sparc help
sparc version
sparc backup  # unavailable
```

`help` and `version` run offline and do not access the network or filesystem.

## Try the local recovery rehearsal

This **developer-only integration test**, not a CLI backup command, creates two disposable databases in a new local PostgreSQL 17 cluster, dumps a synthetic schema, encrypts and verifies an archive, deletes the source database, then restores the rows and sequence into the fresh target. It contacts no hosted project. You need Go and trusted local PostgreSQL 17 binaries (`initdb`, `postgres`, `pg_ctl`, `psql`, `pg_dump`, `pg_restore`). Run as a normal user, not root:

```sh
SPARC_TEST_PG_BIN=/absolute/path/to/postgresql-17/bin go test -tags=integration ./internal/database -run '^TestLocalRecoveryRehearsal$' -count=1 -v
```

On macOS with Homebrew PostgreSQL 17, use `/opt/homebrew/opt/postgresql@17/bin` if that path exists. On Windows PowerShell, set `$env:SPARC_TEST_PG_BIN` to your approved PostgreSQL 17 `bin` directory, then run the same `go test` command. **If `SPARC_TEST_PG_BIN` is unset, the test skips; a green skipped test proves nothing.** The test uses temporary local files, synthetic credentials and data; it does not produce a persistent backup. This narrow schema-only proof does not establish Supabase support, production client packaging, safe arbitrary restores, or full-project coverage.

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
