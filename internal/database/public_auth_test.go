package database

import (
	"context"
	"testing"
)

func TestObservePublicAuthMetadataRejectsNilContextAndUnsupportedRoute(t *testing.T) {
	if _, err := ObservePublicAuthMetadata(nil, ConnectionParams{}, nil); err != ErrCatalogObservation {
		t.Fatalf("nil-context error = %v, want catalog refusal", err)
	}

	clearDatabaseEnvironment(t)
	params := validConnectionParams(t)
	params.Host = "aws-0-us-east-1.pooler.supabase.com"
	params.Port = transactionPort
	params.User = "postgres." + testProjectRef
	if _, err := ObservePublicAuthMetadata(context.Background(), params, []byte("explicit-password")); err != ErrUnsupportedRoute {
		t.Fatalf("transaction-pooler error = %v, want unsupported-route refusal", err)
	}
}

func TestPublicAuthSchemaSelectionIsExact(t *testing.T) {
	for _, test := range []struct {
		name  string
		names []string
		want  bool
	}{
		{name: "exact", names: []string{"public", "auth"}, want: true},
		{name: "reversed", names: []string{"auth", "public"}},
		{name: "extra schema", names: []string{"public", "auth", "storage"}},
		{name: "partial", names: []string{"public"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isPublicAuthSchemaSelection(test.names); got != test.want {
				t.Fatalf("isPublicAuthSchemaSelection(%v) = %t, want %t", test.names, got, test.want)
			}
		})
	}
}
