package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	Format           = "sparc-archive"
	Version          = 1
	maxManifestBytes = 16 << 20
	maxComponents    = 100_000
	maxManifestDepth = 8
	maxScopeBytes    = 4096
	maxTotalBytes    = 128 << 30
)

type Manifest struct {
	Format     string      `json:"format"`
	Version    int         `json:"version"`
	Components []Component `json:"components"`
}

type Component struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Scope  string `json:"scope"`
	Status string `json:"status"`
	Length int64  `json:"length"`
	SHA256 string `json:"sha256"`
}

func (m Manifest) Validate() error {
	if m.Format != Format || m.Version != Version || len(m.Components) == 0 || len(m.Components) > maxComponents {
		return errors.New("unsupported archive format")
	}
	seenIDs := make(map[string]struct{}, len(m.Components))
	seenKeys := make(map[string]struct{}, len(m.Components))
	var total int64
	for i, c := range m.Components {
		if c.ID != fmt.Sprintf("%08x", i) || !validKey(c.Key) || c.Scope == "" || len(c.Scope) > maxScopeBytes || !utf8.ValidString(c.Scope) || hasControl(c.Scope) || (c.Status != "complete" && c.Status != "incomplete") || c.Length < 0 || c.Length > maxTotalBytes-total {
			return errors.New("invalid archive component")
		}
		if _, ok := seenIDs[c.ID]; ok {
			return errors.New("duplicate archive component")
		}
		foldedKey := portableFold(c.Key)
		if _, ok := seenKeys[foldedKey]; ok {
			return errors.New("duplicate archive logical key")
		}
		seenIDs[c.ID] = struct{}{}
		seenKeys[foldedKey] = struct{}{}
		total += c.Length
		if len(c.SHA256) != sha256.Size*2 || strings.ToLower(c.SHA256) != c.SHA256 {
			return errors.New("invalid archive digest")
		}
		if _, err := hex.DecodeString(c.SHA256); err != nil {
			return fmt.Errorf("invalid archive digest: %w", err)
		}
	}
	return nil
}

func ParseManifest(r io.Reader) (Manifest, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return Manifest{}, err
	}
	if len(data) > maxManifestBytes {
		return Manifest{}, errors.New("archive manifest exceeds size limit")
	}
	if !utf8.Valid(data) || !validSurrogateEscapes(data) {
		return Manifest{}, errors.New("invalid archive manifest Unicode")
	}
	if err := checkManifestDepth(data); err != nil {
		return Manifest{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	m, err := parseManifestObject(dec)
	if err != nil {
		return Manifest{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return Manifest{}, errors.New("invalid archive manifest")
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func validSurrogateEscapes(data []byte) bool {
	inString := false
	for i := 0; i < len(data); {
		if !inString {
			inString = data[i] == '"'
			i++
			continue
		}
		if data[i] == '"' {
			inString = false
			i++
			continue
		}
		if data[i] != '\\' {
			i++
			continue
		}
		if i+1 >= len(data) {
			return false
		}
		if data[i+1] != 'u' {
			i += 2
			continue
		}
		if i+6 > len(data) {
			return false
		}
		value, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
		if err != nil {
			return false
		}
		switch {
		case value >= 0xd800 && value <= 0xdbff:
			if i+12 > len(data) || data[i+6] != '\\' || data[i+7] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(data[i+8:i+12]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 12
		case value >= 0xdc00 && value <= 0xdfff:
			return false
		default:
			i += 6
		}
	}
	return !inString
}

func checkManifestDepth(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	depth := 0
	for {
		token, err := dec.Token()
		if err == io.EOF {
			if depth != 0 {
				return errors.New("invalid archive manifest")
			}
			return nil
		}
		if err != nil {
			return errors.New("invalid archive manifest")
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				depth++
				if depth > maxManifestDepth {
					return errors.New("archive manifest exceeds nesting limit")
				}
			case '}', ']':
				depth--
				if depth < 0 {
					return errors.New("invalid archive manifest")
				}
			}
		}
	}
}

func parseManifestObject(dec *json.Decoder) (Manifest, error) {
	if err := begin(dec, '{'); err != nil {
		return Manifest{}, err
	}
	var m Manifest
	seen := map[string]bool{}
	for dec.More() {
		key, err := objectKey(dec, seen, "format", "version", "components")
		if err != nil {
			return Manifest{}, err
		}
		switch key {
		case "format":
			err = dec.Decode(&m.Format)
		case "version":
			m.Version, err = decodeInt(dec)
		case "components":
			m.Components, err = parseComponents(dec)
		}
		if err != nil {
			return Manifest{}, errors.New("invalid archive manifest")
		}
	}
	if err := end(dec, '}'); err != nil || !seen["format"] || !seen["version"] || !seen["components"] {
		return Manifest{}, errors.New("invalid archive manifest")
	}
	return m, nil
}

func parseComponents(dec *json.Decoder) ([]Component, error) {
	if err := begin(dec, '['); err != nil {
		return nil, err
	}
	components := make([]Component, 0)
	for dec.More() {
		if len(components) == maxComponents {
			return nil, errors.New("archive manifest exceeds component limit")
		}
		component, err := parseComponentObject(dec)
		if err != nil {
			return nil, err
		}
		components = append(components, component)
	}
	if err := end(dec, ']'); err != nil {
		return nil, err
	}
	return components, nil
}

func parseComponentObject(dec *json.Decoder) (Component, error) {
	if err := begin(dec, '{'); err != nil {
		return Component{}, err
	}
	var c Component
	seen := map[string]bool{}
	for dec.More() {
		key, err := objectKey(dec, seen, "id", "key", "scope", "status", "length", "sha256")
		if err != nil {
			return Component{}, err
		}
		switch key {
		case "id":
			err = dec.Decode(&c.ID)
		case "key":
			err = dec.Decode(&c.Key)
		case "scope":
			err = dec.Decode(&c.Scope)
		case "status":
			err = dec.Decode(&c.Status)
		case "length":
			c.Length, err = decodeInt64(dec)
		case "sha256":
			err = dec.Decode(&c.SHA256)
		}
		if err != nil {
			return Component{}, errors.New("invalid archive manifest")
		}
	}
	if err := end(dec, '}'); err != nil || len(seen) != 6 {
		return Component{}, errors.New("invalid archive manifest")
	}
	return c, nil
}

func objectKey(dec *json.Decoder, seen map[string]bool, allowed ...string) (string, error) {
	token, err := dec.Token()
	key, ok := token.(string)
	if err != nil || !ok || seen[key] {
		return "", errors.New("invalid archive manifest")
	}
	for _, name := range allowed {
		if key == name {
			seen[key] = true
			return key, nil
		}
	}
	return "", errors.New("invalid archive manifest")
}

func begin(dec *json.Decoder, want rune) error { return delimiter(dec, want) }
func end(dec *json.Decoder, want rune) error   { return delimiter(dec, want) }

func delimiter(dec *json.Decoder, want rune) error {
	token, err := dec.Token()
	if err != nil || token != json.Delim(want) {
		return errors.New("invalid archive manifest")
	}
	return nil
}

func decodeInt(dec *json.Decoder) (int, error) {
	n, err := decodeInt64(dec)
	if err != nil || int64(int(n)) != n {
		return 0, errors.New("invalid archive manifest")
	}
	return int(n), nil
}

func decodeInt64(dec *json.Decoder) (int64, error) {
	var number json.Number
	if err := dec.Decode(&number); err != nil {
		return 0, err
	}
	return strconv.ParseInt(number.String(), 10, 64)
}

func validKey(key string) bool {
	if key == "" || len(key) > maxScopeBytes || !utf8.ValidString(key) || hasControl(key) || strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") || strings.Contains(key, `\\`) || strings.Contains(key, ":") {
		return false
	}
	parts := strings.Split(key, "/")
	if len(parts) > 64 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 255 || strings.ContainsAny(part, `<>:"|?*`) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || windowsDeviceName(part) {
			return false
		}
	}
	return true
}

func windowsDeviceName(part string) bool {
	base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
		suffix := []rune(base)[3:]
		return len(suffix) == 1 && strings.ContainsRune("123456789¹²³", suffix[0])
	}
	return false
}

func portableFold(value string) string {
	var folded strings.Builder
	for _, r := range value {
		min := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < min {
				min = next
			}
		}
		folded.WriteRune(min)
	}
	return folded.String()
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '\u2028' || r == '\u2029' {
			return true
		}
	}
	return false
}
