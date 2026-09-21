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
