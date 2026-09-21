package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// ErrProcess is returned for all process-start, ownership, wait, termination,
// and handle-lifecycle failures. Native errors and process inputs are never
// exposed through this boundary.
var ErrProcess = errors.New("process unavailable")

const processCloseTimeout = 5 * time.Second

type ProcessSpec struct {
	Path                  string
	Args, Env             []string
	Dir                   string
	Stdin, Stdout, Stderr *os.File
}

type nativeProcess interface {
	waitProcess() (int, error)
	terminateProcess() error
	closeProcess() error
}

type Process struct {
	native nativeProcess
	done   chan struct{}

	resultMu  sync.Mutex
	exitCode  int
	waitErr   error
	terminate sync.Once
	termErr   error
	close     sync.Once
	closeErr  error
}

func StartProcess(spec ProcessSpec) (*Process, error) {
	if !validProcessSpec(spec) {
		return nil, ErrProcess
	}
	native, err := startNativeProcess(spec)
	if err != nil {
		return nil, ErrProcess
	}
	return newProcess(native), nil
}

func newProcess(native nativeProcess) *Process {
	process := &Process{native: native, done: make(chan struct{}), exitCode: -1}
	go func() {
		code, err := native.waitProcess()
		process.resultMu.Lock()
		process.exitCode = code
		if err != nil {
			process.exitCode = -1
			process.waitErr = ErrProcess
		}
		process.resultMu.Unlock()
		close(process.done)
	}()
	return process
}

func (p *Process) Wait() (int, error) {
	if p == nil || p.native == nil {
		return -1, ErrProcess
	}
	<-p.done
	p.resultMu.Lock()
	defer p.resultMu.Unlock()
	return p.exitCode, p.waitErr
}

func (p *Process) Terminate(ctx context.Context) error {
	if p == nil || p.native == nil || ctx == nil {
		return ErrProcess
	}
	p.terminateNative()
	if p.termErr != nil {
		return ErrProcess
	}
	select {
	case <-p.done:
		if p.processWaitErr() != nil {
			return ErrProcess
		}
		return nil
	case <-ctx.Done():
		return ErrProcess
	}
}

func (p *Process) Close() error {
	if p == nil || p.native == nil {
		return ErrProcess
	}
	p.close.Do(func() {
		p.terminateNative()
		ctx, cancel := context.WithTimeout(context.Background(), processCloseTimeout)
		defer cancel()
		waitErr := error(nil)
		select {
		case <-p.done:
			waitErr = p.processWaitErr()
		case <-ctx.Done():
			waitErr = ErrProcess
		}
		nativeCloseErr := p.native.closeProcess()
		if p.termErr != nil || waitErr != nil || nativeCloseErr != nil {
			p.closeErr = ErrProcess
		}
	})
	return p.closeErr
}

func (p *Process) processWaitErr() error {
	p.resultMu.Lock()
	defer p.resultMu.Unlock()
	return p.waitErr
}

func (p *Process) terminateNative() {
	p.terminate.Do(func() {
		if err := p.native.terminateProcess(); err != nil {
			p.termErr = ErrProcess
		}
	})
}

func validProcessSpec(spec ProcessSpec) bool {
	if !validPath(spec.Path) || !validPath(spec.Dir) || spec.Stdin == nil || spec.Stdout == nil || spec.Stderr == nil {
		return false
	}
	for _, argument := range spec.Args {
		if !validProcessText(argument) {
			return false
		}
	}
	seen := make(map[string]bool, len(spec.Env))
	for _, entry := range spec.Env {
		key, value, ok := strings.Cut(entry, "=")
		folded := strings.ToUpper(key)
		if !ok || !validProcessEnvKey(key) || !validProcessText(value) || seen[folded] {
			return false
		}
		seen[folded] = true
	}
	return true
}

func validProcessEnvKey(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		character := value[i]
		if character != '_' && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (i == 0 || character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validProcessText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
