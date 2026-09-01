package wire_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/projection"
	"github.com/writtendev/writ/engine/state"
	_ "github.com/writtendev/writ/spec/fixtures"
)

func TestWire_EmptyCollectionsSerializeAsArray(t *testing.T) {
	// 1. Review with empty collections
	r := writ.ReviewResult{
		ObjectID:  "test-review-id",
		Author:    projection.Author{Name: "Alice", Email: "alice@example.com"},
		CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Review: state.Review{
			Title:  "Empty Collections",
			Status: "open",
		},
	}

	wireReview := wire.FromReviewResult(r)
	env := wire.Envelope{
		SchemaVersion: wire.CurrentSchemaVersion,
		Kind:          wire.KindReviewStatus,
		Data:          wireReview,
	}

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(b)
	if !strings.Contains(jsonStr, `"revisions":[]`) {
		t.Errorf("expected revisions to serialize as [], got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"approvals":[]`) {
		t.Errorf("expected approvals to serialize as [], got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"ci_statuses":[]`) {
		t.Errorf("expected ci_statuses to serialize as [], got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"unknown_ops":[]`) {
		t.Errorf("expected unknown_ops to serialize as [], got: %s", jsonStr)
	}

	// 2. Empty ReviewSummaries slice
	emptySummaries := wire.FromReviewResultSummaries(nil)
	listEnv := wire.Envelope{
		SchemaVersion: wire.CurrentSchemaVersion,
		Kind:          wire.KindReviewList,
		Data:          emptySummaries,
	}

	bList, err := json.Marshal(listEnv)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonListStr := string(bList)
	if !strings.Contains(jsonListStr, `"data":[]`) {
		t.Errorf("expected data to serialize as [], got: %s", jsonListStr)
	}
}

func TestWire_FromReviewResult_FullMapping(t *testing.T) {
	now := time.Now().UTC()
	r := writ.ReviewResult{
		ObjectID:  "r-mixed",
		Author:    projection.Author{Name: "Alice", Email: "alice@example.com"},
		CreatedAt: now,
		UpdatedAt: now,
		Review: state.Review{
			Title:       "Full Review",
			Description: "Review description",
			Status:      "open",
			MergeCommit: "aabbcc",
			Reason:      "Approved",
			Revisions: []state.Revision{
				{Base: "base1", Head: "head1"},
			},
			Approvals: []state.Approval{
				{Subject: "bob", Revision: "head1", Verdict: "approve", Message: "LGTM"},
			},
			CIStatuses: []state.CIStatus{
				{Revision: "head1", Name: "ci/lint", State: "success", URL: "https://ci.example.com"},
			},
			UnknownOps: []state.UnknownOp{
				{Commit: "opcommit", OpType: "telemetry", OpVersion: 2},
			},
		},
	}

	wireRev := wire.FromReviewResult(r)
	if wireRev.ObjectID != "r-mixed" || wireRev.Title != "Full Review" {
		t.Errorf("unexpected field mapping: %+v", wireRev)
	}
	if len(wireRev.Revisions) != 1 || wireRev.Revisions[0].Head != "head1" {
		t.Errorf("revisions mapping mismatch: %+v", wireRev.Revisions)
	}
	if len(wireRev.Approvals) != 1 || wireRev.Approvals[0].Verdict != "approve" {
		t.Errorf("approvals mapping mismatch: %+v", wireRev.Approvals)
	}
	if len(wireRev.CIStatuses) != 1 || wireRev.CIStatuses[0].Name != "ci/lint" {
		t.Errorf("ci_statuses mapping mismatch: %+v", wireRev.CIStatuses)
	}
	if len(wireRev.UnknownOps) != 1 || wireRev.UnknownOps[0].OpType != "telemetry" {
		t.Errorf("unknown_ops mapping mismatch: %+v", wireRev.UnknownOps)
	}
}

func TestWire_SyncConverters(t *testing.T) {
	status := wire.FromSyncStatus(writ.SyncStatus{
		Remote:   "origin",
		Unsynced: 3,
	})
	if status.Remote != "origin" || status.Unsynced != 3 {
		t.Errorf("FromSyncStatus mismatch: %+v", status)
	}

	result := wire.FromSyncResult("origin", writ.SyncResult{
		OpsFetched:     2,
		OpsPushed:      1,
		ObjectsTouched: 1,
		Unsynced:       0,
	})
	if result.Remote != "origin" || result.OpsFetched != 2 || result.OpsPushed != 1 || result.ObjectsTouched != 1 || result.Unsynced != 0 {
		t.Errorf("FromSyncResult mismatch: %+v", result)
	}

	syncErr := &writ.SyncError{
		Remote:    "origin",
		Kind:      "auth",
		Message:   "permission denied",
		Advice:    "check ssh key",
		Retryable: false,
		Unsynced:  2,
	}

	statusFail := wire.FromSyncStatusFailure("origin", syncErr, 2)
	if statusFail.Remote != "origin" || statusFail.Unsynced != 2 || statusFail.Failure == nil {
		t.Fatalf("FromSyncStatusFailure mismatch: %+v", statusFail)
	}
	if statusFail.Failure.Kind != "auth" || statusFail.Failure.Message != "permission denied" || statusFail.Failure.Advice != "check ssh key" || statusFail.Failure.Retryable {
		t.Errorf("statusFail.Failure mismatch: %+v", statusFail.Failure)
	}

	resFail := wire.FromSyncResultFailure("origin", writ.SyncResult{OpsFetched: 0, OpsPushed: 0, Unsynced: 2}, syncErr)
	if resFail.Remote != "origin" || resFail.Unsynced != 2 || resFail.Failure == nil {
		t.Fatalf("FromSyncResultFailure mismatch: %+v", resFail)
	}
	if resFail.Failure.Kind != "auth" || resFail.Failure.Message != "permission denied" || resFail.Failure.Advice != "check ssh key" || resFail.Failure.Retryable {
		t.Errorf("resFail.Failure mismatch: %+v", resFail.Failure)
	}
}

func TestWire_CommentMapping(t *testing.T) {
	now := time.Now().UTC()
	resolvedBool := true
	c := writ.CommentResult{
		ObjectID:  "c-100",
		Author:    projection.Author{Name: "Alice", Email: "alice@example.com"},
		CreatedAt: now,
		UpdatedAt: now,
		Comment: state.Comment{
			Subject: state.CommentSubject{
				ObjectType: "review",
				ObjectID:   "r-200",
			},
			Text:     "Needs clarification",
			Resolved: &resolvedBool,
			Actor:    "alice",
		},
		Resolved: []projection.ResolvedPosition{
			{
				Side:      "new",
				Outcome:   "matched",
				Path:      "main.go",
				StartLine: 10,
				EndLine:   12,
			},
		},
	}

	wireComment := wire.FromCommentResult(c)
	if !wireComment.Resolved {
		t.Errorf("expected wireComment.Resolved == true")
	}
	if wireComment.ResolvedBy != "alice" {
		t.Errorf("expected wireComment.ResolvedBy == 'alice', got %q", wireComment.ResolvedBy)
	}
	if len(wireComment.Positions) != 1 || wireComment.Positions[0].Path != "main.go" {
		t.Errorf("unexpected positions: %+v", wireComment.Positions)
	}

	b, err := json.Marshal(wireComment)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(b)
	if !strings.Contains(jsonStr, `"resolved":true`) {
		t.Errorf("expected JSON to contain '\"resolved\":true', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"resolved_by":"alice"`) {
		t.Errorf("expected JSON to contain '\"resolved_by\":\"alice\"', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"positions":[`) {
		t.Errorf("expected JSON to contain '\"positions\":[', got: %s", jsonStr)
	}
}
