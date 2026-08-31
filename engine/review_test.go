package writ_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/spec"
)

func TestReviewRulesDriftGuard(t *testing.T) {
	allRules, err := spec.FieldRules()
	if err != nil {
		t.Fatalf("spec.FieldRules failed: %v", err)
	}

	var expectedRules []writ.Rule
	for _, r := range allRules {
		if r.Vocabulary == "review-ops" {
			expectedRules = append(expectedRules, writ.Rule{
				OpType:    r.OpType,
				OpVersion: r.OpVersion,
				Field:     r.Field,
				Strategy:  r.Strategy,
				Key:       r.Key,
				Lattice:   r.Lattice,
			})
		}
	}

	builtIn := writ.ReviewRules()
	if !reflect.DeepEqual(builtIn, expectedRules) {
		t.Fatalf("ReviewRules() drifted from published review-ops field-rules.json:\n got:  %+v\n want: %+v", builtIn, expectedRules)
	}
}

func TestFoldReviewEmpty(t *testing.T) {
	state, err := writ.FoldReview(nil)
	if err != nil {
		t.Fatalf("FoldReview(nil) returned error: %v", err)
	}
	if !reflect.DeepEqual(state, writ.Review{}) {
		t.Fatalf("expected empty Review, got %+v", state)
	}
}

func TestFoldReviewDefaultSubject(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	// 1. Subject omitted, Author.Email present -> uses Author.Email
	op1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "approval",
			OpVersion:  1,
			Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","verdict":"approve","message":"looks good"}`),
		},
		ID: "op1",
		Author: codec.Identity{
			Name:  "Alice Name",
			Email: "alice@example.com",
			When:  now,
		},
	}

	// 2. Subject omitted, Author.Email empty -> fallback to Author.Name
	op2 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "approval",
			OpVersion:  1,
			Body:       json.RawMessage(`{"revision":"2222222222222222222222222222222222222222","verdict":"request-changes","message":"need tests"}`),
		},
		ID: "op2",
		Author: codec.Identity{
			Name: "Bob Name",
			When: now.Add(time.Minute),
		},
	}

	// 3. Explicit subject provided -> uses explicit subject over Author identity
	op3 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "approval",
			OpVersion:  1,
			Body:       json.RawMessage(`{"revision":"3333333333333333333333333333333333333333","verdict":"approve","subject":"carol-custom","message":"delegated approval"}`),
		},
		ID: "op3",
		Author: codec.Identity{
			Name:  "Alice Name",
			Email: "alice@example.com",
			When:  now.Add(2 * time.Minute),
		},
	}

	state, err := writ.FoldReview([]codec.Op{op1, op2, op3})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	if len(state.Approvals) != 3 {
		t.Fatalf("expected 3 approvals, got %d", len(state.Approvals))
	}

	// Sorted by (subject, revision):
	// "Bob Name" < "alice@example.com" < "carol-custom"
	if state.Approvals[0].Subject != "Bob Name" || state.Approvals[0].Revision != "2222222222222222222222222222222222222222" {
		t.Errorf("approval[0] mismatch: got %+v", state.Approvals[0])
	}
	if state.Approvals[1].Subject != "alice@example.com" || state.Approvals[1].Revision != "1111111111111111111111111111111111111111" {
		t.Errorf("approval[1] mismatch: got %+v", state.Approvals[1])
	}
	if state.Approvals[2].Subject != "carol-custom" || state.Approvals[2].Revision != "3333333333333333333333333333333333333333" {
		t.Errorf("approval[2] mismatch: got %+v", state.Approvals[2])
	}
}

func TestFoldReviewMergedToOpenTolerance(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	createOp := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-merge",
			ObjectType: "review",
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{"title":"Initial Review"}`),
		},
		ID: "c1",
		Author: codec.Identity{
			Email: "author@example.com",
			When:  now,
		},
	}

	mergedOp := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-merge",
			ObjectType: "review",
			OpType:     "set-status",
			OpVersion:  1,
			Body:       json.RawMessage(`{"status":"merged","merge_commit":"abcdef0123456789abcdef0123456789abcdef01"}`),
		},
		ID:      "m1",
		Parents: []string{"c1"},
		Author: codec.Identity{
			Email: "author@example.com",
			When:  now.Add(time.Minute),
		},
	}

	reopenOp := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-merge",
			ObjectType: "review",
			OpType:     "set-status",
			OpVersion:  1,
			Body:       json.RawMessage(`{"status":"open","reason":"accidental merge"}`),
		},
		ID:      "r1",
		Parents: []string{"m1"},
		Author: codec.Identity{
			Email: "author@example.com",
			When:  now.Add(2 * time.Minute),
		},
	}

	state, err := writ.FoldReview([]codec.Op{createOp, mergedOp, reopenOp})
	if err != nil {
		t.Fatalf("FoldReview failed to tolerate merged -> open transition: %v", err)
	}

	if state.Status != "open" {
		t.Errorf("expected status 'open', got %q", state.Status)
	}
	if state.Reason != "accidental merge" {
		t.Errorf("expected reason 'accidental merge', got %q", state.Reason)
	}
	if state.MergeCommit != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("expected merge_commit to be preserved, got %q", state.MergeCommit)
	}
}

func TestFoldReviewSetStatusOmitsMergeCommit(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	status1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-status",
			ObjectType: "review",
			OpType:     "set-status",
			OpVersion:  1,
			Body:       json.RawMessage(`{"status":"merged","merge_commit":"1111111111111111111111111111111111111111","reason":"Landed"}`),
		},
		ID: "s1",
		Author: codec.Identity{
			Email: "author@example.com",
			When:  now,
		},
	}

	status2 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-status",
			ObjectType: "review",
			OpType:     "set-status",
			OpVersion:  1,
			Body:       json.RawMessage(`{"status":"closed","reason":"Superseded"}`),
		},
		ID:      "s2",
		Parents: []string{"s1"},
		Author: codec.Identity{
			Email: "author@example.com",
			When:  now.Add(time.Minute),
		},
	}

	state, err := writ.FoldReview([]codec.Op{status1, status2})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	if state.Status != "closed" {
		t.Errorf("expected status 'closed', got %q", state.Status)
	}
	if state.Reason != "Superseded" {
		t.Errorf("expected reason 'Superseded', got %q", state.Reason)
	}
	if state.MergeCommit != "1111111111111111111111111111111111111111" {
		t.Errorf("expected merge_commit preserved, got %q", state.MergeCommit)
	}
}

func TestFoldReviewUnknownOpVersionAndType(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	v1Create := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-compat",
			ObjectType: "review",
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{"title":"V1 Title","description":"V1 Desc"}`),
		},
		ID: "op-v1",
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now,
		},
	}

	// Future op_version = 2
	v2Update := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-compat",
			ObjectType: "review",
			OpType:     "update",
			OpVersion:  2,
			Body:       json.RawMessage(`{"title":"V2 Should Be Ignored"}`),
		},
		ID:      "op-v2",
		Parents: []string{"op-v1"},
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now.Add(time.Minute),
		},
	}

	// Unknown op_type = "archive"
	unknownType := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-compat",
			ObjectType: "review",
			OpType:     "archive",
			OpVersion:  1,
			Body:       json.RawMessage(`{"title":"Archive Should Be Ignored"}`),
		},
		ID:      "op-unknown",
		Parents: []string{"op-v2"},
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now.Add(2 * time.Minute),
		},
	}

	state, err := writ.FoldReview([]codec.Op{v1Create, v2Update, unknownType})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	if state.Title != "V1 Title" {
		t.Errorf("expected title 'V1 Title', got %q", state.Title)
	}
	if state.Description != "V1 Desc" {
		t.Errorf("expected description 'V1 Desc', got %q", state.Description)
	}

	expectedUnknown := []writ.UnknownOp{
		{Commit: "op-v2", OpType: "update", OpVersion: 2},
		{Commit: "op-unknown", OpType: "archive", OpVersion: 1},
	}
	if !reflect.DeepEqual(state.UnknownOps, expectedUnknown) {
		t.Errorf("unknown_ops mismatch:\n got:  %+v\n want: %+v", state.UnknownOps, expectedUnknown)
	}
}
