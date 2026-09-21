# R02 — client provenance (Task 02)

**Status:** research incomplete; no client payload is approved or shipped.

## Verified source pin

The current PostgreSQL 17 research input is official source, not a selected
client binary payload:

- Version: PostgreSQL 17.11
- URL: <https://ftp.postgresql.org/pub/source/v17.11/postgresql-17.11.tar.bz2>
- SHA-256: `dd27f2b3c59e73ed14aa3324901242bf69a032a6347805f274e6260322d42979`
- Verification basis: the official PostgreSQL 17.11 source directory and its
  SHA-256 sidecar, checked for this Task 02 record on 2026-09-21.

EDB PostgreSQL binary archives are retained only as an **uninspected control
candidate** (<https://www.enterprisedb.com/download-postgresql-binaries>). No
EDB archive, direct artifact URL, checksum, architecture slice, dependency tree,
notice file, license conclusion, or redistribution conclusion was inspected.

## Explicitly unverified payload facts

For macOS arm64, macOS amd64, and Windows amd64, all of the following remain
unverified: actual shipped files, architecture/load commands, import/runtime
closure, minimum OS, build recipe, license/notices, signing identity/status,
and post-signing digests. `build/clients/manifest.json` deliberately contains no
payload file entries and must not be treated as approval to build, download,
or distribute one.

## Next owner-gated evidence

After approval to acquire an exact source/vendor artifact, inspect it before
execution; record recursive files/imports, licenses/notices, architecture,
minimum OS, final signed digests, and exercised TLS dump/restore closure on each
native target. No Homebrew-installed client is a release payload candidate.
