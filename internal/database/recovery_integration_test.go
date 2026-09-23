//go:build integration

package database

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/destination"
	"github.com/burggraf/sparc-cli/internal/platform"
	"github.com/burggraf/sparc-cli/internal/verify"
	"github.com/jackc/pgx/v5"
)

// TestLocalRecoveryRehearsal is an opt-in, local-only proof; it is not a hosted
// Supabase or production restore recipe.
func TestLocalRecoveryRehearsal(t *testing.T) {
	fixture := newPostgresFixture(t)
	rehearseLocalRecovery(t, fixture)
}

func rehearseLocalRecovery(t *testing.T, fixture *postgresFixture) {
	t.Helper()
	if _, err := observeCatalog(context.Background(), fixture.config(t, fixture.caPath), nil); err != nil {
		t.Fatalf("local PostgreSQL 17/TLS check failed: %v", err)
	}
	for _, name := range []string{"pg_dump", "pg_restore"} {
		if info, err := os.Stat(fixtureBinaryPath(fixture.binDir, name)); err != nil || info.IsDir() {
			t.Fatalf("local PostgreSQL fixture requires %s", name)
		}
	}
	// The fixture poisons implicit libpq files. Native test tools use an empty
	// home and explicit verified TLS; no credentials enter argv or the archive.
	cleanHome := filepath.Join(t.TempDir(), "pg-home")
	if err := os.Mkdir(cleanHome, 0700); err != nil {
		t.Fatal("unable to create test-only PostgreSQL home")
	}
	t.Setenv("HOME", cleanHome)
	t.Setenv("USERPROFILE", cleanHome)
	t.Setenv("APPDATA", cleanHome)
	connInfo := func(database string) string {
		return fmt.Sprintf("host=%s hostaddr=127.0.0.1 port=%d user=postgres dbname=%s sslmode=verify-full sslrootcert='%s'", fixture.params.Host, fixture.port, database, fixture.caPath)
	}
	psql := func(database, sql string) {
		t.Helper()
		runFixtureCommandWithInput(t, []byte(sql), fixture.binDir, "psql", "-X", "-v", "ON_ERROR_STOP=1", connInfo(database))
	}
	psql("postgres", "CREATE DATABASE sparc_rehearsal_source;\nCREATE DATABASE sparc_rehearsal_target;\n")
	psql("sparc_rehearsal_source", "CREATE SCHEMA demo;\nCREATE TABLE demo.items (id bigserial PRIMARY KEY, label text NOT NULL);\nINSERT INTO demo.items (label) VALUES ('synthetic-first'), ('雪 synthetic-second');\n")

	// ponytail: private plaintext staging is test-only; production capture must
	// stream through the qualified client runner without leaving a dump behind.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("unable to resolve local fixture staging root")
	}
	privateDir := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(privateDir); err != nil {
		t.Fatal("unable to create local fixture staging directory")
	}
	dumpPath := filepath.Join(privateDir, "fixture.dump")
	dump, err := platform.CreatePrivateFile(dumpPath)
	if err != nil {
		t.Fatal("unable to stage local test dump privately")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, fixtureBinaryPath(fixture.binDir, "pg_dump"), "--format=custom", "--schema=demo", "--dbname", connInfo("sparc_rehearsal_source"))
	cmd.Env = os.Environ()
	cmd.Stdout = dump
	if err := cmd.Run(); err != nil {
		_ = dump.Close()
		t.Fatal("local PostgreSQL dump failed")
	}
	if err := dump.Close(); err != nil {
		t.Fatal("local PostgreSQL dump close failed")
	}
	plain, err := platform.OpenPrivatePayloadFile(dumpPath, false)
	if err != nil {
		t.Fatal("local test dump cannot be opened")
	}
	archiveDir := filepath.Join(privateDir, "encrypted-backup")
	const passphrase = "synthetic-rehearsal-passphrase"
	manifest, createErr := destination.Create(archiveDir, []archive.Input{{Key: "database/demo.dump", Scope: "local PG17 fixture only", Status: "incomplete", Source: plain}}, passphrase)
	closeErr := plain.Close()
	removeErr := os.Remove(dumpPath)
	if createErr != nil || closeErr != nil || removeErr != nil || len(manifest.Components) != 1 {
		t.Fatal("local encrypted archive creation or plaintext cleanup failed")
	}
	report, err := verify.Offline(archiveDir, passphrase)
	if err != nil || !report.IntegrityPassed || report.CaptureStatus != "incomplete" || report.ComponentCount != 1 {
		t.Fatal("local encrypted archive did not verify as intentionally incomplete")
	}
	if _, err := verify.Offline(archiveDir, "incorrect synthetic passphrase"); err == nil {
		t.Fatal("local encrypted archive accepted the wrong passphrase")
	}
	targetConfig := fixture.configForUser(t, fixture.caPath, "postgres")
	targetConfig.Database = "sparc_rehearsal_target"
	target, err := pgx.ConnectConfig(ctx, targetConfig)
	if err != nil {
		t.Fatal("local target database cannot be opened")
	}
	var targetEmpty bool
	if err := target.QueryRow(ctx, "SELECT to_regclass('demo.items') IS NULL").Scan(&targetEmpty); err != nil || !targetEmpty {
		_ = target.Close(ctx)
		t.Fatal("local target already contains rehearsal data")
	}
	if err := target.Close(ctx); err != nil {
		t.Fatal("local target connection did not close")
	}

	// Prove that recovery does not read the original database.
	psql("postgres", "DROP DATABASE sparc_rehearsal_source;\n")
	sourceConfig := fixture.configForUser(t, fixture.caPath, "postgres")
	sourceConfig.Database = "sparc_rehearsal_source"
	if source, err := pgx.ConnectConfig(ctx, sourceConfig); err == nil {
		_ = source.Close(ctx)
		t.Fatal("local source database remained accessible after deletion")
	}

	ciphertext, err := platform.OpenPrivatePayloadFile(filepath.Join(archiveDir, manifest.Components[0].ID+".age"), false)
	if err != nil {
		t.Fatal("verified encrypted test payload cannot be opened")
	}
	decrypted, err := archive.Decrypt(ciphertext, passphrase)
	if err != nil {
		_ = ciphertext.Close()
		t.Fatal("verified encrypted test payload cannot be decrypted")
	}
	cmd = exec.CommandContext(ctx, fixtureBinaryPath(fixture.binDir, "pg_restore"), "--single-transaction", "--exit-on-error", "--dbname", connInfo("sparc_rehearsal_target"))
	cmd.Env = os.Environ()
	cmd.Stdin = decrypted
	runErr := cmd.Run()
	closeErr = ciphertext.Close()
	if runErr != nil || closeErr != nil {
		t.Fatal("local PostgreSQL restore failed")
	}
	target, err = pgx.ConnectConfig(ctx, targetConfig)
	if err != nil {
		t.Fatal("local restored database cannot be opened")
	}
	defer target.Close(context.Background())
	var labels string
	if err := target.QueryRow(ctx, "SELECT string_agg(label, ',' ORDER BY id) FROM demo.items").Scan(&labels); err != nil || labels != "synthetic-first,雪 synthetic-second" {
		t.Fatalf("local restored rows differ: match=%t error=%v", labels == "synthetic-first,雪 synthetic-second", err)
	}
	var nextID int64
	if err := target.QueryRow(ctx, "SELECT nextval('demo.items_id_seq')").Scan(&nextID); err != nil || nextID != 3 {
		t.Fatalf("local restored sequence differs: next=%d error=%v", nextID, err)
	}
	t.Log("PASS: PostgreSQL 17 schema dump -> encrypted archive -> offline verification -> source deletion -> fresh target restore; intentionally incomplete project coverage")
}
