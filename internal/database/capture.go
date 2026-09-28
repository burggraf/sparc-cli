package database

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/destination"
	"github.com/burggraf/sparc-cli/internal/tools"
)

const (
	databaseDumpKey               = "database/postgresql.dump"
	databaseDumpScope             = "one full PostgreSQL database; cluster globals and provider-managed services are excluded"
	schemaSQLKey                  = "database/schema.sql"
	dataSQLKey                    = "database/data.sql"
	recoveryProfileKey            = "database/recovery-profile.json"
	recoveryProfileFormat         = "sparc-recovery-profile"
	recoveryProfileVersion        = 2
	schemaSQLScope                = "application schema DDL for public only; public schema creation made idempotent; managed schema DDL excluded"
	dataSQLScope                  = "public, Auth, and Storage table data with fixed exclusions"
	recoveryProfileScope          = "versioned database capture profile; completeness limitations declared"
	recoveryRolesMissing          = "custom roles and memberships are not captured"
	recoverySnapshotGap           = "schema and data dumps use separate source sessions; a shared snapshot is not established"
	recoveryFingerprintAlgorithm  = "sha256-xor-sum-v1"
	recoveryPublicSchemaTransform = "CREATE SCHEMA public -> CREATE SCHEMA IF NOT EXISTS public"
	databaseToolTimeout           = 6 * time.Hour
	maxToolStderrBytes            = 1 << 20
	maxNativeRootBytes            = 1 << 20
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

// Capture retains the legacy one-component custom-format database capture.
func Capture(ctx context.Context, request CaptureRequest) (archive.Manifest, error) {
	return captureWith(ctx, request, defaultRecoveryOps())
}

// CaptureSplit writes the current versioned, deliberately incomplete recovery profile.
func CaptureSplit(ctx context.Context, request CaptureRequest) (archive.Manifest, error) {
	return captureSplitWith(ctx, request, defaultRecoveryOps())
}

type RecoveryProfileV2 struct {
	Format                    string                     `json:"format"`
	Version                   int                        `json:"version"`
	CaptureStatus             string                     `json:"capture_status"`
	SourceProjectRef          string                     `json:"source_project_ref"`
	SourcePostgresMajor       int                        `json:"source_postgres_major"`
	SourceVersionNum          int                        `json:"source_version_num"`
	PublicSchemaOwner         string                     `json:"public_schema_owner"`
	ClientVersion             string                     `json:"client_version"`
	SchemaDump                recoveryDumpProfile        `json:"schema_dump"`
	DataDump                  recoveryDumpProfile        `json:"data_dump"`
	ExcludedTables            []recoveryProfileTable     `json:"excluded_tables"`
	Extensions                []recoveryProfileExtension `json:"extensions"`
	TableFingerprintAlgorithm string                     `json:"table_fingerprint_algorithm"`
	TableFingerprints         []RecoveryTableFingerprint `json:"table_fingerprints"`
	Roles                     recoveryCoverageProfile    `json:"roles"`
	Snapshot                  recoveryCoverageProfile    `json:"snapshot_consistency"`
	Unknowns                  []string                   `json:"unknowns"`
}

type recoveryDumpProfile struct {
	Key        string   `json:"key"`
	Mode       string   `json:"mode"`
	Schemas    []string `json:"schemas"`
	Status     string   `json:"status"`
	Transforms []string `json:"transforms"`
}

type recoveryProfileTable struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
}

type recoveryProfileExtension struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Schema  string `json:"schema"`
}

type recoveryCoverageProfile struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func captureWith(ctx context.Context, request CaptureRequest, ops recoveryOps) (archive.Manifest, error) {
	rootCert, _, err := prepareCapture(ctx, request, ops, nil)
	if err != nil {
		return archive.Manifest{}, err
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

func captureSplitWith(ctx context.Context, request CaptureRequest, ops recoveryOps) (archive.Manifest, error) {
	rootCert, observation, err := prepareCapture(ctx, request, ops, []string{"public", "auth", "storage"})
	if err != nil || observation.ServerVersionNum < 170000 || observation.ServerVersionNum >= 180000 {
		return archive.Manifest{}, ErrCapture
	}
	schemaReader, schemaWriter := io.Pipe()
	dataReader, dataWriter := io.Pipe()
	profileReader, profileWriter := io.Pipe()
	fingerprintReader, fingerprintWriter := io.Pipe()
	schemaArchiveSink := &archivePipeSink{writer: schemaWriter}
	schemaSink := newPublicSchemaTransformSink(schemaArchiveSink)
	dataSink := &archivePipeSink{writer: dataWriter}
	dataSource := &fingerprintTeeReader{source: dataReader, tee: fingerprintWriter}
	fingerprintResult := make(chan copyFingerprintResult, 1)
	go func() {
		fingerprints, err := parseCopyDataFingerprints(fingerprintReader)
		_ = fingerprintReader.Close()
		fingerprintResult <- copyFingerprintResult{fingerprints: fingerprints, err: err}
	}()
	archiveResult := make(chan archiveWriteResult, 1)
	go func() {
		defer schemaReader.Close()
		defer dataReader.Close()
		defer profileReader.Close()
		manifest, err := ops.create(request.ArchivePath, []archive.Input{
			{Key: schemaSQLKey, Scope: schemaSQLScope, Status: "incomplete", Source: schemaReader},
			{Key: dataSQLKey, Scope: dataSQLScope, Status: "incomplete", Source: dataSource},
			{Key: recoveryProfileKey, Scope: recoveryProfileScope, Status: "complete", Source: profileReader},
		}, request.ArchivePassphrase)
		archiveResult <- archiveWriteResult{manifest: manifest, err: err}
	}()

	runDump := func(mode tools.RunMode, schemas []string, exclusions []tools.TableRef, sink tools.AbortableOutputSink) (tools.RunResult, error) {
		result, runErr := ops.run(ctx, tools.RunRequest{
			Tool: tools.PGDump, Mode: mode, DumpSchemas: schemas, ExcludedTables: exclusions,
			Connection: &tools.PGConnection{
				Host: request.Source.Host, Port: request.Source.Port, User: request.Source.User, Database: request.Source.Database,
				Password: request.SourcePassword, RootCertPEM: rootCert,
			},
			Timeout: databaseToolTimeout, CleanupTimeout: 5 * time.Second,
			StdoutLimit: uint64(archive.MaxComponentBytes), StderrLimit: maxToolStderrBytes, Stdout: sink,
		})
		if ctx.Err() != nil && runErr == nil {
			runErr = tools.ErrRun
		}
		if runErr != nil || result.ExitCode != 0 {
			_ = sink.AbortContext(context.Background())
			return result, runErr
		}
		if sink.CloseContext(context.Background()) != nil {
			return result, tools.ErrOutput
		}
		return result, nil
	}

	schemaResult, schemaErr := runDump(tools.ModeDumpSchema, []string{"public"}, nil, schemaSink)
	var dataResult tools.RunResult
	var dataErr error
	if schemaErr != nil || schemaResult.ExitCode != 0 {
		_ = schemaSink.AbortContext(context.Background())
		_ = dataSink.AbortContext(context.Background())
		_ = dataSource.Close()
	} else {
		dataResult, dataErr = runDump(tools.ModeDumpData, []string{"public", "auth", "storage"}, recoveryDataExclusions(), dataSink)
		if dataErr != nil || dataResult.ExitCode != 0 {
			_ = dataSink.AbortContext(context.Background())
		}
	}
	fingerprints := <-fingerprintResult
	validDumps := schemaErr == nil && schemaResult.ExitCode == 0 && dataErr == nil && dataResult.ExitCode == 0 && fingerprints.err == nil
	if validDumps {
		profile := makeRecoveryProfile(observation, request.Source.ExpectedProjectRef)
		profile.TableFingerprints = fingerprints.fingerprints
		profileBytes, marshalErr := json.Marshal(profile)
		if !validRecoveryProfileV2(profile) {
			err = ErrRecoveryProfile
		} else if marshalErr == nil && len(profileBytes) <= maxRecoveryProfileBytes {
			_, err = profileWriter.Write(profileBytes)
			if err == nil {
				err = profileWriter.Close()
			}
		} else if marshalErr != nil {
			err = marshalErr
		} else {
			err = ErrRecoveryProfile
		}
	} else {
		err = ErrCapture
	}
	if err != nil {
		_ = profileWriter.CloseWithError(ErrCapture)
	}
	_ = dataSource.Close()
	archived := <-archiveResult
	if errors.Is(schemaErr, tools.ErrPayloadUnavailable) || errors.Is(dataErr, tools.ErrPayloadUnavailable) {
		return archive.Manifest{}, tools.ErrPayloadUnavailable
	}
	if !validDumps || err != nil || archived.err != nil || !validSplitCaptureManifest(archived.manifest) {
		return archive.Manifest{}, ErrCapture
	}
	return archived.manifest, nil
}

func prepareCapture(ctx context.Context, request CaptureRequest, ops recoveryOps, schemaNames []string) ([]byte, CatalogObservation, error) {
	if ctx == nil || !validRecoveryConnection(request.Source, request.SourcePassword) || !validRecoveryArchivePath(request.ArchivePath) || archive.ValidatePassphrase(request.ArchivePassphrase) != nil ||
		ops.rootCert == nil || ops.observe == nil || ops.prepareTool == nil || ops.run == nil || ops.create == nil || ops.prepareTool(ctx, tools.PGDump) != nil {
		return nil, CatalogObservation{}, ErrCapture
	}
	rootCert, err := ops.rootCert(request.Source)
	if err != nil || len(rootCert) == 0 || len(rootCert) > maxNativeRootBytes {
		return nil, CatalogObservation{}, ErrCapture
	}
	observation, err := ops.observe(ctx, request.Source, request.SourcePassword, schemaNames)
	if err != nil || observation.ServerMajor != supportedPostgresMajor || !observation.TLS || !observation.ReadOnly {
		return nil, CatalogObservation{}, ErrCapture
	}
	if len(schemaNames) != len(observation.Schemas) {
		return nil, CatalogObservation{}, ErrCapture
	}
	for index, name := range schemaNames {
		if observation.Schemas[index].Name != name || !observation.Schemas[index].Present {
			return nil, CatalogObservation{}, ErrCapture
		}
	}
	return rootCert, observation, nil
}

func makeRecoveryProfile(observation CatalogObservation, sourceProjectRef string) RecoveryProfileV2 {
	excluded := recoveryDataExclusions()
	publicOwner := ""
	for _, schema := range observation.Schemas {
		if schema.Name == "public" && schema.Present {
			publicOwner = schema.Owner
			break
		}
	}
	profile := RecoveryProfileV2{
		Format: recoveryProfileFormat, Version: recoveryProfileVersion, CaptureStatus: "incomplete", SourceProjectRef: sourceProjectRef,
		SourcePostgresMajor: observation.ServerMajor, SourceVersionNum: observation.ServerVersionNum, PublicSchemaOwner: publicOwner, ClientVersion: "PostgreSQL 17.11",
		SchemaDump:     recoveryDumpProfile{Key: schemaSQLKey, Mode: "schema-only", Schemas: []string{"public"}, Status: "incomplete", Transforms: []string{recoveryPublicSchemaTransform}},
		DataDump:       recoveryDumpProfile{Key: dataSQLKey, Mode: "data-only", Schemas: []string{"public", "auth", "storage"}, Status: "incomplete", Transforms: []string{}},
		ExcludedTables: make([]recoveryProfileTable, len(excluded)), Extensions: make([]recoveryProfileExtension, len(observation.Extensions)),
		TableFingerprintAlgorithm: recoveryFingerprintAlgorithm, TableFingerprints: []RecoveryTableFingerprint{},
		Roles:    recoveryCoverageProfile{Status: "missing", Reason: recoveryRolesMissing},
		Snapshot: recoveryCoverageProfile{Status: "unqualified", Reason: recoverySnapshotGap},
		Unknowns: recoveryProfileUnknowns(),
	}
	for index, table := range excluded {
		profile.ExcludedTables[index] = recoveryProfileTable{Schema: table.Schema, Name: table.Name}
	}
	for index, extension := range observation.Extensions {
		profile.Extensions[index] = recoveryProfileExtension{Name: extension.Name, Version: extension.Version, Schema: extension.Schema}
	}
	return profile
}

func recoveryProfileUnknowns() []string {
	return []string{
		"Only public schema DDL is selected; additional application schemas are not inventoried or captured.",
		"Public schema ACLs and database-wide DDL semantics are not qualified for restore.",
		"Managed-schema customizations, extension DDL compatibility, publications, event triggers, and cross-database dependencies are not qualified.",
		"Custom roles, memberships, ownership beyond the recorded public schema owner, and login credentials are not captured.",
		"Sequence state and large objects are captured by SQL but are not fingerprinted or verified.",
		"Storage object bytes, Auth service behavior, project settings, and provider-managed services are outside this database-only capture.",
	}
}

func recoveryDataExclusions() []tools.TableRef {
	return []tools.TableRef{
		{Schema: "auth", Name: "schema_migrations"},
		{Schema: "storage", Name: "migrations"},
		{Schema: "supabase_functions", Name: "migrations"},
		{Schema: "storage", Name: "buckets_vectors"},
		{Schema: "storage", Name: "vector_indexes"},
	}
}

func validSplitCaptureManifest(manifest archive.Manifest) bool {
	if manifest.Validate() != nil || len(manifest.Components) != 3 {
		return false
	}
	want := [...]struct{ key, scope, status string }{
		{schemaSQLKey, schemaSQLScope, "incomplete"},
		{dataSQLKey, dataSQLScope, "incomplete"},
		{recoveryProfileKey, recoveryProfileScope, "complete"},
	}
	for index, expected := range want {
		component := manifest.Components[index]
		if component.Key != expected.key || component.Scope != expected.scope || component.Length == 0 || component.Status != expected.status {
			return false
		}
	}
	return true
}

func defaultRecoveryOps() recoveryOps {
	return recoveryOps{
		rootCert:    nativeRootCertPEM,
		observe:     ObserveCatalog,
		prepareTool: tools.PrepareProductionPayload,
		run:         tools.Run,
		create:      destination.Create,
		verify:      archive.Verify,
		open:        openArchiveComponent,
	}
}

func validRecoveryConnection(params ConnectionParams, password []byte) bool {
	return params.Validate() == nil && credentials.ValidatePGPassEntry(credentials.PGPassEntry{
		Host: params.Host, Port: params.Port, Database: params.Database, User: params.User, Password: password,
	}) == nil
}

func validRecoveryArchivePath(path string) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) {
		return false
	}
	info, err := os.Stat(filepath.Dir(path))
	return err == nil && info.IsDir()
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

type copyFingerprintResult struct {
	fingerprints []RecoveryTableFingerprint
	err          error
}

type fingerprintTeeReader struct {
	source *io.PipeReader
	tee    *io.PipeWriter
	once   sync.Once
}

func (r *fingerprintTeeReader) Read(buffer []byte) (int, error) {
	n, readErr := r.source.Read(buffer)
	if n > 0 {
		written, writeErr := r.tee.Write(buffer[:n])
		if writeErr != nil || written != n {
			r.closeTee(ErrRecoveryData)
			return n, ErrRecoveryData
		}
	}
	if readErr != nil {
		if readErr == io.EOF {
			r.closeTee(nil)
		} else {
			r.closeTee(ErrRecoveryData)
		}
	}
	return n, readErr
}

func (r *fingerprintTeeReader) Close() error {
	closeErr := r.source.Close()
	r.closeTee(ErrCapture)
	return closeErr
}

func (r *fingerprintTeeReader) closeTee(err error) {
	r.once.Do(func() {
		if err != nil {
			_ = r.tee.CloseWithError(err)
		} else {
			_ = r.tee.Close()
		}
	})
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
