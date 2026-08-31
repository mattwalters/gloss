package state

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/internal/fold"
)

// FoldReview executes deterministic fold reduction on an input set of operations
// for a code review collaborative object, returning the materialized Review state.
func FoldReview(ops []codec.Op) (Review, error) {
	if len(ops) == 0 {
		return Review{}, nil
	}

	orderedOps, err := fold.OrderWithTStar(ops)
	if err != nil {
		return Review{}, err
	}

	var state Review
	var revisions []Revision
	var unknownOps []UnknownOp

	type approvalKey struct {
		subject  string
		revision string
	}
	type ciStatusKey struct {
		revision string
		name     string
	}

	approvalsMap := make(map[approvalKey]*Approval)
	ciStatusesMap := make(map[ciStatusKey]*CIStatus)

	for _, o := range orderedOps {
		op := o.Op
		if op.ObjectType != "review" || op.OpVersion != 1 {
			unknownOps = append(unknownOps, UnknownOp{
				Commit:    op.ID,
				OpType:    op.OpType,
				OpVersion: op.OpVersion,
			})
			continue
		}

		var body map[string]any
		if len(op.Body) > 0 {
			if err := json.Unmarshal(op.Body, &body); err != nil {
				return Review{}, fmt.Errorf("fold review: unmarshaling op %s body: %w", op.ID, err)
			}
		}
		if body == nil {
			body = make(map[string]any)
		}

		switch op.OpType {
		case "create", "update":
			if t, ok := body["title"].(string); ok {
				state.Title = t
			}
			if d, ok := body["description"].(string); ok {
				state.Description = d
			}

		case "set-status":
			if s, ok := body["status"].(string); ok {
				state.Status = s
			}
			if mc, ok := body["merge_commit"].(string); ok {
				state.MergeCommit = mc
			}
			if r, ok := body["reason"].(string); ok {
				state.Reason = r
			}

		case "revision":
			base, _ := body["base"].(string)
			head, _ := body["head"].(string)
			revisions = append(revisions, Revision{
				Base: base,
				Head: head,
			})

		case "approval":
			rev, _ := body["revision"].(string)
			subject, _ := body["subject"].(string)

			key := approvalKey{subject: subject, revision: rev}
			entry, ok := approvalsMap[key]
			if !ok {
				entry = &Approval{
					Subject:  subject,
					Revision: rev,
				}
				approvalsMap[key] = entry
			}

			if v, ok := body["verdict"].(string); ok {
				entry.Verdict = v
			}
			if m, ok := body["message"].(string); ok {
				entry.Message = m
			}

		case "ci-status":
			rev, _ := body["revision"].(string)
			name, _ := body["name"].(string)

			key := ciStatusKey{revision: rev, name: name}
			entry, ok := ciStatusesMap[key]
			if !ok {
				entry = &CIStatus{
					Revision: rev,
					Name:     name,
				}
				ciStatusesMap[key] = entry
			}

			if s, ok := body["state"].(string); ok {
				entry.State = s
			}
			if u, ok := body["url"].(string); ok {
				entry.URL = u
			}
			if d, ok := body["description"].(string); ok {
				entry.Description = d
			}
			if sa, ok := body["started_at"].(string); ok {
				entry.StartedAt = sa
			}
			if ca, ok := body["completed_at"].(string); ok {
				entry.CompletedAt = ca
			}
			if eid, ok := body["external_id"].(string); ok {
				entry.ExternalID = eid
			}

		default:
			unknownOps = append(unknownOps, UnknownOp{
				Commit:    op.ID,
				OpType:    op.OpType,
				OpVersion: op.OpVersion,
			})
		}
	}

	state.Revisions = revisions
	state.UnknownOps = unknownOps

	// Approvals: omit entries whose folded verdict is "none" or empty.
	// Sort deterministically by (subject, revision).
	var approvals []Approval
	for _, app := range approvalsMap {
		if app.Verdict != "none" && app.Verdict != "" {
			approvals = append(approvals, *app)
		}
	}
	sort.Slice(approvals, func(i, j int) bool {
		if approvals[i].Subject != approvals[j].Subject {
			return approvals[i].Subject < approvals[j].Subject
		}
		return approvals[i].Revision < approvals[j].Revision
	})
	state.Approvals = approvals

	// CIStatuses: sort deterministically by (revision, name).
	var ciStatuses []CIStatus
	for _, ci := range ciStatusesMap {
		ciStatuses = append(ciStatuses, *ci)
	}
	sort.Slice(ciStatuses, func(i, j int) bool {
		if ciStatuses[i].Revision != ciStatuses[j].Revision {
			return ciStatuses[i].Revision < ciStatuses[j].Revision
		}
		return ciStatuses[i].Name < ciStatuses[j].Name
	})
	state.CIStatuses = ciStatuses

	return state, nil
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
