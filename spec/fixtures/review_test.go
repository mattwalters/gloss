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

// TestReviewFamily registers the review fixture family and runs all descriptions
// carrying review collaborative objects through the typed FoldReview golden test harness.
func TestReviewFamily(t *testing.T) {
	fixtures.Run(t, fixtures.Family{
		Name:      "review",
		GoldenDir: "testdata/golden/review",
		Filter: func(desc *fixtures.Description) bool {
			if !strings.HasPrefix(desc.Name, "fold-") &&
				!strings.HasPrefix(desc.Name, "review-") &&
				!strings.HasPrefix(desc.Name, "forward-compat-") {
				return false
			}
			for _, ref := range desc.Refs {
				for _, gen := range ref.History {
					for _, c := range gen.Commits {
						if c.Op != nil && c.Op.ObjectType == "review" {
							return true
						}
					}
				}
			}
			return false
		},
		Runner: runReviewFixture,
	})
}

type ReviewGolden struct {
	Objects []ReviewObjectGolden `json:"objects"`
}

type ReviewObjectGolden struct {
	ObjectID string      `json:"object_id"`
	Review   writ.Review `json:"review"`
}

func runReviewFixture(t *testing.T, fix *fixtures.Fixture) ([]byte, error) {
	t.Helper()

	store, err := dag.OpenRepo(fix.Repo, identity.Identity{})
	if err != nil {
		return nil, fmt.Errorf("dag.OpenRepo failed: %w", err)
	}

	enumRes, err := store.Enumerate()
	if err != nil {
		return nil, fmt.Errorf("store.Enumerate failed: %w", err)
	}

	var golden ReviewGolden

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
					pureCommit, err := codec.FromGitCommit(fix.Repo, commitObj)
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

	// Sort object IDs for deterministic golden output
	var objectIDs []string
	for objID := range opsByObject {
		objectIDs = append(objectIDs, objID)
	}
	sort.Strings(objectIDs)

	r := rand.New(rand.NewSource(42))

	for _, objID := range objectIDs {
		codecOps := opsByObject[objID]
		var reviewOps []codec.Op
		for _, op := range codecOps {
			if op.ObjectType == "review" {
				reviewOps = append(reviewOps, op)
			}
		}
		if len(reviewOps) == 0 {
			continue
		}

		reviewState, err := writ.FoldReview(reviewOps)
		if err != nil {
			return nil, fmt.Errorf("writ.FoldReview for object %s in %s: %w", objID, fix.Name, err)
		}

		expectedJSON, err := canonicaljson.Marshal(mustJSON(t, reviewState))
		if err != nil {
			return nil, fmt.Errorf("canonicalizing review state for %s: %w", objID, err)
		}

		// Commutativity verification: shuffle input ops 100 times and verify identical output
		for i := 0; i < 100; i++ {
			shuffled := make([]codec.Op, len(reviewOps))
			copy(shuffled, reviewOps)
			r.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			shuffledReview, err := writ.FoldReview(shuffled)
			if err != nil {
				t.Fatalf("commutativity violation on permutation #%d for object %s in %s: %v", i, objID, fix.Name, err)
			}

			shuffledJSON, err := canonicaljson.Marshal(mustJSON(t, shuffledReview))
			if err != nil {
				t.Fatalf("canonicalizing shuffled review state on permutation #%d for %s in %s: %v", i, objID, fix.Name, err)
			}

			if !bytes.Equal(shuffledJSON, expectedJSON) {
				t.Fatalf("commutativity violation on permutation #%d for object %s in fixture %s:\n got:  %s\n want: %s",
					i, objID, fix.Name, string(shuffledJSON), string(expectedJSON))
			}
		}

		golden.Objects = append(golden.Objects, ReviewObjectGolden{
			ObjectID: objID,
			Review:   reviewState,
		})
	}

	if len(golden.Objects) == 0 {
		return nil, fmt.Errorf("review fixture %s yielded zero review objects", fix.Name)
	}

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal review golden: %w", err)
	}
	return append(b, '\n'), nil
}
