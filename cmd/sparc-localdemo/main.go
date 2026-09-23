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

	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/localdemo"
)

const helpText = `Usage: sparc-localdemo --archive DIR [--passphrase-file FILE]

Runs a developer-only PostgreSQL 17 recovery rehearsal using synthetic data.
SPARC_TEST_PG_BIN must name the local PostgreSQL 17 bin directory. The command
creates its own temporary loopback cluster; it accepts no database URL and
never connects to hosted projects. The encrypted archive covers one schema and
is declared incomplete. This command is excluded from normal release builds.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("sparc-localdemo", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var archiveDir, passphraseFile string
	var help bool
	flags.StringVar(&archiveDir, "archive", "", "private destination for encrypted demo archive")
	flags.StringVar(&passphraseFile, "passphrase-file", "", "private file containing the passphrase")
	flags.BoolVar(&help, "help", false, "show help")
	flags.BoolVar(&help, "h", false, "show help")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprint(stderr, "sparc-localdemo: invalid arguments\n")
		return 2
	}
	if help {
		if _, err := io.WriteString(stdout, helpText); err != nil {
			fmt.Fprint(stderr, "sparc-localdemo: unable to write output\n")
			return 1
		}
		return 0
	}
	if archiveDir == "" {
		fmt.Fprint(stderr, "sparc-localdemo: --archive is required\n")
		return 2
	}
	binDir := os.Getenv("SPARC_TEST_PG_BIN")
	if binDir == "" {
		fmt.Fprint(stderr, "sparc-localdemo: set SPARC_TEST_PG_BIN to an absolute PostgreSQL 17 bin directory\n")
		return 2
	}
	var reference *credentials.Reference
	if passphraseFile != "" {
		reference = &credentials.Reference{File: passphraseFile}
	}
	passphrase, err := credentials.Input(reference, stdin, stderr)
	if err != nil {
		fmt.Fprint(stderr, "sparc-localdemo: unable to read passphrase\n")
		return 2
	}
	defer clear(passphrase)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := localdemo.Run(ctx, binDir, archiveDir, passphrase); err != nil {
		writeFailure(stderr, err)
		if errors.Is(ctx.Err(), context.Canceled) && !errors.Is(err, localdemo.ErrCleanup) {
			return 130
		}
		return 1
	}
	if _, err := io.WriteString(stdout, "PASS: synthetic PostgreSQL 17 backup, offline verification, source deletion, and fresh-target restore completed. Archive is schema-only and marked incomplete.\n"); err != nil {
		fmt.Fprint(stderr, "sparc-localdemo: unable to write output\n")
		return 1
	}
	return 0
}

func writeFailure(stderr io.Writer, err error) {
	switch {
	case errors.Is(err, localdemo.ErrTools):
		fmt.Fprint(stderr, "sparc-localdemo: SPARC_TEST_PG_BIN must contain compatible PostgreSQL 17 tools\n")
	case errors.Is(err, localdemo.ErrArchivePath):
		fmt.Fprint(stderr, "sparc-localdemo: archive path must be new and have a private parent directory\n")
	case errors.Is(err, localdemo.ErrCleanup):
		fmt.Fprint(stderr, "sparc-localdemo: private temporary cleanup failed; inspect OS temp for sparc-localdemo-* before retrying\n")
	case errors.Is(err, localdemo.ErrLocalStorage):
		fmt.Fprint(stderr, "sparc-localdemo: private temporary storage unavailable\n")
	case errors.Is(err, localdemo.ErrClusterStart):
		fmt.Fprint(stderr, "sparc-localdemo: disposable PostgreSQL server failed to start\n")
	case errors.Is(err, localdemo.ErrCluster):
		fmt.Fprint(stderr, "sparc-localdemo: disposable PostgreSQL cluster setup failed\n")
	case errors.Is(err, localdemo.ErrDatabase):
		fmt.Fprint(stderr, "sparc-localdemo: synthetic local database setup failed\n")
	case errors.Is(err, localdemo.ErrDump):
		fmt.Fprint(stderr, "sparc-localdemo: synthetic database dump or archive creation failed\n")
	case errors.Is(err, localdemo.ErrArchive):
		fmt.Fprint(stderr, "sparc-localdemo: offline archive verification failed\n")
	case errors.Is(err, localdemo.ErrRestore):
		fmt.Fprint(stderr, "sparc-localdemo: source deletion or fresh-target restore check failed\n")
	default:
		fmt.Fprint(stderr, "sparc-localdemo: local synthetic backup/restore failed\n")
	}
}
