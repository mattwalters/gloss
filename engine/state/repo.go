package state

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/internal/fold"
)

// RepoEntry represents a repository registry entry collaborative object (v1),
// produced by FoldRepo.
type RepoEntry struct {
	RepoID      string      `json:"repo_id"`
	Slug        string      `json:"slug"`
	Remotes     []string    `json:"remotes,omitempty"`
	IsWorkspace bool        `json:"is_workspace,omitempty"`
	UnknownOps  []UnknownOp `json:"unknown_ops,omitempty"`
}

// FoldRepo executes deterministic fold reduction on an input set of operations
// for a repository registry entry collaborative object, returning the materialized RepoEntry state.
func FoldRepo(ops []codec.Op) (RepoEntry, error) {
	if len(ops) == 0 {
		return RepoEntry{}, nil
	}

	orderedOps, err := fold.OrderWithTStar(ops)
	if err != nil {
		return RepoEntry{}, err
	}

	var state RepoEntry
	var unknownOps []UnknownOp
	remotesSet := make(map[string]bool)
	isWorkspaceSet := false

	rules := internalRules(RepoRules())

	for _, o := range orderedOps {
		op := o.Op
		if op.ObjectType != "repo" || op.OpVersion != 1 {
			unknownOps = append(unknownOps, UnknownOp{
				Commit:    op.ID,
				OpType:    op.OpType,
				OpVersion: op.OpVersion,
			})
			continue
		}

		if state.RepoID == "" {
			state.RepoID = op.ObjectID
		}

		var body map[string]any
		if len(op.Body) > 0 {
			if err := json.Unmarshal(op.Body, &body); err != nil {
				return RepoEntry{}, fmt.Errorf("fold repo: unmarshaling op %s body: %w", op.ID, err)
			}
		}
		if body == nil {
			body = make(map[string]any)
		}

		// A field with a declared rule carrying a value its strategy cannot
		// consume makes the whole op uninterpretable (spec/fold.md §7.1). It is
		// quarantined here on exactly the terms the generic driver applies, so
		// the typed reducer and fold.Fold reject the same operations.
		if fold.Uninterpretable(op, body, rules) {
			unknownOps = append(unknownOps, UnknownOp{
				Commit:    op.ID,
				OpType:    op.OpType,
				OpVersion: op.OpVersion,
			})
			continue
		}

		switch op.OpType {
		case "create":
			if s, ok := body["slug"].(string); ok {
				state.Slug = s
			}
			if isWs, ok := body["is_workspace"].(bool); ok {
				if !isWorkspaceSet {
					state.IsWorkspace = isWs
					isWorkspaceSet = true
				}
			}

		case "set-slug":
			if s, ok := body["slug"].(string); ok {
				state.Slug = s
			}

		case "add-remote":
			if r, ok := body["remote"].(string); ok && r != "" {
				remotesSet[r] = true
			}

		default:
			unknownOps = append(unknownOps, UnknownOp{
				Commit:    op.ID,
				OpType:    op.OpType,
				OpVersion: op.OpVersion,
			})
		}
	}

	if state.RepoID == "" && len(orderedOps) > 0 {
		state.RepoID = orderedOps[0].Op.ObjectID
	}

	if len(remotesSet) > 0 {
		var remotes []string
		for r := range remotesSet {
			remotes = append(remotes, r)
		}
		sort.Strings(remotes)
		state.Remotes = remotes
	}

	state.UnknownOps = unknownOps

	return state, nil
}

// RepoRules returns the built-in field merge rules for the repo vocabulary (v1).
func RepoRules() []Rule {
	return []Rule{
		{OpType: "create", OpVersion: 1, Field: "slug", Strategy: "lww"},
		{OpType: "create", OpVersion: 1, Field: "is_workspace", Strategy: "create-once"},
		{OpType: "set-slug", OpVersion: 1, Field: "slug", Strategy: "lww"},
		{OpType: "add-remote", OpVersion: 1, Field: "remote", Strategy: "set-union"},
	}
}
