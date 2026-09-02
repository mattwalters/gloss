package state

import (
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/internal/fold"
)

// Rule specifies the merge strategy and parameters for an (op_type, op_version, field) tuple.
type Rule struct {
	OpType    string   `json:"op_type,omitempty"`
	OpVersion int64    `json:"op_version,omitempty"`
	Field     string   `json:"field"`
	Strategy  string   `json:"strategy"`
	Key       []string `json:"key,omitempty"`
	Lattice   []string `json:"lattice,omitempty"`
}

// Sentinels re-exported from internal/fold.
var (
	// ErrCycle is returned when the operation graph contains a directed cycle.
	ErrCycle = fold.ErrCycle

	// ErrDuplicateOpID is returned when the input set contains duplicate operation IDs.
	ErrDuplicateOpID = fold.ErrDuplicateOpID

	// ErrMixedObjects is returned when the input set spans multiple object IDs.
	ErrMixedObjects = fold.ErrMixedObjects
)

// internalRules converts the public rule table into the internal one. The
// typed reducers below share it so that they and the generic driver decide
// which operations are uninterpretable from the same rules.
func internalRules(rules []Rule) []fold.Rule {
	out := make([]fold.Rule, len(rules))
	for i, r := range rules {
		out[i] = fold.Rule{
			OpType:    r.OpType,
			OpVersion: r.OpVersion,
			Field:     r.Field,
			Strategy:  r.Strategy,
			Key:       r.Key,
			Lattice:   r.Lattice,
		}
	}
	return out
}

// Fold executes deterministic fold reduction on an input set of operations
// against declared field merge rules, returning the resulting ObjectState.
func Fold(ops []codec.Op, rules []Rule) (ObjectState, error) {
	res, err := fold.Fold(ops, internalRules(rules))
	if err != nil {
		return ObjectState{}, err
	}

	totalOrder := make([]OpRef, len(res.TotalOrder))
	for i, ref := range res.TotalOrder {
		totalOrder[i] = OpRef{
			Commit: ref.Commit,
			TStar:  ref.TStar,
		}
	}

	unknownOps := make([]UnknownOp, len(res.UnknownOps))
	for i, u := range res.UnknownOps {
		unknownOps[i] = UnknownOp{
			Commit:    u.Commit,
			OpType:    u.OpType,
			OpVersion: u.OpVersion,
		}
	}

	return ObjectState{
		ObjectID:   res.ObjectID,
		ObjectType: res.ObjectType,
		TotalOrder: totalOrder,
		State:      res.State,
		UnknownOps: unknownOps,
	}, nil
}

// orSetItems returns the items one side of an OR-set body carries. The side
// holds a string or an array of strings (spec/fold.md §5.4); anything else made
// the operation uninterpretable (§7.1) before it reached a reducer, so a
// non-string here is unreachable and skipped rather than rendered.
//
// The typed reducers share this with the generic fold's own orSetItems so a
// body shape one consumes cannot be silently dropped by the other.
func orSetItems(raw any) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case []any:
		items := make([]string, 0, len(v))
		for _, it := range v {
			if s, ok := it.(string); ok {
				items = append(items, s)
			}
		}
		return items
	case []string:
		return v
	}
	return nil
}
