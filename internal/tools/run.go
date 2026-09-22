package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/burggraf/sparc-cli/internal/platform"
)

const (
	operationDirectory = "operations-v1"
	runBufferBytes     = 32 * 1024
	maxCleanupTimeout  = 5 * time.Second
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

// RunRequest permits only a typed tool's version invocation. Structured
// connected invocations are added with the scoped passfile boundary in Task 6.
type RunRequest struct {
	Tool                     Tool
	Version                  bool
	Timeout, CleanupTimeout  time.Duration
	StdoutLimit, StderrLimit uint64
	Stdout                   OutputSink
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

type runOps struct {
	createDir       func(string) error
	removeAll       func(string) error
	random          io.Reader
	openNull        func() (*os.File, error)
	pipe            func() (*os.File, *os.File, error)
	closeFile       func(*os.File) error
	start           func(platform.ProcessSpec) (ownedProcess, error)
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
	}
}

// Run locates only a compiled-in trusted payload. Production inventory is
// currently empty, so it returns ErrPayloadUnavailable until payload approval.
func Run(ctx context.Context, request RunRequest) (RunResult, error) {
	if ctx == nil || !validRunRequest(request) {
		return RunResult{}, ErrRun
	}
	return withRunSink(request, func(sink *runSink) (RunResult, error) {
		target := payloadTarget{OS: runtime.GOOS, Architecture: runtime.GOARCH}
		manifest, err := lookupProductionPayload(request.Tool, supportedPostgreSQLMajor, target)
		if err != nil {
			return RunResult{}, err
		}
		locations, err := platform.NativeLocations()
		if err != nil {
			return RunResult{}, ErrRun
		}
		id, err := packageID(manifest)
		if err != nil {
			return RunResult{}, ErrRun
		}
		return runWithSink(ctx, request, sink, manifest, filepath.Join(locations.CacheDir, payloadCacheDirectory, id), locations.CacheDir, defaultRunOps())
	})
}

func runWith(ctx context.Context, request RunRequest, manifest packageManifest, packagePath, cacheRoot string, ops runOps) (RunResult, error) {
	if ctx == nil || !validRunRequest(request) {
		return RunResult{}, ErrRun
	}
	return withRunSink(request, func(sink *runSink) (RunResult, error) {
		return runWithSink(ctx, request, sink, manifest, packagePath, cacheRoot, ops)
	})
}

func withRunSink(request RunRequest, run func(*runSink) (RunResult, error)) (result RunResult, resultErr error) {
	sink := newRunSink(request.Stdout)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), request.CleanupTimeout)
		defer cancel()
		if sink.close(cleanupCtx) != nil {
			result = RunResult{}
			if resultErr != ErrRun {
				resultErr = ErrOutput
			}
		}
	}()
	return run(sink)
}

func runWithSink(ctx context.Context, request RunRequest, sink *runSink, manifest packageManifest, packagePath, cacheRoot string, ops runOps) (result RunResult, resultErr error) {
	if ops.createDir == nil || ops.removeAll == nil || ops.random == nil || ops.openNull == nil || ops.pipe == nil || ops.closeFile == nil || ops.start == nil {
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
	defer func() {
		if ops.removeAll(operation) != nil {
			result = RunResult{}
			resultErr = ErrRun
		}
	}()
	if ops.prepare != nil && ops.prepare(operation) != nil || runCtx.Err() != nil {
		return RunResult{}, ErrRun
	}
	return executeRun(runCtx, cancel, request, sink, path, operation, ops)
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
	return validTool(request.Tool) && request.Version && request.Timeout > 0 && request.CleanupTimeout > 0 && request.CleanupTimeout <= maxCleanupTimeout && request.StdoutLimit > 0 && request.StderrLimit > 0 && request.Stdout != nil
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

	files   []*runFile
	process ownedProcess

	stdoutResult chan streamResult
	stderrResult chan streamResult
	waitResult   chan processResult
	stdoutDone   bool
	stderrDone   bool
	waitDone     bool
	stdout       streamResult
	stderr       streamResult
	waited       processResult
	workers      sync.WaitGroup
}

func executeRun(ctx context.Context, cancel context.CancelFunc, request RunRequest, sink *runSink, executable, operation string, ops runOps) (result RunResult, resultErr error) {
	run := &runExecution{ctx: ctx, cancel: cancel, request: request, ops: ops, sink: sink, waited: processResult{code: -1}}
	defer func() { result, resultErr = run.finalize(resultErr) }()

	home, temp, config := filepath.Join(operation, "home"), filepath.Join(operation, "tmp"), filepath.Join(operation, "config")
	for _, path := range []string{home, temp, config} {
		if ctx.Err() != nil || ops.createDir(path) != nil {
			return RunResult{}, ErrRun
		}
	}
	stdinFile, err := ops.openNull()
	if stdinFile != nil {
		run.own(stdinFile)
	}
	if err != nil || stdinFile == nil {
		return RunResult{}, ErrRun
	}
	stdin := run.files[len(run.files)-1]
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

	process, err := ops.start(platform.ProcessSpec{
		Path: executable, Args: []string{"--version"}, Env: runEnvironment(home, temp, config), Dir: operation,
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
		run.stderrResult <- pumpDiscard(ctx, stderrRead.file, request.StderrLimit)
	}()

	for !run.stdoutDone || !run.stderrDone || !run.waitDone {
		select {
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
			if value.err != nil || value.code != 0 {
				return RunResult{}, ErrRun
			}
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

	lifecycleErr := false
	// Closing parent pipe ends unblocks pumps even when the child or sink fails.
	for _, file := range r.files {
		if file.close() != nil {
			lifecycleErr = true
		}
	}
	if r.process != nil && r.process.CloseContext(cleanupCtx) != nil {
		lifecycleErr = true
		// The normal cleanup budget must not let an owned waiter or process tree
		// escape. CloseContext is idempotent, so continue fail-stop cleanup after
		// the deadline until ownership resolves.
		_ = r.process.CloseContext(context.Background())
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
	if cause != nil || r.stdout.err != nil || r.stderr.err != nil {
		return RunResult{}, ErrRun
	}
	if r.waited.err != nil || r.waited.code != 0 {
		return RunResult{}, ErrRun
	}
	return RunResult{ExitCode: r.waited.code, StdoutBytes: r.stdout.bytes, StderrBytes: r.stderr.bytes}, nil
}

func (r *runExecution) collect(ctx context.Context) bool {
	for !r.stdoutDone && r.stdoutResult != nil || !r.stderrDone && r.stderrResult != nil || !r.waitDone && r.waitResult != nil {
		select {
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

type runSink struct {
	sink OutputSink
	once sync.Once
	err  error
}

func newRunSink(sink OutputSink) *runSink { return &runSink{sink: sink} }
func (s *runSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	return s.sink.WriteContext(ctx, data)
}
func (s *runSink) CloseContext(ctx context.Context) error { return s.close(ctx) }
func (s *runSink) close(ctx context.Context) error {
	s.once.Do(func() { s.err = s.sink.CloseContext(ctx) })
	return s.err
}

func pumpStdout(ctx context.Context, file *os.File, sink OutputSink, limit uint64) streamResult {
	return pump(ctx, file, limit, func(chunk []byte) (int, error) {
		return sink.WriteContext(ctx, chunk)
	})
}

func pumpDiscard(ctx context.Context, file *os.File, limit uint64) streamResult {
	return pump(ctx, file, limit, func(chunk []byte) (int, error) { return len(chunk), nil })
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

func runEnvironment(home, temp, config string) []string {
	return []string{"HOME=" + home, "TMPDIR=" + temp, "TMP=" + temp, "TEMP=" + temp, "XDG_CONFIG_HOME=" + config, "LANG=C", "LC_ALL=C"}
}
