# Client payload research only

`manifest.json` is a research record, **not** a release payload manifest. No
PostgreSQL or EDB archive is present, approved, downloaded, inspected, or
executable from this repository.

The only pinned input is PostgreSQL 17.11 source:

- URL: `https://ftp.postgresql.org/pub/source/v17.11/postgresql-17.11.tar.bz2`
- SHA-256: `dd27f2b3c59e73ed14aa3324901242bf69a032a6347805f274e6260322d42979`

EDB archives are an uninspected control candidate only. Their exact artifact,
digest, architecture, imports, notices, license obligations, and redistribution
status are unverified. Each platform's file list, import closure, minimum OS,
build recipe, signing, and final digest remain explicitly unverified.

Do not add binaries here without separate owner approval and a reviewed final
inventory.
