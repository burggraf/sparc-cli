package database

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"io"
	"sort"
	"strings"
)

// ponytail: caps COPY/header lines at 16 MiB; use a chunked parser if real dumps exceed it.
const maxFingerprintCopyLineBytes = 16 << 20

func parseCopyDataFingerprints(source io.Reader) ([]RecoveryTableFingerprint, error) {
	if source == nil {
		return nil, ErrRecoveryData
	}
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64<<10), maxFingerprintCopyLineBytes)
	var current *copyTableFingerprint
	fingerprints := make([]RecoveryTableFingerprint, 0)
	seen := make(map[string]struct{})
	totalColumns := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if current == nil {
			if len(line) < len("COPY ") || !strings.EqualFold(string(line[:len("COPY ")]), "COPY ") {
				continue
			}
			schema, name, columns, ok := parseCopyHeader(string(line))
			if !ok || (schema != "public" && schema != "auth" && schema != "storage") || len(fingerprints) >= maxRecoveryTables || len(columns) > 1664 || totalColumns > maxRecoveryColumns-len(columns) {
				return nil, ErrRecoveryData
			}
			key := schema + "\x00" + name
			if _, exists := seen[key]; exists {
				return nil, ErrRecoveryData
			}
			seen[key] = struct{}{}
			totalColumns += len(columns)
			current = &copyTableFingerprint{schema: schema, name: name, columns: columns}
			continue
		}
		if bytes.Equal(line, []byte(`\.`)) {
			fingerprints = append(fingerprints, current.result())
			current = nil
			continue
		}
		if !current.addRow(line) {
			return nil, ErrRecoveryData
		}
	}
	if scanner.Err() != nil || current != nil {
		return nil, ErrRecoveryData
	}
	sort.Slice(fingerprints, func(i, j int) bool {
		if fingerprints[i].Schema != fingerprints[j].Schema {
			return fingerprints[i].Schema < fingerprints[j].Schema
		}
		return fingerprints[i].Name < fingerprints[j].Name
	})
	if !validTableFingerprints(fingerprints) {
		return nil, ErrRecoveryData
	}
	return fingerprints, nil
}

func parseCopyHeader(value string) (string, string, []string, bool) {
	parser := copyHeaderParser{value: value, pos: len("COPY ")}
	schema, ok := parser.identifier()
	if !ok || !parser.consume(".") {
		return "", "", nil, false
	}
	name, ok := parser.identifier()
	if !ok {
		return "", "", nil, false
	}
	parser.spaces()
	if !parser.consume("(") {
		return "", "", nil, false
	}
	columns := make([]string, 0)
	for {
		column, valid := parser.identifier()
		if !valid {
			return "", "", nil, false
		}
		columns = append(columns, column)
		parser.spaces()
		if parser.consume(",") {
			continue
		}
		if !parser.consume(")") {
			return "", "", nil, false
		}
		break
	}
	parser.spaces()
	if !parser.consume("FROM stdin;") || parser.pos != len(parser.value) || !validIdentifier(schema) || !validIdentifier(name) || len(columns) == 0 {
		return "", "", nil, false
	}
	seen := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		if !validIdentifier(column) {
			return "", "", nil, false
		}
		if _, exists := seen[column]; exists {
			return "", "", nil, false
		}
		seen[column] = struct{}{}
	}
	return schema, name, columns, true
}

type copyHeaderParser struct {
	value string
	pos   int
}

func (p *copyHeaderParser) spaces() {
	for p.pos < len(p.value) && (p.value[p.pos] == ' ' || p.value[p.pos] == '\t') {
		p.pos++
	}
}

func (p *copyHeaderParser) consume(value string) bool {
	if !strings.HasPrefix(p.value[p.pos:], value) {
		return false
	}
	p.pos += len(value)
	return true
}

func (p *copyHeaderParser) identifier() (string, bool) {
	p.spaces()
	if p.pos >= len(p.value) {
		return "", false
	}
	if p.value[p.pos] == '"' {
		p.pos++
		var value strings.Builder
		for p.pos < len(p.value) {
			c := p.value[p.pos]
			p.pos++
			if c != '"' {
				value.WriteByte(c)
				continue
			}
			if p.pos < len(p.value) && p.value[p.pos] == '"' {
				p.pos++
				value.WriteByte('"')
				continue
			}
			return value.String(), true
		}
		return "", false
	}
	start := p.pos
	for p.pos < len(p.value) {
		c := p.value[p.pos]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start || p.value[start] >= '0' && p.value[start] <= '9' {
		return "", false
	}
	return p.value[start:p.pos], true
}

type copyTableFingerprint struct {
	schema  string
	name    string
	columns []string
	rows    uint64
	sum     [sha256.Size]byte
	xor     [sha256.Size]byte
}

func (f *copyTableFingerprint) addRow(row []byte) bool {
	if f.rows == ^uint64(0) {
		return false
	}
	digest := sha256.Sum256(row)
	carry := uint16(0)
	for index := len(f.sum) - 1; index >= 0; index-- {
		value := uint16(f.sum[index]) + uint16(digest[index]) + carry
		f.sum[index] = byte(value)
		carry = value >> 8
		f.xor[index] ^= digest[index]
	}
	f.rows++
	return true
}

func (f *copyTableFingerprint) result() RecoveryTableFingerprint {
	h := sha256.New()
	io.WriteString(h, "sparc-copy-fingerprint-v1\x00")
	writeFingerprintString(h, f.schema)
	writeFingerprintString(h, f.name)
	binary.Write(h, binary.BigEndian, uint32(len(f.columns)))
	for _, column := range f.columns {
		writeFingerprintString(h, column)
	}
	binary.Write(h, binary.BigEndian, f.rows)
	h.Write(f.sum[:])
	h.Write(f.xor[:])
	return RecoveryTableFingerprint{
		Schema: f.schema, Name: f.name, Columns: append([]string(nil), f.columns...),
		Rows: f.rows, Fingerprint: hex.EncodeToString(h.Sum(nil)),
	}
}

func writeFingerprintString(h hash.Hash, value string) {
	binary.Write(h, binary.BigEndian, uint32(len(value)))
	io.WriteString(h, value)
}
