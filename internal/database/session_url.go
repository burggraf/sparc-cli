package database

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/burggraf/sparc-cli/internal/credentials"
)

// ParseSupavisorSessionURL accepts only a password-bearing session-pooler URL
// for expectedProjectRef. It returns an explicit verify-full connection and
// never accepts a direct or transaction-pooler route.
func ParseSupavisorSessionURL(raw []byte, expectedProjectRef string) (ConnectionParams, []byte, error) {
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
