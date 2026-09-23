//go:build integration && !windows

package database

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const fixturePassword = "synthetic-local-password"

type postgresFixture struct {
	binDir      string
	dataDir     string
	caPath      string
	wrongCAPath string
	port        uint16
	params      ConnectionParams
}

func newPostgresFixture(t *testing.T) *postgresFixture {
	t.Helper()
	binDir := os.Getenv("SPARC_TEST_PG_BIN")
	if binDir == "" {
		t.Skip("SPARC_TEST_PG_BIN must name an approved local PostgreSQL bin directory")
	}
	if !filepath.IsAbs(binDir) {
		t.Fatal("SPARC_TEST_PG_BIN must be an absolute path")
	}
	for _, name := range [...]string{"initdb", "pg_ctl", "postgres", "psql"} {
		if info, err := os.Stat(filepath.Join(binDir, name)); err != nil || info.IsDir() || info.Mode()&0111 == 0 {
			t.Fatal("SPARC_TEST_PG_BIN does not contain the required PostgreSQL tools")
		}
	}

	clearDatabaseEnvironment(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal("unable to protect local fixture directory")
	}
	homeDir := filepath.Join(root, "home")
	if err := os.Mkdir(homeDir, 0700); err != nil {
		t.Fatal("unable to create local fixture home")
	}
	t.Setenv("HOME", homeDir)

	ref := testProjectRef
	host := "db." + ref + ".supabase.co"
	caPath, wrongCAPath, certPath, keyPath := writeFixtureCertificates(t, root, host)
	port := reserveFixturePort(t)
	dataDir := filepath.Join(root, "data")
	// PostgreSQL limits Unix socket paths to 103 bytes; Go's test directory can
	// exceed that on macOS, so keep only the empty socket directory short-lived.
	socketDir, err := os.MkdirTemp("/tmp", "sparc-pg-socket-")
	if err != nil {
		t.Fatal("unable to create local fixture socket directory")
	}
	if err := os.Chmod(socketDir, 0700); err != nil {
		t.Fatal("unable to protect local fixture socket directory")
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

	runFixtureCommand(t, binDir, "initdb", "-D", dataDir, "-U", "postgres", "--auth-local=trust", "--auth-host=scram-sha-256", "--no-sync")
	if err := os.WriteFile(filepath.Join(dataDir, "pg_hba.conf"), []byte("local all all trust\nhostnossl all all 127.0.0.1/32 reject\nhostssl all all 127.0.0.1/32 scram-sha-256\n"), 0600); err != nil {
		t.Fatal("unable to configure local fixture authentication")
	}

	options := strings.Join([]string{
		"-p", strconv.Itoa(int(port)), "-h", "127.0.0.1", "-k", socketDir,
		"-c", "ssl=on", "-c", "ssl_cert_file=" + certPath, "-c", "ssl_key_file=" + keyPath,
		"-c", "ssl_min_protocol_version=TLSv1.2",
	}, " ")
	logPath := filepath.Join(root, "postgres.log")
	startFixturePostgres(t, binDir, dataDir, logPath, options)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(binDir, "pg_ctl"), "-D", dataDir, "-m", "immediate", "-w", "stop")
		cmd.Env = os.Environ()
		_ = cmd.Run()
	})

	sql := "CREATE ROLE sparc_probe LOGIN PASSWORD '" + fixturePassword + "';\n" +
		"CREATE SCHEMA \"literal schema\";\n" +
		"CREATE SCHEMA \"literalXschema\";\n" +
		"CREATE SCHEMA \"quote\"\"schema\";\n" +
		"CREATE SCHEMA \"back\\slash\";\n" +
		"CREATE SCHEMA \"regex.[x]$\";\n" +
		"CREATE SCHEMA \"雪schema\";\n" +
		"CREATE SCHEMA \"" + strings.Repeat("a", maxIdentifier) + "\";\n" +
		"CREATE SCHEMA \"" + strings.Repeat("雪", maxIdentifier/len("雪")) + "\";\n" +
		"CREATE TABLE \"literal schema\".\"base table\" (id bigint PRIMARY KEY);\n" +
		"CREATE TABLE \"literalXschema\".\"base table\" (id bigint);\n" +
		"CREATE FUNCTION \"literal schema\".\"noop trigger\"() RETURNS trigger LANGUAGE plpgsql AS $body$ BEGIN RETURN NEW; END; $body$;\n" +
		"CREATE TRIGGER \"sample trigger\" BEFORE UPDATE ON \"literal schema\".\"base table\" FOR EACH ROW EXECUTE FUNCTION \"literal schema\".\"noop trigger\"();\n" +
		"CREATE INDEX \"sample index\" ON \"literal schema\".\"base table\" (id);\n" +
		"CREATE VIEW \"literal schema\".\"sample view\" AS SELECT id FROM \"literal schema\".\"base table\";\n" +
		"CREATE MATERIALIZED VIEW \"literal schema\".\"sample matview\" AS SELECT id FROM \"literal schema\".\"base table\";\n" +
		"CREATE SEQUENCE \"literal schema\".\"sample sequence\";\n" +
		"CREATE TABLE \"literal schema\".\"partitioned table\" (id bigint) PARTITION BY RANGE (id);\n" +
		"CREATE TABLE \"literal schema\".\"partitioned child\" PARTITION OF \"literal schema\".\"partitioned table\" FOR VALUES FROM (0) TO (10);\n" +
		"CREATE TABLE \"literal schema\".\"rls table\" (id bigint, owner_name text);\n" +
		"ALTER TABLE \"literal schema\".\"rls table\" ENABLE ROW LEVEL SECURITY;\n" +
		"ALTER TABLE \"literal schema\".\"rls table\" FORCE ROW LEVEL SECURITY;\n" +
		"CREATE POLICY \"owner policy\" ON \"literal schema\".\"rls table\" USING (owner_name = current_user) WITH CHECK (owner_name = current_user);\n"
	runFixtureCommandWithInput(t, []byte(sql), binDir, "psql", "-h", socketDir, "-p", strconv.Itoa(int(port)), "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1")

	return &postgresFixture{
		binDir:      binDir,
		dataDir:     dataDir,
		caPath:      caPath,
		wrongCAPath: wrongCAPath,
		port:        port,
		params: ConnectionParams{
			ExpectedProjectRef: ref,
			Host:               host,
			Port:               directPort,
			Database:           "postgres",
			User:               "sparc_probe",
			SSLMode:            "verify-full",
			SSLRootCert:        caPath,
		},
	}
}

func (f *postgresFixture) config(t *testing.T, caPath string) *pgx.ConnConfig {
	t.Helper()
	params := f.params
	params.SSLRootCert = caPath
	config, err := NewConnConfig(params, []byte(fixturePassword))
	if err != nil {
		t.Fatal("fixture connection configuration rejected")
	}
	// The production contract keeps port 5432. The disposable fixture uses an
	// ephemeral loopback port to avoid colliding with a developer's server.
	config.Port = f.port
	config.LookupFunc = func(_ context.Context, host string) ([]string, error) {
		if host != params.Host {
			return nil, errors.New("unexpected fixture DNS name")
		}
		return []string{"127.0.0.1"}, nil
	}
	config.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != net.JoinHostPort("127.0.0.1", strconv.Itoa(int(f.port))) {
			return nil, errors.New("unexpected fixture dial target")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	return config
}

func startFixturePostgres(t *testing.T, binDir, dataDir, logPath, options string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(binDir, "pg_ctl"), "-D", dataDir, "-l", logPath, "-o", options, "-w", "start")
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		log, _ := os.ReadFile(logPath)
		t.Fatalf("local PostgreSQL fixture command pg_ctl failed: %s", strings.TrimSpace(string(log)))
	}
}

func reserveFixturePort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("unable to reserve local fixture port")
	}
	defer listener.Close()
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}

func runFixtureCommand(t *testing.T, binDir, name string, args ...string) {
	t.Helper()
	runFixtureCommandWithInput(t, nil, binDir, name, args...)
}

func runFixtureCommandWithInput(t *testing.T, input []byte, binDir, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(binDir, name), args...)
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(string(input))
	if err := cmd.Run(); err != nil {
		t.Fatalf("local PostgreSQL fixture command %s failed", name)
	}
}

func writeFixtureCertificates(t *testing.T, dir, host string) (caPath, wrongCAPath, certPath, keyPath string) {
	t.Helper()
	caKey := newFixtureKey(t)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "SPARC local fixture CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caCertificateDER := newFixtureCertificate(t, caTemplate, nil, caKey, caKey)
	caCertificate, err := x509.ParseCertificate(caCertificateDER)
	if err != nil {
		t.Fatal("unable to parse local fixture CA")
	}
	caPath = filepath.Join(dir, "ca.pem")
	writeFixturePEM(t, caPath, "CERTIFICATE", caCertificateDER, 0600)

	wrongKey := newFixtureKey(t)
	wrongCA := newFixtureCertificate(t, &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "SPARC wrong fixture CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}, nil, wrongKey, wrongKey)
	wrongCAPath = filepath.Join(dir, "wrong-ca.pem")
	writeFixturePEM(t, wrongCAPath, "CERTIFICATE", wrongCA, 0600)

	serverKey := newFixtureKey(t)
	serverCertificate := newFixtureCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, caCertificate, caKey, serverKey)
	certPath = filepath.Join(dir, "server.crt")
	keyPath = filepath.Join(dir, "server.key")
	writeFixturePEM(t, certPath, "CERTIFICATE", serverCertificate, 0600)
	writeFixturePEM(t, keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(serverKey), 0600)
	return caPath, wrongCAPath, certPath, keyPath
}

func newFixtureKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("unable to generate local fixture key")
	}
	return key
}

func newFixtureCertificate(t *testing.T, template, parent *x509.Certificate, parentKey, key *rsa.PrivateKey) []byte {
	t.Helper()
	if parent == nil {
		parent = template
	}
	certificate, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal("unable to generate local fixture certificate")
	}
	return certificate
}

func writeFixturePEM(t *testing.T, path, blockType string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: data}), mode); err != nil {
		t.Fatal("unable to write local fixture certificate")
	}
}
