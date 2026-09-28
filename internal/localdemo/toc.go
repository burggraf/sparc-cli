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
// Schema counts come from pg_restore's native TOC schema filters.
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
	env := []string{"LANG=C", "LC_ALL=C", "HOME=/private/tmp", "OPENSSL_CONF=/dev/null"}
	output, err := runArchiveTOC(ctx, tools.pgrestore, env, archivePath, passphrase, "--list")
	if err != nil {
		return TOCSummary{}, err
	}
	summary, err := summarizeArchiveTOC(output)
	clear(output)
	if err != nil {
		return TOCSummary{}, err
	}
	for _, schema := range []string{"auth", "public", "storage"} {
		filtered, err := runArchiveTOC(ctx, tools.pgrestore, env, archivePath, passphrase, "--list", "--schema", schema)
		if err != nil {
			return TOCSummary{}, err
		}
		count, countErr := countArchiveTOCEntries(filtered)
		clear(filtered)
		if countErr != nil {
			return TOCSummary{}, countErr
		}
		switch schema {
		case "auth":
			summary.Auth = count
		case "public":
			summary.Public = count
		case "storage":
			summary.Storage = count
		}
	}
	return summary, nil
}

func runArchiveTOC(ctx context.Context, pgrestore string, env []string, archivePath string, passphrase []byte, args ...string) ([]byte, error) {
	file, err := platform.OpenPrivatePayloadFile(filepath.Join(archivePath, "00000000.age"), false)
	if err != nil {
		return nil, ErrArchive
	}
	plaintext, err := archive.Decrypt(file, string(passphrase))
	if err != nil {
		_ = file.Close()
		return nil, ErrArchive
	}
	var output boundedTOC
	runErr := runCommandWithTimeout(ctx, 2*time.Minute, env, pgrestore, plaintext, &output, args...)
	closeErr := file.Close()
	if runErr != nil || closeErr != nil {
		clear(output.Bytes())
		return nil, ErrArchive
	}
	return output.Bytes(), nil
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
		if len(parts) >= 3 && parts[2] == "EXTENSION" {
			result.Extension++
		}
	}
	if result.Total == 0 {
		return TOCSummary{}, ErrArchive
	}
	return result, nil
}

func countArchiveTOCEntries(data []byte) (int, error) {
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		id, _, ok := strings.Cut(line, ";")
		if !ok {
			return 0, ErrArchive
		}
		if _, err := strconv.ParseUint(strings.TrimSpace(id), 10, 32); err != nil {
			return 0, ErrArchive
		}
		count++
	}
	return count, nil
}
