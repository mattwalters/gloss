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

	// Sort object IDs for deterministic golden output
	var objectIDs []string
	for objID := range enumRes.Ops {
		objectIDs = append(objectIDs, objID)
	}
	sort.Strings(objectIDs)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for _, objID := range objectIDs {
		codecOps := enumRes.Ops[objID]
		if len(codecOps) == 0 || codecOps[0].ObjectType != "review" {
			continue
		}

		reviewState, err := writ.FoldReview(codecOps)
		if err != nil {
			return nil, fmt.Errorf("writ.FoldReview for object %s in %s: %w", objID, fix.Name, err)
		}

		expectedJSON, err := canonicaljson.Marshal(mustJSON(t, reviewState))
		if err != nil {
			return nil, fmt.Errorf("canonicalizing review state for %s: %w", objID, err)
		}

		// Commutativity verification: shuffle input ops 100 times and verify identical output
		for i := 0; i < 100; i++ {
			shuffled := make([]codec.Op, len(codecOps))
			copy(shuffled, codecOps)
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

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal review golden: %w", err)
	}
	return append(b, '\n'), nil
}
