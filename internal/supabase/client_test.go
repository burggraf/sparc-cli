package supabase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewClientUsesFixedEndpointAndRejectsUnsafeTokens(t *testing.T) {
	client, err := NewClient("synthetic-token")
	if err != nil {
		t.Fatal(err)
	}
	if client.baseURL != managementAPIBaseURL {
		t.Fatalf("endpoint = %q", client.baseURL)
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("client inherited an ambient proxy")
	}
	for _, token := range []string{"", "has space", "bad\r\nheader", strings.Repeat("x", maxTokenBytes+1)} {
		if _, err := NewClient(token); !errors.Is(err, ErrClient) {
			t.Errorf("NewClient(%q) error = %v", token, err)
		}
	}
}

func TestListProjectsSendsOnlyBearerOnFixedReadOnlyRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects" {
			t.Errorf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer synthetic-token" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		if r.URL.Query().Get("limit") != fmt.Sprint(pageSize) || r.URL.Query().Get("offset") != "0" {
			t.Errorf("query = %v", r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"project_1","name":"Rocket \ud83d\ude80","organization_id":"org_1","region":"us-east-1","status":"ACTIVE","db_version":"17.6","new_field":"secret-canary"}]`))
	}))
	defer server.Close()
	projects, err := testClient(server).ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Ref != "project_1" || projects[0].Name != "Rocket 🚀" || projects[0].Capabilities.DatabaseVersion != (CapabilityObservation{State: CapabilityObserved, Value: "17.6"}) || projects[0].Capabilities.FeatureInventoryState != CapabilityUnknown {
		t.Fatalf("projects = %#v", projects)
	}
	if len(projects[0].UnknownFields) != 1 || projects[0].UnknownFields[0] != "new_field" || strings.Contains(fmt.Sprint(projects[0].UnknownFields), "secret-canary") {
		t.Fatalf("unknown fields = %#v", projects[0].UnknownFields)
	}
}

func TestListProjectsPaginatesWithBoundedOffsets(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("limit") != fmt.Sprint(pageSize) {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		start, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil {
			t.Fatal(err)
		}
		count := pageSize
		if start == pageSize {
			count = 1
		}
		rows := make([]map[string]any, count)
		for i := range rows {
			rows[i] = map[string]any{"id": fmt.Sprintf("project-%05d", start+i), "name": "Fixture", "organization_id": "org_1", "region": "us-east-1", "status": "ACTIVE"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer server.Close()
	projects, err := testClient(server).ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != pageSize+1 || requests != 2 {
		t.Fatalf("projects=%d requests=%d", len(projects), requests)
	}
	if projects[pageSize].Capabilities.DatabaseVersion.State != CapabilityUnknown || projects[pageSize].Capabilities.FeatureInventoryState != CapabilityUnknown {
		t.Fatalf("unreported capabilities were treated as known: %#v", projects[pageSize].Capabilities)
	}
}

func TestListProjectsDistinguishesEmptyFromForbidden(t *testing.T) {
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	if projects, err := testClient(forbidden).ListProjects(context.Background()); !errors.Is(err, ErrForbidden) || projects != nil {
		t.Fatalf("forbidden result = %#v, %v", projects, err)
	}
	forbidden.Close()
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }))
	defer empty.Close()
	projects, err := testClient(empty).ListProjects(context.Background())
	if err != nil || projects == nil || len(projects) != 0 {
		t.Fatalf("empty result = %#v, %v", projects, err)
	}
}

func TestListProjectsRejectsMalformedAndDuplicateFields(t *testing.T) {
	for _, body := range []string{`{}`, `[{"id":"x","id":"y","name":"n","organization_id":"o","region":"r","status":"ACTIVE"}]`, `[{"id":"x","name":"bad\u0085name","organization_id":"o","region":"r","status":"ACTIVE"}]`, `[{"id":"x","name":"bad\ud800","organization_id":"o","region":"r","status":"ACTIVE"}]`, `not-json`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		if _, err := testClient(server).ListProjects(context.Background()); !errors.Is(err, ErrMalformedResponse) {
			t.Errorf("body %q error = %v", body, err)
		}
		server.Close()
	}
}

func TestClientRefusesRedirectWithoutForwardingBearer(t *testing.T) {
	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		if r.Header.Get("Authorization") != "" {
			t.Errorf("redirect leaked Authorization: %q", r.Header.Get("Authorization"))
		}
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/capture", http.StatusFound)
	}))
	defer redirect.Close()
	if _, err := testClient(redirect).ListProjects(context.Background()); !errors.Is(err, ErrRedirect) {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if targetRequests != 0 {
		t.Fatalf("redirect target was contacted %d times", targetRequests)
	}
}

func TestClientErrorsDoNotExposeTokenOrResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("secret-canary synthetic-token"))
	}))
	defer server.Close()
	client := testClient(server)
	client.token = "synthetic-token"
	_, err := client.ListProjects(context.Background())
	if err == nil || strings.Contains(err.Error(), "synthetic-token") || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("error exposed request/response secrets: %v", err)
	}
}

func TestListProjectsStopsAtConfiguredPaginationLimit(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		rows := make([]map[string]any, pageSize)
		for i := range rows {
			rows[i] = map[string]any{"id": fmt.Sprintf("project-%05d", offset+i), "name": "Fixture", "organization_id": "org_1", "region": "us-east-1", "status": "ACTIVE"}
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer server.Close()
	projects, err := testClient(server).ListProjects(context.Background())
	if !errors.Is(err, ErrPaginationLimit) || projects != nil || requests != maxProjects/pageSize {
		t.Fatalf("bounded list = %d projects, err=%v requests=%d", len(projects), err, requests)
	}
}

func TestClientBoundsResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
	}))
	defer server.Close()
	if _, err := testClient(server).ListProjects(context.Background()); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("ListProjects() error = %v", err)
	}
}

func TestRetryAfterParsingIsBounded(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"7", 7 * time.Second},
		{"-1", 0},
		{"invalid", 0},
		{now.Add(2 * time.Second).Format(http.TimeFormat), 2 * time.Second},
		{"7200", maxRetryAfter},
	} {
		if got := parseRetryAfter(tc.value, now); got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %s, want %s", tc.value, got, tc.want)
		}
	}
}

func TestClientTimeoutAndMapsAPIStatuses(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()
		client := testClient(server)
		client.http.Timeout = 20 * time.Millisecond
		if _, err := client.ListProjects(context.Background()); !errors.Is(err, ErrRequest) {
			t.Fatalf("ListProjects() error = %v", err)
		}
	})
	for _, tc := range []struct {
		status int
		want   error
		retry  string
	}{
		{http.StatusUnauthorized, ErrUnauthorized, ""},
		{http.StatusForbidden, ErrForbidden, ""},
		{http.StatusNotFound, ErrNotFound, ""},
		{http.StatusTooManyRequests, ErrRateLimited, "7"},
		{http.StatusServiceUnavailable, ErrServer, ""},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.retry != "" {
				w.Header().Set("Retry-After", tc.retry)
			}
			w.WriteHeader(tc.status)
		}))
		client := testClient(server)
		_, err := client.ListProjects(context.Background())
		server.Close()
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d error = %v, want %v", tc.status, err, tc.want)
		}
		if tc.status == http.StatusTooManyRequests {
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.RetryAfter != 7*time.Second {
				t.Errorf("429 API error = %#v", apiErr)
			}
		}
	}
}

func testClient(server *httptest.Server) *Client {
	client, err := NewClient("synthetic-token")
	if err != nil {
		panic(err)
	}
	client.baseURL = server.URL + "/v1"
	client.http = server.Client()
	client.http.CheckRedirect = refuseRedirect
	return client
}
