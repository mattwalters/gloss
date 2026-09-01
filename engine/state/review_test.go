package state_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	s "github.com/writtendev/writ/engine/state"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/spec"
)

func TestReviewRulesDriftGuard(t *testing.T) {
	allRules, err := spec.FieldRules()
	if err != nil {
		t.Fatalf("spec.FieldRules failed: %v", err)
	}

	var expectedRules []s.Rule
	for _, r := range allRules {
		if r.Vocabulary == "review-ops" {
			expectedRules = append(expectedRules, s.Rule{
				OpType:    r.OpType,
				OpVersion: r.OpVersion,
				Field:     r.Field,
				Strategy:  r.Strategy,
				Key:       r.Key,
				Lattice:   r.Lattice,
			})
		}
	}

	builtIn := s.ReviewRules()
	if !reflect.DeepEqual(builtIn, expectedRules) {
		t.Fatalf("ReviewRules() drifted from published review-ops field-rules.json:\n got:  %+v\n want: %+v", builtIn, expectedRules)
	}
}

func TestFoldReviewEmpty(t *testing.T) {
	state, err := s.FoldReview(nil)
	if err != nil {
		t.Fatalf("FoldReview(nil) returned error: %v", err)
	}
	if !reflect.DeepEqual(state, s.Review{}) {
		t.Fatalf("expected empty Review, got %+v", state)
	}
}

func TestFoldReviewApprovalsAndRetraction(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	op1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "approval",
			OpVersion:  1,
			Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","verdict":"approve","subject":"user:alice","message":"looks good"}`),
		},
		ID: "op1",
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now,
		},
	}

	op2 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "approval",
			OpVersion:  1,
			Body:       json.RawMessage(`{"revision":"2222222222222222222222222222222222222222","verdict":"request-changes","subject":"user:bob","message":"need tests"}`),
		},
		ID: "op2",
		Author: codec.Identity{
			Email: "bob@example.com",
			When:  now.Add(time.Minute),
		},
	}

	// Alice retracts her verdict on rev 1
	op3 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "approval",
			OpVersion:  1,
			Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","verdict":"none","subject":"user:alice","message":"retracted"}`),
		},
		ID:      "op3",
		Parents: []string{"op1"},
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now.Add(2 * time.Minute),
		},
	}

	state, err := s.FoldReview([]codec.Op{op1, op2, op3})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	if len(state.Approvals) != 1 {
		t.Fatalf("expected 1 active approval after retraction, got %d: %+v", len(state.Approvals), state.Approvals)
	}

	if state.Approvals[0].Subject != "user:bob" || state.Approvals[0].Revision != "2222222222222222222222222222222222222222" || state.Approvals[0].Verdict != "request-changes" {
		t.Errorf("approval mismatch: got %+v", state.Approvals[0])
	}
}

func TestFoldReviewRevisionsPairing(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	rev1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-rev",
			ObjectType: "review",
			OpType:     "revision",
			OpVersion:  1,
			Body:       json.RawMessage(`{"base":"0123456789abcdef0123456789abcdef01234567","head":"1111111111111111111111111111111111111111"}`),
		},
		ID:     "rev1",
		Author: codec.Identity{When: now},
	}

	rev2 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-rev",
			ObjectType: "review",
			OpType:     "revision",
			OpVersion:  1,
			Body:       json.RawMessage(`{"base":"0123456789abcdef0123456789abcdef01234567","head":"2222222222222222222222222222222222222222"}`),
		},
		ID:      "rev2",
		Parents: []string{"rev1"},
		Author:  codec.Identity{When: now.Add(time.Minute)},
	}

	state, err := s.FoldReview([]codec.Op{rev1, rev2})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	if len(state.Revisions) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(state.Revisions))
	}
	if state.Revisions[0].Base != "0123456789abcdef0123456789abcdef01234567" || state.Revisions[0].Head != "1111111111111111111111111111111111111111" {
		t.Errorf("rev[0] mismatch: %+v", state.Revisions[0])
	}
	if state.Revisions[1].Base != "0123456789abcdef0123456789abcdef01234567" || state.Revisions[1].Head != "2222222222222222222222222222222222222222" {
		t.Errorf("rev[1] mismatch: %+v", state.Revisions[1])
	}
}

func TestFoldReviewCIStatuses(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	ci1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-ci",
			ObjectType: "review",
			OpType:     "ci-status",
			OpVersion:  1,
			Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","name":"ci/test","state":"pending","url":"https://ci.example.com/1"}`),
		},
		ID:     "ci1",
		Author: codec.Identity{When: now},
	}

	ci2 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-ci",
			ObjectType: "review",
			OpType:     "ci-status",
			OpVersion:  1,
			Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","name":"ci/test","state":"success","url":"https://ci.example.com/1","description":"passed","started_at":"2026-01-01T00:00:00Z","completed_at":"2026-01-01T00:01:00Z","external_id":"ext-1"}`),
		},
		ID:      "ci2",
		Parents: []string{"ci1"},
		Author:  codec.Identity{When: now.Add(time.Minute)},
	}

	ciLint := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-ci",
			ObjectType: "review",
			OpType:     "ci-status",
			OpVersion:  1,
			Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","name":"ci/lint","state":"success"}`),
		},
		ID:     "ciLint",
		Author: codec.Identity{When: now},
	}

	state, err := s.FoldReview([]codec.Op{ci1, ci2, ciLint})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	if len(state.CIStatuses) != 2 {
		t.Fatalf("expected 2 ci statuses, got %d", len(state.CIStatuses))
	}
	// Deterministically sorted by (revision, name): ci/lint < ci/test
	if state.CIStatuses[0].Name != "ci/lint" || state.CIStatuses[0].State != "success" {
		t.Errorf("ci[0] mismatch: %+v", state.CIStatuses[0])
	}
	if state.CIStatuses[1].Name != "ci/test" || state.CIStatuses[1].State != "success" || state.CIStatuses[1].Description != "passed" || state.CIStatuses[1].ExternalID != "ext-1" {
		t.Errorf("ci[1] mismatch: %+v", state.CIStatuses[1])
	}
}

func TestFoldReviewMalformedBodyError(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	badOp := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-err",
			ObjectType: "review",
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{not valid json`),
		},
		ID:     "bad1",
		Author: codec.Identity{When: now},
	}

	_, errFold := s.Fold([]codec.Op{badOp}, s.ReviewRules())
	if errFold == nil {
		t.Fatal("expected Fold to error on malformed JSON body, got nil")
	}

	_, errReview := s.FoldReview([]codec.Op{badOp})
	if errReview == nil {
		t.Fatal("expected FoldReview to error on malformed JSON body, got nil")
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

	state, err := s.FoldReview([]codec.Op{createOp, mergedOp, reopenOp})
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

	state, err := s.FoldReview([]codec.Op{status1, status2})
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

	state, err := s.FoldReview([]codec.Op{v1Create, v2Update, unknownType})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	if state.Title != "V1 Title" {
		t.Errorf("expected title 'V1 Title', got %q", state.Title)
	}
	if state.Description != "V1 Desc" {
		t.Errorf("expected description 'V1 Desc', got %q", state.Description)
	}

	expectedUnknown := []s.UnknownOp{
		{Commit: "op-v2", OpType: "update", OpVersion: 2},
		{Commit: "op-unknown", OpType: "archive", OpVersion: 1},
	}
	if !reflect.DeepEqual(state.UnknownOps, expectedUnknown) {
		t.Errorf("unknown_ops mismatch:\n got:  %+v\n want: %+v", state.UnknownOps, expectedUnknown)
	}
}

func TestFoldReviewAgreement(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	ops := []codec.Op{
		{
			ID: "c1",
			Envelope: codec.Envelope{
				ObjectID:   "r-agree",
				ObjectType: "review",
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(`{"title":"Initial Title","description":"Initial Description"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now},
		},
		{
			ID:      "r1",
			Parents: []string{"c1"},
			Envelope: codec.Envelope{
				ObjectID:   "r-agree",
				ObjectType: "review",
				OpType:     "revision",
				OpVersion:  1,
				Body:       json.RawMessage(`{"base":"0123456789abcdef0123456789abcdef01234567","head":"1111111111111111111111111111111111111111"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(time.Minute)},
		},
		{
			ID:      "u1",
			Parents: []string{"r1"},
			Envelope: codec.Envelope{
				ObjectID:   "r-agree",
				ObjectType: "review",
				OpType:     "update",
				OpVersion:  1,
				Body:       json.RawMessage(`{"title":"Updated Title"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(2 * time.Minute)},
		},
		{
			ID:      "s1",
			Parents: []string{"u1"},
			Envelope: codec.Envelope{
				ObjectID:   "r-agree",
				ObjectType: "review",
				OpType:     "set-status",
				OpVersion:  1,
				Body:       json.RawMessage(`{"status":"open","reason":"Ready"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(3 * time.Minute)},
		},
		{
			ID:      "app1",
			Parents: []string{"s1"},
			Envelope: codec.Envelope{
				ObjectID:   "r-agree",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","verdict":"approve","subject":"user:bob","message":"LGTM"}`),
			},
			Author: codec.Identity{Email: "bob@example.com", When: now.Add(4 * time.Minute)},
		},
		{
			ID:      "app2",
			Parents: []string{"s1"},
			Envelope: codec.Envelope{
				ObjectID:   "r-agree",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","verdict":"approve","subject":"user:alice"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(4 * time.Minute)},
		},
		{
			ID:      "app2-retract",
			Parents: []string{"app2"},
			Envelope: codec.Envelope{
				ObjectID:   "r-agree",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","verdict":"none","subject":"user:alice"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(5 * time.Minute)},
		},
		{
			ID:      "ci1",
			Parents: []string{"s1"},
			Envelope: codec.Envelope{
				ObjectID:   "r-agree",
				ObjectType: "review",
				OpType:     "ci-status",
				OpVersion:  1,
				Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","name":"ci/lint","state":"success"}`),
			},
			Author: codec.Identity{Email: "ci@example.com", When: now.Add(4 * time.Minute)},
		},
	}

	reviewState, err := s.FoldReview(ops)
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	objectState, err := s.Fold(ops, s.ReviewRules())
	if err != nil {
		t.Fatalf("Fold failed: %v", err)
	}

	if reviewState.Title != objectState.State["title"] {
		t.Errorf("title mismatch: got %q, want %v", reviewState.Title, objectState.State["title"])
	}
	if reviewState.Description != objectState.State["description"] {
		t.Errorf("description mismatch: got %q, want %v", reviewState.Description, objectState.State["description"])
	}
	if reviewState.Status != objectState.State["status"] {
		t.Errorf("status mismatch: got %q, want %v", reviewState.Status, objectState.State["status"])
	}
	if reviewState.Reason != objectState.State["reason"] {
		t.Errorf("reason mismatch: got %q, want %v", reviewState.Reason, objectState.State["reason"])
	}

	baseList, ok := objectState.State["base"].([]any)
	if !ok || len(baseList) != len(reviewState.Revisions) {
		t.Fatalf("base list mismatch: got %v, want %d revisions", baseList, len(reviewState.Revisions))
	}
	for i, rev := range reviewState.Revisions {
		if rev.Base != baseList[i] {
			t.Errorf("revision[%d].Base mismatch: got %q, want %v", i, rev.Base, baseList[i])
		}
	}

	// Approvals: Bob active, Alice retracted
	if len(reviewState.Approvals) != 1 {
		t.Fatalf("expected 1 active approval in FoldReview, got %d", len(reviewState.Approvals))
	}
	if reviewState.Approvals[0].Subject != "user:bob" || reviewState.Approvals[0].Verdict != "approve" {
		t.Errorf("unexpected active approval: %+v", reviewState.Approvals[0])
	}

	// CI Statuses: ci/lint success
	if len(reviewState.CIStatuses) != 1 {
		t.Fatalf("expected 1 active ci status in FoldReview, got %d", len(reviewState.CIStatuses))
	}
	if reviewState.CIStatuses[0].Name != "ci/lint" || reviewState.CIStatuses[0].State != "success" {
		t.Errorf("unexpected active ci status: %+v", reviewState.CIStatuses[0])
	}
}

func TestFoldReviewAssign(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	// Initial create
	op1 := codec.Op{
		ID: "op1",
		Envelope: codec.Envelope{
			ObjectID:   "r-assign",
			ObjectType: "review",
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{"title":"Review with Assignees"}`),
		},
		Author: codec.Identity{Email: "alice@example.com", When: now},
	}

	// Assign alice and bob
	op2 := codec.Op{
		ID:      "op2",
		Parents: []string{"op1"},
		Envelope: codec.Envelope{
			ObjectID:   "r-assign",
			ObjectType: "review",
			OpType:     "assign",
			OpVersion:  1,
			Body:       json.RawMessage(`{"add":["user:alice","user:bob"]}`),
		},
		Author: codec.Identity{Email: "alice@example.com", When: now.Add(time.Minute)},
	}

	// Causal remove bob, add charlie
	op3 := codec.Op{
		ID:      "op3",
		Parents: []string{"op2"},
		Envelope: codec.Envelope{
			ObjectID:   "r-assign",
			ObjectType: "review",
			OpType:     "assign",
			OpVersion:  1,
			Body:       json.RawMessage(`{"remove":["user:bob"],"add":["user:charlie"]}`),
		},
		Author: codec.Identity{Email: "alice@example.com", When: now.Add(2 * time.Minute)},
	}

	// Concurrent op removing charlie from op1 (before charlie was added in op3)
	op4 := codec.Op{
		ID:      "op4",
		Parents: []string{"op1"},
		Envelope: codec.Envelope{
			ObjectID:   "r-assign",
			ObjectType: "review",
			OpType:     "assign",
			OpVersion:  1,
			Body:       json.RawMessage(`{"remove":["user:charlie"]}`),
		},
		Author: codec.Identity{Email: "bob@example.com", When: now.Add(3 * time.Minute)},
	}

	state, err := s.FoldReview([]codec.Op{op1, op2, op3, op4})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	// Alice and Charlie should be present (charlie add in op3 wins over concurrent remove in op4; bob removed causally)
	wantAssignees := []string{"user:alice", "user:charlie"}
	if !reflect.DeepEqual(state.Assignees, wantAssignees) {
		t.Fatalf("assignees mismatch: got %v, want %v", state.Assignees, wantAssignees)
	}
}

func TestFoldReviewLabelsORSetCausalAndConcurrent(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	op1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "label",
			OpVersion:  1,
			Body:       json.RawMessage(`{"add":["area/engine","priority/high"]}`),
		},
		ID: "op1",
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now,
		},
	}

	op2 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "label",
			OpVersion:  1,
			Body:       json.RawMessage(`{"remove":["priority/high"]}`),
		},
		ID:      "op2",
		Parents: []string{"op1"},
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now.Add(time.Minute),
		},
	}

	state, err := s.FoldReview([]codec.Op{op1, op2})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	expectedLabels := []string{"area/engine"}
	if !reflect.DeepEqual(state.Labels, expectedLabels) {
		t.Errorf("labels mismatch: got %v, want %v", state.Labels, expectedLabels)
	}
}

func TestFoldReviewLinksAndRetraction(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	link1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "link",
			OpVersion:  1,
			Body:       json.RawMessage(`{"target":"0123456789abcdef0123456789abcdef","target_type":"issue","relation":"fixes"}`),
		},
		ID: "l1",
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now,
		},
	}

	link2 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "link",
			OpVersion:  1,
			Body:       json.RawMessage(`{"target":"fedcba9876543210fedcba9876543210","target_type":"issue","relation":"relates"}`),
		},
		ID:      "l2",
		Parents: []string{"l1"},
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now.Add(time.Minute),
		},
	}

	// Retract link1
	retract1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "r-test",
			ObjectType: "review",
			OpType:     "link",
			OpVersion:  1,
			Body:       json.RawMessage(`{"target":"0123456789abcdef0123456789abcdef","relation":"none"}`),
		},
		ID:      "r1",
		Parents: []string{"l2"},
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now.Add(2 * time.Minute),
		},
	}

	state, err := s.FoldReview([]codec.Op{link1, link2, retract1})
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	if len(state.Links) != 1 {
		t.Fatalf("expected 1 active link after retraction, got %d", len(state.Links))
	}

	expectedLink := s.Link{
		Target:     "fedcba9876543210fedcba9876543210",
		TargetType: "issue",
		Relation:   "relates",
	}
	if state.Links[0] != expectedLink {
		t.Errorf("link mismatch: got %+v, want %+v", state.Links[0], expectedLink)
	}
}

func TestFoldReviewPersonNormalization(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	// 1. Assign with whitespace and mixed case in both halves:
	//    "  Email:Alice@Example.COM  ", "EMAIL:Bob@Example.Com"
	// 2. Remove normalized: "email:alice@example.com", add " email:Charlie@Example.COM "
	// 3. Approval with whitespace and mixed case: "  Email:Carol@Example.COM  ", verdict "approve"
	// 4. Approval update normalized: "email:carol@example.com", verdict "request-changes"
	ops := []codec.Op{
		{
			ID: "op1",
			Envelope: codec.Envelope{
				ObjectID:   "r-norm",
				ObjectType: "review",
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(`{"title":"Normalization Review"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now},
		},
		{
			ID:      "op2",
			Parents: []string{"op1"},
			Envelope: codec.Envelope{
				ObjectID:   "r-norm",
				ObjectType: "review",
				OpType:     "assign",
				OpVersion:  1,
				Body:       json.RawMessage(`{"add":["  Email:Alice@Example.COM  ", "EMAIL:Bob@Example.Com"]}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(time.Minute)},
		},
		{
			ID:      "op3",
			Parents: []string{"op2"},
			Envelope: codec.Envelope{
				ObjectID:   "r-norm",
				ObjectType: "review",
				OpType:     "assign",
				OpVersion:  1,
				Body:       json.RawMessage(`{"remove":["email:alice@example.com"],"add":[" email:Charlie@Example.COM "]}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(2 * time.Minute)},
		},
		{
			ID:      "op4",
			Parents: []string{"op3"},
			Envelope: codec.Envelope{
				ObjectID:   "r-norm",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","verdict":"approve","subject":"  Email:Carol@Example.COM  "}`),
			},
			Author: codec.Identity{Email: "carol@example.com", When: now.Add(3 * time.Minute)},
		},
		{
			ID:      "op5",
			Parents: []string{"op4"},
			Envelope: codec.Envelope{
				ObjectID:   "r-norm",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","verdict":"request-changes","subject":"email:carol@example.com"}`),
			},
			Author: codec.Identity{Email: "carol@example.com", When: now.Add(4 * time.Minute)},
		},
	}

	state, err := s.FoldReview(ops)
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	// Assignees: alice removed, bob and charlie present (lowercase, sorted)
	wantAssignees := []string{"email:bob@example.com", "email:charlie@example.com"}
	if !reflect.DeepEqual(state.Assignees, wantAssignees) {
		t.Errorf("Assignees = %v, want %v", state.Assignees, wantAssignees)
	}

	// Approvals: email:carol@example.com single entry with updated verdict
	if len(state.Approvals) != 1 {
		t.Fatalf("Approvals count = %d, want 1", len(state.Approvals))
	}
	if state.Approvals[0].Subject != "email:carol@example.com" {
		t.Errorf("Approval Subject = %q, want %q", state.Approvals[0].Subject, "email:carol@example.com")
	}
	if state.Approvals[0].Verdict != "request-changes" {
		t.Errorf("Approval Verdict = %q, want %q", state.Approvals[0].Verdict, "request-changes")
	}
}

