//go:build localdemo

package localdemo

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/platform"
)

// RehearseExistingArchive restores one verified database component into its own
// disposable PostgreSQL 17 cluster. No existing cluster or hosted source is used.
func RehearseExistingArchive(ctx context.Context, binDir, archivePath string, passphrase []byte) (retErr error) {
	if ctx == nil || len(passphrase) == 0 {
		return ErrArchive
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return ErrTools
	}
	manifest, err := archive.Verify(archivePath, string(passphrase))
	if err != nil || len(manifest.Components) != 1 || manifest.Components[0].ID != "00000000" ||
		manifest.Components[0].Key != "database/postgresql.dump" || manifest.Components[0].Status != "incomplete" || manifest.Components[0].Length <= 0 {
		return ErrArchive
	}
	tools, err := resolveTools(binDir)
	if err != nil {
		return err
	}
	// Keep the Unix socket path short; os.TempDir on macOS may exceed sun_path.
	root, err := os.MkdirTemp("/private/tmp", "sparc-restore-")
	if err != nil {
		return ErrLocalStorage
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil || platform.CheckPrivateDir(root) != nil {
		_ = os.RemoveAll(root)
		return ErrLocalStorage
	}
	var env []string
	var dataDir string
	serverMayRun := false
	defer func() { retErr = cleanupCluster(retErr, tools, env, dataDir, root, serverMayRun) }()

	home, appData := filepath.Join(root, "home"), filepath.Join(root, "appdata")
	if platform.CreatePrivateDir(home) != nil || platform.CreatePrivateDir(appData) != nil ||
		platform.WritePrivateFile(filepath.Join(home, ".pgpass"), nil) != nil {
		return ErrLocalStorage
	}
	env, err = sanitizedEnv(binDir, root, home, appData, filepath.Join(home, ".pgpass"))
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
	if runCommand(ctx, env, tools.initdb, nil, io.Discard, "-D", dataDir, "-U", "postgres", "--auth-local=trust", "--auth-host=reject", "--no-sync") != nil {
		return ErrCluster
	}
	// The rehearsal listens on no TCP interface. Its socket is inside the private root.
	if os.WriteFile(filepath.Join(dataDir, "pg_hba.conf"), []byte("local all all trust\nhost all all 127.0.0.1/32 reject\n"), 0600) != nil {
		return ErrCluster
	}
	configPath := filepath.Join(dataDir, "postgresql.conf")
	config, err := os.ReadFile(configPath)
	if err != nil {
		return ErrCluster
	}
	config = append(config, []byte(fmt.Sprintf("\nport = %d\nlisten_addresses = ''\nunix_socket_directories = '%s'\nssl = off\n", port, filepath.Join(root, "s")))...)
	if os.WriteFile(configPath, config, 0600) != nil || platform.CreatePrivateDir(filepath.Join(root, "s")) != nil {
		return ErrCluster
	}
	serverMayRun = true
	if runCommand(ctx, env, tools.pgctl, nil, io.Discard, "-D", dataDir, "-l", filepath.Join(root, "postgres.log"), "-w", "start") != nil {
		return ErrClusterStart
	}
	connInfo := fmt.Sprintf("host=%s port=%s user=postgres dbname=sparc_restore sslmode=disable connect_timeout=5", filepath.Join(root, "s"), strconv.Itoa(port))
	adminInfo := strings.Replace(connInfo, "dbname=sparc_restore", "dbname=postgres", 1)
	if runSQL(ctx, env, tools.psql, adminInfo, "CREATE DATABASE sparc_restore;\n") != nil {
		return ErrDatabase
	}
	ciphertext, err := platform.OpenPrivatePayloadFile(filepath.Join(archivePath, "00000000.age"), false)
	if err != nil {
		return ErrArchive
	}
	plaintext, err := archive.Decrypt(ciphertext, string(passphrase))
	if err != nil {
		_ = ciphertext.Close()
		return ErrArchive
	}
	// A PostgreSQL restore executes the archive's SQL as the disposable server's
	// user. Do not point this rehearsal at an untrusted archive.
	restoreErr := runCommandWithTimeout(ctx, 5*time.Minute, env, tools.pgrestore, plaintext, io.Discard,
		"--single-transaction", "--exit-on-error", "--no-password", "--dbname", connInfo)
	if ciphertext.Close() != nil || restoreErr != nil {
		return ErrRestore
	}
	if _, err := querySQL(ctx, env, tools.psql, connInfo,
		"SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema'"); err != nil {
		return ErrRestore
	}
	return nil
}
