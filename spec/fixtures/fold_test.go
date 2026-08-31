package fixtures_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/spec"
	"github.com/writtendev/writ/spec/fixtures"
)

// TestFoldFamily registers the fold fixture family and runs all fold-*.yaml
// descriptions through the golden test harness.
func TestFoldFamily(t *testing.T) {
	fixtures.Run(t, fixtures.Family{
		Name:      "fold",
		GoldenDir: "testdata/golden/fold",
		Filter: func(desc *fixtures.Description) bool {
			return strings.HasPrefix(desc.Name, "fold-")
		},
		Runner: runFoldFixture,
	})
}

type FoldGolden struct {
	Objects []FoldObjectGolden `json:"objects"`
}

type FoldObjectGolden struct {
	ObjectID   string             `json:"object_id"`
	ObjectType string             `json:"object_type,omitempty"`
	TotalOrder []FoldOpOrderEntry `json:"total_order"`
	State      map[string]any     `json:"state"`
	UnknownOps []FoldUnknownOp    `json:"unknown_ops,omitempty"`
}

type FoldOpOrderEntry struct {
	Commit string `json:"commit"`
	Label  string `json:"label,omitempty"`
	TStar  int64  `json:"t_star"`
}

type FoldUnknownOp struct {
	Commit    string `json:"commit"`
	Label     string `json:"label,omitempty"`
	OpType    string `json:"op_type"`
	OpVersion int64  `json:"op_version"`
}

func runFoldFixture(t *testing.T, fix *fixtures.Fixture) ([]byte, error) {
	t.Helper()

	store, err := dag.OpenRepo(fix.Repo, identity.Identity{})
	if err != nil {
		return nil, fmt.Errorf("dag.OpenRepo failed: %w", err)
	}

	enumRes, err := store.Enumerate()
	if err != nil {
		return nil, fmt.Errorf("store.Enumerate failed: %w", err)
	}

	rules, err := spec.FieldRules()
	if err != nil {
		return nil, fmt.Errorf("loading field rules: %w", err)
	}

	// Map commit SHA to description label
	shaToLabel := make(map[string]string)
	commitIdx := 0
	for _, ref := range fix.Description.Refs {
		for _, gen := range ref.History {
			gs := fix.Manifest.Generations[commitIdx]
			commitIdx++
			for ci, cd := range gen.Commits {
				cState := gs.Commits[ci]
				shaToLabel[cState.SHA] = cd.ID
			}
		}
	}

	// Known op types from field rules
	knownOps := make(map[string]bool)
	for _, r := range rules {
		knownOps[fmt.Sprintf("%s:%d", r.OpType, r.OpVersion)] = true
	}

	var golden FoldGolden

	// Sort object IDs for deterministic golden output
	var objectIDs []string
	for objID := range enumRes.Ops {
		objectIDs = append(objectIDs, objID)
	}
	sort.Strings(objectIDs)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for _, objID := range objectIDs {
		codecOps := enumRes.Ops[objID]
		if len(codecOps) == 0 {
			continue
		}

		var orderOps []spec.OrderOp
		var mergeOps []spec.MergeOp
		var unknownOps []FoldUnknownOp

		for _, cop := range codecOps {
			orderOps = append(orderOps, spec.OrderOp{
				ID:       cop.ID,
				Parents:  cop.Parents,
				Time:     cop.Author.When.UTC().Unix(),
				ObjectID: cop.ObjectID,
			})

			var body map[string]any
			if len(cop.Body) > 0 {
				_ = json.Unmarshal(cop.Body, &body)
			}
			if body == nil {
				body = make(map[string]any)
			}

			mergeOps = append(mergeOps, spec.MergeOp{
				ID:        cop.ID,
				Parents:   cop.Parents,
				Time:      cop.Author.When.UTC().Unix(),
				ObjectID:  cop.ObjectID,
				OpType:    cop.OpType,
				OpVersion: cop.OpVersion,
				Body:      body,
			})

			opKey := fmt.Sprintf("%s:%d", cop.OpType, cop.OpVersion)
			if !knownOps[opKey] {
				unknownOps = append(unknownOps, FoldUnknownOp{
					Commit:    cop.ID,
					Label:     shaToLabel[cop.ID],
					OpType:    cop.OpType,
					OpVersion: cop.OpVersion,
				})
			}
		}

		effectiveTimes := spec.EffectiveTimes(orderOps, objID)
		totalOrder, err := spec.TotalOrder(orderOps, objID)
		if err != nil {
			return nil, fmt.Errorf("total order for object %s: %w", objID, err)
		}

		foldedState, err := spec.Fold(mergeOps, rules)
		if err != nil {
			return nil, fmt.Errorf("fold for object %s: %w", objID, err)
		}

		// Commutativity verification: shuffle input ops 100 times and verify identical output
		expectedJSON, err := canonicaljson.Marshal(mustJSON(t, foldedState))
		if err != nil {
			return nil, fmt.Errorf("canonicalizing folded state for %s: %w", objID, err)
		}

		for i := 0; i < 100; i++ {
			shuffledMerge := make([]spec.MergeOp, len(mergeOps))
			copy(shuffledMerge, mergeOps)
			r.Shuffle(len(shuffledMerge), func(i, j int) {
				shuffledMerge[i], shuffledMerge[j] = shuffledMerge[j], shuffledMerge[i]
			})

			shuffledFolded, err := spec.Fold(shuffledMerge, rules)
			if err != nil {
				t.Fatalf("commutativity violation on permutation #%d for object %s: %v", i, objID, err)
			}

			shuffledJSON, err := canonicaljson.Marshal(mustJSON(t, shuffledFolded))
			if err != nil {
				t.Fatalf("canonicalizing shuffled folded state on permutation #%d: %v", i, err)
			}

			if !bytes.Equal(shuffledJSON, expectedJSON) {
				t.Fatalf("commutativity violation on permutation #%d for object %s in fixture %s:\n got:  %s\n want: %s",
					i, objID, fix.Name, string(shuffledJSON), string(expectedJSON))
			}
		}

		var orderEntries []FoldOpOrderEntry
		for _, sha := range totalOrder {
			orderEntries = append(orderEntries, FoldOpOrderEntry{
				Commit: sha,
				Label:  shaToLabel[sha],
				TStar:  effectiveTimes[sha],
			})
		}

		golden.Objects = append(golden.Objects, FoldObjectGolden{
			ObjectID:   objID,
			ObjectType: codecOps[0].ObjectType,
			TotalOrder: orderEntries,
			State:      foldedState,
			UnknownOps: unknownOps,
		})
	}

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal fold golden: %w", err)
	}
	return append(b, '\n'), nil
}

// TestFoldCoverage asserts that every (op_type, field) rule in published field-rules.json
// is exercised by at least one op in some fold-* fixture repo.
// Catalogue strategies with no v1 vocabulary field are explicitly exempted and required
// to be covered by abstract merge vectors instead.
func TestFoldCoverage(t *testing.T) {
	rules, err := spec.FieldRules()
	if err != nil {
		t.Fatalf("loading field rules: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("no field rules loaded")
	}

	corpus, err := fixtures.LoadCorpus()
	if err != nil {
		t.Fatalf("loading fixture corpus: %v", err)
	}

	// Collect all (op_type, field) writes present in fold-* descriptions
	coveredFields := make(map[string]bool)

	for _, desc := range corpus {
		if !strings.HasPrefix(desc.Name, "fold-") {
			continue
		}
		for _, ref := range desc.Refs {
			for _, gen := range ref.History {
				for _, c := range gen.Commits {
					if c.Op == nil {
						continue
					}
					opType := c.Op.OpType
					if opType == "delete" {
						coveredFields[fmt.Sprintf("%s:deleted", opType)] = true
					}
					if bodyMap, ok := c.Op.Body.(map[string]any); ok {
						for f := range bodyMap {
							coveredFields[fmt.Sprintf("%s:%s", opType, f)] = true
						}
					}
				}
			}
		}
	}

	// Check that every published rule has coverage in fold-* fixtures
	for _, rule := range rules {
		key := fmt.Sprintf("%s:%s", rule.OpType, rule.Field)
		if !coveredFields[key] {
			t.Errorf("uncovered field rule in fold-* fixtures: (%s, op_version: %d, field: %s, strategy: %s)",
				rule.OpType, rule.OpVersion, rule.Field, rule.Strategy)
		}
	}

	// Catalogue strategies with no v1 vocabulary field (covered by abstract merge vectors only)
	exemptStrategies := map[string]bool{
		"set-union":           true,
		"set-observed-remove": true,
		"lattice":             true,
	}

	mergeVectors, err := spec.MergeVectors()
	if err != nil {
		t.Fatalf("loading merge vectors: %v", err)
	}

	abstractCoverage := make(map[string]bool)
	for _, vec := range mergeVectors {
		for _, cfg := range vec.Fields {
			abstractCoverage[cfg.Strategy] = true
		}
	}

	for strat := range exemptStrategies {
		if !abstractCoverage[strat] {
			t.Errorf("exempt catalogue strategy %q has no abstract merge vector under testdata/fold/merge/", strat)
		}
	}

	// Check that every catalogue strategy is accounted for (either in published rules or exempt list)
	usedStrategies := make(map[string]bool)
	for _, r := range rules {
		usedStrategies[r.Strategy] = true
	}
	for strat := range spec.KnownCatalogueStrategies {
		if !usedStrategies[strat] && !exemptStrategies[strat] {
			t.Errorf("catalogue strategy %q is neither used in field rules nor in exemptStrategies", strat)
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
