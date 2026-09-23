// Package cli defines SPARC's command-line contract.
package cli

import (
	"fmt"
	"io"
)

const helpText = `Usage: sparc <command>

Commands:
  backup   Back up a Supabase project (not yet available)
  verify   Verify encrypted archive integrity offline
  restore  Restore a backup (not yet available)

Run "sparc verify --help" for verification options.
`

func writeOutput(stdout, stderr io.Writer, output string) int {
	if _, err := io.WriteString(stdout, output); err != nil {
		fmt.Fprint(stderr, "sparc: unable to write command output\n")
		return 1
	}
	return 0
}

// Run executes the command selected by args and returns its process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return writeOutput(stdout, stderr, helpText)
	}

	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			fmt.Fprint(stderr, "sparc: this command takes no arguments\nRun \"sparc help\" for usage.\n")
			return 2
		}
		return writeOutput(stdout, stderr, helpText)
	case "version", "--version":
		if len(args) != 1 {
			fmt.Fprint(stderr, "sparc: this command takes no arguments\nRun \"sparc help\" for usage.\n")
			return 2
		}
		return writeOutput(stdout, stderr, "sparc dev\n")
	case "verify":
		return runVerify(args[1:], stdin, stdout, stderr)
	case "backup", "restore":
		if len(args) != 1 {
			fmt.Fprint(stderr, "sparc: this command takes no arguments in this scaffold\nRun \"sparc help\" for usage.\n")
			return 2
		}
		fmt.Fprintf(stderr, "sparc: %s is not available in this scaffold\n", args[0])
		return 1
	default:
		fmt.Fprint(stderr, "sparc: unknown command\nRun \"sparc help\" for usage.\n")
		return 2
	}
}
