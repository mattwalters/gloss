package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/state"
)

type notFoundError struct {
	kind string
	id   string
}

func (e notFoundError) Error() string {
	kind := e.kind
	if kind == "" {
		kind = "review"
	}
	return fmt.Sprintf("no %s with id %s", kind, e.id)
}

func (e notFoundError) Unwrap() error {
	return writ.ErrNotFound
}

func openStore(dir string) (*writ.Store, error) {
	if dir == "" {
		dir = "."
	}
	return writ.Open(dir)
}

func renderErr(w io.Writer, err error) int {
	if err == nil {
		return 0
	}

	var cfgErr *identity.ConfigError
	if errors.As(err, &cfgErr) {
		fmt.Fprintf(w, "writ: %v\n", cfgErr)
		return 1
	}

	if errors.Is(err, writ.ErrNoIdentity) {
		fmt.Fprintln(w, "writ: no writer identity configured (run 'writ init' to configure)")
		return 1
	}

	if errors.Is(err, writ.ErrNoSigningKey) {
		fmt.Fprintln(w, "writ: no signing key configured (run 'writ init' to configure)")
		return 1
	}

	if errors.Is(err, writ.ErrNotFound) {
		var nf notFoundError
		if errors.As(err, &nf) {
			fmt.Fprintf(w, "writ: %s\n", nf.Error())
			return 1
		}
	}

	msg := err.Error()
	if !strings.HasPrefix(msg, "writ: ") {
		msg = "writ: " + msg
	}
	fmt.Fprintln(w, msg)
	return 1
}

func resolveReviewID(ctx context.Context, store *writ.Store, prefix string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("review ID required")
	}

	reviews, err := store.Query.Reviews(writ.ReviewFilter{})
	if err != nil {
		return "", err
	}

	var matches []string
	for _, r := range reviews {
		if strings.HasPrefix(r.ObjectID, prefix) {
			matches = append(matches, r.ObjectID)
		}
	}

	if len(matches) == 0 {
		return "", notFoundError{kind: "review", id: prefix}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous review ID prefix %q matches %d reviews (%s)", prefix, len(matches), strings.Join(matches, ", "))
	}

	return matches[0], nil
}

func resolveIssueRef(ctx context.Context, store *writ.Store, ref string) (scope string, slug string, objectID string, err error) {
	des, objID, err := state.ParseReference(ref)
	if err != nil {
		return "", "", "", err
	}

	var issueRepoID string
	var registry []writ.RepoEntry
	if store.Workspace != nil {
		info := store.Workspace.Info()
		if info.Configured {
			issueRepoID = info.WorkspaceRepoID
			repos, rErr := store.Workspace.Repos(ctx)
			if rErr == nil {
				registry = repos
			}
		} else {
			issueRepoID = info.LocalRepoID
		}
	}

	if des == "" || (issueRepoID != "" && des == issueRepoID) {
		return "local", "", objID, nil
	}

	for _, entry := range registry {
		if entry.RepoID == des {
			return "cross-repo", entry.Slug, objID, nil
		}
	}

	return "unresolved", "", objID, nil
}

func resolveIssueID(ctx context.Context, store *writ.Store, prefix string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("issue ID required")
	}

	targetPrefix := prefix
	if strings.Contains(prefix, "#") {
		scope, slug, objID, err := resolveIssueRef(ctx, store, prefix)
		if err != nil {
			return "", err
		}
		if scope == "cross-repo" {
			if slug == "" {
				des, _, _ := state.ParseReference(prefix)
				slug = des
			}
			return "", fmt.Errorf("issue lives in repo %s", slug)
		}
		if scope == "unresolved" {
			return "", notFoundError{kind: "issue", id: prefix}
		}
		targetPrefix = objID
	}

	issues, err := store.Query.Issues(writ.IssueFilter{})
	if err != nil {
		return "", err
	}

	var matches []string
	for _, iss := range issues {
		if strings.HasPrefix(iss.ObjectID, targetPrefix) {
			matches = append(matches, iss.ObjectID)
		}
	}

	if len(matches) == 0 {
		return "", notFoundError{kind: "issue", id: prefix}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous issue ID prefix %q matches %d issues (%s)", prefix, len(matches), strings.Join(matches, ", "))
	}

	return matches[0], nil
}

func parseOrderBy(sortOrder string) (writ.OrderBy, error) {
	if sortOrder == "" {
		return "", nil
	}
	switch sortOrder {
	case "created_at_asc", "created-asc", "created_asc", "created":
		return writ.OrderByCreatedAtAsc, nil
	case "created_at_desc", "created-desc", "created_desc":
		return writ.OrderByCreatedAtDesc, nil
	case "updated_at_asc", "updated-asc", "updated_asc":
		return writ.OrderByUpdatedAtAsc, nil
	case "updated_at_desc", "updated-desc", "updated_desc", "updated":
		return writ.OrderByUpdatedAtDesc, nil
	case "title_asc", "title-asc", "title":
		return writ.OrderByTitleAsc, nil
	case "title_desc", "title-desc":
		return writ.OrderByTitleDesc, nil
	default:
		return "", fmt.Errorf("invalid sort order %q", sortOrder)
	}
}

func gitRevParse(ctx context.Context, dir, ref string) (string, error) {
	if dir == "" {
		dir = "."
	}
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", ref+"^{commit}")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback to plain rev-parse if ^{commit} failed (e.g. if ref is already an OID)
		cmdPlain := exec.CommandContext(ctx, "git", "rev-parse", "--verify", ref)
		cmdPlain.Dir = dir
		outPlain, errPlain := cmdPlain.CombinedOutput()
		if errPlain != nil {
			return "", fmt.Errorf("resolve ref %q: %v (%s)", ref, err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(outPlain)), nil
	}
	return strings.TrimSpace(string(out)), nil
}
