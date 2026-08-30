package spec_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/spec"
)

const schemaPath = "schemas/op-envelope.schema.json"

// compileEnvelopeSchema compiles the committed envelope schema. The
// compiler validates the schema document against its declared
// meta-schema as part of compilation, so a schema that is itself invalid
// draft 2020-12 fails here.
func compileEnvelopeSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := spec.FS.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("reading %s: %v", schemaPath, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parsing %s: %v", schemaPath, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaPath, doc); err != nil {
		t.Fatalf("adding schema resource: %v", err)
	}
	sch, err := c.Compile(schemaPath)
	if err != nil {
		t.Fatalf("compiling %s: %v", schemaPath, err)
	}
	return sch
}

func TestSchemaDeclaresDraft2020(t *testing.T) {
	raw, err := spec.FS.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("reading %s: %v", schemaPath, err)
	}
	var doc struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing %s: %v", schemaPath, err)
	}
	const want = "https://json-schema.org/draft/2020-12/schema"
	if doc.Schema != want {
		t.Errorf("$schema = %q, want %q", doc.Schema, want)
	}
	compileEnvelopeSchema(t) // meta-schema validation happens in Compile
}

// Every valid envelope instance must validate against the schema AND be
// byte-identical to the canonical encoding of its own content — the
// byte-equality rule from spec/op-envelope.md.
func TestValidEnvelopes(t *testing.T) {
	sch := compileEnvelopeSchema(t)
	entries, err := spec.FS.ReadDir("testdata/envelopes/valid")
	if err != nil {
		t.Fatalf("listing valid envelopes: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no valid envelope instances")
	}
	for _, e := range entries {
		t.Run(e.Name(), func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/envelopes/valid/" + e.Name())
			if err != nil {
				t.Fatalf("reading instance: %v", err)
			}
			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parsing instance: %v", err)
			}
			if err := sch.Validate(inst); err != nil {
				t.Errorf("schema validation failed: %v", err)
			}
			canon, err := canonicaljson.Marshal(raw)
			if err != nil {
				t.Fatalf("canonicalizing: %v", err)
			}
			if !bytes.Equal(canon, raw) {
				t.Errorf("instance is not canonical:\n file: %q\ncanon: %q", raw, canon)
			}
		})
	}
}

type invalidEntry struct {
	File    string `json:"file"`
	Rejects string `json:"rejects"`
	Reason  string `json:"reason"`
}

// Every invalid envelope instance must be rejected by the check its
// index entry records: "schema" means schema validation fails on the
// parsed value; "canonicalization" means the file's bytes are not a
// valid canonical encoding (Marshal errors, or returns different bytes).
// The index and the directory must also agree exactly, so an instance
// can't be added without recording why it is invalid.
func TestInvalidEnvelopes(t *testing.T) {
	sch := compileEnvelopeSchema(t)

	rawIndex, err := spec.FS.ReadFile("testdata/envelopes/invalid/index.json")
	if err != nil {
		t.Fatalf("reading index: %v", err)
	}
	var index []invalidEntry
	if err := json.Unmarshal(rawIndex, &index); err != nil {
		t.Fatalf("parsing index: %v", err)
	}

	indexed := map[string]bool{}
	for _, entry := range index {
		indexed[entry.File] = true
	}
	entries, err := spec.FS.ReadDir("testdata/envelopes/invalid")
	if err != nil {
		t.Fatalf("listing invalid envelopes: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "index.json" {
			continue
		}
		if !indexed[e.Name()] {
			t.Errorf("instance %s is not listed in index.json", e.Name())
		}
	}

	for _, entry := range index {
		t.Run(entry.File, func(t *testing.T) {
			if entry.Reason == "" {
				t.Error("index entry has no reason")
			}
			raw, err := spec.FS.ReadFile("testdata/envelopes/invalid/" + entry.File)
			if err != nil {
				t.Fatalf("reading instance: %v", err)
			}
			switch entry.Rejects {
			case "schema":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance (a schema-rejected instance must be parseable JSON): %v", err)
				}
				if err := sch.Validate(inst); err == nil {
					t.Errorf("schema accepted the instance; expected rejection: %s", entry.Reason)
				}
			case "canonicalization":
				canon, err := canonicaljson.Marshal(raw)
				if err == nil && bytes.Equal(canon, raw) {
					t.Errorf("bytes are canonical; expected rejection: %s", entry.Reason)
				}
			default:
				t.Errorf("index entry has unknown rejects value %q", entry.Rejects)
			}
		})
	}
}

// The canonicalization vector file must be well-formed: unique names,
// and exactly one of canonical/error per vector. The byte-level behavior
// of each vector is enforced in engine/codec/canonicaljson's tests,
// which read this same embedded copy.
func TestCanonicalizationVectorsWellFormed(t *testing.T) {
	raw, err := spec.FS.ReadFile("testdata/canonicalization/vectors.json")
	if err != nil {
		t.Fatalf("reading vectors: %v", err)
	}
	var vecs []struct {
		Name      string  `json:"name"`
		Input     *string `json:"input"`
		Canonical *string `json:"canonical"`
		Error     string  `json:"error"`
	}
	if err := json.Unmarshal(raw, &vecs); err != nil {
		t.Fatalf("parsing vectors: %v", err)
	}
	if len(vecs) == 0 {
		t.Fatal("no vectors")
	}
	seen := map[string]bool{}
	for i, v := range vecs {
		name := v.Name
		if name == "" {
			name = fmt.Sprintf("vector %d", i)
			t.Errorf("%s has no name", name)
		}
		if seen[name] {
			t.Errorf("duplicate vector name %q", name)
		}
		seen[name] = true
		if v.Input == nil {
			t.Errorf("%s has no input", name)
		}
		if (v.Canonical == nil) == (v.Error == "") {
			t.Errorf("%s must have exactly one of canonical or error", name)
		}
	}
}
