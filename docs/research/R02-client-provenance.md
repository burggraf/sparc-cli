# R02 — client provenance (Task 02)

**Status:** local macOS arm64 source candidate is under inspection; no release
client payload is approved or shipped. Redistribution remains a separate gate.

## Verified source pin

The current PostgreSQL 17 research input is official source, not a selected
client binary payload:

- Version: PostgreSQL 17.11
- URL: <https://ftp.postgresql.org/pub/source/v17.11/postgresql-17.11.tar.bz2>
- SHA-256: `dd27f2b3c59e73ed14aa3324901242bf69a032a6347805f274e6260322d42979`
- Verification basis: the official PostgreSQL 17.11 source directory and its
  SHA-256 sidecar, checked for this Task 02 record on 2026-09-21.

## Authorized local acquisition (macOS arm64 only)

On 2026-09-25 the operator explicitly approved acquiring the pinned official
source, verifying its digest, inspecting dependencies/licenses, building a
local macOS arm64 candidate, and exercising it on disposable local fixtures.
This does **not** authorize redistribution or hosted access. The HTTPS download
contained 21,787,224 bytes and matched the SHA-256 above before extraction;
7,718 archive entries were checked for the expected root and parent traversal.
Source and build directories are private OS temporary storage outside Git.
The source top-level `COPYRIGHT` contains the PostgreSQL license; the nested
`src/backend/regex/COPYRIGHT` contains additional notices to review if that
code is shipped. This pin and matching digest do not by themselves constitute
signed source provenance or legal sign-off.

The local build selects `--with-ssl=openssl --without-readline --without-icu
--disable-nls`, retaining zlib. It uses Apple clang 21.0.0 and the developer's
Homebrew OpenSSL 3.6.4 headers/libraries; OpenSSL's local `LICENSE.txt` is
Apache-2.0. A parallel first attempt hit a generated-header race; running
`make -C src/backend generated-headers` before sequential
`make -C src/bin/pg_dump`, then installing libpq and the pg_dump subtree into
private OS temporary storage, succeeded. No source/build/payload file is in Git.

The exact local candidate is **not relocatable or release-ready**. Native
`otool -L` finds these direct and transitive imports:

- `pg_dump` SHA-256 `0e5aca733a1c6b53c10de6bf4b675ff84dbf746a9e6da05017a2bb47302fb6ea`
  and `pg_restore` SHA-256 `1c8e9e84ac8c2817b2cbcac9fc84dd0f390b2dce73a3f8e4423c322bc3bdb9f7`
  are Mach-O arm64 executables that load an **absolute private build-prefix**
  `libpq.5.dylib`, Homebrew OpenSSL `libcrypto.3.dylib`, system `libz.1.dylib`
  and `libSystem.B.dylib`.
- `libpq.5.dylib` SHA-256 `8e3cdb120bbdef78d2f0dc76ee617566b254f7d4a1d6a35cf2952b320c555367`
  loads absolute Homebrew OpenSSL `libssl.3.dylib` and `libcrypto.3.dylib`,
  plus system `libSystem.B.dylib`. `libssl.3.dylib` additionally loads
  Homebrew `libcrypto.3.dylib` via an absolute Cellar path. All three locally
  built objects report `LC_BUILD_VERSION minos 27.0`, as do the inspected
  Homebrew SSL libraries. This does **not** qualify a lower macOS minimum.
- The SSL libraries are external machine-installed dependencies. Their
  licensing/notices, installation names, signing and package closure need
  separate review before any embedding, copying or redistribution. The
  PG17.11 source also contains a nested regex `COPYRIGHT` notice; the
  selected client build's use of those files has not been traced to a legal
  conclusion.

On this one developer machine, the candidate's `pg_dump --version` and
`pg_restore --version` print 17.11. The opt-in two-cluster local fixture
`SPARC_TEST_PG_BIN=/opt/homebrew/opt/postgresql@17/bin
SPARC_TEST_PG_CLIENT_BIN=<private-candidate>/bin go test -tags=integration
./internal/database -run '^TestCrossClusterRestoreRequiresTargetOwner$' -count=1`
passed against disposable PostgreSQL 17.9 servers: native `verify-full` dump
refused a wrong CA and wrong hostname, succeeded with explicit CA, the first
single-transaction restore refused a missing role without changing the target,
and the second restored the owner and row after explicit local role provisioning
with the source shut down. This is local mechanism evidence only; it does not
qualify a signed payload, hosted route, Supabase role baseline, portability or
redistribution.

EDB PostgreSQL binary archives are retained only as an **uninspected control
candidate** (<https://www.enterprisedb.com/download-postgresql-binaries>). No
EDB archive, direct artifact URL, checksum, architecture slice, dependency tree,
notice file, license conclusion, or redistribution conclusion was inspected.

## Explicitly unverified payload facts

For macOS amd64 and Windows amd64, all of the following remain unverified:
actual shipped files, architecture/load commands, import/runtime closure,
minimum OS, build recipe, license/notices, signing identity/status, and
post-signing digests. macOS arm64 still lacks a relocatable release payload,
minimum-OS testing, signing, and a redistribution decision.
`build/clients/manifest.json` deliberately contains no
payload file entries and must not be treated as approval to build, download,
or distribute one.

## Next gates

Do not select or embed this candidate: its hardcoded Homebrew/temporary-prefix
imports and `minos 27.0` fail the dependency-free, supported-OS contract.
Before a macOS arm64 release candidate, establish a relocatable approved
OpenSSL/libpq closure, a selected supported minimum OS with native proof,
complete notices, authenticated signing/notarization and clean-machine TLS
restore. Separately review redistribution rights and the exact final payload;
this local approval did not include distribution. macOS amd64 and Windows
amd64 require their own native builds and runtime evidence. No
Homebrew-installed client is a release payload candidate.
