package destination

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/platform"
)

var ErrLocal = errors.New("local archive unavailable")

// Create verifies encrypted payloads in a private sibling staging directory
// before manifest creation, then publishes it with native no-replace semantics.
func Create(finalPath string, inputs []archive.Input, passphrase string) (archive.Manifest, error) {
	if !validTarget(finalPath) || platform.CheckPrivateDir(filepath.Dir(finalPath)) != nil {
		return archive.Manifest{}, ErrLocal
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return archive.Manifest{}, ErrLocal
	}
	staging := filepath.Join(filepath.Dir(finalPath), ".sparc-staging-"+hex.EncodeToString(random[:]))
	if platform.CreatePrivateDir(staging) != nil {
		return archive.Manifest{}, ErrLocal
	}
	published := false
	defer func() {
		if !published {
			cleanupStaging(staging)
		}
	}()
	manifest, err := archive.Write(staging, inputs, passphrase)
	if err != nil {
		return archive.Manifest{}, ErrLocal
	}
	ok, err := platform.PublishPrivateDir(staging, finalPath)
	if err != nil || !ok {
		return archive.Manifest{}, ErrLocal
	}
	published = true
	return manifest, nil
}

func validTarget(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Base(path) != "." && filepath.Base(path) != string(filepath.Separator)
}

func cleanupStaging(path string) {
	if platform.CheckPrivateDir(path) != nil {
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if name != "manifest.age" && !payloadName(name) {
			continue
		}
		if entry.IsDir() {
			continue
		}
		_ = os.Remove(filepath.Join(path, name))
	}
	_ = os.Remove(path)
}

func payloadName(name string) bool {
	if len(name) != 12 || !strings.HasSuffix(name, ".age") {
		return false
	}
	for _, char := range name[:8] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
