package fixtures_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/spec/fixtures"
)

// TestWorkflowStateFamily registers the workflow-state fixture family and runs all descriptions
// carrying workflow-state collaborative objects through the typed FoldWorkflowState golden test harness.
func TestWorkflowStateFamily(t *testing.T) {
	fixtures.Run(t, fixtures.Family{
		Name:      "workflow-state",
		GoldenDir: "testdata/golden/workflow-state",
		Filter: func(desc *fixtures.Description) bool {
			if !strings.HasPrefix(desc.Name, "workflow-state-") {
				return false
			}
			for _, ref := range desc.Refs {
				for _, gen := range ref.History {
					for _, c := range gen.Commits {
						if c.Op != nil && c.Op.ObjectType == "workflow-state" {
							return true
						}
					}
				}
			}
			return false
		},
		Runner: runWorkflowStateFixture,
	})
}

type WorkflowStateGolden struct {
	Objects []WorkflowStateObjectGolden `json:"objects"`
}

type WorkflowStateObjectGolden struct {
	ObjectID      string             `json:"object_id"`
	WorkflowState writ.WorkflowState `json:"workflow_state"`
}

func runWorkflowStateFixture(t *testing.T, fix *fixtures.Fixture) ([]byte, error) {
	t.Helper()

	store, err := dag.OpenRepo(fix.Repo, identity.Identity{})
	if err != nil {
		return nil, fmt.Errorf("dag.OpenRepo failed: %w", err)
	}

	enumRes, err := store.Enumerate()
	if err != nil {
		return nil, fmt.Errorf("store.Enumerate failed: %w", err)
	}

	var golden WorkflowStateGolden

	opsByObject := enumRes.Ops
	if len(opsByObject) == 0 {
		opsByObject = make(map[string][]codec.Op)
		seenCommits := make(map[string]bool)
		cIdx := 0
		for _, ref := range fix.Description.Refs {
			isControl := strings.HasSuffix(ref.Name, "-control")
			for _, gen := range ref.History {
				gs := fix.Manifest.Generations[cIdx]
				cIdx++
				if isControl {
					continue
				}
				for ci := range gen.Commits {
					cState := gs.Commits[ci]
					if seenCommits[cState.SHA] {
						continue
					}
					seenCommits[cState.SHA] = true
					commitObj, err := fix.Repo.CommitObject(plumbing.NewHash(cState.SHA))
					if err != nil {
						return nil, fmt.Errorf("lookup commit %s: %w", cState.SHA, err)
					}
					pureCommit, err := codec.FromGitCommit(fix.Repo.Storer, commitObj)
					if err != nil {
						return nil, fmt.Errorf("from git commit %s: %w", cState.SHA, err)
					}
					op, err := codec.DecodeCommit(pureCommit)
					if err != nil {
						continue
					}
					opsByObject[op.ObjectID] = append(opsByObject[op.ObjectID], op)
				}
			}
		}
	}

	var objectIDs []string
	for objID := range opsByObject {
		objectIDs = append(objectIDs, objID)
	}
	sort.Strings(objectIDs)

	r := rand.New(rand.NewSource(42))

	for _, objID := range objectIDs {
		codecOps := opsByObject[objID]
		var wsOps []codec.Op
		for _, op := range codecOps {
			if op.ObjectType == "workflow-state" {
				wsOps = append(wsOps, op)
			}
		}
		if len(wsOps) == 0 {
			continue
		}

		wsState, err := writ.FoldWorkflowState(wsOps)
		if err != nil {
			return nil, fmt.Errorf("writ.FoldWorkflowState for object %s in %s: %w", objID, fix.Name, err)
		}

		objectState, err := writ.Fold(codecOps, writ.WorkflowStateRules())
		if err != nil {
			return nil, fmt.Errorf("writ.Fold for object %s in %s: %w", objID, fix.Name, err)
		}
		assertWorkflowStateFoldAgreement(t, wsState, objectState, fix.Name, objID)

		expectedJSON, err := canonicaljson.Marshal(mustJSON(t, wsState))
		if err != nil {
			return nil, fmt.Errorf("canonicalizing workflow-state state for %s: %w", objID, err)
		}

		for i := 0; i < 100; i++ {
			shuffled := make([]codec.Op, len(wsOps))
			copy(shuffled, wsOps)
			r.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			shuffledWS, err := writ.FoldWorkflowState(shuffled)
			if err != nil {
				t.Fatalf("commutativity violation on permutation #%d for object %s in %s: %v", i, objID, fix.Name, err)
			}

			shuffledJSON, err := canonicaljson.Marshal(mustJSON(t, shuffledWS))
			if err != nil {
				t.Fatalf("canonicalizing shuffled workflow-state state on permutation #%d for %s in %s: %v", i, objID, fix.Name, err)
			}

			if !bytes.Equal(shuffledJSON, expectedJSON) {
				t.Fatalf("commutativity violation on permutation #%d for object %s in fixture %s:\n got:  %s\n want: %s",
					i, objID, fix.Name, string(shuffledJSON), string(expectedJSON))
			}
		}

		golden.Objects = append(golden.Objects, WorkflowStateObjectGolden{
			ObjectID:      objID,
			WorkflowState: wsState,
		})
	}

	if len(golden.Objects) == 0 {
		return nil, fmt.Errorf("workflow-state fixture %s yielded zero workflow-state objects", fix.Name)
	}

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal workflow-state golden: %w", err)
	}
	return append(b, '\n'), nil
}

func assertWorkflowStateFoldAgreement(t *testing.T, ws writ.WorkflowState, state writ.ObjectState, fixtureName, objectID string) {
	t.Helper()

	if name, ok := state.State["name"].(string); ok {
		if ws.Name != name {
			t.Errorf("[%s/%s] agreement mismatch on name: FoldWorkflowState=%q, Fold=%q", fixtureName, objectID, ws.Name, name)
		}
	} else if ws.Name != "" {
		t.Errorf("[%s/%s] name present in FoldWorkflowState (%q) but not in Fold", fixtureName, objectID, ws.Name)
	}

	if tp, ok := state.State["type"].(string); ok {
		if ws.Type != tp {
			t.Errorf("[%s/%s] agreement mismatch on type: FoldWorkflowState=%q, Fold=%q", fixtureName, objectID, ws.Type, tp)
		}
	} else if ws.Type != "" {
		t.Errorf("[%s/%s] type present in FoldWorkflowState (%q) but not in Fold", fixtureName, objectID, ws.Type)
	}

	if pos, ok := state.State["position"].(string); ok {
		if ws.Position != pos {
			t.Errorf("[%s/%s] agreement mismatch on position: FoldWorkflowState=%q, Fold=%q", fixtureName, objectID, ws.Position, pos)
		}
	} else if ws.Position != "" {
		t.Errorf("[%s/%s] position present in FoldWorkflowState (%q) but not in Fold", fixtureName, objectID, ws.Position)
	}

	if col, ok := state.State["color"].(string); ok {
		if ws.Color != col {
			t.Errorf("[%s/%s] agreement mismatch on color: FoldWorkflowState=%q, Fold=%q", fixtureName, objectID, ws.Color, col)
		}
	} else if ws.Color != "" {
		t.Errorf("[%s/%s] color present in FoldWorkflowState (%q) but not in Fold", fixtureName, objectID, ws.Color)
	}

	if desc, ok := state.State["description"].(string); ok {
		if ws.Description != desc {
			t.Errorf("[%s/%s] agreement mismatch on description: FoldWorkflowState=%q, Fold=%q", fixtureName, objectID, ws.Description, desc)
		}
	} else if ws.Description != "" {
		t.Errorf("[%s/%s] description present in FoldWorkflowState (%q) but not in Fold", fixtureName, objectID, ws.Description)
	}
}
