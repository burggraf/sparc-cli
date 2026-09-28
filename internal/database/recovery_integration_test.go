//go:build integration

package database

import (
	"bytes"
	"context"
	"io"
	"os"
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

func TestBundledPostgreSQLClientRequiresVerifiedTLS(t *testing.T) {
	fixture := newPostgresFixture(t)
	rootCert, err := os.ReadFile(fixture.caPath)
	if err != nil {
		t.Fatal("unable to read local fixture CA")
	}
	wrongRootCert, err := os.ReadFile(fixture.wrongCAPath)
	if err != nil {
		t.Fatal("unable to read wrong local fixture CA")
	}
	connection := func(host string, ca []byte) *tools.PGConnection {
		return &tools.PGConnection{
			Host: host, Port: fixture.port, User: "postgres", Database: "postgres",
			Password: []byte(fixturePassword), RootCertPEM: ca,
		}
	}
	run := func(host string, ca []byte) error {
		_, err := tools.RunCandidate(context.Background(), tools.RunRequest{
			Tool: tools.PGDump, Mode: tools.ModeDump, Connection: connection(host, ca),
			Timeout: 30 * time.Second, CleanupTimeout: time.Second, StdoutLimit: 1 << 20,
			StderrLimit: 1 << 20, Stdout: fixtureOutputSink{},
		})
		return err
	}
	if err := run(fixture.params.Host, wrongRootCert); err == nil {
		t.Fatal("bundled pg_dump accepted an untrusted fixture CA")
	}
	if err := run("wrong.example.test", rootCert); err == nil {
		t.Fatal("bundled pg_dump accepted a hostname mismatch")
	}
	if err := run(fixture.params.Host, rootCert); err != nil {
		t.Fatalf("bundled pg_dump rejected the trusted fixture: %v", err)
	}
}

type fixtureOutputSink struct{}

func (fixtureOutputSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return len(data), nil
}
func (fixtureOutputSink) CloseContext(context.Context) error { return nil }

type fixtureCaptureSink struct{ bytes.Buffer }

func (s *fixtureCaptureSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.Write(data)
}
func (*fixtureCaptureSink) CloseContext(context.Context) error { return nil }

func TestSelectedAuthDataRestoreRequiresExistingUser(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("bundled recovery proof targets macOS arm64")
	}
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"CREATE DATABASE sparc_auth_source", "CREATE DATABASE sparc_auth_target"} {
		if _, err := admin.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	_ = admin.Close(ctx)
	params := fixture.params
	params.User, params.Database = "postgres", "sparc_auth_source"
	source, err := pgx.ConnectConfig(ctx, fixture.configForParams(t, fixture.caPath, params))
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"CREATE SCHEMA auth", "CREATE TABLE auth.users (id int PRIMARY KEY)", "CREATE TABLE auth.sessions (id int PRIMARY KEY, user_id int REFERENCES auth.users(id))", "INSERT INTO auth.users VALUES (42)", "INSERT INTO auth.sessions VALUES (7,42)"} {
		if _, err := source.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	_ = source.Close(ctx)
	params.Database = "sparc_auth_target"
	target, err := pgx.ConnectConfig(ctx, fixture.configForParams(t, fixture.caPath, params))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(ctx)
	for _, sql := range []string{"CREATE SCHEMA auth", "CREATE TABLE auth.users (id int PRIMARY KEY)", "CREATE TABLE auth.sessions (id int PRIMARY KEY, user_id int REFERENCES auth.users(id))", "INSERT INTO auth.users VALUES (42)"} {
		if _, err := target.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.ReadFile(fixture.caPath)
	if err != nil {
		t.Fatal(err)
	}
	connection := &tools.PGConnection{Host: fixture.params.Host, Port: fixture.port, User: "postgres", Password: []byte(fixturePassword), RootCertPEM: root}
	var dump fixtureCaptureSink
	connection.Database = "sparc_auth_source"
	if _, err := tools.RunCandidate(ctx, tools.RunRequest{Tool: tools.PGDump, Mode: tools.ModeDump, Connection: connection, Timeout: time.Minute, CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: &dump}); err != nil {
		t.Fatalf("synthetic dump: %v", err)
	}
	connection.Database = "sparc_auth_target"
	var diagnostic bytes.Buffer
	_, err = tools.RunCandidate(ctx, tools.RunRequest{Tool: tools.PGRestore, Mode: tools.ModeRestoreAuthData, RestoreTable: "sessions", Connection: connection, Input: io.NopCloser(bytes.NewReader(dump.Bytes())), InputLimit: uint64(dump.Len()), Timeout: time.Minute, CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: fixtureOutputSink{}, Stderr: &diagnostic})
	if err != nil {
		t.Fatalf("selected Auth data restore failed: %v (diagnostic bytes: %d)", err, diagnostic.Len())
	}
	var count int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM auth.sessions WHERE user_id=42").Scan(&count); err != nil || count != 1 {
		t.Fatalf("selected Auth session rows=%d err=%v", count, err)
	}
}

func TestContinueRestoreCanPartiallyWriteAfterSchemaConflict(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("bundled recovery proof targets macOS arm64")
	}
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"CREATE DATABASE sparc_trial_source", "CREATE DATABASE sparc_trial_target"} {
		if _, err := admin.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	_ = admin.Close(ctx)
	params := fixture.params
	params.User, params.Database = "postgres", "sparc_trial_source"
	source, err := pgx.ConnectConfig(ctx, fixture.configForParams(t, fixture.caPath, params))
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"CREATE SCHEMA trial", "CREATE TABLE trial.items (id integer PRIMARY KEY)", "INSERT INTO trial.items VALUES (42)"} {
		if _, err := source.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	_ = source.Close(ctx)
	params.Database = "sparc_trial_target"
	target, err := pgx.ConnectConfig(ctx, fixture.configForParams(t, fixture.caPath, params))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(ctx, "CREATE SCHEMA trial"); err != nil {
		t.Fatal(err)
	}
	defer target.Close(ctx)
	root, err := os.ReadFile(fixture.caPath)
	if err != nil {
		t.Fatal(err)
	}
	connection := &tools.PGConnection{Host: fixture.params.Host, Port: fixture.port, User: "postgres", Password: []byte(fixturePassword), RootCertPEM: root}
	var dump fixtureCaptureSink
	connection.Database = "sparc_trial_source"
	if _, err := tools.RunCandidate(ctx, tools.RunRequest{Tool: tools.PGDump, Mode: tools.ModeDump, Connection: connection, Timeout: time.Minute, CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: &dump}); err != nil {
		t.Fatalf("synthetic dump: %v", err)
	}
	connection.Database = "sparc_trial_target"
	var diagnostic bytes.Buffer
	_, err = tools.RunCandidate(ctx, tools.RunRequest{Tool: tools.PGRestore, Mode: tools.ModeRestoreContinue, Connection: connection, Input: io.NopCloser(bytes.NewReader(dump.Bytes())), InputLimit: uint64(dump.Len()), Timeout: time.Minute, CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: fixtureOutputSink{}, Stderr: &diagnostic})
	if err == nil || !strings.Contains(diagnostic.String(), "already exists") {
		t.Fatalf("continued restore error=%v diagnostic=%q", err, diagnostic.String())
	}
	var count int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM trial.items WHERE id=42").Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial target rows=%d err=%v", count, err)
	}
}

func TestSupabaseManagedBaselineSplitRestoreIsAtomic(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("bundled recovery proof targets macOS arm64")
	}
	fixture := newPostgresFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, fixture.configForUser(t, fixture.caPath, "postgres"))
	if err != nil {
		t.Fatal("unable to connect to local fixture administrator")
	}
	for _, name := range []string{"sparc_profile_source", "sparc_profile_target", "sparc_truncated_target"} {
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			t.Fatal("unable to create local recovery database")
		}
	}
	if err := admin.Close(ctx); err != nil {
		t.Fatal("unable to close local fixture administrator")
	}
	connectDB := func(name string) *pgx.Conn {
		t.Helper()
		params := fixture.params
		params.User, params.Database = "postgres", name
		db, err := pgx.ConnectConfig(ctx, fixture.configForParams(t, fixture.caPath, params))
		if err != nil {
			t.Fatal("unable to connect to local recovery database")
		}
		return db
	}

	source := connectDB("sparc_profile_source")
	for _, statement := range []string{
		"CREATE SCHEMA auth",
		"CREATE TABLE auth.users (id integer PRIMARY KEY, email text NOT NULL)",
		"CREATE TABLE auth.sessions (id integer PRIMARY KEY, user_id integer NOT NULL REFERENCES auth.users(id))",
		"CREATE TABLE auth.schema_migrations (version text PRIMARY KEY)",
		"CREATE SCHEMA sparc_app",
		"CREATE TABLE sparc_app.items (id integer PRIMARY KEY, value text NOT NULL)",
		"CREATE TABLE sparc_app.children (id integer PRIMARY KEY, item_id integer NOT NULL REFERENCES sparc_app.items(id))",
		"CREATE SCHEMA storage",
		"CREATE TABLE storage.objects (id integer PRIMARY KEY, object_name text NOT NULL)",
		"CREATE TABLE storage.migrations (version text PRIMARY KEY)",
		"INSERT INTO auth.users VALUES (17, 'synthetic-user')",
		"INSERT INTO auth.sessions VALUES (23, 17)",
		"INSERT INTO auth.schema_migrations VALUES ('source-auth-history')",
		"INSERT INTO sparc_app.items VALUES (31, 'synthetic-item')",
		"INSERT INTO sparc_app.children VALUES (32, 31)",
		"INSERT INTO storage.objects VALUES (41, 'source-object')",
		"INSERT INTO storage.migrations VALUES ('source-storage-history')",
	} {
		if _, err := source.Exec(ctx, statement); err != nil {
			t.Fatal("unable to seed synthetic source database")
		}
	}
	if err := source.Close(ctx); err != nil {
		t.Fatal("unable to close synthetic source database")
	}

	seedBaseline := func(db *pgx.Conn, withConflict bool) {
		t.Helper()
		statements := []string{
			"CREATE SCHEMA auth",
			"CREATE TABLE auth.users (id integer PRIMARY KEY, email text NOT NULL)",
			"CREATE TABLE auth.sessions (id integer PRIMARY KEY, user_id integer NOT NULL REFERENCES auth.users(id))",
			"CREATE TABLE auth.schema_migrations (version text PRIMARY KEY)",
			"CREATE TABLE auth.trigger_log (session_id integer NOT NULL)",
			"CREATE FUNCTION auth.record_session_insert() RETURNS trigger LANGUAGE plpgsql AS $body$ BEGIN INSERT INTO auth.trigger_log VALUES (NEW.id); RETURN NEW; END; $body$",
			"CREATE TRIGGER session_insert_audit AFTER INSERT ON auth.sessions FOR EACH ROW EXECUTE FUNCTION auth.record_session_insert()",
			"INSERT INTO auth.schema_migrations VALUES ('target-auth-history')",
			"CREATE SCHEMA storage",
			"CREATE TABLE storage.objects (id integer PRIMARY KEY, object_name text NOT NULL)",
			"CREATE TABLE storage.migrations (version text PRIMARY KEY)",
			"INSERT INTO storage.migrations VALUES ('target-storage-history')",
		}
		if withConflict {
			statements = append(statements, "INSERT INTO storage.objects VALUES (41, 'target-conflict')")
		}
		for _, statement := range statements {
			if _, err := db.Exec(ctx, statement); err != nil {
				t.Fatal("unable to seed Supabase-shaped managed baseline")
			}
		}
	}
	target := connectDB("sparc_profile_target")
	seedBaseline(target, true)
	defer target.Close(ctx)
	truncated := connectDB("sparc_truncated_target")
	seedBaseline(truncated, false)
	defer truncated.Close(ctx)

	rootCert, err := os.ReadFile(fixture.caPath)
	if err != nil {
		t.Fatal("unable to read local fixture CA")
	}
	connection := func(database string) *tools.PGConnection {
		return &tools.PGConnection{Host: fixture.params.Host, Port: fixture.port, User: "postgres", Database: database, Password: []byte(fixturePassword), RootCertPEM: rootCert}
	}
	capture := func(mode tools.RunMode, schemas []string, excluded []tools.TableRef) *fixtureCaptureSink {
		t.Helper()
		sink := &fixtureCaptureSink{}
		_, err := tools.RunCandidate(ctx, tools.RunRequest{
			Tool: tools.PGDump, Mode: mode, Connection: connection("sparc_profile_source"), DumpSchemas: schemas, ExcludedTables: excluded,
			Timeout: time.Minute, CleanupTimeout: time.Second, StdoutLimit: 1 << 30, StderrLimit: 1 << 20, Stdout: sink,
		})
		if err != nil {
			t.Fatal("local typed PostgreSQL capture failed")
		}
		return sink
	}
	full := capture(tools.ModeDump, nil, nil)
	var fullDiagnostic bytes.Buffer
	_, err = tools.RunCandidate(ctx, tools.RunRequest{
		Tool: tools.PGRestore, Mode: tools.ModeRestore, Connection: connection("sparc_profile_target"),
		Input: io.NopCloser(bytes.NewReader(full.Bytes())), InputLimit: uint64(full.Len()), Timeout: time.Minute,
		CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: fixtureOutputSink{}, Stderr: &fullDiagnostic,
	})
	if err == nil || !strings.Contains(fullDiagnostic.String(), `schema "auth" already exists`) {
		t.Fatal("legacy full-dump restore did not fail on the pre-existing managed Auth schema")
	}
	var appSchema bool
	var users, sessions, triggerEvents, migrationRows int
	if err := target.QueryRow(ctx, "SELECT to_regnamespace('sparc_app') IS NOT NULL").Scan(&appSchema); err != nil || appSchema {
		t.Fatal("failed full-dump restore changed application schema state")
	}
	if err := target.QueryRow(ctx, "SELECT (SELECT count(*) FROM auth.users), (SELECT count(*) FROM auth.sessions), (SELECT count(*) FROM auth.trigger_log), (SELECT count(*) FROM auth.schema_migrations)").Scan(&users, &sessions, &triggerEvents, &migrationRows); err != nil || users != 0 || sessions != 0 || triggerEvents != 0 || migrationRows != 1 {
		t.Fatal("failed full-dump restore changed managed baseline rows")
	}

	schemaDump := capture(tools.ModeDumpSchema, []string{"sparc_app"}, nil)
	dataDump := capture(tools.ModeDumpData, []string{"auth", "sparc_app", "storage"}, []tools.TableRef{
		{Schema: "auth", Name: "schema_migrations"}, {Schema: "storage", Name: "migrations"},
		{Schema: "supabase_functions", Name: "migrations"}, {Schema: "storage", Name: "buckets_vectors"}, {Schema: "storage", Name: "vector_indexes"},
	})
	for _, excluded := range []string{"CREATE SCHEMA auth", "CREATE SCHEMA storage"} {
		if bytes.Contains(schemaDump.Bytes(), []byte(excluded)) {
			t.Fatal("schema-only component includes managed schema DDL")
		}
	}
	for _, excluded := range []string{"source-auth-history", "source-storage-history"} {
		if bytes.Contains(dataDump.Bytes(), []byte(excluded)) {
			t.Fatal("data-only component includes managed migration data")
		}
	}
	positions := []int{strings.Index(dataDump.String(), "COPY auth.users"), strings.Index(dataDump.String(), "COPY sparc_app.items"), strings.Index(dataDump.String(), "COPY storage.objects")}
	if positions[0] < 0 || positions[0] >= positions[1] || positions[1] >= positions[2] {
		t.Fatal("synthetic data fixture does not place its conflicting Storage row after application and Auth rows")
	}
	apply := func(db string, schema io.ReadCloser, data io.ReadCloser, schemaBytes, dataBytes uint64) error {
		return restoreSchemaDataWith(ctx, fixtureCandidateRunner(fixture), connection(db), schema, data, schemaBytes, dataBytes)
	}
	if err := apply("sparc_profile_target", io.NopCloser(bytes.NewReader(schemaDump.Bytes())), io.NopCloser(bytes.NewReader(dataDump.Bytes())), uint64(schemaDump.Len()), uint64(dataDump.Len())); err == nil {
		t.Fatal("late Storage uniqueness error unexpectedly committed the split restore")
	}
	var conflictRows int
	if err := target.QueryRow(ctx, "SELECT to_regnamespace('sparc_app') IS NOT NULL, (SELECT count(*) FROM auth.users), (SELECT count(*) FROM auth.sessions), (SELECT count(*) FROM auth.trigger_log), (SELECT count(*) FROM auth.schema_migrations), (SELECT count(*) FROM storage.objects WHERE id=41 AND object_name='target-conflict')").Scan(&appSchema, &users, &sessions, &triggerEvents, &migrationRows, &conflictRows); err != nil || appSchema || users != 0 || sessions != 0 || triggerEvents != 0 || migrationRows != 1 || conflictRows != 1 {
		t.Fatal("late data error did not roll back schema, Auth, and earlier rows")
	}
	if _, err := target.Exec(ctx, "DELETE FROM storage.objects WHERE id=41"); err != nil {
		t.Fatal("unable to remove synthetic target conflict")
	}
	if err := apply("sparc_profile_target", io.NopCloser(bytes.NewReader(schemaDump.Bytes())), io.NopCloser(bytes.NewReader(dataDump.Bytes())), uint64(schemaDump.Len()), uint64(dataDump.Len())); err != nil {
		t.Fatalf("atomic split restore failed against managed baseline: %v", err)
	}
	var item, child, session, object, authHistory, storageHistory string
	if err := target.QueryRow(ctx, "SELECT value FROM sparc_app.items WHERE id=31").Scan(&item); err != nil {
		t.Fatal("application row missing after split restore")
	}
	if err := target.QueryRow(ctx, "SELECT item_id::text FROM sparc_app.children WHERE id=32").Scan(&child); err != nil {
		t.Fatal("application FK-dependent row missing after split restore")
	}
	if err := target.QueryRow(ctx, "SELECT users.email || ':' || sessions.user_id::text FROM auth.users AS users JOIN auth.sessions AS sessions ON sessions.user_id=users.id WHERE sessions.id=23").Scan(&session); err != nil {
		t.Fatal("Auth FK-dependent rows missing after split restore")
	}
	if err := target.QueryRow(ctx, "SELECT object_name FROM storage.objects WHERE id=41").Scan(&object); err != nil {
		t.Fatal("eligible Storage metadata missing after split restore")
	}
	if err := target.QueryRow(ctx, "SELECT version FROM auth.schema_migrations").Scan(&authHistory); err != nil {
		t.Fatal("target Auth migration history missing")
	}
	if err := target.QueryRow(ctx, "SELECT version FROM storage.migrations").Scan(&storageHistory); err != nil {
		t.Fatal("target Storage migration history missing")
	}
	if item != "synthetic-item" || child != "31" || session != "synthetic-user:17" || object != "source-object" || authHistory != "target-auth-history" || storageHistory != "target-storage-history" {
		t.Fatal("split restore rows or managed migration history differed from expected fixture values")
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM auth.trigger_log").Scan(&triggerEvents); err != nil || triggerEvents != 0 {
		t.Fatal("split restore fired a target-side Auth trigger")
	}
	var replicationRole string
	if err := target.QueryRow(ctx, "SELECT current_setting('session_replication_role')").Scan(&replicationRole); err != nil || replicationRole != "origin" {
		t.Fatal("replication-role override escaped the restore transaction")
	}
	if _, err := target.Exec(ctx, "INSERT INTO auth.sessions VALUES (24, 17)"); err != nil {
		t.Fatal("target Auth trigger did not resume after restore")
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM auth.trigger_log").Scan(&triggerEvents); err != nil || triggerEvents != 1 {
		t.Fatal("target Auth trigger did not fire after the scoped restore")
	}
	for _, statement := range []string{"DELETE FROM auth.sessions WHERE id=24", "DELETE FROM auth.trigger_log"} {
		if _, err := target.Exec(ctx, statement); err != nil {
			t.Fatal("unable to reset synthetic Auth trigger check")
		}
	}

	failingData := failingSQLReader{Reader: bytes.NewReader(dataDump.Bytes())}
	if err := apply("sparc_truncated_target", io.NopCloser(bytes.NewReader(schemaDump.Bytes())), failingData, uint64(schemaDump.Len()), uint64(dataDump.Len())); err == nil {
		t.Fatal("truncated data component unexpectedly committed")
	}
	if err := truncated.QueryRow(ctx, "SELECT to_regnamespace('sparc_app') IS NOT NULL, (SELECT count(*) FROM auth.users), (SELECT count(*) FROM auth.sessions)").Scan(&appSchema, &users, &sessions); err != nil || appSchema || users != 0 || sessions != 0 {
		t.Fatal("truncated SQL stream committed partial database changes")
	}
	if _, err := truncated.Exec(ctx, "CREATE ROLE sparc_restore_restricted LOGIN PASSWORD '"+fixturePassword+"'"); err != nil {
		t.Fatal("unable to create restricted local restore role")
	}
	if _, err := truncated.Exec(ctx, "GRANT CREATE ON DATABASE sparc_truncated_target TO sparc_restore_restricted"); err != nil {
		t.Fatal("unable to grant restricted local schema creation")
	}
	restrictedConnection := connection("sparc_truncated_target")
	restrictedConnection.User = "sparc_restore_restricted"
	restrictedParams := fixture.params
	restrictedParams.User, restrictedParams.Database = restrictedConnection.User, restrictedConnection.Database
	restrictedDB, err := pgx.ConnectConfig(ctx, fixture.configForParams(t, fixture.caPath, restrictedParams))
	if err != nil {
		t.Fatal("restricted restore role cannot authenticate to the local target")
	}
	if err := restrictedDB.Close(ctx); err != nil {
		t.Fatal("unable to close restricted-role preflight")
	}
	if err := restoreSchemaDataWith(ctx, fixtureCandidateRunner(fixture), restrictedConnection,
		io.NopCloser(strings.NewReader("CREATE SCHEMA sparc_role_check;\n")), io.NopCloser(strings.NewReader("SELECT 1;\n")),
		uint64(len("CREATE SCHEMA sparc_role_check;\n")), uint64(len("SELECT 1;\n"))); err == nil {
		t.Fatal("restore continued without permission to set the scoped replication role")
	}
	if err := truncated.QueryRow(ctx, "SELECT to_regnamespace('sparc_role_check') IS NOT NULL").Scan(&appSchema); err != nil || appSchema {
		t.Fatal("unsupported replication-role setting left schema changes behind")
	}
	t.Log("PASS: PG17 managed baseline rejects legacy full DDL; typed schema/data restore preserves migrations and trigger state, and rolls back late SQL/stream failures")
}

type failingSQLReader struct{ io.Reader }

func (r failingSQLReader) Read(buffer []byte) (int, error) {
	n, err := r.Reader.Read(buffer)
	if err == io.EOF {
		return 0, io.ErrUnexpectedEOF
	}
	return n, err
}
func (failingSQLReader) Close() error { return nil }

func TestEncryptedCrossClusterRecovery(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("bundled recovery proof targets macOS arm64")
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
	sourceOps.run = fixtureCandidateRunner(source)
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
	targetOps.run = fixtureCandidateRunner(target)
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

func fixtureCandidateRunner(fixture *postgresFixture) func(context.Context, tools.RunRequest) (tools.RunResult, error) {
	return func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
		if request.Connection != nil {
			request.Connection.Port = fixture.port
		}
		return tools.RunCandidate(ctx, request)
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
