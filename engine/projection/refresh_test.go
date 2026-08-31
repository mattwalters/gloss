package projection_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/projection"
)

func createTestStore(t *testing.T, writerID string) (*git.Repository, *dag.Store) {
	t.Helper()
	repo, err := git.Init(memory.NewStorage(), nil)
	if err != nil {
		t.Fatalf("git.Init: %v", err)
	}

	id := identity.Identity{
		WriterID: identity.WriterID(writerID),
		Author: identity.Author{
			Name:  "Test Writer",
			Email: "writer@example.com",
		},
	}

	store, err := dag.OpenRepo(repo, id)
	if err != nil {
		t.Fatalf("dag.OpenRepo: %v", err)
	}

	return repo, store
}

func makeReviewEnv(objID, opType string, version int64, body map[string]any) codec.Envelope {
	bodyRaw, _ := json.Marshal(body)
	env := codec.Envelope{
		ObjectID:   objID,
		ObjectType: "review",
		OpType:     opType,
		OpVersion:  version,
		Body:       bodyRaw,
	}
	raw, _ := codec.EncodePayload(env)
	env.Raw = raw
	return env
}

func TestIncrementalRefoldMatchesColdRebuild(t *testing.T) {
	ctx := context.Background()
	_, store := createTestStore(t, "0123456789abcdef")

	db, err := projection.Open(":memory:")
	if err != nil {
		t.Fatalf("Open projection failed: %v", err)
	}
	defer db.Close()

	// 1. Initial op: create review
	env1 := makeReviewEnv("rev-1", "create", 1, map[string]any{
		"title":       "Initial Title",
		"description": "Initial Description",
	})
	_, err = store.Append(ctx, env1, nil)
	if err != nil {
		t.Fatalf("store.Append env1 failed: %v", err)
	}

	stats1, err := db.Refresh(store)
	if err != nil {
		t.Fatalf("Refresh 1 failed: %v", err)
	}
	if stats1.OpsDecoded != 1 || stats1.ObjectsTouched != 1 || stats1.Rebuilt {
		t.Fatalf("unexpected stats1: %+v", stats1)
	}

	var title, desc string
	err = db.DB().QueryRow("SELECT title, description FROM reviews WHERE object_id = 'rev-1'").Scan(&title, &desc)
	if err != nil {
		t.Fatalf("query review failed: %v", err)
	}
	if title != "Initial Title" || desc != "Initial Description" {
		t.Fatalf("unexpected review fields: title=%q desc=%q", title, desc)
	}

	// 2. Incremental op: update title and add revision
	env2 := makeReviewEnv("rev-1", "update", 1, map[string]any{
		"title": "Updated Title",
	})
	_, err = store.Append(ctx, env2, nil)
	if err != nil {
		t.Fatalf("store.Append env2 failed: %v", err)
	}

	env3 := makeReviewEnv("rev-1", "revision", 1, map[string]any{
		"base": "0000000000000000000000000000000000000001",
		"head": "0000000000000000000000000000000000000002",
	})
	_, err = store.Append(ctx, env3, nil)
	if err != nil {
		t.Fatalf("store.Append env3 failed: %v", err)
	}

	stats2, err := db.Refresh(store)
	if err != nil {
		t.Fatalf("Refresh 2 failed: %v", err)
	}
	if stats2.OpsDecoded != 2 || stats2.ObjectsTouched != 1 || stats2.Rebuilt {
		t.Fatalf("unexpected stats2: %+v", stats2)
	}

	incrementalDump, err := db.DumpTables()
	if err != nil {
		t.Fatalf("DumpTables incremental failed: %v", err)
	}

	// 3. Cold rebuild comparison
	statsCold, err := db.Rebuild(store)
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
	if !statsCold.Rebuilt || statsCold.ObjectsTouched != 1 {
		t.Fatalf("unexpected statsCold: %+v", statsCold)
	}

	coldDump, err := db.DumpTables()
	if err != nil {
		t.Fatalf("DumpTables cold failed: %v", err)
	}

	if !reflect.DeepEqual(incrementalDump, coldDump) {
		t.Fatalf("incremental dump != cold dump:\nincremental: %+v\ncold: %+v", incrementalDump, coldDump)
	}
}

func TestNewWriterNamespaceDetected(t *testing.T) {
	ctx := context.Background()
	repo, storeA := createTestStore(t, "0123456789abcdef")

	storeB, err := dag.OpenRepo(repo, identity.Identity{
		WriterID: identity.WriterID("fedcba9876543210"),
		Author: identity.Author{
			Name:  "Writer B",
			Email: "writerB@example.com",
		},
	})
	if err != nil {
		t.Fatalf("dag.OpenRepo storeB: %v", err)
	}

	db, err := projection.Open(":memory:")
	if err != nil {
		t.Fatalf("Open projection failed: %v", err)
	}
	defer db.Close()

	// Writer A creates review
	envA := makeReviewEnv("rev-multi", "create", 1, map[string]any{
		"title": "Title From Writer A",
	})
	_, err = storeA.Append(ctx, envA, nil)
	if err != nil {
		t.Fatalf("storeA.Append: %v", err)
	}

	stats1, err := db.Refresh(storeA)
	if err != nil {
		t.Fatalf("Refresh 1: %v", err)
	}
	if stats1.ObjectsTouched != 1 {
		t.Fatalf("expected 1 object touched, got %d", stats1.ObjectsTouched)
	}

	// Writer B appends approval on rev-multi
	envB := makeReviewEnv("rev-multi", "approval", 1, map[string]any{
		"subject":  "writerB",
		"revision": "0000000000000000000000000000000000000001",
		"verdict":  "approved",
		"message":  "Looks great!",
	})
	_, err = storeB.Append(ctx, envB, nil)
	if err != nil {
		t.Fatalf("storeB.Append: %v", err)
	}

	// Refresh should discover Writer B's new chain with no stored cursor
	stats2, err := db.Refresh(storeA)
	if err != nil {
		t.Fatalf("Refresh 2: %v", err)
	}
	if stats2.OpsDecoded != 1 || stats2.ObjectsTouched != 1 || stats2.Rebuilt {
		t.Fatalf("unexpected stats2: %+v", stats2)
	}

	var verdict string
	err = db.DB().QueryRow("SELECT verdict FROM approvals WHERE review_object_id = 'rev-multi' AND subject = 'writerB'").Scan(&verdict)
	if err != nil {
		t.Fatalf("query approval failed: %v", err)
	}
	if verdict != "approved" {
		t.Fatalf("expected verdict 'approved', got %q", verdict)
	}

	// Verify equal to cold rebuild
	incDump, _ := db.DumpTables()
	_, _ = db.Rebuild(storeA)
	coldDump, _ := db.DumpTables()

	if !reflect.DeepEqual(incDump, coldDump) {
		t.Fatalf("new writer incremental dump differs from cold dump")
	}
}

func TestRollbackTriggersRebuild(t *testing.T) {
	ctx := context.Background()
	repo, store := createTestStore(t, "0123456789abcdef")

	db, err := projection.Open(":memory:")
	if err != nil {
		t.Fatalf("Open projection failed: %v", err)
	}
	defer db.Close()

	// Append 2 ops
	env1 := makeReviewEnv("rev-rb", "create", 1, map[string]any{"title": "Title 1"})
	_, _ = store.Append(ctx, env1, nil)
	env2 := makeReviewEnv("rev-rb", "update", 1, map[string]any{"title": "Title 2"})
	_, _ = store.Append(ctx, env2, nil)

	_, err = db.Refresh(store)
	if err != nil {
		t.Fatalf("Refresh before rewind failed: %v", err)
	}

	// Now rewind the ref by creating a brand new commit not in ancestry and pointing ref at it
	envDivergent := makeReviewEnv("rev-rb", "create", 1, map[string]any{"title": "Divergent Title"})
	c := codec.Commit{
		Author: codec.Identity{
			Name:  "Test Writer",
			Email: "writer@example.com",
			When:  time.Unix(1700000000, 0).UTC(),
		},
		Committer: codec.Identity{
			Name:  "Test Writer",
			Email: "writer@example.com",
			When:  time.Unix(1700000000, 0).UTC(),
		},
		Message: "divergent commit",
		Tree: []codec.TreeEntry{
			{
				Name: "op.json",
				Mode: "100644",
				Data: envDivergent.Raw,
			},
		},
	}
	h, err := codec.WriteCommit(ctx, repo.Storer, &c, nil)
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}

	refName := plumbing.ReferenceName("refs/writ/0123456789abcdef/review")
	err = repo.Storer.SetReference(plumbing.NewReferenceFromStrings(refName.String(), h.String()))
	if err != nil {
		t.Fatalf("force-set reference: %v", err)
	}

	// Refresh should detect rollback (previous tip is not ancestor of new tip) and rebuild
	stats, err := db.Refresh(store)
	if err != nil {
		t.Fatalf("Refresh after rewind failed: %v", err)
	}
	if !stats.Rebuilt {
		t.Fatalf("expected Rebuilt=true after rewind, got false")
	}

	var title string
	err = db.DB().QueryRow("SELECT title FROM reviews WHERE object_id = 'rev-rb'").Scan(&title)
	if err != nil {
		t.Fatalf("query title: %v", err)
	}
	if title != "Divergent Title" {
		t.Fatalf("expected title 'Divergent Title', got %q", title)
	}
}

func TestDisappearedChainTriggersRebuild(t *testing.T) {
	ctx := context.Background()
	repo, store := createTestStore(t, "0123456789abcdef")

	db, err := projection.Open(":memory:")
	if err != nil {
		t.Fatalf("Open projection failed: %v", err)
	}
	defer db.Close()

	env1 := makeReviewEnv("rev-del", "create", 1, map[string]any{"title": "Title Del"})
	_, _ = store.Append(ctx, env1, nil)

	_, err = db.Refresh(store)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Delete the chain ref
	refName := plumbing.ReferenceName("refs/writ/0123456789abcdef/review")
	err = repo.Storer.RemoveReference(refName)
	if err != nil {
		t.Fatalf("RemoveReference: %v", err)
	}

	// Refresh should detect chain disappeared and rebuild
	stats, err := db.Refresh(store)
	if err != nil {
		t.Fatalf("Refresh after ref delete failed: %v", err)
	}
	if !stats.Rebuilt {
		t.Fatalf("expected Rebuilt=true after ref delete, got false")
	}

	var count int
	_ = db.DB().QueryRow("SELECT COUNT(*) FROM objects").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 objects after all refs deleted, got %d", count)
	}
}
