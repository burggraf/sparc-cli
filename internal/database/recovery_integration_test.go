//go:build integration

package database

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/burggraf/sparc-cli/internal/localdemo"
	"github.com/burggraf/sparc-cli/internal/platform"
	"github.com/burggraf/sparc-cli/internal/verify"
)

// A schema-only dump does not carry cluster-wide roles. This counterexample
// uses two independent disposable clusters and keeps the target untouched on
// missing owner rather than silently discarding ownership with --no-owner.
func TestCrossClusterRestoreRequiresTargetOwner(t *testing.T) {
	source := newPostgresFixture(t)
	target := newPostgresFixture(t)
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
	runFixtureCommand(t, source.binDir, "pg_dump", "--format=custom", "--schema=sparc_recovery", "--no-password", "--file", dump, "--dbname", connection(source))
	// The source is shut down before any target restore attempt.
	runFixtureCommand(t, source.binDir, "pg_ctl", "-D", source.dataDir, "-m", "immediate", "-w", "stop")

	restore := func() (error, string) {
		restoreCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(restoreCtx, fixtureBinaryPath(target.binDir, "pg_restore"), "--single-transaction", "--exit-on-error", "--no-password", "--dbname", connection(target), dump)
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
