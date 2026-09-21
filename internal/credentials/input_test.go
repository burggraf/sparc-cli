package credentials

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/platform"
	"golang.org/x/term"
)

func privateFile(t *testing.T, data []byte) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "input")
	if err := platform.WritePrivateFile(path, data); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReferenceValidate(t *testing.T) {
	for _, ref := range []Reference{{}, {Env: "TOKEN", File: "/file"}, {Env: "1TOKEN"}, {Env: "TOKEN=secret-canary"}, {Env: "TÖKEN"}, {Env: strings.Repeat("X", 129)}, {File: "relative"}, {File: "/tmp/../file"}, {File: "/secret-canary\n"}, {File: "/bad\xff"}, {File: "/" + strings.Repeat("x", 4096)}} {
		if err := ref.Validate(); err != ErrInput {
			t.Fatalf("invalid ref accepted: %v", err)
		}
	}
	for _, ref := range []Reference{{Env: "_TOKEN_2"}, {Env: strings.Repeat("X", 128)}, {File: privateFile(t, nil)}} {
		if err := ref.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInputExplicitSources(t *testing.T) {
	t.Setenv("SPARC_TEST_SECRET", "  café 日本語  ")
	t.Setenv("SPARC_AMBIENT_SECRET", "secret-canary")
	ref := Reference{Env: "SPARC_TEST_SECRET"}
	var out bytes.Buffer
	got, err := Input(&ref, nil, &out)
	if err != nil || string(got) != "  café 日本語  " || out.Len() != 0 {
		t.Fatalf("explicit env: %v", err)
	}
	for _, suffix := range []string{"", "\n", "\r\n"} {
		ref = Reference{File: privateFile(t, []byte("  café 日本語  "+suffix))}
		got, err = Input(&ref, nil, &out)
		if err != nil || string(got) != "  café 日本語  " {
			t.Fatalf("file suffix: %v", err)
		}
	}
	ref = Reference{Env: "SPARC_MISSING_TEST_SECRET"}
	t.Setenv(ref.Env, "")
	if got, err := Input(&ref, nil, &out); err != ErrInput || got != nil {
		t.Fatal("ambient fallback")
	}
	if got, err := Input(nil, nil, &out); err != ErrInput || got != nil {
		t.Fatal("nil terminal accepted")
	}
}

func TestInputBoundsAndControls(t *testing.T) {
	values := []string{"", "secret-canary\n", "secret-canary\r", "secret-canary\x00", "secret-canary\x1b", "secret-canary\u0085", "secret-canary\u2028", "secret-canary\u2029", "bad\xff", strings.Repeat("x", 16385)}
	for _, value := range values {
		// Environment variables cannot contain NUL, so exercise that case through files.
		if !strings.ContainsRune(value, 0) {
			t.Setenv("SPARC_TEST_SECRET", value)
			if got, err := Input(&Reference{Env: "SPARC_TEST_SECRET"}, nil, io.Discard); err != ErrInput || got != nil {
				t.Fatal("invalid env accepted")
			}
		}
		fileValue := value + "\n"
		if strings.HasSuffix(value, "\r") {
			fileValue += "\n"
		} // A single CRLF is an allowed terminator.
		ref := Reference{File: privateFile(t, []byte(fileValue))}
		if got, err := Input(&ref, nil, io.Discard); err != ErrInput || got != nil {
			t.Fatal("invalid file accepted")
		}
	}
	for _, suffix := range []string{"", "\n", "\r\n"} {
		ref := Reference{File: privateFile(t, []byte(strings.Repeat("x", 16384)+suffix))}
		if got, err := Input(&ref, nil, io.Discard); err != nil || len(got) != 16384 {
			t.Fatalf("limit rejected: %v", err)
		}
	}
}

func TestInputNonTTYDoesNotReadOrPrint(t *testing.T) {
	f, err := os.Open(privateFile(t, []byte("secret-canary")))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out bytes.Buffer
	got, err := Input(nil, f, &out)
	pos, _ := f.Seek(0, io.SeekCurrent)
	if err != ErrInput || got != nil || out.Len() != 0 || pos != 0 {
		t.Fatal("non-TTY consumed or printed input")
	}
}

func TestReadTerminalBoundedLine(t *testing.T) {
	tests := []struct {
		name, input, want string
		err               error
	}{
		{"spaces", "  café 日本語  \n", "  café 日本語  ", nil},
		{"carriage enter", "value\r", "value", nil},
		{"rune backspace", "a日\b本\x7f語\n", "a語", nil},
		{"empty", "\n", "", ErrInput},
		{"invalid UTF8", "bad\xff\n", "", ErrInput},
		{"control", "secret-canary\x1b\n", "", ErrInput},
		{"unicode control", "secret-canary\u0085\n", "", ErrInput},
		{"unicode newline", "secret-canary\u2028\n", "", ErrInput},
		{"cancel", "secret-canary\x03", "", ErrCancelled},
		{"eof key", "secret-canary\x04", "", ErrCancelled},
		{"eof", "secret-canary", "", ErrInput},
		{"limit", strings.Repeat("x", 16384) + "\n", strings.Repeat("x", 16384), nil},
		{"overflow drain", strings.Repeat("x", 16385) + "\bextra\n", "", ErrInput},
		{"overflow ignores cancellation", strings.Repeat("x", 16385) + "\x03secret-canary\x04\n", "", ErrInput},
		{"invalid ignores cancellation", "secret-canary\x1b\x03remainder\n", "", ErrInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := strings.NewReader(test.input)
			got, err := readTerminal(r)
			if err != test.err || string(got) != test.want || (err != nil && got != nil) {
				t.Fatalf("result error=%v", err)
			}
			if r.Len() != 0 {
				t.Fatal("line not drained")
			}
		})
	}
	if got, err := readTerminal(failingReader{}); err != ErrInput || got != nil {
		t.Fatal("reader error leaked")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("secret-canary") }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("secret-canary") }

func TestHiddenInputRestoresAndRedacts(t *testing.T) {
	for _, tc := range []struct {
		name, input            string
		setup, restore, output bool
		want                   error
	}{
		{name: "success", input: "value\n"}, {name: "cancel", input: "secret-canary\x03", want: ErrCancelled},
		{name: "overlong drains after cancel", input: strings.Repeat("x", 16385) + "\x03secret-canary\n", want: ErrInput},
		{name: "read error", input: "secret-canary", want: ErrInput}, {name: "setup error", setup: true, want: ErrInput},
		{name: "restore error", input: "secret-canary\n", restore: true, want: ErrInput}, {name: "output error", output: true, want: ErrInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := os.Open(privateFile(t, []byte(tc.input)))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			restored := false
			raw := false
			makeRaw := func(int) (*term.State, error) {
				raw = true
				if tc.setup {
					return nil, errors.New("secret-canary")
				}
				return &term.State{}, nil
			}
			restore := func(int, *term.State) error {
				restored = true
				if tc.restore {
					return errors.New("secret-canary")
				}
				return nil
			}
			var out bytes.Buffer
			var w io.Writer = &out
			if tc.output {
				w = failingWriter{}
			}
			got, err := readHidden(f, w, makeRaw, restore)
			if err != tc.want || (err != nil && got != nil) {
				t.Fatalf("error=%v", err)
			}
			if raw && !tc.setup && !restored {
				t.Fatal("terminal not restored")
			}
			if !tc.setup && !tc.output {
				wantOutput := "Secret: \n"
				if tc.restore {
					wantOutput = "Secret: "
				}
				if out.String() != wantOutput {
					t.Fatalf("output=%q", out.String())
				}
			}
		})
	}
}

func TestInputErrorsRemainRedacted(t *testing.T) {
	for _, err := range []error{ErrInput, ErrCancelled} {
		if errors.Unwrap(err) != nil {
			t.Fatal("native error retained")
		}
		nested := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", err))
		var out bytes.Buffer
		fmt.Fprintf(&out, "%v %+v %#v", nested, nested, nested)
		log.New(&out, "", 0).Print(nested)
		encoded, e := json.Marshal(struct {
			Error string `json:"error"`
		}{nested.Error()})
		if e != nil {
			t.Fatal(e)
		}
		out.Write(encoded)
		if strings.Contains(out.String(), "secret-canary") {
			t.Fatal("canary leaked")
		}
	}
	ref := Reference{File: filepath.Join(filepath.Dir(privateFile(t, nil)), "secret-canary")}
	if got, err := Input(&ref, nil, io.Discard); err != ErrInput || got != nil {
		t.Fatal("native error not redacted")
	}
}

type outputFailure struct {
	calls, failAt int
	short         bool
}

func (w *outputFailure) Write(data []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		if w.short {
			return 0, nil
		}
		return 0, errors.New("secret-canary")
	}
	return len(data), nil
}

func assertRedacted(t *testing.T, err error) {
	t.Helper()
	nested := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", err))
	var out bytes.Buffer
	fmt.Fprintf(&out, "%v %+v %#v", nested, nested, nested)
	log.New(&out, "", 0).Print(nested)
	encoded, e := json.Marshal(map[string]string{"error": nested.Error()})
	if e != nil {
		t.Fatal(e)
	}
	out.Write(encoded)
	if strings.Contains(out.String(), "secret-canary") {
		t.Fatal("secret present in public error chain")
	}
}

func TestHiddenInputOutputFailureRestores(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		for _, short := range []bool{false, true} {
			f, err := os.Open(privateFile(t, []byte("secret-canary\n")))
			if err != nil {
				t.Fatal(err)
			}
			restored := false
			got, err := readHidden(f, &outputFailure{failAt: failAt, short: short}, func(int) (*term.State, error) { return &term.State{}, nil }, func(int, *term.State) error { restored = true; return nil })
			f.Close()
			if err != ErrInput || got != nil || !restored {
				t.Fatal("output failure did not restore/discard secret")
			}
			assertRedacted(t, err)
		}
	}
}

func TestInputMissingSourceAndPipeRefusal(t *testing.T) {
	const name = "SPARC_TEST_ABSENT_SECRET"
	t.Setenv(name, "placeholder")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	got, err := Input(&Reference{Env: name}, nil, io.Discard)
	if got != nil || err != ErrInput {
		t.Fatal("absent env accepted")
	}
	assertRedacted(t, err)
	ref := Reference{Env: "secret-canary=invalid"}
	got, err = Input(&ref, nil, io.Discard)
	if got != nil || err != ErrInput {
		t.Fatal("invalid env accepted")
	}
	assertRedacted(t, err)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := w.Write([]byte("secret-canary")); err != nil {
		t.Fatal(err)
	}
	w.Close()
	var out bytes.Buffer
	got, err = Input(nil, r, &out)
	if got != nil || err != ErrInput || out.Len() != 0 {
		t.Fatal("pipe input accepted")
	}
	rest, err := io.ReadAll(r)
	if err != nil || string(rest) != "secret-canary" {
		t.Fatal("pipe consumed")
	}
}

type hiddenEventWriter func([]byte) (int, error)

func (w hiddenEventWriter) Write(data []byte) (int, error) { return w(data) }

func TestHiddenInputRestoresBeforeNewline(t *testing.T) {
	for _, tc := range []struct {
		name, input, wantEvents string
		failWrite, failRestores int
		want                    error
	}{
		{name: "success", input: "secret-canary\n", wantEvents: "prompt,read,restore,newline"},
		{name: "read failure", input: "secret-canary", wantEvents: "prompt,read,restore,newline", want: ErrInput},
		{name: "cancel", input: "secret-canary\x03", wantEvents: "prompt,read,restore,newline", want: ErrCancelled},
		{name: "prompt failure", input: "secret-canary\n", failWrite: 1, wantEvents: "prompt,restore", want: ErrInput},
		{name: "newline failure", input: "secret-canary\n", failWrite: 2, wantEvents: "prompt,read,restore,newline", want: ErrInput},
		{name: "fallback succeeds", input: "secret-canary\n", failRestores: 1, wantEvents: "prompt,read,restore,restore", want: ErrInput},
		{name: "fallback fails", input: "secret-canary\n", failRestores: 2, wantEvents: "prompt,read,restore,restore", want: ErrInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := os.Open(privateFile(t, []byte(tc.input)))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var events []string
			writes, restores := 0, 0
			restored := false
			writer := hiddenEventWriter(func(data []byte) (int, error) {
				writes++
				if writes == 1 {
					offset, err := f.Seek(0, io.SeekCurrent)
					if err != nil || offset != 0 || string(data) != "Secret: " {
						t.Fatal("prompt must precede read")
					}
					events = append(events, "prompt")
				} else {
					if !restored || string(data) != "\n" {
						t.Fatal("newline written before successful restoration")
					}
					events = append(events, "newline")
				}
				if writes == tc.failWrite {
					return 0, errors.New("secret-canary")
				}
				return len(data), nil
			})
			restore := func(int, *term.State) error {
				restores++
				offset, err := f.Seek(0, io.SeekCurrent)
				if err != nil {
					t.Fatal(err)
				}
				if tc.failWrite == 1 {
					if offset != 0 {
						t.Fatal("read after failed prompt")
					}
				} else {
					if offset != int64(len(tc.input)) {
						t.Fatal("restoration preceded completed read")
					}
					if restores == 1 {
						events = append(events, "read")
					}
				}
				events = append(events, "restore")
				if restores <= tc.failRestores {
					return errors.New("secret-canary")
				}
				restored = true
				return nil
			}
			got, err := readHidden(f, writer, func(int) (*term.State, error) { return &term.State{}, nil }, restore)
			if err != tc.want || (err != nil && got != nil) {
				t.Fatalf("error=%v, result should be discarded on failure", err)
			}
			if err == nil && string(got) != "secret-canary" {
				t.Fatal("successful value lost")
			}
			if got := strings.Join(events, ","); got != tc.wantEvents {
				t.Fatalf("events=%s, want %s", got, tc.wantEvents)
			}
			if err != nil {
				assertRedacted(t, err)
			}
		})
	}
}
