//go:build windows

package platform

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestProcessWindowsAssignmentAndResumeFailuresCleanUp(t *testing.T) {
	terminationCode := strconv.FormatUint(processTerminationExitCode, 10)
	cleanup := []string{
		"terminate-process:" + terminationCode,
		"wait:" + strconv.Itoa(processCleanupMilliseconds),
		"close:3", "close:2", "close:1",
	}
	assignedCleanup := append([]string{"terminate-job:" + terminationCode}, cleanup...)
	for _, test := range []struct {
		name          string
		assignErr     error
		resume        uint32
		resumeErr     error
		closeError    bool
		resumeBlocked bool
		want          []string
	}{
		{
			name: "existing job assignment", assignErr: errors.New("assign"), resumeBlocked: true,
			want: append([]string{"assign:1:2"}, cleanup...),
		},
		{
			name: "resume error", resumeErr: errors.New("resume"),
			want: append([]string{"assign:1:2", "resume:3"}, assignedCleanup...),
		},
		{
			name: "unexpected suspend count", resume: 2,
			want: append([]string{"assign:1:2", "resume:3"}, assignedCleanup...),
		},
		{
			name: "thread close error", resume: 1, closeError: true,
			want: append([]string{"assign:1:2", "resume:3", "close:3"}, assignedCleanup...),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			resumeCalled := false
			threadCloseCalls := 0
			ops := windowsStartOps{
				assign: func(job, process windows.Handle) error {
					calls = append(calls, "assign:"+handles(job, process))
					return test.assignErr
				},
				resume: func(thread windows.Handle) (uint32, error) {
					resumeCalled = true
					calls = append(calls, "resume:"+strconv.FormatUint(uint64(thread), 10))
					return test.resume, test.resumeErr
				},
				terminateJob: func(job windows.Handle, code uint32) error {
					calls = append(calls, "terminate-job:"+strconv.FormatUint(uint64(code), 10))
					return nil
				},
				terminateProcess: func(process windows.Handle, code uint32) error {
					calls = append(calls, "terminate-process:"+strconv.FormatUint(uint64(code), 10))
					return nil
				},
				wait: func(process windows.Handle, milliseconds uint32) (uint32, error) {
					calls = append(calls, "wait:"+strconv.FormatUint(uint64(milliseconds), 10))
					return windows.WAIT_OBJECT_0, nil
				},
				close: func(handle windows.Handle) error {
					calls = append(calls, "close:"+strconv.FormatUint(uint64(handle), 10))
					if test.closeError && handle == 3 {
						threadCloseCalls++
						if threadCloseCalls == 1 {
							return errors.New("close")
						}
					}
					return nil
				},
			}
			process, err := finishWindowsStart(1, windows.ProcessInformation{Process: 2, Thread: 3}, ops)
			if process != nil || err == nil {
				t.Fatalf("finishWindowsStart = %#v, %v", process, err)
			}
			if test.resumeBlocked && resumeCalled {
				t.Fatal("ResumeThread called after assignment failure")
			}
			if !reflect.DeepEqual(calls, test.want) {
				t.Fatalf("calls = %#v; want exact sequence %#v", calls, test.want)
			}
		})
	}
}

func TestProcessWindowsStartCleanupRetriesUntilExitBeforeClosingHandles(t *testing.T) {
	for _, test := range []struct {
		name       string
		assigned   bool
		firstWait  uint32
		firstError error
	}{
		{name: "unassigned timeout", firstWait: uint32(windows.WAIT_TIMEOUT)},
		{name: "assigned wait error", assigned: true, firstWait: windows.WAIT_FAILED, firstError: errors.New("wait")},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			waitCalls := 0
			confirmedExit := false
			ops := windowsStartOps{
				terminateJob: func(windows.Handle, uint32) error {
					calls = append(calls, "terminate-job")
					return nil
				},
				terminateProcess: func(windows.Handle, uint32) error {
					calls = append(calls, "terminate-process")
					if waitCalls == 0 {
						return errors.New("transient terminate failure")
					}
					return nil
				},
				wait: func(windows.Handle, uint32) (uint32, error) {
					calls = append(calls, "wait")
					waitCalls++
					if waitCalls == 1 {
						return test.firstWait, test.firstError
					}
					confirmedExit = true
					return windows.WAIT_OBJECT_0, nil
				},
				sleep: func(delay time.Duration) {
					if delay != processCleanupRetryDelay {
						t.Fatalf("retry delay = %v", delay)
					}
					calls = append(calls, "sleep")
				},
				close: func(handle windows.Handle) error {
					if !confirmedExit {
						t.Fatalf("handle %d closed before confirmed process exit", handle)
					}
					calls = append(calls, "close:"+strconv.FormatUint(uint64(handle), 10))
					return nil
				},
			}

			cleanupWindowsStartFailure(1, windows.ProcessInformation{Process: 2, Thread: 3}, test.assigned, ops)
			want := []string{"terminate-process", "wait", "sleep", "terminate-process", "wait", "close:3", "close:2", "close:1"}
			if test.assigned {
				want = []string{
					"terminate-job", "terminate-process", "wait", "sleep",
					"terminate-job", "terminate-process", "wait",
					"close:3", "close:2", "close:1",
				}
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls = %#v; want %#v", calls, want)
			}
		})
	}
}

func TestProcessWindowsSuccessfulStartClosesThreadOnly(t *testing.T) {
	var calls []string
	ops := windowsStartOps{
		assign: func(windows.Handle, windows.Handle) error { calls = append(calls, "assign"); return nil },
		resume: func(windows.Handle) (uint32, error) { calls = append(calls, "resume"); return 1, nil },
		close:  func(windows.Handle) error { calls = append(calls, "close-thread"); return nil },
	}
	native, err := finishWindowsStart(1, windows.ProcessInformation{Process: 2, Thread: 3}, ops)
	if err != nil || native == nil || !reflect.DeepEqual(calls, []string{"assign", "resume", "close-thread"}) {
		t.Fatalf("finishWindowsStart = %#v, %v, calls %#v", native, err, calls)
	}
}

func handles(left, right windows.Handle) string {
	return strconv.FormatUint(uint64(left), 10) + ":" + strconv.FormatUint(uint64(right), 10)
}
