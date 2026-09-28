package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/platform"
)

const (
	operationDirectory = "operations-v1"
	runBufferBytes     = 32 * 1024
	maxCleanupTimeout  = 5 * time.Second
	maxRootCertBytes   = 1 << 20
	maxStreamBytes     = uint64(128 << 30)
)

var ErrRun = errors.New("tool run failed")
var ErrOutput = errors.New("tool output failed")

// OutputSink receives stdout synchronously. WriteContext and CloseContext must
// stop when ctx is canceled. CleanupTimeout bounds normal cleanup; process
// ownership resolution and internal goroutine joins fail closed past that bound.
type OutputSink interface {
	WriteContext(context.Context, []byte) (int, error)
	CloseContext(context.Context) error
}

// AbortableOutputSink can invalidate a streaming destination when its producer
// fails after writing a prefix.
type AbortableOutputSink interface {
	OutputSink
	AbortContext(context.Context) error
}

// PGConnection is the only accepted connection input for a PostgreSQL client.
type PGConnection struct {
	Host, User, Database string
	Port                 uint16
	Password             []byte
	RootCertPEM          []byte
}

type RunMode uint8

const (
	ModeVersion RunMode = iota + 1
	ModeDump
	ModeRestore
	// ModeRestoreContinue is a destructive, non-atomic trial on a disposable
	// target. It may leave partial writes when pg_restore reports errors.
	ModeRestoreContinue
	// ModeRestoreAuthData is a strict, single-table data-only recovery trial
	// restricted to known Auth tables on a disposable target.
	ModeRestoreAuthData
	// ModeRestoreSQL streams a plain SQL restore script through psql in one
	// transaction and stops at the first error.
	ModeRestoreSQL
	// ModeDumpSchema and ModeDumpData create separate plain-SQL components;
	// both require explicit schema selections.
	ModeDumpSchema
	ModeDumpData
)

// TableRef is a simple schema/table pair used by a typed dump exclusion.
type TableRef struct {
	Schema string
	Name   string
}

// RunRequest accepts only fixed version, dump, or restore operations. It has
// no free-form arguments, executable path, cwd, or environment.
type RunRequest struct {
	Tool                     Tool
	Mode                     RunMode
	Connection               *PGConnection
	testHostAddr             string
	Input                    io.ReadCloser
	InputLimit               uint64 // exact plaintext length and hard stream cap
	RestoreTable             string
	DumpSchemas              []string
	ExcludedTables           []TableRef
	Timeout, CleanupTimeout  time.Duration
	StdoutLimit, StderrLimit uint64
	Stdout                   OutputSink
	// Stderr receives bounded raw child diagnostics, if explicitly requested.
	// It may contain SQL or identifiers; callers must keep it local and private.
	Stderr io.Writer
}

// RunResult is populated only on success, after the process waiter and both
// stream pumps join. Any error returns a zero result, although Stdout may already
// contain the bounded prefix accepted by its sink.
type RunResult struct {
	ExitCode    int
	StdoutBytes uint64
	StderrBytes uint64
}

type ownedProcess interface {
	Wait() (int, error)
	CloseContext(context.Context) error
}

type passfileLifecycle interface {
	Path() string
	Close() error
}

type runOps struct {
	createDir       func(string) error
	removeAll       func(string) error
	random          io.Reader
	openNull        func() (*os.File, error)
	pipe            func() (*os.File, *os.File, error)
	closeFile       func(*os.File) error
	start           func(platform.ProcessSpec) (ownedProcess, error)
	newPassfile     func(string, credentials.PGPassEntry) (passfileLifecycle, error)
	validate        func(context.Context, Tool, packageManifest, string, string) (string, error) // test-only validation seam
	prepare         func(string) error                                                           // test-only per-call operation setup
	afterWaitResult func()                                                                       // test-only post-send gate
}

func defaultRunOps() runOps {
	return runOps{
		createDir: platform.CreatePrivateDir,
		removeAll: os.RemoveAll,
		random:    rand.Reader,
		openNull:  func() (*os.File, error) { return os.Open(os.DevNull) },
		pipe:      os.Pipe,
		closeFile: (*os.File).Close,
		start: func(spec platform.ProcessSpec) (ownedProcess, error) {
			return platform.StartProcess(spec)
		},
		newPassfile: func(directory string, entry credentials.PGPassEntry) (passfileLifecycle, error) {
			return credentials.NewPGPassfile(directory, entry)
		},
	}
}

// PrepareProductionPayload privately extracts and validates the compiled-in tool
// package before any source connection is attempted.
func PrepareProductionPayload(ctx context.Context, tool Tool) error {
	_, _, _, err := prepareProductionPayload(ctx, tool)
	return err
}

func prepareProductionPayload(ctx context.Context, tool Tool) (packageManifest, string, string, error) {
	if ctx == nil {
		return packageManifest{}, "", "", ErrRun
	}
	target := payloadTarget{OS: runtime.GOOS, Architecture: runtime.GOARCH}
	manifest, err := lookupProductionPayload(tool, supportedPostgreSQLMajor, target)
	if err != nil {
		return packageManifest{}, "", "", err
	}
	payload, ok := productionPayloadArchives[target]
	if !ok {
		return packageManifest{}, "", "", ErrInvalidPayload
	}
	locations, err := platform.NativeLocations()
	if err != nil || ensurePrivateCacheRoot(locations.CacheDir) != nil {
		return packageManifest{}, "", "", ErrRun
	}
	packagePath, err := preparePayload(ctx, locations.CacheDir, bytes.NewReader(payload), manifest)
	if err != nil {
		return packageManifest{}, "", "", ErrRun
	}
	return manifest, packagePath, locations.CacheDir, nil
}

func ensurePrivateCacheRoot(path string) error {
	if platform.CheckPrivateDir(path) == nil {
		return nil
	}
	if platform.CreatePrivateDir(path) == nil || platform.CheckPrivateDir(path) == nil {
		return nil
	}
	return ErrRun
}

// Run executes a typed operation using only a compiled-in trusted payload.
func Run(ctx context.Context, request RunRequest) (RunResult, error) {
	if ctx == nil || request.testHostAddr != "" || !validRunRequest(request) {
		return RunResult{}, ErrRun
	}
	return withRunSink(request, func(request RunRequest, sink *runSink) (RunResult, error) {
		manifest, packagePath, cacheRoot, err := prepareProductionPayload(ctx, request.Tool)
		if err != nil {
			return RunResult{}, err
		}
		return runWithSink(ctx, request, sink, manifest, packagePath, cacheRoot, defaultRunOps())
	})
}

func runWith(ctx context.Context, request RunRequest, manifest packageManifest, packagePath, cacheRoot string, ops runOps) (RunResult, error) {
	if ctx == nil || !validRunRequest(request) {
		return RunResult{}, ErrRun
	}
	return withRunSink(request, func(request RunRequest, sink *runSink) (RunResult, error) {
		return runWithSink(ctx, request, sink, manifest, packagePath, cacheRoot, ops)
	})
}

func withRunSink(request RunRequest, run func(RunRequest, *runSink) (RunResult, error)) (result RunResult, resultErr error) {
	if request.Mode == ModeRestore || request.Mode == ModeRestoreContinue || request.Mode == ModeRestoreAuthData || request.Mode == ModeRestoreSQL {
		request.Input = &runInput{ReadCloser: request.Input}
	}
	sink := newRunSink(request.Stdout)
	defer func() {
		if request.Input != nil && request.Input.Close() != nil {
			result = RunResult{}
			if resultErr != ErrOutput {
				resultErr = ErrRun
			}
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), request.CleanupTimeout)
		defer cancel()
		if sink.finish(cleanupCtx, resultErr) != nil {
			result = RunResult{}
			if resultErr != ErrRun {
				resultErr = ErrOutput
			}
		}
		if sink.postClose != nil && sink.postClose() != nil {
			result = RunResult{}
			resultErr = ErrRun
		}
	}()
	return run(request, sink)
}

func runWithSink(ctx context.Context, request RunRequest, sink *runSink, manifest packageManifest, packagePath, cacheRoot string, ops runOps) (result RunResult, resultErr error) {
	if ops.createDir == nil || ops.removeAll == nil || ops.random == nil || ops.openNull == nil || ops.pipe == nil || ops.closeFile == nil || ops.start == nil || ops.newPassfile == nil {
		return RunResult{}, ErrRun
	}
	runCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	validate := validateRunInputs
	if ops.validate != nil {
		validate = ops.validate
	}
	path, err := validate(runCtx, request.Tool, manifest, packagePath, cacheRoot)
	if err != nil || runCtx.Err() != nil {
		return RunResult{}, ErrRun
	}
	operation, err := createOperationDirectory(runCtx, cacheRoot, ops)
	if err != nil {
		return RunResult{}, ErrRun
	}
	var passfile passfileLifecycle
	sink.postClose = func() error {
		failed := passfile != nil && passfile.Close() != nil
		if ops.removeAll(operation) != nil {
			failed = true
		}
		if failed {
			return ErrRun
		}
		return nil
	}
	if ops.prepare != nil && ops.prepare(operation) != nil || runCtx.Err() != nil {
		return RunResult{}, ErrRun
	}
	rootCertPath := ""
	if request.Connection != nil {
		passfile, err = ops.newPassfile(operation, credentials.PGPassEntry{Host: request.Connection.Host, Port: request.Connection.Port, Database: request.Connection.Database, User: request.Connection.User, Password: request.Connection.Password})
		if err != nil {
			return RunResult{}, ErrRun
		}
		rootCertPath = filepath.Join(operation, "root.crt")
		if platform.WritePrivateFile(rootCertPath, request.Connection.RootCertPEM) != nil {
			return RunResult{}, ErrRun
		}
	}
	return executeRun(runCtx, cancel, request, sink, path, operation, passfilePath(passfile), rootCertPath, ops)
}

func validateRunInputs(ctx context.Context, tool Tool, manifest packageManifest, packagePath, cacheRoot string) (string, error) {
	if ctx.Err() != nil || validateManifest(manifest) != nil || platform.CheckPrivateDir(cacheRoot) != nil || ctx.Err() != nil || validatePayloadPackage(ctx, packagePath, manifest) != nil {
		return "", ErrRun
	}
	executable, ok := manifest.Executables[tool]
	if !ok {
		return "", ErrRun
	}
	path := filepath.Join(packagePath, filepath.FromSlash(executable))
	if validatePayloadFile(ctx, path, manifestFile(manifest, executable), manifest.Target) != nil || ctx.Err() != nil {
		return "", ErrRun
	}
	return path, nil
}

func validRunRequest(request RunRequest) bool {
	if !validTool(request.Tool) || request.Timeout <= 0 || request.CleanupTimeout <= 0 || request.CleanupTimeout > maxCleanupTimeout || request.StdoutLimit == 0 || request.StderrLimit == 0 || request.Stdout == nil || request.Mode != ModeRestoreAuthData && request.RestoreTable != "" || request.Mode != ModeDumpSchema && request.Mode != ModeDumpData && (len(request.DumpSchemas) != 0 || len(request.ExcludedTables) != 0) {
		return false
	}
	switch request.Mode {
	case ModeVersion:
		return request.Connection == nil && request.Input == nil && request.InputLimit == 0
	case ModeDump:
		if request.Tool != PGDump || request.Connection == nil || request.Input != nil || request.InputLimit != 0 {
			return false
		}
	case ModeDumpSchema:
		if request.Tool != PGDump || request.Connection == nil || request.Input != nil || request.InputLimit != 0 || !validDumpSchemas(request.DumpSchemas, false) || len(request.ExcludedTables) != 0 {
			return false
		}
	case ModeDumpData:
		if request.Tool != PGDump || request.Connection == nil || request.Input != nil || request.InputLimit != 0 || !validDumpSchemas(request.DumpSchemas, true) || !validDataExclusions(request.ExcludedTables) {
			return false
		}
	case ModeRestore, ModeRestoreContinue, ModeRestoreAuthData:
		if request.Tool != PGRestore || request.Connection == nil || request.Input == nil || request.InputLimit == 0 || request.InputLimit > maxStreamBytes || request.Mode == ModeRestoreAuthData && !allowedAuthDataTable(request.RestoreTable) {
			return false
		}
	case ModeRestoreSQL:
		if request.Tool != PSQL || request.Connection == nil || request.Input == nil || request.InputLimit == 0 || request.InputLimit > maxStreamBytes {
			return false
		}
	default:
		return false
	}
	return (request.testHostAddr == "" || request.testHostAddr == "127.0.0.1") &&
		len(request.Connection.RootCertPEM) > 0 && len(request.Connection.RootCertPEM) <= maxRootCertBytes && validRootCertPEM(request.Connection.RootCertPEM) &&
		credentials.ValidatePGPassEntry(credentials.PGPassEntry{Host: request.Connection.Host, Port: request.Connection.Port, Database: request.Connection.Database, User: request.Connection.User, Password: request.Connection.Password}) == nil
}

func validRootCertPEM(data []byte) bool {
	count := 0
	for {
		data = bytes.TrimSpace(data)
		if len(data) == 0 {
			return count > 0
		}
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return false
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !certificate.BasicConstraintsValid || !certificate.IsCA {
			return false
		}
		data, count = rest, count+1
	}
}

func manifestFile(manifest packageManifest, path string) payloadFile {
	for _, file := range manifest.Files {
		if file.Path == path {
			return file
		}
	}
	return payloadFile{}
}

func createOperationDirectory(ctx context.Context, cacheRoot string, ops runOps) (string, error) {
	if ctx.Err() != nil {
		return "", ErrRun
	}
	parent := filepath.Join(cacheRoot, operationDirectory)
	if err := ensurePrivateDirectory(parent, ops.createDir); err != nil || ctx.Err() != nil {
		return "", ErrRun
	}
	var randomID [16]byte
	for range 16 {
		if _, err := io.ReadFull(&contextReader{ctx: ctx, reader: ops.random}, randomID[:]); err != nil {
			return "", ErrRun
		}
		path := filepath.Join(parent, ".operation-"+hex.EncodeToString(randomID[:]))
		if err := ops.createDir(path); err == nil {
			if ctx.Err() != nil {
				_ = ops.removeAll(path)
				return "", ErrRun
			}
			return path, nil
		}
		if _, err := os.Lstat(path); err != nil && !os.IsNotExist(err) {
			return "", ErrRun
		}
	}
	return "", ErrRun
}

type runExecution struct {
	ctx     context.Context
	cancel  context.CancelFunc
	request RunRequest
	ops     runOps
	sink    *runSink

	files              []*runFile
	process            ownedProcess
	processClosed      bool
	processCloseFailed bool

	inputResult  chan streamResult
	stdoutResult chan streamResult
	stderrResult chan streamResult
	waitResult   chan processResult
	inputDone    bool
	stdoutDone   bool
	stderrDone   bool
	waitDone     bool
	input        streamResult
	stdout       streamResult
	stderr       streamResult
	waited       processResult
	workers      sync.WaitGroup
}

func executeRun(ctx context.Context, cancel context.CancelFunc, request RunRequest, sink *runSink, executable, operation, passfile, rootCertPath string, ops runOps) (result RunResult, resultErr error) {
	run := &runExecution{ctx: ctx, cancel: cancel, request: request, ops: ops, sink: sink, inputDone: request.Input == nil, waited: processResult{code: -1}}
	defer func() { result, resultErr = run.finalize(resultErr) }()

	home, temp, config := filepath.Join(operation, "home"), filepath.Join(operation, "tmp"), filepath.Join(operation, "config")
	for _, path := range []string{home, temp, config} {
		if ctx.Err() != nil || ops.createDir(path) != nil {
			return RunResult{}, ErrRun
		}
	}
	var stdin, stdinWrite *runFile
	var err error
	if request.Mode == ModeRestore || request.Mode == ModeRestoreContinue || request.Mode == ModeRestoreAuthData || request.Mode == ModeRestoreSQL {
		reader, writer, pipeErr := ops.pipe()
		if reader != nil {
			stdin = run.own(reader)
		}
		if writer != nil {
			stdinWrite = run.own(writer)
		}
		if pipeErr != nil || stdin == nil || stdinWrite == nil {
			return RunResult{}, ErrRun
		}
	} else {
		stdinFile, openErr := ops.openNull()
		if stdinFile != nil {
			stdin = run.own(stdinFile)
		}
		if openErr != nil || stdin == nil {
			return RunResult{}, ErrRun
		}
	}
	stdoutReadFile, stdoutWriteFile, err := ops.pipe()
	if stdoutReadFile != nil {
		run.own(stdoutReadFile)
	}
	if stdoutWriteFile != nil {
		run.own(stdoutWriteFile)
	}
	if err != nil || stdoutReadFile == nil || stdoutWriteFile == nil {
		return RunResult{}, ErrRun
	}
	stdoutRead, stdoutWrite := run.files[len(run.files)-2], run.files[len(run.files)-1]
	stderrReadFile, stderrWriteFile, err := ops.pipe()
	if stderrReadFile != nil {
		run.own(stderrReadFile)
	}
	if stderrWriteFile != nil {
		run.own(stderrWriteFile)
	}
	if err != nil || stderrReadFile == nil || stderrWriteFile == nil {
		return RunResult{}, ErrRun
	}
	stderrRead, stderrWrite := run.files[len(run.files)-2], run.files[len(run.files)-1]
	if ctx.Err() != nil {
		return RunResult{}, ErrRun
	}

	arguments, err := runArguments(request)
	if err != nil {
		return RunResult{}, ErrRun
	}
	if stdinWrite != nil {
		run.inputResult = make(chan streamResult, 1)
		run.workers.Add(1)
		go func() {
			defer run.workers.Done()
			pump := pumpInput
			if request.Mode == ModeRestoreAuthData {
				pump = pumpInputForSelected
			}
			result := pump(ctx, request.Input, stdinWrite.file, request.InputLimit)
			if result.err == nil && stdinWrite.close() != nil {
				result.err = ErrRun
			}
			run.inputResult <- result
		}()
	}
	process, err := ops.start(platform.ProcessSpec{
		Path: executable, Args: arguments, Env: runEnvironment(home, temp, config, passfile, rootCertPath, request.testHostAddr), Dir: operation,
		Stdin: stdin.file, Stdout: stdoutWrite.file, Stderr: stderrWrite.file,
	})
	if process != nil {
		run.process = process
	}
	if err != nil || process == nil || ctx.Err() != nil {
		return RunResult{}, ErrRun
	}
	run.waitResult = make(chan processResult, 1)
	run.workers.Add(1)
	go func() {
		defer run.workers.Done()
		code, waitErr := process.Wait()
		run.waitResult <- processResult{code: code, err: waitErr}
		if ops.afterWaitResult != nil {
			ops.afterWaitResult()
		}
	}()

	// The child owns only its inherited/duplicated standard handles. Closing all
	// parent copies here is required for deterministic EOF and no handle leakage.
	if stdin.close() != nil || stdoutWrite.close() != nil || stderrWrite.close() != nil {
		return RunResult{}, ErrRun
	}

	run.stdoutResult = make(chan streamResult, 1)
	run.stderrResult = make(chan streamResult, 1)
	run.workers.Add(2)
	go func() {
		defer run.workers.Done()
		run.stdoutResult <- pumpStdout(ctx, stdoutRead.file, run.sink, request.StdoutLimit)
	}()
	go func() {
		defer run.workers.Done()
		if request.Stderr != nil {
			run.stderrResult <- pumpStderr(ctx, stderrRead.file, request.Stderr, request.StderrLimit)
		} else {
			run.stderrResult <- pumpDiscard(ctx, stderrRead.file, request.StderrLimit)
		}
	}()

	for !run.inputDone || !run.stdoutDone || !run.stderrDone || !run.waitDone {
		select {
		case value := <-run.inputResult:
			run.input, run.inputDone = value, true
			run.inputResult = nil
			if value.err != nil {
				// Kill before closing stdin so psql cannot mistake a bad stream for EOF;
				// leave stdout/stderr pumps running to retain bounded diagnostics.
				abortCtx, abortCancel := context.WithTimeout(context.Background(), request.CleanupTimeout)
				run.closeProcess(abortCtx)
				abortCancel()
				_ = stdinWrite.close()
			}
			// A child may reject the first SQL statement and close stdin while
			// writing its error to stderr. Let the output pumps drain first.
		case value := <-run.stdoutResult:
			run.stdout, run.stdoutDone = value, true
			run.stdoutResult = nil
			if value.err != nil {
				return RunResult{}, value.err
			}
		case value := <-run.stderrResult:
			run.stderr, run.stderrDone = value, true
			run.stderrResult = nil
			if value.err != nil {
				return RunResult{}, value.err
			}
		case value := <-run.waitResult:
			run.waited, run.waitDone = value, true
			run.waitResult = nil
			if request.Mode == ModeRestoreSQL && request.Input != nil && !run.inputDone && (value.err != nil || value.code != 0) {
				// A failed transactional psql child may leave a gated input reader
				// waiting for verification output that will never arrive.
				_ = request.Input.Close()
			}
			// Drain stderr after a nonzero exit so the optional diagnostic sink
			// gets the actual failure before finalize reports ErrRun.
		case <-ctx.Done():
			return RunResult{}, ErrRun
		}
	}
	return RunResult{}, nil
}

func (r *runExecution) own(file *os.File) *runFile {
	owned := newRunFile(file, r.ops.closeFile)
	r.files = append(r.files, owned)
	return owned
}

func (r *runExecution) finalize(cause error) (RunResult, error) {
	operationCanceled := r.ctx.Err() != nil
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), r.request.CleanupTimeout)
	defer cleanupCancel()
	r.cancel()

	lifecycleErr := r.processCloseFailed
	// On failure, terminate the child before closing its stdin pipe: a transactional
	// SQL client must not interpret a failed input stream as successful EOF.
	if cause != nil && r.request.Input != nil && r.process != nil {
		r.closeProcess(cleanupCtx)
		lifecycleErr = lifecycleErr || r.processCloseFailed
	}
	// Closing parent pipe ends unblocks pumps even when the child or sink fails.
	for _, file := range r.files {
		if file.close() != nil {
			lifecycleErr = true
		}
	}
	if r.request.Input != nil && r.request.Input.Close() != nil {
		lifecycleErr = true
	}
	if r.process != nil && !r.processClosed {
		r.closeProcess(cleanupCtx)
		lifecycleErr = lifecycleErr || r.processCloseFailed
	}
	if !r.collect(cleanupCtx) {
		lifecycleErr = true
		_ = r.collect(context.Background())
	}
	// Result collection precedes the explicit join: a buffered send can complete
	// before its wrapper's deferred cleanup has finished.
	r.workers.Wait()
	if lifecycleErr || operationCanceled {
		return RunResult{}, ErrRun
	}
	if cause == ErrOutput || r.stdout.err == ErrOutput || r.stderr.err == ErrOutput {
		return RunResult{}, ErrOutput
	}
	if cause != nil || r.input.err != nil || r.stdout.err != nil || r.stderr.err != nil {
		return RunResult{}, ErrRun
	}
	if r.waited.err != nil || r.waited.code != 0 {
		return RunResult{}, ErrRun
	}
	return RunResult{ExitCode: r.waited.code, StdoutBytes: r.stdout.bytes, StderrBytes: r.stderr.bytes}, nil
}

func (r *runExecution) closeProcess(ctx context.Context) {
	if r.process == nil || r.processClosed {
		return
	}
	r.processClosed = true
	if r.process.CloseContext(ctx) != nil {
		r.processCloseFailed = true
		_ = r.process.CloseContext(context.Background())
	}
}

func (r *runExecution) collect(ctx context.Context) bool {
	for !r.inputDone && r.inputResult != nil || !r.stdoutDone && r.stdoutResult != nil || !r.stderrDone && r.stderrResult != nil || !r.waitDone && r.waitResult != nil {
		select {
		case value := <-r.inputResult:
			r.input, r.inputDone, r.inputResult = value, true, nil
		case value := <-r.stdoutResult:
			r.stdout, r.stdoutDone, r.stdoutResult = value, true, nil
		case value := <-r.stderrResult:
			r.stderr, r.stderrDone, r.stderrResult = value, true, nil
		case value := <-r.waitResult:
			r.waited, r.waitDone, r.waitResult = value, true, nil
		case <-ctx.Done():
			return false
		}
	}
	return true
}

type streamResult struct {
	bytes uint64
	err   error
}

type processResult struct {
	code int
	err  error
}

type runFile struct {
	file      *os.File
	closeFile func(*os.File) error
	once      sync.Once
	err       error
}

func newRunFile(file *os.File, closeFile func(*os.File) error) *runFile {
	return &runFile{file: file, closeFile: closeFile}
}

func (f *runFile) close() error {
	f.once.Do(func() {
		if f.file != nil {
			f.err = f.closeFile(f.file)
		}
	})
	return f.err
}

type runInput struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (r *runInput) Close() error {
	r.once.Do(func() { r.err = r.ReadCloser.Close() })
	return r.err
}

type runSink struct {
	sink      OutputSink
	postClose func() error
	once      sync.Once
	err       error
}

func newRunSink(sink OutputSink) *runSink { return &runSink{sink: sink} }
func (s *runSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	return s.sink.WriteContext(ctx, data)
}
func (s *runSink) CloseContext(ctx context.Context) error { return s.finish(ctx, nil) }
func (s *runSink) finish(ctx context.Context, cause error) error {
	s.once.Do(func() {
		if cause != nil {
			if aborter, ok := s.sink.(AbortableOutputSink); ok {
				s.err = aborter.AbortContext(ctx)
				return
			}
		}
		s.err = s.sink.CloseContext(ctx)
	})
	return s.err
}

func pumpStdout(ctx context.Context, file *os.File, sink OutputSink, limit uint64) streamResult {
	return pump(ctx, file, limit, func(chunk []byte) (int, error) {
		return sink.WriteContext(ctx, chunk)
	})
}

func pumpStderr(ctx context.Context, file *os.File, destination io.Writer, limit uint64) streamResult {
	return pump(ctx, file, limit, destination.Write)
}

func pumpDiscard(ctx context.Context, file *os.File, limit uint64) streamResult {
	return pump(ctx, file, limit, func(chunk []byte) (int, error) { return len(chunk), nil })
}

func pumpInput(ctx context.Context, source io.Reader, destination io.Writer, limit uint64) streamResult {
	return pumpInputWithEarlyClose(ctx, source, destination, limit, false)
}

func pumpInputForSelected(ctx context.Context, source io.Reader, destination io.Writer, limit uint64) streamResult {
	return pumpInputWithEarlyClose(ctx, source, destination, limit, true)
}

func pumpInputWithEarlyClose(ctx context.Context, source io.Reader, destination io.Writer, limit uint64, allowEarlyClose bool) streamResult {
	buffer := make([]byte, runBufferBytes)
	var total uint64
	zeroReads := 0
	childClosed := false
	for {
		if ctx.Err() != nil {
			return streamResult{bytes: total, err: ErrRun}
		}
		remaining := limit - total
		readSize := len(buffer)
		if remaining < uint64(readSize) {
			readSize = int(remaining) + 1
		}
		n, readErr := source.Read(buffer[:readSize])
		if n < 0 || n > readSize {
			return streamResult{bytes: total, err: ErrRun}
		}
		if n > 0 {
			allowed := n
			excess := uint64(n) > remaining
			if excess {
				allowed = int(remaining)
			}
			total += uint64(allowed)
			if !childClosed {
				for written := 0; written < allowed; {
					if ctx.Err() != nil {
						return streamResult{bytes: total, err: ErrRun}
					}
					count, writeErr := destination.Write(buffer[written:allowed])
					if count < 0 || count > allowed-written {
						return streamResult{bytes: total, err: ErrRun}
					}
					written += count
					if allowEarlyClose && errors.Is(writeErr, syscall.EPIPE) {
						// pg_restore may stop reading a verified custom archive once
						// the selected table is complete. Still drain the source to
						// catch decryption errors and enforce InputLimit.
						childClosed = true
						break
					}
					if writeErr != nil || count == 0 {
						return streamResult{bytes: total, err: ErrRun}
					}
				}
			}
			if excess {
				return streamResult{bytes: total, err: ErrRun}
			}
			zeroReads = 0
		}
		if readErr == io.EOF {
			if total != limit {
				return streamResult{bytes: total, err: ErrRun}
			}
			return streamResult{bytes: total}
		}
		if readErr != nil {
			return streamResult{bytes: total, err: ErrRun}
		}
		if n == 0 {
			zeroReads++
			if zeroReads >= 100 {
				return streamResult{bytes: total, err: ErrRun}
			}
		} else {
			zeroReads = 0
		}
	}
}

func pump(ctx context.Context, file *os.File, limit uint64, consume func([]byte) (int, error)) streamResult {
	buffer := make([]byte, runBufferBytes)
	var total uint64
	for {
		if ctx.Err() != nil {
			return streamResult{bytes: total, err: ErrRun}
		}
		remaining := limit - total
		readBytes := len(buffer)
		if remaining < uint64(readBytes) {
			readBytes = int(remaining) + 1
		}
		n, readErr := file.Read(buffer[:readBytes])
		if n > 0 {
			allowed := n
			excess := uint64(n) > remaining
			if excess {
				allowed = int(remaining)
			}
			if allowed > 0 {
				consumed, consumeErr := consume(buffer[:allowed])
				if consumed < 0 || consumed > allowed {
					return streamResult{bytes: total, err: ErrOutput}
				}
				total += uint64(consumed)
				if consumeErr != nil || consumed != allowed {
					return streamResult{bytes: total, err: ErrOutput}
				}
			}
			if excess {
				return streamResult{bytes: total, err: ErrOutput}
			}
		}
		if readErr == io.EOF {
			return streamResult{bytes: total}
		}
		if readErr != nil {
			return streamResult{bytes: total, err: ErrRun}
		}
		if n == 0 {
			return streamResult{bytes: total, err: ErrRun}
		}
	}
}

func passfilePath(passfile passfileLifecycle) string {
	if passfile == nil {
		return ""
	}
	return passfile.Path()
}

func runArguments(request RunRequest) ([]string, error) {
	if request.Mode == ModeVersion {
		return []string{"--version"}, nil
	}
	if request.Connection == nil {
		return nil, ErrRun
	}
	connection := request.Connection
	common := []string{"--no-password", "--host=" + connection.Host, "--port=" + strconv.FormatUint(uint64(connection.Port), 10), "--username=" + connection.User, "--dbname=" + connection.Database}
	switch request.Mode {
	case ModeDump:
		return append([]string{"--format=custom"}, common...), nil
	case ModeDumpSchema:
		args := append([]string{"--format=plain", "--schema-only"}, common...)
		for _, schema := range request.DumpSchemas {
			args = append(args, "--schema="+schema)
		}
		return args, nil
	case ModeDumpData:
		args := append([]string{"--format=plain", "--data-only"}, common...)
		for _, schema := range request.DumpSchemas {
			args = append(args, "--schema="+schema)
		}
		for _, table := range request.ExcludedTables {
			args = append(args, "--exclude-table="+table.Schema+"."+table.Name)
		}
		return args, nil
	case ModeRestore:
		return append([]string{"--single-transaction", "--exit-on-error"}, common...), nil
	case ModeRestoreContinue:
		return common, nil
	case ModeRestoreAuthData:
		if !allowedAuthDataTable(request.RestoreTable) {
			return nil, ErrRun
		}
		return append([]string{"--single-transaction", "--exit-on-error", "--data-only", "--schema=auth", "--table=" + request.RestoreTable}, common...), nil
	case ModeRestoreSQL:
		return append([]string{"-X", "-q", "-t", "--single-transaction", "--set=ON_ERROR_STOP=1"}, common...), nil
	default:
		return nil, ErrRun
	}
}

func allowedAuthDataTable(name string) bool {
	switch name {
	case "sessions", "identities", "refresh_tokens", "mfa_amr_claims", "one_time_tokens":
		return true
	default:
		return false
	}
}

func validDumpSchemas(schemas []string, allowManagedData bool) bool {
	if len(schemas) == 0 || len(schemas) > 64 {
		return false
	}
	seen := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		if !simplePGIdentifier(schema) || blockedDumpSchema(strings.ToLower(schema), allowManagedData) {
			return false
		}
		if _, exists := seen[schema]; exists {
			return false
		}
		seen[schema] = struct{}{}
	}
	return true
}

func validDataExclusions(tables []TableRef) bool {
	if !validExcludedTables(tables) {
		return false
	}
	for _, required := range [...]TableRef{
		{Schema: "auth", Name: "schema_migrations"},
		{Schema: "storage", Name: "migrations"},
		{Schema: "supabase_functions", Name: "migrations"},
		{Schema: "storage", Name: "buckets_vectors"},
		{Schema: "storage", Name: "vector_indexes"},
	} {
		found := false
		for _, actual := range tables {
			if actual == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func validExcludedTables(tables []TableRef) bool {
	if len(tables) > 1024 {
		return false
	}
	seen := make(map[TableRef]struct{}, len(tables))
	for _, table := range tables {
		if !simplePGIdentifier(table.Schema) || !simplePGIdentifier(table.Name) {
			return false
		}
		if _, exists := seen[table]; exists {
			return false
		}
		seen[table] = struct{}{}
	}
	return true
}

func blockedDumpSchema(schema string, allowManagedData bool) bool {
	if allowManagedData && (schema == "auth" || schema == "storage") {
		return false
	}
	switch schema {
	case "information_schema", "_analytics", "_realtime", "_supavisor", "auth", "etl", "extensions", "pgbouncer", "realtime", "storage", "supabase_functions", "supabase_migrations", "cron", "dbdev", "graphql", "graphql_public", "net", "pgmq", "pgsodium", "pgsodium_masks", "pgtle", "repack", "tiger", "tiger_data", "topology", "vault":
		return true
	}
	return strings.HasPrefix(schema, "pg_") || strings.HasPrefix(schema, "timescaledb_") || strings.HasPrefix(schema, "_timescaledb_")
}

func simplePGIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 63 || !(value[0] == '_' || value[0] >= 'a' && value[0] <= 'z' || value[0] >= 'A' && value[0] <= 'Z') {
		return false
	}
	for index := 1; index < len(value); index++ {
		c := value[index]
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func runEnvironment(home, temp, config, passfile, rootCert, testHostAddr string) []string {
	environment := []string{"HOME=" + home, "TMPDIR=" + temp, "TMP=" + temp, "TEMP=" + temp, "XDG_CONFIG_HOME=" + config, "LANG=C", "LC_ALL=C"}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		environment = append(environment, "OPENSSL_CONF=/dev/null", "OPENSSL_MODULES="+config)
	}
	if passfile != "" {
		environment = append(environment, "PGPASSFILE="+passfile, "PGSSLMODE=verify-full", "PGSSLROOTCERT="+rootCert)
	}
	if testHostAddr != "" {
		environment = append(environment, "PGHOSTADDR="+testHostAddr)
	}
	return environment
}
