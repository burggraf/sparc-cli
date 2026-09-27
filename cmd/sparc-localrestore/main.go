//go:build localdemo

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/localdemo"
	"golang.org/x/term"
)

const helpText = `Usage: sparc-localrestore --archive DIR [--passphrase-file FILE]

Verifies and restores one SPARC database dump into a NEW temporary PostgreSQL 17
cluster created with /opt/homebrew/opt/postgresql@17/bin. The cluster uses only
a private Unix socket, is stopped and removed afterward, and never connects to
Supabase or an existing PostgreSQL service. The archive is read-only. This
executes SQL from the archive: use only an archive you trust. A successful
restore does not establish completeness against the hosted source.
--show-postgres-error prints up to 64 KiB of raw PostgreSQL errors on your
terminal. They may include sensitive SQL or object names: DO NOT paste them.
`

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("sparc-localrestore", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var archivePath, passphraseFile string
	var help, showPostgresError bool
	flags.StringVar(&archivePath, "archive", "", "existing encrypted SPARC archive")
	flags.StringVar(&passphraseFile, "passphrase-file", "", "file containing archive passphrase")
	flags.BoolVar(&help, "help", false, "show help")
	flags.BoolVar(&showPostgresError, "show-postgres-error", false, "show raw PostgreSQL error locally (may contain sensitive SQL)")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "sparc-localrestore: invalid arguments")
		return 2
	}
	if help {
		if _, err := io.WriteString(stdout, helpText); err != nil {
			return 1
		}
		return 0
	}
	if archivePath == "" {
		fmt.Fprintln(stderr, "sparc-localrestore: --archive is required")
		return 2
	}
	if showPostgresError {
		file, ok := stderr.(*os.File)
		if !ok || !term.IsTerminal(int(file.Fd())) {
			fmt.Fprintln(stderr, "sparc-localrestore: raw PostgreSQL diagnostics require terminal stderr")
			return 2
		}
	}
	var err error
	archivePath, err = filepath.Abs(archivePath)
	if err != nil {
		fmt.Fprintln(stderr, "sparc-localrestore: invalid archive path")
		return 2
	}
	var reference *credentials.Reference
	if passphraseFile != "" {
		passphraseFile, err = filepath.Abs(passphraseFile)
		if err != nil {
			fmt.Fprintln(stderr, "sparc-localrestore: invalid passphrase path")
			return 2
		}
		reference = &credentials.Reference{File: passphraseFile}
	}
	passphrase, err := credentials.Input(reference, stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "sparc-localrestore: unable to read passphrase")
		return 2
	}
	defer func() {
		for i := range passphrase {
			passphrase[i] = 0
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var restoreErr error
	if showPostgresError {
		restoreErr = localdemo.RehearseExistingArchiveWithDiagnostics(ctx, "/opt/homebrew/opt/postgresql@17/bin", archivePath, passphrase, stderr)
	} else {
		restoreErr = localdemo.RehearseExistingArchive(ctx, "/opt/homebrew/opt/postgresql@17/bin", archivePath, passphrase)
	}
	if err := restoreErr; err != nil {
		switch {
		case errors.Is(err, localdemo.ErrArchive):
			fmt.Fprintln(stderr, "sparc-localrestore: archive verification or format failed")
		case errors.Is(err, localdemo.ErrTools):
			fmt.Fprintln(stderr, "sparc-localrestore: Homebrew PostgreSQL 17 tools unavailable")
		case errors.Is(err, localdemo.ErrRestoreMissingRole):
			fmt.Fprintln(stderr, "sparc-localrestore: one or more database roles required by the archive are absent from the disposable PostgreSQL cluster")
		case errors.Is(err, localdemo.ErrRestoreMissingExtension):
			fmt.Fprintln(stderr, "sparc-localrestore: one or more extensions required by the archive are unavailable in Homebrew PostgreSQL")
		case errors.Is(err, localdemo.ErrRestorePermission):
			fmt.Fprintln(stderr, "sparc-localrestore: PostgreSQL denied an operation required by the archive")
		case errors.Is(err, localdemo.ErrRestoreConflict):
			fmt.Fprintln(stderr, "sparc-localrestore: the archive contains conflicting PostgreSQL objects")
		case errors.Is(err, localdemo.ErrRestoreCatalog):
			fmt.Fprintln(stderr, "sparc-localrestore: pg_restore succeeded but the restored catalog query failed")
		case errors.Is(err, localdemo.ErrRestore):
			fmt.Fprintln(stderr, "sparc-localrestore: PostgreSQL rejected the restore; diagnostic did not match a known safe category")
		case errors.Is(err, localdemo.ErrCleanup):
			fmt.Fprintln(stderr, "sparc-localrestore: cluster cleanup failed; inspect /private/tmp/sparc-restore-* before retrying")
		default:
			fmt.Fprintln(stderr, "sparc-localrestore: disposable local PostgreSQL setup failed")
		}
		return 1
	}
	if _, err := io.WriteString(stdout, "Local PostgreSQL 17 restore succeeded; archive recovery is possible on this disposable cluster. Hosted-source fidelity and full-project coverage are NOT proven.\n"); err != nil {
		return 1
	}
	return 0
}
