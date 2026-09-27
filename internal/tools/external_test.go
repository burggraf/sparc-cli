package tools

import (
	"context"
	"testing"
	"time"
)

func TestRunExternalRejectsInvalidInputs(t *testing.T) {
	request := RunRequest{
		Tool: PGDump, Mode: ModeVersion, Timeout: time.Second, CleanupTimeout: time.Second,
		StdoutLimit: 1, StderrLimit: 1, Stdout: &memorySink{},
	}
	for _, test := range []struct {
		name string
		ctx  context.Context
		bin  string
		req  RunRequest
	}{
		{"nil context", nil, "/tmp/pg/bin", request},
		{"relative bin", context.Background(), "pg/bin", request},
		{"not bin directory", context.Background(), "/tmp/pg/client", request},
		{"invalid request", context.Background(), "/tmp/pg/bin", RunRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := RunExternal(test.ctx, test.bin, test.req); err != ErrRun {
				t.Fatalf("RunExternal() error = %v, want %v", err, ErrRun)
			}
		})
	}
}
