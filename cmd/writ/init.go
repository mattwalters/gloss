package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/sync"
)

func runInit(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ init [-C <dir>] [remote...]

Initialize writ repository configuration by resolving or minting a writer ID,
verifying SSH signing key configuration, and adding fetch refspecs for git remotes.

Flags:
  -C <dir>    Run as if writ was started in <dir>
`)
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	targetDir := dir
	if targetDir == "" {
		targetDir = "."
	}

	// 1. Resolve repo root via git rev-parse --show-toplevel
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = targetDir
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(stderr, "writ init: not a git repository (or any of the parent directories)\n")
		return 1
	}
	repoRoot := strings.TrimSpace(string(out))

	// 2. Discover existing chains for collision avoidance
	var taken func(identity.WriterID) bool
	repo, err := git.PlainOpenWithOptions(repoRoot, &git.PlainOpenOptions{DetectDotGit: true})
	if err == nil {
		if chains, err := dag.Chains(repo.Storer); err == nil {
			existing := make(map[identity.WriterID]struct{}, len(chains))
			for _, chain := range chains {
				existing[chain.Ref.WriterID] = struct{}{}
			}
			taken = func(id identity.WriterID) bool {
				_, ok := existing[id]
				return ok
			}
		}
	}

	writerID, minted, err := identity.EnsureWriterID(ctx, repoRoot, taken)
	if err != nil {
		fmt.Fprintf(stderr, "writ init: ensure writer ID: %v\n", err)
		return 1
	}

	if minted {
		fmt.Fprintf(stdout, "Writer ID: %s (minted)\n", writerID)
	} else {
		fmt.Fprintf(stdout, "Writer ID: %s (already configured)\n", writerID)
	}

	// 3. Load identity to report author and key state
	ident, err := identity.Load(ctx, repoRoot)
	if err != nil {
		var cfgErr *identity.ConfigError
		if errors.As(err, &cfgErr) {
			switch {
			case errors.Is(cfgErr.Problem, identity.ErrMissing) || errors.Is(cfgErr.Problem, identity.ErrUnsupportedFormat) || errors.Is(cfgErr.Problem, identity.ErrInvalid):
				if cfgErr.Key == "gpg.format" || cfgErr.Key == "user.signingKey" || strings.HasPrefix(cfgErr.Key, "user.") {
					fmt.Fprintf(stderr, "warning: SSH signing key or identity not fully configured (%v)\n", cfgErr)
					fmt.Fprintf(stderr, "To configure SSH signing for Writ and Git:\n")
					fmt.Fprintf(stderr, "  git config gpg.format ssh\n")
					fmt.Fprintf(stderr, "  git config user.signingKey ~/.ssh/id_ed25519.pub\n")
					fmt.Fprintf(stderr, "Optionally configure verification allowed signers:\n")
					fmt.Fprintf(stderr, "  git config gpg.ssh.allowedSignersFile ~/.ssh/allowed_signers\n")
				} else {
					fmt.Fprintf(stderr, "warning: identity configuration: %v\n", cfgErr)
				}
			default:
				fmt.Fprintf(stderr, "warning: identity configuration: %v\n", err)
			}
		} else {
			fmt.Fprintf(stderr, "warning: identity configuration: %v\n", err)
		}
	} else {
		if ident.Key.Literal {
			fmt.Fprintf(stdout, "Signing key: key::%s (ssh)\n", ident.Key.Value)
		} else {
			fmt.Fprintf(stdout, "Signing key: %s (ssh)\n", ident.Key.Value)
		}
	}

	// 4. Configure fetch refspecs for remotes
	remotes := fs.Args()
	if len(remotes) == 0 {
		cmdRemote := exec.CommandContext(ctx, "git", "remote")
		cmdRemote.Dir = repoRoot
		outRemote, err := cmdRemote.Output()
		if err != nil {
			fmt.Fprintf(stderr, "writ init: list remotes: %v\n", err)
			return 1
		}
		lines := strings.Split(strings.TrimSpace(string(outRemote)), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				remotes = append(remotes, trimmed)
			}
		}
	}

	if len(remotes) == 0 {
		fmt.Fprintln(stdout, "No git remotes configured; fetch refspec will be added when a remote is configured.")
		return 0
	}

	client, err := sync.Open(repoRoot, identity.Identity{WriterID: writerID})
	if err != nil {
		fmt.Fprintf(stderr, "writ init: open sync client: %v\n", err)
		return 1
	}

	for _, remote := range remotes {
		status, err := client.Ensure(ctx, remote)
		if err != nil {
			fmt.Fprintf(stderr, "writ init: remote %q: %v\n", remote, err)
			return 1
		}
		if status.Repaired {
			fmt.Fprintf(stdout, "Configured fetch refspec for remote %q (%s)\n", remote, status.Expected)
		} else {
			fmt.Fprintf(stdout, "Fetch refspec for remote %q is already configured (%s)\n", remote, status.Expected)
		}
	}

	return 0
}
