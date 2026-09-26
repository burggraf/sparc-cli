package database

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	directPort      = 5432
	transactionPort = 6543
	maxIdentifier   = 63
	maxCAPath       = 4096
)

type RouteKind string

const (
	RouteUnknown           RouteKind = "unknown"
	RouteDirect            RouteKind = "direct"
	RouteSessionPooler     RouteKind = "session_pooler"
	RouteTransactionPooler RouteKind = "transaction_pooler"
)

var ErrConnectionParameters = errors.New("invalid database connection parameters")
var ErrUnsupportedRoute = errors.New("database route is not qualified")
var ErrAmbientConfiguration = errors.New("ambient database configuration refused")

const pgxApplicationName = "sparc-cli"

// ConnectionParams contains only explicit, non-secret connection settings.
type ConnectionParams struct {
	ExpectedProjectRef string
	Host               string
	Port               uint16
	Database           string
	User               string
	SSLMode            string
	SSLRootCert        string
}

// ClassifyRoute identifies documented Supabase endpoint shapes. For a shared
// session pooler, ConnectionParams.Validate also binds the username to the
// expected project ref; this does not independently attest the backend.
func ClassifyRoute(projectRef, host string, port uint16) RouteKind {
	if !validProjectRefLabel(projectRef) || !validDNSName(host) {
		return RouteUnknown
	}
	if host == "db."+projectRef+".supabase.co" {
		return RouteDirect
	}
	if !isPoolerHost(host) {
		return RouteUnknown
	}
	switch port {
	case directPort:
		return RouteSessionPooler
	case transactionPort:
		return RouteTransactionPooler
	default:
		return RouteUnknown
	}
}

// Validate checks the offline input contract. It does not read the CA file or
// establish TLS; connection execution remains a separate qualification gate.
func (p ConnectionParams) Validate() error {
	if !validProjectRefLabel(p.ExpectedProjectRef) || !validDNSName(p.Host) ||
		!validIdentifier(p.Database) || p.SSLMode != "verify-full" || !validRootCert(p.SSLRootCert) {
		return ErrConnectionParameters
	}
	switch ClassifyRoute(p.ExpectedProjectRef, p.Host, p.Port) {
	case RouteDirect:
		if p.Port != directPort || !validIdentifier(p.User) {
			return ErrConnectionParameters
		}
		return nil
	case RouteSessionPooler:
		if p.Port != directPort || p.User != "postgres."+p.ExpectedProjectRef {
			return ErrConnectionParameters
		}
		return nil
	case RouteTransactionPooler:
		return ErrUnsupportedRoute
	default:
		return ErrConnectionParameters
	}
}

// NewConnConfig builds a pgx config without accepting process-level PostgreSQL
// settings, service files, passfiles, client certificates, or hidden TLS defaults.
func NewConnConfig(params ConnectionParams, password []byte) (*pgx.ConnConfig, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	if credentials.ValidatePGPassEntry(credentials.PGPassEntry{
		Host: params.Host, Port: params.Port, Database: params.Database, User: params.User, Password: password,
	}) != nil {
		return nil, ErrConnectionParameters
	}
	if hasAmbientDatabaseConfiguration() {
		return nil, ErrAmbientConfiguration
	}

	query := url.Values{}
	query.Set("sslmode", params.SSLMode)
	rootCert := params.SSLRootCert
	if rootCert == "supabase" {
		// pgx needs a recognized root source during parsing; the system pool is
		// replaced with the embedded Supabase pool before this config is returned.
		rootCert = "system"
	}
	query.Set("sslrootcert", rootCert)
	query.Set("sslcert", "")
	query.Set("sslkey", "")
	query.Set("passfile", "")
	query.Set("servicefile", "")
	query.Set("target_session_attrs", "any")
	query.Set("application_name", pgxApplicationName)

	uri := url.URL{
		Scheme:   "postgres",
		User:     url.User(params.User),
		Host:     net.JoinHostPort(params.Host, strconv.Itoa(int(params.Port))),
		Path:     "/" + params.Database,
		RawPath:  "/" + url.PathEscape(params.Database),
		RawQuery: query.Encode(),
	}
	config, err := pgx.ParseConfigWithOptions(uri.String(), pgx.ParseConfigOptions{
		ParseConfigOptions: pgconn.ParseConfigOptions{ConnStringAllowedKeys: []string{
			"host", "port", "user", "database", "sslmode", "sslrootcert", "sslcert", "sslkey",
			"passfile", "servicefile", "target_session_attrs", "application_name",
		}},
	})
	if err != nil {
		return nil, ErrConnectionParameters
	}
	if hasAmbientDatabaseConfiguration() {
		return nil, ErrAmbientConfiguration
	}
	if config.TLSConfig == nil {
		return nil, ErrConnectionParameters
	}
	if params.SSLRootCert == "supabase" {
		roots, ok := supabaseRootCAPool()
		if !ok {
			return nil, ErrConnectionParameters
		}
		config.TLSConfig.RootCAs = roots
	}
	if config.Password != "" || config.Host != params.Host || config.Port != params.Port ||
		config.Database != params.Database || config.User != params.User || config.TLSConfig == nil ||
		config.TLSConfig.ServerName != params.Host || config.TLSConfig.InsecureSkipVerify ||
		config.TLSConfig.RootCAs == nil || config.TLSConfig.VerifyPeerCertificate != nil ||
		len(config.Fallbacks) != 0 || len(config.RuntimeParams) != 1 ||
		config.RuntimeParams["application_name"] != pgxApplicationName || config.ValidateConnect != nil {
		return nil, ErrConnectionParameters
	}
	config.Password = string(password)
	return config, nil
}

func hasAmbientDatabaseConfiguration() bool {
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		name = strings.ToUpper(name)
		if strings.HasPrefix(name, "PG") || name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR" {
			return true
		}
	}
	return false
}

func validProjectRefLabel(ref string) bool {
	return validDNSLabel(ref)
}

func validDNSName(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !validDNSLabel(label) {
			return false
		}
	}
	return true
}

func validDNSLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || !isASCIIAlphaNumeric(label[0]) || !isASCIIAlphaNumeric(label[len(label)-1]) {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if !isASCIIAlphaNumeric(c) && c != '-' {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

func isPoolerHost(host string) bool {
	const suffix = ".pooler.supabase.com"
	label, ok := strings.CutSuffix(host, suffix)
	if !ok || strings.Contains(label, ".") || !strings.HasPrefix(label, "aws-") {
		return false
	}
	index, region, ok := strings.Cut(strings.TrimPrefix(label, "aws-"), "-")
	if !ok || index == "" || region == "" {
		return false
	}
	for _, digit := range index {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func validIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= maxIdentifier && validText(value)
}

func validText(value string) bool {
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

func validRootCert(value string) bool {
	if value == "system" || value == "supabase" {
		return true
	}
	return len(value) > 0 && len(value) <= maxCAPath && validText(value) && filepath.IsAbs(value) && filepath.Clean(value) == value
}
