//go:build hosted

package database

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/burggraf/sparc-cli/internal/credentials"
)

func TestParseHostedSupavisorSessionURL(t *testing.T) {
	params, password, err := ParseSupavisorSessionURL(
		[]byte("postgresql://postgres."+testProjectRef+":p%40ss%3Aword@aws-1-us-east-2.pooler.supabase.com:5432/postgres?sslmode=require"),
		testProjectRef,
	)
	if err != nil {
		t.Fatalf("ParseSupavisorSessionURL() = %v", err)
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
	params, password, err := ParseSupavisorSessionURL(urlBytes, contract.sourceProjectRef)
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
	t.Logf("read-only public/auth metadata probe passed: PostgreSQL 17, verified TLS, read-only transaction, column_rows=%d constraint_rows=%d", len(metadata.Columns), len(metadata.Constraints))
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
			if _, _, err := ParseSupavisorSessionURL([]byte(test.raw), test.projectRef); err == nil || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("ParseSupavisorSessionURL() = %v, want sanitized refusal", err)
			}
		})
	}
}
