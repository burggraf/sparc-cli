package database

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/destination"
	"github.com/burggraf/sparc-cli/internal/platform"
	"github.com/burggraf/sparc-cli/internal/tools"
)

const recoveryTestPassphrase = "synthetic-recovery-passphrase"

func TestCaptureRequiresPostgres17VerifiedReadOnlyObservation(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*CatalogObservation)
	}{
		{"wrong major", func(o *CatalogObservation) { o.ServerMajor = 16 }},
		{"TLS absent", func(o *CatalogObservation) { o.TLS = false }},
		{"read-only absent", func(o *CatalogObservation) { o.ReadOnly = false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ops := recoveryTestOps()
			observation := CatalogObservation{ServerMajor: 17, TLS: true, ReadOnly: true}
			test.edit(&observation)
			ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
				return observation, nil
			}
			var ran, published bool
			ops.run = func(context.Context, tools.RunRequest) (tools.RunResult, error) {
				ran = true
				return tools.RunResult{}, nil
			}
			ops.create = func(string, []archive.Input, string) (archive.Manifest, error) {
				published = true
				return archive.Manifest{}, nil
			}
			request := captureTestRequest(t)
			if _, err := captureWith(context.Background(), request, ops); err != ErrCapture {
				t.Fatalf("capture error = %v", err)
			}
			if ran || published {
				t.Fatalf("unsafe source reached dump/publication: ran=%v published=%v", ran, published)
			}
		})
	}
}

func TestCaptureRejectsMissingBundledToolBeforeSourceObservation(t *testing.T) {
	request := captureTestRequest(t)
	ops := recoveryTestOps()
	observed := false
	ops.prepareTool = func(_ context.Context, tool tools.Tool) error {
		if tool != tools.PGDump {
			t.Fatalf("preflight tool = %v, want pg_dump", tool)
		}
		return tools.ErrPayloadUnavailable
	}
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		observed = true
		return CatalogObservation{}, nil
	}
	if _, err := captureWith(context.Background(), request, ops); err != ErrCapture || observed {
		t.Fatalf("missing bundled client reached source observation: err=%v observed=%t", err, observed)
	}
}

func TestCaptureRejectsMissingArchiveParentBeforeSourceObservation(t *testing.T) {
	request := captureTestRequest(t)
	request.ArchivePath = filepath.Join(t.TempDir(), "missing", "capture")
	ops := recoveryTestOps()
	observed := false
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		observed = true
		return CatalogObservation{}, nil
	}
	if _, err := captureWith(context.Background(), request, ops); err != ErrCapture || observed {
		t.Fatalf("missing archive parent reached source observation: err=%v observed=%t", err, observed)
	}
}

func TestCaptureAcceptsOrdinaryArchiveParentBeforeSourceObservation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "ordinary")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	request := captureTestRequest(t)
	request.ArchivePath = filepath.Join(parent, "capture")
	ops := recoveryTestOps()
	observed := false
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		observed = true
		return CatalogObservation{}, ErrCapture
	}
	if _, err := captureWith(context.Background(), request, ops); err != ErrCapture || !observed {
		t.Fatalf("ordinary archive parent was rejected before source observation: err=%v observed=%t", err, observed)
	}
}

func TestCaptureStreamsOneIncompleteDatabaseComponent(t *testing.T) {
	request := captureTestRequest(t)
	ops := recoveryTestOps()
	const dump = "synthetic custom-format pg_dump"
	got := make(chan capturedTestComponent, 1)
	ops.create = func(path string, inputs []archive.Input, passphrase string) (archive.Manifest, error) {
		if len(inputs) != 1 {
			return archive.Manifest{}, errors.New("wrong component count")
		}
		data, err := io.ReadAll(inputs[0].Source)
		got <- capturedTestComponent{path: path, input: inputs[0], passphrase: passphrase, data: string(data), err: err}
		if err != nil {
			return archive.Manifest{}, err
		}
		return testDatabaseDumpManifest([]byte(data)), nil
	}
	ops.run = func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
		if request.Tool != tools.PGDump || request.Mode != tools.ModeDump || request.Connection == nil || request.Connection.Host != "db.abcdefghijklmnopqrst.supabase.co" || string(request.Connection.RootCertPEM) != "synthetic-root" {
			return tools.RunResult{}, tools.ErrRun
		}
		written, err := request.Stdout.WriteContext(ctx, []byte(dump))
		closeErr := request.Stdout.CloseContext(ctx)
		if err != nil || closeErr != nil || written != len(dump) {
			return tools.RunResult{}, tools.ErrOutput
		}
		return tools.RunResult{ExitCode: 0, StdoutBytes: uint64(written)}, nil
	}
	manifest, err := captureWith(context.Background(), request, ops)
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	component := <-got
	if component.err != nil || component.path != request.ArchivePath || component.passphrase != request.ArchivePassphrase || component.data != dump ||
		component.input.Key != databaseDumpKey || component.input.Scope != databaseDumpScope || component.input.Status != "incomplete" {
		t.Fatalf("captured component = %#v", component)
	}
	if len(manifest.Components) != 1 || manifest.Components[0].Key != databaseDumpKey || manifest.Components[0].Status != "incomplete" {
		t.Fatalf("capture manifest = %#v", manifest)
	}
}

func TestCaptureSplitPublishesTwoEncryptedSQLComponentsAndProfile(t *testing.T) {
	request := captureTestRequest(t)
	ops := recoveryTestOps()
	ops.create = destination.Create
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		return CatalogObservation{
			ServerVersionNum: 170009, ServerMajor: 17, TLS: true, ReadOnly: true,
			Schemas:    []SchemaObservation{{Name: "public", Present: true}, {Name: "auth", Present: true}, {Name: "storage", Present: true}},
			Extensions: []ExtensionObservation{{Name: "postgis", Version: "3.5.2", Schema: "extensions"}},
		}, nil
	}
	var calls int
	ops.run = func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
		calls++
		if request.Tool != tools.PGDump || request.Connection == nil || request.Connection.Host != "db.abcdefghijklmnopqrst.supabase.co" || string(request.Connection.RootCertPEM) != "synthetic-root" {
			t.Fatalf("split dump request = %#v", request)
		}
		var payload string
		switch request.Mode {
		case tools.ModeDumpSchema:
			if !equalStrings(request.DumpSchemas, []string{"public"}) || len(request.ExcludedTables) != 0 {
				t.Fatalf("schema dump selection = %#v %#v", request.DumpSchemas, request.ExcludedTables)
			}
			payload = "CREATE TABLE public.synthetic_items (id integer);\n"
		case tools.ModeDumpData:
			if !equalStrings(request.DumpSchemas, []string{"public", "auth", "storage"}) || !hasRequiredRecoveryExclusions(request.ExcludedTables) {
				t.Fatalf("data dump selection = %#v %#v", request.DumpSchemas, request.ExcludedTables)
			}
			payload = "COPY auth.users (id) FROM stdin;\n17\n\\.\n"
		default:
			t.Fatalf("unexpected dump mode %v", request.Mode)
		}
		written, err := request.Stdout.WriteContext(ctx, []byte(payload))
		closeErr := request.Stdout.CloseContext(ctx)
		if err != nil || closeErr != nil || written != len(payload) {
			return tools.RunResult{}, tools.ErrOutput
		}
		return tools.RunResult{ExitCode: 0, StdoutBytes: uint64(written)}, nil
	}

	manifest, err := captureSplitWith(context.Background(), request, ops)
	if err != nil {
		t.Fatalf("split capture failed: %v", err)
	}
	if calls != 2 || len(manifest.Components) != 3 || manifest.Components[0].Key != "database/schema.sql" || manifest.Components[1].Key != "database/data.sql" || manifest.Components[2].Key != recoveryProfileKey {
		t.Fatalf("split capture manifest = %#v, calls=%d", manifest, calls)
	}
	verified, err := archive.Verify(request.ArchivePath, request.ArchivePassphrase)
	if err != nil || len(verified.Components) != 3 {
		t.Fatalf("split archive verification = %#v, %v", verified, err)
	}
	components := make([][]byte, len(manifest.Components))
	for index, component := range manifest.Components {
		components[index] = readEncryptedTestComponent(t, request.ArchivePath, component, request.ArchivePassphrase)
	}
	if string(components[0]) != "CREATE TABLE public.synthetic_items (id integer);\n" || !strings.Contains(string(components[1]), "COPY auth.users") {
		t.Fatalf("split SQL components = %q / %q", components[0], components[1])
	}
	var profile RecoveryProfileV1
	if err := json.Unmarshal(components[2], &profile); err != nil {
		t.Fatalf("recovery profile is not JSON: %v", err)
	}
	if profile.Format != recoveryProfileFormat || profile.Version != 1 || profile.CaptureStatus != "incomplete" || profile.SourcePostgresMajor != 17 || profile.SourceVersionNum != 170009 || profile.ClientVersion != "PostgreSQL 17.11" || profile.SchemaDump.Key != "database/schema.sql" || profile.DataDump.Key != "database/data.sql" || len(profile.Unknowns) == 0 || len(profile.Extensions) != 1 {
		t.Fatalf("recovery profile = %#v", profile)
	}
	if strings.Contains(string(components[2]), "synthetic-db-password") || strings.Contains(string(components[2]), "synthetic-root") || strings.Contains(string(components[2]), "abcdefghijklmnopqrst") {
		t.Fatal("recovery profile disclosed source credentials or project identity")
	}
}

func TestCaptureSplitRequiresManagedDataSchemasBeforeDump(t *testing.T) {
	request := captureTestRequest(t)
	ops := recoveryTestOps()
	observed := false
	var ran, published bool
	ops.observe = func(_ context.Context, _ ConnectionParams, _ []byte, schemas []string) (CatalogObservation, error) {
		observed = true
		if !equalStrings(schemas, []string{"public", "auth", "storage"}) {
			t.Fatalf("source schema preflight = %#v", schemas)
		}
		return CatalogObservation{
			ServerVersionNum: 170009, ServerMajor: 17, TLS: true, ReadOnly: true,
			Schemas: []SchemaObservation{{Name: "public", Present: true}, {Name: "auth", Present: true}},
		}, nil
	}
	ops.run = func(context.Context, tools.RunRequest) (tools.RunResult, error) {
		ran = true
		return tools.RunResult{}, nil
	}
	ops.create = func(string, []archive.Input, string) (archive.Manifest, error) {
		published = true
		return archive.Manifest{}, nil
	}
	if _, err := captureSplitWith(context.Background(), request, ops); err != ErrCapture || !observed || ran || published {
		t.Fatalf("missing source schema preflight = err %v, observed=%t ran=%t published=%t", err, observed, ran, published)
	}
}

func TestCaptureSplitDataFailureLeavesNoPublishedArchive(t *testing.T) {
	request := captureTestRequest(t)
	ops := recoveryTestOps()
	ops.create = destination.Create
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		return CatalogObservation{ServerVersionNum: 170009, ServerMajor: 17, TLS: true, ReadOnly: true, Schemas: []SchemaObservation{{Name: "public", Present: true}, {Name: "auth", Present: true}, {Name: "storage", Present: true}}}, nil
	}
	calls := 0
	ops.run = func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
		calls++
		if request.Mode == tools.ModeDumpSchema {
			_, _ = request.Stdout.WriteContext(ctx, []byte("CREATE TABLE public.synthetic_items (id integer);\n"))
			_ = request.Stdout.CloseContext(ctx)
			return tools.RunResult{ExitCode: 0}, nil
		}
		_, _ = request.Stdout.WriteContext(ctx, []byte("partial SQL"))
		return tools.RunResult{}, tools.ErrRun
	}
	if _, err := captureSplitWith(context.Background(), request, ops); err != ErrCapture || calls != 2 {
		t.Fatalf("split failure = %v, calls=%d", err, calls)
	}
	if _, err := os.Lstat(request.ArchivePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed split capture published an archive: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(request.ArchivePath))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed split capture left staging files: entries=%v err=%v", entries, err)
	}
}

func TestCaptureAbortsPartialDumpAndRefusesPublicationFailure(t *testing.T) {
	t.Run("dump exits after prefix", func(t *testing.T) {
		ops := recoveryTestOps()
		published := make(chan bool, 1)
		ops.create = func(_ string, inputs []archive.Input, _ string) (archive.Manifest, error) {
			data, err := io.ReadAll(inputs[0].Source)
			published <- err == nil && len(data) > 0
			return archive.Manifest{}, err
		}
		ops.run = func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
			_, _ = request.Stdout.WriteContext(ctx, []byte("valid-looking-prefix"))
			return tools.RunResult{}, tools.ErrRun
		}
		if _, err := captureWith(context.Background(), captureTestRequest(t), ops); err != ErrCapture {
			t.Fatalf("capture error = %v", err)
		}
		if <-published {
			t.Fatal("partial dump was publishable")
		}
	})

	t.Run("destination rejects archive", func(t *testing.T) {
		ops := recoveryTestOps()
		ops.create = func(_ string, inputs []archive.Input, _ string) (archive.Manifest, error) {
			_, err := io.Copy(io.Discard, inputs[0].Source)
			if err != nil {
				return archive.Manifest{}, err
			}
			return archive.Manifest{}, errors.New("synthetic publication failure")
		}
		ops.run = func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
			_, _ = request.Stdout.WriteContext(ctx, []byte("synthetic dump"))
			_ = request.Stdout.CloseContext(ctx)
			return tools.RunResult{}, nil
		}
		if _, err := captureWith(context.Background(), captureTestRequest(t), ops); err != ErrCapture {
			t.Fatalf("capture error = %v", err)
		}
	})
}

func TestCaptureCancellationUnblocksStreamingSink(t *testing.T) {
	request := captureTestRequest(t)
	ops := recoveryTestOps()
	releaseArchive := make(chan struct{})
	writeBlocked := make(chan struct{})
	ops.create = func(string, []archive.Input, string) (archive.Manifest, error) {
		<-releaseArchive
		return archive.Manifest{}, ErrCapture
	}
	ops.run = func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
		writeResult := make(chan error, 1)
		go func() {
			_, err := request.Stdout.WriteContext(ctx, bytes.Repeat([]byte("x"), 1<<20))
			writeResult <- err
		}()
		select {
		case err := <-writeResult:
			close(releaseArchive)
			return tools.RunResult{}, err
		case <-time.After(200 * time.Millisecond):
			close(writeBlocked)
		}
		err := <-writeResult
		close(releaseArchive)
		if err != nil {
			return tools.RunResult{}, tools.ErrOutput
		}
		return tools.RunResult{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := captureWith(ctx, request, ops)
		result <- err
	}()
	select {
	case <-writeBlocked:
	case <-time.After(10 * time.Second):
		t.Fatal("capture output did not block as expected")
	}
	cancel()
	select {
	case err := <-result:
		if err != ErrCapture {
			t.Fatalf("capture cancellation error = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("capture cancellation did not unblock the output stream")
	}
}

func TestCaptureFailedDumpLeavesNoPublishedArchive(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(parent); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "failed-capture")
	ops := defaultRecoveryOps()
	ops.rootCert = func(ConnectionParams) ([]byte, error) { return []byte("synthetic-root"), nil }
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		return CatalogObservation{ServerMajor: 17, TLS: true, ReadOnly: true}, nil
	}
	ops.run = func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
		_, _ = request.Stdout.WriteContext(ctx, []byte("valid-looking-prefix"))
		return tools.RunResult{}, tools.ErrRun
	}
	request := captureTestRequest(t)
	request.ArchivePath = path
	if _, err := captureWith(context.Background(), request, ops); err != ErrCapture {
		t.Fatalf("capture error = %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed dump published an archive: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed dump left staging files: entries=%v err=%v", entries, err)
	}
}

func TestRestoreVerifiesExactArchiveBeforeTargetObservation(t *testing.T) {
	ops := recoveryTestOps()
	observed := false
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		observed = true
		return emptyRecoveryObservation(), nil
	}
	manifest := testDatabaseDumpManifest([]byte("dump"))
	manifest.Components[0].Key = "database/other.dump"
	ops.verify = func(string, string) (archive.Manifest, error) { return manifest, nil }
	if err := restoreWith(context.Background(), restoreTestRequest(t), ops); err != ErrRestore {
		t.Fatalf("restore error = %v", err)
	}
	if observed {
		t.Fatal("target was observed before the archive component was accepted")
	}
}

func TestRestorePreflightsTargetBeforeDecryptingOrRunning(t *testing.T) {
	ops := recoveryTestOps()
	observation := emptyRecoveryObservation()
	observation.Relations = []RelationObservation{{Schema: "public", Name: "seeded_table"}}
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		return observation, nil
	}
	var opened, ran bool
	ops.open = func(string, archive.Component, string) (io.ReadCloser, error) {
		opened = true
		return io.NopCloser(strings.NewReader("dump")), nil
	}
	ops.run = func(context.Context, tools.RunRequest) (tools.RunResult, error) {
		ran = true
		return tools.RunResult{}, nil
	}
	if err := restoreWith(context.Background(), restoreTestRequest(t), ops); err != ErrRestore {
		t.Fatalf("restore error = %v", err)
	}
	if opened || ran {
		t.Fatalf("unsafe target reached decryption/restore: opened=%v ran=%v", opened, ran)
	}
}

func TestRestoreVerifiesThenPreflightsThenStreamsSingleTargetComponent(t *testing.T) {
	request := restoreTestRequest(t)
	ops := recoveryTestOps()
	var steps []string
	ops.verify = func(path, passphrase string) (archive.Manifest, error) {
		steps = append(steps, "verify")
		if path != request.ArchivePath || passphrase != request.ArchivePassphrase {
			return archive.Manifest{}, ErrRestore
		}
		return testDatabaseDumpManifest([]byte("synthetic encrypted dump")), nil
	}
	ops.observe = func(_ context.Context, params ConnectionParams, password []byte, schemas []string) (CatalogObservation, error) {
		steps = append(steps, "observe")
		if params != request.Target || string(password) != string(request.TargetPassword) || strings.Join(schemas, ",") != "public,sparc_app" {
			return CatalogObservation{}, ErrRestore
		}
		return emptyRecoveryObservation(), nil
	}
	ops.open = func(_ string, component archive.Component, passphrase string) (io.ReadCloser, error) {
		steps = append(steps, "decrypt")
		if component.Key != databaseDumpKey || passphrase != request.ArchivePassphrase {
			return nil, ErrRestore
		}
		return io.NopCloser(strings.NewReader("synthetic encrypted dump")), nil
	}
	ops.run = func(_ context.Context, operation tools.RunRequest) (tools.RunResult, error) {
		steps = append(steps, "restore")
		if operation.Tool != tools.PGRestore || operation.Mode != tools.ModeRestore || operation.Connection == nil ||
			operation.Connection.Host != request.Target.Host || operation.Connection.User != request.Target.User ||
			operation.InputLimit != uint64(len("synthetic encrypted dump")) || string(operation.Connection.RootCertPEM) != "synthetic-root" {
			return tools.RunResult{}, tools.ErrRun
		}
		data, err := io.ReadAll(operation.Input)
		if closeErr := operation.Input.Close(); err != nil || closeErr != nil || string(data) != "synthetic encrypted dump" {
			return tools.RunResult{}, tools.ErrRun
		}
		return tools.RunResult{ExitCode: 0}, nil
	}
	if err := restoreWith(context.Background(), request, ops); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if strings.Join(steps, ",") != "verify,observe,decrypt,restore" {
		t.Fatalf("restore order = %v", steps)
	}
}

func TestRestoreRejectsMissingBundledToolBeforeTargetObservation(t *testing.T) {
	request := restoreTestRequest(t)
	ops := recoveryTestOps()
	verified, observed := false, false
	ops.verify = func(string, string) (archive.Manifest, error) {
		verified = true
		return testDatabaseDumpManifest([]byte("dump")), nil
	}
	ops.prepareTool = func(_ context.Context, tool tools.Tool) error {
		if tool != tools.PGRestore {
			t.Fatalf("preflight tool = %v, want pg_restore", tool)
		}
		return tools.ErrPayloadUnavailable
	}
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		observed = true
		return emptyRecoveryObservation(), nil
	}
	if err := restoreWith(context.Background(), request, ops); err != ErrRestore || !verified || observed {
		t.Fatalf("restore preflight err=%v verified=%t observed=%t", err, verified, observed)
	}
}

func TestRestoreRefusesArchiveAndTargetObservationFailures(t *testing.T) {
	t.Run("archive verification", func(t *testing.T) {
		ops := recoveryTestOps()
		var observed bool
		ops.verify = func(string, string) (archive.Manifest, error) { return archive.Manifest{}, archive.ErrArchive }
		ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
			observed = true
			return emptyRecoveryObservation(), nil
		}
		if err := restoreWith(context.Background(), restoreTestRequest(t), ops); err != ErrRestore || observed {
			t.Fatalf("restore error=%v observed=%v", err, observed)
		}
	})
	t.Run("target observation", func(t *testing.T) {
		ops := recoveryTestOps()
		ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
			return CatalogObservation{}, ErrDatabaseConnect
		}
		var opened bool
		ops.open = func(string, archive.Component, string) (io.ReadCloser, error) { opened = true; return nil, ErrRestore }
		if err := restoreWith(context.Background(), restoreTestRequest(t), ops); err != ErrRestore || opened {
			t.Fatalf("restore error=%v opened=%v", err, opened)
		}
	})
}

type capturedTestComponent struct {
	path, passphrase, data string
	input                  archive.Input
	err                    error
}

type recoveryTestInput struct {
	io.Reader
	closed bool
}

func (r *recoveryTestInput) Close() error { r.closed = true; return nil }

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func hasRequiredRecoveryExclusions(tables []tools.TableRef) bool {
	want := recoveryDataExclusions()
	if len(tables) != len(want) {
		return false
	}
	for index := range want {
		if tables[index] != want[index] {
			return false
		}
	}
	return true
}

func readEncryptedTestComponent(t *testing.T, archivePath string, component archive.Component, passphrase string) []byte {
	t.Helper()
	file, err := os.Open(filepath.Join(archivePath, component.ID+".age"))
	if err != nil {
		t.Fatal("unable to open test archive component")
	}
	defer file.Close()
	reader, err := archive.Decrypt(file, passphrase)
	if err != nil {
		t.Fatal("unable to decrypt test archive component")
	}
	data, err := io.ReadAll(io.LimitReader(reader, component.Length+1))
	if err != nil || int64(len(data)) != component.Length {
		t.Fatal("test archive component did not match its declared length")
	}
	return data
}

func captureTestRequest(t *testing.T) CaptureRequest {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("unable to resolve capture test root")
	}
	privateDir := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(privateDir); err != nil {
		t.Fatal("unable to create private capture test directory")
	}
	return CaptureRequest{
		Source: validConnectionParams(t), SourcePassword: []byte("synthetic-db-password"),
		ArchivePath: filepath.Join(privateDir, "capture"), ArchivePassphrase: recoveryTestPassphrase,
	}
}

func restoreTestRequest(t *testing.T) RestoreRequest {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("unable to resolve restore test root")
	}
	privateDir := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(privateDir); err != nil {
		t.Fatal("unable to create private restore test directory")
	}
	return RestoreRequest{
		Target: validConnectionParams(t), TargetPassword: []byte("synthetic-target-password"),
		ArchivePath: filepath.Join(privateDir, "archive"), ArchivePassphrase: recoveryTestPassphrase,
		EmptyScope: EmptyTargetScopeV1{RequiredPresent: []string{"public"}, RequiredAbsent: []string{"sparc_app"}},
	}
}

func recoveryTestOps() recoveryOps {
	return recoveryOps{
		rootCert: func(ConnectionParams) ([]byte, error) { return []byte("synthetic-root"), nil },
		observe: func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
			return CatalogObservation{ServerMajor: 17, TLS: true, ReadOnly: true}, nil
		},
		prepareTool: func(context.Context, tools.Tool) error { return nil },
		run: func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
			if request.Stdout != nil {
				_ = request.Stdout.CloseContext(ctx)
			}
			if request.Input != nil {
				_, _ = io.Copy(io.Discard, request.Input)
				_ = request.Input.Close()
			}
			return tools.RunResult{ExitCode: 0}, nil
		},
		create: func(_ string, inputs []archive.Input, passphrase string) (archive.Manifest, error) {
			if len(inputs) != 1 {
				return archive.Manifest{}, ErrCapture
			}
			data, err := io.ReadAll(inputs[0].Source)
			if err != nil {
				return archive.Manifest{}, err
			}
			return testDatabaseDumpManifest(data), nil
		},
		verify: func(string, string) (archive.Manifest, error) { return testDatabaseDumpManifest([]byte("dump")), nil },
		open: func(_ string, component archive.Component, _ string) (io.ReadCloser, error) {
			return &recoveryTestInput{Reader: strings.NewReader(strings.Repeat("d", int(component.Length)))}, nil
		},
	}
}

func emptyRecoveryObservation() CatalogObservation {
	return CatalogObservation{
		ServerMajor: 17, TLS: true, ReadOnly: true,
		Schemas:  []SchemaObservation{{Name: "public", Present: true}, {Name: "sparc_app", Present: false}},
		Security: SecurityObservation{Observed: true},
	}
}

func testDatabaseDumpManifest(data []byte) archive.Manifest {
	digest := sha256.Sum256(data)
	return archive.Manifest{Format: archive.Format, Version: archive.Version, Components: []archive.Component{{
		ID: "00000000", Key: databaseDumpKey, Scope: databaseDumpScope, Status: "incomplete",
		Length: int64(len(data)), SHA256: hex.EncodeToString(digest[:]),
	}}}
}
