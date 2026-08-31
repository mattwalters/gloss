package projection_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/projection"
	"github.com/writtendev/writ/spec/fixtures"
)

func TestFixturesIncrementalVsColdAndFoldAgreement(t *testing.T) {
	corpus, err := fixtures.LoadCorpus()
	if err != nil {
		t.Fatalf("fixtures.LoadCorpus: %v", err)
	}

	for _, desc := range corpus {
		desc := desc
		// Select fold, forward-compat, multi-writer, and review-mixed-signals fixtures
		if !strings.HasPrefix(desc.Name, "fold-") &&
			!strings.HasPrefix(desc.Name, "forward-compat-") &&
			!strings.HasPrefix(desc.Name, "multi-writer-") &&
			desc.Name != "review-mixed-signals" {
			continue
		}

		t.Run(desc.Name, func(t *testing.T) {
			repoDir := filepath.Join(t.TempDir(), "repo")
			manifest, err := fixtures.Generate(desc, repoDir)
			if err != nil {
				t.Fatalf("fixtures.Generate %s: %v", desc.Name, err)
			}

			repo, err := git.PlainOpen(repoDir)
			if err != nil {
				t.Fatalf("git.PlainOpen %s: %v", desc.Name, err)
			}

			store, err := dag.OpenRepo(repo, identity.Identity{})
			if err != nil {
				t.Fatalf("dag.OpenRepo %s: %v", desc.Name, err)
			}

			// 1. Cold build
			dbCold, err := projection.Open(":memory:")
			if err != nil {
				t.Fatalf("Open cold db: %v", err)
			}
			defer dbCold.Close()

			statsCold, err := dbCold.Refresh(store)
			if err != nil {
				t.Fatalf("dbCold.Refresh: %v", err)
			}
			coldDump, err := dbCold.DumpTables()
			if err != nil {
				t.Fatalf("dbCold.DumpTables: %v", err)
			}

			// 2. Incremental build: step refs commit by commit
			// Record final ref tips
			finalRefs := make(map[string]plumbing.Hash)
			refChains := make(map[string][]plumbing.Hash)

			for _, r := range manifest.Refs {
				refName := plumbing.ReferenceName(r.Name)
				finalHash := plumbing.NewHash(r.Commit)
				finalRefs[r.Name] = finalHash

				// Find ancestry of finalHash in topological / chronological order
				var ancestry []plumbing.Hash
				curr := finalHash
				for !curr.IsZero() {
					ancestry = append([]plumbing.Hash{curr}, ancestry...)
					cObj, err := repo.CommitObject(curr)
					if err != nil || len(cObj.ParentHashes) == 0 {
						break
					}
					curr = cObj.ParentHashes[0]
				}
				refChains[r.Name] = ancestry

				// Reset ref to first commit in ancestry
				if len(ancestry) > 0 {
					_ = repo.Storer.SetReference(plumbing.NewReferenceFromStrings(refName.String(), ancestry[0].String()))
				}
			}

			dbInc, err := projection.Open(":memory:")
			if err != nil {
				t.Fatalf("Open inc db: %v", err)
			}
			defer dbInc.Close()

			// Initial refresh at starting commit of each ref
			_, err = dbInc.Refresh(store)
			if err != nil {
				t.Fatalf("dbInc.Refresh initial: %v", err)
			}

			// Step each ref forward one commit at a time with a Refresh between steps
			maxLen := 0
			for _, chain := range refChains {
				if len(chain) > maxLen {
					maxLen = len(chain)
				}
			}

			for step := 1; step < maxLen; step++ {
				movedAny := false
				for refNameStr, chain := range refChains {
					if step < len(chain) {
						refName := plumbing.ReferenceName(refNameStr)
						_ = repo.Storer.SetReference(plumbing.NewReferenceFromStrings(refName.String(), chain[step].String()))
						movedAny = true
					}
				}
				if movedAny {
					_, err := dbInc.Refresh(store)
					if err != nil {
						t.Fatalf("dbInc.Refresh step %d: %v", step, err)
					}
				}
			}

			// Ensure all refs are at final tips
			for refNameStr, finalHash := range finalRefs {
				refName := plumbing.ReferenceName(refNameStr)
				_ = repo.Storer.SetReference(plumbing.NewReferenceFromStrings(refName.String(), finalHash.String()))
			}
			_, err = dbInc.Refresh(store)
			if err != nil {
				t.Fatalf("dbInc.Refresh final: %v", err)
			}

			incDump, err := dbInc.DumpTables()
			if err != nil {
				t.Fatalf("dbInc.DumpTables: %v", err)
			}

			// 3. Assert incremental == cold
			if !reflect.DeepEqual(incDump, coldDump) {
				t.Fatalf("fixture %s: incremental dump != cold dump:\nincremental: %+v\ncold: %+v",
					desc.Name, incDump, coldDump)
			}

			// 4. Cross-check: Projection == Fold for all reviews and comments
			enumRes, err := store.Enumerate()
			if err != nil {
				t.Fatalf("store.Enumerate %s: %v", desc.Name, err)
			}

			for objID, ops := range enumRes.Ops {
				if len(ops) == 0 {
					continue
				}

				hasReview := false
				hasComment := false
				for _, op := range ops {
					if op.ObjectType == "review" {
						hasReview = true
					}
					if op.ObjectType == "comment" {
						hasComment = true
					}
				}

				if hasReview {
					reviewState, err := writ.FoldReview(ops)
					if err != nil {
						t.Fatalf("writ.FoldReview for %s in %s: %v", objID, desc.Name, err)
					}

					var title, descText, status, mergeCommit, reason string
					err = dbCold.DB().QueryRow(`
						SELECT title, description, status, merge_commit, reason
						FROM reviews
						WHERE object_id = ?
					`, objID).Scan(&title, &descText, &status, &mergeCommit, &reason)
					if err != nil {
						t.Fatalf("query review %s: %v", objID, err)
					}

					if title != reviewState.Title || descText != reviewState.Description ||
						status != reviewState.Status || mergeCommit != reviewState.MergeCommit ||
						reason != reviewState.Reason {
						t.Fatalf("review %s in %s differs between fold and projection:\n fold: %+v\n proj: title=%q desc=%q status=%q merge=%q reason=%q",
							objID, desc.Name, reviewState, title, descText, status, mergeCommit, reason)
					}

					// Verify revisions count
					var revCount int
					_ = dbCold.DB().QueryRow("SELECT COUNT(*) FROM review_revisions WHERE review_object_id = ?", objID).Scan(&revCount)
					if revCount != len(reviewState.Revisions) {
						t.Fatalf("review %s revisions count mismatch: fold=%d proj=%d", objID, len(reviewState.Revisions), revCount)
					}

					// Verify approvals count
					var appCount int
					_ = dbCold.DB().QueryRow("SELECT COUNT(*) FROM approvals WHERE review_object_id = ?", objID).Scan(&appCount)
					if appCount != len(reviewState.Approvals) {
						t.Fatalf("review %s approvals count mismatch: fold=%d proj=%d", objID, len(reviewState.Approvals), appCount)
					}

					// Verify ci_statuses count
					var ciCount int
					_ = dbCold.DB().QueryRow("SELECT COUNT(*) FROM ci_statuses WHERE review_object_id = ?", objID).Scan(&ciCount)
					if ciCount != len(reviewState.CIStatuses) {
						t.Fatalf("review %s ci_statuses count mismatch: fold=%d proj=%d", objID, len(reviewState.CIStatuses), ciCount)
					}
				} else if hasComment {
					commentState, err := writ.FoldComment(ops)
					if err != nil {
						t.Fatalf("writ.FoldComment for %s in %s: %v", objID, desc.Name, err)
					}

					var subType, subID, text, inReplyTo, anchorStr string
					var deletedInt int
					err = dbCold.DB().QueryRow(`
						SELECT subject_type, subject_id, text, in_reply_to, anchor, deleted
						FROM comments
						WHERE object_id = ?
					`, objID).Scan(&subType, &subID, &text, &inReplyTo, &anchorStr, &deletedInt)
					if err != nil {
						t.Fatalf("query comment %s: %v", objID, err)
					}

					if subType != commentState.Subject.ObjectType || subID != commentState.Subject.ObjectID ||
						text != commentState.Text || inReplyTo != commentState.InReplyTo ||
						(deletedInt == 1) != commentState.Deleted {
						t.Fatalf("comment %s in %s differs between fold and projection", objID, desc.Name)
					}
				}
			}

			_ = statsCold
		})
	}
}
