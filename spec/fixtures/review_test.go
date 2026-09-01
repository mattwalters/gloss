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

		// Assert agreement between Fold(ops, ReviewRules()) and FoldReview(ops) across corpus (DoD #2)
		objectState, err := writ.Fold(codecOps, writ.ReviewRules())
		if err != nil {
			return nil, fmt.Errorf("writ.Fold for object %s in %s: %w", objID, fix.Name, err)
		}
		assertReviewFoldAgreement(t, reviewState, objectState, fix.Name, objID)

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

func assertReviewFoldAgreement(t *testing.T, review writ.Review, state writ.ObjectState, fixtureName, objectID string) {
	t.Helper()

	// 1. Scalar fields
	if title, ok := state.State["title"].(string); ok {
		if review.Title != title {
			t.Errorf("[%s/%s] agreement mismatch on title: FoldReview=%q, Fold=%q", fixtureName, objectID, review.Title, title)
		}
	} else if review.Title != "" {
		t.Errorf("[%s/%s] title present in FoldReview (%q) but not in Fold", fixtureName, objectID, review.Title)
	}

	if status, ok := state.State["status"].(string); ok {
		if review.Status != status {
			t.Errorf("[%s/%s] agreement mismatch on status: FoldReview=%q, Fold=%q", fixtureName, objectID, review.Status, status)
		}
	} else if review.Status != "" {
		t.Errorf("[%s/%s] status present in FoldReview (%q) but not in Fold", fixtureName, objectID, review.Status)
	}

	if mc, ok := state.State["merge_commit"].(string); ok {
		if review.MergeCommit != mc {
			t.Errorf("[%s/%s] agreement mismatch on merge_commit: FoldReview=%q, Fold=%q", fixtureName, objectID, review.MergeCommit, mc)
		}
	} else if review.MergeCommit != "" {
		t.Errorf("[%s/%s] merge_commit present in FoldReview (%q) but not in Fold", fixtureName, objectID, review.MergeCommit)
	}

	if reason, ok := state.State["reason"].(string); ok {
		if review.Reason != reason {
			t.Errorf("[%s/%s] agreement mismatch on reason: FoldReview=%q, Fold=%q", fixtureName, objectID, review.Reason, reason)
		}
	} else if review.Reason != "" {
		t.Errorf("[%s/%s] reason present in FoldReview (%q) but not in Fold", fixtureName, objectID, review.Reason)
	}

	// 2. Revisions pairing vs parallel append lists
	var baseList, headList []string
	if rawBases, ok := state.State["base"].([]any); ok {
		for _, b := range rawBases {
			baseList = append(baseList, fmt.Sprint(b))
		}
	}
	if rawHeads, ok := state.State["head"].([]any); ok {
		for _, h := range rawHeads {
			headList = append(headList, fmt.Sprint(h))
		}
	}

	if len(review.Revisions) != len(baseList) || len(review.Revisions) != len(headList) {
		t.Errorf("[%s/%s] revisions count mismatch: FoldReview=%d, Fold base=%d, Fold head=%d",
			fixtureName, objectID, len(review.Revisions), len(baseList), len(headList))
	} else {
		for i, rev := range review.Revisions {
			if rev.Base != baseList[i] || rev.Head != headList[i] {
				t.Errorf("[%s/%s] revision[%d] mismatch: FoldReview={%s, %s}, Fold={%s, %s}",
					fixtureName, objectID, i, rev.Base, rev.Head, baseList[i], headList[i])
			}
		}
	}

	// 3. Approvals: compare active verdicts
	if rawVerdicts, ok := state.State["verdict"].([]any); ok {
		activeVerdicts := make(map[string]string)
		for _, v := range rawVerdicts {
			if m, ok := v.(map[string]any); ok {
				var keyList []string
				switch ks := m["key"].(type) {
				case []string:
					keyList = ks
				case []any:
					for _, k := range ks {
						keyList = append(keyList, fmt.Sprint(k))
					}
				}
				if len(keyList) >= 2 {
					subj := keyList[0]
					rev := keyList[1]
					val := fmt.Sprint(m["value"])
					if val != "none" && val != "" {
						activeVerdicts[subj+":"+rev] = val
					}
				}
			}
		}

		if len(review.Approvals) != len(activeVerdicts) {
			t.Errorf("[%s/%s] approvals count mismatch: FoldReview=%d, Fold active=%d",
				fixtureName, objectID, len(review.Approvals), len(activeVerdicts))
		}
		for _, app := range review.Approvals {
			k := app.Subject + ":" + app.Revision
			if expectedVerdict, exists := activeVerdicts[k]; !exists || expectedVerdict != app.Verdict {
				t.Errorf("[%s/%s] approval %s mismatch: FoldReview verdict=%q, Fold=%q",
					fixtureName, objectID, k, app.Verdict, expectedVerdict)
			}
		}
	}

	// 4. CIStatuses: compare active states
	if rawStates, ok := state.State["state"].([]any); ok {
		activeStates := make(map[string]string)
		for _, s := range rawStates {
			if m, ok := s.(map[string]any); ok {
				var keyList []string
				switch ks := m["key"].(type) {
				case []string:
					keyList = ks
				case []any:
					for _, k := range ks {
						keyList = append(keyList, fmt.Sprint(k))
					}
				}
				if len(keyList) >= 2 {
					rev := keyList[0]
					name := keyList[1]
					val := fmt.Sprint(m["value"])
					activeStates[rev+":"+name] = val
				}
			}
		}

		if len(review.CIStatuses) != len(activeStates) {
			t.Errorf("[%s/%s] ci statuses count mismatch: FoldReview=%d, Fold active=%d",
				fixtureName, objectID, len(review.CIStatuses), len(activeStates))
		}
		for _, ci := range review.CIStatuses {
			k := ci.Revision + ":" + ci.Name
			if expectedState, exists := activeStates[k]; !exists || expectedState != ci.State {
				t.Errorf("[%s/%s] ci status %s mismatch: FoldReview state=%q, Fold=%q",
					fixtureName, objectID, k, ci.State, expectedState)
			}
		}
	}

	// 5. Links: compare active relations
	if rawRelations, ok := state.State["relation"].([]any); ok {
		activeRelations := make(map[string]string)
		for _, v := range rawRelations {
			if m, ok := v.(map[string]any); ok {
				var keyList []string
				switch ks := m["key"].(type) {
				case []string:
					keyList = ks
				case []any:
					for _, k := range ks {
						keyList = append(keyList, fmt.Sprint(k))
					}
				}
				if len(keyList) >= 1 {
					target := keyList[0]
					val := fmt.Sprint(m["value"])
					if val != "none" && val != "" {
						activeRelations[target] = val
					}
				}
			}
		}

		if len(review.Links) != len(activeRelations) {
			t.Errorf("[%s/%s] links count mismatch: FoldReview=%d, Fold active=%d",
				fixtureName, objectID, len(review.Links), len(activeRelations))
		}
		for _, l := range review.Links {
			if expectedRel, exists := activeRelations[l.Target]; !exists || expectedRel != l.Relation {
				t.Errorf("[%s/%s] link %s mismatch: FoldReview relation=%q, Fold=%q",
					fixtureName, objectID, l.Target, l.Relation, expectedRel)
			}
		}
	}
}
