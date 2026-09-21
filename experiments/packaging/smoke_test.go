package packaging

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const syntheticHelperSource = `package main

import "fmt"

func main() { fmt.Println("synthetic-helper 1") }
`

func TestOptInEmbeddedSmoke(t *testing.T) {
	if os.Getenv("SPARC_PACKAGING_SMOKE") != "1" {
		t.Skip("set SPARC_PACKAGING_SMOKE=1 to run the macOS embed/extract smoke")
	}
	work := t.TempDir()
	goEnvironment, err := offlineGoEnvironment(work)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skipf("macOS arm64 experiment, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	for _, tool := range []string{"go", "codesign", "xattr", "file"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required already-installed tool %q is absent: %v", tool, err)
		}
	}

	helper := filepath.Join(work, "synthetic-helper")
	if err := os.WriteFile(filepath.Join(work, "helper.go"), []byte(syntheticHelperSource), 0o600); err != nil {
		t.Fatal(err)
	}
	runCommand(t, work, goEnvironment, "go", "build", "-o", helper, "helper.go")
	runCommand(t, work, nil, "codesign", "--force", "--sign", "-", helper)
	runCommand(t, work, nil, "codesign", "--verify", "--strict", helper)

	original, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	expected := hash(original)
	adjacentOutput, err := RunVerified(helper, expected, nil, sanitizedEnvironment(work))
	if err != nil {
		t.Fatalf("adjacent helper execution: %v", err)
	}
	if string(adjacentOutput) != "synthetic-helper 1\n" {
		t.Fatalf("adjacent helper output = %q", adjacentOutput)
	}

	currentDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join(currentDirectory, "../.."))
	outerDir, err := os.MkdirTemp(filepath.Join(repositoryRoot, "experiments", "packaging"), ".smoke-outer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outerDir)
	if err := os.WriteFile(filepath.Join(outerDir, "helper"), original, 0o700); err != nil {
		t.Fatal(err)
	}
	outerSource := fmt.Sprintf(`package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	packaging "github.com/burggraf/sparc-cli/experiments/packaging"
)

//go:embed helper
var helper []byte

const expected = %q

func main() {
	parent := os.Args[1]
	root, err := os.MkdirTemp(parent, "embedded-private-")
	if err != nil { panic(err) }
	path, err := packaging.Extract(helper, expected, filepath.Join(root, "payload"))
	if err != nil { panic(err) }
	output, err := packaging.RunVerified(path, expected, nil, []string{"PATH=/usr/bin:/bin", "HOME=" + root, "TMPDIR=" + root})
	if err != nil { panic(err) }
	fmt.Printf("%%s\n%%s", path, output)
}
`, expected)
	if err := os.WriteFile(filepath.Join(outerDir, "main.go"), []byte(outerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	outer := filepath.Join(work, "embedded-outer")
	runCommand(t, repositoryRoot, goEnvironment, "go", "build", "-o", outer, filepath.Join(outerDir, "main.go"))

	parent := filepath.Join(work, "extraction-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(outer, parent)
	command.Env = sanitizedEnvironment(work)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("embedded outer execution: %v: %s", err, output)
	}
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) != 2 || lines[1] != "synthetic-helper 1" {
		t.Fatalf("embedded outer output = %q", output)
	}
	extracted := lines[0]
	if !filepath.IsAbs(extracted) {
		t.Fatalf("embedded helper path is not absolute: %q", extracted)
	}
	extractedBytes, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, extractedBytes) {
		t.Fatal("embedded extraction changed signed helper bytes")
	}
	runCommand(t, work, nil, "codesign", "--verify", "--strict", extracted)

	originalCDHash := codeDirectoryHash(t, helper)
	extractedCDHash := codeDirectoryHash(t, extracted)
	if originalCDHash != extractedCDHash {
		t.Fatalf("CDHash changed during extraction: %s != %s", originalCDHash, extractedCDHash)
	}

	t.Logf("host=%s/%s", runtime.GOOS, runtime.GOARCH)
	t.Logf("go=%s", strings.TrimSpace(runCommand(t, work, goEnvironment, "go", "version")))
	t.Logf("file original=%s", strings.TrimSpace(runCommand(t, work, nil, "file", helper)))
	t.Logf("file extracted=%s", strings.TrimSpace(runCommand(t, work, nil, "file", extracted)))
	t.Logf("sha256 original=%s extracted=%s", expected, hash(extractedBytes))
	t.Logf("CDHash original=%s extracted=%s", originalCDHash, extractedCDHash)
	t.Logf("xattr original=%q extracted=%q", strings.TrimSpace(runCommand(t, work, nil, "xattr", "-l", helper)), strings.TrimSpace(runCommand(t, work, nil, "xattr", "-l", extracted)))
}

func offlineGoEnvironment(root string) ([]string, error) {
	for _, requirement := range [][2]string{
		{"GOTOOLCHAIN", "local"},
		{"GOPROXY", "off"},
		{"GOSUMDB", "off"},
	} {
		if os.Getenv(requirement[0]) != requirement[1] {
			return nil, fmt.Errorf("SPARC_PACKAGING_SMOKE=1 requires %s=%s; refusing child Go build", requirement[0], requirement[1])
		}
	}
	return []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + root,
		"TMPDIR=" + root,
		"GOCACHE=" + filepath.Join(root, "go-cache"),
		"GOMODCACHE=" + filepath.Join(root, "go-mod-cache"),
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOSUMDB=off",
	}, nil
}

func sanitizedEnvironment(root string) []string {
	return []string{"PATH=/usr/bin:/bin", "HOME=" + root, "TMPDIR=" + root}
}

func runCommand(t *testing.T, directory string, environment []string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = directory
	if environment != nil {
		command.Env = environment
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v: %s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func codeDirectoryHash(t *testing.T, path string) string {
	t.Helper()
	command := exec.Command("codesign", "-dvvv", path)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("codesign -dvvv %s: %v: %s", path, err, output)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if value, ok := strings.CutPrefix(line, "CDHash="); ok {
			return value
		}
	}
	t.Fatalf("codesign -dvvv output did not include CDHash: %s", output)
	return ""
}
