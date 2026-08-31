package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/sync"
)

type syncResultJSON struct {
	Remote string `json:"remote"`
	writ.SyncResult
}

type jsonResponse struct {
	Remotes []any `json:"remotes"`
}

func runSync(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var statusMode bool
	var jsonMode bool

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&statusMode, "status", false, "Report unpushed ops count without network transport")
	fs.BoolVar(&jsonMode, "json", false, "Output result as JSON")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ sync [-C <dir>] [--status] [--json] [remote...]

Synchronize collaborative SDLC operations with one or more git remotes.

Fetch remote operations, push local operations, and refresh the local projection cache.
With no remote specified, defaults to 'origin' or the sole configured remote.

Flags:
  -C <dir>    Run as if writ was started in <dir>
      --status Report unpushed ops count without network transport
      --json   Output result as JSON

Exit codes:
  0  Success
  1  Transport or unclassified git failure
  2  Usage error (bad flag, no resolvable default remote)
  3  Unknown or unconfigured remote
  4  Rejected non-fast-forward update
  5  Not a git repository / store cannot be opened
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

	store, err := writ.Open(targetDir)
	if err != nil {
		fmt.Fprintf(stderr, "writ sync: %v\n", err)
		return 5
	}
	defer store.Close()

	if store.Writer().ID == "" {
		fmt.Fprintln(stderr, "warning: no writer identity configured (run 'writ init' to configure)")
	}

	remotes := fs.Args()
	if len(remotes) == 0 {
		configuredRemotes, err := listGitRemotes(ctx, targetDir)
		if err != nil {
			fmt.Fprintf(stderr, "writ sync: list remotes: %v\n", err)
			return 1
		}
		hasOrigin := false
		for _, r := range configuredRemotes {
			if r == "origin" {
				hasOrigin = true
				break
			}
		}
		if hasOrigin {
			remotes = []string{"origin"}
		} else if len(configuredRemotes) == 1 {
			remotes = []string{configuredRemotes[0]}
		} else if len(configuredRemotes) == 0 {
			fmt.Fprintln(stderr, "writ sync: no remotes configured; specify a remote or add one with 'git remote add'")
			return 2
		} else {
			fmt.Fprintln(stderr, "writ sync: multiple remotes configured but none named 'origin'; specify a remote explicitly")
			return 2
		}
	}

	var firstExitCode int
	jsonResp := jsonResponse{
		Remotes: make([]any, 0, len(remotes)),
	}

	for _, remote := range remotes {
		if statusMode {
			status, err := store.SyncStatus(ctx, remote)
			if err != nil {
				fmt.Fprintf(stderr, "writ sync: %s: %v\n", remote, err)
				if firstExitCode == 0 {
					firstExitCode = exitCodeFor(err)
				}
				continue
			}
			if jsonMode {
				jsonResp.Remotes = append(jsonResp.Remotes, status)
			} else {
				fmt.Fprintln(stdout, formatSyncStatus(remote, status))
			}
		} else {
			res, err := store.Sync(ctx, remote)
			if err != nil {
				fmt.Fprintf(stderr, "writ sync: %s: %v\n", remote, err)
				if firstExitCode == 0 {
					firstExitCode = exitCodeFor(err)
				}
				continue
			}
			if jsonMode {
				jsonResp.Remotes = append(jsonResp.Remotes, syncResultJSON{
					Remote:     remote,
					SyncResult: res,
				})
			} else {
				fmt.Fprintln(stdout, formatSyncResult(remote, res))
			}
		}
	}

	if jsonMode && firstExitCode == 0 {
		enc := json.NewEncoder(stdout)
		if err := enc.Encode(jsonResp); err != nil {
			fmt.Fprintf(stderr, "writ sync: marshal json: %v\n", err)
			return 1
		}
	}

	return firstExitCode
}

func listGitRemotes(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var remotes []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			remotes = append(remotes, trimmed)
		}
	}
	return remotes, nil
}

func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, writ.ErrUnknownRemote) || errors.Is(err, sync.ErrUnknownRemote) {
		return 3
	}
	if errors.Is(err, writ.ErrNonFastForward) || errors.Is(err, sync.ErrNonFastForward) {
		return 4
	}
	if strings.Contains(err.Error(), "not a git repository") ||
		strings.Contains(err.Error(), "stat path") ||
		strings.Contains(err.Error(), "resolve absolute path") ||
		strings.Contains(err.Error(), "open git repo") ||
		strings.Contains(err.Error(), "open dag store") ||
		strings.Contains(err.Error(), "open projection db") ||
		strings.Contains(err.Error(), "open sync client") ||
		strings.Contains(err.Error(), "create projection cache dir") ||
		errors.Is(err, git.ErrRepositoryNotExists) {
		return 5
	}
	return 1
}

func plural(n int, singular, pluralStr string) string {
	if n == 1 {
		return singular
	}
	return pluralStr
}

func formatSyncResult(remote string, res writ.SyncResult) string {
	if res.OpsFetched == 0 && res.OpsPushed == 0 && res.ObjectsTouched == 0 && res.Unsynced == 0 {
		return fmt.Sprintf("%s: up to date", remote)
	}

	var parts []string
	if res.OpsFetched > 0 {
		parts = append(parts, fmt.Sprintf("fetched %d %s", res.OpsFetched, plural(res.OpsFetched, "op", "ops")))
	}
	if res.OpsPushed > 0 {
		parts = append(parts, fmt.Sprintf("pushed %d %s", res.OpsPushed, plural(res.OpsPushed, "op", "ops")))
	}
	if res.ObjectsTouched > 0 {
		parts = append(parts, fmt.Sprintf("%d %s updated", res.ObjectsTouched, plural(res.ObjectsTouched, "object", "objects")))
	}
	if res.Unsynced > 0 {
		parts = append(parts, fmt.Sprintf("%d %s unsynced", res.Unsynced, plural(res.Unsynced, "op", "ops")))
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%s: up to date", remote)
	}
	return fmt.Sprintf("%s: %s", remote, strings.Join(parts, ", "))
}

func formatSyncStatus(remote string, status writ.SyncStatus) string {
	if status.Unsynced == 0 {
		return fmt.Sprintf("%s: up to date", remote)
	}
	return fmt.Sprintf("%s: %d %s unsynced", remote, status.Unsynced, plural(status.Unsynced, "op", "ops"))
}
