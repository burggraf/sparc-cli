package destination

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestCreatePublishesVerifiedPrivateArchiveWithoutClobber(t *testing.T) {
	parent := privateParent(t)
	final := filepath.Join(parent, "backup")
	manifest, err := Create(final, []archive.Input{{Key: "database/schema.sql", Scope: "database", Source: strings.NewReader("schema")}}, "passphrase")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(manifest.Components) != 1 || platform.CheckPrivateDir(final) != nil {
		t.Fatalf("archive was not published privately: %#v", manifest)
	}
	if _, err := archive.Verify(final, "passphrase"); err != nil {
		t.Fatalf("published archive did not verify: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "backup" {
		t.Fatalf("parent entries after publication = %v", entries)
	}
	if err := os.WriteFile(filepath.Join(final, "sentinel"), []byte("winner"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Create(final, []archive.Input{{Key: "database/other.sql", Scope: "database", Source: bytes.NewReader([]byte("other"))}}, "passphrase")
	if err == nil {
		t.Fatal("Create() replaced an existing archive")
	}
	got, err := os.ReadFile(filepath.Join(final, "sentinel"))
	if err != nil || string(got) != "winner" {
		t.Fatalf("existing archive changed: %q, %v", got, err)
	}
}

func TestCreateRejectsNonPrivateParentWithoutSideEffects(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "ordinary")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(parent, "backup")
	if _, err := Create(final, []archive.Input{{Key: "database/schema.sql", Scope: "database", Source: strings.NewReader("schema")}}, "passphrase"); err == nil {
		t.Fatal("Create() accepted a non-private parent")
	}
	if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
		t.Fatalf("parent changed: %v, %v", entries, err)
	}
}

func TestCreateFailureLeavesNoFinalDirectoryOrStaging(t *testing.T) {
	parent := privateParent(t)
	final := filepath.Join(parent, "backup")
	_, err := Create(final, []archive.Input{{Key: "database/schema.sql", Scope: "database", Source: failingReader{}}}, "passphrase")
	if err == nil {
		t.Fatal("Create() succeeded with a failed source")
	}
	entries, readErr := os.ReadDir(parent)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("failure left staging entries %v, %v", entries, readErr)
	}
}

func privateParent(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "private")
	if err := platform.CreatePrivateDir(parent); err != nil {
		t.Fatal(err)
	}
	return parent
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, os.ErrClosed }
