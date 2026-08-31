package codec

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
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

	var payload []byte
	payloadObj := &plumbing.MemoryObject{}
	if err := commit.EncodeWithoutSignature(payloadObj); err == nil {
		if r, err := payloadObj.Reader(); err == nil {
			payload, _ = io.ReadAll(r)
			_ = r.Close()
		}
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
		Payload:   payload,
		Tree:      treeEntries,
	}, nil
}

// ToGitCommit converts a pure, repository-independent Commit into a go-git object.Commit,
// reconstructing the tree and blob objects in memory to derive the TreeHash and commit Hash.
func ToGitCommit(c Commit) (*object.Commit, error) {
	treeHash, err := buildTreeFromEntries(c.Tree, nil)
	if err != nil {
		return nil, fmt.Errorf("codec: build commit tree: %w", err)
	}

	parents := make([]plumbing.Hash, len(c.Parents))
	for i, p := range c.Parents {
		parents[i] = plumbing.NewHash(p)
	}

	gitCommit := &object.Commit{
		Author: object.Signature{
			Name:  c.Author.Name,
			Email: c.Author.Email,
			When:  c.Author.When,
		},
		Committer: object.Signature{
			Name:  c.Committer.Name,
			Email: c.Committer.Email,
			When:  c.Committer.When,
		},
		Message:      c.Message,
		TreeHash:     treeHash,
		ParentHashes: parents,
		PGPSignature: c.Signature,
	}

	if c.ID != "" {
		gitCommit.Hash = plumbing.NewHash(c.ID)
	} else {
		cObj := &plumbing.MemoryObject{}
		cObj.SetType(plumbing.CommitObject)
		if err := gitCommit.Encode(cObj); err == nil {
			gitCommit.Hash = cObj.Hash()
		}
	}

	return gitCommit, nil
}

// buildTreeFromEntries derives the tree hash for entries. When s is non-nil
// the blob and tree objects are also persisted into it, so a caller that writes
// objects and a caller that only computes hashes agree by construction.
func buildTreeFromEntries(entries []TreeEntry, s storage.Storer) (plumbing.Hash, error) {
	tree := &object.Tree{}

	for _, entry := range entries {
		var mode filemode.FileMode
		if entry.Mode != "" {
			if parsedMode, err := filemode.New(entry.Mode); err == nil {
				mode = parsedMode
			}
		}
		if mode == 0 {
			mode = filemode.Regular
		}

		var hash plumbing.Hash
		if len(entry.Entries) > 0 {
			subHash, err := buildTreeFromEntries(entry.Entries, s)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			hash = subHash
			mode = filemode.Dir
		} else if entry.Data != nil {
			blobObj := &plumbing.MemoryObject{}
			blobObj.SetType(plumbing.BlobObject)
			blobObj.SetSize(int64(len(entry.Data)))
			w, err := blobObj.Writer()
			if err != nil {
				return plumbing.ZeroHash, err
			}
			if _, err := w.Write(entry.Data); err != nil {
				_ = w.Close()
				return plumbing.ZeroHash, err
			}
			if err := w.Close(); err != nil {
				return plumbing.ZeroHash, err
			}
			hash = blobObj.Hash()
			if s != nil {
				if _, err := s.SetEncodedObject(blobObj); err != nil {
					return plumbing.ZeroHash, fmt.Errorf("store blob: %w", err)
				}
			}
		} else if entry.Hash != "" {
			hash = plumbing.NewHash(entry.Hash)
		}

		tree.Entries = append(tree.Entries, object.TreeEntry{
			Name: entry.Name,
			Mode: mode,
			Hash: hash,
		})
	}

	treeObj := &plumbing.MemoryObject{}
	treeObj.SetType(plumbing.TreeObject)
	if err := tree.Encode(treeObj); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode tree: %w", err)
	}
	if s != nil {
		if _, err := s.SetEncodedObject(treeObj); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("store tree: %w", err)
		}
	}
	return treeObj.Hash(), nil
}

// WriteCommit persists a Commit — its blobs, tree, and the commit object — into s,
// signing it first when signer is non-nil. The written commit's hash is recorded in
// commit.ID and returned. Object hashes are derived by the same builder ToGitCommit
// uses, so a written commit and a computed one agree.
func WriteCommit(ctx context.Context, s storage.Storer, commit *Commit, signer Signer) (plumbing.Hash, error) {
	if s == nil {
		return plumbing.ZeroHash, errors.New("codec: nil storer")
	}
	if commit == nil {
		return plumbing.ZeroHash, errors.New("codec: nil commit")
	}

	if _, err := buildTreeFromEntries(commit.Tree, s); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("codec: write commit tree: %w", err)
	}

	if signer != nil {
		if err := SignCommit(ctx, signer, commit); err != nil {
			return plumbing.ZeroHash, err
		}
	}

	gitCommit, err := ToGitCommit(*commit)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("codec: build commit object: %w", err)
	}
	commitObj := s.NewEncodedObject()
	if err := gitCommit.Encode(commitObj); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("codec: encode commit: %w", err)
	}
	commitHash, err := s.SetEncodedObject(commitObj)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("codec: store commit: %w", err)
	}

	commit.ID = commitHash.String()
	return commitHash, nil
}
