package spec_test

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/spec"
)

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
			gotOrder, err := spec.TotalOrder(vec.Ops, vec.ObjectID)
			if err != nil {
				t.Fatalf("spec.TotalOrder failed: %v", err)
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
				shuffledOrder, err := spec.TotalOrder(shuffled, vec.ObjectID)
				if err != nil {
					t.Fatalf("spec.TotalOrder on shuffled input failed: %v", err)
				}
				if !reflect.DeepEqual(shuffledOrder, vec.ExpectedOrder) {
					t.Fatalf("order changed across permutation #%d:\n got: %v\nwant: %v", i, shuffledOrder, vec.ExpectedOrder)
				}
			}
		})
	}
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
			var rules []spec.FieldRule
			for fieldName, cfg := range vec.Fields {
				rules = append(rules, spec.FieldRule{
					Field:    fieldName,
					Strategy: cfg.Strategy,
					Key:      cfg.Key,
					Lattice:  cfg.Lattice,
				})
			}

			foldedState, err := spec.Fold(vec.Ops, rules)
			if err != nil {
				t.Fatalf("spec.Fold failed: %v", err)
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
