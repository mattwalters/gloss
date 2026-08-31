package spec_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/spec"
)

type readerProfile struct {
	Profile  string            `json:"profile"`
	KnownOps []knownOpCapacity `json:"known_ops"`
}

type knownOpCapacity struct {
	ObjectType string  `json:"object_type"`
	OpType     string  `json:"op_type"`
	Versions   []int64 `json:"versions"`
}

type forwardCompatEntry struct {
	Disposition string   `json:"disposition"`
	Rules       []string `json:"rules"`
	Reason      string   `json:"reason"`
}

func loadReaderProfile(t *testing.T) readerProfile {
	t.Helper()
	raw, err := spec.FS.ReadFile("testdata/forward-compat/reader-profile.json")
	if err != nil {
		t.Fatalf("reading reader profile: %v", err)
	}
	var p readerProfile
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decoding reader profile: %v", err)
	}
	if len(p.KnownOps) == 0 {
		t.Fatal("reader profile declares no known ops")
	}
	return p
}

func loadForwardCompatIndex(t *testing.T) map[string]forwardCompatEntry {
	t.Helper()
	raw, err := spec.FS.ReadFile("testdata/forward-compat/index.json")
	if err != nil {
		t.Fatalf("reading forward-compat index.json: %v", err)
	}
	var index map[string]forwardCompatEntry
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatalf("decoding forward-compat index.json: %v", err)
	}
	if len(index) == 0 {
		t.Fatal("forward-compat index is empty")
	}
	return index
}

// deriveDisposition deterministically computes whether an op payload is
// interpretable or opaque under a given reader profile.
func deriveDisposition(profile readerProfile, rawOp []byte) (string, error) {
	var env struct {
		ObjectType string `json:"object_type"`
		OpType     string `json:"op_type"`
		OpVersion  int64  `json:"op_version"`
	}
	if err := json.Unmarshal(rawOp, &env); err != nil {
		return "", fmt.Errorf("decoding envelope: %w", err)
	}
	for _, k := range profile.KnownOps {
		if k.ObjectType == env.ObjectType && k.OpType == env.OpType {
			for _, v := range k.Versions {
				if v == env.OpVersion {
					return "interpretable", nil
				}
			}
			return "opaque", nil
		}
	}
	return "opaque", nil
}

// TestForwardCompatConformances tests every instance in the forward-compat
// testdata corpus:
// 1. Every instance validates against op-envelope.schema.json (unknown is not invalid).
// 2. Every instance is byte-canonical and survives canonicaljson round-trip unmodified.
// 3. Derived disposition (from reader profile + payload) matches index.json.
// 4. Directory and index agree 1-to-1.
func TestForwardCompatConformances(t *testing.T) {
	sch, _ := envelopeSchema(t)
	profile := loadReaderProfile(t)
	index := loadForwardCompatIndex(t)

	entries, err := spec.FS.ReadDir("testdata/forward-compat/ops")
	if err != nil {
		t.Fatalf("reading testdata/forward-compat/ops directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no forward-compat op instances found")
	}

	filesInDir := make(map[string]bool)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			filesInDir[e.Name()] = true
		}
	}

	// 1-to-1 agreement between directory and index
	for name := range index {
		if !filesInDir[name] {
			t.Errorf("index.json lists %s but file does not exist in ops/", name)
		}
	}
	for name := range filesInDir {
		if _, ok := index[name]; !ok {
			t.Errorf("ops/%s exists but is not listed in index.json", name)
		}
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			entry, ok := index[name]
			if !ok {
				t.Fatalf("instance %s missing from index.json", name)
			}
			if entry.Reason == "" {
				t.Error("index entry has empty reason")
			}
			if len(entry.Rules) == 0 {
				t.Error("index entry cites no rule IDs")
			}
			if entry.Disposition != "interpretable" && entry.Disposition != "opaque" {
				t.Errorf("invalid disposition %q (want interpretable or opaque)", entry.Disposition)
			}

			path := "testdata/forward-compat/ops/" + name
			raw, err := spec.FS.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}

			// Schema validation (unknown is NOT invalid)
			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parsing JSON instance: %v", err)
			}
			if err := sch.Validate(inst); err != nil {
				t.Errorf("schema validation failed (unknown op must be valid envelope): %v", err)
			}

			// Codec leg: canonical JSON round-trip
			canon, err := canonicaljson.Marshal(raw)
			if err != nil {
				t.Fatalf("canonicalizing instance: %v", err)
			}
			if !bytes.Equal(canon, raw) {
				t.Errorf("instance bytes are not canonical:\n  file: %q\n canon: %q", raw, canon)
			}

			// Disposition derivation check
			derived, err := deriveDisposition(profile, raw)
			if err != nil {
				t.Fatalf("deriving disposition: %v", err)
			}
			if derived != entry.Disposition {
				t.Errorf("disposition mismatch for %s: derived %q, index declares %q (reason: %s)", name, derived, entry.Disposition, entry.Reason)
			}
		})
	}
}

// TestForwardCompatRuleCoverage parses spec/forward-compatibility.md to find all
// numbered rules (FC-1, FC-2, ...) and ensures every single rule ID is cited in
// index.json.
func TestForwardCompatRuleCoverage(t *testing.T) {
	index := loadForwardCompatIndex(t)

	// Read spec/forward-compatibility.md
	docPath := "forward-compatibility.md"
	rawDoc, err := os.ReadFile(docPath)
	if err != nil {
		// Try from repo root if running from parent directory
		docPath = filepath.Join("spec", "forward-compatibility.md")
		rawDoc, err = os.ReadFile(docPath)
		if err != nil {
			t.Fatalf("reading spec/forward-compatibility.md: %v", err)
		}
	}

	re := regexp.MustCompile(`\bFC-\d+\b`)
	matches := re.FindAllString(string(rawDoc), -1)
	if len(matches) == 0 {
		t.Fatal("no FC-* rule IDs found in spec/forward-compatibility.md")
	}

	definedRules := make(map[string]bool)
	for _, m := range matches {
		definedRules[m] = true
	}

	citedRules := make(map[string]bool)
	for _, entry := range index {
		for _, r := range entry.Rules {
			citedRules[r] = true
			if !definedRules[r] {
				t.Errorf("index.json cites rule %s which is not defined in spec/forward-compatibility.md", r)
			}
		}
	}

	var uncited []string
	for r := range definedRules {
		if !citedRules[r] {
			uncited = append(uncited, r)
		}
	}
	if len(uncited) > 0 {
		sort.Strings(uncited)
		t.Errorf("the following normative rule IDs are defined in spec/forward-compatibility.md but never cited in testdata/forward-compat/index.json: %v", uncited)
	}
}

// TestNegativeDispositionDerivation verifies that the derivation check is active
// and fails if an expected disposition is inverted.
func TestNegativeDispositionDerivation(t *testing.T) {
	profile := loadReaderProfile(t)
	raw, err := spec.FS.ReadFile("testdata/forward-compat/ops/future-op-version.json")
	if err != nil {
		t.Fatal(err)
	}
	derived, err := deriveDisposition(profile, raw)
	if err != nil {
		t.Fatal(err)
	}
	if derived == "interpretable" {
		t.Errorf("expected future-op-version to derive as opaque, got %q", derived)
	}
}
