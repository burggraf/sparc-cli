package tools

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/burggraf/sparc-cli/internal/platform"
)

const (
	payloadCacheDirectory = "tools-v1"
	payloadReceiptName    = ".sparc-receipt-v1"
	payloadBufferBytes    = 32 * 1024
	tarBlockBytes         = 512
)

var ErrExtraction = errors.New("tool extraction failed")

type extractionOps struct {
	createDir  func(string) error
	createFile func(string) (*os.File, error)
	write      func(*os.File, []byte) (int, error)
	sync       func(*os.File) error
	close      func(*os.File) error
	seal       func(*os.File) error
	publish    func(string, string) (bool, error)
	removeAll  func(string) error
	random     io.Reader
}

func defaultExtractionOps() extractionOps {
	return extractionOps{
		createDir:  platform.CreatePrivateDir,
		createFile: platform.CreatePrivateFile,
		write:      (*os.File).Write,
		sync:       (*os.File).Sync,
		close:      (*os.File).Close,
		seal:       platform.SealPrivateExecutable,
		publish:    platform.PublishPrivateDir,
		removeAll:  os.RemoveAll,
		random:     rand.Reader,
	}
}

func preparePayload(ctx context.Context, cacheRoot string, source io.ReadSeeker, manifest packageManifest) (string, error) {
	return preparePayloadWith(ctx, cacheRoot, source, manifest, defaultExtractionOps())
}

func preparePayloadWith(ctx context.Context, cacheRoot string, source io.ReadSeeker, manifest packageManifest, ops extractionOps) (string, error) {
	if ctx == nil || source == nil || validateManifest(manifest) != nil || manifestUsesReservedPath(manifest) || platform.CheckPrivateDir(cacheRoot) != nil {
		return "", ErrExtraction
	}
	id, err := packageID(manifest)
	if err != nil {
		return "", ErrExtraction
	}
	cacheDir := filepath.Join(cacheRoot, payloadCacheDirectory)
	if err := ensurePrivateDirectory(cacheDir, ops.createDir); err != nil {
		return "", ErrExtraction
	}
	destination := filepath.Join(cacheDir, id)
	if _, err := os.Lstat(destination); err == nil {
		if validatePayloadPackage(ctx, destination, manifest) != nil {
			return "", ErrExtraction
		}
		return destination, nil
	} else if !os.IsNotExist(err) {
		return "", ErrExtraction
	}
	if err := verifyCompressedSource(ctx, source, manifest); err != nil {
		return "", ErrExtraction
	}
	staging, err := createStagingDirectory(cacheDir, ops)
	if err != nil {
		return "", ErrExtraction
	}
	owned := true
	defer func() {
		if owned {
			_ = ops.removeAll(staging)
		}
	}()
	if err := extractArchive(ctx, source, staging, manifest, id, ops); err != nil {
		return "", ErrExtraction
	}
	published, err := ops.publish(staging, destination)
	if err != nil {
		return "", ErrExtraction
	}
	if published {
		owned = false
	} else if err := ops.removeAll(staging); err != nil {
		return "", ErrExtraction
	} else {
		owned = false
	}
	if validatePayloadPackage(ctx, destination, manifest) != nil {
		return "", ErrExtraction
	}
	return destination, nil
}

func ensurePrivateDirectory(path string, create func(string) error) error {
	if err := create(path); err == nil {
		return nil
	}
	return platform.CheckPrivateDir(path)
}

func createStagingDirectory(parent string, ops extractionOps) (string, error) {
	var random [16]byte
	for range 16 {
		if _, err := io.ReadFull(ops.random, random[:]); err != nil {
			return "", ErrExtraction
		}
		path := filepath.Join(parent, ".staging-"+hex.EncodeToString(random[:]))
		if err := ops.createDir(path); err == nil {
			return path, nil
		}
		if _, err := os.Lstat(path); err != nil && !os.IsNotExist(err) {
			return "", ErrExtraction
		}
	}
	return "", ErrExtraction
}

func verifyCompressedSource(ctx context.Context, source io.ReadSeeker, manifest packageManifest) error {
	if offset, err := source.Seek(0, io.SeekStart); err != nil || offset != 0 {
		return ErrExtraction
	}
	hash := sha256.New()
	limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: source}, N: int64(maxCompressedBytes) + 1}
	n, err := io.CopyBuffer(hash, limited, make([]byte, payloadBufferBytes))
	if err != nil || uint64(n) != manifest.CompressedLength || !equalDigest(hash.Sum(nil), manifest.CompressedSHA256) {
		return ErrExtraction
	}
	if offset, err := source.Seek(0, io.SeekStart); err != nil || offset != 0 {
		return ErrExtraction
	}
	return nil
}

func equalDigest(value []byte, want [sha256.Size]byte) bool {
	return len(value) == len(want) && string(value) == string(want[:])
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}

func extractArchive(ctx context.Context, source io.Reader, staging string, manifest packageManifest, id string, ops extractionOps) error {
	expectedTarBytes, ok := tarStructureLength(manifest)
	if !ok {
		return ErrExtraction
	}
	contextSource := &contextReader{ctx: ctx, reader: source}
	compressedHash := sha256.New()
	limited := &io.LimitedReader{R: io.TeeReader(contextSource, compressedHash), N: int64(manifest.CompressedLength)}
	buffered := bufio.NewReaderSize(limited, payloadBufferBytes)
	compressed, err := gzip.NewReader(buffered)
	if err != nil || compressed.Name != "" || compressed.Comment != "" || len(compressed.Extra) != 0 || !compressed.ModTime.Equal(time.Time{}) || compressed.OS != 255 {
		return ErrExtraction
	}
	compressed.Multistream(false)
	tarStream := &countingReader{reader: compressed}
	archive := tar.NewReader(tarStream)
	expected := make(map[string]payloadFile, len(manifest.Files))
	for _, file := range manifest.Files {
		expected[file.Path] = file
	}
	seen := make(map[string]bool, len(expected))
	createdDirectories := map[string]bool{"": true}
	var expanded uint64
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil || header == nil || header.Format != tar.FormatUSTAR || header.Typeflag != tar.TypeReg || header.Linkname != "" || len(header.PAXRecords) != 0 || len(header.Xattrs) != 0 || header.Uid != 0 || header.Gid != 0 || header.Devmajor != 0 || header.Devminor != 0 || header.Uname != "" || header.Gname != "" || header.ModTime.Unix() != 0 || !header.AccessTime.Equal(time.Time{}) || !header.ChangeTime.Equal(time.Time{}) {
			return ErrExtraction
		}
		file, ok := expected[header.Name]
		if !ok || seen[header.Name] || !portablePayloadPath(header.Name) || header.Size < 0 || uint64(header.Size) != file.Length || header.Mode != int64(file.Mode) {
			return ErrExtraction
		}
		expanded, ok = boundedAdd(expanded, uint64(header.Size), maxExpandedBytes)
		if !ok {
			return ErrExtraction
		}
		if err := createManifestDirectories(staging, header.Name, createdDirectories, ops.createDir); err != nil {
			return ErrExtraction
		}
		if err := extractPayloadFile(ctx, archive, filepath.Join(staging, filepath.FromSlash(header.Name)), file, manifest.Target, ops); err != nil {
			return ErrExtraction
		}
		seen[header.Name] = true
	}
	if len(seen) != len(expected) || tarStream.count != expectedTarBytes {
		return ErrExtraction
	}
	var trailing [1]byte
	if n, err := compressed.Read(trailing[:]); n != 0 || err != io.EOF {
		return ErrExtraction
	}
	if compressed.Close() != nil || buffered.Buffered() != 0 || limited.N != 0 || !equalDigest(compressedHash.Sum(nil), manifest.CompressedSHA256) {
		return ErrExtraction
	}
	var appended [1]byte
	if n, err := contextSource.Read(appended[:]); n != 0 || err != io.EOF {
		return ErrExtraction
	}
	return writeReceipt(filepath.Join(staging, payloadReceiptName), receiptFor(id), ops)
}

func tarStructureLength(manifest packageManifest) (uint64, bool) {
	total := uint64(2 * tarBlockBytes)
	for _, file := range manifest.Files {
		withPadding, ok := boundedAdd(file.Length, tarBlockBytes-1, math.MaxUint64)
		if !ok {
			return 0, false
		}
		padded := withPadding / tarBlockBytes * tarBlockBytes
		total, ok = boundedAdd(total, tarBlockBytes, math.MaxUint64)
		if !ok {
			return 0, false
		}
		total, ok = boundedAdd(total, padded, math.MaxUint64)
		if !ok {
			return 0, false
		}
	}
	return total, true
}

type countingReader struct {
	reader io.Reader
	count  uint64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.count += uint64(n)
	return n, err
}

func createManifestDirectories(root, archivePath string, created map[string]bool, create func(string) error) error {
	parent := filepath.ToSlash(filepath.Dir(archivePath))
	if parent == "." || parent == "" {
		return nil
	}
	parts := strings.Split(parent, "/")
	logical := ""
	for _, part := range parts {
		if logical == "" {
			logical = part
		} else {
			logical += "/" + part
		}
		if !created[logical] {
			if err := create(filepath.Join(root, filepath.FromSlash(logical))); err != nil {
				return ErrExtraction
			}
			created[logical] = true
		}
	}
	return nil
}

func extractPayloadFile(ctx context.Context, reader io.Reader, path string, expected payloadFile, target payloadTarget, ops extractionOps) error {
	file, err := ops.createFile(path)
	if err != nil {
		return ErrExtraction
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	hash := sha256.New()
	remaining := expected.Length
	buffer := make([]byte, payloadBufferBytes)
	for remaining > 0 {
		select {
		case <-ctx.Done():
			return ErrExtraction
		default:
		}
		want := uint64(len(buffer))
		if remaining < want {
			want = remaining
		}
		n, readErr := io.ReadFull(reader, buffer[:want])
		if readErr != nil || n == 0 {
			return ErrExtraction
		}
		hash.Write(buffer[:n])
		written, writeErr := ops.write(file, buffer[:n])
		if writeErr != nil || written != n {
			return ErrExtraction
		}
		remaining -= uint64(n)
	}
	if !equalDigest(hash.Sum(nil), expected.SHA256) {
		return ErrExtraction
	}
	if expected.Purpose == purposeExecutable {
		if ops.sync(file) != nil || ops.seal(file) != nil {
			return ErrExtraction
		}
	} else if ops.sync(file) != nil {
		return ErrExtraction
	}
	if ops.close(file) != nil {
		return ErrExtraction
	}
	closed = true
	if expected.Purpose == purposeExecutable {
		opened, err := platform.OpenPrivatePayloadFile(path, true)
		if err != nil {
			return ErrExtraction
		}
		valid := validExecutable(opened, target)
		closeErr := opened.Close()
		if !valid || closeErr != nil {
			return ErrExtraction
		}
	}
	return nil
}

func validExecutable(file *os.File, target payloadTarget) bool {
	switch target.OS {
	case "darwin":
		parsed, err := macho.NewFile(file)
		if err != nil {
			return false
		}
		defer parsed.Close()
		return parsed.Type == macho.TypeExec && (target.Architecture == "arm64" && parsed.Cpu == macho.CpuArm64 || target.Architecture == "amd64" && parsed.Cpu == macho.CpuAmd64)
	case "windows":
		parsed, err := pe.NewFile(file)
		if err != nil {
			return false
		}
		defer parsed.Close()
		_, pe32Plus := parsed.OptionalHeader.(*pe.OptionalHeader64)
		characteristics := parsed.FileHeader.Characteristics
		return target.Architecture == "amd64" && parsed.Machine == pe.IMAGE_FILE_MACHINE_AMD64 && pe32Plus && characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE != 0 && characteristics&pe.IMAGE_FILE_DLL == 0
	default:
		return false
	}
}

func receiptFor(id string) []byte {
	return []byte("SPARC-TOOLS-RECEIPT\nschema=1\npackage_id=" + id + "\n")
}

func writeReceipt(path string, receipt []byte, ops extractionOps) error {
	if len(receipt) > 128 {
		return ErrExtraction
	}
	file, err := ops.createFile(path)
	if err != nil {
		return ErrExtraction
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	n, err := ops.write(file, receipt)
	if err != nil || n != len(receipt) || ops.sync(file) != nil || ops.close(file) != nil {
		return ErrExtraction
	}
	closed = true
	return nil
}

func validatePayloadPackage(ctx context.Context, path string, manifest packageManifest) error {
	if ctx == nil || validateManifest(manifest) != nil || manifestUsesReservedPath(manifest) || platform.CheckPrivateDir(path) != nil {
		return ErrExtraction
	}
	id, err := packageID(manifest)
	if err != nil {
		return ErrExtraction
	}
	receipt, err := platform.ReadPrivateFile(filepath.Join(path, payloadReceiptName), 128)
	if err != nil || string(receipt) != string(receiptFor(id)) {
		return ErrExtraction
	}
	expected := make(map[string]payloadFile, len(manifest.Files))
	expectedDirectories := make(map[string]bool)
	for _, file := range manifest.Files {
		expected[file.Path] = file
		for parent := filepath.ToSlash(filepath.Dir(file.Path)); parent != "." && parent != ""; parent = filepath.ToSlash(filepath.Dir(parent)) {
			expectedDirectories[parent] = true
		}
	}
	found := make(map[string]bool, len(expected))
	err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return ErrExtraction
		}
		select {
		case <-ctx.Done():
			return ErrExtraction
		default:
		}
		if current == path {
			return nil
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return ErrExtraction
		}
		logical := filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrExtraction
		}
		if entry.IsDir() {
			if !expectedDirectories[logical] || platform.CheckPrivateDir(current) != nil {
				return ErrExtraction
			}
			return nil
		}
		if logical == payloadReceiptName {
			return nil
		}
		expectedFile, ok := expected[logical]
		if !ok || found[logical] {
			return ErrExtraction
		}
		if validatePayloadFile(ctx, current, expectedFile, manifest.Target) != nil {
			return ErrExtraction
		}
		found[logical] = true
		return nil
	})
	if err != nil || len(found) != len(expected) {
		return ErrExtraction
	}
	return nil
}

func validatePayloadFile(ctx context.Context, path string, expected payloadFile, target payloadTarget) error {
	file, err := platform.OpenPrivatePayloadFile(path, expected.Purpose == purposeExecutable)
	if err != nil {
		return ErrExtraction
	}
	valid := true
	info, err := file.Stat()
	if err != nil || info.Size() < 0 || uint64(info.Size()) != expected.Length {
		valid = false
	}
	hash := sha256.New()
	if valid {
		if _, err := io.CopyBuffer(hash, &contextReader{ctx: ctx, reader: file}, make([]byte, payloadBufferBytes)); err != nil || !equalDigest(hash.Sum(nil), expected.SHA256) {
			valid = false
		}
	}
	if valid && expected.Purpose == purposeExecutable && !validExecutable(file, target) {
		valid = false
	}
	if file.Close() != nil {
		valid = false
	}
	if !valid {
		return ErrExtraction
	}
	return nil
}

func manifestUsesReservedPath(manifest packageManifest) bool {
	for _, file := range manifest.Files {
		if strings.EqualFold(file.Path, payloadReceiptName) || strings.HasPrefix(strings.ToLower(file.Path), strings.ToLower(payloadReceiptName)+"/") {
			return true
		}
	}
	return false
}
