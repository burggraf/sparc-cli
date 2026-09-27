package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/database"
)

const backupHelpText = `Usage: sparc backup --project-ref REF --database-url-file FILE --archive DIR [--passphrase-file FILE]

Creates an encrypted local database archive using the PostgreSQL 17 client
bundled in the macOS arm64 application. No separate PostgreSQL installation is
required. FILE and DIR must be absolute private paths. The database URL is read
only from FILE; it is never accepted in argv. The archive is marked incomplete:
Auth, Storage, project configuration, roles, and provider-managed services are
not included.
`

type backupOps struct {
	input   func(*credentials.Reference, *os.File, io.Writer) ([]byte, error)
	parse   func([]byte, string) (database.ConnectionParams, []byte, error)
	capture func(context.Context, database.CaptureRequest) (archive.Manifest, error)
}

func runBackup(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runBackupWith(args, stdin, stdout, stderr, backupOps{
		input: credentials.Input, parse: database.ParseSupavisorSessionURL, capture: database.Capture,
	})
}

func runBackupWith(args []string, stdin io.Reader, stdout, stderr io.Writer, ops backupOps) int {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var projectRef, databaseURLFile, archiveDir, passphraseFile string
	var help bool
	flags.StringVar(&projectRef, "project-ref", "", "Supabase project ref")
	flags.StringVar(&databaseURLFile, "database-url-file", "", "private session-pooler URL file")
	flags.StringVar(&archiveDir, "archive", "", "new private local archive directory")
	flags.StringVar(&passphraseFile, "passphrase-file", "", "private file containing archive passphrase")
	flags.BoolVar(&help, "help", false, "show help")
	flags.BoolVar(&help, "h", false, "show help")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprint(stderr, "sparc: invalid backup arguments\n")
		return 2
	}
	if help {
		return writeOutput(stdout, stderr, backupHelpText)
	}
	if projectRef == "" || databaseURLFile == "" || archiveDir == "" || ops.input == nil || ops.parse == nil || ops.capture == nil {
		fmt.Fprint(stderr, "sparc: invalid backup arguments\n")
		return 2
	}

	urlBytes, err := ops.input(&credentials.Reference{File: databaseURLFile}, nil, stderr)
	if err != nil {
		fmt.Fprint(stderr, "sparc: unable to read database connection\n")
		return 2
	}
	defer clearBytes(urlBytes)
	source, password, err := ops.parse(urlBytes, projectRef)
	if err != nil {
		fmt.Fprint(stderr, "sparc: unable to read database connection\n")
		return 2
	}
	defer clearBytes(password)

	var passphraseReference *credentials.Reference
	if passphraseFile != "" {
		passphraseReference = &credentials.Reference{File: passphraseFile}
	}
	stdinFile, _ := stdin.(*os.File)
	passphrase, err := ops.input(passphraseReference, stdinFile, stderr)
	if err != nil {
		fmt.Fprint(stderr, "sparc: unable to read passphrase\n")
		return 2
	}
	defer clearBytes(passphrase)
	if _, err := ops.capture(context.Background(), database.CaptureRequest{
		Source: source, SourcePassword: password, ArchivePath: archiveDir, ArchivePassphrase: string(passphrase),
	}); err != nil {
		fmt.Fprint(stderr, "sparc: database backup failed\n")
		return 1
	}
	if writeOutput(stdout, stderr, "Database archive created.\nCapture declaration: incomplete.\n") != 0 {
		return 1
	}
	return 3
}

func clearBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
