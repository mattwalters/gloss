package resolve_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/engine/resolve"
	"github.com/writtendev/writ/spec"
)

func detectHashAlgo(anchorRaw []byte) resolve.HashAlgo {
	var a struct {
		Old *struct {
			Commit string `json:"commit"`
			Blob   string `json:"blob"`
		} `json:"old,omitempty"`
		New *struct {
			Commit string `json:"commit"`
			Blob   string `json:"blob"`
		} `json:"new,omitempty"`
	}
	if err := json.Unmarshal(anchorRaw, &a); err == nil {
		if a.New != nil {
			if len(a.New.Commit) == 64 || len(a.New.Blob) == 64 {
				return resolve.SHA256
			}
		}
		if a.Old != nil {
			if len(a.Old.Commit) == 64 || len(a.Old.Blob) == 64 {
				return resolve.SHA256
			}
		}
	}
	return resolve.SHA1
}

func TestConformanceVectors(t *testing.T) {
	cases, err := spec.ResolutionVectors()
	if err != nil {
		t.Fatalf("loading resolution vectors: %v", err)
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			anchor, err := resolve.ParseAnchor(c.Anchor)
			if err != nil {
				t.Fatalf("ParseAnchor: %v", err)
			}

			files := make(map[string][]byte, len(c.Target.Files))
			for p, content := range c.Target.Files {
				files[p] = []byte(content)
			}

			algo := detectHashAlgo(c.Anchor)
			tree := resolve.NewTree(files, algo)

			outcome := resolve.Resolve(anchor, tree)

			outcomeJSON, err := json.Marshal(outcome)
			if err != nil {
				t.Fatalf("marshaling outcome: %v", err)
			}

			actualCanon, err := canonicaljson.Marshal(outcomeJSON)
			if err != nil {
				t.Fatalf("canonicalizing actual outcome: %v", err)
			}

			expectCanon, err := canonicaljson.Marshal(c.Expect)
			if err != nil {
				t.Fatalf("canonicalizing expect outcome: %v", err)
			}

			if !bytes.Equal(actualCanon, expectCanon) {
				t.Errorf("%s mismatch:\nactual:\n%s\nexpect:\n%s", c.Name, string(actualCanon), string(expectCanon))
			}

			// Assert outcome's anchor canonicalizes equal to input anchor (orphaned but preserved)
			outcomeAnchorJSON, err := json.Marshal(outcome.Anchor)
			if err != nil {
				t.Fatalf("marshaling outcome anchor: %v", err)
			}
			outcomeAnchorCanon, err := canonicaljson.Marshal(outcomeAnchorJSON)
			if err != nil {
				t.Fatalf("canonicalizing outcome anchor: %v", err)
			}

			inputAnchorCanon, err := canonicaljson.Marshal(c.Anchor)
			if err != nil {
				t.Fatalf("canonicalizing input anchor: %v", err)
			}

			if !bytes.Equal(outcomeAnchorCanon, inputAnchorCanon) {
				t.Errorf("%s anchor preservation mismatch:\nactual anchor:\n%s\ninput anchor:\n%s", c.Name, string(outcomeAnchorCanon), string(inputAnchorCanon))
			}
		})
	}
}
