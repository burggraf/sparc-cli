//go:build darwin

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
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

func TestStartNativeProcessDarwinEmptyEnvironmentDoesNotInherit(t *testing.T) {
	t.Setenv("SPARC_AMBIENT_CANARY", "must-not-inherit")
	files, stdout := pipeFiles(t)
	native, err := startNativeProcess(ProcessSpec{
		Path: os.Args[0], Args: []string{"-test.run=^TestProcessHelper$", "--", "report-empty-env", "pipe-report"},
		Env: make([]string, 0), Dir: t.TempDir(),
		Stdin: files.Stdin, Stdout: files.Stdout, Stderr: files.Stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	process := newProcess(native)
	t.Cleanup(func() { _ = process.Close() })
	if err := files.Stdout.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stdout.SetReadDeadline(time.Now().Add(processTestTimeout)); err != nil {
		t.Fatal(err)
	}
	var report helperReport
	if err := json.NewDecoder(stdout).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if code, err := waitProcessDeadline(t, process); code != 0 || err != nil {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	if report.Ambient != "" || strings.Join(report.Args, ",") != "pipe-report" {
		t.Fatalf("report = %#v", report)
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

func TestPollDarwinExitUsesZombieFallbackAfterEmptyKqueuePoll(t *testing.T) {
	const pid = 42
	exited, err := pollDarwinExit(7, pid, darwinExitPollOps{
		kevent: func(kqueue int, changes, events []unix.Kevent_t, timeout *unix.Timespec) (int, error) {
			if kqueue != 7 || changes != nil || len(events) != 1 || timeout == nil {
				t.Fatalf("kevent poll = %d, %#v, %#v, %#v", kqueue, changes, events, timeout)
			}
			return 0, nil
		},
		processState: func(gotPID int) (int32, int8, error) {
			if gotPID != pid {
				t.Fatalf("process state pid = %d", gotPID)
			}
			return pid, darwinZombieState, nil
		},
	})
	if err != nil || !exited {
		t.Fatalf("pollDarwinExit = %v, %v; want true, nil", exited, err)
	}
}

func TestDarwinExitObserverUsesSysctlFallbackOnlyOnce(t *testing.T) {
	var polls, stateCalls int
	observe := newDarwinExitObserver(7, 42, darwinExitPollOps{
		kevent: func(kqueue int, changes, events []unix.Kevent_t, timeout *unix.Timespec) (int, error) {
			polls++
			if kqueue != 7 || changes != nil || len(events) != 1 || timeout == nil {
				t.Fatalf("kevent poll = %d, %#v, %#v, %#v", kqueue, changes, events, timeout)
			}
			return 0, nil
		},
		processState: func(pid int) (int32, int8, error) {
			stateCalls++
			if pid != 42 {
				t.Fatalf("process state pid = %d", pid)
			}
			return 42, darwinIdleState, nil
		},
	})
	for range 8 {
		exited, err := observe()
		if exited || err != nil {
			t.Fatalf("observe = %v, %v; want false, nil", exited, err)
		}
	}
	if polls != 8 || stateCalls != 1 {
		t.Fatalf("polls=%d state calls=%d; want 8 and 1", polls, stateCalls)
	}
}

func TestPollDarwinExitFailsClosedOnMissingOrUnexpectedProcessState(t *testing.T) {
	tests := []struct {
		name  string
		state func(int) (int32, int8, error)
	}{
		{"missing", func(int) (int32, int8, error) { return 0, 0, unix.ESRCH }},
		{"wrong pid", func(int) (int32, int8, error) { return 41, darwinZombieState, nil }},
		{"unexpected state", func(int) (int32, int8, error) { return 42, darwinZombieState + 1, nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exited, err := pollDarwinExit(7, 42, darwinExitPollOps{
				kevent:       func(int, []unix.Kevent_t, []unix.Kevent_t, *unix.Timespec) (int, error) { return 0, nil },
				processState: test.state,
			})
			if err == nil || exited {
				t.Fatalf("pollDarwinExit = %v, %v; want false, error", exited, err)
			}
		})
	}
}

func TestSetupDarwinProcessRegistrationFailureCleansUpInSafeOrder(t *testing.T) {
	files, stdout := pipeFiles(t)
	process, err := os.StartProcess(os.Args[0], []string{os.Args[0], "-test.run=^TestProcessHelper$", "--", "hold"}, &os.ProcAttr{
		Dir:   t.TempDir(),
		Env:   []string{"SPARC_PROCESS_HELPER=1"},
		Files: []*os.File{files.Stdin, files.Stdout, files.Stderr},
		Sys:   &syscall.SysProcAttr{Setpgid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	pid := process.Pid
	t.Cleanup(func() {
		var status unix.WaitStatus
		reapedPID, waitErr := unix.Wait4(pid, &status, unix.WNOHANG, nil)
		if waitErr == nil && reapedPID == 0 {
			_ = unix.Kill(-pid, unix.SIGKILL)
			_, _ = unix.Wait4(pid, &status, 0, nil)
		}
		if process.Pid != -1 {
			_ = process.Release()
		}
	})
	if err := files.Stdout.Close(); err != nil {
		t.Fatal(err)
	}
	if ready := readByteDeadline(t, stdout); ready != 'R' {
		t.Fatalf("readiness = %q", ready)
	}

	registrationErr := errors.New("registration failed")
	sequence := make([]string, 0, 5)
	descriptor := -1
	signaledWhileUnreaped := false
	ops := nativeDarwinStartOps()
	nativeKqueue := ops.kqueue
	nativeWait4 := ops.wait4
	nativeKill := ops.kill
	nativeRelease := ops.release
	nativeClose := ops.closeFD
	ops.kqueue = func() (int, error) {
		fd, err := nativeKqueue()
		descriptor = fd
		return fd, err
	}
	ops.poll.kevent = func(kqueue int, changes, events []unix.Kevent_t, timeout *unix.Timespec) (int, error) {
		sequence = append(sequence, "register")
		if kqueue != descriptor || len(changes) != 1 || events != nil || timeout != nil {
			return -1, errors.New("invalid registration")
		}
		change := changes[0]
		if change.Ident != uint64(pid) || change.Filter != unix.EVFILT_PROC || change.Flags != unix.EV_ADD|unix.EV_ONESHOT || change.Fflags != unix.NOTE_EXIT {
			return -1, errors.New("invalid exit filter")
		}
		return -1, registrationErr
	}
	ops.kill = func(groupID int, signal unix.Signal) error {
		sequence = append(sequence, "signal")
		if groupID != -pid || signal != unix.SIGKILL {
			t.Errorf("kill = %d, %v", groupID, signal)
		}
		var status unix.WaitStatus
		reapedPID, waitErr := unix.Wait4(pid, &status, unix.WNOHANG, nil)
		signaledWhileUnreaped = reapedPID == 0 && waitErr == nil && process.Pid == pid
		return nativeKill(groupID, signal)
	}
	ops.wait4 = func(waitPID int, status *unix.WaitStatus, options int, usage *unix.Rusage) (int, error) {
		sequence = append(sequence, "reap")
		if waitPID != pid || status == nil || options != 0 || usage != nil {
			t.Errorf("wait4 = %d, %#v, %d, %#v", waitPID, status, options, usage)
		}
		return nativeWait4(waitPID, status, options, usage)
	}
	ops.release = func(owned *os.Process) error {
		sequence = append(sequence, "release")
		if owned != process {
			t.Error("released a different process")
		}
		return nativeRelease(owned)
	}
	ops.closeFD = func(fd int) error {
		sequence = append(sequence, "close")
		if fd != descriptor {
			t.Errorf("closed descriptor = %d; want %d", fd, descriptor)
		}
		return nativeClose(fd)
	}

	native, err := setupDarwinProcess(process, ops)
	if native != nil || !errors.Is(err, registrationErr) {
		t.Fatalf("setupDarwinProcess = %#v, %v", native, err)
	}
	if got := strings.Join(sequence, ","); got != "register,signal,reap,release,close" {
		t.Fatalf("cleanup sequence = %s", got)
	}
	if !signaledWhileUnreaped {
		t.Fatal("group was not signaled while the direct child remained unreaped")
	}
	if process.Pid != -1 {
		t.Fatalf("released process pid = %d; want -1", process.Pid)
	}
	var status unix.WaitStatus
	if _, err := unix.Wait4(pid, &status, unix.WNOHANG, nil); !errors.Is(err, unix.ECHILD) {
		t.Fatalf("direct child remained waitable: %v", err)
	}
	if err := unix.Close(descriptor); !errors.Is(err, unix.EBADF) {
		t.Fatalf("kqueue descriptor remained open: %v", err)
	}
}

func TestProcessDarwinExitNotificationSignalsBeforeReap(t *testing.T) {
	var sequence []string
	process := &darwinProcess{
		groupID:     42,
		kqueue:      -1,
		observeExit: func() (bool, error) { return true, nil },
		wait4: func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error) {
			sequence = append(sequence, "reap")
			if len(sequence) != 2 || sequence[0] != "signal" {
				t.Fatalf("sequence at reap = %v", sequence)
			}
			return 42, nil
		},
		kill: func(pid int, signal unix.Signal) error {
			if pid != -42 || signal != unix.SIGKILL {
				t.Fatalf("kill = %d, %v", pid, signal)
			}
			sequence = append(sequence, "signal")
			return nil
		},
	}
	owned := newProcess(process)
	if code, err := waitProcessDeadline(t, owned); code != 0 || err != nil {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	if strings.Join(sequence, ",") != "signal,reap" {
		t.Fatalf("sequence = %v", sequence)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDarwinECHILDPublishesNoLaterSignal(t *testing.T) {
	reapPaused := make(chan struct{})
	publishReap := make(chan struct{})
	terminateStarted := make(chan struct{})
	allowTerminate := make(chan struct{})
	var killCalls atomic.Int32
	process := &darwinProcess{
		groupID:     42,
		kqueue:      -1,
		signaled:    true,
		observeExit: func() (bool, error) { return true, nil },
		wait4: func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error) {
			return -1, unix.ECHILD
		},
		kill: func(int, unix.Signal) error {
			killCalls.Add(1)
			return nil
		},
		afterNativeReap: func() {
			close(reapPaused)
			select {
			case <-publishReap:
			case <-time.After(processTestTimeout):
			}
		},
		beforeTerminate: func() {
			close(terminateStarted)
			select {
			case <-allowTerminate:
			case <-time.After(processTestTimeout):
			}
		},
	}
	owned := newProcess(process)
	awaitDarwinSignal(t, reapPaused, "ECHILD publication gate")
	if process.mu.TryLock() {
		process.mu.Unlock()
		t.Fatal("ECHILD state did not retain lifecycle mutex before publication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), processTestTimeout)
	defer cancel()
	terminateDone := make(chan error, 1)
	go func() { terminateDone <- owned.Terminate(ctx) }()
	awaitDarwinSignal(t, terminateStarted, "termination attempt")
	close(allowTerminate)
	close(publishReap)
	if err := awaitDarwinError(t, terminateDone, "termination completion"); err != ErrProcess {
		t.Fatalf("Terminate = %v", err)
	}
	if got := killCalls.Load(); got != 0 {
		t.Fatalf("signals after ECHILD = %d; want 0", got)
	}
	if _, err := waitProcessDeadline(t, owned); err != ErrProcess {
		t.Fatalf("Wait = %v", err)
	}
}

func TestProcessDarwinLiveTerminationStillSignals(t *testing.T) {
	var exited atomic.Bool
	var killCalls atomic.Int32
	process := &darwinProcess{
		groupID:     42,
		kqueue:      -1,
		observeExit: func() (bool, error) { return exited.Load(), nil },
		wait4:       func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error) { return 42, nil },
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
		t.Fatalf("live group cleanup calls = %d; want 1", got)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDarwinTreatsMissingLiveGroupAsTerminated(t *testing.T) {
	process := &darwinProcess{groupID: 42, kqueue: -1, kill: func(int, unix.Signal) error { return unix.ESRCH }}
	if err := process.terminateProcess(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDarwinRetriesTransientResidualGroupCleanup(t *testing.T) {
	var killCalls atomic.Int32
	process := &darwinProcess{
		groupID:     42,
		kqueue:      -1,
		observeExit: func() (bool, error) { return true, nil },
		wait4:       func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error) { return 42, nil },
		kill: func(int, unix.Signal) error {
			if killCalls.Add(1) == 1 {
				return errors.New("secret-canary")
			}
			return nil
		},
	}
	owned := newProcess(process)
	if code, err := waitProcessDeadline(t, owned); code != 0 || err != nil {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	if got := killCalls.Load(); got != 2 {
		t.Fatalf("residual group cleanup calls = %d; want 2", got)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDarwinCloseRetriesTerminationPastDeadlineAndJoinsWaiter(t *testing.T) {
	firstKill := make(chan struct{})
	var killed atomic.Bool
	var killCalls atomic.Int32
	process := &darwinProcess{
		groupID:     42,
		kqueue:      -1,
		observeExit: func() (bool, error) { return killed.Load(), nil },
		wait4:       func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error) { return 42, nil },
		kill: func(int, unix.Signal) error {
			if killCalls.Add(1) == 1 {
				close(firstKill)
				return errors.New("secret-canary")
			}
			killed.Store(true)
			return nil
		},
	}
	owned := newProcess(process)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	closed := make(chan error, 1)
	go func() { closed <- owned.CloseContext(ctx) }()
	awaitDarwinSignal(t, firstKill, "first failed kill")
	select {
	case err := <-closed:
		t.Fatalf("CloseContext returned before retry and waiter join: %v", err)
	default:
	}
	if err := awaitDarwinError(t, closed, "hard cleanup completion"); err != ErrProcess {
		t.Fatalf("CloseContext = %v", err)
	}
	select {
	case <-owned.done:
	default:
		t.Fatal("CloseContext returned before process waiter joined")
	}
	if got := killCalls.Load(); got != 2 {
		t.Fatalf("termination calls = %d; want 2", got)
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
