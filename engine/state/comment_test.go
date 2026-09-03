package state_test

import (
	"encoding/json"
	"math/rand"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	s "github.com/writtendev/writ/engine/state"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/spec/fixtures"
)

func TestFoldCommentsThreadsFixture(t *testing.T) {
	corpus, err := fixtures.LoadCorpus()
	if err != nil {
		t.Fatalf("fixtures.LoadCorpus: %v", err)
	}

	var commentDesc *fixtures.Description
	for _, desc := range corpus {
		if desc.Name == "fold-comment-threads" {
			commentDesc = desc
			break
		}
	}
	if commentDesc == nil {
		t.Fatal("fixture description fold-comment-threads not found")
	}

	repoDir := filepath.Join(t.TempDir(), "repo")
	if _, err := fixtures.Generate(commentDesc, repoDir); err != nil {
		t.Fatalf("fixtures.Generate: %v", err)
	}

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("git.PlainOpen: %v", err)
	}

	store, err := dag.OpenRepo(repo, identity.Identity{})
	if err != nil {
		t.Fatalf("dag.OpenRepo: %v", err)
	}

	enumRes, err := store.Enumerate()
	if err != nil {
		t.Fatalf("store.Enumerate: %v", err)
	}

	var allOps []codec.Op
	for _, ops := range enumRes.Ops {
		allOps = append(allOps, ops...)
	}

	// 1. Fold the comment threads
	threads, err := s.FoldComments(allOps)
	if err != nil {
		t.Fatalf("s.FoldComments: %v", err)
	}

	if len(threads) != 2 {
		t.Fatalf("expected 2 root threads (c-root, c-del-target), got %d", len(threads))
	}

	// Roots should be ordered by creation (t*, commit):
	// c-root was created at 00:00:00, c-del-target was created at 00:01:10
	if threads[0].ObjectID != "c-root" {
		t.Errorf("expected first root to be 'c-root', got %q", threads[0].ObjectID)
	}
	if threads[1].ObjectID != "c-del-target" {
		t.Errorf("expected second root to be 'c-del-target', got %q", threads[1].ObjectID)
	}

	// c-root assertions
	cRoot := threads[0]
	if cRoot.Comment.Deleted {
		t.Errorf("expected c-root not deleted")
	}
	if cRoot.Comment.Text != "Bob edited root comment" {
		t.Errorf("expected c-root text 'Bob edited root comment', got %q", cRoot.Comment.Text)
	}
	if cRoot.Comment.Anchor == nil {
		t.Errorf("expected c-root to have anchor")
	}
	if len(cRoot.Replies) != 1 {
		t.Fatalf("expected c-root to have 1 reply (c-reply), got %d", len(cRoot.Replies))
	}

	// c-reply assertions
	cReply := cRoot.Replies[0]
	if cReply.ObjectID != "c-reply" {
		t.Errorf("expected reply to be 'c-reply', got %q", cReply.ObjectID)
	}
	if !cReply.Comment.Deleted {
		t.Errorf("expected c-reply to be deleted (deleted: true)")
	}
	if cReply.Comment.Text != "Bob post-delete edit" {
		t.Errorf("expected c-reply text 'Bob post-delete edit', got %q", cReply.Comment.Text)
	}
	if cReply.Comment.InReplyTo != "c-root" {
		t.Errorf("expected c-reply in_reply_to 'c-root', got %q", cReply.Comment.InReplyTo)
	}
	if len(cReply.Replies) != 0 {
		t.Errorf("expected c-reply to have 0 replies, got %d", len(cReply.Replies))
	}

	// c-del-target assertions
	cDel := threads[1]
	if !cDel.Comment.Deleted {
		t.Errorf("expected c-del-target to be deleted (deleted: true)")
	}
	if cDel.Comment.Text != "Alice concurrent edit on del target" {
		t.Errorf("expected c-del-target text 'Alice concurrent edit on del target', got %q", cDel.Comment.Text)
	}
	if len(cDel.Replies) != 0 {
		t.Errorf("expected c-del-target to have 0 replies, got %d", len(cDel.Replies))
	}

	// 2. Commutativity under 100 input shuffles
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 100; i++ {
		shuffledOps := make([]codec.Op, len(allOps))
		copy(shuffledOps, allOps)
		r.Shuffle(len(shuffledOps), func(a, b int) {
			shuffledOps[a], shuffledOps[b] = shuffledOps[b], shuffledOps[a]
		})

		shuffledThreads, err := s.FoldComments(shuffledOps)
		if err != nil {
			t.Fatalf("permutation %d failed: %v", i, err)
		}

		if !reflect.DeepEqual(shuffledThreads, threads) {
			t.Fatalf("permutation %d output differed from canonical fold output", i)
		}
	}
}

func TestFoldCommentUnknownOps(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	ops := []codec.Op{
		{
			ID: "c1-create",
			Envelope: codec.Envelope{
				ObjectID:   "c-1",
				ObjectType: "comment",
				OpType:     "create",
				OpVersion:  1,
				Body:       []byte(`{"subject":{"object_type":"review","object_id":"r-1"},"text":"Initial text"}`),
			},
			Author: codec.Identity{When: baseTime},
		},
		{
			ID:      "c1-future",
			Parents: []string{"c1-create"},
			Envelope: codec.Envelope{
				ObjectID:   "c-1",
				ObjectType: "comment",
				OpType:     "react",
				OpVersion:  2,
				Body:       []byte(`{"reaction":"thumbs_up"}`),
			},
			Author: codec.Identity{When: baseTime.Add(time.Minute)},
		},
	}

	c, err := s.FoldComment(ops)
	if err != nil {
		t.Fatalf("s.FoldComment failed: %v", err)
	}

	if len(c.UnknownOps) != 1 {
		t.Fatalf("expected 1 UnknownOp on Comment, got %d", len(c.UnknownOps))
	}
	if c.UnknownOps[0].Commit != "c1-future" || c.UnknownOps[0].OpType != "react" || c.UnknownOps[0].OpVersion != 2 {
		t.Errorf("unexpected UnknownOp: %+v", c.UnknownOps[0])
	}

	threads, err := s.FoldComments(ops)
	if err != nil {
		t.Fatalf("s.FoldComments failed: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread root, got %d", len(threads))
	}
	if len(threads[0].UnknownOps) != 1 {
		t.Fatalf("expected 1 UnknownOp on CommentThread, got %d", len(threads[0].UnknownOps))
	}
	if threads[0].UnknownOps[0].Commit != "c1-future" {
		t.Errorf("unexpected thread UnknownOp: %+v", threads[0].UnknownOps[0])
	}
}

func TestFoldCommentEmptyValues(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	ops := []codec.Op{
		{
			ID: "c-create",
			Envelope: codec.Envelope{
				ObjectID:   "c-empty-val",
				ObjectType: "comment",
				OpType:     "create",
				OpVersion:  1,
				Body:       []byte(`{"subject":{"object_type":"review","object_id":"r-1"},"text":"Initial text"}`),
			},
			Author: codec.Identity{When: baseTime},
		},
		{
			ID:      "c-edit",
			Parents: []string{"c-create"},
			Envelope: codec.Envelope{
				ObjectID:   "c-empty-val",
				ObjectType: "comment",
				OpType:     "edit",
				OpVersion:  1,
				Body:       []byte(`{"text":""}`),
			},
			Author: codec.Identity{When: baseTime.Add(time.Minute)},
		},
		{
			ID:      "c-resolve",
			Parents: []string{"c-edit"},
			Envelope: codec.Envelope{
				ObjectID:   "c-empty-val",
				ObjectType: "comment",
				OpType:     "resolve",
				OpVersion:  1,
				Body:       []byte(`{"resolved":true,"resolved_by":"   "}`),
			},
			Author: codec.Identity{When: baseTime.Add(2 * time.Minute)},
		},
	}

	c, err := s.FoldComment(ops)
	if err != nil {
		t.Fatalf("s.FoldComment failed: %v", err)
	}

	// 1. Verify in-memory typed values reflect empty strings
	if c.Text != "" {
		t.Errorf("expected empty c.Text in memory, got %q", c.Text)
	}
	if c.ResolvedBy != "" {
		t.Errorf("expected empty c.ResolvedBy in memory, got %q", c.ResolvedBy)
	}
	if !c.IsResolved() {
		t.Errorf("expected c.IsResolved() to be true")
	}

	// 2. Verify JSON serialization omits the empty fields via omitempty tags
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshaling comment: %v", err)
	}

	var rawMap map[string]any
	if err := json.Unmarshal(b, &rawMap); err != nil {
		t.Fatalf("unmarshaling comment JSON: %v", err)
	}

	if _, ok := rawMap["text"]; ok {
		t.Errorf("expected 'text' field to be omitted from JSON via omitempty, got: %v", rawMap["text"])
	}
	if _, ok := rawMap["resolved_by"]; ok {
		t.Errorf("expected 'resolved_by' field to be omitted from JSON via omitempty, got: %v", rawMap["resolved_by"])
	}
	if resolved, ok := rawMap["resolved"].(bool); !ok || !resolved {
		t.Errorf("expected 'resolved': true in JSON, got: %v", rawMap["resolved"])
	}
}

