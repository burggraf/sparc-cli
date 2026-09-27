# Embedded PostgreSQL client payload

The macOS arm64 `sparc` executable embeds a PostgreSQL 17.11 client package and its runtime closure. At first client use, SPARC verifies the embedded archive and each extracted file, then installs it below its private user cache. No PostgreSQL/OpenSSL installation, `PATH` lookup, network download, or separate client path is needed.

The owner authorized bundling this exact client/runtime set into the macOS arm64 app with provenance and notices. This does not authorize claiming support for macOS amd64, Windows, or untested macOS releases. The recorded Mach-O deployment metadata is 13.0; runtime evidence is only from the current macOS 27 arm64 host. Signing and notarization remain open.

## Provenance

- PostgreSQL 17.11 source: `https://ftp.postgresql.org/pub/source/v17.11/postgresql-17.11.tar.bz2`, SHA-256 `dd27f2b3c59e73ed14aa3324901242bf69a032a6347805f274e6260322d42979`.
- OpenSSL 3.5.8 source: `https://github.com/openssl/openssl/releases/download/openssl-3.5.8/openssl-3.5.8.tar.gz`, SHA-256 `a8f84a39918ec6415ce765d9b429d313ba97b8143169c172e734b9514464f5b2`. The source archive digest was verified; its detached signature was not verified because GPG was unavailable.
- `manifest.json` records build inputs, payload file digests, runtime imports, archive digest, and the precise qualification limits.
- `sparc licenses` prints the embedded PostgreSQL and OpenSSL notices, including PostgreSQL's bundled regex/Tcl notices. The same notice file is in the payload package.

The payload is macOS arm64 only. The embedded clients load bundled `@rpath` libpq/OpenSSL files and macOS system `libSystem`/`libz`; no Homebrew path is required at runtime. No hosted backup has been performed.
