//go:build localdemo

package database

import (
	"context"
	"io"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/tools"
	"github.com/jackc/pgx/v5"
)

// RestoreAuthDataTableTrial is a developer-only, single-table, data-only
// transaction on a disposable target. It refuses to append to a nonempty table.
func RestoreAuthDataTableTrial(ctx context.Context, request RestoreRequest, table string, localOutput io.Writer) (stage, category string, rows int64, err error) {
	stage = "input validation"
	if ctx == nil || !selectedAuthTable(table) || !validRecoveryConnection(request.Target, request.TargetPassword) || !validRecoveryArchivePath(request.ArchivePath) || archive.ValidatePassphrase(request.ArchivePassphrase) != nil {
		return stage, "", 0, ErrRestore
	}
	stage = "archive verification"
	manifest, e := archive.Verify(request.ArchivePath, request.ArchivePassphrase)
	if e != nil || !validDatabaseDumpManifest(manifest) {
		return stage, "", 0, ErrRestore
	}
	stage = "client preparation"
	if tools.PrepareProductionPayload(ctx, tools.PGRestore) != nil {
		return stage, "", 0, ErrRestore
	}
	stage = "target preflight"
	observation, e := ObserveCatalog(ctx, request.Target, request.TargetPassword, []string{"auth"})
	if e != nil || observation.ServerMajor != supportedPostgresMajor || !observation.ReadOnly || !observation.TLS || CheckTargetSecurityV1(observation) != nil {
		return stage, "", 0, ErrRestore
	}
	exists := false
	for _, relation := range observation.Relations {
		if relation.Schema == "auth" && relation.Name == table && relation.Kind == "r" {
			exists = true
		}
	}
	if !exists {
		return stage, "", 0, ErrRestore
	}
	before, e := countAuthTableRows(ctx, request, table)
	if e != nil || before != 0 {
		return stage, "", 0, ErrRestore
	}
	root, e := nativeRootCertPEM(request.Target)
	if e != nil || len(root) == 0 || len(root) > maxNativeRootBytes {
		return stage, "", 0, ErrRestore
	}
	stage = "archive decryption"
	input, e := openDatabaseDump(request.ArchivePath, manifest.Components[0], request.ArchivePassphrase)
	if e != nil {
		return stage, "", 0, ErrRestore
	}
	stage = "pg_restore"
	var diagnostic trialDiagnostic
	result, runErr := tools.Run(ctx, tools.RunRequest{
		Tool: tools.PGRestore, Mode: tools.ModeRestoreAuthData, RestoreTable: table,
		Connection: &tools.PGConnection{Host: request.Target.Host, Port: request.Target.Port, User: request.Target.User, Database: request.Target.Database, Password: request.TargetPassword, RootCertPEM: root},
		Input:      input, InputLimit: uint64(manifest.Components[0].Length),
		Timeout: 5 * time.Minute, CleanupTimeout: 5 * time.Second,
		StdoutLimit: maxToolStderrBytes, StderrLimit: maxToolStderrBytes,
		Stdout: discardToolOutput{}, Stderr: &diagnostic,
	})
	closeErr := input.Close()
	if runErr != nil || result.ExitCode != 0 || closeErr != nil {
		category = classifyTrialFailure(diagnostic.Bytes())
		if localOutput != nil {
			_, _ = localOutput.Write(diagnostic.Bytes())
		}
		clear(diagnostic.Bytes())
		return stage, category, 0, ErrRestore
	}
	clear(diagnostic.Bytes())
	stage = "target verification"
	rows, e = countAuthTableRows(ctx, request, table)
	if e != nil || rows == 0 {
		return stage, "", 0, ErrRestore
	}
	return stage, "", rows, nil
}

func selectedAuthTable(name string) bool {
	switch name {
	case "sessions", "identities", "refresh_tokens", "mfa_amr_claims", "one_time_tokens":
		return true
	default:
		return false
	}
}

func countAuthTableRows(ctx context.Context, request RestoreRequest, table string) (int64, error) {
	config, err := NewConnConfig(request.Target, request.TargetPassword)
	if err != nil {
		return 0, err
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return 0, err
	}
	defer conn.Close(context.Background())
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	var rows int64
	err = tx.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{"auth", table}.Sanitize()).Scan(&rows)
	return rows, err
}
