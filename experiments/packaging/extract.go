// Package packaging is an isolated, non-production packaging experiment.
package packaging

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const helperName = "synthetic-helper"

// Extract verifies payload before creating destination, then creates one new
// private directory without replacing any existing path.
func Extract(payload []byte, expectedSHA256, destination string) (string, error) {
	if hash(payload) != expectedSHA256 {
		return "", fmt.Errorf("synthetic helper hash mismatch")
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return "", fmt.Errorf("create private destination: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(destination)
		}
	}()

	path := filepath.Join(destination, helperName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return "", fmt.Errorf("create helper: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write helper: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close helper: %w", err)
	}
	cleanup = false
	return path, nil
}

// RunVerified checks current bytes before absolute-path execution with only
// the supplied sanitized environment.
func RunVerified(path, expectedSHA256 string, args, environment []string) ([]byte, error) {
	if environment == nil {
		return nil, fmt.Errorf("sanitized helper environment required")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("helper path must be absolute")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read helper: %w", err)
	}
	if hash(payload) != expectedSHA256 {
		return nil, fmt.Errorf("synthetic helper hash mismatch")
	}
	command := exec.Command(path, args...)
	command.Dir = filepath.Dir(path)
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("execute verified helper: %w", err)
	}
	return output, nil
}

func hash(payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%x", sum)
}
