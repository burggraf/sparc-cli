//go:build darwin

package platform

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	darwinWaitPollInterval = time.Millisecond
	// XNU's exported struct extern_proc ABI defines process states 1 through 5.
	darwinIdleState   = 1
	darwinZombieState = 5
)

// Darwin owns the direct child and its process group. Deliberately detached
// descendants and cleanup after abnormal parent death are outside this native
// mechanism and require separate qualification before operational use.
type darwinProcess struct {
	process *os.Process
	groupID int
	kqueue  int

	mu           sync.Mutex
	exitObserved bool
	reaped       bool
	signaled     bool
	observeExit  func() (bool, error)
	wait4        func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error)
	kill         func(int, unix.Signal) error
	closeFD      func(int) error

	// Test-only per-instance gates; production leaves both nil.
	afterNativeReap func()
	beforeTerminate func()
}

type darwinExitPollOps struct {
	kevent       func(int, []unix.Kevent_t, []unix.Kevent_t, *unix.Timespec) (int, error)
	processState func(int) (int32, int8, error)
}

type darwinStartOps struct {
	kqueue             func() (int, error)
	poll               darwinExitPollOps
	wait4              func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error)
	kill               func(int, unix.Signal) error
	release            func(*os.Process) error
	closeFD            func(int) error
	groupHasOnlyZombie func(int) bool
}

func nativeDarwinStartOps() darwinStartOps {
	return darwinStartOps{
		kqueue: unix.Kqueue,
		poll: darwinExitPollOps{
			kevent: unix.Kevent,
			processState: func(pid int) (int32, int8, error) {
				info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
				if err != nil {
					return 0, 0, err
				}
				return info.Proc.P_pid, info.Proc.P_stat, nil
			},
		},
		wait4: unix.Wait4, kill: unix.Kill,
		release: func(process *os.Process) error { return process.Release() },
		closeFD: unix.Close, groupHasOnlyZombie: darwinGroupHasOnlyZombie,
	}
}

func startNativeProcess(spec ProcessSpec) (nativeProcess, error) {
	arguments := make([]string, 1, len(spec.Args)+1)
	arguments[0] = spec.Path
	arguments = append(arguments, spec.Args...)
	environment := make([]string, len(spec.Env))
	copy(environment, spec.Env)
	process, err := os.StartProcess(spec.Path, arguments, &os.ProcAttr{
		Dir:   spec.Dir,
		Env:   environment,
		Files: []*os.File{spec.Stdin, spec.Stdout, spec.Stderr},
		Sys:   &syscall.SysProcAttr{Setpgid: true},
	})
	if err != nil {
		return nil, err
	}
	return setupDarwinProcess(process, nativeDarwinStartOps())
}

func setupDarwinProcess(process *os.Process, ops darwinStartOps) (nativeProcess, error) {
	kqueue, err := ops.kqueue()
	if err != nil {
		cleanupDarwinStart(process, -1, ops)
		return nil, err
	}
	change := unix.Kevent_t{
		Ident:  uint64(process.Pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}
	if _, err = ops.poll.kevent(kqueue, []unix.Kevent_t{change}, nil, nil); err != nil {
		cleanupDarwinStart(process, kqueue, ops)
		return nil, err
	}

	pid := process.Pid
	return &darwinProcess{
		process: process, groupID: pid, kqueue: kqueue,
		observeExit: newDarwinExitObserver(kqueue, pid, ops.poll),
		wait4:       ops.wait4, kill: ops.kill, closeFD: ops.closeFD,
	}, nil
}

func newDarwinExitObserver(kqueue, pid int, ops darwinExitPollOps) func() (bool, error) {
	fallbackPending := true
	return func() (bool, error) {
		exited, usedFallback, err := pollDarwinExitWithFallback(kqueue, pid, ops, fallbackPending)
		if usedFallback {
			fallbackPending = false
		}
		return exited, err
	}
}

func pollDarwinExit(kqueue, pid int, ops darwinExitPollOps) (bool, error) {
	exited, _, err := pollDarwinExitWithFallback(kqueue, pid, ops, true)
	return exited, err
}

func pollDarwinExitWithFallback(kqueue, pid int, ops darwinExitPollOps, fallbackPending bool) (bool, bool, error) {
	events := make([]unix.Kevent_t, 1)
	timeout := unix.Timespec{}
	n, err := ops.kevent(kqueue, nil, events, &timeout)
	if err != nil {
		return false, false, err
	}
	if n == 1 {
		event := events[0]
		if event.Ident != uint64(pid) || event.Filter != unix.EVFILT_PROC || event.Fflags&unix.NOTE_EXIT == 0 || event.Flags&unix.EV_ERROR != 0 {
			return false, false, ErrProcess
		}
		return true, false, nil
	}
	if n != 0 {
		return false, false, ErrProcess
	}
	if !fallbackPending {
		return false, false, nil
	}

	// A child can exit in the small StartProcess-to-kevent registration window.
	// Darwin does not replay NOTE_EXIT when a filter is attached to that zombie,
	// so consult the same native, nonreaping process state once as a race fallback.
	observedPID, state, err := ops.processState(pid)
	if err != nil {
		return false, true, err
	}
	if observedPID != int32(pid) || state < darwinIdleState || state > darwinZombieState {
		return false, true, ErrProcess
	}
	return state == darwinZombieState, true, nil
}

func cleanupDarwinStart(process *os.Process, kqueue int, ops darwinStartOps) {
	pid := process.Pid
	// The unreaped direct child pins its PID/PGID, so group signaling here cannot
	// target a reused group even if notification setup failed after launch.
	for {
		err := ops.kill(-pid, unix.SIGKILL)
		if err == nil || errors.Is(err, unix.ESRCH) || errors.Is(err, unix.EPERM) && ops.groupHasOnlyZombie(pid) {
			break
		}
		time.Sleep(darwinWaitPollInterval)
	}
	for {
		var status unix.WaitStatus
		_, err := ops.wait4(pid, &status, 0, nil)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	_ = ops.release(process)
	if kqueue >= 0 {
		_ = ops.closeFD(kqueue)
	}
}

func (p *darwinProcess) waitProcess() (int, error) {
	ticker := time.NewTicker(darwinWaitPollInterval)
	defer ticker.Stop()
	waitFailed := false
	for {
		p.mu.Lock()
		if !p.exitObserved {
			exited, err := p.observeExit()
			if errors.Is(err, unix.EINTR) {
				p.mu.Unlock()
				continue
			}
			if err != nil {
				// Notification failure still leaves the direct child unreaped, so
				// fail closed by terminating the pinned group before reaping it.
				waitFailed = true
				p.exitObserved = true
			} else if !exited {
				p.mu.Unlock()
				<-ticker.C
				continue
			} else {
				p.exitObserved = true
			}
		}

		// NOTE_EXIT observes the direct child without reaping it. While that
		// child remains waitable its PID pins the numeric process group, so the
		// residual-group signal cannot target a reused PGID.
		if !p.signaled {
			if err := p.killGroup(); err != nil {
				p.mu.Unlock()
				<-ticker.C
				continue
			}
		}

		var status unix.WaitStatus
		pid, err := p.wait4(p.groupID, &status, 0, nil)
		if errors.Is(err, unix.EINTR) {
			p.mu.Unlock()
			continue
		}
		// From ECHILD onward ownership is uncertain: publish the terminal state
		// under the same mutex before any concurrent termination can proceed.
		p.reaped = true
		if p.afterNativeReap != nil {
			p.afterNativeReap()
		}
		releaseErr := p.releaseProcess()
		closeErr := p.closeKqueue()
		p.mu.Unlock()
		if err != nil || pid != p.groupID || waitFailed || releaseErr != nil || closeErr != nil {
			return -1, ErrProcess
		}
		return status.ExitStatus(), nil
	}
}

func (p *darwinProcess) terminateProcess() error {
	if p.beforeTerminate != nil {
		p.beforeTerminate()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reaped {
		return nil
	}
	return p.killGroup()
}

func (p *darwinProcess) killGroup() error {
	err := p.kill(-p.groupID, unix.SIGKILL)
	if err == nil || errors.Is(err, unix.ESRCH) || p.exitObserved && errors.Is(err, unix.EPERM) && darwinGroupHasOnlyZombie(p.groupID) {
		p.signaled = true
		return nil
	}
	return err
}

func darwinGroupHasOnlyZombie(groupID int) bool {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", groupID)
	return err == nil && len(processes) == 1 && processes[0].Proc.P_pid == int32(groupID) && processes[0].Proc.P_stat == darwinZombieState
}

func (p *darwinProcess) releaseProcess() error {
	if p.process == nil {
		return nil
	}
	err := p.process.Release()
	p.process = nil
	return err
}

func (p *darwinProcess) closeKqueue() error {
	if p.kqueue < 0 || p.closeFD == nil {
		return nil
	}
	err := p.closeFD(p.kqueue)
	p.kqueue = -1
	return err
}

func (*darwinProcess) closeProcess() error { return nil }
