// Command writ is the porcelain CLI for humans, with --json output for
// scripts and agents. See ARCHITECTURE.md and VISION.md for the shape it
// is expected to grow into.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

func main() {
	ctx := context.Background()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	// Handle help flags at root
	switch args[0] {
	case "-h", "-help", "--help", "help":
		printUsage(stdout)
		return 0
	}

	// Support root -C flag, e.g. "writ -C <dir> init"
	var defaultDir string
	if args[0] == "-C" {
		if len(args) < 2 {
			fmt.Fprintln(stderr, "writ: option -C requires an argument")
			return 2
		}
		defaultDir = args[1]
		args = args[2:]
		if len(args) == 0 {
			printUsage(stderr)
			return 2
		}
	}

	switch args[0] {
	case "-h", "-help", "--help", "help":
		printUsage(stdout)
		return 0
	case "init":
		return runInit(ctx, defaultDir, args[1:], stdout, stderr)
	case "issue":
		return runIssue(ctx, defaultDir, args[1:], stdout, stderr)
	case "review":
		return runReview(ctx, defaultDir, args[1:], stdout, stderr)
	case "sync":
		return runSync(ctx, defaultDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: writ [-C <dir>] <command> [arguments]

Commands:
  init      Initialize writ configuration (writer ID and remote fetch refspecs)
  issue     Manage issues (create, status, assign, list, link)
  review    Manage code reviews (open, comment, approve, status, list)
  sync      Synchronize operations with git remotes

Run 'writ <command> -h' for more information on a command.
`)
}
