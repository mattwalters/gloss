package spec_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/spec"
)

// computeEffectiveTimes calculates t*(u) = max(u.time, max_{p in Parents_S(u)} t*(p))
// for all ops in the restricted input set.
func computeEffectiveTimes(ops []spec.OrderOp, inSet map[string]bool) map[string]int64 {
	tStar := make(map[string]int64, len(inSet))
	opMap := make(map[string]spec.OrderOp, len(ops))
	for _, op := range ops {
		opMap[op.ID] = op
	}

	var getTStar func(id string) int64
	getTStar = func(id string) int64 {
		if t, ok := tStar[id]; ok {
			return t
		}
		op := opMap[id]
		res := op.Time
		for _, p := range op.Parents {
			if inSet[p] {
				pt := getTStar(p)
				if pt > res {
					res = pt
				}
			}
		}
		tStar[id] = res
		return res
	}

	for id := range inSet {
		getTStar(id)
	}
	return tStar
}

// computeTotalOrder produces the deterministic total order sequence L of ops for target ObjectID.
func computeTotalOrder(ops []spec.OrderOp, objectID string) ([]string, error) {
	inSet := make(map[string]bool)
	for _, op := range ops {
		if op.ObjectID == objectID {
			inSet[op.ID] = true
		}
	}
	if len(inSet) == 0 {
		return nil, nil
	}

	tStar := computeEffectiveTimes(ops, inSet)

	inDegree := make(map[string]int, len(inSet))
	children := make(map[string][]string, len(inSet))
	for _, op := range ops {
		if !inSet[op.ID] {
			continue
		}
		var parentsInSet int
		for _, p := range op.Parents {
			if inSet[p] {
				parentsInSet++
				children[p] = append(children[p], op.ID)
			}
		}
		inDegree[op.ID] = parentsInSet
	}

	// Ready queue
	var ready []string
	for id, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}

	var order []string
	for len(ready) > 0 {
		// Pick ready op with minimal (t*, id)
		bestIdx := 0
		bestID := ready[0]
		bestT := tStar[bestID]

		for i := 1; i < len(ready); i++ {
			candID := ready[i]
			candT := tStar[candID]
			if candT < bestT || (candT == bestT && candID < bestID) {
				bestIdx = i
				bestID = candID
				bestT = candT
			}
		}

		// Remove chosen from ready
		ready = append(ready, ready[bestIdx]) // temporary swap for remove
		ready = append(ready[:bestIdx], ready[bestIdx+1:len(ready)-1]...)

		order = append(order, bestID)

		// Unblock children
		for _, ch := range children[bestID] {
			inDegree[ch]--
			if inDegree[ch] == 0 {
				ready = append(ready, ch)
			}
		}
	}

	if len(order) != len(inSet) {
		return nil, fmt.Errorf("cycle detected in restricted DAG: emitted %d of %d ops", len(order), len(inSet))
	}

	return order, nil
}

func TestOrderVectorsLoad(t *testing.T) {
	vectors, err := spec.OrderVectors()
	if err != nil {
		t.Fatalf("loading order vectors: %v", err)
	}
	if len(vectors) == 0 {
		t.Fatal("no order vectors loaded")
	}
}

func TestOrderingVectors(t *testing.T) {
	vectors, err := spec.OrderVectors()
	if err != nil {
		t.Fatalf("loading order vectors: %v", err)
	}

	for _, vec := range vectors {
		t.Run(vec.Name, func(t *testing.T) {
			gotOrder, err := computeTotalOrder(vec.Ops, vec.ObjectID)
			if err != nil {
				t.Fatalf("computeTotalOrder failed: %v", err)
			}

			if !reflect.DeepEqual(gotOrder, vec.ExpectedOrder) {
				t.Errorf("total order mismatch:\n got: %v\nwant: %v", gotOrder, vec.ExpectedOrder)
			}

			// Independent check: Assert that vec.ExpectedOrder is a valid topological sort of the restricted DAG
			inSet := make(map[string]bool)
			for _, op := range vec.Ops {
				if op.ObjectID == vec.ObjectID {
					inSet[op.ID] = true
				}
			}
			pos := make(map[string]int, len(vec.ExpectedOrder))
			for i, id := range vec.ExpectedOrder {
				pos[id] = i
			}
			for _, op := range vec.Ops {
				if !inSet[op.ID] {
					continue
				}
				for _, p := range op.Parents {
					if inSet[p] {
						if pos[p] >= pos[op.ID] {
							t.Errorf("topological violation: parent %s (pos %d) appears at or after child %s (pos %d)",
								p, pos[p], op.ID, pos[op.ID])
						}
					}
				}
			}

			// Permutation invariance: shuffle input ops 100 times and verify output is identical
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			for i := 0; i < 100; i++ {
				shuffled := make([]spec.OrderOp, len(vec.Ops))
				copy(shuffled, vec.Ops)
				r.Shuffle(len(shuffled), func(i, j int) {
					shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
				})
				shuffledOrder, err := computeTotalOrder(shuffled, vec.ObjectID)
				if err != nil {
					t.Fatalf("computeTotalOrder on shuffled input failed: %v", err)
				}
				if !reflect.DeepEqual(shuffledOrder, vec.ExpectedOrder) {
					t.Fatalf("order changed across permutation #%d:\n got: %v\nwant: %v", i, shuffledOrder, vec.ExpectedOrder)
				}
			}
		})
	}
}

// buildReachabilityMap computes transitive reachability (isAncestor) in the restricted DAG.
func buildReachabilityMap(ops []spec.MergeOp, inSet map[string]bool) map[string]map[string]bool {
	parentsMap := make(map[string][]string, len(inSet))
	for _, op := range ops {
		if !inSet[op.ID] {
			continue
		}
		var pList []string
		for _, p := range op.Parents {
			if inSet[p] {
				pList = append(pList, p)
			}
		}
		parentsMap[op.ID] = pList
	}

	ancestors := make(map[string]map[string]bool, len(inSet))
	for id := range inSet {
		ancestors[id] = make(map[string]bool)
	}

	var dfs func(curr, target string)
	dfs = func(curr, target string) {
		for _, p := range parentsMap[curr] {
			if !ancestors[target][p] {
				ancestors[target][p] = true
				dfs(p, target)
			}
		}
	}

	for id := range inSet {
		dfs(id, id)
	}

	return ancestors
}

// foldMerge executes the reference merge reducer on a MergeVector.
func foldMerge(vec spec.MergeVector) (map[string]any, error) {
	var orderOps []spec.OrderOp
	inSet := make(map[string]bool)
	opMap := make(map[string]spec.MergeOp, len(vec.Ops))
	for _, op := range vec.Ops {
		opMap[op.ID] = op
		if op.ObjectID == vec.ObjectID {
			inSet[op.ID] = true
		}
		orderOps = append(orderOps, spec.OrderOp{
			ID:       op.ID,
			Parents:  op.Parents,
			Time:     op.Time,
			ObjectID: op.ObjectID,
		})
	}

	totalOrder, err := computeTotalOrder(orderOps, vec.ObjectID)
	if err != nil {
		return nil, fmt.Errorf("ordering ops: %w", err)
	}

	ancestors := buildReachabilityMap(vec.Ops, inSet)
	// isAncestor(a, b) returns true if a is an ancestor of b (a happened-before b)
	isAncestor := func(a, b string) bool {
		return ancestors[b][a]
	}

	state := make(map[string]any)

	for fieldName, cfg := range vec.Fields {
		switch cfg.Strategy {
		case "lww":
			for _, id := range totalOrder {
				op := opMap[id]
				if val, ok := op.Body[fieldName]; ok {
					state[fieldName] = val
				}
			}

		case "create-once":
			for _, id := range totalOrder {
				op := opMap[id]
				if _, alreadySet := state[fieldName]; !alreadySet {
					if val, ok := op.Body[fieldName]; ok && val != nil {
						state[fieldName] = val
					}
				}
			}

		case "set-union":
			unionSet := make(map[string]bool)
			for _, id := range totalOrder {
				op := opMap[id]
				if raw, ok := op.Body[fieldName]; ok {
					switch val := raw.(type) {
					case []any:
						for _, item := range val {
							unionSet[fmt.Sprint(item)] = true
						}
					case []string:
						for _, item := range val {
							unionSet[item] = true
						}
					case string:
						unionSet[val] = true
					}
				}
			}
			var result []string
			for k := range unionSet {
				result = append(result, k)
			}
			sort.Strings(result)
			state[fieldName] = result

		case "set-observed-remove":
			type addRecord struct {
				opID string
				item string
			}
			var adds []addRecord
			type removeRecord struct {
				opID string
				item string
			}
			var removes []removeRecord

			for _, op := range vec.Ops {
				if !inSet[op.ID] {
					continue
				}
				if raw, ok := op.Body[fieldName]; ok {
					if bodyMap, ok := raw.(map[string]any); ok {
						if addRaw, ok := bodyMap["add"].([]any); ok {
							for _, it := range addRaw {
								adds = append(adds, addRecord{opID: op.ID, item: fmt.Sprint(it)})
							}
						}
						if remRaw, ok := bodyMap["remove"].([]any); ok {
							for _, it := range remRaw {
								removes = append(removes, removeRecord{opID: op.ID, item: fmt.Sprint(it)})
							}
						}
					}
				}
			}

			presentSet := make(map[string]bool)
			for _, add := range adds {
				// add is active if no remove of the same item causally succeeds add.opID
				removed := false
				for _, rem := range removes {
					if rem.item == add.item && isAncestor(add.opID, rem.opID) {
						removed = true
						break
					}
				}
				if !removed {
					presentSet[add.item] = true
				}
			}
			var result []string
			for k := range presentSet {
				result = append(result, k)
			}
			sort.Strings(result)
			state[fieldName] = result

		case "append":
			var list []any
			for _, id := range totalOrder {
				op := opMap[id]
				if raw, ok := op.Body[fieldName]; ok {
					if slice, ok := raw.([]any); ok {
						list = append(list, slice...)
					} else {
						list = append(list, raw)
					}
				}
			}
			state[fieldName] = list

		case "tombstone":
			var deletes []string
			var undeletes []string
			for _, op := range vec.Ops {
				if !inSet[op.ID] {
					continue
				}
				if op.OpType == "delete" || op.Body[fieldName] == true {
					deletes = append(deletes, op.ID)
				} else if op.OpType == "undelete" || op.Body[fieldName] == false {
					undeletes = append(undeletes, op.ID)
				}
			}

			isDeleted := false
			for _, d := range deletes {
				cleared := false
				for _, u := range undeletes {
					if isAncestor(d, u) {
						cleared = true
						break
					}
				}
				if !cleared {
					isDeleted = true
					break
				}
			}
			state[fieldName] = isDeleted

		case "keyed-lww":
			// lww applied independently within each declared key tuple.
			type keyedEntry struct {
				key   []string
				value any
			}
			latest := make(map[string]*keyedEntry)
			for _, id := range totalOrder {
				op := opMap[id]
				val, ok := op.Body[fieldName]
				if !ok {
					continue
				}
				key := make([]string, 0, len(cfg.Key))
				for _, kf := range cfg.Key {
					key = append(key, fmt.Sprint(op.Body[kf]))
				}
				latest[fmt.Sprintf("%q", key)] = &keyedEntry{key: key, value: val}
			}
			entries := make([]*keyedEntry, 0, len(latest))
			for _, e := range latest {
				entries = append(entries, e)
			}
			sort.Slice(entries, func(i, j int) bool {
				a, b := entries[i].key, entries[j].key
				for x := range a {
					if a[x] != b[x] {
						return a[x] < b[x]
					}
				}
				return false
			})
			keyed := make([]any, 0, len(entries))
			for _, e := range entries {
				keyed = append(keyed, map[string]any{"key": e.key, "value": e.value})
			}
			state[fieldName] = keyed

		case "lattice":
			rankMap := make(map[string]int, len(cfg.Lattice))
			for i, elem := range cfg.Lattice {
				rankMap[elem] = i
			}
			currentRank := -1
			var currentVal string
			for _, id := range totalOrder {
				op := opMap[id]
				if raw, ok := op.Body[fieldName]; ok {
					valStr := fmt.Sprint(raw)
					if r, ok := rankMap[valStr]; ok {
						if r > currentRank {
							currentRank = r
							currentVal = valStr
						}
					}
				}
			}
			state[fieldName] = currentVal
		}
	}

	return state, nil
}

func TestMergeVectorsLoad(t *testing.T) {
	vectors, err := spec.MergeVectors()
	if err != nil {
		t.Fatalf("loading merge vectors: %v", err)
	}
	if len(vectors) == 0 {
		t.Fatal("no merge vectors loaded")
	}
}

func TestMergeVectors(t *testing.T) {
	vectors, err := spec.MergeVectors()
	if err != nil {
		t.Fatalf("loading merge vectors: %v", err)
	}

	for _, vec := range vectors {
		t.Run(vec.Name, func(t *testing.T) {
			foldedState, err := foldMerge(vec)
			if err != nil {
				t.Fatalf("foldMerge failed: %v", err)
			}

			gotJSON, err := canonicaljson.Marshal(mustJSON(t, foldedState))
			if err != nil {
				t.Fatalf("canonicalizing got state: %v", err)
			}

			wantJSON, err := canonicaljson.Marshal(mustJSON(t, vec.ExpectedState))
			if err != nil {
				t.Fatalf("canonicalizing want state: %v", err)
			}

			if !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("folded state mismatch:\n got: %s\nwant: %s", string(gotJSON), string(wantJSON))
			}
		})
	}
}

// TestMergeCoverage guards that every strategy in the closed catalogue has at least one vector.
func TestMergeCoverage(t *testing.T) {
	vectors, err := spec.MergeVectors()
	if err != nil {
		t.Fatalf("loading merge vectors: %v", err)
	}

	covered := make(map[string]bool)
	for _, vec := range vectors {
		for _, cfg := range vec.Fields {
			covered[cfg.Strategy] = true
		}
	}

	for strat := range spec.KnownCatalogueStrategies {
		if !covered[strat] {
			t.Errorf("catalogue strategy %q has no test vector in testdata/fold/merge/", strat)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return b
}
