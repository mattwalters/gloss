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

// TestProjectFamily registers the project fixture family and runs all descriptions
// carrying project collaborative objects through the typed FoldProject golden test harness.
func TestProjectFamily(t *testing.T) {
	fixtures.Run(t, fixtures.Family{
		Name:      "project",
		GoldenDir: "testdata/golden/project",
		Filter: func(desc *fixtures.Description) bool {
			if !strings.HasPrefix(desc.Name, "project-") {
				return false
			}
			for _, ref := range desc.Refs {
				for _, gen := range ref.History {
					for _, c := range gen.Commits {
						if c.Op != nil && c.Op.ObjectType == "project" {
							return true
						}
					}
				}
			}
			return false
		},
		Runner: runProjectFixture,
	})
}

// TestCycleFamily registers the cycle fixture family and runs all descriptions
// carrying cycle collaborative objects through the typed FoldCycle golden test harness.
func TestCycleFamily(t *testing.T) {
	fixtures.Run(t, fixtures.Family{
		Name:      "cycle",
		GoldenDir: "testdata/golden/cycle",
		Filter: func(desc *fixtures.Description) bool {
			if !strings.HasPrefix(desc.Name, "cycle-") {
				return false
			}
			for _, ref := range desc.Refs {
				for _, gen := range ref.History {
					for _, c := range gen.Commits {
						if c.Op != nil && c.Op.ObjectType == "cycle" {
							return true
						}
					}
				}
			}
			return false
		},
		Runner: runCycleFixture,
	})
}

type ProjectGolden struct {
	Objects []ProjectObjectGolden `json:"objects"`
}

type ProjectObjectGolden struct {
	ObjectID string       `json:"object_id"`
	Project  writ.Project `json:"project"`
}

type CycleGolden struct {
	Objects []CycleObjectGolden `json:"objects"`
}

type CycleObjectGolden struct {
	ObjectID string     `json:"object_id"`
	Cycle    writ.Cycle `json:"cycle"`
}

func runProjectFixture(t *testing.T, fix *fixtures.Fixture) ([]byte, error) {
	t.Helper()

	store, err := dag.OpenRepo(fix.Repo, identity.Identity{})
	if err != nil {
		return nil, fmt.Errorf("dag.OpenRepo failed: %w", err)
	}

	enumRes, err := store.Enumerate()
	if err != nil {
		return nil, fmt.Errorf("store.Enumerate failed: %w", err)
	}

	var golden ProjectGolden

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
		var projectOps []codec.Op
		for _, op := range codecOps {
			if op.ObjectType == "project" {
				projectOps = append(projectOps, op)
			}
		}
		if len(projectOps) == 0 {
			continue
		}

		projectState, err := writ.FoldProject(projectOps)
		if err != nil {
			return nil, fmt.Errorf("writ.FoldProject for object %s in %s: %w", objID, fix.Name, err)
		}

		objectState, err := writ.Fold(codecOps, writ.ProjectRules())
		if err != nil {
			return nil, fmt.Errorf("writ.Fold for object %s in %s: %w", objID, fix.Name, err)
		}
		assertProjectFoldAgreement(t, projectState, objectState, fix.Name, objID)

		expectedJSON, err := canonicaljson.Marshal(mustJSON(t, projectState))
		if err != nil {
			return nil, fmt.Errorf("canonicalizing project state for %s: %w", objID, err)
		}

		for i := 0; i < 100; i++ {
			shuffled := make([]codec.Op, len(projectOps))
			copy(shuffled, projectOps)
			r.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			shuffledProject, err := writ.FoldProject(shuffled)
			if err != nil {
				t.Fatalf("commutativity violation on permutation #%d for object %s in %s: %v", i, objID, fix.Name, err)
			}

			shuffledJSON, err := canonicaljson.Marshal(mustJSON(t, shuffledProject))
			if err != nil {
				t.Fatalf("canonicalizing shuffled project state on permutation #%d for %s in %s: %v", i, objID, fix.Name, err)
			}

			if !bytes.Equal(shuffledJSON, expectedJSON) {
				t.Fatalf("commutativity violation on permutation #%d for object %s in fixture %s:\n got:  %s\n want: %s",
					i, objID, fix.Name, string(shuffledJSON), string(expectedJSON))
			}
		}

		golden.Objects = append(golden.Objects, ProjectObjectGolden{
			ObjectID: objID,
			Project:  projectState,
		})
	}

	if len(golden.Objects) == 0 {
		return nil, fmt.Errorf("project fixture %s yielded zero project objects", fix.Name)
	}

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal project golden: %w", err)
	}
	return append(b, '\n'), nil
}

func runCycleFixture(t *testing.T, fix *fixtures.Fixture) ([]byte, error) {
	t.Helper()

	store, err := dag.OpenRepo(fix.Repo, identity.Identity{})
	if err != nil {
		return nil, fmt.Errorf("dag.OpenRepo failed: %w", err)
	}

	enumRes, err := store.Enumerate()
	if err != nil {
		return nil, fmt.Errorf("store.Enumerate failed: %w", err)
	}

	var golden CycleGolden

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
		var cycleOps []codec.Op
		for _, op := range codecOps {
			if op.ObjectType == "cycle" {
				cycleOps = append(cycleOps, op)
			}
		}
		if len(cycleOps) == 0 {
			continue
		}

		cycleState, err := writ.FoldCycle(cycleOps)
		if err != nil {
			return nil, fmt.Errorf("writ.FoldCycle for object %s in %s: %w", objID, fix.Name, err)
		}

		objectState, err := writ.Fold(codecOps, writ.CycleRules())
		if err != nil {
			return nil, fmt.Errorf("writ.Fold for object %s in %s: %w", objID, fix.Name, err)
		}
		assertCycleFoldAgreement(t, cycleState, objectState, fix.Name, objID)

		expectedJSON, err := canonicaljson.Marshal(mustJSON(t, cycleState))
		if err != nil {
			return nil, fmt.Errorf("canonicalizing cycle state for %s: %w", objID, err)
		}

		for i := 0; i < 100; i++ {
			shuffled := make([]codec.Op, len(cycleOps))
			copy(shuffled, cycleOps)
			r.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			shuffledCycle, err := writ.FoldCycle(shuffled)
			if err != nil {
				t.Fatalf("commutativity violation on permutation #%d for object %s in %s: %v", i, objID, fix.Name, err)
			}

			shuffledJSON, err := canonicaljson.Marshal(mustJSON(t, shuffledCycle))
			if err != nil {
				t.Fatalf("canonicalizing shuffled cycle state on permutation #%d for %s in %s: %v", i, objID, fix.Name, err)
			}

			if !bytes.Equal(shuffledJSON, expectedJSON) {
				t.Fatalf("commutativity violation on permutation #%d for object %s in fixture %s:\n got:  %s\n want: %s",
					i, objID, fix.Name, string(shuffledJSON), string(expectedJSON))
			}
		}

		golden.Objects = append(golden.Objects, CycleObjectGolden{
			ObjectID: objID,
			Cycle:    cycleState,
		})
	}

	if len(golden.Objects) == 0 {
		return nil, fmt.Errorf("cycle fixture %s yielded zero cycle objects", fix.Name)
	}

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal cycle golden: %w", err)
	}
	return append(b, '\n'), nil
}

func assertProjectFoldAgreement(t *testing.T, project writ.Project, state writ.ObjectState, fixtureName, objectID string) {
	t.Helper()

	if title, ok := state.State["title"].(string); ok {
		if project.Title != title {
			t.Errorf("[%s/%s] agreement mismatch on title: FoldProject=%q, Fold=%q", fixtureName, objectID, project.Title, title)
		}
	} else if project.Title != "" {
		t.Errorf("[%s/%s] title present in FoldProject (%q) but not in Fold", fixtureName, objectID, project.Title)
	}

	if desc, ok := state.State["description"].(string); ok {
		if project.Description != desc {
			t.Errorf("[%s/%s] agreement mismatch on description: FoldProject=%q, Fold=%q", fixtureName, objectID, project.Description, desc)
		}
	} else if project.Description != "" {
		t.Errorf("[%s/%s] description present in FoldProject (%q) but not in Fold", fixtureName, objectID, project.Description)
	}

	if status, ok := state.State["status"].(string); ok {
		if project.Status != status {
			t.Errorf("[%s/%s] agreement mismatch on status: FoldProject=%q, Fold=%q", fixtureName, objectID, project.Status, status)
		}
	} else if project.Status != "" {
		t.Errorf("[%s/%s] status present in FoldProject (%q) but not in Fold", fixtureName, objectID, project.Status)
	}

	if reason, ok := state.State["reason"].(string); ok {
		if project.Reason != reason {
			t.Errorf("[%s/%s] agreement mismatch on reason: FoldProject=%q, Fold=%q", fixtureName, objectID, project.Reason, reason)
		}
	} else if project.Reason != "" {
		t.Errorf("[%s/%s] reason present in FoldProject (%q) but not in Fold", fixtureName, objectID, project.Reason)
	}
}

func assertCycleFoldAgreement(t *testing.T, cycle writ.Cycle, state writ.ObjectState, fixtureName, objectID string) {
	t.Helper()

	if title, ok := state.State["title"].(string); ok {
		if cycle.Title != title {
			t.Errorf("[%s/%s] agreement mismatch on title: FoldCycle=%q, Fold=%q", fixtureName, objectID, cycle.Title, title)
		}
	} else if cycle.Title != "" {
		t.Errorf("[%s/%s] title present in FoldCycle (%q) but not in Fold", fixtureName, objectID, cycle.Title)
	}

	if desc, ok := state.State["description"].(string); ok {
		if cycle.Description != desc {
			t.Errorf("[%s/%s] agreement mismatch on description: FoldCycle=%q, Fold=%q", fixtureName, objectID, cycle.Description, desc)
		}
	} else if cycle.Description != "" {
		t.Errorf("[%s/%s] description present in FoldCycle (%q) but not in Fold", fixtureName, objectID, cycle.Description)
	}

	if s, ok := state.State["starts_at"].(string); ok {
		if cycle.StartsAt != s {
			t.Errorf("[%s/%s] agreement mismatch on starts_at: FoldCycle=%q, Fold=%q", fixtureName, objectID, cycle.StartsAt, s)
		}
	} else if cycle.StartsAt != "" {
		t.Errorf("[%s/%s] starts_at present in FoldCycle (%q) but not in Fold", fixtureName, objectID, cycle.StartsAt)
	}

	if e, ok := state.State["ends_at"].(string); ok {
		if cycle.EndsAt != e {
			t.Errorf("[%s/%s] agreement mismatch on ends_at: FoldCycle=%q, Fold=%q", fixtureName, objectID, cycle.EndsAt, e)
		}
	} else if cycle.EndsAt != "" {
		t.Errorf("[%s/%s] ends_at present in FoldCycle (%q) but not in Fold", fixtureName, objectID, cycle.EndsAt)
	}
}
