//go:build localdemo

package localdemo

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxTOCEntries = 200_000

type tocSelection struct {
	summary   TOCSummary
	schemaIDs []uint32
	dataIDs   []uint32
}

func (p tocSelection) Blocked() bool {
	return p.summary.ManagedDDL > 0 || p.summary.Unsupported > 0 || p.summary.Unknown > 0
}

type tocEntry struct {
	id         uint32
	descriptor string
	namespace  string
	tag        string
}

type tocKind uint8

const (
	tocDDL tocKind = iota + 1
	tocData
	tocUnsupported
)

type tocDescriptor struct {
	name string
	kind tocKind
}

// Keep multiword PostgreSQL descriptions before their prefixes. Unknown types
// remain blocked instead of being inferred safe from their namespace.
var tocDescriptors = []tocDescriptor{
	{"MATERIALIZED VIEW DATA", tocData},
	{"TEXT SEARCH CONFIGURATION", tocDDL},
	{"TEXT SEARCH DICTIONARY", tocDDL},
	{"TEXT SEARCH PARSER", tocDDL},
	{"TEXT SEARCH TEMPLATE", tocDDL},
	{"FOREIGN DATA WRAPPER", tocUnsupported},
	{"SEQUENCE OWNED BY", tocDDL},
	{"OPERATOR FAMILY", tocDDL},
	{"OPERATOR CLASS", tocDDL},
	{"FK CONSTRAINT", tocDDL},
	{"DEFAULT ACL", tocDDL},
	{"LARGE OBJECT DATA", tocUnsupported},
	{"PUBLICATION TABLE", tocUnsupported},
	{"TABLE DATA", tocData},
	{"MATERIALIZED VIEW", tocDDL},
	{"FOREIGN TABLE", tocUnsupported},
	{"EVENT TRIGGER", tocUnsupported},
	{"USER MAPPING", tocUnsupported},
	{"ACCESS METHOD", tocUnsupported},
	{"SEQUENCE SET", tocData},
	{"PROCEDURE", tocDDL},
	{"AGGREGATE", tocDDL},
	{"COLLATION", tocDDL},
	{"CONVERSION", tocDDL},
	{"OPERATOR", tocDDL},
	{"EXTENSION", tocUnsupported},
	{"PUBLICATION", tocUnsupported},
	{"SUBSCRIPTION", tocUnsupported},
	{"LARGE OBJECT", tocUnsupported},
	{"BLOBS", tocUnsupported},
	{"BLOB", tocUnsupported},
	{"STATISTICS", tocDDL},
	{"TRANSFORM", tocDDL},
	{"CONSTRAINT", tocDDL},
	{"SEQUENCE", tocDDL},
	{"FUNCTION", tocDDL},
	{"DATABASE", tocUnsupported},
	{"SCHEMA", tocDDL},
	{"TABLE", tocDDL},
	{"VIEW", tocDDL},
	{"TYPE", tocDDL},
	{"DOMAIN", tocDDL},
	{"CAST", tocDDL},
	{"DEFAULT", tocDDL},
	{"INDEX", tocDDL},
	{"TRIGGER", tocDDL},
	{"RULE", tocDDL},
	{"POLICY", tocDDL},
	{"ACL", tocDDL},
	{"COMMENT", tocDDL},
	{"ROW SECURITY", tocDDL},
	{"SERVER", tocUnsupported},
}

var excludedTableData = map[string]struct{}{
	"auth.schema_migrations":        {},
	"storage.migrations":            {},
	"supabase_functions.migrations": {},
	"storage.buckets_vectors":       {},
	"storage.vector_indexes":        {},
}

var protectedSchemas = map[string]struct{}{
	"_analytics": {}, "_realtime": {}, "_supavisor": {},
	"auth": {}, "cron": {}, "dbdev": {}, "etl": {}, "extensions": {},
	"graphql": {}, "graphql_public": {}, "net": {}, "pgbouncer": {}, "pgmq": {},
	"pgsodium": {}, "pgsodium_masks": {}, "pgtle": {}, "realtime": {},
	"repack": {}, "storage": {}, "supabase_functions": {}, "supabase_migrations": {},
	"tiger": {}, "tiger_data": {}, "topology": {}, "vault": {},
}

// ponytail: pg_restore's TOC list is display text, not a structured format; ambiguous whitespace schemas and unknown entry types block here. Replace this parser with a structured archive TOC reader before broadening accepted identifiers/types.
func selectArchiveTOC(data []byte) (tocSelection, error) {
	if len(data) == 0 || len(data) > maxTOCBytes || !utf8.Valid(data) {
		return tocSelection{}, ErrArchive
	}
	lines := strings.Split(string(data), "\n")
	schemaNames := make(map[string]struct{})
	seen := make(map[uint32]struct{})
	entries := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if entries >= maxTOCEntries {
			return tocSelection{}, ErrArchive
		}
		entry, _, err := parseTOCEntry(line)
		if err != nil {
			return tocSelection{}, ErrArchive
		}
		if _, exists := seen[entry.id]; exists {
			return tocSelection{}, ErrArchive
		}
		seen[entry.id] = struct{}{}
		entries++
		if entry.descriptor == "SCHEMA" {
			schemaNames[entry.tag] = struct{}{}
		}
	}
	if entries == 0 {
		return tocSelection{}, ErrArchive
	}
	plan := tocSelection{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		entry, descriptor, err := parseTOCEntry(line)
		if err != nil {
			return tocSelection{}, ErrArchive
		}
		plan.summary.Total++
		if entry.descriptor == "EXTENSION" {
			plan.summary.Extension++
		}
		if descriptor == nil {
			plan.summary.Unknown++
			continue
		}
		if descriptor.kind == tocUnsupported {
			plan.summary.Unsupported++
			continue
		}
		if descriptor.kind == tocData {
			if ambiguousNamespace(entry.namespace, schemaNames) || !knownTOCSchema(entry.namespace, schemaNames) {
				plan.summary.Unknown++
				continue
			}
			if excludedDataEntry(entry) {
				plan.summary.ExcludedData++
				continue
			}
			if !allowedDataSchema(entry.namespace) {
				plan.summary.Unsupported++
				continue
			}
			plan.dataIDs = append(plan.dataIDs, entry.id)
			plan.summary.SelectedData++
			continue
		}
		if entry.descriptor == "SCHEMA" {
			schema := entry.tag
			if strings.ContainsFunc(schema, unicode.IsSpace) {
				plan.summary.Unsupported++
				continue
			}
			if schema == "public" {
				continue // Supabase/PostgreSQL target supplies this schema.
			}
			if isManagedSchema(schema) {
				if schema == "auth" || schema == "storage" {
					plan.summary.ManagedDDL++
				} else {
					plan.summary.Unsupported++
				}
				continue
			}
			plan.schemaIDs = append(plan.schemaIDs, entry.id)
			plan.summary.SelectedSchema++
			continue
		}
		if entry.namespace == "" || entry.namespace == "-" {
			plan.summary.Unsupported++
			continue
		}
		if ambiguousNamespace(entry.namespace, schemaNames) || !knownTOCSchema(entry.namespace, schemaNames) {
			plan.summary.Unknown++
			continue
		}
		if isManagedSchema(entry.namespace) {
			if entry.namespace == "auth" || entry.namespace == "storage" {
				plan.summary.ManagedDDL++
			} else {
				plan.summary.Unsupported++
			}
			continue
		}
		plan.schemaIDs = append(plan.schemaIDs, entry.id)
		plan.summary.SelectedSchema++
	}
	if plan.summary.Total == 0 {
		return tocSelection{}, ErrArchive
	}
	return plan, nil
}

func parseTOCEntry(line string) (tocEntry, *tocDescriptor, error) {
	idText, remainder, ok := strings.Cut(line, ";")
	if !ok || idText == "" || !allASCIIDigits(strings.TrimSpace(idText)) {
		return tocEntry{}, nil, ErrArchive
	}
	id, err := strconv.ParseUint(strings.TrimSpace(idText), 10, 32)
	if err != nil || id == 0 {
		return tocEntry{}, nil, ErrArchive
	}
	fields, err := splitTOCFields(remainder)
	if err != nil || len(fields) < 6 || !isOID(fields[0]) || !isOID(fields[1]) {
		return tocEntry{}, nil, ErrArchive
	}
	for i := range tocDescriptors {
		descriptor := &tocDescriptors[i]
		name := strings.Fields(descriptor.name)
		if !hasPrefixFields(fields[2:], name) {
			continue
		}
		namespaceIndex := 2 + len(name)
		if len(fields) < namespaceIndex+3 {
			return tocEntry{}, nil, ErrArchive
		}
		namespace := fields[namespaceIndex]
		if namespace == "" || strings.ContainsFunc(namespace, unicode.IsSpace) || fields[len(fields)-1] == "" {
			return tocEntry{}, nil, ErrArchive
		}
		tagFields := fields[namespaceIndex+1 : len(fields)-1]
		if len(tagFields) == 0 || strings.Join(tagFields, "") == "" {
			return tocEntry{}, nil, ErrArchive
		}
		return tocEntry{
			id: uint32(id), descriptor: descriptor.name,
			namespace: namespace, tag: strings.Join(tagFields, " "),
		}, descriptor, nil
	}
	return tocEntry{id: uint32(id)}, nil, nil
}

func splitTOCFields(input string) ([]string, error) {
	var fields []string
	var field strings.Builder
	quoted, started := false, false
	flush := func() {
		if started {
			fields = append(fields, field.String())
			field.Reset()
			started = false
		}
	}
	for i := 0; i < len(input); {
		r, size := utf8.DecodeRuneInString(input[i:])
		if r == '"' {
			if quoted && i+1 < len(input) && input[i+1] == '"' {
				field.WriteByte('"')
				i += 2
				continue
			}
			if !quoted && started {
				return nil, ErrArchive
			}
			quoted = !quoted
			started = true
			i += size
			continue
		}
		if !quoted && unicode.IsSpace(r) {
			flush()
			i += size
			continue
		}
		field.WriteString(input[i : i+size])
		started = true
		i += size
	}
	if quoted {
		return nil, ErrArchive
	}
	flush()
	return fields, nil
}

func hasPrefixFields(fields, prefix []string) bool {
	if len(fields) < len(prefix) {
		return false
	}
	for i := range prefix {
		if fields[i] != prefix[i] {
			return false
		}
	}
	return true
}

func isOID(value string) bool {
	if !allASCIIDigits(value) {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 32)
	return err == nil
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func excludedDataEntry(entry tocEntry) bool {
	key := entry.namespace + "." + entry.tag
	if _, excluded := excludedTableData[key]; excluded {
		return true
	}
	// pg_restore's human-readable list joins identifiers with spaces. If a
	// multiword owner made a known excluded table ambiguous, refuse its data
	// rather than accidentally selecting migration/service state.
	for excluded := range excludedTableData {
		if strings.HasPrefix(key, excluded+" ") {
			return true
		}
	}
	return false
}

func allowedDataSchema(schema string) bool {
	if schema == "public" || schema == "auth" || schema == "storage" {
		return true
	}
	return !isManagedSchema(schema)
}

func ambiguousNamespace(namespace string, schemas map[string]struct{}) bool {
	for schema := range schemas {
		if strings.HasPrefix(schema, namespace+" ") {
			return true
		}
	}
	return false
}

func knownTOCSchema(schema string, schemas map[string]struct{}) bool {
	if schema == "public" || isManagedSchema(schema) {
		return true
	}
	_, ok := schemas[schema]
	return ok
}

func isManagedSchema(schema string) bool {
	if _, ok := protectedSchemas[schema]; ok || schema == "information_schema" || strings.HasPrefix(schema, "pg_") {
		return true
	}
	return strings.HasPrefix(schema, "timescaledb_") || strings.HasPrefix(schema, "_timescaledb_")
}
