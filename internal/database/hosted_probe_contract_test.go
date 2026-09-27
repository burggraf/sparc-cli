//go:build hosted

package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/burggraf/sparc-cli/internal/platform"
)

const (
	maxHostedProbeConfigBytes = 8192
	hostedProbeMaxWindow      = 30 * time.Minute
	hostedProbeScope          = "readonly-public-auth-metadata-v1"
)

var errHostedProbeContract = errors.New("hosted test contract refused")

type hostedProbeContract struct {
	sourceProjectRef  string
	connectionURLFile string
	expiresAt         time.Time
}

func loadHostedProbeConfig(path string, now time.Time) (hostedProbeContract, error) {
	if len(path) == 0 || len(path) > maxCAPath || !filepath.IsAbs(path) || filepath.Clean(path) != path || !validText(path) {
		return hostedProbeContract{}, errHostedProbeContract
	}
	data, err := platform.ReadPrivateFile(path, maxHostedProbeConfigBytes)
	if err != nil {
		return hostedProbeContract{}, errHostedProbeContract
	}
	return parseHostedProbeConfig(data, now)
}

func parseHostedProbeConfig(data []byte, now time.Time) (hostedProbeContract, error) {
	if len(data) == 0 || len(data) > maxHostedProbeConfigBytes || !utf8.Valid(data) || !json.Valid(data) || !hostedJSONPairedSurrogates(data) {
		return hostedProbeContract{}, errHostedProbeContract
	}
	fields, err := hostedProbeFields(data)
	if err != nil || len(fields) != 7 {
		return hostedProbeContract{}, errHostedProbeContract
	}
	for _, key := range []string{"version", "authorization_scope", "source_project_ref", "connection_url_file", "not_before", "expires_at", "cost_cap_usd"} {
		if _, ok := fields[key]; !ok {
			return hostedProbeContract{}, errHostedProbeContract
		}
	}
	if !bytes.Equal(bytes.TrimSpace(fields["version"]), []byte("1")) ||
		!bytes.Equal(bytes.TrimSpace(fields["cost_cap_usd"]), []byte("0")) {
		return hostedProbeContract{}, errHostedProbeContract
	}

	scope, scopeOK := hostedJSONString(fields["authorization_scope"])
	projectRef, refOK := hostedJSONString(fields["source_project_ref"])
	urlFile, pathOK := hostedJSONString(fields["connection_url_file"])
	notBeforeText, startOK := hostedJSONString(fields["not_before"])
	expiresText, expiresOK := hostedJSONString(fields["expires_at"])
	if !scopeOK || scope != hostedProbeScope || !refOK || !validProjectRefLabel(projectRef) ||
		!pathOK || len(urlFile) > maxCAPath || !filepath.IsAbs(urlFile) || filepath.Clean(urlFile) != urlFile || !validText(urlFile) ||
		!startOK || !expiresOK {
		return hostedProbeContract{}, errHostedProbeContract
	}
	notBefore, err := time.Parse(time.RFC3339, notBeforeText)
	if err != nil {
		return hostedProbeContract{}, errHostedProbeContract
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresText)
	if err != nil || notBefore.After(now) || !expiresAt.After(now) || !expiresAt.After(notBefore) || expiresAt.Sub(notBefore) > hostedProbeMaxWindow {
		return hostedProbeContract{}, errHostedProbeContract
	}
	return hostedProbeContract{sourceProjectRef: projectRef, connectionURLFile: urlFile, expiresAt: expiresAt}, nil
}

func hostedProbeFields(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errHostedProbeContract
	}
	fields := make(map[string]json.RawMessage, 7)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errHostedProbeContract
		}
		if _, exists := fields[key]; exists {
			return nil, errHostedProbeContract
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errHostedProbeContract
		}
		fields[key] = value
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return nil, errHostedProbeContract
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errHostedProbeContract
	}
	return fields, nil
}

func hostedJSONString(raw []byte) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '"' {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || !validText(value) {
		return "", false
	}
	return value, true
}

func hostedJSONPairedSurrogates(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		code, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, _ := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
