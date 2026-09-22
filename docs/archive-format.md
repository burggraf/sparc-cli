# SPARC archive format

Task 06 provides a local, passphrase-only archive primitive. The format is
`sparc-archive` version 1; it is not compatible with the historical
`sparc-local-artifacts` prototype.

## Layout and publication

Every payload is a separate standard binary [age](https://age-encryption.org/)
file. Payload filenames are opaque, contiguous lowercase hexadecimal IDs:
`00000000.age`, `00000001.age`, and so on. The encrypted `manifest.age` is
published last. Its absence means the directory is incomplete and must not be
treated as an archive. Failures can leave ciphertext payloads, but never a
complete marker.

The encrypted manifest records only the format/version and each component's
opaque ID, logical key, scope, completion status, plaintext byte length, and
lowercase SHA-256 digest. Logical keys preserve Unicode exactly and use `/` as
the separator; they reject absolute, empty, dot, parent, backslash, colon, and
control-character path segments. The manifest parser rejects duplicate or
unknown fields, duplicate IDs/keys, non-contiguous IDs, unsupported versions,
non-integer lengths, invalid digests, excess depth, and inputs over 16 MiB.
It permits at most 100,000 components, 4,096 UTF-8 bytes per key or scope, 64
key segments, and 128 GiB total declared plaintext.

## Encryption and recovery

Each component and the manifest are encrypted independently with
`filippo.io/age` scrypt recipients from an operator-supplied passphrase. The
passphrase is nonempty, valid UTF-8, free of control/line-separator characters,
and bounded to 4,096 bytes; it is never placed in argv, filenames, manifest data,
or ordinary diagnostics. Correct decryption requires reading
to authenticated EOF: a wrong passphrase, truncation, or ciphertext corruption
must fail. Encryption does **not** authenticate the sender or establish payload
provenance.

The archive tests cover zero-byte, small, multi-chunk, and streamed 8 MiB
payloads; wrong keys; truncation/corruption; finalization writes; direct age
library decryption; manifest-last publication; source errors; existing-manifest
refusal; and encrypted payload/manifest round trips. `BenchmarkEncryptLargeStream` measures allocation
with a generated stream rather than a large in-memory byte slice.

This work does not yet define local destination privacy/no-clobber semantics,
archive reading/verification, source-mutation detection, resume journals,
remote destinations, PostgreSQL payload provenance, or Supabase compatibility.
A production payload inventory remains empty.
