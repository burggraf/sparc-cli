package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/verify"
)

const verifyHelpText = `Usage: sparc verify --archive DIR [--passphrase-file FILE]

Verifies encrypted archive integrity without network access. In a terminal, the
passphrase is requested without echo unless --passphrase-file is supplied.
`

func runVerify(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var archiveDir, passphraseFile string
	var help bool
	flags.StringVar(&archiveDir, "archive", "", "encrypted archive directory")
	flags.StringVar(&passphraseFile, "passphrase-file", "", "private file containing the passphrase")
	flags.BoolVar(&help, "help", false, "show help")
	flags.BoolVar(&help, "h", false, "show help")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprint(stderr, "sparc: invalid verify arguments\n")
		return 2
	}
	if help {
		return writeOutput(stdout, stderr, verifyHelpText)
	}
	if archiveDir == "" {
		fmt.Fprint(stderr, "sparc: invalid verify arguments\n")
		return 2
	}
	var reference *credentials.Reference
	if passphraseFile != "" {
		reference = &credentials.Reference{File: passphraseFile}
	}
	stdinFile, _ := stdin.(*os.File)
	passphrase, err := credentials.Input(reference, stdinFile, stderr)
	if err != nil {
		fmt.Fprint(stderr, "sparc: unable to read passphrase\n")
		return 2
	}
	defer clear(passphrase)

	report, err := verify.Offline(archiveDir, string(passphrase))
	if err != nil || !report.IntegrityPassed {
		fmt.Fprint(stderr, "sparc: archive verification failed\n")
		return 1
	}
	capture := report.CaptureStatus
	if capture != "complete" {
		capture = "incomplete"
	}
	output := fmt.Sprintf("Integrity: passed\nCapture declaration: %s\nComponents: %d\n", capture, report.ComponentCount)
	if report.CaptureStatus != "complete" {
		if writeOutput(stdout, stderr, output) != 0 {
			return 1
		}
		return 3
	}
	return writeOutput(stdout, stderr, output)
}
