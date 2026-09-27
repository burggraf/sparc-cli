package database

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/destination"
	"github.com/burggraf/sparc-cli/internal/platform"
	"github.com/burggraf/sparc-cli/internal/tools"
)

const (
	databaseDumpKey     = "database/postgresql.dump"
	databaseDumpScope   = "one full PostgreSQL database; cluster globals and provider-managed services are excluded"
	databaseToolTimeout = 6 * time.Hour
	maxToolStderrBytes  = 1 << 20
	maxNativeRootBytes  = 1 << 20
)

var ErrCapture = errors.New("database capture failed")
var ErrRestore = errors.New("database restore failed")

// CaptureRequest names one source database and new local encrypted archive.
type CaptureRequest struct {
	Source            ConnectionParams
	SourcePassword    []byte
	ArchivePath       string
	ArchivePassphrase string
}

type recoveryOps struct {
	rootCert    func(ConnectionParams) ([]byte, error)
	observe     func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error)
	prepareTool func(context.Context, tools.Tool) error
	run         func(context.Context, tools.RunRequest) (tools.RunResult, error)
	create      func(string, []archive.Input, string) (archive.Manifest, error)
	verify      func(string, string) (archive.Manifest, error)
	open        func(string, archive.Component, string) (io.ReadCloser, error)
}

// Capture observes the source read-only, then streams one incomplete custom-format
// database component through the encrypted local archive publisher.
func Capture(ctx context.Context, request CaptureRequest) (archive.Manifest, error) {
	return captureWith(ctx, request, defaultRecoveryOps())
}

func captureWith(ctx context.Context, request CaptureRequest, ops recoveryOps) (archive.Manifest, error) {
	if ctx == nil || !validRecoveryConnection(request.Source, request.SourcePassword) || !validRecoveryArchivePath(request.ArchivePath) || archive.ValidatePassphrase(request.ArchivePassphrase) != nil ||
		ops.rootCert == nil || ops.observe == nil || ops.prepareTool == nil || ops.run == nil || ops.create == nil || ops.prepareTool(ctx, tools.PGDump) != nil {
		return archive.Manifest{}, ErrCapture
	}
	rootCert, err := ops.rootCert(request.Source)
	if err != nil || len(rootCert) == 0 || len(rootCert) > maxNativeRootBytes {
		return archive.Manifest{}, ErrCapture
	}
	observation, err := ops.observe(ctx, request.Source, request.SourcePassword, nil)
	if err != nil || observation.ServerMajor != supportedPostgresMajor || !observation.TLS || !observation.ReadOnly {
		return archive.Manifest{}, ErrCapture
	}

	reader, writer := io.Pipe()
	stream := &archivePipeSink{writer: writer}
	archiveResult := make(chan archiveWriteResult, 1)
	go func() {
		defer reader.Close()
		manifest, err := ops.create(request.ArchivePath, []archive.Input{{
			Key: databaseDumpKey, Scope: databaseDumpScope, Status: "incomplete", Source: reader,
		}}, request.ArchivePassphrase)
		archiveResult <- archiveWriteResult{manifest: manifest, err: err}
	}()

	result, runErr := ops.run(ctx, tools.RunRequest{
		Tool: tools.PGDump, Mode: tools.ModeDump,
		Connection: &tools.PGConnection{
			Host: request.Source.Host, Port: request.Source.Port, User: request.Source.User, Database: request.Source.Database,
			Password: request.SourcePassword, RootCertPEM: rootCert,
		},
		Timeout: databaseToolTimeout, CleanupTimeout: 5 * time.Second,
		StdoutLimit: uint64(archive.MaxComponentBytes), StderrLimit: maxToolStderrBytes,
		Stdout: stream,
	})
	if ctx.Err() != nil && runErr == nil {
		runErr = tools.ErrRun
	}
	if runErr != nil || result.ExitCode != 0 {
		_ = stream.AbortContext(context.Background())
	} else {
		_ = stream.CloseContext(context.Background())
	}
	archived := <-archiveResult
	if errors.Is(runErr, tools.ErrPayloadUnavailable) {
		return archive.Manifest{}, tools.ErrPayloadUnavailable
	}
	if runErr != nil || result.ExitCode != 0 || archived.err != nil || !validDatabaseDumpManifest(archived.manifest) {
		return archive.Manifest{}, ErrCapture
	}
	return archived.manifest, nil
}

func defaultRecoveryOps() recoveryOps {
	return recoveryOps{
		rootCert:    nativeRootCertPEM,
		observe:     ObserveCatalog,
		prepareTool: tools.PrepareProductionPayload,
		run:         tools.Run,
		create:      destination.Create,
		verify:      archive.Verify,
		open:        openDatabaseDump,
	}
}

func validRecoveryConnection(params ConnectionParams, password []byte) bool {
	return params.Validate() == nil && credentials.ValidatePGPassEntry(credentials.PGPassEntry{
		Host: params.Host, Port: params.Port, Database: params.Database, User: params.User, Password: password,
	}) == nil
}

func validRecoveryArchivePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Base(path) != "." && filepath.Base(path) != string(filepath.Separator) &&
		platform.CheckPrivateDir(filepath.Dir(path)) == nil
}

func validDatabaseDumpManifest(manifest archive.Manifest) bool {
	return manifest.Validate() == nil && len(manifest.Components) == 1 && manifest.Components[0].Key == databaseDumpKey &&
		manifest.Components[0].Scope == databaseDumpScope && manifest.Components[0].Status == "incomplete" && manifest.Components[0].Length > 0
}

func nativeRootCertPEM(params ConnectionParams) ([]byte, error) {
	if params.SSLRootCert == "supabase" {
		if _, ok := supabaseRootCAPool(); !ok {
			return nil, ErrConnectionParameters
		}
		return append([]byte(nil), supabaseRootCA...), nil
	}
	if params.SSLRootCert == "system" {
		return nil, ErrConnectionParameters
	}
	path := params.SSLRootCert
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, ErrConnectionParameters
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrConnectionParameters
	}
	opened, statErr := file.Stat()
	data, readErr := io.ReadAll(io.LimitReader(file, maxNativeRootBytes+1))
	closeErr := file.Close()
	if statErr != nil || !os.SameFile(before, opened) || readErr != nil || closeErr != nil || len(data) == 0 || len(data) > maxNativeRootBytes || !x509.NewCertPool().AppendCertsFromPEM(data) {
		return nil, ErrConnectionParameters
	}
	return data, nil
}

type archiveWriteResult struct {
	manifest archive.Manifest
	err      error
}

type archivePipeSink struct {
	writer *io.PipeWriter
	once   sync.Once
	err    error
}

func (s *archivePipeSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	abortDone := make(chan struct{})
	stopAbort := context.AfterFunc(ctx, func() {
		_ = s.AbortContext(context.Background())
		close(abortDone)
	})
	n, err := s.writer.Write(data)
	if !stopAbort() {
		<-abortDone
	}
	if ctx.Err() != nil {
		return n, ctx.Err()
	}
	return n, err
}

func (s *archivePipeSink) CloseContext(context.Context) error {
	s.once.Do(func() { s.err = s.writer.Close() })
	return s.err
}

func (s *archivePipeSink) AbortContext(context.Context) error {
	s.once.Do(func() { s.err = s.writer.CloseWithError(ErrCapture) })
	return s.err
}

var _ tools.AbortableOutputSink = (*archivePipeSink)(nil)
