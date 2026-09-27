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
)

const helpText = `Usage: sparc-localrestore --archive DIR [--passphrase-file FILE]

Verifies and restores one SPARC database dump into a NEW temporary PostgreSQL 17
cluster created with /opt/homebrew/opt/postgresql@17/bin. The cluster uses only
a private Unix socket, is stopped and removed afterward, and never connects to
Supabase or an existing PostgreSQL service. The archive is read-only. This
executes SQL from the archive: use only an archive you trust. A successful
restore does not establish completeness against the hosted source.
`

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("sparc-localrestore", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var archivePath, passphraseFile string
	var help bool
	flags.StringVar(&archivePath, "archive", "", "existing encrypted SPARC archive")
	flags.StringVar(&passphraseFile, "passphrase-file", "", "file containing archive passphrase")
	flags.BoolVar(&help, "help", false, "show help")
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
	if err := localdemo.RehearseExistingArchive(ctx, "/opt/homebrew/opt/postgresql@17/bin", archivePath, passphrase); err != nil {
		switch {
		case errors.Is(err, localdemo.ErrArchive):
			fmt.Fprintln(stderr, "sparc-localrestore: archive verification or format failed")
		case errors.Is(err, localdemo.ErrTools):
			fmt.Fprintln(stderr, "sparc-localrestore: Homebrew PostgreSQL 17 tools unavailable")
		case errors.Is(err, localdemo.ErrRestore):
			fmt.Fprintln(stderr, "sparc-localrestore: PostgreSQL rejected the restore; missing roles/extensions or other restore errors are possible")
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
