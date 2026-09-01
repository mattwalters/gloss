package fold

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/internal/person"
)

// Accumulator defines the interface for state reducers in the closed strategy catalogue.
type Accumulator interface {
	// Apply updates the accumulator with an operation in total order L.
	Apply(op codec.Op, body map[string]any, rawBody map[string]json.RawMessage) error
	// HasValue returns true if at least one operation has contributed to this accumulator.
	HasValue() bool
	// Result returns the folded value for this field in serialized representation.
	Result() (any, error)
}

// StrategyFactory creates an Accumulator for a given rule.
type StrategyFactory func(rule Rule, reach ReachOracle) (Accumulator, error)

var strategyCatalogue = map[string]StrategyFactory{
	"lww":                 newLWWAccumulator,
	"create-once":         newCreateOnceAccumulator,
	"set-union":           newSetUnionAccumulator,
	"set-observed-remove": newSetObservedRemoveAccumulator,
	"append":              newAppendAccumulator,
	"tombstone":           newTombstoneAccumulator,
	"lattice":             newLatticeAccumulator,
	"keyed-lww":           newKeyedLWWAccumulator,
}

// NewAccumulator instantiates an Accumulator for the specified rule and reachability oracle.
func NewAccumulator(rule Rule, reach ReachOracle) (Accumulator, error) {
	factory, ok := strategyCatalogue[rule.Strategy]
	if !ok {
		return nil, fmt.Errorf("fold: unknown strategy %q", rule.Strategy)
	}
	return factory(rule, reach)
}

// 1. LWW (Last-Writer-Wins)
type lwwAccumulator struct {
	field  string
	hasVal bool
	val    any
}

func newLWWAccumulator(rule Rule, _ ReachOracle) (Accumulator, error) {
	return &lwwAccumulator{field: rule.Field}, nil
}

func (a *lwwAccumulator) Apply(op codec.Op, body map[string]any, _ map[string]json.RawMessage) error {
	if val, ok := body[a.field]; ok && val != nil {
		if s, ok := val.(string); ok && a.field == "resolved_by" && op.OpType == "resolve" {
			val = person.NormalizePerson(s)
		}
		a.val = val
		a.hasVal = true
	}
	return nil
}

func (a *lwwAccumulator) HasValue() bool { return a.hasVal }
func (a *lwwAccumulator) Result() (any, error) { return a.val, nil }

// 2. Create-Once
type createOnceAccumulator struct {
	field  string
	hasVal bool
	val    any
}

func newCreateOnceAccumulator(rule Rule, _ ReachOracle) (Accumulator, error) {
	return &createOnceAccumulator{field: rule.Field}, nil
}

func (a *createOnceAccumulator) Apply(_ codec.Op, body map[string]any, rawBody map[string]json.RawMessage) error {
	if !a.hasVal {
		if raw, ok := rawBody[a.field]; ok && len(raw) > 0 && string(raw) != "null" {
			a.val = raw
			a.hasVal = true
		} else if val, ok := body[a.field]; ok && val != nil {
			a.val = val
			a.hasVal = true
		}
	}
	return nil
}

func (a *createOnceAccumulator) HasValue() bool { return a.hasVal }
func (a *createOnceAccumulator) Result() (any, error) { return a.val, nil }

// 3. Set-Union
type setUnionAccumulator struct {
	field  string
	hasSet bool
	set    map[string]bool
}

func newSetUnionAccumulator(rule Rule, _ ReachOracle) (Accumulator, error) {
	return &setUnionAccumulator{
		field: rule.Field,
		set:   make(map[string]bool),
	}, nil
}

func (a *setUnionAccumulator) Apply(_ codec.Op, body map[string]any, _ map[string]json.RawMessage) error {
	raw, ok := body[a.field]
	if !ok || raw == nil {
		return nil
	}
	a.hasSet = true
	// Elements that are the empty string are dropped (spec/fold.md §5.3).
	add := func(item string) {
		if item != "" {
			a.set[item] = true
		}
	}
	switch val := raw.(type) {
	case []any:
		for _, item := range val {
			add(fmt.Sprint(item))
		}
	case []string:
		for _, item := range val {
			add(item)
		}
	case string:
		add(val)
	default:
		add(fmt.Sprint(val))
	}
	return nil
}

func (a *setUnionAccumulator) HasValue() bool { return a.hasSet }
func (a *setUnionAccumulator) Result() (any, error) {
	res := make([]string, 0, len(a.set))
	for k := range a.set {
		res = append(res, k)
	}
	sort.Strings(res)
	return res, nil
}

// 4. Set-Observed-Remove (Add-Wins OR-Set)
type orSetAddRecord struct {
	opID string
	item string
}

type orSetRemoveRecord struct {
	opID string
	item string
}

type setObservedRemoveAccumulator struct {
	field   string
	reach   ReachOracle
	hasOps  bool
	adds    []orSetAddRecord
	removes []orSetRemoveRecord
}

func newSetObservedRemoveAccumulator(rule Rule, reach ReachOracle) (Accumulator, error) {
	return &setObservedRemoveAccumulator{
		field: rule.Field,
		reach: reach,
	}, nil
}

func (a *setObservedRemoveAccumulator) Apply(op codec.Op, body map[string]any, _ map[string]json.RawMessage) error {
	var addRaw, remRaw any
	if bodyMap, ok := body[a.field].(map[string]any); ok {
		addRaw = bodyMap["add"]
		remRaw = bodyMap["remove"]
	} else {
		if raw, ok := body[a.field]; ok && raw != nil {
			if a.field == "add" {
				addRaw = raw
				remRaw = body["remove"]
			} else if a.field == "remove" {
				remRaw = raw
				addRaw = body["add"]
			}
		}
	}

	if addRaw == nil && remRaw == nil {
		return nil
	}
	a.hasOps = true

	// Person-valued fields normalize per spec/identifiers.md; every other item
	// is taken verbatim. Items that are empty after normalization are dropped
	// from both sides of the OR-set, whatever the op type (spec/fold.md §5.4).
	normalizeItem := func(it string) string {
		if op.OpType == "assign" {
			return person.NormalizePerson(it)
		}
		return it
	}

	if slice, ok := addRaw.([]any); ok {
		for _, it := range slice {
			if item := normalizeItem(fmt.Sprint(it)); item != "" {
				a.adds = append(a.adds, orSetAddRecord{opID: op.ID, item: item})
			}
		}
	} else if slice, ok := addRaw.([]string); ok {
		for _, it := range slice {
			if item := normalizeItem(it); item != "" {
				a.adds = append(a.adds, orSetAddRecord{opID: op.ID, item: item})
			}
		}
	}

	if slice, ok := remRaw.([]any); ok {
		for _, it := range slice {
			if item := normalizeItem(fmt.Sprint(it)); item != "" {
				a.removes = append(a.removes, orSetRemoveRecord{opID: op.ID, item: item})
			}
		}
	} else if slice, ok := remRaw.([]string); ok {
		for _, it := range slice {
			if item := normalizeItem(it); item != "" {
				a.removes = append(a.removes, orSetRemoveRecord{opID: op.ID, item: item})
			}
		}
	}
	return nil
}

func (a *setObservedRemoveAccumulator) HasValue() bool { return a.hasOps }
func (a *setObservedRemoveAccumulator) Result() (any, error) {
	presentSet := make(map[string]bool)
	for _, add := range a.adds {
		removed := false
		for _, rem := range a.removes {
			if rem.item == add.item && a.reach.IsAncestor(add.opID, rem.opID) {
				removed = true
				break
			}
		}
		if !removed {
			presentSet[add.item] = true
		}
	}
	res := make([]string, 0, len(presentSet))
	for k := range presentSet {
		res = append(res, k)
	}
	sort.Strings(res)
	return res, nil
}

// 5. Append
type appendAccumulator struct {
	field     string
	hasAppend bool
	list      []any
}

func newAppendAccumulator(rule Rule, _ ReachOracle) (Accumulator, error) {
	return &appendAccumulator{field: rule.Field}, nil
}

func (a *appendAccumulator) Apply(_ codec.Op, body map[string]any, _ map[string]json.RawMessage) error {
	raw, ok := body[a.field]
	if !ok || raw == nil {
		return nil
	}
	a.hasAppend = true
	if slice, ok := raw.([]any); ok {
		a.list = append(a.list, slice...)
	} else {
		a.list = append(a.list, raw)
	}
	return nil
}

func (a *appendAccumulator) HasValue() bool { return a.hasAppend }
func (a *appendAccumulator) Result() (any, error) { return a.list, nil }

// 6. Tombstone
type tombstoneAccumulator struct {
	field        string
	reach        ReachOracle
	hasTombstone bool
	deletes      []string
	undeletes    []string
}

func newTombstoneAccumulator(rule Rule, reach ReachOracle) (Accumulator, error) {
	return &tombstoneAccumulator{
		field: rule.Field,
		reach: reach,
	}, nil
}

func (a *tombstoneAccumulator) Apply(op codec.Op, body map[string]any, _ map[string]json.RawMessage) error {
	val, hasField := body[a.field]
	if op.OpType == "delete" || (hasField && val == true) {
		a.deletes = append(a.deletes, op.ID)
		a.hasTombstone = true
	} else if op.OpType == "undelete" || (hasField && val == false) {
		a.undeletes = append(a.undeletes, op.ID)
		a.hasTombstone = true
	}
	return nil
}

func (a *tombstoneAccumulator) HasValue() bool { return a.hasTombstone }
func (a *tombstoneAccumulator) Result() (any, error) {
	isDeleted := false
	for _, d := range a.deletes {
		cleared := false
		for _, u := range a.undeletes {
			if a.reach.IsAncestor(d, u) {
				cleared = true
				break
			}
		}
		if !cleared {
			isDeleted = true
			break
		}
	}
	return isDeleted, nil
}

// 7. Lattice
type latticeAccumulator struct {
	field       string
	rankMap     map[string]int
	currentRank int
	currentVal  string
	hasLattice  bool
}

func newLatticeAccumulator(rule Rule, _ ReachOracle) (Accumulator, error) {
	if len(rule.Lattice) == 0 {
		return nil, fmt.Errorf("fold: lattice strategy for field %q requires non-empty lattice elements", rule.Field)
	}
	rankMap := make(map[string]int, len(rule.Lattice))
	for i, elem := range rule.Lattice {
		rankMap[elem] = i
	}
	return &latticeAccumulator{
		field:       rule.Field,
		rankMap:     rankMap,
		currentRank: -1,
	}, nil
}

func (a *latticeAccumulator) Apply(_ codec.Op, body map[string]any, _ map[string]json.RawMessage) error {
	raw, ok := body[a.field]
	if !ok || raw == nil {
		return nil
	}
	valStr := fmt.Sprint(raw)
	if rk, ok := a.rankMap[valStr]; ok {
		if rk > a.currentRank {
			a.currentRank = rk
			a.currentVal = valStr
			a.hasLattice = true
		}
	}
	return nil
}

func (a *latticeAccumulator) HasValue() bool { return a.hasLattice }
func (a *latticeAccumulator) Result() (any, error) { return a.currentVal, nil }

// 8. Keyed-LWW
type keyedEntry struct {
	key   []string
	value any
}

type keyedLWWAccumulator struct {
	field    string
	keyCols  []string
	hasKeyed bool
	latest   map[string]*keyedEntry
}

func newKeyedLWWAccumulator(rule Rule, _ ReachOracle) (Accumulator, error) {
	if len(rule.Key) == 0 {
		return nil, fmt.Errorf("fold: keyed-lww strategy for field %q requires non-empty key", rule.Field)
	}
	return &keyedLWWAccumulator{
		field:   rule.Field,
		keyCols: rule.Key,
		latest:  make(map[string]*keyedEntry),
	}, nil
}

func (a *keyedLWWAccumulator) Apply(op codec.Op, body map[string]any, _ map[string]json.RawMessage) error {
	val, ok := body[a.field]
	if !ok {
		return nil
	}
	a.hasKeyed = true
	// The stored value is normalized on the same terms as the key component it
	// mirrors: a person identifier reads back normalized per spec/identifiers.md.
	if a.field == "subject" && op.OpType == "approval" {
		if s, isStr := val.(string); isStr {
			val = person.NormalizePerson(s)
		}
	}
	key := make([]string, 0, len(a.keyCols))
	for _, kf := range a.keyCols {
		if val, ok := body[kf]; ok && val != nil {
			vStr := fmt.Sprint(val)
			if kf == "subject" && op.OpType == "approval" {
				vStr = person.NormalizePerson(vStr)
			}
			key = append(key, vStr)
		} else {
			key = append(key, "")
		}
	}
	keyStr := fmt.Sprintf("%q", key)
	a.latest[keyStr] = &keyedEntry{key: key, value: val}
	return nil
}

func (a *keyedLWWAccumulator) HasValue() bool { return a.hasKeyed }
func (a *keyedLWWAccumulator) Result() (any, error) {
	entries := make([]*keyedEntry, 0, len(a.latest))
	for _, e := range a.latest {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		aKey, bKey := entries[i].key, entries[j].key
		for x := range aKey {
			if aKey[x] != bKey[x] {
				return aKey[x] < bKey[x]
			}
		}
		return false
	})
	keyed := make([]any, 0, len(entries))
	for _, e := range entries {
		keyed = append(keyed, map[string]any{
			"key":   e.key,
			"value": e.value,
		})
	}
	return keyed, nil
}
