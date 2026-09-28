//go:build localdemo

// sparc-hostedtry is a developer-only, opt-in full-dump restore experiment.
// It is not part of the production sparc binary.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/database"
	"golang.org/x/term"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("sparc-hostedtry", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var archivePath, targetFile, projectRef, passphraseFile string
	var confirmed, showPostgresError bool
	flags.StringVar(&archivePath, "archive", "", "existing encrypted archive")
	flags.StringVar(&targetFile, "target-file", "", "file containing the target session-pooler URL")
	flags.StringVar(&projectRef, "project-ref", "", "exact target Supabase project ref")
	flags.StringVar(&passphraseFile, "passphrase-file", "", "file containing archive passphrase (omit to prompt)")
	flags.BoolVar(&confirmed, "confirm-disposable-target", false, "authorize a hosted write to the disposable target")
	flags.BoolVar(&showPostgresError, "show-postgres-error", false, "show up to 64 KiB of raw PostgreSQL error on the local terminal (may contain sensitive SQL)")
	if flags.Parse(args) != nil || flags.NArg() != 0 || archivePath == "" || targetFile == "" || projectRef == "" || !confirmed {
		fmt.Fprintln(stderr, "sparc-hostedtry: --archive, --target-file, --project-ref and --confirm-disposable-target are required")
		return 2
	}
	if showPostgresError {
		file, ok := stderr.(*os.File)
		if !ok || !term.IsTerminal(int(file.Fd())) {
			fmt.Fprintln(stderr, "sparc-hostedtry: raw PostgreSQL diagnostics require terminal stderr")
			return 2
		}
	}
	var err error
	archivePath, err = filepath.Abs(archivePath)
	if err != nil {
		fmt.Fprintln(stderr, "sparc-hostedtry: invalid archive path")
		return 2
	}
	targetFile, err = filepath.Abs(targetFile)
	if err != nil {
		fmt.Fprintln(stderr, "sparc-hostedtry: invalid target credential path")
		return 2
	}
	raw, err := credentials.Input(&credentials.Reference{File: targetFile}, nil, nil)
	if err != nil {
		fmt.Fprintln(stderr, "sparc-hostedtry: target credential file unreadable")
		return 2
	}
	defer clear(raw)
	target, password, err := database.ParseSupavisorSessionURL(raw, projectRef)
	if err != nil {
		fmt.Fprintln(stderr, "sparc-hostedtry: target credential does not match qualified project/session route")
		return 2
	}
	defer clear(password)
	var passRef *credentials.Reference
	if passphraseFile != "" {
		passphraseFile, err = filepath.Abs(passphraseFile)
		if err != nil {
			fmt.Fprintln(stderr, "sparc-hostedtry: invalid passphrase path")
			return 2
		}
		passRef = &credentials.Reference{File: passphraseFile}
	}
	passphrase, err := credentials.Input(passRef, stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "sparc-hostedtry: unable to read archive passphrase")
		return 2
	}
	defer clear(passphrase)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// The existing restore verifies the encrypted archive before connecting,
	// checks that public is empty, then runs pg_restore -1 --exit-on-error.
	var localOutput io.Writer
	if showPostgresError {
		localOutput = stderr
	}
	stage, category, trialErr := database.RestoreTrial(ctx, database.RestoreRequest{
		Target: target, TargetPassword: password, ArchivePath: archivePath, ArchivePassphrase: string(passphrase),
		EmptyScope: database.EmptyTargetScopeV1{RequiredPresent: []string{"public"}},
	}, localOutput)
	if trialErr != nil {
		if category == "" {
			fmt.Fprintf(stderr, "sparc-hostedtry: restore failed at %s; no recovery claimed. No automatic retry or cleanup was performed\n", stage)
		} else {
			fmt.Fprintf(stderr, "sparc-hostedtry: restore failed at %s (%s); no recovery claimed. No automatic retry or cleanup was performed\n", stage, category)
		}
		return 1
	}
	if _, err := io.WriteString(stdout, "PostgreSQL accepted the complete dump in one transaction. Source fidelity and full Supabase project recovery are NOT proven.\n"); err != nil {
		return 1
	}
	return 0
}
