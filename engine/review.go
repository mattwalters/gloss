package writ

import (
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/internal/fold"
)

// FoldReview executes deterministic fold reduction on an input set of operations
// for a code review collaborative object, returning the materialized Review state.
func FoldReview(ops []codec.Op) (Review, error) {
	state, err := fold.FoldReview(ops)
	if err != nil {
		return Review{}, err
	}

	var revisions []Revision
	if len(state.Revisions) > 0 {
		revisions = make([]Revision, len(state.Revisions))
		for i, r := range state.Revisions {
			revisions[i] = Revision{
				Base: r.Base,
				Head: r.Head,
			}
		}
	}

	var approvals []Approval
	if len(state.Approvals) > 0 {
		approvals = make([]Approval, len(state.Approvals))
		for i, a := range state.Approvals {
			approvals[i] = Approval{
				Subject:  a.Subject,
				Revision: a.Revision,
				Verdict:  a.Verdict,
				Message:  a.Message,
			}
		}
	}

	var ciStatuses []CIStatus
	if len(state.CIStatuses) > 0 {
		ciStatuses = make([]CIStatus, len(state.CIStatuses))
		for i, c := range state.CIStatuses {
			ciStatuses[i] = CIStatus{
				Revision:    c.Revision,
				Name:        c.Name,
				State:       c.State,
				URL:         c.URL,
				Description: c.Description,
				StartedAt:   c.StartedAt,
				CompletedAt: c.CompletedAt,
				ExternalID:  c.ExternalID,
			}
		}
	}

	var unknownOps []UnknownOp
	if len(state.UnknownOps) > 0 {
		unknownOps = make([]UnknownOp, len(state.UnknownOps))
		for i, u := range state.UnknownOps {
			unknownOps[i] = UnknownOp{
				Commit:    u.Commit,
				OpType:    u.OpType,
				OpVersion: u.OpVersion,
			}
		}
	}

	return Review{
		Title:       state.Title,
		Description: state.Description,
		Status:      state.Status,
		MergeCommit: state.MergeCommit,
		Reason:      state.Reason,
		Revisions:   revisions,
		Approvals:   approvals,
		CIStatuses:  ciStatuses,
		UnknownOps:  unknownOps,
	}, nil
}

// ReviewRules returns the built-in field merge rules for the review-ops vocabulary (v1).
func ReviewRules() []Rule {
	return []Rule{
		{OpType: "create", OpVersion: 1, Field: "title", Strategy: "lww"},
		{OpType: "create", OpVersion: 1, Field: "description", Strategy: "lww"},
		{OpType: "revision", OpVersion: 1, Field: "base", Strategy: "append"},
		{OpType: "revision", OpVersion: 1, Field: "head", Strategy: "append"},
		{OpType: "update", OpVersion: 1, Field: "title", Strategy: "lww"},
		{OpType: "update", OpVersion: 1, Field: "description", Strategy: "lww"},
		{OpType: "set-status", OpVersion: 1, Field: "status", Strategy: "lww"},
		{OpType: "set-status", OpVersion: 1, Field: "merge_commit", Strategy: "lww"},
		{OpType: "set-status", OpVersion: 1, Field: "reason", Strategy: "lww"},
		{OpType: "approval", OpVersion: 1, Field: "revision", Strategy: "keyed-lww", Key: []string{"subject", "revision"}},
		{OpType: "approval", OpVersion: 1, Field: "verdict", Strategy: "keyed-lww", Key: []string{"subject", "revision"}},
		{OpType: "approval", OpVersion: 1, Field: "subject", Strategy: "keyed-lww", Key: []string{"subject", "revision"}},
		{OpType: "approval", OpVersion: 1, Field: "message", Strategy: "keyed-lww", Key: []string{"subject", "revision"}},
		{OpType: "ci-status", OpVersion: 1, Field: "revision", Strategy: "keyed-lww", Key: []string{"revision", "name"}},
		{OpType: "ci-status", OpVersion: 1, Field: "name", Strategy: "keyed-lww", Key: []string{"revision", "name"}},
		{OpType: "ci-status", OpVersion: 1, Field: "state", Strategy: "keyed-lww", Key: []string{"revision", "name"}},
		{OpType: "ci-status", OpVersion: 1, Field: "url", Strategy: "keyed-lww", Key: []string{"revision", "name"}},
		{OpType: "ci-status", OpVersion: 1, Field: "description", Strategy: "keyed-lww", Key: []string{"revision", "name"}},
		{OpType: "ci-status", OpVersion: 1, Field: "started_at", Strategy: "keyed-lww", Key: []string{"revision", "name"}},
		{OpType: "ci-status", OpVersion: 1, Field: "completed_at", Strategy: "keyed-lww", Key: []string{"revision", "name"}},
		{OpType: "ci-status", OpVersion: 1, Field: "external_id", Strategy: "keyed-lww", Key: []string{"revision", "name"}},
	}
}
