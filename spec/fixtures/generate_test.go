package fixtures

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func requireSSHKeygen(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not on PATH; fixture generation needs it to sign commits")
	}
}

// TestCorpusMatchesGolden regenerates every checked-in fixture description
// and compares its manifest against the committed golden file. This is
// the check that CI runs: a mismatch means either the generator changed
// behavior or a description changed without its golden being updated
// (via `go run ./spec/fixtures/gen -update-golden`).
func TestCorpusMatchesGolden(t *testing.T) {
	requireSSHKeygen(t)

	descs, err := LoadCorpus()
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(descs) == 0 {
		t.Fatal("no fixture descriptions found")
	}

	for _, desc := range descs {
		desc := desc
		t.Run(desc.Name, func(t *testing.T) {
			manifest, err := Generate(desc, filepath.Join(t.TempDir(), "repo"))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			got := marshalManifest(t, manifest)

			goldenPath := filepath.Join("testdata", "golden", desc.Name+".json")
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v (run `go run ./spec/fixtures/gen -update-golden` if this fixture is new)", goldenPath, err)
			}
			if string(got) != string(want) {
				t.Errorf("manifest for %s does not match %s\n\ngot:\n%s\n\nwant:\n%s", desc.Name, goldenPath, got, want)
			}
		})
	}
}

// TestGenerationIsDeterministic builds each description twice, into
// separate directories, and checks the two manifests are byte-identical.
// This is the direct test of the DoD's determinism requirement,
// independent of whether a golden file happens to be present or current.
func TestGenerationIsDeterministic(t *testing.T) {
	requireSSHKeygen(t)

	descs, err := LoadCorpus()
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}

	for _, desc := range descs {
		desc := desc
		t.Run(desc.Name, func(t *testing.T) {
			m1, err := Generate(desc, filepath.Join(t.TempDir(), "repo"))
			if err != nil {
				t.Fatalf("Generate (1st): %v", err)
			}
			m2, err := Generate(desc, filepath.Join(t.TempDir(), "repo"))
			if err != nil {
				t.Fatalf("Generate (2nd): %v", err)
			}
			b1, b2 := marshalManifest(t, m1), marshalManifest(t, m2)
			if string(b1) != string(b2) {
				t.Errorf("two generations of %s disagree:\n\nfirst:\n%s\n\nsecond:\n%s", desc.Name, b1, b2)
			}
		})
	}
}

// TestForcePushedBranchOrphansCommits checks the force-pushed-branch
// fixture actually expresses what its description claims: the ref's
// final tip does not descend from the kept-alive pre-rewrite generation,
// and the pre-rewrite commit is still present as a real, readable object
// under its keep_as ref rather than lost.
func TestForcePushedBranchOrphansCommits(t *testing.T) {
	requireSSHKeygen(t)

	descs, err := LoadCorpus()
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	var desc *Description
	for _, d := range descs {
		if d.Name == "force-pushed-branch" {
			desc = d
		}
	}
	if desc == nil {
		t.Fatal("force-pushed-branch fixture not found")
	}

	repoDir := filepath.Join(t.TempDir(), "repo")
	manifest, err := Generate(desc, repoDir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var mainTip, preRewrite string
	for _, r := range manifest.Refs {
		switch r.Name {
		case "refs/heads/main":
			mainTip = r.Commit
		case "refs/fixture-history/main/gen-0":
			preRewrite = r.Commit
		}
	}
	if mainTip == "" || preRewrite == "" {
		t.Fatalf("expected both refs in manifest, got %+v", manifest.Refs)
	}
	if mainTip == preRewrite {
		t.Fatalf("main tip and pre-rewrite tip are the same commit; fixture doesn't express a rewrite")
	}

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}

	tipCommit, err := repo.CommitObject(plumbing.NewHash(mainTip))
	if err != nil {
		t.Fatalf("CommitObject(main tip): %v", err)
	}
	ancestor, err := isAncestorOf(tipCommit, plumbing.NewHash(preRewrite))
	if err != nil {
		t.Fatalf("ancestry check: %v", err)
	}
	if ancestor {
		t.Fatal("pre-rewrite commit is still an ancestor of main's tip; force-push wasn't actually a rewrite")
	}

	if _, err := repo.CommitObject(plumbing.NewHash(preRewrite)); err != nil {
		t.Fatalf("pre-rewrite commit %s is not readable from the object store: %v", preRewrite, err)
	}
}

// isAncestorOf reports whether candidate is reachable by walking tip's
// parents.
func isAncestorOf(tip *object.Commit, candidate plumbing.Hash) (bool, error) {
	found := false
	err := tip.Parents().ForEach(func(p *object.Commit) error {
		if found {
			return nil
		}
		if p.Hash == candidate {
			found = true
			return nil
		}
		ok, err := isAncestorOf(p, candidate)
		if err != nil {
			return err
		}
		found = ok
		return nil
	})
	return found, err
}

func marshalManifest(t *testing.T, m *Manifest) []byte {
	t.Helper()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return append(b, '\n')
}
