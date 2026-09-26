package database

import (
	"path/filepath"
	"strings"
	"testing"
)

const testProjectRef = "abcdefghijklmnopqrst"

func validConnectionParams(t *testing.T) ConnectionParams {
	t.Helper()
	return ConnectionParams{
		ExpectedProjectRef: testProjectRef,
		Host:               "db." + testProjectRef + ".supabase.co",
		Port:               5432,
		Database:           "postgres",
		User:               "postgres",
		SSLMode:            "verify-full",
		SSLRootCert:        filepath.Join(t.TempDir(), "root.crt"),
	}
}

func TestClassifyRoute(t *testing.T) {
	for _, test := range []struct {
		name, projectRef, host string
		port                   uint16
		want                   RouteKind
	}{
		{"direct exact project", testProjectRef, "db." + testProjectRef + ".supabase.co", 5432, RouteDirect},
		{"session pooler", testProjectRef, "aws-0-us-east-1.pooler.supabase.com", 5432, RouteSessionPooler},
		{"transaction pooler", testProjectRef, "aws-0-us-east-1.pooler.supabase.com", 6543, RouteTransactionPooler},
		{"wrong direct project", testProjectRef, "db.otherprojectref1234.supabase.co", 5432, RouteUnknown},
		{"pooler wrong domain", testProjectRef, "aws-0-us-east-1.pooler.attacker.test", 5432, RouteUnknown},
		{"pooler unexpected port", testProjectRef, "aws-0-us-east-1.pooler.supabase.com", 5440, RouteUnknown},
		{"malformed pooler hostname", testProjectRef, "evil.aws-0-us-east-1.pooler.supabase.com", 5432, RouteUnknown},
		{"pooler index is not numeric", testProjectRef, "aws-x-us-east-1.pooler.supabase.com", 5432, RouteUnknown},
		{"pooler region missing", testProjectRef, "aws-1-.pooler.supabase.com", 5432, RouteUnknown},
		{"invalid ref", "bad.ref", "db.bad.ref.supabase.co", 5432, RouteUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyRoute(test.projectRef, test.host, test.port); got != test.want {
				t.Fatalf("ClassifyRoute() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidateConnectionParametersAcceptsCanonicalDirectRoute(t *testing.T) {
	params := validConnectionParams(t)
	if err := params.Validate(); err != nil {
		t.Fatalf("valid direct parameters rejected: %v", err)
	}
}

func TestValidateConnectionParametersAcceptsProjectBoundSessionPooler(t *testing.T) {
	params := validConnectionParams(t)
	params.Host = "aws-1-us-east-2.pooler.supabase.com"
	params.User = "postgres." + testProjectRef
	if err := params.Validate(); err != nil {
		t.Fatalf("valid session-pooler parameters rejected: %v", err)
	}
}

func TestValidateConnectionParametersRejectsMismatchedSessionPoolerIdentity(t *testing.T) {
	params := validConnectionParams(t)
	params.Host = "aws-1-us-east-2.pooler.supabase.com"
	params.User = "postgres.otherprojectref1234"
	if err := params.Validate(); err != ErrConnectionParameters {
		t.Fatalf("Validate() = %v, want fixed parameter error", err)
	}
}

func TestValidateConnectionParametersRejectsTransactionPooler(t *testing.T) {
	params := validConnectionParams(t)
	params.Host = "aws-1-us-east-2.pooler.supabase.com"
	params.Port = transactionPort
	params.User = "postgres." + testProjectRef
	if err := params.Validate(); err != ErrUnsupportedRoute {
		t.Fatalf("Validate() = %v, want unsupported-route refusal", err)
	}
}

func TestValidateConnectionParametersRequiresVerifyFullAndExplicitTrustSource(t *testing.T) {
	for _, mode := range []string{"", "disable", "allow", "prefer", "require", "verify-ca", "VERIFY-FULL"} {
		t.Run(mode, func(t *testing.T) {
			params := validConnectionParams(t)
			params.SSLMode = mode
			if err := params.Validate(); err != ErrConnectionParameters {
				t.Fatalf("Validate() = %v, want fixed parameter error", err)
			}
		})
	}
	for _, root := range []string{"", "root.crt", "https://example.test/root.crt"} {
		t.Run("root "+root, func(t *testing.T) {
			params := validConnectionParams(t)
			params.SSLRootCert = root
			if err := params.Validate(); err != ErrConnectionParameters {
				t.Fatalf("Validate() = %v, want fixed parameter error", err)
			}
		})
	}
	for _, root := range []string{"system", "supabase"} {
		t.Run(root, func(t *testing.T) {
			params := validConnectionParams(t)
			params.SSLRootCert = root
			if err := params.Validate(); err != nil {
				t.Fatalf("explicit %s trust rejected: %v", root, err)
			}
		})
	}
}

func TestValidateConnectionParametersRejectsMalformedOrMismatchedInputs(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*ConnectionParams)
	}{
		{"missing project ref", func(p *ConnectionParams) { p.ExpectedProjectRef = "" }},
		{"invalid project ref", func(p *ConnectionParams) { p.ExpectedProjectRef = "bad.ref" }},
		{"project ref not bound to host", func(p *ConnectionParams) { p.ExpectedProjectRef = "zyxwvutsrqponmlkjihgfedcba" }},
		{"host case mismatch", func(p *ConnectionParams) { p.Host = "DB." + testProjectRef + ".supabase.co" }},
		{"trailing host dot", func(p *ConnectionParams) { p.Host += "." }},
		{"host URL", func(p *ConnectionParams) { p.Host = "postgres://db." + testProjectRef + ".supabase.co" }},
		{"wrong port", func(p *ConnectionParams) { p.Port = 6543 }},
		{"zero port", func(p *ConnectionParams) { p.Port = 0 }},
		{"missing database", func(p *ConnectionParams) { p.Database = "" }},
		{"oversized database", func(p *ConnectionParams) { p.Database = strings.Repeat("d", 64) }},
		{"database control", func(p *ConnectionParams) { p.Database = "postgres\n" }},
		{"missing user", func(p *ConnectionParams) { p.User = "" }},
		{"oversized user", func(p *ConnectionParams) { p.User = strings.Repeat("u", 64) }},
		{"user control", func(p *ConnectionParams) { p.User = "postgres\x00" }},
		{"unclean CA path", func(p *ConnectionParams) { p.SSLRootCert = "/tmp/../root.crt" }},
		{"CA path control", func(p *ConnectionParams) { p.SSLRootCert = "/tmp/root\u001b.crt" }},
		{"CA path unicode control", func(p *ConnectionParams) { p.SSLRootCert = "/tmp/root\u0085.crt" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			params := validConnectionParams(t)
			test.edit(&params)
			if err := params.Validate(); err != ErrConnectionParameters {
				t.Fatalf("Validate() = %v, want fixed parameter error", err)
			}
		})
	}
}

func TestConnectionValidationErrorsDoNotEchoInputs(t *testing.T) {
	params := validConnectionParams(t)
	params.Host = "secret-canary.invalid"
	params.User = "secret-canary"
	if err := params.Validate(); err != ErrConnectionParameters || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("Validate() exposed input or returned wrong error: %v", err)
	}
}
