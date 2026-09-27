//go:build hosted

package database

import (
	"strings"
	"testing"
	"time"
)

const validHostedProbeConfigJSON = `{"version":1,"authorization_scope":"readonly-public-auth-metadata-v1","source_project_ref":"abcdefghijklmnopqrstuvwx","connection_url_file":"/private/sparc-session-url","not_before":"2026-09-26T11:55:00Z","expires_at":"2026-09-26T12:20:00Z","cost_cap_usd":0}`

func TestParseHostedProbeConfigAcceptsOnlyCurrentMetadataScope(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	contract, err := parseHostedProbeConfig([]byte(validHostedProbeConfigJSON), now)
	if err != nil {
		t.Fatalf("valid hosted probe contract rejected: %v", err)
	}
	if contract.sourceProjectRef != "abcdefghijklmnopqrstuvwx" || contract.connectionURLFile != "/private/sparc-session-url" || !contract.expiresAt.Equal(time.Date(2026, 9, 26, 12, 20, 0, 0, time.UTC)) {
		t.Fatal("hosted probe contract did not retain its exact non-secret scope")
	}
}

func TestParseHostedProbeConfigAllowsLiteralEscapedUnicodeText(t *testing.T) {
	raw := replaceHostedProbeField(validHostedProbeConfigJSON, `"connection_url_file":"/private/sparc-session-url"`, `"connection_url_file":"/private/\\uD800"`)
	if _, err := parseHostedProbeConfig([]byte(raw), time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("literal backslash-u path text was rejected: %v", err)
	}
}

func TestParseHostedProbeConfigRejectsUnknownStaleAndBroaderScopes(t *testing.T) {
	windowTooLong := `"expires_at":"2026-09-26T13:00:00Z"`
	duplicate := `"source_project_ref":"abcdefghijklmnopqrstuvwx","source_project_ref":"zyxwvutsrqponmlkjihgfedcba"`
	escapedDuplicate := `"source_project_ref":"abcdefghijklmnopqrstuvwx","source_\u0070roject_ref":"zyxwvutsrqponmlkjihgfedcba"`
	unknownTarget := `,"target_project_ref":"another-project"`
	for _, test := range []struct {
		name string
		raw  string
	}{
		{"duplicate field", replaceHostedProbeField(validHostedProbeConfigJSON, `"source_project_ref":"abcdefghijklmnopqrstuvwx"`, duplicate)},
		{"escaped duplicate field", replaceHostedProbeField(validHostedProbeConfigJSON, `"source_project_ref":"abcdefghijklmnopqrstuvwx"`, escapedDuplicate)},
		{"unknown target scope", validHostedProbeConfigJSON[:len(validHostedProbeConfigJSON)-1] + unknownTarget + "}"},
		{"wrong operation", replaceHostedProbeField(validHostedProbeConfigJSON, `"authorization_scope":"readonly-public-auth-metadata-v1"`, `"authorization_scope":"database-backup"`)},
		{"bad source project ref", replaceHostedProbeField(validHostedProbeConfigJSON, `"source_project_ref":"abcdefghijklmnopqrstuvwx"`, `"source_project_ref":"not a ref"`)},
		{"relative credential path", replaceHostedProbeField(validHostedProbeConfigJSON, `"connection_url_file":"/private/sparc-session-url"`, `"connection_url_file":"relative/url"`)},
		{"unpaired surrogate", replaceHostedProbeField(validHostedProbeConfigJSON, `"connection_url_file":"/private/sparc-session-url"`, `"connection_url_file":"/private/\uD800"`)},
		{"not yet authorized", replaceHostedProbeField(validHostedProbeConfigJSON, `"not_before":"2026-09-26T11:55:00Z"`, `"not_before":"2026-09-26T12:01:00Z"`)},
		{"expired authorization", replaceHostedProbeField(validHostedProbeConfigJSON, `"expires_at":"2026-09-26T12:20:00Z"`, `"expires_at":"2026-09-26T11:59:59Z"`)},
		{"window exceeds policy", replaceHostedProbeField(validHostedProbeConfigJSON, `"expires_at":"2026-09-26T12:20:00Z"`, windowTooLong)},
		{"nonzero cost cap", replaceHostedProbeField(validHostedProbeConfigJSON, `"cost_cap_usd":0`, `"cost_cap_usd":1`)},
		{"null cost cap", replaceHostedProbeField(validHostedProbeConfigJSON, `"cost_cap_usd":0`, `"cost_cap_usd":null`)},
		{"non-integer version", replaceHostedProbeField(validHostedProbeConfigJSON, `"version":1`, `"version":1.0`)},
		{"trailing JSON", validHostedProbeConfigJSON + ` {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
			if _, err := parseHostedProbeConfig([]byte(test.raw), now); err == nil || err != errHostedProbeContract {
				t.Fatalf("parseHostedProbeConfig() error = %v, want fixed hosted-contract refusal", err)
			}
		})
	}
}

func replaceHostedProbeField(raw, old, replacement string) string {
	return strings.Replace(raw, old, replacement, 1)
}
