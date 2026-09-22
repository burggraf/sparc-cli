package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/burggraf/sparc-cli/internal/platform"
)

const (
	maxPGPassFieldBytes = 4096
	maxPGPassBytes      = 16 * 1024
)

// ErrPassfile is returned without filesystem or secret details.
var ErrPassfile = errors.New("credential passfile unavailable")

type PGPassEntry struct {
	Host, Database, User string
	Port                 uint16
	Password             []byte
}

// PGPassfile owns one exclusively-created private PostgreSQL passfile.
type PGPassfile struct {
	path   string
	remove func(string) error
	once   sync.Once
	err    error
}

// NewPGPassfile creates and closes a private passfile before it is returned.
func NewPGPassfile(privateDir string, entry PGPassEntry) (*PGPassfile, error) {
	return newPGPassfileWith(privateDir, entry, rand.Reader, platform.CreatePrivateFile, os.Remove, (*os.File).Write, (*os.File).Sync, (*os.File).Close)
}

func newPGPassfileWith(privateDir string, entry PGPassEntry, random io.Reader, create func(string) (*os.File, error), remove func(string) error, write func(*os.File, []byte) (int, error), syncFile func(*os.File) error, closeFile func(*os.File) error) (*PGPassfile, error) {
	line, err := pgPassLine(entry)
	if err != nil || platform.CheckPrivateDir(privateDir) != nil || random == nil || create == nil || remove == nil || write == nil || syncFile == nil || closeFile == nil {
		return nil, ErrPassfile
	}
	var token [16]byte
	for range 16 {
		if _, err := io.ReadFull(random, token[:]); err != nil {
			return nil, ErrPassfile
		}
		path := filepath.Join(privateDir, ".pgpass-"+hex.EncodeToString(token[:]))
		file, err := create(path)
		if err != nil {
			continue
		}
		n, writeErr := write(file, line)
		syncErr := syncFile(file)
		closeErr := closeFile(file)
		if writeErr != nil || n != len(line) || syncErr != nil || closeErr != nil {
			_ = remove(path)
			return nil, ErrPassfile
		}
		return &PGPassfile{path: path, remove: remove}, nil
	}
	return nil, ErrPassfile
}

func (p *PGPassfile) Path() string {
	if p == nil {
		return ""
	}
	return p.path
}

// Close removes only this passfile. It is safe to call repeatedly.
func (p *PGPassfile) Close() error {
	if p == nil || p.path == "" || p.remove == nil {
		return ErrPassfile
	}
	p.once.Do(func() {
		if p.remove(p.path) != nil {
			p.err = ErrPassfile
		}
	})
	return p.err
}

// ValidatePGPassEntry validates the pure pgpass and connection-selection
// boundary without creating a file or exposing entry contents in an error.
func ValidatePGPassEntry(entry PGPassEntry) error {
	if entry.Port == 0 || !validPGPassField(entry.Host) || !validPGPassDatabase(entry.Database) || !validPGPassField(entry.User) || !validPGPassPassword(entry.Password) {
		return ErrPassfile
	}
	return nil
}

func pgPassLine(entry PGPassEntry) ([]byte, error) {
	if ValidatePGPassEntry(entry) != nil {
		return nil, ErrPassfile
	}
	return []byte(escapePGPass(entry.Host) + ":" + strconv.FormatUint(uint64(entry.Port), 10) + ":" + escapePGPass(entry.Database) + ":" + escapePGPass(entry.User) + ":" + escapePGPass(string(entry.Password)) + "\n"), nil
}

func validPGPassField(value string) bool {
	return len(value) > 0 && len(value) <= maxPGPassFieldBytes && validPGPassRunes(value) && value != "*"
}

func validPGPassDatabase(value string) bool {
	if !validPGPassField(value) || strings.Contains(value, "=") {
		return false
	}
	lower := strings.ToLower(value)
	return !strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://")
}

func validPGPassPassword(value []byte) bool {
	return len(value) > 0 && len(value) <= maxPGPassBytes && validPGPassRunes(string(value))
}

func validPGPassRunes(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func escapePGPass(value string) string {
	return strings.NewReplacer("\\", "\\\\", ":", "\\:").Replace(value)
}
