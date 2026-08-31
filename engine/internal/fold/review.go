package fold

import (
	"encoding/json"
	"sort"

	"github.com/writtendev/writ/engine/codec"
)

// Revision represents a code revision push (base and head commits) on a review.
type Revision struct {
	Base string `json:"base"`
	Head string `json:"head"`
}

// Approval represents a review verdict vote for a specific revision head.
type Approval struct {
	Subject  string `json:"subject"`
	Revision string `json:"revision"`
	Verdict  string `json:"verdict"`
	Message  string `json:"message,omitempty"`
}

// CIStatus represents an automated check result attached to a revision head.
type CIStatus struct {
	Revision    string `json:"revision"`
	Name        string `json:"name"`
	State       string `json:"state"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
}

// ReviewState is the folded state of a code review collaborative object (v1).
type ReviewState struct {
	Title       string     `json:"title,omitempty"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status,omitempty"`
	MergeCommit string     `json:"merge_commit,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	Revisions   []Revision `json:"revisions,omitempty"`
	Approvals   []Approval `json:"approvals,omitempty"`
	CIStatuses  []CIStatus `json:"ci_statuses,omitempty"`
}

type approvalKey struct {
	subject  string
	revision string
}

type ciStatusKey struct {
	revision string
	name     string
}

// FoldReview executes deterministic fold reduction on an input set of operations
// for a code review collaborative object, returning the resulting ReviewState.
func FoldReview(ops []codec.Op) (ReviewState, error) {
	if len(ops) == 0 {
		return ReviewState{}, nil
	}

	orderedOps, err := OrderWithTStar(ops)
	if err != nil {
		return ReviewState{}, err
	}

	var state ReviewState
	var revisions []Revision

	approvalsMap := make(map[approvalKey]*Approval)
	ciStatusesMap := make(map[ciStatusKey]*CIStatus)

	for _, o := range orderedOps {
		op := o.Op
		if op.OpVersion != 1 {
			continue
		}

		var body map[string]any
		if len(op.Body) > 0 {
			_ = json.Unmarshal(op.Body, &body)
		}
		if body == nil {
			continue
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
			if subject == "" {
				if op.Author.Email != "" {
					subject = op.Author.Email
				} else {
					subject = op.Author.Name
				}
			}

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
		}
	}

	state.Revisions = revisions

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
