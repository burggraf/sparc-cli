package database

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/platform"
	"github.com/burggraf/sparc-cli/internal/tools"
)

// RestoreRequest contains only the target, archive, and caller-declared empty scope.
type RestoreRequest struct {
	Target            ConnectionParams
	TargetPassword    []byte
	ArchivePath       string
	ArchivePassphrase string
	EmptyScope        EmptyTargetScopeV1
}

// Restore verifies the archive and target scope before streaming to pg_restore.
func Restore(ctx context.Context, request RestoreRequest) error {
	return restoreWith(ctx, request, defaultRecoveryOps())
}

func restoreWith(ctx context.Context, request RestoreRequest, ops recoveryOps) error {
	if ctx == nil || !validRecoveryConnection(request.Target, request.TargetPassword) || !validRecoveryArchivePath(request.ArchivePath) ||
		archive.ValidatePassphrase(request.ArchivePassphrase) != nil || !validEmptyTargetScope(request.EmptyScope) ||
		ops.verify == nil || ops.rootCert == nil || ops.observe == nil || ops.open == nil || ops.run == nil {
		return ErrRestore
	}
	manifest, err := ops.verify(request.ArchivePath, request.ArchivePassphrase)
	if err != nil || !validDatabaseDumpManifest(manifest) {
		return ErrRestore
	}
	component := manifest.Components[0]
	rootCert, err := ops.rootCert(request.Target)
	if err != nil || len(rootCert) == 0 || len(rootCert) > maxNativeRootBytes {
		return ErrRestore
	}
	schemas := append(append([]string(nil), request.EmptyScope.RequiredPresent...), request.EmptyScope.RequiredAbsent...)
	observation, err := ops.observe(ctx, request.Target, request.TargetPassword, schemas)
	if err != nil || CheckEmptyTargetV1(observation, request.EmptyScope) != nil {
		return ErrRestore
	}
	input, err := ops.open(request.ArchivePath, component, request.ArchivePassphrase)
	if err != nil || input == nil {
		return ErrRestore
	}
	result, runErr := ops.run(ctx, tools.RunRequest{
		Tool: tools.PGRestore, Mode: tools.ModeRestore,
		Connection: &tools.PGConnection{
			Host: request.Target.Host, Port: request.Target.Port, User: request.Target.User, Database: request.Target.Database,
			Password: request.TargetPassword, RootCertPEM: rootCert,
		},
		Input: input, InputLimit: uint64(component.Length),
		Timeout: databaseToolTimeout, CleanupTimeout: 5 * time.Second,
		StdoutLimit: maxToolStderrBytes, StderrLimit: maxToolStderrBytes, Stdout: discardToolOutput{},
	})
	closeErr := input.Close()
	if errors.Is(runErr, tools.ErrPayloadUnavailable) {
		return tools.ErrPayloadUnavailable
	}
	if runErr != nil || result.ExitCode != 0 || closeErr != nil {
		return ErrRestore
	}
	return nil
}

func openDatabaseDump(dir string, component archive.Component, passphrase string) (io.ReadCloser, error) {
	if component.ID != "00000000" || component.Length <= 0 || component.Length > archive.MaxComponentBytes {
		return nil, ErrRestore
	}
	path := filepath.Join(dir, component.ID+".age")
	ciphertext, err := platform.OpenPrivatePayloadFile(path, false)
	if err != nil {
		return nil, ErrRestore
	}
	info, err := ciphertext.Stat()
	maxCiphertext := component.Length + component.Length/(64<<10)*32 + 1<<20
	if err != nil || info.Size() <= 0 || info.Size() > maxCiphertext {
		_ = ciphertext.Close()
		return nil, ErrRestore
	}
	plain, err := archive.Decrypt(ciphertext, passphrase)
	if err != nil {
		_ = ciphertext.Close()
		return nil, ErrRestore
	}
	return &databaseDumpReader{Reader: plain, file: ciphertext}, nil
}

type databaseDumpReader struct {
	io.Reader
	file *os.File
	once sync.Once
	err  error
}

func (r *databaseDumpReader) Close() error {
	r.once.Do(func() { r.err = r.file.Close() })
	return r.err
}

type discardToolOutput struct{}

func (discardToolOutput) WriteContext(ctx context.Context, data []byte) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return len(data), nil
}
func (discardToolOutput) CloseContext(context.Context) error { return nil }

var _ tools.OutputSink = discardToolOutput{}
