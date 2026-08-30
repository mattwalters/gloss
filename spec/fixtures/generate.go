package fixtures

import (
	"fmt"
	"io"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// timeLayout is RFC 3339 in UTC — the only form commit timestamps are
// ever recorded in, so two generations of the same description always
// agree on how a given instant is spelled.
const timeLayout = time.RFC3339

// Generate builds a bare git repository at outDir from desc and returns
// the manifest describing what it produced. outDir must not already
// exist. Every commit is signed by its author's fixture identity (see
// identity.go), so generation requires ssh-keygen (OpenSSH 8.2+) on PATH.
func Generate(desc *Description, outDir string) (*Manifest, error) {
	repo, err := git.PlainInit(outDir, true)
	if err != nil {
		return nil, fmt.Errorf("fixtures: init repo at %s: %w", outDir, err)
	}

	sgnr, err := newSigner()
	if err != nil {
		return nil, err
	}
	defer sgnr.close()

	manifest := &Manifest{}

	for _, ref := range desc.Refs {
		for gi, gen := range ref.History {
			var parent plumbing.Hash
			var parents []plumbing.Hash
			state := GenerationState{Ref: ref.Name, Index: gi, KeptAs: gen.KeptAs}

			for _, cd := range gen.Commits {
				id, err := lookupIdentity(cd.Author)
				if err != nil {
					return nil, err
				}
				if !parent.IsZero() {
					parents = []plumbing.Hash{parent}
				} else {
					parents = nil
				}
				commit, hash, err := buildCommit(repo, sgnr, id, cd, parents)
				if err != nil {
					return nil, fmt.Errorf("fixtures: ref %q generation %d: %w", ref.Name, gi, err)
				}
				parent = hash
				state.Commits = append(state.Commits, commitState(commit, hash))
			}

			isLast := gi == len(ref.History)-1
			if gen.KeptAs != "" {
				if err := setRef(repo, gen.KeptAs, parent); err != nil {
					return nil, err
				}
				manifest.Refs = append(manifest.Refs, RefState{Name: gen.KeptAs, Commit: parent.String()})
			}
			if isLast {
				if err := setRef(repo, ref.Name, parent); err != nil {
					return nil, err
				}
				manifest.Refs = append(manifest.Refs, RefState{Name: ref.Name, Commit: parent.String()})
			}
			manifest.Generations = append(manifest.Generations, state)
		}
	}

	return manifest, nil
}

func setRef(repo *git.Repository, name string, hash plumbing.Hash) error {
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(name), hash)); err != nil {
		return fmt.Errorf("fixtures: set ref %s: %w", name, err)
	}
	return nil
}

// buildCommit writes files as a tree, constructs a commit object with the
// given parents, signs it, and stores it. Returns the decoded commit
// (with its final signature) and its hash.
func buildCommit(repo *git.Repository, sgnr *signer, id identity, cd CommitDesc, parents []plumbing.Hash) (*object.Commit, plumbing.Hash, error) {
	treeHash, err := buildTree(repo.Storer, cd.Files)
	if err != nil {
		return nil, plumbing.ZeroHash, err
	}

	sig := object.Signature{Name: id.Name, Email: id.Email, When: cd.Timestamp.UTC()}
	commit := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      cd.Message,
		TreeHash:     treeHash,
		ParentHashes: parents,
	}

	payloadObj := repo.Storer.NewEncodedObject()
	if err := commit.EncodeWithoutSignature(payloadObj); err != nil {
		return nil, plumbing.ZeroHash, fmt.Errorf("encode commit payload: %w", err)
	}
	r, err := payloadObj.Reader()
	if err != nil {
		return nil, plumbing.ZeroHash, fmt.Errorf("read commit payload: %w", err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, plumbing.ZeroHash, fmt.Errorf("read commit payload: %w", err)
	}

	armored, err := sgnr.sign(id, payload)
	if err != nil {
		return nil, plumbing.ZeroHash, err
	}
	commit.PGPSignature = armored

	commitObj := repo.Storer.NewEncodedObject()
	commitObj.SetType(plumbing.CommitObject)
	if err := commit.Encode(commitObj); err != nil {
		return nil, plumbing.ZeroHash, fmt.Errorf("encode signed commit: %w", err)
	}
	hash, err := repo.Storer.SetEncodedObject(commitObj)
	if err != nil {
		return nil, plumbing.ZeroHash, fmt.Errorf("store commit: %w", err)
	}

	return commit, hash, nil
}

func commitState(c *object.Commit, hash plumbing.Hash) CommitState {
	parents := make([]string, len(c.ParentHashes))
	for i, p := range c.ParentHashes {
		parents[i] = p.String()
	}
	return CommitState{
		SHA:       hash.String(),
		Tree:      c.TreeHash.String(),
		Parents:   parents,
		Author:    fmt.Sprintf("%s <%s>", c.Author.Name, c.Author.Email),
		Timestamp: c.Author.When.Format(timeLayout),
		Message:   c.Message,
		Signed:    c.PGPSignature != "",
	}
}
