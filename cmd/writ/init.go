package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/sync"
	"github.com/writtendev/writ/internal/gitdir"
)

type initOpts struct {
	dir string
}

func newInitFlagSet(defaultDir string) (*flag.FlagSet, *initOpts) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	opts := &initOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"init"}, initCmd)
	}
	return fs, opts
}

func runInit(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newInitFlagSet(defaultDir)
	fs.SetOutput(stderr)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	targetDir := opts.dir
	if targetDir == "" {
		targetDir = "."
	}

	// 1. Resolve repo root via git rev-parse
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = targetDir
	out, err := cmd.Output()
	var repoRoot string
	if err == nil {
		repoRoot = strings.TrimSpace(string(out))
	} else {
		// Check if target directory is a bare repository
		cmdBare := exec.CommandContext(ctx, "git", "rev-parse", "--is-bare-repository")
		cmdBare.Dir = targetDir
		outBare, errBare := cmdBare.Output()
		if errBare == nil && strings.TrimSpace(string(outBare)) == "true" {
			cmdGitDir := exec.CommandContext(ctx, "git", "rev-parse", "--absolute-git-dir")
			cmdGitDir.Dir = targetDir
			outGitDir, errGitDir := cmdGitDir.Output()
			if errGitDir == nil {
				repoRoot = strings.TrimSpace(string(outGitDir))
			}
		}
	}

	if repoRoot == "" {
		fmt.Fprintf(stderr, "writ init: not a git repository (or any of the parent directories)\n")
		return 1
	}

	// 2. Discover existing chains for collision avoidance
	var taken func(identity.WriterID) bool
	if gitInfo, err := gitdir.Resolve(repoRoot); err == nil {
		storer, err := gitdir.OpenStorage(gitInfo)
		if err != nil {
			fmt.Fprintf(stderr, "writ init: %v\n", err)
			return 1
		}
		if chains, err := dag.Chains(storer); err == nil {
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

	repoID, repoMinted, err := identity.EnsureRepoID(ctx, repoRoot)
	if err != nil {
		fmt.Fprintf(stderr, "writ init: ensure repo ID: %v\n", err)
		return 1
	}

	if repoMinted {
		fmt.Fprintf(stdout, "Repo ID: %s (minted)\n", repoID)
	} else {
		fmt.Fprintf(stdout, "Repo ID: %s (already configured)\n", repoID)
	}

	// 3. Report the person identifier this repo will write into op payloads.
	// It is derived, not minted: writ.personId when set, else email:<user.email>.
	// Reported separately from the identity load below because a repo with no
	// signing key configured still has a person identifier, and because
	// "which person am I?" is the question a new user asks first.
	if gitCfg, cfgErr := identity.ReadGitConfig(ctx, repoRoot); cfgErr == nil {
		personID, personErr := identity.DerivePersonID(gitCfg)
		switch {
		case personErr != nil:
			fmt.Fprintf(stderr, "warning: no person identifier: %v\n", personErr)
			fmt.Fprintf(stderr, "Configure one of:\n")
			fmt.Fprintf(stderr, "  git config %s user:<handle>\n", identity.PersonIDKey)
			fmt.Fprintf(stderr, "  git config user.email <address>\n")
		case gitCfg["writ.personid"] != "":
			fmt.Fprintf(stdout, "Person ID: %s (from %s)\n", personID, identity.PersonIDKey)
		default:
			fmt.Fprintf(stdout, "Person ID: %s (derived from user.email)\n", personID)
		}
	}

	// 4. Load identity to report author and key state
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

	// 5. Configure fetch refspecs for remotes
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
	} else {
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
	}

	// 6. Register repository in workspace if writ.workspace is configured
	gitCfg, _ := identity.ReadGitConfig(ctx, repoRoot)
	if rawWs, ok := gitCfg["writ.workspace"]; ok && strings.TrimSpace(rawWs) != "" {
		wsPath := strings.TrimSpace(rawWs)
		if !filepath.IsAbs(wsPath) {
			wsPath = filepath.Clean(filepath.Join(repoRoot, wsPath))
		}
		store, err := writ.Open(repoRoot)
		if err == nil {
			defer store.Close()
			slug := filepath.Base(repoRoot)
			if store.Workspace != nil && store.Workspace.IsConfigured() {
				var remoteURLs []string
				for _, r := range remotes {
					cmdURL := exec.CommandContext(ctx, "git", "remote", "get-url", r)
					cmdURL.Dir = repoRoot
					if outURL, errURL := cmdURL.Output(); errURL == nil {
						if u := strings.TrimSpace(string(outURL)); u != "" {
							remoteURLs = append(remoteURLs, u)
						}
					}
				}
				if err := store.Workspace.Register(ctx, slug, remoteURLs); err == nil {
					fmt.Fprintf(stdout, "Registered repository in workspace %s (%s)\n", wsPath, slug)
				} else {
					fmt.Fprintf(stderr, "warning: could not register in workspace: %v\n", err)
				}
			}
		}
	}

	return 0
}
