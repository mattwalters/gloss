package spec_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/spec"
)

// envelopeSchemaID is declared in review_ops_test.go; anchorSchemaID in
// anchors_test.go.
const commentSchemaID = "https://writ.dev/spec/comment.schema.json"

var commentSchemaOnce = sync.OnceValue(func() compiledSchema {
	c := jsonschema.NewCompiler()

	// Register envelope schema dependency.
	envRaw, err := spec.FS.ReadFile("schemas/op-envelope.schema.json")
	if err != nil {
		return compiledSchema{err: fmt.Errorf("reading envelope schema: %w", err)}
	}
	envDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(envRaw))
	if err != nil {
		return compiledSchema{err: fmt.Errorf("unmarshaling envelope schema: %w", err)}
	}
	if err := c.AddResource(envelopeSchemaID, envDoc); err != nil {
		return compiledSchema{err: fmt.Errorf("adding envelope schema resource: %w", err)}
	}

	// Register anchor schema dependency.
	ancRaw, err := spec.FS.ReadFile("schemas/anchor.schema.json")
	if err != nil {
		return compiledSchema{err: fmt.Errorf("reading anchor schema: %w", err)}
	}
	ancDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(ancRaw))
	if err != nil {
		return compiledSchema{err: fmt.Errorf("unmarshaling anchor schema: %w", err)}
	}
	if err := c.AddResource(anchorSchemaID, ancDoc); err != nil {
		return compiledSchema{err: fmt.Errorf("adding anchor schema resource: %w", err)}
	}

	// Register and compile comment schema.
	raw, err := spec.FS.ReadFile("schemas/comment.schema.json")
	if err != nil {
		return compiledSchema{err: fmt.Errorf("reading comment schema: %w", err)}
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return compiledSchema{err: fmt.Errorf("unmarshaling comment schema: %w", err)}
	}
	if err := c.AddResource(commentSchemaID, doc); err != nil {
		return compiledSchema{err: fmt.Errorf("adding comment schema resource: %w", err)}
	}

	sch, err := c.Compile(commentSchemaID)
	if err != nil {
		return compiledSchema{err: fmt.Errorf("compiling comment schema: %w", err)}
	}

	return compiledSchema{sch: sch, raw: raw}
})

func compileCommentSchema(t *testing.T) (*jsonschema.Schema, []byte) {
	t.Helper()
	c := commentSchemaOnce()
	if c.err != nil {
		t.Fatalf("compiling comment schema: %v", c.err)
	}
	return c.sch, c.raw
}

// commentInvariants enforces cross-field and delegation invariants for
// comment payloads that JSON Schema cannot express.
func commentInvariants(c map[string]any) error {
	objID, _ := c["object_id"].(string)
	body, ok := c["body"].(map[string]any)
	if !ok {
		return nil
	}
	if inReplyTo, ok := body["in_reply_to"].(string); ok {
		if inReplyTo == objID && objID != "" {
			return fmt.Errorf("comment %q cannot reply to itself", objID)
		}
	}
	if subj, ok := body["subject"].(map[string]any); ok {
		subjID, _ := subj["object_id"].(string)
		subjType, _ := subj["object_type"].(string)
		if subjType == "comment" && subjID == objID && objID != "" {
			return fmt.Errorf("comment %q cannot have itself as subject", objID)
		}
	}
	if anchor, ok := body["anchor"].(map[string]any); ok {
		if err := anchorInvariants(anchor); err != nil {
			return fmt.Errorf("anchor invariant: %w", err)
		}
	}
	return nil
}

func validateCommentVector(t *testing.T, sch *jsonschema.Schema, raw []byte) error {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("unmarshal json: %w", err)
	}
	if err := sch.Validate(inst); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	var c map[string]any
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("unmarshal map: %w", err)
	}
	if err := commentInvariants(c); err != nil {
		return fmt.Errorf("invariant: %w", err)
	}
	return nil
}

func TestCommentSchemaCompiles(t *testing.T) {
	_, raw := compileCommentSchema(t)
	var doc struct {
		Schema string `json:"$schema"`
		ID     string `json:"$id"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing schema: %v", err)
	}
	const wantSchema = "https://json-schema.org/draft/2020-12/schema"
	if doc.Schema != wantSchema {
		t.Errorf("$schema = %q, want %q", doc.Schema, wantSchema)
	}
	if doc.ID != commentSchemaID {
		t.Errorf("$id = %q, want %q", doc.ID, commentSchemaID)
	}
}

func TestValidCommentVectors(t *testing.T) {
	sch, _ := compileCommentSchema(t)
	names := readDirNames(t, "testdata/comments/valid")
	if len(names) == 0 {
		t.Fatal("no valid comment vectors found")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/comments/valid/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateCommentVector(t, sch, raw); err != nil {
				t.Errorf("valid vector rejected: %v", err)
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

func TestInvalidCommentVectors(t *testing.T) {
	sch, _ := compileCommentSchema(t)

	rawIndex, err := spec.FS.ReadFile("testdata/comments/invalid/index.json")
	if err != nil {
		t.Fatal(err)
	}
	var index map[string]struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rawIndex, &index); err != nil {
		t.Fatalf("decoding index.json: %v", err)
	}

	names := readDirNames(t, "testdata/comments/invalid")
	files := make(map[string]bool)
	for _, name := range names {
		if name != "index.json" {
			files[name] = true
		}
	}
	for name := range index {
		if !files[name] {
			t.Errorf("index.json lists %s but the file does not exist", name)
		}
	}

	for name := range files {
		entry, ok := index[name]
		if !ok {
			t.Errorf("%s has no index.json entry recording its expected rejection", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/comments/invalid/" + name)
			if err != nil {
				t.Fatal(err)
			}
			switch entry.Kind {
			case "schema":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance (a schema-rejected instance must be parseable JSON): %v", err)
				}
				if err := sch.Validate(inst); err == nil {
					t.Errorf("schema accepted the instance; expected rejection: %s", entry.Reason)
				}
			case "invariant":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance: %v", err)
				}
				if err := sch.Validate(inst); err != nil {
					t.Errorf("schema rejected an invariant-kind vector (%v); expected only invariant to fail: %s", err, entry.Reason)
				}
				var c map[string]any
				if err := json.Unmarshal(raw, &c); err != nil {
					t.Fatal(err)
				}
				if err := commentInvariants(c); err == nil {
					t.Errorf("invariants accepted it; expected rejection: %s", entry.Reason)
				}
			case "canonicalization":
				canon, err := canonicaljson.Marshal(raw)
				if err == nil && bytes.Equal(canon, raw) {
					t.Errorf("bytes are canonical; expected rejection: %s", entry.Reason)
				}
			default:
				t.Errorf("index.json kind %q unknown (want schema, invariant, or canonicalization)", entry.Kind)
			}
		})
	}
}

// TestThreadWalking demonstrates DoD item 1: threading is representable
// without mutable state, walking immutable in_reply_to edges from leaf to
// thread root.
func TestThreadWalking(t *testing.T) {
	sch, _ := compileCommentSchema(t)

	threadFiles := []string{
		"testdata/comments/valid/thread-root.json",
		"testdata/comments/valid/thread-reply-1.json",
		"testdata/comments/valid/thread-reply-2.json",
	}

	type commentOp struct {
		ObjectID string `json:"object_id"`
		Body     struct {
			InReplyTo string `json:"in_reply_to"`
			Text      string `json:"text"`
		} `json:"body"`
	}

	commentsByID := make(map[string]commentOp)

	for _, file := range threadFiles {
		raw, err := spec.FS.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		if err := validateCommentVector(t, sch, raw); err != nil {
			t.Fatalf("validating %s: %v", file, err)
		}
		var op commentOp
		if err := json.Unmarshal(raw, &op); err != nil {
			t.Fatalf("unmarshaling %s: %v", file, err)
		}
		commentsByID[op.ObjectID] = op
	}

	// Walk from leaf (depth 2) to root (depth 0).
	currentID := "c-t3"
	var chain []string
	for currentID != "" {
		chain = append(chain, currentID)
		op, ok := commentsByID[currentID]
		if !ok {
			t.Fatalf("broken reply edge to %s", currentID)
		}
		currentID = op.Body.InReplyTo
	}

	wantChain := []string{"c-t3", "c-t2", "c-t1"}
	if len(chain) != len(wantChain) {
		t.Fatalf("chain length = %d, want %d", len(chain), len(wantChain))
	}
	for i, id := range chain {
		if id != wantChain[i] {
			t.Errorf("chain[%d] = %q, want %q", i, id, wantChain[i])
		}
	}
}

// TestGitHubCommentVectors checks the informative conversion vectors under
// testdata/comments/github/ (DoD item 4).
func TestGitHubCommentVectors(t *testing.T) {
	sch, _ := compileCommentSchema(t)
	names := readDirNames(t, "testdata/comments/github")
	if len(names) == 0 {
		t.Fatal("no GitHub comment vectors found")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/comments/github/" + name)
			if err != nil {
				t.Fatal(err)
			}
			var vec struct {
				GitHub  json.RawMessage `json:"github"`
				Comment json.RawMessage `json:"comment"`
			}
			if err := json.Unmarshal(raw, &vec); err != nil {
				t.Fatalf("decoding vector: %v", err)
			}
			if len(vec.GitHub) == 0 {
				t.Fatal("vector missing github field")
			}
			if len(vec.Comment) == 0 {
				t.Fatal("vector missing comment field")
			}
			if err := validateCommentVector(t, sch, vec.Comment); err != nil {
				t.Errorf("comment payload rejected: %v", err)
			}
		})
	}
}

func TestCommentFieldRules(t *testing.T) {
	rawRules, err := spec.FS.ReadFile("testdata/comments/field-rules.json")
	if err != nil {
		t.Fatalf("reading comments/field-rules.json: %v", err)
	}

	var rules []spec.FieldRule
	if err := json.Unmarshal(rawRules, &rules); err != nil {
		t.Fatalf("decoding comments/field-rules.json: %v", err)
	}

	ruleMap := make(map[string]spec.FieldRule)
	for _, r := range rules {
		if r.OpType == "" || r.OpVersion < 1 || r.Field == "" {
			t.Errorf("invalid rule entry: %+v", r)
		}
		if !spec.KnownCatalogueStrategies[r.Strategy] {
			t.Errorf("strategy %q for (%s, %d, %s) is not in the closed catalogue", r.Strategy, r.OpType, r.OpVersion, r.Field)
		}
		key := fmt.Sprintf("%s:%d:%s", r.OpType, r.OpVersion, r.Field)
		if _, exists := ruleMap[key]; exists {
			t.Errorf("duplicate rule for %s", key)
		}
		ruleMap[key] = r
	}

	// Verify that all properties in comment schema have matching field rules.
	expectedFields := map[string][]string{
		"create": {"subject", "text", "in_reply_to", "anchor"},
		"edit":   {"text"},
		"delete": {"deleted"},
	}

	for opType, fields := range expectedFields {
		for _, field := range fields {
			key := fmt.Sprintf("%s:1:%s", opType, field)
			if _, ok := ruleMap[key]; !ok {
				t.Errorf("missing field rule for (%s, op_version: 1, field: %s)", opType, field)
			}
		}
	}

	allRules, err := spec.FieldRules()
	if err != nil {
		t.Fatalf("spec.FieldRules() failed: %v", err)
	}
	if len(allRules) == 0 {
		t.Fatal("spec.FieldRules() returned no rules")
	}
}

