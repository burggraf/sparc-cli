package database

import (
	"strings"
	"testing"
)

func TestParseSupavisorSessionURL(t *testing.T) {
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
		t.Fatalf("parsed connection parameters/password did not match expected session route")
	}
}

func TestParseSupavisorSessionURLRejectsUnsafeInputWithoutLeakingPassword(t *testing.T) {
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
		{"missing password", "postgresql://postgres." + testProjectRef + "@aws-1-us-east-2.pooler.supabase.com:5432/postgres", testProjectRef},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ParseSupavisorSessionURL([]byte(test.raw), test.projectRef)
			if err != ErrConnectionParameters || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("ParseSupavisorSessionURL() = %v, want sanitized connection refusal", err)
			}
		})
	}
}
