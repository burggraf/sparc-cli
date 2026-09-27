//go:build integration

package database

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/localdemo"
	"github.com/burggraf/sparc-cli/internal/platform"
	"github.com/burggraf/sparc-cli/internal/tools"
	"github.com/burggraf/sparc-cli/internal/verify"
)

// A schema-only dump does not carry cluster-wide roles. This counterexample
// uses two independent disposable clusters and keeps the target untouched on
// missing owner rather than silently discarding ownership with --no-owner.
func TestCrossClusterRestoreRequiresTargetOwner(t *testing.T) {
	source := newPostgresFixture(t)
	target := newPostgresFixture(t)
	clientBin := source.binDir
	if candidate := os.Getenv("SPARC_TEST_PG_CLIENT_BIN"); candidate != "" {
		if !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
			t.Fatal("SPARC_TEST_PG_CLIENT_BIN must be an absolute clean path")
		}
		clientBin = candidate
	}
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, source.configForUser(t, source.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local source fixture")
	}
	for _, sql := range []string{
		"CREATE ROLE sparc_recovery_owner NOLOGIN",
		"CREATE SCHEMA sparc_recovery AUTHORIZATION sparc_recovery_owner",
		"SET ROLE sparc_recovery_owner",
		"CREATE TABLE sparc_recovery.items (value text NOT NULL)",
		"INSERT INTO sparc_recovery.items VALUES ('synthetic-canary')",
		"RESET ROLE",
	} {
		if _, err := admin.Exec(ctx, sql); err != nil {
			t.Fatal("unable to seed local source fixture")
		}
	}
	if err := admin.Close(ctx); err != nil {
		t.Fatal("unable to close local source fixture connection")
	}

	// Plaintext is limited to this disposable test directory; this is a native
	// pg_dump/pg_restore recipe experiment, not a SPARC archive or hosted test.
	dump := filepath.Join(t.TempDir(), "synthetic.dump")
	// The fixture intentionally poisons libpq's default client certificates;
	// native tool probes need a fresh private home, not those negative controls.
	cleanHome := t.TempDir()
	t.Setenv("HOME", cleanHome)
	t.Setenv("USERPROFILE", cleanHome)
	t.Setenv("APPDATA", cleanHome)
	connection := func(f *postgresFixture) string {
		return fmt.Sprintf("host=%s hostaddr=127.0.0.1 port=%d user=postgres dbname=postgres sslmode=verify-full sslrootcert='%s'", f.params.Host, f.port, f.caPath)
	}
	// Both probes must fail during native TLS verification, before pg_dump can
	// access the synthetic source or publish a usable dump.
	for _, probe := range []struct{ host, ca, diagnostic string }{
		{source.params.Host, source.wrongCAPath, "certificate verify failed"},
		{"wrong.example.test", source.caPath, "does not match host name"},
	} {
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		connInfo := fmt.Sprintf("host=%s hostaddr=127.0.0.1 port=%d user=postgres dbname=postgres sslmode=verify-full sslrootcert='%s'", probe.host, source.port, probe.ca)
		cmd := exec.CommandContext(probeCtx, fixtureBinaryPath(clientBin, "pg_dump"), "--format=custom", "--schema=sparc_recovery", "--no-password", "--file", filepath.Join(t.TempDir(), "rejected.dump"), "--dbname", connInfo)
		cmd.Env = os.Environ()
		var diagnostic bytes.Buffer
		cmd.Stderr = &diagnostic
		err := cmd.Run()
		probeErr := probeCtx.Err()
		cancel()
		if err == nil || probeErr != nil || !strings.Contains(diagnostic.String(), probe.diagnostic) {
			t.Fatal("native pg_dump did not specifically reject bad TLS trust or hostname")
		}
	}
	runFixtureCommand(t, clientBin, "pg_dump", "--format=custom", "--schema=sparc_recovery", "--no-password", "--file", dump, "--dbname", connection(source))
	// The source is shut down before any target restore attempt.
	runFixtureCommand(t, source.binDir, "pg_ctl", "-D", source.dataDir, "-m", "immediate", "-w", "stop")

	restore := func() (error, string) {
		restoreCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(restoreCtx, fixtureBinaryPath(clientBin, "pg_restore"), "--single-transaction", "--exit-on-error", "--no-password", "--dbname", connection(target), dump)
		cmd.Env = os.Environ()
		var diagnostic bytes.Buffer
		cmd.Stderr = &diagnostic
		err := cmd.Run()
		if restoreCtx.Err() != nil {
			return restoreCtx.Err(), ""
		}
		return err, diagnostic.String()
	}
	if err, diagnostic := restore(); err == nil || !strings.Contains(diagnostic, `role "sparc_recovery_owner" does not exist`) {
		t.Fatal("restore did not specifically refuse the missing target owner role")
	}
	targetAdmin, err := pgx.ConnectConfig(ctx, target.configForUser(t, target.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local target fixture")
	}
	defer targetAdmin.Close(ctx)
	var schema, ownerPresent bool
	if err := targetAdmin.QueryRow(ctx, "SELECT to_regnamespace('sparc_recovery') IS NOT NULL, EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'sparc_recovery_owner')").Scan(&schema, &ownerPresent); err != nil || schema || ownerPresent {
		t.Fatal("failed restore changed the target schema or created the missing role")
	}
	// Explicit synthetic role provisioning demonstrates the narrow prerequisite;
	// it is not a policy for copying source roles into a hosted target.
	if _, err := targetAdmin.Exec(ctx, "CREATE ROLE sparc_recovery_owner NOLOGIN"); err != nil {
		t.Fatal("unable to provision the synthetic target role")
	}
	if err, _ := restore(); err != nil {
		t.Fatal("restore with an explicit target owner role failed")
	}
	var owner, value string
	if err := targetAdmin.QueryRow(ctx, `SELECT pg_catalog.pg_get_userbyid(relation.relowner)::text, item.value
		FROM sparc_recovery.items AS item
		JOIN pg_catalog.pg_class AS relation ON relation.oid = 'sparc_recovery.items'::pg_catalog.regclass`).Scan(&owner, &value); err != nil || owner != "sparc_recovery_owner" || value != "synthetic-canary" {
		t.Fatal("cross-cluster restore did not preserve the synthetic row and owner")
	}
}

func TestEncryptedCrossClusterRecovery(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("private candidate recovery proof requires the local macOS arm64 client")
	}
	clientBin := os.Getenv("SPARC_TEST_PG_CLIENT_BIN")
	if clientBin == "" {
		t.Skip("SPARC_TEST_PG_CLIENT_BIN must select the private PostgreSQL 17 candidate")
	}
	if !filepath.IsAbs(clientBin) || filepath.Clean(clientBin) != clientBin {
		t.Fatal("SPARC_TEST_PG_CLIENT_BIN must be an absolute clean path")
	}
	source, target := newPostgresFixture(t), newPostgresFixture(t)
	ctx := context.Background()

	admin, err := pgx.ConnectConfig(ctx, source.configForUser(t, source.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local source fixture")
	}
	for _, statement := range []string{
		"CREATE ROLE sparc_recovery_owner NOLOGIN",
		"CREATE DATABASE sparc_recovery_source",
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal("unable to create synthetic source role/database")
		}
	}
	if err := admin.Close(ctx); err != nil {
		t.Fatal("unable to close source administrator connection")
	}
	sourceParams := source.params
	sourceParams.Database, sourceParams.User = "sparc_recovery_source", "postgres"
	sourceDB, err := pgx.ConnectConfig(ctx, source.configForParams(t, source.caPath, sourceParams))
	if err != nil {
		t.Fatal("unable to connect to synthetic source database")
	}
	for _, statement := range []string{
		"CREATE SCHEMA sparc_recovery AUTHORIZATION sparc_recovery_owner",
		"SET ROLE sparc_recovery_owner",
		"CREATE TABLE sparc_recovery.items (id bigserial PRIMARY KEY, value text NOT NULL)",
		"INSERT INTO sparc_recovery.items (value) VALUES ('synthetic-canary')",
		"CREATE TABLE sparc_recovery.migration_history (version text NOT NULL)",
		"INSERT INTO sparc_recovery.migration_history VALUES ('SELECT pg_catalog.pg_sleep(99); -- inert history canary')",
		"RESET ROLE",
	} {
		if _, err := sourceDB.Exec(ctx, statement); err != nil {
			t.Fatal("unable to seed synthetic source database")
		}
	}
	if err := sourceDB.Close(ctx); err != nil {
		t.Fatal("unable to close source database connection")
	}

	targetAdmin, err := pgx.ConnectConfig(ctx, target.configForUser(t, target.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local target fixture")
	}
	if _, err := targetAdmin.Exec(ctx, "CREATE DATABASE sparc_recovery_target"); err != nil {
		t.Fatal("unable to create synthetic target database")
	}
	if err := targetAdmin.Close(ctx); err != nil {
		t.Fatal("unable to close target administrator connection")
	}
	targetParams := target.params
	targetParams.Database, targetParams.User = "sparc_recovery_target", "postgres"

	privateRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("unable to resolve encrypted archive parent")
	}
	archiveParent := filepath.Join(privateRoot, "private")
	if err := platform.CreatePrivateDir(archiveParent); err != nil {
		t.Fatal("unable to create private encrypted archive parent")
	}
	archivePath := filepath.Join(archiveParent, "recovery")
	const archivePassphrase = "synthetic-encrypted-recovery-passphrase"
	sourceOps := defaultRecoveryOps()
	sourceOps.observe = fixtureRecoveryObserver(t, source)
	sourceOps.run = fixtureCandidateRunner(clientBin, source)
	manifest, err := captureWith(ctx, CaptureRequest{
		Source: sourceParams, SourcePassword: []byte(fixturePassword), ArchivePath: archivePath, ArchivePassphrase: archivePassphrase,
	}, sourceOps)
	if err != nil || len(manifest.Components) != 1 || manifest.Components[0].Status != "incomplete" {
		t.Fatal("encrypted local source capture failed")
	}
	if _, err := archive.Verify(archivePath, archivePassphrase); err != nil {
		t.Fatal("encrypted archive failed offline verification after capture")
	}
	runFixtureCommand(t, source.binDir, "pg_ctl", "-D", source.dataDir, "-m", "immediate", "-w", "stop")

	targetOps := defaultRecoveryOps()
	targetOps.observe = fixtureRecoveryObserver(t, target)
	targetOps.run = fixtureCandidateRunner(clientBin, target)
	restoreRequest := RestoreRequest{
		Target: targetParams, TargetPassword: []byte(fixturePassword), ArchivePath: archivePath, ArchivePassphrase: archivePassphrase,
		EmptyScope: EmptyTargetScopeV1{RequiredPresent: []string{"public"}, RequiredAbsent: []string{"sparc_recovery"}},
	}
	if err := restoreWith(ctx, restoreRequest, targetOps); err == nil {
		t.Fatal("restore unexpectedly succeeded without the source owner role on target")
	}
	if _, err := archive.Verify(archivePath, archivePassphrase); err != nil {
		t.Fatal("failed restore changed archive integrity")
	}
	targetDB, err := pgx.ConnectConfig(ctx, target.configForParams(t, target.caPath, targetParams))
	if err != nil {
		t.Fatal("unable to inspect target after rolled-back restore")
	}
	var schemaExists, ownerExists bool
	if err := targetDB.QueryRow(ctx, `SELECT to_regnamespace('sparc_recovery') IS NOT NULL,
		EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'sparc_recovery_owner')`).Scan(&schemaExists, &ownerExists); err != nil || schemaExists || ownerExists {
		t.Fatal("failed restore did not roll back the missing-owner transaction")
	}
	if _, err := targetDB.Exec(ctx, "CREATE ROLE sparc_recovery_owner NOLOGIN"); err != nil {
		t.Fatal("unable to provision the explicit synthetic target owner")
	}
	if err := targetDB.Close(ctx); err != nil {
		t.Fatal("unable to close target database connection")
	}
	if err := restoreWith(ctx, restoreRequest, targetOps); err != nil {
		t.Fatal("encrypted restore into the explicitly prepared empty target failed")
	}
	if _, err := archive.Verify(archivePath, archivePassphrase); err != nil {
		t.Fatal("successful restore changed archive integrity")
	}
	targetDB, err = pgx.ConnectConfig(ctx, target.configForParams(t, target.caPath, targetParams))
	if err != nil {
		t.Fatal("unable to verify restored target")
	}
	defer targetDB.Close(ctx)
	var owner, value, history string
	var nextID int64
	if err := targetDB.QueryRow(ctx, `SELECT pg_catalog.pg_get_userbyid(relation.relowner)::text, item.value,
		(SELECT version FROM sparc_recovery.migration_history LIMIT 1)
		FROM sparc_recovery.items AS item
		JOIN pg_catalog.pg_class AS relation ON relation.oid = 'sparc_recovery.items'::pg_catalog.regclass`).Scan(&owner, &value, &history); err != nil {
		t.Fatal("unable to verify restored synthetic objects")
	}
	if err := targetDB.QueryRow(ctx, "SELECT nextval('sparc_recovery.items_id_seq')").Scan(&nextID); err != nil || owner != "sparc_recovery_owner" || value != "synthetic-canary" || history != "SELECT pg_catalog.pg_sleep(99); -- inert history canary" || nextID != 2 {
		t.Fatal("restored row, owner, sequence, or inert migration-history text differed")
	}
	t.Log("PASS: local PG17 source -> encrypted archive -> source shutdown -> empty-target preflight -> owner-failure rollback -> explicit-role restore; globals and provider-managed services remain excluded")
}

func fixtureRecoveryObserver(t *testing.T, fixture *postgresFixture) func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
	t.Helper()
	return func(ctx context.Context, params ConnectionParams, password []byte, schemas []string) (CatalogObservation, error) {
		if string(password) != fixturePassword {
			return CatalogObservation{}, ErrConnectionParameters
		}
		config := fixture.configForParams(t, params.SSLRootCert, params)
		return observeCatalog(ctx, config, schemas)
	}
}

func fixtureCandidateRunner(clientBin string, fixture *postgresFixture) func(context.Context, tools.RunRequest) (tools.RunResult, error) {
	return func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
		if request.Connection != nil {
			request.Connection.Port = fixture.port
		}
		return tools.RunCandidate(ctx, clientBin, request)
	}
}

func TestLocalRecoveryRehearsal(t *testing.T) {
	binDir := os.Getenv("SPARC_TEST_PG_BIN")
	if binDir == "" {
		t.Skip("SPARC_TEST_PG_BIN must name an explicitly selected local PostgreSQL 17 bin directory")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("unable to resolve local rehearsal output root")
	}
	privateDir := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(privateDir); err != nil {
		t.Fatal("unable to create local rehearsal output directory")
	}
	archiveDir := filepath.Join(privateDir, "encrypted-backup")
	const passphrase = "synthetic-rehearsal-passphrase"
	if err := localdemo.Run(context.Background(), binDir, archiveDir, []byte(passphrase)); err != nil {
		t.Fatalf("local-only backup/restore rehearsal failed: %v", err)
	}
	report, err := verify.Offline(archiveDir, passphrase)
	if err != nil || !report.IntegrityPassed || report.CaptureStatus != "incomplete" || report.ComponentCount != 1 {
		t.Fatal("local encrypted archive did not verify as intentionally incomplete")
	}
	t.Log("PASS: local PostgreSQL 17 backup -> encrypted archive -> source deletion -> restore into a fresh local target; intentionally incomplete project coverage")
}
