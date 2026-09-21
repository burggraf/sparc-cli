//go:build darwin

package platform

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestProcessDarwinReapsDirectChildAndDoesNotLeakDescriptors(t *testing.T) {
	files := devNullFiles(t)
	spec := ProcessSpec{
		Path: os.Args[0], Args: []string{"-test.run=^TestProcessHelper$", "--", "exit", "0"},
		Env: []string{"SPARC_PROCESS_HELPER=1"}, Dir: t.TempDir(),
		Stdin: files.Stdin, Stdout: files.Stdout, Stderr: files.Stderr,
	}
	before := darwinDescriptorCount(t)
	var reapedPID int
	for i := 0; i < 16; i++ {
		native, err := startNativeProcess(spec)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			reapedPID = native.(*darwinProcess).groupID
		}
		process := newProcess(native)
		if code, err := waitProcessDeadline(t, process); err != nil || code != 0 {
			t.Fatalf("Wait = %d, %v", code, err)
		}
		if err := process.Close(); err != nil {
			t.Fatal(err)
		}
	}
	var status unix.WaitStatus
	if _, err := unix.Wait4(reapedPID, &status, unix.WNOHANG, nil); !errors.Is(err, unix.ECHILD) {
		t.Fatalf("direct child remained waitable: %v", err)
	}
	after := darwinDescriptorCount(t)
	if after != before {
		t.Fatalf("descriptor count changed from %d to %d", before, after)
	}
}

func darwinDescriptorCount(t *testing.T) int {
	t.Helper()
	directory, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("descriptor inventory: %v, %v", readErr, closeErr)
	}
	return len(names)
}

func TestProcessDarwinNativeReapExcludesLateSignal(t *testing.T) {
	reapPaused := make(chan struct{})
	publishReap := make(chan struct{})
	terminateStarted := make(chan struct{})
	allowTerminate := make(chan struct{})
	var killCalls atomic.Int32
	process := &darwinProcess{
		groupID: 42,
		wait4: func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error) {
			return 42, nil
		},
		kill: func(int, unix.Signal) error {
			killCalls.Add(1)
			return nil
		},
		afterNativeReap: func() {
			close(reapPaused)
			<-publishReap
		},
		beforeTerminate: func() {
			close(terminateStarted)
			<-allowTerminate
		},
	}
	owned := newProcess(process)
	awaitDarwinSignal(t, reapPaused, "native reap")
	if process.mu.TryLock() {
		process.mu.Unlock()
		t.Fatal("native reap did not retain lifecycle mutex before publication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), processTestTimeout)
	defer cancel()
	terminateDone := make(chan error, 1)
	go func() { terminateDone <- owned.Terminate(ctx) }()
	awaitDarwinSignal(t, terminateStarted, "termination attempt")
	close(allowTerminate)
	for range 100 {
		runtime.Gosched()
	}
	if got := killCalls.Load(); got != 0 {
		t.Fatalf("group signaled %d times during native reap", got)
	}
	close(publishReap)
	if err := awaitDarwinError(t, terminateDone, "termination completion"); err != nil {
		t.Fatal(err)
	}
	if got := killCalls.Load(); got != 0 {
		t.Fatalf("reaped group signaled %d times", got)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDarwinLiveTerminationStillSignals(t *testing.T) {
	var exited atomic.Bool
	var killCalls atomic.Int32
	process := &darwinProcess{
		groupID: 42,
		wait4: func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error) {
			if exited.Load() {
				return 42, nil
			}
			return 0, nil
		},
		kill: func(pid int, signal unix.Signal) error {
			if pid != -42 || signal != unix.SIGKILL {
				t.Fatalf("kill = %d, %v", pid, signal)
			}
			killCalls.Add(1)
			exited.Store(true)
			return nil
		},
	}
	owned := newProcess(process)
	ctx, cancel := context.WithTimeout(context.Background(), processTestTimeout)
	defer cancel()
	if err := owned.Terminate(ctx); err != nil {
		t.Fatal(err)
	}
	if got := killCalls.Load(); got != 1 {
		t.Fatalf("live group signaled %d times", got)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDarwinTreatsMissingLiveGroupAsTerminated(t *testing.T) {
	process := &darwinProcess{groupID: 42, kill: func(int, unix.Signal) error { return unix.ESRCH }}
	if err := process.terminateProcess(); err != nil {
		t.Fatal(err)
	}
}

func awaitDarwinSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	timer := time.NewTimer(processTestTimeout)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", name)
	}
}

func awaitDarwinError(t *testing.T, result <-chan error, name string) error {
	t.Helper()
	timer := time.NewTimer(processTestTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", name)
		return ErrProcess
	}
}
