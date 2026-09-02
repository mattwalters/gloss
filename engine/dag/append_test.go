package dag_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
)

func testIdentity(wID string, name string, email string) identity.Identity {
	writerID, _ := identity.ParseWriterID(wID)
	return identity.Identity{
		WriterID: writerID,
		Author: identity.Author{
			Name:  name,
			Email: email,
		},
	}
}

func initTestRepo(t *testing.T) (string, *git.Repository) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit failed: %v", err)
	}
	return dir, repo
}

func snapshotRefs(t *testing.T, repo *git.Repository) map[string]string {
	t.Helper()
	iter, err := repo.References()
	if err != nil {
		t.Fatalf("References failed: %v", err)
	}
	defer iter.Close()
	refs := make(map[string]string)
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref != nil && ref.Type() == plumbing.HashReference {
			refs[ref.Name().String()] = ref.Hash().String()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach ref: %v", err)
	}
	return refs
}

func TestAppend_AtomicAndLocalChainOnly(t *testing.T) {
	dir, repo := initTestRepo(t)
	ident := testIdentity("0123456789abcdef", "Alice", "alice@example.test")

	// Set an unrelated branch and tag to ensure they are untouched
	dummyCommit := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	_ = repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", dummyCommit))
	_ = repo.Storer.SetReference(plumbing.NewHashReference("refs/tags/v1.0.0", dummyCommit))

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store, err := dag.Open(dir, ident, dag.WithNow(func() time.Time { return fixedTime }))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	before := snapshotRefs(t, repo)

	env1 := codec.Envelope{
		ObjectID:   "rev-1",
		ObjectType: "review",
		OpType:     "create",
		OpVersion:  1,
		Body:       json.RawMessage(`{"title":"Review 1"}`),
	}

	op1, err := store.Append(context.Background(), env1, nil)
	if err != nil {
		t.Fatalf("Append 1 failed: %v", err)
	}

	after1 := snapshotRefs(t, repo)

	// Assert exactly one ref moved: refs/writ/0123456789abcdef/review
	expectedRef := "refs/writ/0123456789abcdef/review"
	if len(after1) != len(before)+1 {
		t.Fatalf("expected ref count %d, got %d", len(before)+1, len(after1))
	}
	if after1[expectedRef] != op1.ID {
		t.Fatalf("ref %s = %s, want %s", expectedRef, after1[expectedRef], op1.ID)
	}
	for k, v := range before {
		if after1[k] != v {
			t.Errorf("ref %s unexpectedly changed from %s to %s", k, v, after1[k])
		}
	}

	// Verify commit is a root commit (0 parents)
	commitObj, err := repo.CommitObject(plumbing.NewHash(op1.ID))
	if err != nil {
		t.Fatalf("CommitObject failed: %v", err)
	}
	if len(commitObj.ParentHashes) != 0 {
		t.Fatalf("root op should have 0 parents, got %d", len(commitObj.ParentHashes))
	}

	// Append second op onto same chain
	env2 := codec.Envelope{
		ObjectID:   "rev-1",
		ObjectType: "review",
		OpType:     "update",
		OpVersion:  1,
		Body:       json.RawMessage(`{"title":"Review 1 updated"}`),
	}

	op2, err := store.Append(context.Background(), env2, nil)
	if err != nil {
		t.Fatalf("Append 2 failed: %v", err)
	}

	after2 := snapshotRefs(t, repo)
	if len(after2) != len(after1) {
		t.Fatalf("ref count changed: %d -> %d", len(after1), len(after2))
	}
	if after2[expectedRef] != op2.ID {
		t.Fatalf("ref %s = %s, want %s", expectedRef, after2[expectedRef], op2.ID)
	}

	// Verify second commit has parents[0] = op1.ID (chain spine)
	commitObj2, err := repo.CommitObject(plumbing.NewHash(op2.ID))
	if err != nil {
		t.Fatalf("CommitObject 2 failed: %v", err)
	}
	if len(commitObj2.ParentHashes) != 1 || commitObj2.ParentHashes[0].String() != op1.ID {
		t.Fatalf("commit 2 parents = %v, want [%s]", commitObj2.ParentHashes, op1.ID)
	}
}

func TestAppend_CausalParents(t *testing.T) {
	dir, repo := initTestRepo(t)
	ident := testIdentity("0123456789abcdef", "Alice", "alice@example.test")
	store, err := dag.Open(dir, ident)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Append op 1 (root on review chain)
	env1 := codec.Envelope{
		ObjectID:   "rev-1",
		ObjectType: "review",
		OpType:     "create",
		OpVersion:  1,
		Body:       json.RawMessage(`{"title":"Initial"}`),
	}
	op1, err := store.Append(context.Background(), env1, nil)
	if err != nil {
		t.Fatalf("Append 1 failed: %v", err)
	}

	// Append op on comment chain referencing op1 as causal parent
	envComment := codec.Envelope{
		ObjectID:   "rev-1",
		ObjectType: "comment",
		OpType:     "create",
		OpVersion:  1,
		Body:       json.RawMessage(`{"subject":{"object_type":"review","object_id":"rev-1"},"text":"hello"}`),
	}
	opComment, err := store.Append(context.Background(), envComment, []string{op1.ID})
	if err != nil {
		t.Fatalf("Append comment failed: %v", err)
	}

	// Because comment chain was empty, parents[0] is the causal parent op1.ID
	if len(opComment.Parents) != 1 || opComment.Parents[0] != op1.ID {
		t.Fatalf("comment parents = %v, want [%s]", opComment.Parents, op1.ID)
	}

	// Now append a second comment with another causal parent
	opComment2, err := store.Append(context.Background(), envComment, []string{op1.ID})
	if err != nil {
		t.Fatalf("Append second comment failed: %v", err)
	}
	// parents[0] must be comment predecessor (opComment.ID), parents[1] is causal parent (op1.ID)
	if len(opComment2.Parents) != 2 || opComment2.Parents[0] != opComment.ID || opComment2.Parents[1] != op1.ID {
		t.Fatalf("comment2 parents = %v, want [%s, %s]", opComment2.Parents, opComment.ID, op1.ID)
	}

	// Test invalid causal parents:
	// 1. Non-existent hash
	_, err = store.Append(context.Background(), envComment, []string{"9999999999999999999999999999999999999999"})
	if !errors.Is(err, dag.ErrInvalidParent) {
		t.Errorf("expected ErrInvalidParent, got %v", err)
	}

	// 2. Non-op commit (commit without op.json)
	nonOpHash, err := writeNonOpCommit(repo)
	if err != nil {
		t.Fatalf("writeNonOpCommit failed: %v", err)
	}
	_, err = store.Append(context.Background(), envComment, []string{nonOpHash.String()})
	if !errors.Is(err, dag.ErrNonOpParent) {
		t.Errorf("expected ErrNonOpParent, got %v", err)
	}
}

func writeNonOpCommit(repo *git.Repository) (plumbing.Hash, error) {
	// Create a commit with empty tree (no op.json)
	treeObj := repo.Storer.NewEncodedObject()
	treeObj.SetType(plumbing.TreeObject)
	treeHash, err := repo.Storer.SetEncodedObject(treeObj)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	commit := &object.Commit{
		Author: object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now().UTC(),
		},
		Committer: object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now().UTC(),
		},
		Message:  "non-op commit",
		TreeHash: treeHash,
	}
	commitObj := repo.Storer.NewEncodedObject()
	commitObj.SetType(plumbing.CommitObject)
	if err := commit.Encode(commitObj); err != nil {
		return plumbing.ZeroHash, err
	}
	return repo.Storer.SetEncodedObject(commitObj)
}

func TestAppend_ConcurrentRace(t *testing.T) {
	dir, repo := initTestRepo(t)
	ident := testIdentity("0123456789abcdef", "Alice", "alice@example.test")
	store, err := dag.Open(dir, ident)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	const goroutines = 10
	const opsPerGoroutine = 10
	const totalOps = goroutines * opsPerGoroutine

	var wg sync.WaitGroup
	wg.Add(goroutines)

	type appendResult struct {
		op  *codec.Op
		err error
	}
	results := make(chan appendResult, totalOps)

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				env := codec.Envelope{
					ObjectID:   fmt.Sprintf("rev-%d-%d", gid, i),
					ObjectType: "review",
					OpType:     "create",
					OpVersion:  1,
					Body:       json.RawMessage(`{"title":"Initial"}`),
				}
				op, err := store.Append(context.Background(), env, nil)
				results <- appendResult{op: op, err: err}
			}
		}(g)
	}

	wg.Wait()
	close(results)

	allOps := make(map[string]bool)
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent append error: %v", r.err)
		}
		allOps[r.op.ID] = true
	}

	if len(allOps) != totalOps {
		t.Fatalf("expected %d unique ops, got %d", totalOps, len(allOps))
	}

	// Check final chain tip
	refName := dag.LocalRefName(ident.WriterID, "review")
	ref, err := repo.Reference(refName, true)
	if err != nil {
		t.Fatalf("Reference failed: %v", err)
	}

	// Walk from final tip down to root along parents[0]
	// Verify that ALL totalOps are reachable along the single spine!
	spineCount := 0
	currHash := ref.Hash()
	visitedOnSpine := make(map[string]bool)

	for !currHash.IsZero() {
		commitObj, err := repo.CommitObject(currHash)
		if err != nil {
			t.Fatalf("CommitObject %s failed: %v", currHash, err)
		}
		if !allOps[currHash.String()] {
			t.Fatalf("commit %s on spine was not in appended ops", currHash)
		}
		visitedOnSpine[currHash.String()] = true
		spineCount++

		if len(commitObj.ParentHashes) == 0 {
			break
		}
		currHash = commitObj.ParentHashes[0]
	}

	if spineCount != totalOps {
		t.Fatalf("expected spine length %d, got %d (some ops were lost)", totalOps, spineCount)
	}
}

func TestAppend_WithSigner(t *testing.T) {
	dir, repo := initTestRepo(t)
	ident := testIdentity("0123456789abcdef", "Alice", "alice@example.test")

	signerCalled := false
	signer := dag.SignerFunc(func(_ context.Context, payload []byte) (string, error) {
		signerCalled = true
		return "-----BEGIN SSH SIGNATURE-----\nsignature-bytes\n-----END SSH SIGNATURE-----", nil
	})

	store, err := dag.Open(dir, ident, dag.WithSigner(signer))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	env := codec.Envelope{
		ObjectID:   "rev-1",
		ObjectType: "review",
		OpType:     "create",
		OpVersion:  1,
		Body:       json.RawMessage(`{"title":"Initial"}`),
	}

	op, err := store.Append(context.Background(), env, nil)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	if !signerCalled {
		t.Errorf("signer was not called")
	}
	if op.Signature == "" {
		t.Errorf("op.Signature is empty")
	}

	commitObj, err := repo.CommitObject(plumbing.NewHash(op.ID))
	if err != nil {
		t.Fatalf("CommitObject failed: %v", err)
	}
	if commitObj.PGPSignature == "" {
		t.Errorf("commit.PGPSignature is empty in storage")
	}
}

func TestAppend_InvalidObjectType(t *testing.T) {
	dir, _ := initTestRepo(t)
	ident := testIdentity("0123456789abcdef", "Alice", "alice@example.test")
	store, err := dag.Open(dir, ident)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	env := codec.Envelope{
		ObjectID:   "rev-1",
		ObjectType: "Invalid_Type!",
		OpType:     "create",
		OpVersion:  1,
		Body:       json.RawMessage(`{}`),
	}

	_, err = store.Append(context.Background(), env, nil)
	if err == nil {
		t.Fatalf("expected error on invalid object type")
	}
}
