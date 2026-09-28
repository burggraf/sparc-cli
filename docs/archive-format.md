# SPARC archive format

Tasks 06–07 provide a local, passphrase-only archive primitive and offline
verifier. The format is `sparc-archive` version 1; it is not compatible with
the historical `sparc-local-artifacts` prototype.

## Layout and publication

Every payload is a separate standard binary [age](https://age-encryption.org/)
file. Payload filenames are opaque, contiguous lowercase hexadecimal IDs:
`00000000.age`, `00000001.age`, and so on. The encrypted `manifest.age` is
written last inside a private staging directory, after every encrypted payload
has been read back through authenticated EOF and its length/digest checked. Only
then is the complete private directory atomically published at the final path
with native no-replace semantics. Its absence at that final path means the
archive is incomplete. A crash may leave a randomly named private staging
folder; it does not publish the requested final path. This is not a multi-file
transaction or a crash-durability guarantee.

The encrypted manifest records the format/version and each component's opaque
ID, logical key, scope, completion status, plaintext byte length, and lowercase
SHA-256 digest. Logical keys preserve Unicode code points exactly and use `/` as
the separator; they reject absolute, empty, dot, parent, backslash, colon,
control-character, Windows-reserved, and trailing-dot/space segments. Keys that
collide under Unicode simple case folding are rejected. No Unicode
normalization is performed; extraction must separately handle normalized-name
collisions. The manifest parser rejects duplicate or unknown fields, duplicate
IDs/keys, non-contiguous IDs, unsupported versions, non-integer lengths,
invalid digests, malformed UTF-8/unpaired surrogate escapes, nesting beyond
eight containers, and inputs over 16 MiB.
It permits at most 100,000 components, 4,096 UTF-8 bytes per key or scope, 64
key segments, and 128 GiB total declared plaintext.

## Encryption and recovery

Each component and the manifest are encrypted independently with
`filippo.io/age` scrypt recipients from an operator-supplied passphrase. Work
factor 18 is explicit for encryption and is the maximum accepted on decryption,
bounding hostile archive KDF work. The passphrase is nonempty, valid UTF-8,
free of control/line-separator characters, and bounded to 4,096 bytes; it is
never placed in argv, filenames, manifest data, or ordinary diagnostics.
Correct decryption requires reading to authenticated EOF: a wrong passphrase,
truncation, or ciphertext corruption must fail. Encryption does **not**
authenticate the sender or establish payload provenance. The passphrase and
scrypt state are ordinary process memory; no secure-memory erasure is claimed.

`destination.Create` receives an absolute final path from the CLI, which resolves
relative input paths from the current directory. Its parent must already exist
and be usable for archive creation; a normal user-writable directory such as
`0755` is accepted. It creates a private random sibling staging folder, and the
native no-replace directory publisher atomically publishes the finished folder.
Directories writable by other users remain refused. Local external/removable
volumes may work when their parent passes native checks; network filesystems and
UNC paths are refused by current native boundaries. Interrupted staging cleanup
is best-effort, and a crash may leave ciphertext in a private staging folder.

`archive.Verify` requires the private archive directory SPARC created, an authenticated bounded
manifest, exactly the listed payload files plus `manifest.age`, and each
payload's authenticated EOF, declared length, and digest. Extra files, missing
payloads, symlinks, wrong passphrases, and corruption are refused. It executes
no archive content. `verify.Offline` requires only the archive path and recovery
passphrase; it has no source-config or network dependency. Its integrity result
is separate from the manifest's declared capture status: a byte-valid archive
marked incomplete remains incomplete, and even a declared-complete archive is
not proof that all Supabase recovery components were captured.

The archive tests cover zero-byte, small, multi-chunk, and streamed 8 MiB
payloads; wrong/unsafe passphrases; truncation/corruption; finalization writes;
direct age-library decryption; read-back checks before manifest creation;
no-clobber final-directory publication; bounded manifest/path validation;
source-folder removal before offline verification; and encrypted payload and
manifest round trips. `BenchmarkEncryptLargeStream` measures allocation with a
generated stream instead of a large in-memory plaintext slice. Each artifact
runs age's scrypt KDF independently; this format is not yet qualified for large
object counts or production workloads.

These paths reuse the native private-storage primitives; effective macOS ACL
privacy and native Windows runtime behavior remain unqualified (see
[credential/private-storage gates](credentials.md)). Same-user path replacement
and crash-durability are not addressed.

Source-mutation detection, resumable journals, remote destinations, a public
unpack or offline-verify command, PostgreSQL payload provenance, and Supabase
compatibility remain unimplemented. A production payload inventory remains
empty.

The [R17 Supabase-aware database profile](research/R17-supabase-cli-dump-profile.md)
uses a separate, version-2 encrypted `database/recovery-profile.json` component
inside the **existing** v1 container. New `sparc backup` captures contain
`database/schema.sql`, `database/data.sql`, and that profile as three separately
encrypted components; schema and data are marked `incomplete`. The profile records
the source project reference, source public-schema owner, PostgreSQL/client
versions, exact selections/exclusions, missing roles, unqualified snapshot
consistency, and archive-derived per-table fingerprints.

The schema stream rewrites only the top-level `CREATE SCHEMA public;` statement
to `CREATE SCHEMA IF NOT EXISTS public;`; capture fails closed unless exactly one
such statement is found. The profile records this transform. Data SQL remains
unchanged. Fingerprints use the archived SQL `COPY` rows and verify table columns,
row counts, and row-content aggregates before an internal split restore can
commit. This does not verify sequences, large objects, ACL/RLS semantics, service
behavior, or archive-time consistency.

No container version or top-level manifest fields changed. Existing
one-component `database/postgresql.dump` archives retain their original
incomplete meaning. Older version-1 split profiles remain structurally
verifiable but are refused by current split-restore validation;
there is no profile upgrade path. The public `sparc restore` command remains
unavailable; the internal split-restore experiment is not a production recovery
path and has not been run against a hosted target.
