package packaging

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestOfflineGoEnvironmentRequiresPinnedSettings(t *testing.T) {
	for _, test := range []struct {
		name, key, value, want string
	}{
		{"toolchain", "GOTOOLCHAIN", "auto", "SPARC_PACKAGING_SMOKE=1 requires GOTOOLCHAIN=local; refusing child Go build"},
		{"proxy", "GOPROXY", "https://proxy.invalid", "SPARC_PACKAGING_SMOKE=1 requires GOPROXY=off; refusing child Go build"},
		{"sumdb", "GOSUMDB", "sum.golang.org", "SPARC_PACKAGING_SMOKE=1 requires GOSUMDB=off; refusing child Go build"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOTOOLCHAIN", "local")
			t.Setenv("GOPROXY", "off")
			t.Setenv("GOSUMDB", "off")
			t.Setenv(test.key, test.value)

			_, err := offlineGoEnvironment(t.TempDir())
			if err == nil {
				t.Fatalf("offlineGoEnvironment accepted %s=%s", test.key, test.value)
			}
			if err.Error() != test.want {
				t.Fatalf("offline Go contract error = %q, want %q", err.Error(), test.want)
			}
		})
	}
}

func TestOfflineGoEnvironmentIsBounded(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	root := t.TempDir()

	got, err := offlineGoEnvironment(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + root,
		"TMPDIR=" + root,
		"GOCACHE=" + filepath.Join(root, "go-cache"),
		"GOMODCACHE=" + filepath.Join(root, "go-mod-cache"),
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOSUMDB=off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("offline Go environment = %#v, want %#v", got, want)
	}
}
