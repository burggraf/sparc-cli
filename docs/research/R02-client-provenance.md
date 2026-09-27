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

The initial Homebrew-linked candidate is **not relocatable or release-ready**.
Native `otool -L` finds these direct and transitive imports:

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

### Local OpenSSL 3.5.8 LTS portability experiment (2026-09-26)

The operator separately approved local OpenSSL source acquisition/build and
synthetic-fixture testing only; this added no hosted access or redistribution
authority. OpenSSL's [official downloads page](https://openssl-library.org/source/)
lists 3.5.8 as the 3.5 LTS series (EOL 2030-04-08). The 53,213,818-byte
source archive came from the [official GitHub release](https://github.com/openssl/openssl/releases/download/openssl-3.5.8/openssl-3.5.8.tar.gz);
its [SHA-256 sidecar](https://github.com/openssl/openssl/releases/download/openssl-3.5.8/openssl-3.5.8.tar.gz.sha256)
matched `a8f84a39918ec6415ce765d9b429d313ba97b8143169c172e734b9514464f5b2`.
The `.asc` signature was downloaded but not verified because GPG/GPGV is not
installed. The verified-digest source and all outputs stayed in private OS
temporary storage; no source or payload entered Git. OpenSSL 3.x uses
[Apache-2.0](https://openssl-library.org/license/), but this is not
redistribution or legal approval.

The first OpenSSL 3.5.8 build used `darwin64-arm64-cc`,
`-mmacosx-version-min=13.0`, `no-shared no-pinshared`, and Xcode 27.0. All
1,105 Mach-O records across the resulting static archives encoded `minos 13.0`.
However, PostgreSQL 17.11's upstream `libpq-refs-stamp` check rejected the
static-linked libpq because it had undefined `_atexit` and `_pthread_exit`
references. The guard was not bypassed. The experiment switched to shared
OpenSSL, configured with `no-tests no-apps no-legacy` and the same exploratory
13.0 deployment target. Its arm64 `libssl.3.dylib` and `libcrypto.3.dylib`
encode `minos 13.0` and load only system `libSystem`; `no-legacy` intentionally
omits the optional legacy provider and is not a complete OpenSSL feature-set
qualification. Static archives were moved out of the link search path so the
PostgreSQL candidate used the shared libraries.

A fresh PostgreSQL 17.11 build used the private OpenSSL 3.5.8 headers/libraries,
`--with-ssl=openssl --disable-rpath`, and the same 13.0 target. PostgreSQL's
configure log reports the installed Homebrew `openssl` command as 3.6.4, but its
compiler/linker probes use the private 3.5.8 prefix; final load commands and
runtime loading confirm the candidate libraries. The `pg_dump`, `pg_restore`,
`libpq.5.dylib`, `libssl.3.dylib`, and `libcrypto.3.dylib` runtime files are
arm64 and encode `minos 13.0`. Their dependencies were rewritten locally to
`@rpath` install names with relative run paths. `otool -L` showed only adjacent
candidate libraries plus system `libSystem`/`libz`; `DYLD_PRINT_LIBRARIES`
confirmed all three non-system libraries loaded from a copied, relocated
candidate after the original candidate and OpenSSL prefixes were hidden.

This remains an experiment, not a selected or release-ready payload. `strings`
shows OpenSSL `OPENSSLDIR`/`MODULESDIR` and libpq's compiled system-config path
still contain private build prefixes. The synthetic recipe supplies an explicit
CA and direct connection parameters; the native client succeeded after those
build prefixes were hidden, both with `OPENSSL_CONF=/dev/null` and an empty
module directory and with those environment variables unset. The helper's
bounded child environment is constructed in `internal/tools/run.go`; default
CA, OpenSSL-config, module, and service-file behavior still needs a deliberate
production-path decision and review before selecting any payload.

With the relocated candidate and disposable PostgreSQL 17.9 fixtures,
`TestCrossClusterRestoreRequiresTargetOwner` passed: bad CA and hostname were
refused, the correct CA allowed the dump, restore refused a missing target
owner without creating schema, and explicit synthetic role provisioning then
allowed the owner/data restore. Fresh `go test -mod=readonly ./... -count=1`,
full database integration and race suites, and default/integration `go vet` also
passed. This host is macOS 27.0: `minos 13.0` metadata is not runtime
qualification on macOS 13. No signing, notarization, redistribution, hosted
backup, or restore was performed.

EDB PostgreSQL binary archives are retained only as an **uninspected control
candidate** (<https://www.enterprisedb.com/download-postgresql-binaries>). No
EDB archive, direct artifact URL, checksum, architecture slice, dependency tree,
notice file, license conclusion, or redistribution conclusion was inspected.

## Explicitly unverified payload facts

For macOS amd64 and Windows amd64, all of the following remain unverified:
actual shipped files, architecture/load commands, import/runtime closure,
minimum OS, build recipe, license/notices, signing identity/status, and
post-signing digests. macOS arm64 now has a private relocatable load-command
experiment with `minos 13.0` and synthetic TLS/restore evidence, but no runtime
test on macOS 13, selected supported minimum, signed release payload, or
redistribution decision. Its compiled OpenSSL/libpq default directories also
still point to private build prefixes.
`build/clients/manifest.json` deliberately contains no
payload file entries and must not be treated as approval to build, download,
or distribute one.

## Next gates

Do not select or embed either local candidate: the initial Homebrew-linked build
still has absolute imports and `minos 27.0`; the later local OpenSSL 3.5.8
experiment is unsigned, not runtime-tested on its exploratory macOS 13 target,
and embeds private config-directory paths. Before any macOS arm64 release
candidate, select and natively prove the supported minimum OS, establish a
production-path OpenSSL/libpq closure, complete notices and redistribution
review, and obtain authenticated signing/notarization plus clean-machine TLS
restore evidence. This local approval did not include distribution or hosted
use. macOS amd64 and Windows amd64 require their own native builds and runtime
evidence. No Homebrew-installed client or temporary candidate is approved as a
release payload.
