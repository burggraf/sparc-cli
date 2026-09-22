//go:build windows

package platform

import (
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	processTerminationExitCode = 0x53504152 // "SPAR"
	processCleanupMilliseconds = 5000
	processCleanupRetryDelay   = 10 * time.Millisecond
)

type windowsStartOps struct {
	assign           func(windows.Handle, windows.Handle) error
	resume           func(windows.Handle) (uint32, error)
	terminateJob     func(windows.Handle, uint32) error
	terminateProcess func(windows.Handle, uint32) error
	wait             func(windows.Handle, uint32) (uint32, error)
	sleep            func(time.Duration)
	close            func(windows.Handle) error
}

func defaultWindowsStartOps() windowsStartOps {
	return windowsStartOps{
		assign: windows.AssignProcessToJobObject, resume: windows.ResumeThread,
		terminateJob: windows.TerminateJobObject, terminateProcess: windows.TerminateProcess,
		wait: windows.WaitForSingleObject, sleep: time.Sleep, close: windows.CloseHandle,
	}
}

type windowsProcess struct {
	process windows.Handle
	job     windows.Handle
}

func startNativeProcess(spec ProcessSpec) (nativeProcess, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	runtime.KeepAlive(&limits)

	standard, err := duplicateStandardHandles(spec)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	closeStandard := func() error {
		var closeErr error
		failed := standard[:0]
		for _, handle := range standard {
			if err := windows.CloseHandle(handle); err != nil {
				closeErr = err
				failed = append(failed, handle)
			}
		}
		standard = failed
		return closeErr
	}
	defer closeStandard() // Retry only handles whose close reported failure.

	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		_ = closeStandard()
		_ = windows.CloseHandle(job)
		return nil, err
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&standard[0]), uintptr(len(standard))*unsafe.Sizeof(standard[0])); err != nil {
		_ = closeStandard()
		_ = windows.CloseHandle(job)
		return nil, err
	}

	application, err := windows.UTF16PtrFromString(spec.Path)
	if err != nil {
		_ = closeStandard()
		_ = windows.CloseHandle(job)
		return nil, err
	}
	arguments := append([]string{spec.Path}, spec.Args...)
	commandLine, err := windows.UTF16FromString(windows.ComposeCommandLine(arguments))
	if err != nil {
		_ = closeStandard()
		_ = windows.CloseHandle(job)
		return nil, err
	}
	directory, err := windows.UTF16PtrFromString(spec.Dir)
	if err != nil {
		_ = closeStandard()
		_ = windows.CloseHandle(job)
		return nil, err
	}
	environment, err := windowsEnvironment(spec.Env)
	if err != nil {
		_ = closeStandard()
		_ = windows.CloseHandle(job)
		return nil, err
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  standard[0],
			StdOutput: standard[1],
			StdErr:    standard[2],
		},
		ProcThreadAttributeList: attributes.List(),
	}
	information := windows.ProcessInformation{}
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_DEFAULT_ERROR_MODE | windows.EXTENDED_STARTUPINFO_PRESENT)
	err = windows.CreateProcess(application, &commandLine[0], nil, nil, true, flags, &environment[0], directory, &startup.StartupInfo, &information)
	standardCloseErr := closeStandard()
	runtime.KeepAlive(spec.Stdin)
	runtime.KeepAlive(spec.Stdout)
	runtime.KeepAlive(spec.Stderr)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	ops := defaultWindowsStartOps()
	if standardCloseErr != nil {
		cleanupWindowsStartFailure(job, information, false, ops)
		return nil, standardCloseErr
	}
	return finishWindowsStart(job, information, ops)
}

func duplicateStandardHandles(spec ProcessSpec) ([]windows.Handle, error) {
	current, err := windows.GetCurrentProcess()
	if err != nil {
		return nil, err
	}
	sources := []*os.File{spec.Stdin, spec.Stdout, spec.Stderr}
	handles := make([]windows.Handle, 0, len(sources))
	for _, file := range sources {
		var duplicate windows.Handle
		if err := windows.DuplicateHandle(current, windows.Handle(file.Fd()), current, &duplicate, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			for _, handle := range handles {
				_ = windows.CloseHandle(handle)
			}
			return nil, err
		}
		handles = append(handles, duplicate)
	}
	return handles, nil
}

func windowsEnvironment(entries []string) ([]uint16, error) {
	ordered := append([]string(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool {
		left, right := strings.ToUpper(ordered[i]), strings.ToUpper(ordered[j])
		if left == right {
			return ordered[i] < ordered[j]
		}
		return left < right
	})
	block := make([]uint16, 0, 2)
	for _, entry := range ordered {
		encoded, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, err
		}
		block = append(block, encoded...)
	}
	block = append(block, 0)
	if len(ordered) == 0 {
		block = append(block, 0)
	}
	return block, nil
}

func finishWindowsStart(job windows.Handle, information windows.ProcessInformation, ops windowsStartOps) (nativeProcess, error) {
	if err := ops.assign(job, information.Process); err != nil {
		cleanupWindowsStartFailure(job, information, false, ops)
		return nil, err
	}
	previous, err := ops.resume(information.Thread)
	if err != nil || previous != 1 {
		cleanupWindowsStartFailure(job, information, true, ops)
		if err != nil {
			return nil, err
		}
		return nil, windows.ERROR_INVALID_STATE
	}
	if err := ops.close(information.Thread); err != nil {
		cleanupWindowsStartFailure(job, information, true, ops)
		return nil, err
	}
	return &windowsProcess{process: information.Process, job: job}, nil
}

func cleanupWindowsStartFailure(job windows.Handle, information windows.ProcessInformation, assigned bool, ops windowsStartOps) {
	if information.Process != 0 {
		for {
			if assigned {
				_ = ops.terminateJob(job, processTerminationExitCode)
			}
			_ = ops.terminateProcess(information.Process, processTerminationExitCode)
			result, err := ops.wait(information.Process, processCleanupMilliseconds)
			if err == nil && result == windows.WAIT_OBJECT_0 {
				break
			}
			ops.sleep(processCleanupRetryDelay)
		}
	}
	if information.Thread != 0 {
		_ = ops.close(information.Thread)
	}
	if information.Process != 0 {
		_ = ops.close(information.Process)
	}
	_ = ops.close(job)
}

func (p *windowsProcess) waitProcess() (int, error) {
	result, waitErr := windows.WaitForSingleObject(p.process, windows.INFINITE)
	var code uint32
	exitErr := error(nil)
	if waitErr == nil && result == windows.WAIT_OBJECT_0 {
		exitErr = windows.GetExitCodeProcess(p.process, &code)
	}
	terminateErr := windows.TerminateJobObject(p.job, processTerminationExitCode)
	closeErr := windows.CloseHandle(p.process)
	if waitErr != nil || result != windows.WAIT_OBJECT_0 || exitErr != nil || terminateErr != nil || closeErr != nil {
		return -1, ErrProcess
	}
	return int(code), nil
}

func (p *windowsProcess) terminateProcess() error {
	return windows.TerminateJobObject(p.job, processTerminationExitCode)
}

func (p *windowsProcess) closeProcess() error {
	return windows.CloseHandle(p.job)
}
