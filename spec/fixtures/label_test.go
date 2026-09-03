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

// TestLabelFamily registers the label fixture family and runs all descriptions
// carrying label collaborative objects through the typed FoldLabel golden test harness.
func TestLabelFamily(t *testing.T) {
	fixtures.Run(t, fixtures.Family{
		Name:      "label",
		GoldenDir: "testdata/golden/label",
		Filter: func(desc *fixtures.Description) bool {
			if !strings.HasPrefix(desc.Name, "label-") {
				return false
			}
			for _, ref := range desc.Refs {
				for _, gen := range ref.History {
					for _, c := range gen.Commits {
						if c.Op != nil && c.Op.ObjectType == "label" {
							return true
						}
					}
				}
			}
			return false
		},
		Runner: runLabelFixture,
	})
}

type LabelGolden struct {
	Objects []LabelObjectGolden `json:"objects"`
}

type LabelObjectGolden struct {
	ObjectID string     `json:"object_id"`
	Label    writ.Label `json:"label"`
}

func runLabelFixture(t *testing.T, fix *fixtures.Fixture) ([]byte, error) {
	t.Helper()

	store, err := dag.OpenRepo(fix.Repo, identity.Identity{})
	if err != nil {
		return nil, fmt.Errorf("dag.OpenRepo failed: %w", err)
	}

	enumRes, err := store.Enumerate()
	if err != nil {
		return nil, fmt.Errorf("store.Enumerate failed: %w", err)
	}

	var golden LabelGolden

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
		var labelOps []codec.Op
		for _, op := range codecOps {
			if op.ObjectType == "label" {
				labelOps = append(labelOps, op)
			}
		}
		if len(labelOps) == 0 {
			continue
		}

		lblState, err := writ.FoldLabel(labelOps)
		if err != nil {
			return nil, fmt.Errorf("writ.FoldLabel for object %s in %s: %w", objID, fix.Name, err)
		}

		objectState, err := writ.Fold(codecOps, writ.LabelRules())
		if err != nil {
			return nil, fmt.Errorf("writ.Fold for object %s in %s: %w", objID, fix.Name, err)
		}
		assertLabelFoldAgreement(t, lblState, objectState, fix.Name, objID)

		expectedJSON, err := canonicaljson.Marshal(mustJSON(t, lblState))
		if err != nil {
			return nil, fmt.Errorf("canonicalizing label state for %s: %w", objID, err)
		}

		for i := 0; i < 100; i++ {
			shuffled := make([]codec.Op, len(labelOps))
			copy(shuffled, labelOps)
			r.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			shuffledLbl, err := writ.FoldLabel(shuffled)
			if err != nil {
				t.Fatalf("commutativity violation on permutation #%d for object %s in %s: %v", i, objID, fix.Name, err)
			}

			shuffledJSON, err := canonicaljson.Marshal(mustJSON(t, shuffledLbl))
			if err != nil {
				t.Fatalf("canonicalizing shuffled label state on permutation #%d for %s in %s: %v", i, objID, fix.Name, err)
			}

			if !bytes.Equal(shuffledJSON, expectedJSON) {
				t.Fatalf("commutativity violation on permutation #%d for object %s in fixture %s:\n got:  %s\n want: %s",
					i, objID, fix.Name, string(shuffledJSON), string(expectedJSON))
			}
		}

		golden.Objects = append(golden.Objects, LabelObjectGolden{
			ObjectID: objID,
			Label:    lblState,
		})
	}

	if len(golden.Objects) == 0 {
		return nil, fmt.Errorf("label fixture %s yielded zero label objects", fix.Name)
	}

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal label golden: %w", err)
	}
	return append(b, '\n'), nil
}

func assertLabelFoldAgreement(t *testing.T, lbl writ.Label, state writ.ObjectState, fixtureName, objectID string) {
	t.Helper()

	if name, ok := state.State["name"].(string); ok {
		if lbl.Name != name {
			t.Errorf("[%s/%s] agreement mismatch on name: FoldLabel=%q, Fold=%q", fixtureName, objectID, lbl.Name, name)
		}
	} else if lbl.Name != "" {
		t.Errorf("[%s/%s] name present in FoldLabel (%q) but not in Fold", fixtureName, objectID, lbl.Name)
	}

	if col, ok := state.State["color"].(string); ok {
		if lbl.Color != col {
			t.Errorf("[%s/%s] agreement mismatch on color: FoldLabel=%q, Fold=%q", fixtureName, objectID, lbl.Color, col)
		}
	} else if lbl.Color != "" {
		t.Errorf("[%s/%s] color present in FoldLabel (%q) but not in Fold", fixtureName, objectID, lbl.Color)
	}

	if desc, ok := state.State["description"].(string); ok {
		if lbl.Description != desc {
			t.Errorf("[%s/%s] agreement mismatch on description: FoldLabel=%q, Fold=%q", fixtureName, objectID, lbl.Description, desc)
		}
	} else if lbl.Description != "" {
		t.Errorf("[%s/%s] description present in FoldLabel (%q) but not in Fold", fixtureName, objectID, lbl.Description)
	}
}
