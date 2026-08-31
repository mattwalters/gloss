package spec

import (
	"fmt"
	"sort"
)

// EffectiveTimes computes the causality-monotone effective timestamp
// t*(u) = max(u.time, max_{p in Parents_S(u)} t*(p))
// for all ops in the restricted input set for target objectID.
func EffectiveTimes(ops []OrderOp, objectID string) map[string]int64 {
	inSet := make(map[string]bool)
	for _, op := range ops {
		if op.ObjectID == objectID {
			inSet[op.ID] = true
		}
	}
	return EffectiveTimesInSet(ops, inSet)
}

// EffectiveTimesInSet computes effective timestamps for ops within an explicit inSet.
func EffectiveTimesInSet(ops []OrderOp, inSet map[string]bool) map[string]int64 {
	tStar := make(map[string]int64, len(inSet))
	opMap := make(map[string]OrderOp, len(ops))
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

// TotalOrder produces the deterministic total order sequence L of ops for target objectID
// using Kahn's algorithm with a priority queue ordered by (t*, id).
//
// This is the spec's reference implementation of the total order algorithm defined in spec/fold.md §4.
func TotalOrder(ops []OrderOp, objectID string) ([]string, error) {
	inSet := make(map[string]bool)
	for _, op := range ops {
		if op.ObjectID == objectID {
			inSet[op.ID] = true
		}
	}
	if len(inSet) == 0 {
		return nil, nil
	}

	tStar := EffectiveTimesInSet(ops, inSet)

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
		ready = append(ready[:bestIdx], ready[bestIdx+1:]...)

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

// BuildReachabilityMap computes transitive reachability (isAncestor) in the restricted DAG.
func BuildReachabilityMap(ops []MergeOp, inSet map[string]bool) map[string]map[string]bool {
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

// Fold is the spec's reference fold reducer. It executes deterministic fold reduction
// on an input set of operations against the declared catalogue field rules.
//
// This is the normative reference reducer used to produce and check golden fold outputs.
// Engine reducers (WRIT-25/26/27) are independent implementations validated against the same goldens.
func Fold(ops []MergeOp, rules []FieldRule) (map[string]any, error) {
	if len(ops) == 0 {
		return make(map[string]any), nil
	}

	objectID := ops[0].ObjectID
	inSet := make(map[string]bool)
	var orderOps []OrderOp
	opMap := make(map[string]MergeOp, len(ops))

	for _, op := range ops {
		opMap[op.ID] = op
		if op.ObjectID == objectID {
			inSet[op.ID] = true
		}
		orderOps = append(orderOps, OrderOp{
			ID:       op.ID,
			Parents:  op.Parents,
			Time:     op.Time,
			ObjectID: op.ObjectID,
		})
	}

	totalOrder, err := TotalOrder(orderOps, objectID)
	if err != nil {
		return nil, fmt.Errorf("spec: ordering ops: %w", err)
	}

	ancestors := BuildReachabilityMap(ops, inSet)
	isAncestor := func(a, b string) bool {
		return ancestors[b][a]
	}

	// Helper to check if an op matches a rule
	opMatchesRule := func(op MergeOp, r FieldRule) bool {
		if r.OpType != "" && r.OpType != op.OpType {
			return false
		}
		if r.OpVersion != 0 && op.OpVersion != 0 && r.OpVersion != op.OpVersion {
			return false
		}
		return true
	}

	// Find all rules that match ops actually present in the input set
	matchedRulesByField := make(map[string][]FieldRule)
	for _, r := range rules {
		for _, op := range ops {
			if !inSet[op.ID] {
				continue
			}
			if opMatchesRule(op, r) {
				if _, present := op.Body[r.Field]; present || op.OpType == "delete" || op.OpType == "undelete" {
					matchedRulesByField[r.Field] = append(matchedRulesByField[r.Field], r)
					break
				}
			}
		}
	}

	state := make(map[string]any)

	// Iterate fields in deterministic order
	var fieldNames []string
	for f := range matchedRulesByField {
		fieldNames = append(fieldNames, f)
	}
	sort.Strings(fieldNames)

	for _, fieldName := range fieldNames {
		frs := matchedRulesByField[fieldName]
		if len(frs) == 0 {
			continue
		}
		primaryRule := frs[0]

		switch primaryRule.Strategy {
		case "lww":
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if val, present := op.Body[fieldName]; present && val != nil {
							state[fieldName] = val
						}
					}
				}
			}

		case "create-once":
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if _, alreadySet := state[fieldName]; !alreadySet {
							if val, present := op.Body[fieldName]; present && val != nil {
								state[fieldName] = val
							}
						}
					}
				}
			}

		case "set-union":
			unionSet := make(map[string]bool)
			hasSet := false
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if raw, present := op.Body[fieldName]; present {
							hasSet = true
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
				}
			}
			if hasSet {
				var result []string
				for k := range unionSet {
					result = append(result, k)
				}
				sort.Strings(result)
				state[fieldName] = result
			}

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
			hasOps := false

			for _, op := range ops {
				if !inSet[op.ID] {
					continue
				}
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if raw, present := op.Body[fieldName]; present {
							hasOps = true
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
				}
			}

			if hasOps {
				presentSet := make(map[string]bool)
				for _, add := range adds {
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
			}

		case "append":
			var list []any
			hasAppend := false
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if raw, present := op.Body[fieldName]; present {
							hasAppend = true
							if slice, ok := raw.([]any); ok {
								list = append(list, slice...)
							} else {
								list = append(list, raw)
							}
						}
					}
				}
			}
			if hasAppend {
				state[fieldName] = list
			}

		case "tombstone":
			var deletes []string
			var undeletes []string
			hasTombstone := false
			for _, op := range ops {
				if !inSet[op.ID] {
					continue
				}
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if op.OpType == "delete" || op.Body[fieldName] == true {
							deletes = append(deletes, op.ID)
							hasTombstone = true
						} else if op.OpType == "undelete" || op.Body[fieldName] == false {
							undeletes = append(undeletes, op.ID)
							hasTombstone = true
						}
					}
				}
			}

			if hasTombstone {
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
			}

		case "lattice":
			rankMap := make(map[string]int, len(primaryRule.Lattice))
			for i, elem := range primaryRule.Lattice {
				rankMap[elem] = i
			}
			currentRank := -1
			var currentVal string
			hasLattice := false
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if raw, present := op.Body[fieldName]; present {
							valStr := fmt.Sprint(raw)
							if rk, ok := rankMap[valStr]; ok {
								if rk > currentRank {
									currentRank = rk
									currentVal = valStr
									hasLattice = true
								}
							}
						}
					}
				}
			}
			if hasLattice {
				state[fieldName] = currentVal
			}

		case "keyed-lww":
			type keyedEntry struct {
				key   []string
				value any
			}
			latest := make(map[string]*keyedEntry)
			hasKeyed := false
			for _, id := range totalOrder {
				op := opMap[id]
				for _, rule := range frs {
					if opMatchesRule(op, rule) {
						val, present := op.Body[fieldName]
						if !present {
							continue
						}
						hasKeyed = true
						key := make([]string, 0, len(rule.Key))
						for _, kf := range rule.Key {
							key = append(key, fmt.Sprint(op.Body[kf]))
						}
						latest[fmt.Sprintf("%q", key)] = &keyedEntry{key: key, value: val}
					}
				}
			}

			if hasKeyed {
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
			}
		}
	}

	return state, nil
}
