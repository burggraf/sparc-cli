package platform

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const processTestTimeout = 10 * time.Second

func TestProcessSuccessNonzeroAndRepeatedWait(t *testing.T) {
	for _, test := range []struct {
		name string
		code int
	}{
		{"success", 0},
		{"nonzero", 23},
	} {
		t.Run(test.name, func(t *testing.T) {
			process := startHelperProcess(t, []string{"exit", strconv.Itoa(test.code)}, nil, t.TempDir(), devNullFiles(t))
			for range 2 {
				code, err := waitProcessDeadline(t, process)
				if err != nil || code != test.code {
					t.Fatalf("Wait = %d, %v; want %d, nil", code, err, test.code)
				}
			}
			if err := process.Close(); err != nil {
				t.Fatal(err)
			}
			if err := process.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProcessExactArgumentsEnvironmentAndDirectory(t *testing.T) {
	t.Setenv("SPARC_AMBIENT_CANARY", "must-not-inherit")
	directory := t.TempDir()
	files, stdout := pipeFiles(t)
	arguments := []string{"report", "plain", "two words", `quote"inside`, `slashes\\\"tail`, "雪"}
	environment := []string{"SPARC_PROCESS_HELPER=report", "ONLY_VALUE=two words 雪"}
	process := startHelperProcess(t, arguments, environment, directory, files)
	_ = files.Stdout.Close()
	_ = stdout.SetReadDeadline(time.Now().Add(processTestTimeout))
	var report helperReport
	if err := json.NewDecoder(stdout).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if code, err := waitProcessDeadline(t, process); err != nil || code != 0 {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.Args, "\x00") != strings.Join(arguments[1:], "\x00") {
		t.Fatalf("args = %#v; want %#v", report.Args, arguments[1:])
	}
	wantDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	wantPath, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	gotPath, err := filepath.EvalSymlinks(report.Path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Value != "two words 雪" || report.Ambient != "" || report.Dir != wantDirectory || gotPath != wantPath {
		t.Fatalf("report = %#v", report)
	}
}

func TestProcessRejectsInvalidSpecAndFailedStart(t *testing.T) {
	files := devNullFiles(t)
	valid := ProcessSpec{Path: os.Args[0], Args: []string{"-test.run=^TestProcessHelper$", "--", "exit", "0"}, Env: []string{"SPARC_PROCESS_HELPER=1"}, Dir: t.TempDir(), Stdin: files.Stdin, Stdout: files.Stdout, Stderr: files.Stderr}
	tests := []struct {
		name string
		edit func(*ProcessSpec)
	}{
		{"empty path", func(spec *ProcessSpec) { spec.Path = "" }},
		{"relative path", func(spec *ProcessSpec) { spec.Path = "relative" }},
		{"unclean path", func(spec *ProcessSpec) {
			spec.Path += string(os.PathSeparator) + ".." + string(os.PathSeparator) + filepath.Base(spec.Path)
		}},
		{"control argument", func(spec *ProcessSpec) { spec.Args = []string{"bad\narg"} }},
		{"invalid environment", func(spec *ProcessSpec) { spec.Env = []string{"NO_EQUALS"} }},
		{"invalid environment key", func(spec *ProcessSpec) { spec.Env = []string{"BAD-NAME=value"} }},
		{"duplicate environment", func(spec *ProcessSpec) { spec.Env = []string{"Name=a", "name=b"} }},
		{"relative directory", func(spec *ProcessSpec) { spec.Dir = "." }},
		{"nil stdin", func(spec *ProcessSpec) { spec.Stdin = nil }},
		{"nil stdout", func(spec *ProcessSpec) { spec.Stdout = nil }},
		{"nil stderr", func(spec *ProcessSpec) { spec.Stderr = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := valid
			test.edit(&spec)
			process, err := StartProcess(spec)
			if process != nil || err != ErrProcess || errors.Unwrap(err) != nil {
				t.Fatalf("StartProcess = %#v, %v", process, err)
			}
		})
	}
	missing := valid
	missing.Path = filepath.Join(t.TempDir(), "secret-canary-missing")
	if process, err := StartProcess(missing); process != nil || err != ErrProcess || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("failed start = %#v, %v", process, err)
	}
}

func TestProcessTerminateEndlessTreeAndRetainedPipe(t *testing.T) {
	modes := []string{"tree", "detached-parent"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			files, stdout := pipeFiles(t)
			process := startHelperProcess(t, []string{mode}, []string{"SPARC_PROCESS_HELPER=1"}, t.TempDir(), files)
			_ = files.Stdout.Close()
			_ = stdout.SetReadDeadline(time.Now().Add(processTestTimeout))
			reader := bufio.NewReader(stdout)
			if ready, err := reader.ReadByte(); err != nil || ready != 'R' {
				t.Fatalf("readiness = %q, %v", ready, err)
			}
			if mode == "detached-parent" {
				if code, err := waitProcessDeadline(t, process); err != nil || code != 0 {
					t.Fatalf("parent Wait = %d, %v", code, err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), processTestTimeout)
			defer cancel()
			if err := process.Terminate(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.ReadByte(); err != io.EOF {
				t.Fatalf("descendant retained output after termination: %v", err)
			}
			if err := process.Terminate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := process.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProcessCancellationAndInjectedFailures(t *testing.T) {
	cancelWait := make(chan struct{})
	process := startProcessForTest(&fakeNativeProcess{wait: cancelWait})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := process.Terminate(cancelled); err != ErrProcess {
		t.Fatalf("cancelled Terminate = %v", err)
	}
	close(cancelWait)
	fresh, freshCancel := context.WithTimeout(context.Background(), processTestTimeout)
	defer freshCancel()
	if err := process.Terminate(fresh); err != nil {
		t.Fatalf("repeated Terminate = %v", err)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}

	timeoutWait := make(chan struct{})
	process = startProcessForTest(&fakeNativeProcess{wait: timeoutWait})
	expired, expireCancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer expireCancel()
	if err := process.Terminate(expired); err != ErrProcess {
		t.Fatalf("timed out Terminate = %v", err)
	}
	close(timeoutWait)
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}

	blockedWait := make(chan struct{})
	native := &fakeNativeProcess{wait: blockedWait, terminateErr: errors.New("secret-canary-terminate")}
	process = startProcessForTest(native)
	cancelled, cancel = context.WithCancel(context.Background())
	cancel()
	if err := process.Terminate(cancelled); err != ErrProcess || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("Terminate = %v", err)
	}
	close(blockedWait)
	if _, err := waitProcessDeadline(t, process); err != nil {
		t.Fatal(err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("Close after retryable termination failure = %v", err)
	}

	closeFailure := &fakeNativeProcess{wait: closedChannel(), closeErr: errors.New("secret-canary-close")}
	process = startProcessForTest(closeFailure)
	if _, err := waitProcessDeadline(t, process); err != nil {
		t.Fatal(err)
	}
	if err := process.Close(); err != ErrProcess || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("Close = %v", err)
	}
	if err := process.Close(); err != ErrProcess {
		t.Fatalf("repeated Close = %v", err)
	}

	waitFailure := &fakeNativeProcess{wait: closedChannel(), waitErr: errors.New("secret-canary-wait")}
	process = startProcessForTest(waitFailure)
	if code, err := waitProcessDeadline(t, process); code != -1 || err != ErrProcess || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	if err := process.Terminate(context.Background()); err != ErrProcess {
		t.Fatalf("Terminate after wait failure = %v", err)
	}
	if err := process.Close(); err != ErrProcess {
		t.Fatalf("Close after wait failure = %v", err)
	}
}

func TestProcessConcurrentLifecycle(t *testing.T) {
	files, stdout := pipeFiles(t)
	process := startHelperProcess(t, []string{"hold"}, []string{"SPARC_PROCESS_HELPER=1"}, t.TempDir(), files)
	_ = files.Stdout.Close()
	if ready := readByteDeadline(t, stdout); ready != 'R' {
		t.Fatalf("readiness = %q", ready)
	}
	ctx, cancel := context.WithTimeout(context.Background(), processTestTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = process.Terminate(ctx) }()
	}
	waits := make([]<-chan processWaitResult, 4)
	for i := range waits {
		waits[i] = startProcessWait(process)
	}
	waitGroupDeadline(t, &wg)
	for _, wait := range waits {
		if _, err := awaitProcessWait(t, process, wait); err != nil {
			t.Fatal(err)
		}
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
}

type helperReport struct {
	Args    []string
	Value   string
	Ambient string
	Dir     string
	Path    string
}

func TestProcessHelper(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if os.Getenv("SPARC_PROCESS_HELPER") == "" && (separator < 0 || separator+1 >= len(os.Args) || os.Args[separator+1] != "report-empty-env") {
		t.Skip("helper process")
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(111)
	}
	arguments := os.Args[separator+1:]
	switch arguments[0] {
	case "exit":
		code, _ := strconv.Atoi(arguments[1])
		os.Exit(code)
	case "report", "report-empty-env":
		directory, err := os.Getwd()
		if err != nil {
			os.Exit(112)
		}
		executable, err := os.Executable()
		if err != nil {
			os.Exit(113)
		}
		_ = json.NewEncoder(os.Stdout).Encode(helperReport{Args: arguments[1:], Value: os.Getenv("ONLY_VALUE"), Ambient: os.Getenv("SPARC_AMBIENT_CANARY"), Dir: directory, Path: executable})
	case "hold", "hold-ready":
		if arguments[0] == "hold-ready" {
			_ = os.WriteFile(".sparc-process-child-ready", []byte("ready"), 0o600)
		}
		_, _ = os.Stdout.Write([]byte{'R'})
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "tree", "detached-parent":
		childMode := "hold"
		if arguments[0] == "detached-parent" {
			childMode = "hold-ready"
		}
		command := exec.Command(os.Args[0], "-test.run=^TestProcessHelper$", "--", childMode)
		command.Env = []string{"SPARC_PROCESS_HELPER=1"}
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			os.Exit(114)
		}
		if arguments[0] == "detached-parent" {
			deadline := time.Now().Add(processTestTimeout)
			for {
				if _, err := os.Stat(".sparc-process-child-ready"); err == nil {
					os.Exit(0)
				}
				if time.Now().After(deadline) {
					os.Exit(116)
				}
				runtime.Gosched()
			}
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
	default:
		os.Exit(115)
	}
}

type processFiles struct {
	Stdin, Stdout, Stderr *os.File
	inputWriter           *os.File
}

func devNullFiles(t *testing.T) processFiles {
	t.Helper()
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = stdout.Close(); _ = stderr.Close() })
	return processFiles{Stdin: stdin, Stdout: stdout, Stderr: stderr}
}

func pipeFiles(t *testing.T) (processFiles, *os.File) {
	t.Helper()
	stdin, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = inputWriter.Close()
		_ = stdout.Close()
		_ = writer.Close()
		_ = stderr.Close()
	})
	return processFiles{Stdin: stdin, Stdout: writer, Stderr: stderr, inputWriter: inputWriter}, stdout
}

func startHelperProcess(t *testing.T, arguments, environment []string, directory string, files processFiles) *Process {
	t.Helper()
	if len(environment) == 0 {
		environment = []string{"SPARC_PROCESS_HELPER=1"}
	}
	process, err := StartProcess(ProcessSpec{
		Path:  os.Args[0],
		Args:  append([]string{"-test.run=^TestProcessHelper$", "--"}, arguments...),
		Env:   environment,
		Dir:   directory,
		Stdin: files.Stdin, Stdout: files.Stdout, Stderr: files.Stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Close() })
	return process
}

type processWaitResult struct {
	code int
	err  error
}

func startProcessWait(process *Process) <-chan processWaitResult {
	result := make(chan processWaitResult, 1)
	go func() {
		code, err := process.Wait()
		result <- processWaitResult{code: code, err: err}
	}()
	return result
}

func waitProcessDeadline(t *testing.T, process *Process) (int, error) {
	t.Helper()
	return awaitProcessWait(t, process, startProcessWait(process))
}

func awaitProcessWait(t *testing.T, process *Process, result <-chan processWaitResult) (int, error) {
	t.Helper()
	timer := time.NewTimer(processTestTimeout)
	defer timer.Stop()
	select {
	case completed := <-result:
		return completed.code, completed.err
	case <-timer.C:
		_ = process.Close()
		t.Fatal("Process.Wait timed out")
		return -1, ErrProcess
	}
}

func waitGroupDeadline(t *testing.T, group *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	timer := time.NewTimer(processTestTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatal("wait group timed out")
	}
}

func readByteDeadline(t *testing.T, file *os.File) byte {
	t.Helper()
	_ = file.SetReadDeadline(time.Now().Add(processTestTimeout))
	var one [1]byte
	if _, err := io.ReadFull(file, one[:]); err != nil {
		t.Fatal(err)
	}
	return one[0]
}

type fakeNativeProcess struct {
	wait                            <-chan struct{}
	waitErr, terminateErr, closeErr error
}

func (p *fakeNativeProcess) waitProcess() (int, error) { <-p.wait; return 0, p.waitErr }
func (p *fakeNativeProcess) terminateProcess() error   { return p.terminateErr }
func (p *fakeNativeProcess) closeProcess() error       { return p.closeErr }

func closedChannel() <-chan struct{} { channel := make(chan struct{}); close(channel); return channel }

func startProcessForTest(native nativeProcess) *Process { return newProcess(native) }
