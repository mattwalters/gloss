package writ

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine/dag"
)

// SyncResult reports aggregate statistics from a sync operation.
type SyncResult struct {
	// OpsFetched is the number of new op commits fetched from the remote.
	OpsFetched int `json:"ops_fetched"`

	// OpsPushed is the number of local op commits pushed to the remote.
	OpsPushed int `json:"ops_pushed"`

	// ObjectsTouched is the number of collaborative objects whose materialized state changed.
	ObjectsTouched int `json:"objects_touched"`

	// Unsynced is the remaining number of unpushed local ops for the remote.
	Unsynced int `json:"unsynced"`
}

// SyncStatus reports the synchronization status against a git remote.
type SyncStatus struct {
	// Remote is the name of the git remote (e.g. "origin").
	Remote string `json:"remote"`

	// Unsynced is the number of local op commits not yet pushed to the remote.
	Unsynced int `json:"unsynced"`
}

// Sync ensures fetch refspecs in .git/config, fetches remote operations, pushes local operations,
// and refreshes the projection cache.
func (s *Store) Sync(ctx context.Context, remote string) (SyncResult, error) {
	if s == nil {
		return SyncResult{}, fmt.Errorf("writ: store is nil")
	}
	if remote == "" {
		return SyncResult{}, fmt.Errorf("writ: remote cannot be empty")
	}

	// 1. Ensure fetch refspec
	if _, err := s.syncClient.Ensure(ctx, remote); err != nil {
		return SyncResult{}, fmt.Errorf("writ: ensure refspecs for %s: %w", remote, err)
	}

	// 2. Fetch remote operations
	fetchRes, err := s.syncClient.Fetch(ctx, remote)
	if err != nil {
		return SyncResult{}, fmt.Errorf("writ: fetch %s: %w", remote, err)
	}

	opsFetched := 0
	for _, u := range fetchRes.Updates {
		opsFetched += countCommitsBetween(s.repo, u.Old, u.New)
	}

	// 3. Push local operations if identity is configured
	opsPushed := 0
	if s.hasIdentity && s.identity.WriterID != "" {
		pushRes, err := s.syncClient.Push(ctx, remote)
		if err != nil {
			return SyncResult{}, fmt.Errorf("writ: push %s: %w", remote, err)
		}
		for _, u := range pushRes.Updates {
			opsPushed += countCommitsBetween(s.repo, u.Old, u.New)
		}
	}

	// 4. Refresh projection
	refreshStats, err := s.Refresh(ctx)
	if err != nil {
		return SyncResult{}, fmt.Errorf("writ: refresh after sync: %w", err)
	}

	// 5. Compute remaining unsynced count
	unsynced, err := s.countUnsynced(ctx, remote)
	if err != nil {
		return SyncResult{}, fmt.Errorf("writ: count unsynced: %w", err)
	}

	return SyncResult{
		OpsFetched:     opsFetched,
		OpsPushed:      opsPushed,
		ObjectsTouched: refreshStats.ObjectsTouched,
		Unsynced:       unsynced,
	}, nil
}

// SyncStatus reports the number of local operations not yet pushed to the remote.
func (s *Store) SyncStatus(ctx context.Context, remote string) (SyncStatus, error) {
	if s == nil {
		return SyncStatus{}, fmt.Errorf("writ: store is nil")
	}
	if remote == "" {
		return SyncStatus{}, fmt.Errorf("writ: remote cannot be empty")
	}

	unsynced, err := s.countUnsynced(ctx, remote)
	if err != nil {
		return SyncStatus{}, err
	}

	return SyncStatus{
		Remote:   remote,
		Unsynced: unsynced,
	}, nil
}

func (s *Store) countUnsynced(ctx context.Context, remote string) (int, error) {
	if !s.hasIdentity || s.identity.WriterID == "" {
		return 0, nil
	}

	chains, err := dag.Chains(s.repo.Storer)
	if err != nil {
		return 0, fmt.Errorf("writ: list chains: %w", err)
	}

	totalUnsynced := 0
	localPrefix := fmt.Sprintf("refs/writ/%s/", s.identity.WriterID)
	remotePrefix := fmt.Sprintf("refs/remotes/%s/writ/%s/", remote, s.identity.WriterID)

	for _, chain := range chains {
		refName := chain.Ref.Name.String()
		if len(refName) <= len(localPrefix) || refName[:len(localPrefix)] != localPrefix {
			continue
		}
		objType := chain.Ref.ObjectType
		remoteRefName := plumbing.ReferenceName(remotePrefix + objType)

		remoteChain, exists := chains[remoteRefName.String()]
		var remoteTip plumbing.Hash
		if exists {
			remoteTip = remoteChain.Tip
		}

		totalUnsynced += countCommitsBetween(s.repo, remoteTip, chain.Tip)
	}

	return totalUnsynced, nil
}

func countCommitsBetween(repo *git.Repository, oldHash, newHash plumbing.Hash) int {
	if newHash == plumbing.ZeroHash || repo == nil {
		return 0
	}
	if oldHash == newHash {
		return 0
	}

	startCommit, err := repo.CommitObject(newHash)
	if err != nil {
		return 0
	}
	targetAuthor := startCommit.Author

	count := 0
	curr := newHash
	visited := make(map[plumbing.Hash]bool)
	for curr != plumbing.ZeroHash && curr != oldHash && !visited[curr] {
		visited[curr] = true
		commit, err := repo.CommitObject(curr)
		if err != nil {
			break
		}
		if commit.Author.Name != targetAuthor.Name || commit.Author.Email != targetAuthor.Email {
			break
		}
		count++
		if len(commit.ParentHashes) == 0 {
			break
		}
		curr = commit.ParentHashes[0]
	}
	return count
}
