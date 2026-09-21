// Package tools owns trusted bundled-tool inventory and execution boundaries.
// The production payload inventory is intentionally empty until client artifacts
// are separately approved.
package tools

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strings"
)

const (
	payloadSchemaVersion            = 1
	supportedPostgreSQLMajor        = 17
	maxPayloadFiles                 = 1024
	maxPayloadPathBytes             = 240
	maxPayloadFileBytes      uint64 = 256 << 20
	maxCompressedBytes       uint64 = 512 << 20
	maxExpandedBytes         uint64 = 1 << 30
)

var ErrPayloadUnavailable = errors.New("tool payload unavailable")
var ErrInvalidPayload = errors.New("invalid tool payload")

type Tool uint8

const (
	PGDump Tool = iota + 1
	PGRestore
	PSQL
)

type filePurpose uint8

const (
	purposeExecutable filePurpose = iota + 1
	purposeRuntime
	purposeNotice
)

type payloadTarget struct {
	OS           string
	Architecture string
}

type payloadFile struct {
	Path    string
	Purpose filePurpose
	Mode    uint32
	Length  uint64
	SHA256  [sha256.Size]byte
}

type packageManifest struct {
	SchemaVersion    uint32
	PostgreSQLMajor  uint16
	Target           payloadTarget
	CompressedLength uint64
	CompressedSHA256 [sha256.Size]byte
	Files            []payloadFile
	Executables      map[Tool]string
}

// No runtime file, environment variable, PATH entry, or adjacent executable can
// populate this inventory. Approved payloads must be compiled into a later build.
var productionPayloads = [...]packageManifest{}

func lookupProductionPayload(tool Tool, major uint16, target payloadTarget) (packageManifest, error) {
	return selectPayload(productionPayloads[:], tool, major, target)
}

func selectPayload(inventory []packageManifest, tool Tool, major uint16, target payloadTarget) (packageManifest, error) {
	if !validTool(tool) || major != supportedPostgreSQLMajor || !validTarget(target) {
		return packageManifest{}, ErrInvalidPayload
	}
	var selected packageManifest
	matches := 0
	invalid := false
	for _, manifest := range inventory {
		if manifest.PostgreSQLMajor != major || manifest.Target != target {
			continue
		}
		matches++
		if validateManifest(manifest) != nil {
			invalid = true
			continue
		}
		selected = manifest
	}
	if matches == 0 {
		return packageManifest{}, ErrPayloadUnavailable
	}
	if invalid || matches != 1 {
		return packageManifest{}, ErrInvalidPayload
	}
	return selected, nil
}

func validateManifest(manifest packageManifest) error {
	if manifest.SchemaVersion != payloadSchemaVersion || manifest.PostgreSQLMajor != supportedPostgreSQLMajor ||
		!validTarget(manifest.Target) || manifest.CompressedLength == 0 || manifest.CompressedLength > maxCompressedBytes ||
		zeroDigest(manifest.CompressedSHA256) || len(manifest.Files) == 0 || len(manifest.Files) > maxPayloadFiles {
		return ErrInvalidPayload
	}

	paths := make([]string, 0, len(manifest.Files))
	files := make(map[string]payloadFile, len(manifest.Files))
	var expanded uint64
	for _, file := range manifest.Files {
		if !portablePayloadPath(file.Path) || file.Length == 0 || file.Length > maxPayloadFileBytes || zeroDigest(file.SHA256) {
			return ErrInvalidPayload
		}
		switch file.Purpose {
		case purposeExecutable:
			if file.Mode != 0o700 {
				return ErrInvalidPayload
			}
		case purposeRuntime, purposeNotice:
			if file.Mode != 0o600 {
				return ErrInvalidPayload
			}
		default:
			return ErrInvalidPayload
		}
		var ok bool
		expanded, ok = boundedAdd(expanded, file.Length, maxExpandedBytes)
		if !ok {
			return ErrInvalidPayload
		}
		normalized := strings.ToLower(file.Path)
		for _, existing := range paths {
			if normalized == existing || strings.HasPrefix(normalized, existing+"/") || strings.HasPrefix(existing, normalized+"/") {
				return ErrInvalidPayload
			}
		}
		paths = append(paths, normalized)
		files[file.Path] = file
	}

	if len(manifest.Executables) != 3 {
		return ErrInvalidPayload
	}
	mapped := make(map[string]bool, 3)
	for _, tool := range []Tool{PGDump, PGRestore, PSQL} {
		path, ok := manifest.Executables[tool]
		file, exists := files[path]
		if !ok || !exists || file.Purpose != purposeExecutable || mapped[path] {
			return ErrInvalidPayload
		}
		mapped[path] = true
	}
	for tool := range manifest.Executables {
		if !validTool(tool) {
			return ErrInvalidPayload
		}
	}
	for _, file := range manifest.Files {
		if file.Purpose == purposeExecutable && !mapped[file.Path] {
			return ErrInvalidPayload
		}
	}
	return nil
}

func packageID(manifest packageManifest) (string, error) {
	if validateManifest(manifest) != nil {
		return "", ErrInvalidPayload
	}
	identity := manifestIdentity(manifest)
	return hex.EncodeToString(identity[:]), nil
}

// manifestIdentity is separate from validation so tests can prove every field is
// identity-bound even when mutating a field makes the manifest unsupported.
func manifestIdentity(manifest packageManifest) [sha256.Size]byte {
	var canonical bytes.Buffer
	writeUint32(&canonical, manifest.SchemaVersion)
	writeUint16(&canonical, manifest.PostgreSQLMajor)
	writeString(&canonical, manifest.Target.OS)
	writeString(&canonical, manifest.Target.Architecture)
	writeUint64(&canonical, manifest.CompressedLength)
	canonical.Write(manifest.CompressedSHA256[:])

	files := append([]payloadFile(nil), manifest.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	writeUint32(&canonical, uint32(len(files)))
	for _, file := range files {
		writeString(&canonical, file.Path)
		canonical.WriteByte(byte(file.Purpose))
		writeUint32(&canonical, file.Mode)
		writeUint64(&canonical, file.Length)
		canonical.Write(file.SHA256[:])
	}
	for _, tool := range []Tool{PGDump, PGRestore, PSQL} {
		canonical.WriteByte(byte(tool))
		writeString(&canonical, manifest.Executables[tool])
	}
	return sha256.Sum256(canonical.Bytes())
}

func boundedAdd(total, value, limit uint64) (uint64, bool) {
	if value > math.MaxUint64-total {
		return 0, false
	}
	total += value
	return total, total <= limit
}

func validTool(tool Tool) bool { return tool == PGDump || tool == PGRestore || tool == PSQL }

func validTarget(target payloadTarget) bool {
	return target == (payloadTarget{OS: "darwin", Architecture: "arm64"}) ||
		target == (payloadTarget{OS: "darwin", Architecture: "amd64"}) ||
		target == (payloadTarget{OS: "windows", Architecture: "amd64"})
}

func zeroDigest(digest [sha256.Size]byte) bool { return digest == [sha256.Size]byte{} }

func portablePayloadPath(path string) bool {
	if path == "" || len(path) > maxPayloadPathBytes || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	for _, component := range strings.Split(path, "/") {
		if component == "" || component == "." || component == ".." || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || windowsReservedName(component) {
			return false
		}
		for i := 0; i < len(component); i++ {
			c := component[i]
			if c < 0x21 || c > 0x7e || strings.ContainsRune(`<>:"\|?*`, rune(c)) {
				return false
			}
		}
	}
	return true
}

func windowsReservedName(component string) bool {
	base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}

func writeString(buffer *bytes.Buffer, value string) {
	writeUint32(buffer, uint32(len(value)))
	buffer.WriteString(value)
}

func writeUint16(buffer *bytes.Buffer, value uint16) {
	_ = binary.Write(buffer, binary.BigEndian, value)
}
func writeUint32(buffer *bytes.Buffer, value uint32) {
	_ = binary.Write(buffer, binary.BigEndian, value)
}
func writeUint64(buffer *bytes.Buffer, value uint64) {
	_ = binary.Write(buffer, binary.BigEndian, value)
}
