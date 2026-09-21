package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/platform"
)

func configFile(t *testing.T, text string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.json")
	if err := platform.WritePrivateFile(path, []byte(text)); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadV1(t *testing.T) {
	for _, name := range []string{"名前", strings.Repeat("x", 256), `literal \uD800`, "😀"} {
		want := Config{Version: 1, Profiles: []Profile{{Name: name, ProjectRef: "source-ref", Destination: "s3://backups/prefix", ManagementToken: &credentials.Reference{Env: "SPARC_MANAGEMENT_TOKEN"}}}}
		encoded, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Load(configFile(t, string(encoded)))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip: %#v, %v", got, err)
		}
	}
	for _, text := range []string{`{"version":1,"profiles":[]}`, `{"profiles":[],"version":1}`, `{"version":1,"profiles":[{"name":"\ud83d\ude00"}]}`, `{"version":1,"profiles":[{"name":"replacement character \ufffd"}]}`} {
		if _, err := Load(configFile(t, text)); err != nil {
			t.Fatal(err)
		}
	}
	file := configFile(t, "synthetic")
	refJSON, _ := json.Marshal(file)
	if _, err := Load(configFile(t, `{"version":1,"profiles":[{"name":"local","management_token":{"file":`+string(refJSON)+`}}]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	tests := map[string]string{
		"empty": "", "BOM": "\xef\xbb\xbf" + `{"version":1,"profiles":[]}`, "invalid utf8": `{"version":1,"profiles":[{"name":"` + "\xff" + `"}]}`,
		"unknown root": `{"version":1,"profiles":[],"secret-canary":1}`, "case version": `{"Version":1,"profiles":[]}`,
		"duplicate root": `{"version":1,"version":1,"profiles":[]}`, "escaped duplicate": `{"version":1,"ver\u0073ion":1,"profiles":[]}`,
		"duplicate profile": `{"version":1,"profiles":[{"name":"x","name":"y"}]}`, "duplicate ref": `{"version":1,"profiles":[{"name":"x","management_token":{"env":"A","e\u006ev":"B"}}]}`,
		"unknown profile":   `{"version":1,"profiles":[{"name":"x","password":"secret-canary"}]}`,
		"unknown reference": `{"version":1,"profiles":[{"name":"x","management_token":{"token":"secret-canary"}}]}`,
		"unknown version":   `{"version":2,"profiles":[]}`, "float version": `{"version":1.0,"profiles":[]}`, "exponent version": `{"version":1e0,"profiles":[]}`, "string version": `{"version":"1","profiles":[]}`,
		"null version": `{"version":null,"profiles":[]}`, "null profiles": `{"version":1,"profiles":null}`, "null profile": `{"version":1,"profiles":[null]}`, "null name": `{"version":1,"profiles":[{"name":null}]}`, "numeric name": `{"version":1,"profiles":[{"name":2}]}`, "null ref": `{"version":1,"profiles":[{"name":"x","management_token":null}]}`,
		"missing version": `{"profiles":[]}`, "missing profiles": `{"version":1}`, "missing name": `{"version":1,"profiles":[{}]}`, "empty name": `{"version":1,"profiles":[{"name":""}]}`, "blank name": `{"version":1,"profiles":[{"name":"  "}]}`,
		"empty project": `{"version":1,"profiles":[{"name":"x","project_ref":""}]}`, "blank destination": `{"version":1,"profiles":[{"name":"x","destination":" "}]}`,
		"duplicate names": `{"version":1,"profiles":[{"name":"x"},{"name":"x"}]}`, "two sources": `{"version":1,"profiles":[{"name":"x","management_token":{"env":"A","file":"/file"}}]}`, "empty ref": `{"version":1,"profiles":[{"name":"x","management_token":{}}]}`, "empty extra source": `{"version":1,"profiles":[{"name":"x","management_token":{"env":"A","file":""}}]}`,
		"invalid env": `{"version":1,"profiles":[{"name":"x","management_token":{"env":"1TOKEN"}}]}`, "relative file": `{"version":1,"profiles":[{"name":"x","management_token":{"file":"relative"}}]}`,
		"high surrogate": `{"version":1,"profiles":[{"name":"\ud800"}]}`, "low surrogate": `{"version":1,"profiles":[{"name":"\udc00"}]}`, "bad pair": `{"version":1,"profiles":[{"name":"\ud800\u0041"}]}`,
		"control": `{"version":1,"profiles":[{"name":"secret-canary\u001b"}]}`, "unicode control": `{"version":1,"profiles":[{"name":"secret-canary\u0085"}]}`, "control key": `{"version":1,"profiles":[],"secret-canary\n":1}`,
		"userinfo": `{"version":1,"profiles":[{"name":"x","destination":"https://user:secret-canary@example.test/backup"}]}`, "network path userinfo": `{"version":1,"profiles":[{"name":"x","destination":"//user:secret-canary@example.test/backup"}]}`,
		"trailing": `{"version":1,"profiles":[]} {}`, "root array": `[]`, "nested": `{"version":1,"profiles":[[[[[[[[[[]]]]]]]]]]}`,
		"oversized file":    strings.Repeat(" ", 65537),
		"malformed URI":     `{"version":1,"profiles":[{"name":"x","destination":"https://example.test/%invalid"}]}`,
		"unicode newline":   `{"version":1,"profiles":[{"name":"secret-canary\u2028"}]}`,
		"unicode paragraph": `{"version":1,"profiles":[{"name":"secret-canary\u2029"}]}`,
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := Load(configFile(t, text))
			if err != ErrConfig || !reflect.DeepEqual(got, Config{}) || errors.Unwrap(err) != nil {
				t.Fatalf("got %#v, %v", got, err)
			}
			var out bytes.Buffer
			nested := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", err))
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
				t.Fatal("canary exposed")
			}
		})
	}
}

func TestLoadBounds(t *testing.T) {
	for _, field := range []string{"name", "project_ref", "destination", "env", "file"} {
		limit := 256
		if field == "destination" || field == "file" {
			limit = 4096
		}
		if field == "env" {
			limit = 128
		}
		for _, n := range []int{limit, limit + 1} {
			p := map[string]any{"name": "x"}
			value := strings.Repeat("x", n)
			if field == "file" {
				value = string(os.PathSeparator) + value[1:]
				if os.PathSeparator == '\\' {
					value = `C:\` + value[3:]
				}
			}
			if field == "file" || field == "env" {
				p["management_token"] = map[string]any{field: value}
			} else {
				p[field] = value
			}
			encoded, _ := json.Marshal(map[string]any{"version": 1, "profiles": []any{p}})
			_, err := Load(configFile(t, string(encoded)))
			if (err == nil) != (n == limit) {
				t.Fatalf("%s size %d error=%v", field, n, err)
			}
		}
	}
	for _, n := range []int{32, 33} {
		profiles := make([]Profile, n)
		for i := range profiles {
			profiles[i].Name = fmt.Sprint(i)
		}
		encoded, _ := json.Marshal(Config{Version: 1, Profiles: profiles})
		_, err := Load(configFile(t, string(encoded)))
		if (err == nil) != (n == 32) {
			t.Fatalf("profile bound %d: %v", n, err)
		}
	}
	text := `{"version":1,"profiles":[]}`
	if _, err := Load(configFile(t, text+strings.Repeat(" ", 65536-len(text)))); err != nil {
		t.Fatal("exact file bound", err)
	}
	path := filepath.Join(filepath.Dir(configFile(t, text)), "secret-canary")
	if got, err := Load(path); err != ErrConfig || !reflect.DeepEqual(got, Config{}) {
		t.Fatal("native error disclosed")
	}
}

func TestLoadDestinationMetadataDoesNotResolveReferences(t *testing.T) {
	for _, destination := range []string{`C:\backups\100%@home`, `/backups/100%@home`, `s3://backups/prefix`, `relative%@folder`} {
		want := Config{Version: 1, Profiles: []Profile{{Name: "local", Destination: destination, ManagementToken: &credentials.Reference{Env: "SPARC_TEST_NEVER_RESOLVE"}}}}
		t.Setenv("SPARC_TEST_NEVER_RESOLVE", "secret-canary\n")
		encoded, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Load(configFile(t, string(encoded)))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("metadata rejected or resolved: %v", err)
		}
	}
}
