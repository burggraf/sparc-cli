//go:build hosted

package database

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/burggraf/sparc-cli/internal/credentials"
)

func TestParseHostedSupavisorSessionURL(t *testing.T) {
	params, password, err := parseHostedSupavisorSessionURL(
		[]byte("postgresql://postgres."+testProjectRef+":p%40ss%3Aword@aws-1-us-east-2.pooler.supabase.com:5432/postgres?sslmode=require"),
		testProjectRef,
	)
	if err != nil {
		t.Fatalf("parseHostedSupavisorSessionURL() = %v", err)
	}
	if params.ExpectedProjectRef != testProjectRef || params.Host != "aws-1-us-east-2.pooler.supabase.com" ||
		params.Port != directPort || params.Database != "postgres" || params.User != "postgres."+testProjectRef ||
		params.SSLMode != "verify-full" || params.SSLRootCert != "supabase" || string(password) != "p@ss:word" {
		t.Fatalf("parsed connection parameters/password did not match the expected route")
	}
}

func TestHostedReadOnlySupavisorProbe(t *testing.T) {
	configPath := os.Getenv("SPARC_HOSTED_TEST_CONFIG")
	if configPath == "" {
		t.Skip("explicit private, time-bounded authorization config is required")
	}
	contract, err := loadHostedProbeConfig(configPath, time.Now())
	if err != nil {
		t.Fatal("hosted test contract refused")
	}
	ctx, cancel := context.WithDeadline(context.Background(), contract.expiresAt)
	defer cancel()
	urlBytes, err := credentials.Input(&credentials.Reference{File: contract.connectionURLFile}, nil, nil)
	if err != nil {
		t.Fatal("hosted connection input unavailable")
	}
	defer clear(urlBytes)
	params, password, err := parseHostedSupavisorSessionURL(urlBytes, contract.sourceProjectRef)
	if err != nil {
		t.Fatal("hosted session route rejected")
	}
	defer clear(password)
	metadata, err := ObservePublicAuthMetadata(ctx, params, password)
	if err != nil {
		t.Fatalf("read-only public/auth metadata probe failed: %v", err)
	}
	if !metadata.Catalog.TLS || !metadata.Catalog.ReadOnly || metadata.Catalog.ServerMajor != supportedPostgresMajor || len(metadata.Catalog.Schemas) != 2 {
		t.Fatal("hosted read-only public/auth probe did not meet the expected checks")
	}
	t.Logf("read-only public/auth metadata probe passed: PostgreSQL 17, verified TLS, read-only transaction, columns=%d constraints=%d", len(metadata.Columns), len(metadata.Constraints))
}

func parseHostedSupavisorSessionURL(raw []byte, expectedProjectRef string) (ConnectionParams, []byte, error) {
	if len(raw) == 0 || len(raw) > 16*1024 || string(raw) != strings.TrimSpace(string(raw)) {
		return ConnectionParams{}, nil, ErrConnectionParameters
	}
	parsed, err := url.Parse(string(raw))
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" ||
		parsed.Opaque != "" || parsed.User == nil || parsed.ForceQuery || parsed.Fragment != "" ||
		parsed.RawFragment != "" || parsed.Path != "/postgres" || parsed.RawPath != "" {
		return ConnectionParams{}, nil, ErrConnectionParameters
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return ConnectionParams{}, nil, ErrConnectionParameters
	}
	for key := range query {
		if key != "sslmode" {
			return ConnectionParams{}, nil, ErrConnectionParameters
		}
	}
	if modes, ok := query["sslmode"]; ok && (len(modes) != 1 || modes[0] != "require" && modes[0] != "verify-full") {
		return ConnectionParams{}, nil, ErrConnectionParameters
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || port != strconv.Itoa(directPort) {
		return ConnectionParams{}, nil, ErrConnectionParameters
	}
	password, hasPassword := parsed.User.Password()
	params := ConnectionParams{
		ExpectedProjectRef: expectedProjectRef,
		Host:               host,
		Port:               directPort,
		Database:           strings.TrimPrefix(parsed.Path, "/"),
		User:               parsed.User.Username(),
		SSLMode:            "verify-full",
		SSLRootCert:        "supabase",
	}
	if !hasPassword || ClassifyRoute(expectedProjectRef, params.Host, params.Port) != RouteSessionPooler ||
		params.Validate() != nil || credentials.ValidatePGPassEntry(credentials.PGPassEntry{
		Host: params.Host, Port: params.Port, Database: params.Database, User: params.User, Password: []byte(password),
	}) != nil {
		return ConnectionParams{}, nil, ErrConnectionParameters
	}
	return params, []byte(password), nil
}

func TestParseHostedSupavisorSessionURLRejectsUnsafeInputs(t *testing.T) {
	valid := "postgresql://postgres." + testProjectRef + ":secret-canary@aws-1-us-east-2.pooler.supabase.com:5432/postgres"
	for _, test := range []struct {
		name, raw, projectRef string
	}{
		{"wrong tenant", strings.Replace(valid, "postgres."+testProjectRef, "postgres.otherprojectref1234", 1), testProjectRef},
		{"transaction pooling", strings.Replace(valid, ":5432/", ":6543/", 1), testProjectRef},
		{"direct endpoint", strings.Replace(valid, "aws-1-us-east-2.pooler.supabase.com", "db."+testProjectRef+".supabase.co", 1), testProjectRef},
		{"wrong database", strings.Replace(valid, "/postgres", "/other", 1), testProjectRef},
		{"weak TLS", valid + "?sslmode=disable", testProjectRef},
		{"unknown query option", valid + "?options=-c%20search_path%3Dpublic", testProjectRef},
		{"fragment", valid + "#secret-canary", testProjectRef},
		{"wrong expected ref", valid, "zyxwvutsrqponmlkjihgfedcba"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := parseHostedSupavisorSessionURL([]byte(test.raw), test.projectRef); err == nil || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("parseHostedSupavisorSessionURL() = %v, want sanitized refusal", err)
			}
		})
	}
}
