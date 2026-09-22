package tools

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/burggraf/sparc-cli/internal/platform"
)

const runTestTimeout = 10 * time.Second

var preparedRunFixture struct {
	once                             sync.Once
	manifest                         packageManifest
	packagePath, cacheRoot, tempRoot string
}

func TestRunUsesOnlyTypedVersionAndIsolatedProcessSpec(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	t.Setenv("PATH", "secret-path")
	t.Setenv("PGPASSWORD", "secret-password")
	t.Setenv("DYLD_INSERT_LIBRARIES", "secret-loader")
	t.Setenv("HTTPS_PROXY", "secret-proxy")
	t.Setenv("SystemRoot", "secret-system-root")
	t.Setenv("HOME", "secret-home")

	sink := &memorySink{}
	var got platform.ProcessSpec
	ops := runTestOps(func(spec platform.ProcessSpec) (ownedProcess, error) {
		got = spec
		if _, err := spec.Stdout.Write([]byte("version\n")); err != nil {
			return nil, err
		}
		if _, err := spec.Stderr.Write([]byte("warning\n")); err != nil {
			return nil, err
		}
		return completedProcess(0), nil
	})
	result, err := runWith(context.Background(), validRunRequestForTest(sink), manifest, packagePath, cacheRoot, ops)
	if err != nil {
		t.Fatal(err)
	}
	if result != (RunResult{ExitCode: 0, StdoutBytes: 8, StderrBytes: 8}) || sink.String() != "version\n" || sink.closeCount() != 1 {
		t.Fatalf("result/sink = %#v, %q, closes=%d", result, sink.String(), sink.closeCount())
	}
	if got.Path == "" || got.Dir == "" || strings.Join(got.Args, "\x00") != "--version" || got.Stdin == nil || got.Stdout == nil || got.Stderr == nil {
		t.Fatalf("unexpected spec: %#v", got)
	}
	for _, forbidden := range []string{"PATH=", "PG", "DYLD", "PROXY", "SYSTEMROOT=", "secret-"} {
		for _, entry := range got.Env {
			if strings.HasPrefix(entry, forbidden) || strings.Contains(entry, "secret-") {
				t.Fatalf("ambient value leaked into env: %q", entry)
			}
		}
	}
	for _, required := range []string{"HOME=", "TMPDIR=", "XDG_CONFIG_HOME=", "LANG=C", "LC_ALL=C"} {
		if !containsEnvironment(got.Env, required) {
			t.Fatalf("missing %q in %#v", required, got.Env)
		}
	}
	if !strings.HasPrefix(got.Path, packagePath+string(filepath.Separator)) || got.Dir == packagePath || !strings.HasPrefix(got.Dir, filepath.Join(cacheRoot, operationDirectory)+string(filepath.Separator)) {
		t.Fatalf("untrusted path selection: path=%q dir=%q", got.Path, got.Dir)
	}
}

func TestRunRejectsInvalidRequestsAndPrestartCancellation(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	for _, request := range []RunRequest{
		{},
		{Tool: PSQL, Version: true, Timeout: time.Second, CleanupTimeout: time.Second, StdoutLimit: 1, StderrLimit: 1},
		{Tool: PSQL, Version: true, Timeout: 0, CleanupTimeout: time.Second, StdoutLimit: 1, StderrLimit: 1, Stdout: &memorySink{}},
		{Tool: PSQL, Version: true, Timeout: time.Second, CleanupTimeout: maxCleanupTimeout + time.Nanosecond, StdoutLimit: 1, StderrLimit: 1, Stdout: &memorySink{}},
	} {
		if _, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, runTestOps(nil)); err != ErrRun {
			t.Fatalf("invalid request error = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	ops := runTestOps(func(platform.ProcessSpec) (ownedProcess, error) { called = true; return completedProcess(0), nil })
	cancelledSink := &memorySink{closeErr: errors.New("secret-canary")}
	if _, err := runWith(ctx, validRunRequestForTest(cancelledSink), manifest, packagePath, cacheRoot, ops); err != ErrRun || called || cancelledSink.closeCount() != 1 {
		t.Fatalf("pre-start cancellation = %v, called=%v, closes=%d", err, called, cancelledSink.closeCount())
	}
	t.Setenv("HOME", "hostile-relative-home")
	emptyInventorySink := &memorySink{}
	if _, err := Run(context.Background(), validRunRequestForTest(emptyInventorySink)); err != ErrPayloadUnavailable || emptyInventorySink.closeCount() != 1 {
		t.Fatalf("production empty inventory = %v, closes=%d", err, emptyInventorySink.closeCount())
	}
	ops = runTestOps(func(platform.ProcessSpec) (ownedProcess, error) { return completedProcess(0), nil })
	ops.removeAll = func(string) error { return errors.New("secret-canary") }
	if _, err := runWith(context.Background(), validRunRequestForTest(&memorySink{}), manifest, packagePath, cacheRoot, ops); err != ErrRun {
		t.Fatalf("operation cleanup failure = %v", err)
	}
}

func TestRunOwnsSinkAcrossPreExecutionFailures(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	for _, test := range []struct {
		name string
		edit func(*packageManifest, *runOps)
	}{
		{
			name: "validation",
			edit: func(manifest *packageManifest, _ *runOps) { manifest.Files = nil },
		},
		{
			name: "operation directory",
			edit: func(_ *packageManifest, ops *runOps) {
				createDir := ops.createDir
				ops.createDir = func(path string) error {
					if strings.HasPrefix(filepath.Base(path), ".operation-") {
						return errors.New("secret-canary")
					}
					return createDir(path)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := cloneManifest(manifest)
			ops := runTestOps(func(platform.ProcessSpec) (ownedProcess, error) {
				t.Fatal("process started after pre-execution failure")
				return nil, nil
			})
			test.edit(&manifest, &ops)
			sink := &memorySink{closeErr: errors.New("secret-canary")}
			if result, err := runWith(context.Background(), validRunRequestForTest(sink), manifest, packagePath, cacheRoot, ops); result != (RunResult{}) || err != ErrRun || sink.closeCount() != 1 {
				t.Fatalf("run = %#v, %v, closes=%d", result, err, sink.closeCount())
			}
		})
	}

	sink := &memorySink{closeErr: errors.New("secret-canary")}
	if result, err := Run(context.Background(), validRunRequestForTest(sink)); result != (RunResult{}) || err != ErrOutput || sink.closeCount() != 1 {
		t.Fatalf("empty inventory close failure = %#v, %v, closes=%d", result, err, sink.closeCount())
	}
}

func TestRunClassifiesProcessAndCleanupFailures(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	for _, test := range []struct {
		name    string
		process *fakeRunProcess
		want    error
	}{
		{"failed start", nil, ErrRun},
		{"nonzero", completedProcess(23), ErrRun},
		{"wait failure", failedProcess(-1, errors.New("secret-canary")), ErrRun},
		{"terminate failure", &fakeRunProcess{done: make(chan struct{}), waitReturned: make(chan struct{}), terminateErr: errors.New("secret-canary")}, ErrRun},
		{"close failure", closeFailureProcess(), ErrRun},
	} {
		t.Run(test.name, func(t *testing.T) {
			ops := runTestOps(func(spec platform.ProcessSpec) (ownedProcess, error) {
				if test.name == "failed start" {
					return nil, errors.New("secret-canary")
				}
				if test.name == "terminate failure" {
					_, _ = spec.Stdout.Write([]byte("x"))
				}
				return test.process, nil
			})
			sink := &memorySink{}
			if test.name == "terminate failure" {
				sink.writeErr = errors.New("secret-canary")
			}
			request := validRunRequestForTest(sink)
			result, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops)
			if err != test.want || result != (RunResult{}) || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("run = %#v, %v", result, err)
			}
		})
	}
}

func TestRunPumpsLimitsAndSinkFailures(t *testing.T) {
	for _, test := range []struct {
		name                     string
		stdout, stderr           string
		stdoutLimit, stderrLimit uint64
		sink                     *memorySink
		want                     error
		wantSink                 string
	}{
		{"exact limits", "abcd", "wxyz", 4, 4, &memorySink{}, nil, "abcd"},
		{"stdout one byte excess", "abcde", "", 4, 1, &memorySink{}, ErrOutput, "abcd"},
		{"stderr one byte excess", "", "abcde", 1, 4, &memorySink{}, ErrOutput, ""},
		{"zero short write", "a", "", 1, 1, &memorySink{short: true}, ErrOutput, ""},
		{"partial write", "abcd", "", 4, 1, &memorySink{partial: 2}, ErrOutput, "ab"},
		{"partial write with error", "abcd", "", 4, 1, &memorySink{partial: 2, writeErr: errors.New("secret-canary")}, ErrOutput, "ab"},
		{"sink write", "a", "", 1, 1, &memorySink{writeErr: errors.New("secret-canary")}, ErrOutput, ""},
		{"sink close", "", "", 1, 1, &memorySink{closeErr: errors.New("secret-canary")}, ErrOutput, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest, packagePath, cacheRoot := preparedRunPayload(t)
			ops := runTestOps(func(spec platform.ProcessSpec) (ownedProcess, error) {
				if _, err := spec.Stdout.Write([]byte(test.stdout)); err != nil {
					return nil, err
				}
				if _, err := spec.Stderr.Write([]byte(test.stderr)); err != nil {
					return nil, err
				}
				return completedProcess(0), nil
			})
			request := validRunRequestForTest(test.sink)
			request.StdoutLimit, request.StderrLimit = test.stdoutLimit, test.stderrLimit
			result, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops)
			if err != test.want || (err != nil && strings.Contains(err.Error(), "secret-canary")) {
				t.Fatalf("run = %#v, %v", result, err)
			}
			if err != nil && result != (RunResult{}) {
				t.Fatalf("failure result = %#v; want zero", result)
			}
			if err == nil && result != (RunResult{ExitCode: 0, StdoutBytes: uint64(len(test.stdout)), StderrBytes: uint64(len(test.stderr))}) {
				t.Fatalf("success result = %#v", result)
			}
			if got := test.sink.String(); got != test.wantSink {
				t.Fatalf("sink = %q; want %q", got, test.wantSink)
			}
		})
	}
}

func TestRunConcurrentStreamErrorsReturnZeroDeterministically(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	for _, test := range []struct {
		name, stdout, stderr string
	}{
		{"stdout overflow", "abcde", "wxyz"},
		{"stderr overflow", "abcd", "wxyzq"},
		{"both overflow", "abcde", "wxyzq"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for iteration := range 25 {
				sink := newSignalingSink()
				process := &fakeRunProcess{done: make(chan struct{}), waitReturned: make(chan struct{}), result: processResult{code: 0}}
				var deferredStderr *os.File
				ops := runTestOps(func(spec platform.ProcessSpec) (ownedProcess, error) {
					deferredStderr = spec.Stderr
					if _, err := spec.Stdout.Write([]byte(test.stdout)); err != nil {
						return nil, err
					}
					go func() {
						<-sink.written
						_, _ = spec.Stderr.Write([]byte(test.stderr))
						_ = spec.Stderr.Close()
						process.finish()
						close(sink.release)
					}()
					return process, nil
				})
				closeFile := ops.closeFile
				ops.closeFile = func(file *os.File) error {
					if file == deferredStderr {
						return nil // model the synthetic child's ownership of its stderr duplicate
					}
					return closeFile(file)
				}
				request := validRunRequestForTest(sink)
				request.StdoutLimit, request.StderrLimit = 4, 4
				result, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops)
				if err != ErrOutput || result != (RunResult{}) || sink.String() != "abcd" || sink.closeCount() != 1 {
					t.Fatalf("iteration %d: run = %#v, %v, sink=%q, closes=%d", iteration, result, err, sink.String(), sink.closeCount())
				}
			}
		})
	}
}

func TestRunPumpsAreBoundedAndCancellationAware(t *testing.T) {
	for _, test := range []struct {
		name, data string
		limit      uint64
		stdout     bool
		want       error
	}{
		{"stdout exact", strings.Repeat("x", runBufferBytes), runBufferBytes, true, nil},
		{"stdout excess", strings.Repeat("x", runBufferBytes+1), runBufferBytes, true, ErrOutput},
		{"stderr exact", strings.Repeat("x", runBufferBytes), runBufferBytes, false, nil},
		{"stderr excess", strings.Repeat("x", runBufferBytes+1), runBufferBytes, false, ErrOutput},
	} {
		t.Run(test.name, func(t *testing.T) {
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			done := make(chan error, 1)
			go func() { _, err := write.Write([]byte(test.data)); _ = write.Close(); done <- err }()
			sink := &memorySink{}
			var result streamResult
			if test.stdout {
				result = pumpStdout(context.Background(), read, sink, test.limit)
			} else {
				result = pumpDiscard(context.Background(), read, test.limit)
			}
			wantBytes := uint64(len(test.data))
			if wantBytes > test.limit {
				wantBytes = test.limit
			}
			if result.err != test.want || result.bytes != wantBytes || test.stdout && sink.String() != test.data[:wantBytes] {
				t.Fatalf("pump = %#v, sink=%d", result, len(sink.String()))
			}
			if err := <-done; err != nil && test.want == nil {
				t.Fatal(err)
			}
		})
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := pumpStdout(ctx, read, &memorySink{}, 1); result.err != ErrRun {
		t.Fatalf("cancelled pump = %#v", result)
	}
	failedRead, failedWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = failedRead.Close()
	_ = failedWrite.Close()
	if result := pumpStdout(context.Background(), failedRead, &memorySink{}, 1); result.err != ErrRun {
		t.Fatalf("failed read pump = %#v", result)
	}
}

func TestRunSynchronousBackpressureFailureAndCallerCancellation(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "release", true: "failure"}[fail], func(t *testing.T) {
			sink := newGatedSink(fail)
			ops := runTestOps(func(spec platform.ProcessSpec) (ownedProcess, error) {
				if _, err := spec.Stdout.Write([]byte("x")); err != nil {
					return nil, err
				}
				return completedProcess(0), nil
			})
			result := make(chan error, 1)
			go func() {
				_, err := runWith(context.Background(), validRunRequestForTest(sink), manifest, packagePath, cacheRoot, ops)
				result <- err
			}()
			awaitSignal(t, sink.entered, "sink backpressure")
			select {
			case err := <-result:
				t.Fatalf("run returned before sink release: %v", err)
			default:
			}
			close(sink.release)
			want := error(nil)
			if fail {
				want = ErrOutput
			}
			if err := awaitError(t, result, "backpressure run"); err != want || sink.closeCount() != 1 {
				t.Fatalf("run = %v, closes=%d", err, sink.closeCount())
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	process := waitingProcess()
	ops := runTestOps(func(platform.ProcessSpec) (ownedProcess, error) {
		close(started)
		return process, nil
	})
	result := make(chan error, 1)
	go func() {
		_, err := runWith(ctx, validRunRequestForTest(&memorySink{}), manifest, packagePath, cacheRoot, ops)
		result <- err
	}()
	awaitSignal(t, started, "process start")
	cancel()
	if err := awaitError(t, result, "caller cancellation"); err != ErrRun {
		t.Fatalf("cancelled run = %v", err)
	}
	awaitSignal(t, process.waitReturned, "process waiter join")
}

func TestRunCallerCancellationUnblocksSinkAndJoins(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	ctx, cancel := context.WithCancel(context.Background())
	sink := newCancelBlockedSink()
	process := waitingProcess()
	ops := runTestOps(func(spec platform.ProcessSpec) (ownedProcess, error) {
		if _, err := spec.Stdout.Write([]byte("blocked")); err != nil {
			return nil, err
		}
		return process, nil
	})
	result := make(chan error, 1)
	go func() {
		_, err := runWith(ctx, validRunRequestForTest(sink), manifest, packagePath, cacheRoot, ops)
		result <- err
	}()
	awaitSignal(t, sink.entered, "blocked sink")
	cancel()
	if err := awaitError(t, result, "blocked sink cancellation"); err != ErrRun {
		t.Fatalf("cancelled blocked sink = %v", err)
	}
	awaitSignal(t, sink.exited, "sink writer join")
	awaitSignal(t, process.waitReturned, "process waiter join")
	if sink.closeCount() != 1 {
		t.Fatalf("sink closes = %d; want 1", sink.closeCount())
	}
}

func TestRunConcurrentOperationDirectoriesAreUniqueAndRemoved(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	const workers = 8
	var mu sync.Mutex
	created := make(map[string]bool)
	removed := make(map[string]bool)
	var group sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			ops := runTestOps(func(spec platform.ProcessSpec) (ownedProcess, error) {
				_, _ = spec.Stdout.Write([]byte("ok"))
				return completedProcess(0), nil
			})
			create := ops.createDir
			ops.createDir = func(path string) error {
				err := create(path)
				if err == nil && strings.HasPrefix(filepath.Base(path), ".operation-") {
					mu.Lock()
					if created[path] {
						err = errors.New("duplicate operation directory")
					}
					created[path] = true
					mu.Unlock()
				}
				return err
			}
			remove := ops.removeAll
			ops.removeAll = func(path string) error {
				mu.Lock()
				removed[path] = true
				mu.Unlock()
				return remove(path)
			}
			_, err := runWith(context.Background(), validRunRequestForTest(&memorySink{}), manifest, packagePath, cacheRoot, ops)
			errs <- err
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(created) != workers || len(removed) != workers {
		t.Fatalf("operation directories created=%d removed=%d", len(created), len(removed))
	}
	for path := range created {
		if !removed[path] {
			t.Fatalf("operation directory not removed: %s", path)
		}
	}
}

func TestRunSimultaneousLargeStdoutAndStderr(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	sink := &memorySink{}
	ops := defaultRunOps()
	ops.prepare = func(operation string) error {
		return platform.WritePrivateFile(filepath.Join(operation, ".sparc-run-test-mode"), []byte("large"))
	}
	request := validRunRequestForTest(sink)
	request.Timeout = runTestTimeout
	request.StdoutLimit = 4 * runBufferBytes
	request.StderrLimit = 4 * runBufferBytes
	result, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops)
	if err != nil || result.StdoutBytes != 4*runBufferBytes || result.StderrBytes != 4*runBufferBytes || len(sink.String()) != 4*runBufferBytes {
		t.Fatalf("large run = %#v, %v, sink=%d", result, err, len(sink.String()))
	}
}

func TestRunSetupAndDescriptorFailuresAreBoundedAndClosed(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	for _, test := range []struct {
		name string
		edit func(*runOps)
	}{
		{"null open", func(ops *runOps) { ops.openNull = func() (*os.File, error) { return nil, errors.New("secret-canary") } }},
		{"pipe", func(ops *runOps) {
			ops.pipe = func() (*os.File, *os.File, error) { return nil, nil, errors.New("secret-canary") }
		}},
		{"close", func(ops *runOps) {
			closeFile := ops.closeFile
			var once sync.Once
			ops.closeFile = func(file *os.File) (err error) {
				err = closeFile(file)
				once.Do(func() { err = errors.New("secret-canary") })
				return err
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			sink := &memorySink{}
			process := completedProcess(0)
			ops := runTestOps(func(platform.ProcessSpec) (ownedProcess, error) { return process, nil })
			test.edit(&ops)
			result, err := runWith(context.Background(), validRunRequestForTest(sink), manifest, packagePath, cacheRoot, ops)
			if err != ErrRun || result != (RunResult{}) || strings.Contains(err.Error(), "secret-canary") || sink.closeCount() != 1 {
				t.Fatalf("run = %#v, %v, closes=%d", result, err, sink.closeCount())
			}
		})
	}

	started := make(chan struct{})
	var validationCtx context.Context
	var startCalled atomic.Bool
	ops := runTestOps(func(platform.ProcessSpec) (ownedProcess, error) {
		startCalled.Store(true)
		return nil, ErrRun
	})
	ops.validate = func(ctx context.Context, tool Tool, manifest packageManifest, packagePath, _ string) (string, error) {
		validationCtx = ctx
		return filepath.Join(packagePath, filepath.FromSlash(manifest.Executables[tool])), nil
	}
	create := ops.createDir
	ops.createDir = func(path string) error {
		if filepath.Base(path) == "home" {
			close(started)
			<-validationCtx.Done()
		}
		return create(path)
	}
	request := validRunRequestForTest(&memorySink{})
	request.Timeout = 20 * time.Millisecond
	result := make(chan error, 1)
	go func() {
		_, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops)
		result <- err
	}()
	awaitSignal(t, started, "setup gate")
	if err := awaitError(t, result, "setup timeout"); err != ErrRun || startCalled.Load() {
		t.Fatalf("setup timeout = %v, start=%v", err, startCalled.Load())
	}
}

func TestRunValidationUsesOperationDeadline(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	var deadline time.Time
	ops := runTestOps(func(platform.ProcessSpec) (ownedProcess, error) {
		t.Fatal("process started after validation timeout")
		return nil, nil
	})
	ops.validate = func(ctx context.Context, _ Tool, _ packageManifest, _, _ string) (string, error) {
		var ok bool
		deadline, ok = ctx.Deadline()
		if !ok {
			t.Fatal("validation context has no operation deadline")
		}
		<-ctx.Done()
		return "", ctx.Err()
	}
	request := validRunRequestForTest(&memorySink{})
	request.Timeout = 20 * time.Millisecond
	if _, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops); err != ErrRun || deadline.IsZero() {
		t.Fatalf("validation timeout = %v, deadline=%v", err, deadline)
	}
}

func TestRunOwnsOneSidedFailedPipesExactlyOnce(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	for _, failedCall := range []int{1, 2} {
		for _, returnedSide := range []string{"read", "write"} {
			name := []string{"stdout", "stderr"}[failedCall-1] + " " + returnedSide
			t.Run(name, func(t *testing.T) {
				sink := &memorySink{}
				ops := runTestOps(func(platform.ProcessSpec) (ownedProcess, error) {
					t.Fatal("process started after pipe failure")
					return nil, nil
				})
				pipeCall := 0
				var returned *os.File
				ops.pipe = func() (*os.File, *os.File, error) {
					pipeCall++
					read, write, err := os.Pipe()
					if err != nil {
						return nil, nil, err
					}
					if pipeCall != failedCall {
						return read, write, nil
					}
					if returnedSide == "read" {
						returned = read
						_ = write.Close()
						return read, nil, errors.New("secret-canary")
					}
					returned = write
					_ = read.Close()
					return nil, write, errors.New("secret-canary")
				}
				closeFile := ops.closeFile
				var mu sync.Mutex
				closes := make(map[*os.File]int)
				ops.closeFile = func(file *os.File) error {
					mu.Lock()
					closes[file]++
					mu.Unlock()
					return closeFile(file)
				}
				result, err := runWith(context.Background(), validRunRequestForTest(sink), manifest, packagePath, cacheRoot, ops)
				if err != ErrRun || result != (RunResult{}) || strings.Contains(err.Error(), "secret-canary") {
					t.Fatalf("run = %#v, %v", result, err)
				}
				mu.Lock()
				defer mu.Unlock()
				if returned == nil || closes[returned] != 1 {
					t.Fatalf("returned descriptor closes = %d; want 1", closes[returned])
				}
				for file, count := range closes {
					if count != 1 {
						t.Fatalf("descriptor %d closed %d times", file.Fd(), count)
					}
				}
				if sink.closeCount() != 1 {
					t.Fatalf("sink closes = %d; want 1", sink.closeCount())
				}
			})
		}
	}
}

func TestRunClosesEveryDescriptorExactlyOnce(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	process := completedProcess(0)
	ops := runTestOps(func(spec platform.ProcessSpec) (ownedProcess, error) {
		_, _ = spec.Stdout.Write([]byte("ok"))
		return process, nil
	})
	closeFile := ops.closeFile
	var mu sync.Mutex
	closes := make(map[*os.File]int)
	ops.closeFile = func(file *os.File) error {
		mu.Lock()
		closes[file]++
		mu.Unlock()
		return closeFile(file)
	}
	if _, err := runWith(context.Background(), validRunRequestForTest(&memorySink{}), manifest, packagePath, cacheRoot, ops); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, process.waitReturned, "process waiter join")
	mu.Lock()
	defer mu.Unlock()
	if len(closes) != 5 {
		t.Fatalf("closed descriptors = %d; want 5", len(closes))
	}
	for file, count := range closes {
		if count != 1 {
			t.Fatalf("descriptor %d closed %d times", file.Fd(), count)
		}
	}
}

func TestRunCleanupDeadlineCannotReleaseProcessWaiter(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	process := newRetryCloseProcess()
	ops := runTestOps(func(spec platform.ProcessSpec) (ownedProcess, error) {
		_, _ = spec.Stdout.Write([]byte("x"))
		return process, nil
	})
	sink := &memorySink{writeErr: errors.New("secret-canary")}
	request := validRunRequestForTest(sink)
	request.CleanupTimeout = 20 * time.Millisecond
	result := make(chan error, 1)
	go func() {
		_, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops)
		result <- err
	}()
	awaitSignal(t, process.firstCloseReturned, "normal cleanup expiration")
	if err := awaitError(t, result, "fail-stop cleanup completion"); err != ErrRun {
		t.Fatalf("Run = %v", err)
	}
	select {
	case <-process.waitReturned:
	default:
		t.Fatal("Run returned before process waiter joined")
	}
	if got := process.closeCalls.Load(); got != 2 {
		t.Fatalf("CloseContext calls = %d; want 2", got)
	}
}

func TestRunJoinsWaitWrapperAfterReceivingResult(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	wrapperAfterSend := make(chan struct{})
	releaseWrapper := make(chan struct{})
	ops := runTestOps(func(platform.ProcessSpec) (ownedProcess, error) { return completedProcess(0), nil })
	ops.afterWaitResult = func() {
		close(wrapperAfterSend)
		<-releaseWrapper
	}
	result := make(chan error, 1)
	go func() {
		_, err := runWith(context.Background(), validRunRequestForTest(&memorySink{}), manifest, packagePath, cacheRoot, ops)
		result <- err
	}()
	awaitSignal(t, wrapperAfterSend, "wait wrapper post-result gate")
	select {
	case err := <-result:
		t.Fatalf("Run returned while wait wrapper still executed: %v", err)
	default:
	}
	close(releaseWrapper)
	if err := awaitError(t, result, "joined wait wrapper"); err != nil {
		t.Fatal(err)
	}
}

func TestRunSinkCloseHonorsCleanupDeadline(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	sink := &deadlineCloseSink{}
	request := validRunRequestForTest(sink)
	request.CleanupTimeout = 20 * time.Millisecond
	ops := runTestOps(func(platform.ProcessSpec) (ownedProcess, error) { return completedProcess(0), nil })
	if _, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops); err != ErrOutput {
		t.Fatalf("deadline close = %v", err)
	}
	if !sink.returned.Load() {
		t.Fatal("sink close did not return before Run")
	}
}

func TestRunParentExitKillsGrandchildRetainingPipes(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	sink := &memorySink{}
	ops := defaultRunOps()
	ops.prepare = func(operation string) error {
		return platform.WritePrivateFile(filepath.Join(operation, ".sparc-run-test-mode"), []byte("retained-pipe"))
	}
	request := validRunRequestForTest(sink)
	request.Timeout = runTestTimeout
	result, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops)
	if err != nil || result.ExitCode != 0 || sink.String() != "parent\n" {
		t.Fatalf("retained-pipe run = %#v, %v, output=%q", result, err, sink.String())
	}
}

func TestRunActualHelperVersionInvocation(t *testing.T) {
	manifest, packagePath, cacheRoot := preparedRunPayload(t)
	sink := &memorySink{}
	ops := defaultRunOps()
	ops.prepare = func(operation string) error {
		return platform.WritePrivateFile(filepath.Join(operation, ".sparc-run-test-mode"), []byte("report"))
	}
	request := validRunRequestForTest(sink)
	request.Timeout = runTestTimeout
	result, err := runWith(context.Background(), request, manifest, packagePath, cacheRoot, ops)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || !bytes.Contains(sink.bytes.Bytes(), []byte(`"args":["--version"]`)) || !bytes.Contains(sink.bytes.Bytes(), []byte(`"path":`)) {
		t.Fatalf("result/output = %#v, %q", result, sink.String())
	}
}

func preparedRunPayload(t *testing.T) (packageManifest, string, string) {
	t.Helper()
	preparedRunFixture.once.Do(func() {
		manifest, archive, _ := syntheticExecutableArchive(t)
		root, err := os.MkdirTemp("", "sparc-run-fixture-")
		if err != nil {
			t.Fatal(err)
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil || platform.CheckPrivateDir(root) != nil {
			t.Fatal(ErrRun)
		}
		cacheRoot := filepath.Join(root, "cache")
		if err := platform.CreatePrivateDir(cacheRoot); err != nil {
			t.Fatal(err)
		}
		path, err := preparePayload(context.Background(), cacheRoot, bytes.NewReader(archive), manifest)
		if err != nil {
			t.Fatal(err)
		}
		preparedRunFixture.manifest = manifest
		preparedRunFixture.packagePath = path
		preparedRunFixture.cacheRoot = cacheRoot
		preparedRunFixture.tempRoot = root
	})
	return cloneManifest(preparedRunFixture.manifest), preparedRunFixture.packagePath, preparedRunFixture.cacheRoot
}

func validRunRequestForTest(sink OutputSink) RunRequest {
	return RunRequest{Tool: PSQL, Version: true, Timeout: time.Second, CleanupTimeout: time.Second, StdoutLimit: 1 << 20, StderrLimit: 1 << 20, Stdout: sink}
}

func runTestOps(start func(platform.ProcessSpec) (ownedProcess, error)) runOps {
	ops := defaultRunOps()
	if start != nil {
		ops.start = start
	}
	return ops
}

type fakeRunProcess struct {
	done                   chan struct{}
	waitReturned           chan struct{}
	result                 processResult
	terminateErr, closeErr error
	mu                     sync.Mutex
	finished               bool
	waitOnce               sync.Once
}

func completedProcess(code int) *fakeRunProcess {
	process := &fakeRunProcess{done: make(chan struct{}), waitReturned: make(chan struct{}), result: processResult{code: code}, finished: true}
	close(process.done)
	return process
}
func failedProcess(code int, err error) *fakeRunProcess {
	process := completedProcess(code)
	process.result.err = err
	return process
}
func closeFailureProcess() *fakeRunProcess {
	process := completedProcess(0)
	process.closeErr = errors.New("secret-canary")
	return process
}
func waitingProcess() *fakeRunProcess {
	return &fakeRunProcess{done: make(chan struct{}), waitReturned: make(chan struct{}), result: processResult{code: -1}}
}
func (p *fakeRunProcess) Wait() (int, error) {
	<-p.done
	p.waitOnce.Do(func() {
		if p.waitReturned != nil {
			close(p.waitReturned)
		}
	})
	return p.result.code, p.result.err
}
func (p *fakeRunProcess) CloseContext(context.Context) error {
	p.finish()
	if p.terminateErr != nil {
		return p.terminateErr
	}
	return p.closeErr
}
func (p *fakeRunProcess) finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.finished {
		p.finished = true
		close(p.done)
	}
}

type retryCloseProcess struct {
	done               chan struct{}
	waitReturned       chan struct{}
	firstCloseReturned chan struct{}
	closeCalls         atomic.Int32
}

func newRetryCloseProcess() *retryCloseProcess {
	return &retryCloseProcess{done: make(chan struct{}), waitReturned: make(chan struct{}), firstCloseReturned: make(chan struct{})}
}

func (p *retryCloseProcess) Wait() (int, error) {
	<-p.done
	close(p.waitReturned)
	return 0, nil
}

func (p *retryCloseProcess) CloseContext(ctx context.Context) error {
	if p.closeCalls.Add(1) == 1 {
		<-ctx.Done()
		close(p.firstCloseReturned)
		return ctx.Err()
	}
	close(p.done)
	return nil
}

type memorySink struct {
	bytes              bytes.Buffer
	writeErr, closeErr error
	partial            int
	short              bool
	closes             int
	mu                 sync.Mutex
}

func (s *memorySink) WriteContext(ctx context.Context, data []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.partial > 0 {
		n := min(s.partial, len(data))
		_, _ = s.bytes.Write(data[:n])
		return n, s.writeErr
	}
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	if s.short {
		return 0, nil
	}
	return s.bytes.Write(data)
}
func (s *memorySink) CloseContext(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return s.closeErr
}
func (s *memorySink) String() string  { s.mu.Lock(); defer s.mu.Unlock(); return s.bytes.String() }
func (s *memorySink) closeCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.closes }

type signalingSink struct {
	memorySink
	written chan struct{}
	release chan struct{}
	once    sync.Once
}

func newSignalingSink() *signalingSink {
	return &signalingSink{written: make(chan struct{}), release: make(chan struct{})}
}

func (s *signalingSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	n, err := s.memorySink.WriteContext(ctx, data)
	s.once.Do(func() { close(s.written) })
	<-s.release
	return n, err
}

type deadlineCloseSink struct{ returned atomic.Bool }

func (*deadlineCloseSink) WriteContext(context.Context, []byte) (int, error) { return 0, nil }
func (s *deadlineCloseSink) CloseContext(ctx context.Context) error {
	<-ctx.Done()
	s.returned.Store(true)
	return ctx.Err()
}

type cancelBlockedSink struct {
	entered chan struct{}
	exited  chan struct{}
	once    sync.Once
	mu      sync.Mutex
	closes  int
}

func newCancelBlockedSink() *cancelBlockedSink {
	return &cancelBlockedSink{entered: make(chan struct{}), exited: make(chan struct{})}
}

func (s *cancelBlockedSink) WriteContext(ctx context.Context, _ []byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	<-ctx.Done()
	close(s.exited)
	return 0, ctx.Err()
}

func (s *cancelBlockedSink) CloseContext(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}

func (s *cancelBlockedSink) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

type gatedSink struct {
	entered chan struct{}
	release chan struct{}
	fail    bool
	once    sync.Once
	mu      sync.Mutex
	closes  int
}

func newGatedSink(fail bool) *gatedSink {
	return &gatedSink{entered: make(chan struct{}), release: make(chan struct{}), fail: fail}
}
func (s *gatedSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
		if s.fail {
			return 0, errors.New("secret-canary")
		}
		return len(data), nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
func (s *gatedSink) CloseContext(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}
func (s *gatedSink) closeCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.closes }

func awaitSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	timer := time.NewTimer(runTestTimeout)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", name)
	}
}

func awaitError(t *testing.T, result <-chan error, name string) error {
	t.Helper()
	timer := time.NewTimer(runTestTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", name)
		return ErrRun
	}
}

func containsEnvironment(entries []string, prefix string) bool {
	for _, entry := range entries {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}
