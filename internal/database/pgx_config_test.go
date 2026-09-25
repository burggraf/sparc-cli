package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewConnConfigBuildsExplicitVerifiedConfig(t *testing.T) {
	clearDatabaseEnvironment(t)
	t.Setenv("HOME", t.TempDir())
	params := validConnectionParams(t)
	params.SSLRootCert = "system"
	params.User = "role @name"
	params.Database = "db/name ?"
	password := []byte("p@ss:/\\? secret-canary")

	config, err := NewConnConfig(params, password)
	if err != nil {
		t.Fatalf("NewConnConfig() = %v", err)
	}
	if config.Host != params.Host || config.Port != params.Port || config.User != params.User || config.Database != params.Database {
		t.Fatalf("connection settings changed: host=%q port=%d user=%q database=%q", config.Host, config.Port, config.User, config.Database)
	}
	if config.Password != string(password) {
		t.Fatal("explicit password was not retained in memory")
	}
	if strings.Contains(config.ConnString(), "secret-canary") {
		t.Fatal("password appeared in the parsed connection string")
	}
	if config.TLSConfig == nil || config.TLSConfig.ServerName != params.Host || config.TLSConfig.InsecureSkipVerify || config.TLSConfig.RootCAs == nil {
		t.Fatal("config does not require trusted hostname verification")
	}
	if len(config.Fallbacks) != 0 {
		t.Fatalf("unexpected TLS fallback count: %d", len(config.Fallbacks))
	}
	if len(config.RuntimeParams) != 1 || config.RuntimeParams["application_name"] != "sparc-cli" {
		t.Fatalf("unexpected runtime parameters: %#v", config.RuntimeParams)
	}
}

func TestNewConnConfigPreservesVerifiedSessionPoolerRoute(t *testing.T) {
	clearDatabaseEnvironment(t)
	params := validConnectionParams(t)
	params.Host = "aws-1-us-east-2.pooler.supabase.com"
	params.User = "postgres." + testProjectRef
	params.SSLRootCert = "system"
	config, err := NewConnConfig(params, []byte("explicit-password"))
	if err != nil {
		t.Fatalf("NewConnConfig() = %v", err)
	}
	if config.Host != params.Host || config.Port != directPort || config.User != params.User ||
		config.TLSConfig == nil || config.TLSConfig.ServerName != params.Host || config.TLSConfig.InsecureSkipVerify ||
		config.TLSConfig.RootCAs == nil || len(config.Fallbacks) != 0 {
		t.Fatalf("session-pooler route or verified TLS changed: %#v", config)
	}
}

func TestNewConnConfigRejectsAmbientDatabaseAndTrustSettings(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"PGOPTIONS", "-c search_path=pg_catalog secret-canary"},
		{"PGSERVICE", "untrusted-service"},
		{"PGSERVICEFILE", "/tmp/secret-canary"},
		{"PGHOST", "attacker.example.test"},
		{"PGPASSWORD", "secret-canary"},
		{"PGSSLROOTCERT", "/tmp/secret-canary"},
		{"SSL_CERT_FILE", "/tmp/secret-canary"},
		{"SSL_CERT_DIR", "/tmp/secret-canary"},
	} {
		t.Run(test.key, func(t *testing.T) {
			clearDatabaseEnvironment(t)
			t.Setenv(test.key, test.value)
			params := validConnectionParams(t)
			params.SSLRootCert = "system"
			config, err := NewConnConfig(params, []byte("explicit-password"))
			if config != nil || err != ErrAmbientConfiguration || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("NewConnConfig() = %#v, %v", config, err)
			}
		})
	}
}

func TestNewConnConfigRejectsUnreadableCAWithoutEcho(t *testing.T) {
	clearDatabaseEnvironment(t)
	params := validConnectionParams(t)
	params.SSLRootCert = filepath.Join(t.TempDir(), "secret-canary-root.pem")
	if config, err := NewConnConfig(params, []byte("explicit-password")); config != nil || err != ErrConnectionParameters || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("NewConnConfig() leaked trust path or accepted missing CA: %v", err)
	}
}

func TestNewConnConfigRejectsTransactionPooler(t *testing.T) {
	clearDatabaseEnvironment(t)
	params := validConnectionParams(t)
	params.Host = "aws-0-us-east-1.pooler.supabase.com"
	params.Port = transactionPort
	params.User = "postgres." + testProjectRef
	params.SSLRootCert = "system"
	if config, err := NewConnConfig(params, []byte("explicit-password")); config != nil || err != ErrUnsupportedRoute {
		t.Fatalf("NewConnConfig() = %#v, %v; want unsupported-route refusal", config, err)
	}
}

func TestNewConnConfigRejectsInvalidCredentialsWithoutEcho(t *testing.T) {
	for _, test := range []struct {
		name     string
		password []byte
	}{
		{"missing", nil},
		{"empty", []byte{}},
		{"line break", []byte("line\nbreak")},
		{"NUL", []byte("secret-canary\x00")},
		{"oversized", []byte(strings.Repeat("x", 16*1024+1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearDatabaseEnvironment(t)
			params := validConnectionParams(t)
			params.SSLRootCert = "system"
			config, err := NewConnConfig(params, test.password)
			if config != nil || err != ErrConnectionParameters || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("NewConnConfig() = %#v, %v", config, err)
			}
		})
	}
}

func clearDatabaseEnvironment(t *testing.T) {
	t.Helper()
	for _, pair := range os.Environ() {
		name, _, ok := strings.Cut(pair, "=")
		if !ok || !isDatabaseEnvironment(name) {
			continue
		}
		value, wasSet := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal("unable to isolate test environment")
		}
		t.Cleanup(func() {
			if wasSet {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}

func isDatabaseEnvironment(name string) bool {
	name = strings.ToUpper(name)
	return strings.HasPrefix(name, "PG") || name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR"
}
