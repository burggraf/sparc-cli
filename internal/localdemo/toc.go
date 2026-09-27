//go:build localdemo

package localdemo

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/platform"
)

// TOCSummary reports only counts, never SQL, names or role identifiers.
type TOCSummary struct {
	Total, Auth, Public, Storage, Extension int
}

const maxTOCBytes = 8 << 20

type boundedTOC struct{ bytes.Buffer }

func (b *boundedTOC) Write(p []byte) (int, error) {
	if len(p) > maxTOCBytes-b.Len() {
		return 0, ErrArchive
	}
	return b.Buffer.Write(p)
}

// InspectArchiveTOC verifies the encrypted archive then asks the explicitly
// selected local pg_restore to list its TOC without connecting to any database.
func InspectArchiveTOC(ctx context.Context, binDir, archivePath string, passphrase []byte) (TOCSummary, error) {
	if ctx == nil || len(passphrase) == 0 {
		return TOCSummary{}, ErrArchive
	}
	manifest, err := archive.Verify(archivePath, string(passphrase))
	if err != nil || len(manifest.Components) != 1 || manifest.Components[0].ID != "00000000" || manifest.Components[0].Key != "database/postgresql.dump" {
		return TOCSummary{}, ErrArchive
	}
	tools, err := resolveTools(binDir)
	if err != nil {
		return TOCSummary{}, err
	}
	file, err := platform.OpenPrivatePayloadFile(filepath.Join(archivePath, "00000000.age"), false)
	if err != nil {
		return TOCSummary{}, ErrArchive
	}
	defer file.Close()
	plaintext, err := archive.Decrypt(file, string(passphrase))
	if err != nil {
		return TOCSummary{}, ErrArchive
	}
	var output boundedTOC
	env := []string{"LANG=C", "LC_ALL=C", "HOME=/private/tmp", "OPENSSL_CONF=/dev/null"}
	if runCommandWithTimeout(ctx, 2*time.Minute, env, tools.pgrestore, plaintext, &output, "--list") != nil {
		return TOCSummary{}, ErrArchive
	}
	summary, err := summarizeArchiveTOC(output.Bytes())
	captured := output.Bytes()
	for i := range captured {
		captured[i] = 0
	}
	return summary, err
}

func summarizeArchiveTOC(data []byte) (TOCSummary, error) {
	var result TOCSummary
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		id, fields, ok := strings.Cut(line, ";")
		if !ok || strings.TrimSpace(id) == "" {
			return TOCSummary{}, ErrArchive
		}
		if _, err := strconv.ParseUint(strings.TrimSpace(id), 10, 32); err != nil {
			return TOCSummary{}, ErrArchive
		}
		parts := strings.Fields(fields)
		if len(parts) < 4 {
			return TOCSummary{}, ErrArchive
		}
		result.Total++
		if parts[2] == "EXTENSION" {
			result.Extension++
		}
		var auth, public, storage bool
		for _, token := range parts[3:] {
			switch token {
			case "auth":
				auth = true
			case "public":
				public = true
			case "storage":
				storage = true
			}
		}
		if auth {
			result.Auth++
		}
		if public {
			result.Public++
		}
		if storage {
			result.Storage++
		}
	}
	if result.Total == 0 {
		return TOCSummary{}, ErrArchive
	}
	return result, nil
}
