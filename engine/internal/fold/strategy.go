package fold

import (
	"fmt"
	"sort"

	"github.com/writtendev/writ/engine/codec"
)

// Accumulator defines the interface for state reducers in the closed strategy catalogue.
type Accumulator interface {
	// Apply updates the accumulator with an operation in total order L.
	Apply(op codec.Op, body map[string]any) error
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

func (a *lwwAccumulator) Apply(_ codec.Op, body map[string]any) error {
	if val, ok := body[a.field]; ok && val != nil {
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

func (a *createOnceAccumulator) Apply(_ codec.Op, body map[string]any) error {
	if !a.hasVal {
		if val, ok := body[a.field]; ok && val != nil {
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

func (a *setUnionAccumulator) Apply(_ codec.Op, body map[string]any) error {
	raw, ok := body[a.field]
	if !ok || raw == nil {
		return nil
	}
	a.hasSet = true
	switch val := raw.(type) {
	case []any:
		for _, item := range val {
			a.set[fmt.Sprint(item)] = true
		}
	case []string:
		for _, item := range val {
			a.set[item] = true
		}
	case string:
		a.set[val] = true
	default:
		a.set[fmt.Sprint(val)] = true
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

func (a *setObservedRemoveAccumulator) Apply(op codec.Op, body map[string]any) error {
	raw, ok := body[a.field]
	if !ok || raw == nil {
		return nil
	}
	bodyMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	a.hasOps = true
	if addRaw, ok := bodyMap["add"].([]any); ok {
		for _, it := range addRaw {
			a.adds = append(a.adds, orSetAddRecord{opID: op.ID, item: fmt.Sprint(it)})
		}
	} else if addRaw, ok := bodyMap["add"].([]string); ok {
		for _, it := range addRaw {
			a.adds = append(a.adds, orSetAddRecord{opID: op.ID, item: it})
		}
	}
	if remRaw, ok := bodyMap["remove"].([]any); ok {
		for _, it := range remRaw {
			a.removes = append(a.removes, orSetRemoveRecord{opID: op.ID, item: fmt.Sprint(it)})
		}
	} else if remRaw, ok := bodyMap["remove"].([]string); ok {
		for _, it := range remRaw {
			a.removes = append(a.removes, orSetRemoveRecord{opID: op.ID, item: it})
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

func (a *appendAccumulator) Apply(_ codec.Op, body map[string]any) error {
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

func (a *tombstoneAccumulator) Apply(op codec.Op, body map[string]any) error {
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

func (a *latticeAccumulator) Apply(_ codec.Op, body map[string]any) error {
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

func (a *keyedLWWAccumulator) Apply(_ codec.Op, body map[string]any) error {
	val, ok := body[a.field]
	if !ok {
		return nil
	}
	a.hasKeyed = true
	key := make([]string, 0, len(a.keyCols))
	for _, kf := range a.keyCols {
		key = append(key, fmt.Sprint(body[kf]))
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
