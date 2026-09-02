package state_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/writtendev/writ/engine/codec"
	s "github.com/writtendev/writ/engine/state"
	"github.com/writtendev/writ/spec"
)

func TestRepoRulesDriftGuard(t *testing.T) {
	allRules, err := spec.FieldRules()
	if err != nil {
		t.Fatalf("spec.FieldRules failed: %v", err)
	}

	var expectedRules []s.Rule
	for _, r := range allRules {
		if r.Vocabulary == "repo" {
			expectedRules = append(expectedRules, s.Rule{
				OpType:    r.OpType,
				OpVersion: r.OpVersion,
				Field:     r.Field,
				Strategy:  r.Strategy,
				Key:       r.Key,
				Lattice:   r.Lattice,
			})
		}
	}

	builtIn := s.RepoRules()
	if !reflect.DeepEqual(builtIn, expectedRules) {
		t.Fatalf("RepoRules() drifted from published repo field-rules.json:\n got:  %+v\n want: %+v", builtIn, expectedRules)
	}
}

func TestFoldRepoEmpty(t *testing.T) {
	state, err := s.FoldRepo(nil)
	if err != nil {
		t.Fatalf("FoldRepo(nil) returned error: %v", err)
	}
	if !reflect.DeepEqual(state, s.RepoEntry{}) {
		t.Fatalf("expected empty RepoEntry, got %+v", state)
	}
}

func TestFoldRepoLifecycle(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	repoID := "a1b2c3d4e5f60718293a4b5c6d7e8f90"

	createOp := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   repoID,
			ObjectType: "repo",
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{"slug":"writtendev/writ","is_workspace":true}`),
		},
		ID: "c1",
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now,
		},
	}

	setSlugOp := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   repoID,
			ObjectType: "repo",
			OpType:     "set-slug",
			OpVersion:  1,
			Body:       json.RawMessage(`{"slug":"writtendev/writ-core"}`),
		},
		ID:      "s1",
		Parents: []string{"c1"},
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now.Add(time.Minute),
		},
	}

	addRemoteOp1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   repoID,
			ObjectType: "repo",
			OpType:     "add-remote",
			OpVersion:  1,
			Body:       json.RawMessage(`{"remote":"https://github.com/writtendev/writ.git"}`),
		},
		ID:      "r1",
		Parents: []string{"s1"},
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now.Add(2 * time.Minute),
		},
	}

	addRemoteOp2 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   repoID,
			ObjectType: "repo",
			OpType:     "add-remote",
			OpVersion:  1,
			Body:       json.RawMessage(`{"remote":"git@github.com:writtendev/writ.git"}`),
		},
		ID:      "r2",
		Parents: []string{"r1"},
		Author: codec.Identity{
			Email: "alice@example.com",
			When:  now.Add(3 * time.Minute),
		},
	}

	state, err := s.FoldRepo([]codec.Op{createOp, setSlugOp, addRemoteOp1, addRemoteOp2})
	if err != nil {
		t.Fatalf("FoldRepo failed: %v", err)
	}

	if state.RepoID != repoID {
		t.Errorf("repo_id mismatch: got %q, want %q", state.RepoID, repoID)
	}
	if state.Slug != "writtendev/writ-core" {
		t.Errorf("slug mismatch: got %q, want %q", state.Slug, "writtendev/writ-core")
	}
	if !state.IsWorkspace {
		t.Errorf("is_workspace mismatch: got false, want true")
	}
	expectedRemotes := []string{
		"git@github.com:writtendev/writ.git",
		"https://github.com/writtendev/writ.git",
	}
	if !reflect.DeepEqual(state.Remotes, expectedRemotes) {
		t.Errorf("remotes mismatch: got %v, want %v", state.Remotes, expectedRemotes)
	}
}

func TestFoldRepoCreateOnceWorkspace(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	repoID := "a1b2c3d4e5f60718293a4b5c6d7e8f90"

	// create op with is_workspace = true
	op1 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   repoID,
			ObjectType: "repo",
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{"slug":"repo-1","is_workspace":true}`),
		},
		ID:     "op1",
		Author: codec.Identity{Email: "alice@example.com", When: now},
	}

	// concurrent create op with is_workspace = false
	op2 := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   repoID,
			ObjectType: "repo",
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{"slug":"repo-1","is_workspace":false}`),
		},
		ID:     "op2",
		Author: codec.Identity{Email: "bob@example.com", When: now.Add(time.Minute)},
	}

	state, err := s.FoldRepo([]codec.Op{op1, op2})
	if err != nil {
		t.Fatalf("FoldRepo failed: %v", err)
	}

	if !state.IsWorkspace {
		t.Errorf("expected is_workspace create-once to retain first value (true), got false")
	}
}

func TestFoldRepoUnknownOpVersionAndType(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	repoID := "a1b2c3d4e5f60718293a4b5c6d7e8f90"

	v1Create := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   repoID,
			ObjectType: "repo",
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{"slug":"writ"}`),
		},
		ID:     "op-v1",
		Author: codec.Identity{Email: "alice@example.com", When: now},
	}

	v2Update := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   repoID,
			ObjectType: "repo",
			OpType:     "create",
			OpVersion:  2,
			Body:       json.RawMessage(`{"slug":"writ-v2"}`),
		},
		ID:      "op-v2",
		Parents: []string{"op-v1"},
		Author:  codec.Identity{Email: "alice@example.com", When: now.Add(time.Minute)},
	}

	state, err := s.FoldRepo([]codec.Op{v1Create, v2Update})
	if err != nil {
		t.Fatalf("FoldRepo failed: %v", err)
	}

	if state.Slug != "writ" {
		t.Errorf("slug mismatch: got %q, want 'writ'", state.Slug)
	}
	if len(state.UnknownOps) != 1 || state.UnknownOps[0].Commit != "op-v2" {
		t.Errorf("unknown_ops mismatch: %+v", state.UnknownOps)
	}
}

func TestFoldRepoMalformedBodyError(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	badOp := codec.Op{
		Envelope: codec.Envelope{
			ObjectID:   "a1b2c3d4e5f60718293a4b5c6d7e8f90",
			ObjectType: "repo",
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{malformed`),
		},
		ID:     "bad1",
		Author: codec.Identity{When: now},
	}

	_, errFold := s.Fold([]codec.Op{badOp}, s.RepoRules())
	if errFold == nil {
		t.Fatal("expected Fold to error on malformed JSON body, got nil")
	}

	_, errRepo := s.FoldRepo([]codec.Op{badOp})
	if errRepo == nil {
		t.Fatal("expected FoldRepo to error on malformed JSON body, got nil")
	}
}

func TestFoldRepoAgreement(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	repoID := "a1b2c3d4e5f60718293a4b5c6d7e8f90"

	ops := []codec.Op{
		{
			ID: "c1",
			Envelope: codec.Envelope{
				ObjectID:   repoID,
				ObjectType: "repo",
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(`{"slug":"repo-agree","is_workspace":false}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now},
		},
		{
			ID:      "s1",
			Parents: []string{"c1"},
			Envelope: codec.Envelope{
				ObjectID:   repoID,
				ObjectType: "repo",
				OpType:     "set-slug",
				OpVersion:  1,
				Body:       json.RawMessage(`{"slug":"repo-agree-new"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(time.Minute)},
		},
		{
			ID:      "r1",
			Parents: []string{"s1"},
			Envelope: codec.Envelope{
				ObjectID:   repoID,
				ObjectType: "repo",
				OpType:     "add-remote",
				OpVersion:  1,
				Body:       json.RawMessage(`{"remote":"git@github.com:example/repo.git"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(2 * time.Minute)},
		},
	}

	repoState, err := s.FoldRepo(ops)
	if err != nil {
		t.Fatalf("FoldRepo failed: %v", err)
	}

	objectState, err := s.Fold(ops, s.RepoRules())
	if err != nil {
		t.Fatalf("Fold failed: %v", err)
	}

	if repoState.Slug != objectState.State["slug"] {
		t.Errorf("slug mismatch: got %q, want %v", repoState.Slug, objectState.State["slug"])
	}
	expectedRemotes := []string{"git@github.com:example/repo.git"}
	if !reflect.DeepEqual(repoState.Remotes, expectedRemotes) {
		t.Errorf("remotes mismatch: got %v, want %v", repoState.Remotes, expectedRemotes)
	}
}

// TestFoldRepoSetUnionBodyShapes pins FoldRepo against the generic driver and
// the reference fold on both shapes a `set-union` field consumes: a string, or
// an array of strings (spec/fold.md §5.3).
//
// The array form was a divergence. FoldRepo read `remote` with a bare
// `.(string)` assertion, so `{"remote":["origin","upstream"]}` — which the
// uninterpretability predicate accepts and which both other implementations
// fold to that pair — produced no remotes at all, and no quarantine record
// either. Nothing in the log said so: §7.1's whole point is that an operation
// either folds or is reported, and this one did neither. It reached the public
// API and, through it, the SQLite projection.
func TestFoldRepoSetUnionBodyShapes(t *testing.T) {
	repoID := "b2c3d4e5f60718293a4b5c6d7e8f9012"
	remoteOp := func(id, parent, body string, when int64) codec.Op {
		op := codec.Op{
			Envelope: codec.Envelope{
				ObjectID:   repoID,
				ObjectType: "repo",
				OpType:     "add-remote",
				OpVersion:  1,
				Body:       json.RawMessage(body),
			},
			ID:     id,
			Author: codec.Identity{When: time.Unix(when, 0).UTC()},
		}
		if parent != "" {
			op.Parents = []string{parent}
		}
		return op
	}

	ops := []codec.Op{
		remoteOp("m1", "", `{"remote":["origin","upstream"]}`, 100),
		remoteOp("m2", "m1", `{"remote":"fork"}`, 200),
		remoteOp("m3", "m2", `{"remote":["",  "kept"]}`, 300),
		remoteOp("m4", "m3", `{"remote":["never",7]}`, 400),
	}

	repoState, err := s.FoldRepo(ops)
	if err != nil {
		t.Fatalf("FoldRepo failed: %v", err)
	}

	// m4 is uninterpretable, so its well-formed `never` dies with the op.
	// The empty string is not an element (§5.3).
	want := []string{"fork", "kept", "origin", "upstream"}
	if !reflect.DeepEqual(repoState.Remotes, want) {
		t.Errorf("Remotes = %v, want %v", repoState.Remotes, want)
	}

	var quarantined []string
	for _, u := range repoState.UnknownOps {
		quarantined = append(quarantined, u.Commit)
	}
	if !reflect.DeepEqual(quarantined, []string{"m4"}) {
		t.Errorf("quarantined = %v, want [m4]", quarantined)
	}

	// The generic driver must reach the same set through the same rules, and
	// quarantine the same operation.
	generic, err := s.Fold(ops, s.RepoRules())
	if err != nil {
		t.Fatalf("Fold failed: %v", err)
	}
	if !reflect.DeepEqual(generic.State["remote"], want) {
		t.Errorf("generic remote = %v, want %v", generic.State["remote"], want)
	}
	var genericQuarantined []string
	for _, u := range generic.UnknownOps {
		genericQuarantined = append(genericQuarantined, u.Commit)
	}
	if !reflect.DeepEqual(genericQuarantined, quarantined) {
		t.Errorf("generic quarantined = %v, want %v", genericQuarantined, quarantined)
	}
}
