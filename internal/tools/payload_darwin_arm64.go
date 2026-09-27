//go:build darwin && arm64

package tools

import _ "embed"

//go:embed payloads/darwin-arm64.tar.gz
var darwinArm64PayloadArchive []byte

func init() {
	manifest := packageManifest{
		SchemaVersion:    1,
		PostgreSQLMajor:  17,
		Target:           payloadTarget{OS: "darwin", Architecture: "arm64"},
		CompressedLength: 3615199,
		CompressedSHA256: payloadDigest("9fc7d7596c1b13c240ab12e661a89e529a47ca880e0faa5ff3b07f472bd0069e"),
		Files: []payloadFile{
			{Path: "bin/pg_dump", Purpose: purposeExecutable, Mode: 0o700, Length: 458104, SHA256: payloadDigest("9bbffc2dcd3a94f2e80c3fdcff84473850b638f6a4eb2b67306d58eca91120bd")},
			{Path: "bin/pg_restore", Purpose: purposeExecutable, Mode: 0o700, Length: 251768, SHA256: payloadDigest("d557bf01442153a90960b22d6f37695ea7cb4e03cd92d61211a1d93396443b7f")},
			{Path: "bin/psql", Purpose: purposeExecutable, Mode: 0o700, Length: 552152, SHA256: payloadDigest("801a29726b5c110a3e5a765af8408fc26d8fdcf3808ee62334795944582341cc")},
			{Path: "lib/libpq.5.dylib", Purpose: purposeRuntime, Mode: 0o600, Length: 317184, SHA256: payloadDigest("fb1c3ad77df600d46224c9490e46073ad891423bf6cb0eba55d0f7c8982b3d40")},
			{Path: "lib/libssl.3.dylib", Purpose: purposeRuntime, Mode: 0o600, Length: 1381584, SHA256: payloadDigest("c75133be4790a93fed844e259d824138c38ff3bffdddad89aeffdfffdf791e45")},
			{Path: "lib/libcrypto.3.dylib", Purpose: purposeRuntime, Mode: 0o600, Length: 6314560, SHA256: payloadDigest("6dfdba4b25d216bef1b3d1ffeacd6a57bf116559f45c649c748422929c0fe3b6")},
			{Path: "licenses/THIRD_PARTY_NOTICES.txt", Purpose: purposeNotice, Mode: 0o600, Length: 16054, SHA256: payloadDigest("e9237451946aad087875d3f1a72bb326abd6003e2bf7e91d9d6d24aac6e9c96a")},
		},
		Executables: map[Tool]string{
			PGDump: "bin/pg_dump", PGRestore: "bin/pg_restore", PSQL: "bin/psql",
		},
	}
	productionPayloads = append(productionPayloads, manifest)
	productionPayloadArchives[manifest.Target] = darwinArm64PayloadArchive
}
