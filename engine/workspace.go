package writ

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/state"
)

// WorkspaceInfo contains summary and discovery information about the active workspace.
type WorkspaceInfo struct {
	LocalRepoID     string `json:"local_repo_id,omitempty"`
	WorkspaceRepoID string `json:"workspace_repo_id,omitempty"`
	Slug            string `json:"slug,omitempty"`
	Configured      bool   `json:"configured"`
	Path            string `json:"path,omitempty"`
}

// Workspace provides operations and resolution over a multi-repository workspace.
type Workspace struct {
	store       *Store
	localRepoID string
	configured  bool
	wsPath      string
	wsStore     *Store
	wsOpened    bool
	mu          sync.Mutex
}

func newWorkspace(store *Store, localRepoID string, wsPath string) *Workspace {
	configured := wsPath != ""
	return &Workspace{
		store:       store,
		localRepoID: localRepoID,
		configured:  configured,
		wsPath:      wsPath,
	}
}

// IsConfigured reports whether a workspace repository is configured for this store.
func (w *Workspace) IsConfigured() bool {
	return w != nil && w.configured
}

func (w *Workspace) getStore(ctx context.Context) (*Store, error) {
	if !w.IsConfigured() {
		return nil, ErrWorkspaceUnconfigured
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.wsStore != nil {
		return w.wsStore, nil
	}

	var opts []Option
	if w.store != nil {
		if w.store.syncClient != nil && w.store.syncClient.GitBinary() != "" {
			opts = append(opts, WithGitBinary(w.store.syncClient.GitBinary()))
		}
		if w.store.hasSigner && w.store.signer != nil {
			opts = append(opts, WithSigner(w.store.signer))
		}
	}

	// Open the workspace store
	wsStore, err := Open(w.wsPath, opts...)
	if err != nil {
		return nil, fmt.Errorf("writ: open workspace store %s: %w", w.wsPath, err)
	}

	w.wsStore = wsStore
	w.wsOpened = true
	return w.wsStore, nil
}

// Info returns discovery and configuration metadata for the workspace.
func (w *Workspace) Info() WorkspaceInfo {
	if w == nil {
		return WorkspaceInfo{}
	}

	info := WorkspaceInfo{
		LocalRepoID: w.localRepoID,
		Configured:  w.configured,
		Path:        w.wsPath,
	}

	if !w.configured {
		return info
	}

	wsStore, err := w.getStore(context.Background())
	if err != nil {
		return info
	}

	// Workspace repo ID
	wsRepoID := wsStore.Workspace.localRepoID
	if wsRepoID == "" {
		wsDir := wsStore.gitInfo.WorkTree
		if wsDir == "" {
			wsDir = wsStore.gitInfo.GitDir
		}
		loadedID, _ := identity.LoadRepoID(context.Background(), wsDir)
		wsRepoID = string(loadedID)
	}
	info.WorkspaceRepoID = wsRepoID

	// Look up local repo's slug in the workspace registry if known
	if w.localRepoID != "" {
		_ = wsStore.maybeAutoRefresh(context.Background())
		entry, err := wsStore.projection.Repo(w.localRepoID)
		if err == nil {
			info.Slug = entry.Slug
		}
	}

	return info
}

// Repos returns all registered repositories from the repository registry in the workspace.
func (w *Workspace) Repos(ctx context.Context) ([]RepoEntry, error) {
	if !w.IsConfigured() {
		if w != nil && w.store != nil && w.store.projection != nil {
			if err := w.store.maybeAutoRefresh(ctx); err != nil {
				return nil, err
			}
			return w.store.projection.Repos()
		}
		return nil, nil
	}

	wsStore, err := w.getStore(ctx)
	if err != nil {
		return nil, err
	}

	if err := wsStore.maybeAutoRefresh(ctx); err != nil {
		return nil, err
	}

	return wsStore.projection.Repos()
}

// Register registers or updates the repository registry entry for the local repository in the workspace.
func (w *Workspace) Register(ctx context.Context, slug string, remotes []string) error {
	if !w.IsConfigured() {
		return ErrWorkspaceUnconfigured
	}
	if slug == "" {
		return fmt.Errorf("writ: repo slug cannot be empty")
	}
	if strings.ContainsAny(slug, " \t\r\n") {
		return fmt.Errorf("writ: repo slug cannot contain whitespace")
	}

	wsStore, err := w.getStore(ctx)
	if err != nil {
		return err
	}

	if err := wsStore.ensureWritable(); err != nil {
		return err
	}

	localRepoID := w.localRepoID
	if localRepoID == "" {
		repoDir := w.store.gitInfo.WorkTree
		if repoDir == "" {
			repoDir = w.store.gitInfo.GitDir
		}
		id, _, err := identity.EnsureRepoID(ctx, repoDir)
		if err != nil {
			return fmt.Errorf("writ: ensure local repo id: %w", err)
		}
		localRepoID = string(id)
		w.localRepoID = localRepoID
	}

	if err := wsStore.maybeAutoRefresh(ctx); err != nil {
		return err
	}

	existingRepo, err := wsStore.projection.Repo(localRepoID)
	isNew := errors.Is(err, ErrNotFound)
	if err != nil && !isNew {
		return fmt.Errorf("writ: query repo %s in workspace: %w", localRepoID, err)
	}

	frontier, err := wsStore.projection.Frontier(localRepoID)
	if err != nil {
		return fmt.Errorf("writ: get frontier for repo %s: %w", localRepoID, err)
	}

	if isNew {
		// Determine if this repository is the workspace repository itself
		isWorkspace := false
		localWorkTree := w.store.gitInfo.WorkTree
		wsWorkTree := wsStore.gitInfo.WorkTree
		if localWorkTree != "" && wsWorkTree != "" {
			rel, relErr := filepath.Rel(wsWorkTree, localWorkTree)
			if relErr == nil && rel == "." {
				isWorkspace = true
			}
		}

		body := map[string]any{
			"slug": slug,
		}
		if isWorkspace {
			body["is_workspace"] = true
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("writ: marshal repo create body: %w", err)
		}

		env := codec.Envelope{
			ObjectID:   localRepoID,
			ObjectType: "repo",
			OpType:     "create",
			OpVersion:  1,
			Body:       bodyBytes,
		}

		op, err := wsStore.dagStore.Append(ctx, env, frontier)
		if err != nil {
			return fmt.Errorf("writ: append repo create: %w", err)
		}
		frontier = []string{op.ID}
	} else if existingRepo.Slug != slug {
		body := map[string]any{
			"slug": slug,
		}
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("writ: marshal set-slug body: %w", err)
		}

		env := codec.Envelope{
			ObjectID:   localRepoID,
			ObjectType: "repo",
			OpType:     "set-slug",
			OpVersion:  1,
			Body:       bodyBytes,
		}

		op, err := wsStore.dagStore.Append(ctx, env, frontier)
		if err != nil {
			return fmt.Errorf("writ: append set-slug: %w", err)
		}
		frontier = []string{op.ID}
	}

	existingRemotes := make(map[string]bool)
	if !isNew {
		for _, r := range existingRepo.Remotes {
			existingRemotes[r] = true
		}
	}

	for _, remote := range remotes {
		if remote == "" || existingRemotes[remote] {
			continue
		}
		body := map[string]any{
			"remote": remote,
		}
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("writ: marshal add-remote body: %w", err)
		}

		env := codec.Envelope{
			ObjectID:   localRepoID,
			ObjectType: "repo",
			OpType:     "add-remote",
			OpVersion:  1,
			Body:       bodyBytes,
		}

		op, err := wsStore.dagStore.Append(ctx, env, frontier)
		if err != nil {
			return fmt.Errorf("writ: append add-remote: %w", err)
		}
		frontier = []string{op.ID}
		existingRemotes[remote] = true
	}

	_ = wsStore.maybeAutoRefresh(ctx)
	return nil
}

// Resolve resolves a reference string against the workspace repository registry and local repository ID.
func (w *Workspace) Resolve(ctx context.Context, reference string) (ResolvedReference, error) {
	// Parse first to validate syntax
	if _, _, err := state.ParseReference(reference); err != nil {
		return ResolvedReference{}, err
	}

	var registry []RepoEntry
	if w.IsConfigured() {
		repos, err := w.Repos(ctx)
		if err == nil {
			registry = repos
		}
	}

	return state.ResolveReference(reference, w.localRepoID, registry)
}
