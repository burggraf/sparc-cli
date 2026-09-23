//go:build integration || localdemo

// Package localdemo contains a developer-only recovery rehearsal. It is not
// linked into the normal sparc executable and only targets its own loopback
// PostgreSQL cluster.
package localdemo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/destination"
	"github.com/burggraf/sparc-cli/internal/platform"
	"github.com/burggraf/sparc-cli/internal/verify"
)

var (
	ErrTools        = errors.New("SPARC_TEST_PG_BIN must name compatible PostgreSQL 17 tools")
	ErrArchivePath  = errors.New("archive destination must be new and have a private parent")
	ErrLocalStorage = errors.New("private local rehearsal storage unavailable")
	ErrCluster      = errors.New("disposable local PostgreSQL cluster failed")
	ErrClusterStart = errors.New("disposable local PostgreSQL server failed to start")
	ErrDatabase     = errors.New("synthetic local database setup failed")
	ErrDump         = errors.New("local synthetic database dump failed")
	ErrArchive      = errors.New("local encrypted archive verification failed")
	ErrRestore      = errors.New("local synthetic database restore verification failed")
	ErrCleanup      = errors.New("local PostgreSQL cleanup failed; temporary files were retained")
)

const commandTimeout = 30 * time.Second

type pgTools struct {
	initdb, pgctl, postgres, psql, pgdump, pgrestore string
}

// Run creates an isolated PostgreSQL 17 loopback cluster, backs up only its
// synthetic demo schema, deletes the source database, and restores to its fresh
// target. The archive is retained at archivePath; the cluster is removed.
func Run(ctx context.Context, binDir, archivePath string, passphrase []byte) (retErr error) {
	if ctx == nil || len(passphrase) == 0 {
		return ErrLocalStorage
	}
	tools, err := resolveTools(binDir)
	if err != nil {
		return err
	}
	if err := validateArchivePath(archivePath); err != nil {
		return err
	}

	tempRoot, err := os.MkdirTemp("", "sparc-localdemo-")
	if err != nil {
		return ErrLocalStorage
	}
	root, err := filepath.EvalSymlinks(tempRoot)
	if err != nil || platform.CheckPrivateDir(root) != nil {
		_ = os.RemoveAll(tempRoot)
		return ErrLocalStorage
	}
	var env []string
	var dataDir string
	serverMayRun := false
	defer func() {
		retErr = cleanupCluster(retErr, tools, env, dataDir, tempRoot, serverMayRun)
	}()

	home := filepath.Join(root, "home")
	appData := filepath.Join(root, "appdata")
	if platform.CreatePrivateDir(home) != nil || platform.CreatePrivateDir(appData) != nil {
		return ErrLocalStorage
	}
	passfile := filepath.Join(home, ".pgpass")
	if platform.WritePrivateFile(passfile, nil) != nil {
		return ErrLocalStorage
	}
	env, err = sanitizedEnv(binDir, root, home, appData, passfile)
	if err != nil {
		return ErrLocalStorage
	}
	if err := checkVersions(ctx, tools, env); err != nil {
		return err
	}

	port, err := reserveLoopbackPort()
	if err != nil {
		return ErrCluster
	}
	dataDir = filepath.Join(root, "data")
	logPath := filepath.Join(root, "postgres.log")
	// ponytail: --no-sync is safe only for this synthetic temporary cluster; remove it for persistent-cluster tests.
	if runCommand(ctx, env, tools.initdb, nil, io.Discard, "-D", dataDir, "-U", "postgres", "--auth-local=trust", "--auth-host=trust", "--no-sync") != nil {
		return ErrCluster
	}
	if configureCluster(dataDir, port) != nil {
		return ErrCluster
	}
	serverMayRun = true
	if runCommand(ctx, env, tools.pgctl, nil, io.Discard, "-D", dataDir, "-l", logPath, "-w", "start") != nil {
		return ErrClusterStart
	}

	portString := strconv.Itoa(port)
	if runSQL(ctx, env, tools.psql, connectionInfo(portString, "postgres"), "CREATE DATABASE sparc_demo_source;\nCREATE DATABASE sparc_demo_target;\n") != nil {
		return ErrDatabase
	}
	if runSQL(ctx, env, tools.psql, connectionInfo(portString, "sparc_demo_source"), "CREATE SCHEMA demo;\nCREATE TABLE demo.items (id bigserial PRIMARY KEY, label text NOT NULL);\nINSERT INTO demo.items (label) VALUES ('synthetic-first'), ('雪 synthetic-second');\n") != nil {
		return ErrDatabase
	}
	schemaAbsent, err := targetSchemaAbsent(ctx, env, tools.psql, portString, "sparc_demo_target")
	if err != nil || !schemaAbsent {
		return ErrDatabase
	}

	manifest, err := dumpToArchive(ctx, env, tools.pgdump, portString, archivePath, passphrase)
	if err != nil || len(manifest.Components) != 1 {
		return ErrDump
	}
	report, err := verify.Offline(archivePath, string(passphrase))
	if err != nil || !report.IntegrityPassed || report.CaptureStatus != "incomplete" || report.ComponentCount != 1 {
		return ErrArchive
	}
	if runSQL(ctx, env, tools.psql, connectionInfo(portString, "postgres"), "DROP DATABASE sparc_demo_source;\n") != nil {
		return ErrRestore
	}
	if exists, err := databaseExists(ctx, env, tools.psql, portString); err != nil || exists {
		return ErrRestore
	}
	if restoreFromArchive(ctx, env, tools.pgrestore, portString, archivePath, manifest.Components[0].ID, passphrase) != nil {
		return ErrRestore
	}
	if !checkRestoredData(ctx, env, tools.psql, portString) {
		return ErrRestore
	}
	return nil
}

func resolveTools(binDir string) (pgTools, error) {
	if !filepath.IsAbs(binDir) || filepath.Clean(binDir) != binDir {
		return pgTools{}, ErrTools
	}
	paths := pgTools{
		initdb:    binaryPath(binDir, "initdb"),
		pgctl:     binaryPath(binDir, "pg_ctl"),
		postgres:  binaryPath(binDir, "postgres"),
		psql:      binaryPath(binDir, "psql"),
		pgdump:    binaryPath(binDir, "pg_dump"),
		pgrestore: binaryPath(binDir, "pg_restore"),
	}
	for _, path := range []string{paths.initdb, paths.pgctl, paths.postgres, paths.psql, paths.pgdump, paths.pgrestore} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
			return pgTools{}, ErrTools
		}
	}
	return paths, nil
}

func binaryPath(binDir, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(binDir, name)
}

func validateArchivePath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || platform.CheckPrivateDir(filepath.Dir(path)) != nil {
		return ErrArchivePath
	}
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return ErrArchivePath
	}
	return nil
}

func sanitizedEnv(binDir, root, home, appData, passfile string) ([]string, error) {
	path := binDir
	var systemRoot string
	if runtime.GOOS == "windows" {
		systemRoot = os.Getenv("SystemRoot")
		if systemRoot == "" {
			return nil, ErrLocalStorage
		}
		path += string(os.PathListSeparator) + filepath.Join(systemRoot, "System32")
	}
	env := []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"APPDATA=" + appData,
		"LOCALAPPDATA=" + appData,
		"TMPDIR=" + root,
		"TEMP=" + root,
		"TMP=" + root,
		"PATH=" + path,
		"LC_ALL=C",
		"LANG=C",
		"PGPASSFILE=" + passfile,
	}
	if runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+systemRoot, "WINDIR="+systemRoot)
	}
	return env, nil
}

func checkVersions(ctx context.Context, tools pgTools, env []string) error {
	for _, path := range []string{tools.initdb, tools.pgctl, tools.postgres, tools.psql, tools.pgdump, tools.pgrestore} {
		var output boundedBuffer
		if runCommand(ctx, env, path, nil, &output, "--version") != nil || output.overflow || postgresMajor(output.String()) != 17 {
			return ErrTools
		}
	}
	return nil
}

func postgresMajor(output string) int {
	for _, field := range strings.Fields(output) {
		field = strings.Trim(field, "(),")
		if field == "" || field[0] < '0' || field[0] > '9' {
			continue
		}
		major, _, _ := strings.Cut(field, ".")
		version, err := strconv.Atoi(major)
		if err == nil {
			return version
		}
	}
	return 0
}

type boundedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	const limit = 1024
	remaining := limit - b.Len()
	if remaining > 0 {
		keep := min(remaining, len(p))
		_, _ = b.Buffer.Write(p[:keep])
	}
	if len(p) > remaining {
		b.overflow = true
	}
	return len(p), nil
}

func configureCluster(dataDir string, port int) error {
	// ponytail: trust auth/TLS-off only for this private synthetic loopback cluster; external DBs use the qualified production profile.
	hba := "local all all trust\nhost all all 127.0.0.1/32 trust\nhost all all ::1/128 reject\n"
	if err := os.WriteFile(filepath.Join(dataDir, "pg_hba.conf"), []byte(hba), 0600); err != nil {
		return ErrCluster
	}
	configPath := filepath.Join(dataDir, "postgresql.conf")
	config, err := os.ReadFile(configPath)
	if err != nil {
		return ErrCluster
	}
	config = append(config, []byte(fmt.Sprintf("\nport = %d\nlisten_addresses = '127.0.0.1'\nunix_socket_directories = ''\nssl = off\n", port))...)
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		return ErrCluster
	}
	return nil
}

func reserveLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func connectionInfo(port, database string) string {
	return fmt.Sprintf("host=127.0.0.1 port=%s user=postgres dbname=%s sslmode=disable connect_timeout=5", port, database)
}

func runSQL(ctx context.Context, env []string, psql, connInfo, sql string) error {
	return runCommand(ctx, env, psql, strings.NewReader(sql), io.Discard, "-X", "--no-password", "--set=ON_ERROR_STOP=1", "--dbname", connInfo)
}

func querySQL(ctx context.Context, env []string, psql, connInfo, sql string) (string, error) {
	var output boundedBuffer
	args := []string{"-X", "--no-password", "--set=ON_ERROR_STOP=1", "--tuples-only", "--no-align", "--quiet", "--dbname", connInfo, "--command", sql}
	if runCommand(ctx, env, psql, nil, &output, args...) != nil || output.overflow {
		return "", ErrCluster
	}
	return strings.TrimSpace(output.String()), nil
}

func runCommand(ctx context.Context, env []string, executable string, stdin io.Reader, stdout io.Writer, args ...string) error {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, executable, args...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}

func dumpToArchive(ctx context.Context, env []string, pgdump, port, archivePath string, passphrase []byte) (archive.Manifest, error) {
	reader, writer := io.Pipe()
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, pgdump, "--format=custom", "--schema=demo", "--no-password", "--dbname", connectionInfo(port, "sparc_demo_source"))
	cmd.Env = env
	cmd.Stdout = writer
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	commandDone := make(chan error, 1)
	go func() {
		err := cmd.Run()
		if err != nil {
			_ = writer.CloseWithError(err)
		} else {
			_ = writer.Close()
		}
		commandDone <- err
	}()
	manifest, archiveErr := destination.Create(archivePath, []archive.Input{{Key: "database/demo.dump", Scope: "developer-only local PG17 demo schema", Status: "incomplete", Source: reader}}, string(passphrase))
	_ = reader.Close()
	commandErr := <-commandDone
	if archiveErr != nil || commandErr != nil {
		return archive.Manifest{}, ErrDump
	}
	return manifest, nil
}

func targetSchemaAbsent(ctx context.Context, env []string, psql, port, database string) (bool, error) {
	result, err := querySQL(ctx, env, psql, connectionInfo(port, database), "SELECT to_regclass('demo.items') IS NULL")
	if err != nil || result != "t" && result != "f" {
		return false, ErrDatabase
	}
	return result == "t", nil
}

func databaseExists(ctx context.Context, env []string, psql, port string) (bool, error) {
	result, err := querySQL(ctx, env, psql, connectionInfo(port, "postgres"), "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'sparc_demo_source')")
	if err != nil || result != "t" && result != "f" {
		return false, ErrCluster
	}
	return result == "t", nil
}

func restoreFromArchive(ctx context.Context, env []string, pgrestore, port, archivePath, componentID string, passphrase []byte) error {
	ciphertext, err := platform.OpenPrivatePayloadFile(filepath.Join(archivePath, componentID+".age"), false)
	if err != nil {
		return ErrArchive
	}
	decrypted, err := archive.Decrypt(ciphertext, string(passphrase))
	if err != nil {
		_ = ciphertext.Close()
		return ErrArchive
	}
	err = runCommand(ctx, env, pgrestore, decrypted, io.Discard, "--single-transaction", "--exit-on-error", "--no-password", "--dbname", connectionInfo(port, "sparc_demo_target"))
	closeErr := ciphertext.Close()
	if err != nil || closeErr != nil {
		return ErrRestore
	}
	return nil
}

func checkRestoredData(ctx context.Context, env []string, psql, port string) bool {
	connInfo := connectionInfo(port, "sparc_demo_target")
	labels, labelsErr := querySQL(ctx, env, psql, connInfo, "SELECT string_agg(label, ',' ORDER BY id) FROM demo.items")
	nextID, sequenceErr := querySQL(ctx, env, psql, connInfo, "SELECT nextval('demo.items_id_seq')")
	return labelsErr == nil && sequenceErr == nil && labels == "synthetic-first,雪 synthetic-second" && nextID == "3"
}

func cleanupCluster(prior error, tools pgTools, env []string, dataDir, tempRoot string, stop bool) error {
	if stop && dataDir != "" {
		if _, err := os.Stat(filepath.Join(dataDir, "postmaster.pid")); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			stopErr := runCommand(ctx, env, tools.pgctl, nil, io.Discard, "-D", dataDir, "-m", "fast", "-w", "stop")
			cancel()
			if stopErr != nil {
				return errors.Join(prior, ErrCleanup)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.Join(prior, ErrCleanup)
		}
	}
	if err := os.RemoveAll(tempRoot); err != nil {
		return errors.Join(prior, ErrCleanup)
	}
	return prior
}
