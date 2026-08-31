package codec

import (
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// FromGitCommit converts a go-git object.Commit into a pure, repository-independent Commit
// value by reading its tree entries and op.json blob data.
func FromGitCommit(repo *git.Repository, commit *object.Commit) (Commit, error) {
	if commit == nil {
		return Commit{}, errors.New("codec: nil git commit")
	}

	tree, err := commit.Tree()
	if err != nil {
		return Commit{}, fmt.Errorf("codec: commit tree: %w", err)
	}

	var treeEntries []TreeEntry
	for _, entry := range tree.Entries {
		te := TreeEntry{
			Name: entry.Name,
			Mode: entry.Mode.String(),
			Hash: entry.Hash.String(),
		}
		if entry.Name == "op.json" {
			blob, err := tree.TreeEntryFile(&entry)
			if err == nil {
				r, err := blob.Reader()
				if err == nil {
					data, err := io.ReadAll(r)
					_ = r.Close()
					if err == nil {
						te.Data = data
					}
				}
			}
		}
		if entry.Mode == filemode.Dir && repo != nil {
			subTree, err := repo.TreeObject(entry.Hash)
			if err == nil {
				for _, subEntry := range subTree.Entries {
					subTe := TreeEntry{
						Name: subEntry.Name,
						Mode: subEntry.Mode.String(),
						Hash: subEntry.Hash.String(),
					}
					if subEntry.Name == "op.json" {
						subBlob, err := subTree.TreeEntryFile(&subEntry)
						if err == nil {
							r, err := subBlob.Reader()
							if err == nil {
								data, err := io.ReadAll(r)
								_ = r.Close()
								if err == nil {
									subTe.Data = data
								}
							}
						}
					}
					te.Entries = append(te.Entries, subTe)
				}
			}
		}
		treeEntries = append(treeEntries, te)
	}

	parents := make([]string, len(commit.ParentHashes))
	for i, p := range commit.ParentHashes {
		parents[i] = p.String()
	}

	return Commit{
		ID:      commit.Hash.String(),
		Parents: parents,
		Author: Identity{
			Name:  commit.Author.Name,
			Email: commit.Author.Email,
			When:  commit.Author.When,
		},
		Committer: Identity{
			Name:  commit.Committer.Name,
			Email: commit.Committer.Email,
			When:  commit.Committer.When,
		},
		Message:   commit.Message,
		Signature: commit.PGPSignature,
		Tree:      treeEntries,
	}, nil
}
