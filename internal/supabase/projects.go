package supabase

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"sort"
	"strconv"
	"unicode"
	"unicode/utf8"
)

const (
	pageSize               = 100
	maxProjects            = 10_000
	maxProjectListBytes    = 16 << 20
	maxProjectNameBytes    = 256
	maxProjectMetadataSize = 128
)

// Project contains bounded identity metadata and explicitly unknown capabilities.
type Project struct {
	Ref            string
	Name           string
	OrganizationID string
	Region         string
	Status         string
	Capabilities   ProjectCapabilities
	UnknownFields  []string
}

// ListProjects reads the fixed, offset-paginated project endpoint. A successful
// empty list means no projects were returned to this token; permission errors
// are returned distinctly and never converted to absence.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	projects := make([]Project, 0)
	seen := make(map[string]struct{})
	var totalBytes int
	for offset := 0; offset < maxProjects; offset += pageSize {
		data, err := c.get(ctx, "/projects", url.Values{"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}})
		if err != nil {
			return nil, err
		}
		totalBytes += len(data)
		if totalBytes > maxProjectListBytes {
			return nil, ErrPaginationLimit
		}
		page, err := decodeProjectPage(data)
		if err != nil || len(page) > pageSize {
			return nil, ErrMalformedResponse
		}
		if len(projects)+len(page) > maxProjects {
			return nil, ErrPaginationLimit
		}
		for _, project := range page {
			if _, exists := seen[project.Ref]; exists {
				return nil, ErrMalformedResponse
			}
			seen[project.Ref] = struct{}{}
			projects = append(projects, project)
		}
		if len(page) < pageSize {
			return projects, nil
		}
	}
	return nil, ErrPaginationLimit
}

func decodeProjectPage(data []byte) ([]Project, error) {
	if !utf8.Valid(data) || !validJSONSurrogates(data) {
		return nil, ErrMalformedResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, ErrMalformedResponse
	}
	projects := make([]Project, 0)
	for decoder.More() {
		if len(projects) == pageSize {
			return nil, ErrMalformedResponse
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return nil, ErrMalformedResponse
		}
		project, err := decodeProject(raw)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim(']') {
		return nil, ErrMalformedResponse
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrMalformedResponse
	}
	return projects, nil
}

func validJSONSurrogates(data []byte) bool {
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

func decodeProject(data []byte) (Project, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return Project{}, ErrMalformedResponse
	}
	project := Project{Capabilities: ProjectCapabilities{
		DatabaseVersion:       CapabilityObservation{State: CapabilityUnknown},
		FeatureInventoryState: CapabilityUnknown,
	}}
	seen := make(map[string]struct{}, 7)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return Project{}, ErrMalformedResponse
		}
		if _, exists := seen[key]; exists {
			return Project{}, ErrMalformedResponse
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return Project{}, ErrMalformedResponse
		}
		switch key {
		case "id":
			project.Ref, err = decodeString(value, 128, true)
		case "name":
			project.Name, err = decodeString(value, maxProjectNameBytes, true)
		case "organization_id":
			project.OrganizationID, err = decodeString(value, maxProjectMetadataSize, true)
		case "region":
			project.Region, err = decodeString(value, maxProjectMetadataSize, true)
		case "status":
			project.Status, err = decodeString(value, maxProjectMetadataSize, true)
		case "db_version":
			var version string
			version, err = decodeString(value, maxProjectMetadataSize, false)
			if err == nil && version != "" {
				if !safeAPIText(version) {
					return Project{}, ErrMalformedResponse
				}
				project.Capabilities.DatabaseVersion = CapabilityObservation{State: CapabilityObserved, Value: version}
			}
		default:
			if len(key) == 0 || len(key) > maxProjectMetadataSize || !safeAPIText(key) {
				return Project{}, ErrMalformedResponse
			}
			project.UnknownFields = append(project.UnknownFields, key)
		}
		if err != nil {
			return Project{}, ErrMalformedResponse
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return Project{}, ErrMalformedResponse
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Project{}, ErrMalformedResponse
	}
	for _, required := range []string{"id", "name", "organization_id", "region", "status"} {
		if _, ok := seen[required]; !ok {
			return Project{}, ErrMalformedResponse
		}
	}
	if !validProjectRef(project.Ref) || !safeAPIText(project.Name) || !safeAPIText(project.OrganizationID) || !safeAPIText(project.Region) || !safeAPIText(project.Status) {
		return Project{}, ErrMalformedResponse
	}
	sort.Strings(project.UnknownFields)
	return project, nil
}

func decodeString(raw json.RawMessage, maxBytes int, required bool) (string, error) {
	if string(raw) == "null" && !required {
		return "", nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || len(value) > maxBytes || (required && value == "") || !utf8.ValidString(value) {
		return "", ErrMalformedResponse
	}
	return value, nil
}

func validProjectRef(ref string) bool {
	if ref == "" || len(ref) > 128 {
		return false
	}
	for i := 0; i < len(ref); i++ {
		c := ref[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func safeAPIText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}
