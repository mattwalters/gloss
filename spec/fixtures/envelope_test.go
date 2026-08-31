package fixtures_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/spec"
	"github.com/writtendev/writ/spec/fixtures"
)

const envelopeSchemaPath = "schemas/op-envelope.schema.json"

var envelopeSchemaOnce = sync.OnceValue(func() *jsonschema.Schema {
	raw, err := spec.FS.ReadFile(envelopeSchemaPath)
	if err != nil {
		panic(fmt.Sprintf("read envelope schema: %v", err))
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		panic(fmt.Sprintf("unmarshal envelope schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(envelopeSchemaPath, doc); err != nil {
		panic(fmt.Sprintf("add schema resource: %v", err))
	}
	sch, err := c.Compile(envelopeSchemaPath)
	if err != nil {
		panic(fmt.Sprintf("compile envelope schema: %v", err))
	}
	return sch
})

// TestEnvelopeFamily registers the envelope fixture family and runs all
// envelope-*.yaml descriptions through the golden test harness.
func TestEnvelopeFamily(t *testing.T) {
	fixtures.Run(t, fixtures.Family{
		Name:      "envelope",
		GoldenDir: "testdata/golden/envelope",
		Filter: func(desc *fixtures.Description) bool {
			return strings.HasPrefix(desc.Name, "envelope-")
		},
		Runner: runEnvelopeFixture,
	})
}

type EnvelopeGolden struct {
	Ops []OpGoldenState `json:"ops"`
}

type OpGoldenState struct {
	Ref                     string           `json:"ref"`
	Commit                  string           `json:"commit"`
	Parents                 []string         `json:"parents"`
	Author                  string           `json:"author"`
	Committer               string           `json:"committer"`
	Timestamp               string           `json:"timestamp"`
	TreeEntries             []TreeEntryState `json:"tree_entries"`
	Payload                 string           `json:"payload,omitempty"`
	CanonicalPayload        string           `json:"canonical_payload,omitempty"`
	Signed                  bool             `json:"signed"`
	SignatureKeyFingerprint string           `json:"signature_key_fingerprint,omitempty"`
	VerificationOutcome     string           `json:"verification_outcome"`
	Expected                DispositionState `json:"expected"`
	Observed                DispositionState `json:"observed"`
}

type TreeEntryState struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
	SHA  string `json:"sha"`
}

type DispositionState struct {
	Disposition string `json:"disposition"`
	Reason      string `json:"reason,omitempty"`
}

func runEnvelopeFixture(t *testing.T, fix *fixtures.Fixture) ([]byte, error) {
	t.Helper()

	verifier, err := fixtures.NewVerifier()
	if err != nil {
		return nil, fmt.Errorf("create verifier: %w", err)
	}
	defer verifier.Close()

	sch := envelopeSchemaOnce()

	var golden EnvelopeGolden

	// Map manifest generation commits to descriptions
	commitDescMap := make(map[string]fixtures.CommitDesc)
	genStateMap := make(map[string]fixtures.GenerationState)
	for _, gs := range fix.Manifest.Generations {
		genStateMap[gs.Ref] = gs
	}

	commitIdx := 0
	for _, ref := range fix.Description.Refs {
		for _, gen := range ref.History {
			gs := fix.Manifest.Generations[commitIdx]
			commitIdx++
			for ci, cd := range gen.Commits {
				cState := gs.Commits[ci]
				commitDescMap[cState.SHA] = cd

				commitHash := plumbing.NewHash(cState.SHA)
				commit, err := fix.Repo.CommitObject(commitHash)
				if err != nil {
					return nil, fmt.Errorf("lookup commit %s: %w", cState.SHA, err)
				}

				opState, err := evaluateOpCommit(t, fix, ref.Name, commit, cd, verifier, sch)
				if err != nil {
					return nil, err
				}
				golden.Ops = append(golden.Ops, *opState)
			}
		}
	}

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal envelope golden: %w", err)
	}
	return append(b, '\n'), nil
}

func evaluateOpCommit(t *testing.T, fix *fixtures.Fixture, refName string, commit *object.Commit, cd fixtures.CommitDesc, verifier *fixtures.Verifier, sch *jsonschema.Schema) (*OpGoldenState, error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("commit tree: %w", err)
	}

	var treeEntries []TreeEntryState
	var opBlobContent string
	var opJsonFound bool
	var opJsonInSubdir bool
	var invalidMode bool

	for _, entry := range tree.Entries {
		treeEntries = append(treeEntries, TreeEntryState{
			Name: entry.Name,
			Mode: entry.Mode.String(),
			SHA:  entry.Hash.String(),
		})
		if entry.Name == "op.json" {
			opJsonFound = true
			if entry.Mode != filemode.Regular {
				invalidMode = true
			}
			blob, err := tree.TreeEntryFile(&entry)
			if err == nil {
				r, err := blob.Reader()
				if err == nil {
					content, _ := io.ReadAll(r)
					r.Close()
					opBlobContent = string(content)
				}
			}
		}
		if entry.Mode == filemode.Dir {
			subTree, err := fix.Repo.TreeObject(entry.Hash)
			if err == nil {
				for _, subEntry := range subTree.Entries {
					if subEntry.Name == "op.json" {
						opJsonInSubdir = true
					}
				}
			}
		}
	}

	// 1. Expected disposition
	expected := DispositionState{
		Disposition: "accept",
	}
	if cd.Expect != nil && !cd.Expect.Accept && cd.Expect.Reject != "" {
		expected = DispositionState{
			Disposition: "reject",
			Reason:      cd.Expect.Reject,
		}
	}

	// 2. Observed disposition evaluation
	var observed DispositionState

	// A. Tree validation
	if !opJsonFound && opJsonInSubdir {
		observed = DispositionState{Disposition: "reject", Reason: "op-json-subdirectory"}
	} else if !opJsonFound {
		observed = DispositionState{Disposition: "reject", Reason: "missing-op-json"}
	} else if len(tree.Entries) > 1 {
		observed = DispositionState{Disposition: "reject", Reason: "extra-tree-entry"}
	} else if invalidMode {
		observed = DispositionState{Disposition: "reject", Reason: "invalid-op-json-mode"}
	}

	// B. Committer validation
	if observed.Disposition == "" {
		if commit.Committer.Name != commit.Author.Name ||
			commit.Committer.Email != commit.Author.Email ||
			!commit.Committer.When.Equal(commit.Author.When) {
			observed = DispositionState{Disposition: "reject", Reason: "committer-mismatch"}
		}
	}

	// C. Payload validation
	var canonicalPayload string
	if observed.Disposition == "" {
		canonBytes, canonErr := canonicaljson.Marshal([]byte(opBlobContent))
		if canonErr != nil {
			reason := "non-canonical-payload"
			if strings.Contains(canonErr.Error(), "duplicate") {
				reason = "duplicate-key"
			} else if strings.Contains(canonErr.Error(), "lone surrogate") {
				reason = "lone-surrogate"
			}
			observed = DispositionState{Disposition: "reject", Reason: reason}
		} else {
			canonicalPayload = string(canonBytes)
			if !bytes.Equal(canonBytes, []byte(opBlobContent)) {
				observed = DispositionState{Disposition: "reject", Reason: "non-canonical-payload"}
			} else {
				// Validate against schema
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(opBlobContent)))
				if err != nil {
					observed = DispositionState{Disposition: "reject", Reason: "schema-violation"}
				} else if err := sch.Validate(inst); err != nil {
					observed = DispositionState{Disposition: "reject", Reason: "schema-violation"}
				}
			}
		}
	} else if opBlobContent != "" {
		canonBytes, canonErr := canonicaljson.Marshal([]byte(opBlobContent))
		if canonErr == nil {
			canonicalPayload = string(canonBytes)
		}
	}

	// D. Signature verification
	verResult, err := verifier.VerifyCommit(commit, cd.Author, commit.Author.Email)
	if err != nil {
		return nil, fmt.Errorf("verify commit signature: %w", err)
	}

	if observed.Disposition == "" {
		if !verResult.Valid {
			observed = DispositionState{Disposition: "reject", Reason: verResult.Outcome}
		} else {
			observed = DispositionState{Disposition: "accept"}
		}
	}

	// Assert declared expectation matches observed disposition
	if expected != observed {
		t.Fatalf("fixture %s commit %s: expected disposition %+v, observed %+v (verification: %+v)",
			fix.Name, commit.Hash.String(), expected, observed, verResult)
	}

	parents := make([]string, len(commit.ParentHashes))
	for i, p := range commit.ParentHashes {
		parents[i] = p.String()
	}

	return &OpGoldenState{
		Ref:                     refName,
		Commit:                  commit.Hash.String(),
		Parents:                 parents,
		Author:                  fmt.Sprintf("%s <%s>", commit.Author.Name, commit.Author.Email),
		Committer:               fmt.Sprintf("%s <%s>", commit.Committer.Name, commit.Committer.Email),
		Timestamp:               commit.Author.When.UTC().Format("2006-01-02T15:04:05Z07:00"),
		TreeEntries:             treeEntries,
		Payload:                 opBlobContent,
		CanonicalPayload:        canonicalPayload,
		Signed:                  commit.PGPSignature != "",
		SignatureKeyFingerprint: verResult.KeyFingerprint,
		VerificationOutcome:     verResult.Outcome,
		Expected:                expected,
		Observed:                observed,
	}, nil
}
