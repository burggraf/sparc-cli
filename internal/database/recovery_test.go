package database

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
			Schemas:    []SchemaObservation{{Name: "public", Present: true, Owner: "postgres"}, {Name: "auth", Present: true}, {Name: "storage", Present: true}},
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
			payload = "CREATE SCHEMA public;\nCREATE TABLE public.synthetic_items (id integer);\n"
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
	if string(components[0]) != "CREATE SCHEMA IF NOT EXISTS public;\nCREATE TABLE public.synthetic_items (id integer);\n" || !strings.Contains(string(components[1]), "COPY auth.users") {
		t.Fatal("split SQL components did not match the fixed profile")
	}
	var profile RecoveryProfileV2
	if err := json.Unmarshal(components[2], &profile); err != nil {
		t.Fatalf("recovery profile is not JSON: %v", err)
	}
	if profile.Format != recoveryProfileFormat || profile.Version != recoveryProfileVersion || profile.CaptureStatus != "incomplete" || profile.SourceProjectRef != "abcdefghijklmnopqrst" || profile.SourcePostgresMajor != 17 || profile.SourceVersionNum != 170009 || profile.ClientVersion != "PostgreSQL 17.11" || profile.PublicSchemaOwner != "postgres" || profile.SchemaDump.Key != "database/schema.sql" || profile.DataDump.Key != "database/data.sql" || !equalStrings(profile.SchemaDump.Transforms, []string{recoveryPublicSchemaTransform}) || len(profile.Unknowns) == 0 || len(profile.Extensions) != 1 {
		t.Fatalf("recovery profile = %#v", profile)
	}
	if strings.Contains(string(components[2]), "synthetic-db-password") || strings.Contains(string(components[2]), "synthetic-root") {
		t.Fatal("recovery profile disclosed source credentials")
	}
}

func TestCopyDataFingerprintsAreOrderIndependentAndContentSensitive(t *testing.T) {
	first := "COPY public.items (id, value) FROM stdin;\n1\talpha\n2\tbeta\\nline\n\\.\n"
	second := "COPY public.items (id, value) FROM stdin;\n2\tbeta\\nline\n1\talpha\n\\.\n"
	changed := "COPY public.items (id, value) FROM stdin;\n1\talpha\n2\tchanged\n\\.\n"
	parse := func(data string) []RecoveryTableFingerprint {
		t.Helper()
		fingerprints, err := parseCopyDataFingerprints(strings.NewReader(data))
		if err != nil || len(fingerprints) != 1 {
			t.Fatalf("COPY fingerprint parse = %#v, %v", fingerprints, err)
		}
		return fingerprints
	}
	one, two, three := parse(first)[0], parse(second)[0], parse(changed)[0]
	if one.Rows != 2 || one.Fingerprint != two.Fingerprint || one.Fingerprint == three.Fingerprint || !equalStrings(one.Columns, []string{"id", "value"}) {
		t.Fatalf("COPY fingerprints = %#v / %#v / %#v", one, two, three)
	}
}

func TestCopyDataFingerprintsRefuseUnknownOrTruncatedStreams(t *testing.T) {
	for _, input := range []string{
		"COPY public.items (id) FROM stdin;\n1\n",
		"COPY cron.job (id) FROM stdin;\n1\n\\.\n",
		"COPY public.items FROM PROGRAM 'unsafe';\n",
	} {
		if _, err := parseCopyDataFingerprints(strings.NewReader(input)); err != ErrRecoveryData {
			t.Fatalf("unqualified COPY input error = %v", err)
		}
	}
}

func TestFingerprintVerificationSinkRequiresMatchingRowsBeforeCommit(t *testing.T) {
	fingerprints, err := parseCopyDataFingerprints(strings.NewReader("COPY public.items (id, value) FROM stdin;\n1\talpha\n2\tbeta\n\\.\n"))
	if err != nil {
		t.Fatal(err)
	}
	validOutput := "\n  \nSPARC_VERIFY_BEGIN 0\n1\talpha\n2\tbeta\nSPARC_VERIFY_END 0\nSPARC_VERIFY_DONE\n"
	gate := newRestoreCommitGate()
	if n, err := gate.Read(nil); n != 0 || err != nil {
		t.Fatalf("empty gate read = %d, %v", n, err)
	}
	sink := newFingerprintVerificationSink(fingerprints, gate)
	if _, err := sink.WriteContext(context.Background(), []byte(validOutput)); err != nil || sink.CloseContext(context.Background()) != nil {
		t.Fatalf("valid archive-derived verification rejected: %v", err)
	}
	if _, err := gate.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("verified transaction gate = %v", err)
	}

	gate = newRestoreCommitGate()
	sink = newFingerprintVerificationSink(fingerprints, gate)
	if _, err := sink.WriteContext(context.Background(), []byte(strings.Replace(validOutput, "2\tbeta", "2\tchanged", 1))); err != ErrRecoveryMismatch {
		t.Fatalf("changed archive-derived row error = %v", err)
	}
	select {
	case err := <-gate.ready:
		if err != ErrRestore {
			t.Fatalf("mismatch gate result = %v, want cancellation", err)
		}
	default:
		t.Fatal("mismatched archive data did not cancel the transaction input gate")
	}
}

func TestFingerprintVerificationAcceptsEmptyRowsAfterTheirMarker(t *testing.T) {
	fingerprints, err := parseCopyDataFingerprints(strings.NewReader("COPY public.items (value) FROM stdin;\n\n\\.\n"))
	if err != nil || len(fingerprints) != 1 {
		t.Fatalf("empty COPY row fingerprint = %#v, %v", fingerprints, err)
	}
	gate := newRestoreCommitGate()
	sink := newFingerprintVerificationSink(fingerprints, gate)
	output := "\nSPARC_VERIFY_BEGIN 0\n\nSPARC_VERIFY_END 0\nSPARC_VERIFY_DONE\n"
	if _, err := sink.WriteContext(context.Background(), []byte(output)); err != nil || sink.CloseContext(context.Background()) != nil {
		t.Fatalf("empty archive row rejected: %v", err)
	}
}

func TestPublicSchemaTransformOnlyRewritesTopLevelStatement(t *testing.T) {
	input := "CREATE FUNCTION public.literal() RETURNS text LANGUAGE sql AS $body$\nCREATE SCHEMA public;\n$body$;\nCREATE SCHEMA public;\n"
	want := "CREATE FUNCTION public.literal() RETURNS text LANGUAGE sql AS $body$\nCREATE SCHEMA public;\n$body$;\nCREATE SCHEMA IF NOT EXISTS public;\n"
	destination := &recoveryTestSink{}
	sink := newPublicSchemaTransformSink(destination)
	for offset := 0; offset < len(input); {
		end := offset + 7
		if end > len(input) {
			end = len(input)
		}
		if _, err := sink.WriteContext(context.Background(), []byte(input[offset:end])); err != nil {
			t.Fatalf("schema transform write failed: %v", err)
		}
		offset = end
	}
	if err := sink.CloseContext(context.Background()); err != nil || destination.String() != want {
		t.Fatalf("schema transform failed or did not preserve SQL: %v", err)
	}
}

func TestPublicSchemaTransformFailsClosedWhenStatementIsMissing(t *testing.T) {
	destination := &recoveryTestSink{}
	sink := newPublicSchemaTransformSink(destination)
	if _, err := sink.WriteContext(context.Background(), []byte("CREATE TABLE public.items (id integer);\n")); err != nil {
		t.Fatal(err)
	}
	if err := sink.CloseContext(context.Background()); err != tools.ErrOutput || !destination.aborted {
		t.Fatalf("missing public schema transform = %v, aborted=%t", err, destination.aborted)
	}
}

func TestPublicSchemaTransformRejectsUnterminatedSQL(t *testing.T) {
	destination := &recoveryTestSink{}
	sink := newPublicSchemaTransformSink(destination)
	if _, err := sink.WriteContext(context.Background(), []byte("CREATE SCHEMA public;\nSELECT $body$\n")); err != nil {
		t.Fatal(err)
	}
	if err := sink.CloseContext(context.Background()); err != tools.ErrOutput || !destination.aborted {
		t.Fatalf("unterminated schema SQL = %v, aborted=%t", err, destination.aborted)
	}
}

func TestFingerprintVerificationSQLQuotesIdentifiers(t *testing.T) {
	fingerprints := []RecoveryTableFingerprint{{Schema: "public", Name: "odd table", Columns: []string{`a"b`}, Rows: 0, Fingerprint: strings.Repeat("0", 64)}}
	script, err := buildFingerprintVerificationSQL(fingerprints)
	if err != nil || !strings.Contains(script, `COPY (SELECT "a""b" FROM ONLY "public"."odd table") TO STDOUT;`) {
		t.Fatalf("verification SQL did not quote identifiers: %v", err)
	}
}

func TestParseRecoveryProfileV2FailsClosed(t *testing.T) {
	observation := CatalogObservation{ServerMajor: 17, ServerVersionNum: 170009, Schemas: []SchemaObservation{{Name: "public", Present: true, Owner: "postgres"}}}
	profile := makeRecoveryProfile(observation, "abcdefghijklmnopqrst")
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseRecoveryProfileV2(encoded)
	if err != nil || parsed.SourceProjectRef != "abcdefghijklmnopqrst" || parsed.CaptureStatus != "incomplete" {
		t.Fatalf("valid profile parse = %#v, %v", parsed, err)
	}
	withoutUnknowns := profile
	withoutUnknowns.Unknowns = nil
	missingUnknowns, err := json.Marshal(withoutUnknowns)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string][]byte{
		"unknown field":         append(append([]byte(nil), encoded[:len(encoded)-1]...), []byte(`,"future":true}`)...),
		"duplicate field":       append(append([]byte(nil), encoded[:len(encoded)-1]...), []byte(`,"version":1}`)...),
		"unknown version":       bytes.Replace(encoded, []byte(`"version":2`), []byte(`"version":3`), 1),
		"older profile version": bytes.Replace(encoded, []byte(`"version":2`), []byte(`"version":1`), 1),
		"missing unknown":       missingUnknowns,
		"invalid schema owner":  bytes.Replace(encoded, []byte(`"public_schema_owner":"postgres"`), []byte(`"public_schema_owner":""`), 1),
		"complete declaration":  bytes.Replace(encoded, []byte(`"capture_status":"incomplete"`), []byte(`"capture_status":"complete"`), 1),
	}
	for name, malformed := range mutations {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRecoveryProfileV2(malformed); err != ErrRecoveryProfile {
				t.Fatalf("malformed profile error = %v", err)
			}
		})
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
		return CatalogObservation{ServerVersionNum: 170009, ServerMajor: 17, TLS: true, ReadOnly: true, Schemas: []SchemaObservation{{Name: "public", Present: true, Owner: "postgres"}, {Name: "auth", Present: true, Owner: "postgres"}, {Name: "storage", Present: true, Owner: "postgres"}}}, nil
	}
	calls := 0
	ops.run = func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
		calls++
		if request.Mode == tools.ModeDumpSchema {
			_, _ = request.Stdout.WriteContext(ctx, []byte("CREATE SCHEMA public;\nCREATE TABLE public.synthetic_items (id integer);\n"))
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

func TestRestoreSplitRejectsLegacyArchiveBeforeTargetContact(t *testing.T) {
	request := restoreTestRequest(t)
	request.EmptyScope = EmptyTargetScopeV1{RequiredPresent: []string{"public"}}
	ops := recoveryTestOps()
	observed := false
	ops.verify = func(string, string) (archive.Manifest, error) { return testDatabaseDumpManifest([]byte("legacy")), nil }
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		observed = true
		return CatalogObservation{}, nil
	}
	if err := restoreSplitWith(context.Background(), request, ops); err != ErrRestore || observed {
		t.Fatalf("legacy split-restore refusal = %v, target observed=%t", err, observed)
	}
}

func TestRestoreSplitRefusesSameSourceProjectBeforeTargetContact(t *testing.T) {
	request := restoreTestRequest(t)
	request.EmptyScope = EmptyTargetScopeV1{RequiredPresent: []string{"public"}}
	request.Target.ExpectedProjectRef = "abcdefghijklmnopqrst"
	profile, err := json.Marshal(makeRecoveryProfile(CatalogObservation{ServerMajor: 17, ServerVersionNum: 170009, Schemas: []SchemaObservation{{Name: "public", Present: true, Owner: "postgres"}}}, request.Target.ExpectedProjectRef))
	if err != nil {
		t.Fatal(err)
	}
	manifest := splitRecoveryManifestForTest(profile)
	ops := recoveryTestOps()
	ops.verify = func(string, string) (archive.Manifest, error) { return manifest, nil }
	ops.open = func(_ string, component archive.Component, _ string) (io.ReadCloser, error) {
		if component.Key == recoveryProfileKey {
			return io.NopCloser(bytes.NewReader(profile)), nil
		}
		return io.NopCloser(strings.NewReader(strings.Repeat("x", int(component.Length)))), nil
	}
	observed, rooted := false, false
	ops.rootCert = func(ConnectionParams) ([]byte, error) { rooted = true; return []byte("root"), nil }
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		observed = true
		return CatalogObservation{}, nil
	}
	if err := restoreSplitWith(context.Background(), request, ops); err != ErrRestore || observed || rooted {
		t.Fatalf("same-source refusal = %v, target observed=%t root loaded=%t", err, observed, rooted)
	}
}

func TestRestoreSplitRefusesVersion1ProfileBeforeTargetContact(t *testing.T) {
	request := restoreTestRequest(t)
	request.EmptyScope = EmptyTargetScopeV1{RequiredPresent: []string{"public"}}
	profile := makeRecoveryProfile(CatalogObservation{ServerMajor: 17, ServerVersionNum: 170009, Schemas: []SchemaObservation{{Name: "public", Present: true, Owner: "postgres"}}}, "zyxwvutsrqponmlkjihg")
	profile.Version = 1
	profileBytes, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	manifest := splitRecoveryManifestForTest(profileBytes)
	ops := recoveryTestOps()
	ops.verify = func(string, string) (archive.Manifest, error) { return manifest, nil }
	ops.open = func(_ string, component archive.Component, _ string) (io.ReadCloser, error) {
		if component.Key == recoveryProfileKey {
			return io.NopCloser(bytes.NewReader(profileBytes)), nil
		}
		return io.NopCloser(strings.NewReader(strings.Repeat("x", int(component.Length)))), nil
	}
	observed := false
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		observed = true
		return CatalogObservation{}, nil
	}
	if err := restoreSplitWith(context.Background(), request, ops); err != ErrRestore || observed {
		t.Fatalf("version-1 split profile refusal = %v, target observed=%t", err, observed)
	}
}

func TestSplitTargetRequiresCapturedPublicSchemaOwner(t *testing.T) {
	schemas := []SchemaObservation{{Name: "public", Present: true, Owner: "postgres"}, {Name: "auth", Present: true}, {Name: "storage", Present: true}}
	if !splitTargetSchemasPresent(schemas, "postgres") || splitTargetSchemasPresent(schemas, "different_owner") {
		t.Fatal("split target schema-owner check accepted an incompatible baseline")
	}
	schemas[1].Present = false
	if splitTargetSchemasPresent(schemas, "postgres") {
		t.Fatal("split target schema check accepted a missing managed schema")
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

type recoveryTestSink struct {
	bytes.Buffer
	aborted bool
}

func (s *recoveryTestSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.Write(data)
}
func (*recoveryTestSink) CloseContext(context.Context) error { return nil }
func (s *recoveryTestSink) AbortContext(context.Context) error {
	s.aborted = true
	return nil
}

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

func splitRecoveryManifestForTest(profile []byte) archive.Manifest {
	components := []struct {
		key, scope, status string
		data               []byte
	}{
		{schemaSQLKey, schemaSQLScope, "incomplete", []byte("CREATE TABLE public.items (id integer);\n")},
		{dataSQLKey, dataSQLScope, "incomplete", []byte("COPY public.items (id) FROM stdin;\n1\n\\.\n")},
		{recoveryProfileKey, recoveryProfileScope, "complete", profile},
	}
	manifest := archive.Manifest{Format: archive.Format, Version: archive.Version}
	for index, component := range components {
		digest := sha256.Sum256(component.data)
		manifest.Components = append(manifest.Components, archive.Component{
			ID: fmt.Sprintf("%08x", index), Key: component.key, Scope: component.scope, Status: component.status,
			Length: int64(len(component.data)), SHA256: hex.EncodeToString(digest[:]),
		})
	}
	return manifest
}

func testDatabaseDumpManifest(data []byte) archive.Manifest {
	digest := sha256.Sum256(data)
	return archive.Manifest{Format: archive.Format, Version: archive.Version, Components: []archive.Component{{
		ID: "00000000", Key: databaseDumpKey, Scope: databaseDumpScope, Status: "incomplete",
		Length: int64(len(data)), SHA256: hex.EncodeToString(digest[:]),
	}}}
}
