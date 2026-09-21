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

const darwinWaitPollInterval = time.Millisecond

// Darwin owns the direct child and its process group. Deliberately detached
// descendants and cleanup after abnormal parent death are outside this native
// mechanism and require separate qualification before operational use.
type darwinProcess struct {
	process *os.Process
	groupID int

	mu     sync.Mutex // Serializes the actual reap with the group-signal decision.
	reaped bool
	wait4  func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error)
	kill   func(int, unix.Signal) error

	// Test-only per-instance gates; production leaves both nil.
	afterNativeReap func()
	beforeTerminate func()
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
	return &darwinProcess{process: process, groupID: process.Pid, wait4: unix.Wait4, kill: unix.Kill}, nil
}

func (p *darwinProcess) waitProcess() (int, error) {
	ticker := time.NewTicker(darwinWaitPollInterval)
	defer ticker.Stop()
	for {
		var status unix.WaitStatus
		p.mu.Lock()
		pid, err := p.wait4(p.groupID, &status, unix.WNOHANG, nil)
		if errors.Is(err, unix.EINTR) {
			p.mu.Unlock()
			continue
		}
		if err != nil {
			if errors.Is(err, unix.ECHILD) {
				p.reaped = true
				p.releaseProcess()
			}
			p.mu.Unlock()
			return -1, err
		}
		if pid == p.groupID {
			if p.afterNativeReap != nil {
				p.afterNativeReap()
			}
			p.reaped = true
			releaseErr := p.releaseProcess()
			p.mu.Unlock()
			if releaseErr != nil {
				return -1, releaseErr
			}
			return status.ExitStatus(), nil
		}
		p.mu.Unlock()
		if pid != 0 {
			return -1, ErrProcess
		}
		<-ticker.C
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
	err := p.kill(-p.groupID, unix.SIGKILL)
	if err == nil || errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func (p *darwinProcess) releaseProcess() error {
	if p.process == nil {
		return nil
	}
	return p.process.Release()
}

func (*darwinProcess) closeProcess() error { return nil }
