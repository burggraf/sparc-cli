// Package platform provides native private-storage boundaries. Same-user path
// races and privileged users are outside this boundary. See docs/credentials.md
// for platform qualification gates, including macOS extended ACLs.
package platform

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrPrivateStorage = errors.New("private storage unavailable")

type Locations struct {
	ConfigFile string
	DataDir    string
	CacheDir   string
}

func validPath(path string) bool {
	if !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return false
		}
	}
	return validNativePath(path)
}

// ancestors includes the volume/root anchor but excludes the final object.
func ancestors(path string) []string {
	var paths []string
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		paths = append(paths, parent)
		if filepath.Dir(parent) == parent {
			break
		}
	}
	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}
	return paths
}

func locations(config, data, cache string) (Locations, error) {
	for _, path := range []string{config, data, cache} {
		if !validPath(path) {
			return Locations{}, ErrPrivateStorage
		}
	}
	return Locations{ConfigFile: config, DataDir: data, CacheDir: cache}, nil
}

func WritePrivateFile(path string, data []byte) error {
	return writePrivateFileWith(path, data, (*os.File).Write, (*os.File).Close)
}

func writePrivateFileWith(path string, data []byte, write func(*os.File, []byte) (int, error), close func(*os.File) error) error {
	file, err := openPrivateFile(path, true)
	if err != nil {
		return ErrPrivateStorage
	}
	n, writeErr := write(file, data)
	closeErr := close(file)
	if writeErr != nil || n != len(data) || closeErr != nil {
		// Only a newly, exclusively created object is removed. No existing file is repaired.
		_ = os.Remove(path)
		return ErrPrivateStorage
	}
	return nil
}

// CreatePrivateFile creates a non-executable private file for bounded streaming.
func CreatePrivateFile(path string) (*os.File, error) {
	file, err := createPrivatePayloadFile(path)
	if err != nil {
		return nil, ErrPrivateStorage
	}
	return file, nil
}

// OpenPrivatePayloadFile opens a previously created data or executable payload.
func OpenPrivatePayloadFile(path string, executable bool) (*os.File, error) {
	file, err := openPrivatePayloadFile(path, executable)
	if err != nil {
		return nil, ErrPrivateStorage
	}
	return file, nil
}

// SealPrivateExecutable flushes and seals a private file as executable where the
// native platform has executable mode bits.
func SealPrivateExecutable(file *os.File) error {
	if file == nil || sealPrivateExecutable(file) != nil {
		return ErrPrivateStorage
	}
	return nil
}

// PublishPrivateDir atomically publishes a private sibling directory without
// replacing or inspecting an existing winner.
func PublishPrivateDir(staging, destination string) (bool, error) {
	return publishPrivateDirWith(staging, destination, publishPrivateDir)
}

func publishPrivateDirWith(staging, destination string, publish func(string, string) (bool, error)) (bool, error) {
	if !validPath(staging) || !validPath(destination) || staging == destination || filepath.Dir(staging) != filepath.Dir(destination) {
		return false, ErrPrivateStorage
	}
	parent := filepath.Dir(staging)
	if CheckPrivateDir(parent) != nil || CheckPrivateDir(staging) != nil {
		return false, ErrPrivateStorage
	}
	published, err := publish(staging, destination)
	if err != nil {
		return false, ErrPrivateStorage
	}
	return published, nil
}

func ReadPrivateFile(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes == math.MaxInt64 {
		return nil, ErrPrivateStorage
	}
	file, err := openPrivateFile(path, false)
	if err != nil {
		return nil, ErrPrivateStorage
	}
	info, statErr := file.Stat()
	if statErr != nil || info.Size() > maxBytes {
		file.Close()
		return nil, ErrPrivateStorage
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxBytes+1))
	closeErr := file.Close()
	if readErr != nil || int64(len(data)) > maxBytes || closeErr != nil {
		return nil, ErrPrivateStorage
	}
	return data, nil
}

// Path components are split without normalizing away forbidden input.
func pathComponents(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
}
