package credentials

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestValidatePGPassEntryAcceptsBoundedLiteralPunctuation(t *testing.T) {
	entry := PGPassEntry{
		Host:     strings.Repeat("h", maxPGPassFieldBytes),
		Port:     5432,
		Database: strings.Repeat("d", maxPGPassFieldBytes),
		User:     strings.Repeat("u", maxPGPassFieldBytes),
		Password: bytes.Repeat([]byte("p"), maxPGPassBytes),
	}
	if err := ValidatePGPassEntry(entry); err != nil {
		t.Fatal(err)
	}
	entry = PGPassEntry{Host: "host*?=:\\", Port: 5432, Database: "database*? :\\", User: "user*? :\\", Password: []byte("password*? :\\")}
	if err := ValidatePGPassEntry(entry); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*PGPassEntry){
		func(value *PGPassEntry) { value.Host += "x" },
		func(value *PGPassEntry) { value.Database += "x" },
		func(value *PGPassEntry) { value.User += "x" },
		func(value *PGPassEntry) { value.Password = append(value.Password, 'x') },
	} {
		value := PGPassEntry{Host: strings.Repeat("h", maxPGPassFieldBytes), Port: 5432, Database: strings.Repeat("d", maxPGPassFieldBytes), User: strings.Repeat("u", maxPGPassFieldBytes), Password: bytes.Repeat([]byte("p"), maxPGPassBytes)}
		edit(&value)
		if err := ValidatePGPassEntry(value); err != ErrPassfile {
			t.Fatalf("oversized value accepted: %v", err)
		}
	}
}

func TestValidatePGPassEntryRejectsSelectorsControlsAndConnectionDatabases(t *testing.T) {
	valid := PGPassEntry{Host: "host", Port: 5432, Database: "database", User: "user", Password: []byte("password")}
	invalid := []PGPassEntry{
		{},
		{Host: "host", Port: 0, Database: "database", User: "user", Password: []byte("password")},
		{Host: "*", Port: 5432, Database: "database", User: "user", Password: []byte("password")},
		{Host: "host", Port: 5432, Database: "*", User: "user", Password: []byte("password")},
		{Host: "host", Port: 5432, Database: "database", User: "*", Password: []byte("password")},
		{Host: "host", Port: 5432, Database: "service=ignored", User: "user", Password: []byte("password")},
		{Host: "host", Port: 5432, Database: "passfile=ignored", User: "user", Password: []byte("password")},
		{Host: "host", Port: 5432, Database: "key=value", User: "user", Password: []byte("password")},
		{Host: "host", Port: 5432, Database: "postgres://host/db", User: "user", Password: []byte("password")},
		{Host: "host", Port: 5432, Database: "POSTGRESQL://host/db", User: "user", Password: []byte("password")},
	}
	for _, field := range []string{"bad\n", "bad\r", "bad\x00", "bad\x1b", "bad\xff"} {
		value := valid
		value.Database = field
		invalid = append(invalid, value)
		value = valid
		value.User = field
		invalid = append(invalid, value)
		value = valid
		value.Password = []byte(field)
		invalid = append(invalid, value)
	}
	for _, entry := range invalid {
		if err := ValidatePGPassEntry(entry); err != ErrPassfile {
			t.Fatalf("invalid entry accepted: %#v, %v", entry, err)
		}
	}
}

func TestPGPassfileWritesExactEscapedLineAndRemoves(t *testing.T) {
	dir := privatePassfileDir(t)
	entry := PGPassEntry{Host: " host:one\\two*? ", Port: 5432, Database: " database:one\\two*? ", User: " user:one\\two*? ", Password: []byte(" password:one\\two*? ")}
	passfile, err := NewPGPassfile(dir, entry)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(passfile.Path()) != dir || !strings.HasPrefix(filepath.Base(passfile.Path()), ".pgpass-") {
		t.Fatalf("unexpected passfile path %q", passfile.Path())
	}
	line, err := platform.ReadPrivateFile(passfile.Path(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	want := " host\\:one\\\\two*? :5432: database\\:one\\\\two*? : user\\:one\\\\two*? : password\\:one\\\\two*? \n"
	if string(line) != want {
		t.Fatalf("line = %q; want %q", line, want)
	}
	if err := passfile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(passfile.Path()); !os.IsNotExist(err) {
		t.Fatalf("passfile remained after Close: %v", err)
	}
	if err := passfile.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPGPassfileConcurrentUniqueFiles(t *testing.T) {
	dir := privatePassfileDir(t)
	const workers = 16
	paths := make(chan string, workers)
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			passfile, err := NewPGPassfile(dir, PGPassEntry{Host: "host", Port: 5432, Database: "database", User: "user", Password: []byte("password")})
			if err == nil {
				paths <- passfile.Path()
				err = passfile.Close()
			}
			errs <- err
		}()
	}
	group.Wait()
	close(paths)
	close(errs)
	seen := make(map[string]bool)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for path := range paths {
		if seen[path] {
			t.Fatalf("duplicate path %q", path)
		}
		seen[path] = true
	}
	if len(seen) != workers {
		t.Fatalf("paths = %d; want %d", len(seen), workers)
	}
}

func TestPGPassfileOwnedWriteSyncAndCloseFailuresRemoveAndRedact(t *testing.T) {
	dir := privatePassfileDir(t)
	entry := PGPassEntry{Host: "host", Port: 5432, Database: "database", User: "user", Password: []byte("secret-canary")}
	for _, test := range []struct {
		name  string
		write func(*os.File, []byte) (int, error)
		sync  func(*os.File) error
		close func(*os.File) error
	}{
		{"write", func(*os.File, []byte) (int, error) { return 0, errors.New("secret-canary") }, (*os.File).Sync, (*os.File).Close},
		{"sync", (*os.File).Write, func(*os.File) error { return errors.New("secret-canary") }, (*os.File).Close},
		{"close", (*os.File).Write, (*os.File).Sync, func(file *os.File) error { _ = file.Close(); return errors.New("secret-canary") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var created string
			create := func(path string) (*os.File, error) {
				created = path
				return platform.CreatePrivateFile(path)
			}
			passfile, err := newPGPassfileWith(dir, entry, bytes.NewReader(bytes.Repeat([]byte{1}, 16)), create, os.Remove, test.write, test.sync, test.close)
			if passfile != nil || err != ErrPassfile || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("failure = %#v, %v", passfile, err)
			}
			if _, err := os.Lstat(created); !os.IsNotExist(err) {
				t.Fatalf("owned passfile remained: %v", err)
			}
		})
	}
}

func TestPGPassfileCollisionExhaustionAndCleanupFailureAreRedacted(t *testing.T) {
	dir := privatePassfileDir(t)
	entry := PGPassEntry{Host: "host", Port: 5432, Database: "database", User: "user", Password: []byte("secret-canary")}
	calls := 0
	passfile, err := newPGPassfileWith(dir, entry, bytes.NewReader(bytes.Repeat([]byte{1}, 16*16)), func(string) (*os.File, error) {
		calls++
		return nil, os.ErrExist
	}, os.Remove, (*os.File).Write, (*os.File).Sync, (*os.File).Close)
	if passfile != nil || err != ErrPassfile || calls != 16 {
		t.Fatalf("collision exhaustion = %#v, %v, calls=%d", passfile, err, calls)
	}
	passfile, err = newPGPassfileWith(dir, entry, bytes.NewReader(bytes.Repeat([]byte{2}, 16)), platform.CreatePrivateFile, func(string) error { return errors.New("secret-canary") }, (*os.File).Write, (*os.File).Sync, (*os.File).Close)
	if err != nil {
		t.Fatal(err)
	}
	if err := passfile.Close(); err != ErrPassfile || strings.Contains(err.Error(), "secret-canary") {
		t.Fatalf("cleanup failure = %v", err)
	}
}

func privatePassfileDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}
