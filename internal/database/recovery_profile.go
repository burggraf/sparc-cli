package database

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	maxRecoveryProfileBytes = 16 << 20
	maxRecoveryTables       = 10000
	maxRecoveryColumns      = 100000
)

var (
	ErrRecoveryProfile = errors.New("unsupported recovery profile")
	ErrRecoveryData    = errors.New("unsupported or invalid dump data")
)

type RecoveryTableFingerprint struct {
	Schema      string   `json:"schema"`
	Name        string   `json:"name"`
	Columns     []string `json:"columns"`
	Rows        uint64   `json:"rows"`
	Fingerprint string   `json:"sha256"`
}

func parseRecoveryProfileV2(data []byte) (RecoveryProfileV2, error) {
	if len(data) == 0 || len(data) > maxRecoveryProfileBytes || !utf8.Valid(data) {
		return RecoveryProfileV2{}, ErrRecoveryProfile
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var profile RecoveryProfileV2
	if decoder.Decode(&profile) != nil {
		return RecoveryProfileV2{}, ErrRecoveryProfile
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return RecoveryProfileV2{}, ErrRecoveryProfile
	}
	canonical, err := json.Marshal(profile)
	if err != nil || !bytes.Equal(data, canonical) || !validRecoveryProfileV2(profile) {
		return RecoveryProfileV2{}, ErrRecoveryProfile
	}
	return profile, nil
}

func validRecoveryProfileV2(profile RecoveryProfileV2) bool {
	if profile.Format != recoveryProfileFormat || profile.Version != recoveryProfileVersion || profile.CaptureStatus != "incomplete" ||
		!validProjectRefLabel(profile.SourceProjectRef) || !validIdentifier(profile.PublicSchemaOwner) || profile.SourcePostgresMajor != supportedPostgresMajor ||
		profile.SourceVersionNum < 170000 || profile.SourceVersionNum >= 180000 || profile.ClientVersion != "PostgreSQL 17.11" {
		return false
	}
	if profile.SchemaDump.Key != schemaSQLKey || profile.SchemaDump.Mode != "schema-only" || profile.SchemaDump.Status != "incomplete" ||
		!sameRecoveryStrings(profile.SchemaDump.Schemas, []string{"public"}) || !sameRecoveryStrings(profile.SchemaDump.Transforms, []string{recoveryPublicSchemaTransform}) ||
		profile.DataDump.Key != dataSQLKey || profile.DataDump.Mode != "data-only" || profile.DataDump.Status != "incomplete" ||
		!sameRecoveryStrings(profile.DataDump.Schemas, []string{"public", "auth", "storage"}) || !sameRecoveryStrings(profile.DataDump.Transforms, []string{}) {
		return false
	}
	exclusions := recoveryDataExclusions()
	if profile.ExcludedTables == nil || len(profile.ExcludedTables) != len(exclusions) || profile.Extensions == nil ||
		profile.TableFingerprintAlgorithm != recoveryFingerprintAlgorithm || profile.TableFingerprints == nil || !validTableFingerprints(profile.TableFingerprints) ||
		profile.Roles != (recoveryCoverageProfile{Status: "missing", Reason: recoveryRolesMissing}) ||
		profile.Snapshot != (recoveryCoverageProfile{Status: "unqualified", Reason: recoverySnapshotGap}) ||
		!sameRecoveryStrings(profile.Unknowns, recoveryProfileUnknowns()) {
		return false
	}
	for index, table := range exclusions {
		if profile.ExcludedTables[index] != (recoveryProfileTable{Schema: table.Schema, Name: table.Name}) {
			return false
		}
	}
	if len(profile.Extensions) > maxObservedExtensions {
		return false
	}
	seen := make(map[string]struct{}, len(profile.Extensions))
	previous := ""
	for _, extension := range profile.Extensions {
		if !validIdentifier(extension.Name) || len(extension.Version) == 0 || len(extension.Version) > maxExtensionVersion || !validText(extension.Version) || !validIdentifier(extension.Schema) || extension.Name <= previous {
			return false
		}
		if _, exists := seen[extension.Name]; exists {
			return false
		}
		seen[extension.Name] = struct{}{}
		previous = extension.Name
	}
	return true
}

func validTableFingerprints(fingerprints []RecoveryTableFingerprint) bool {
	if len(fingerprints) > maxRecoveryTables {
		return false
	}
	allowedSchemas := map[string]bool{"public": true, "auth": true, "storage": true}
	totalColumns := 0
	previous := ""
	for _, fingerprint := range fingerprints {
		key := fingerprint.Schema + "\x00" + fingerprint.Name
		if !allowedSchemas[fingerprint.Schema] || !validIdentifier(fingerprint.Name) || fingerprint.Columns == nil || len(fingerprint.Columns) == 0 || len(fingerprint.Columns) > 1664 || key <= previous || len(fingerprint.Fingerprint) != 64 {
			return false
		}
		if _, err := hex.DecodeString(fingerprint.Fingerprint); err != nil || strings.ToLower(fingerprint.Fingerprint) != fingerprint.Fingerprint {
			return false
		}
		seenColumns := make(map[string]struct{}, len(fingerprint.Columns))
		for _, column := range fingerprint.Columns {
			if !validIdentifier(column) {
				return false
			}
			if _, exists := seenColumns[column]; exists {
				return false
			}
			seenColumns[column] = struct{}{}
		}
		totalColumns += len(fingerprint.Columns)
		if totalColumns > maxRecoveryColumns {
			return false
		}
		previous = key
	}
	return true
}

func sameRecoveryStrings(got, want []string) bool {
	if got == nil || len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
