package cli

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
)

// roundTripperFunc adapts a function for an isolated standard HTTP check.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestRunCommandContract(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "help",
			args:       []string{"help"},
			wantCode:   0,
			wantStdout: "Usage: sparc <command>\n\nCommands:\n  backup   Back up a Supabase project (not yet available)\n  verify   Verify a backup (not yet available)\n  restore  Restore a backup (not yet available)\n\nRun \"sparc <command> --help\" for more information when commands become available.\n",
		},
		{
			name:       "help flag",
			args:       []string{"--help"},
			wantCode:   0,
			wantStdout: "Usage: sparc <command>\n\nCommands:\n  backup   Back up a Supabase project (not yet available)\n  verify   Verify a backup (not yet available)\n  restore  Restore a backup (not yet available)\n\nRun \"sparc <command> --help\" for more information when commands become available.\n",
		},
		{
			name:       "version",
			args:       []string{"version"},
			wantCode:   0,
			wantStdout: "sparc dev\n",
		},
		{
			name:       "version flag",
			args:       []string{"--version"},
			wantCode:   0,
			wantStdout: "sparc dev\n",
		},
		{
			name:       "backup unavailable",
			args:       []string{"backup"},
			wantCode:   1,
			wantStderr: "sparc: backup is not available in this scaffold\n",
		},
		{
			name:       "verify unavailable",
			args:       []string{"verify"},
			wantCode:   1,
			wantStderr: "sparc: verify is not available in this scaffold\n",
		},
		{
			name:       "restore unavailable",
			args:       []string{"restore"},
			wantCode:   1,
			wantStderr: "sparc: restore is not available in this scaffold\n",
		},
		{
			name:       "unknown command",
			args:       []string{"archive"},
			wantCode:   2,
			wantStderr: "sparc: unknown command\nRun \"sparc help\" for usage.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := Run(tt.args, strings.NewReader(""), &stdout, &stderr); got != tt.wantCode {
				t.Errorf("Run(%q) exit code = %d, want %d", tt.args, got, tt.wantCode)
			}
			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("Run(%q) stdout = %q, want %q", tt.args, got, tt.wantStdout)
			}
			if got := stderr.String(); got != tt.wantStderr {
				t.Errorf("Run(%q) stderr = %q, want %q", tt.args, got, tt.wantStderr)
			}
		})
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestRunReturnsFailureWhenStdoutWriteFails(t *testing.T) {
	sentinel := errors.New("stdout writer sentinel")
	tests := []struct {
		name string
		args []string
	}{
		{name: "no args", args: nil},
		{name: "help", args: []string{"help"}},
		{name: "version", args: []string{"version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := Run(tt.args, strings.NewReader(""), errorWriter{err: sentinel}, &stderr); got != 1 {
				t.Errorf("Run(%q) exit code = %d, want 1", tt.args, got)
			}
			const wantStderr = "sparc: unable to write command output\n"
			if got := stderr.String(); got != wantStderr {
				t.Errorf("Run(%q) stderr = %q, want %q", tt.args, got, wantStderr)
			}
			if strings.Contains(stderr.String(), sentinel.Error()) {
				t.Errorf("Run(%q) exposed writer error", tt.args)
			}
		})
	}
}

func TestRunRejectsExtraArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "help", args: []string{"help", "extra"}},
		{name: "version", args: []string{"version", "extra"}},
		{name: "backup option", args: []string{"backup", "--to", "/backup"}},
		{name: "verify argument", args: []string{"verify", "/backup"}},
		{name: "restore option", args: []string{"restore", "--project", "target"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := Run(tt.args, strings.NewReader(""), &stdout, &stderr); got != 2 {
				t.Errorf("Run(%q) exit code = %d, want 2", tt.args, got)
			}
			if got := stdout.String(); got != "" {
				t.Errorf("Run(%q) stdout = %q, want empty", tt.args, got)
			}
			const wantStderr = "sparc: this command takes no arguments in this scaffold\nRun \"sparc help\" for usage.\n"
			if got := stderr.String(); got != wantStderr {
				t.Errorf("Run(%q) stderr = %q, want %q", tt.args, got, wantStderr)
			}
		})
	}
}

// This observes only the current directory and standard HTTP default transport.
func TestRunHelpAndVersionHaveNoObservedSideEffects(t *testing.T) {
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Error(err)
		}
	})

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("sentinel", []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		panic("help/version attempted an HTTP request through the default transport")
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	for _, args := range [][]string{{"help"}, {"version"}} {
		var stdout, stderr bytes.Buffer
		if got := Run(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
			t.Errorf("Run(%q) exit code = %d, want 0", args, got)
		}
		if stderr.Len() != 0 {
			t.Errorf("Run(%q) wrote stderr: %q", args, stderr.String())
		}
	}

	contents, err := os.ReadFile("sentinel")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != "unchanged" {
		t.Errorf("sentinel contents = %q, want unchanged", got)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "sentinel" {
		t.Errorf("working directory entries = %v, want only sentinel", entries)
	}
}

func TestRunUnknownCommandDoesNotDiscloseInput(t *testing.T) {
	for _, arg := range []string{"secret-canary", "\x1b[31msecret-canary\n"} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{arg}, strings.NewReader(""), &stdout, &stderr); code != 2 {
			t.Fatalf("exit = %d", code)
		}
		if stdout.Len() != 0 || stderr.String() != "sparc: unknown command\nRun \"sparc help\" for usage.\n" {
			t.Fatal("unexpected public output")
		}
		if strings.Contains(stderr.String(), "secret-canary") || strings.Contains(stderr.String(), "\x1b") {
			t.Fatal("input disclosed")
		}
	}
}
