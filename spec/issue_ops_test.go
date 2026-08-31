package spec_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/spec"
)

const (
	issueOpsSchemaID = "https://writ.dev/spec/issue-ops.schema.json"
)

// compileIssueOpsSchemas compiles the envelope, identifiers, and issue-ops schemas.
// Compilation also validates the documents against the draft 2020-12 meta-schema.
func compileIssueOpsSchemas(t *testing.T) (*jsonschema.Schema, *jsonschema.Schema) {
	t.Helper()
	envRaw, err := spec.FS.ReadFile("schemas/op-envelope.schema.json")
	if err != nil {
		t.Fatalf("reading envelope schema: %v", err)
	}
	envDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(envRaw))
	if err != nil {
		t.Fatalf("decoding envelope schema: %v", err)
	}

	idRaw, err := spec.FS.ReadFile("schemas/identifiers.schema.json")
	if err != nil {
		t.Fatalf("reading identifiers schema: %v", err)
	}
	idDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(idRaw))
	if err != nil {
		t.Fatalf("decoding identifiers schema: %v", err)
	}

	issRaw, err := spec.FS.ReadFile("schemas/issue-ops.schema.json")
	if err != nil {
		t.Fatalf("reading issue-ops schema: %v", err)
	}
	issDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(issRaw))
	if err != nil {
		t.Fatalf("decoding issue-ops schema: %v", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(envelopeSchemaID, envDoc); err != nil {
		t.Fatalf("adding envelope schema resource: %v", err)
	}
	if err := c.AddResource(identifiersSchemaID, idDoc); err != nil {
		t.Fatalf("adding identifiers schema resource: %v", err)
	}
	if err := c.AddResource(issueOpsSchemaID, issDoc); err != nil {
		t.Fatalf("adding issue-ops schema resource: %v", err)
	}

	envSch, err := c.Compile(envelopeSchemaID)
	if err != nil {
		t.Fatalf("compiling envelope schema: %v", err)
	}
	issSch, err := c.Compile(issueOpsSchemaID)
	if err != nil {
		t.Fatalf("compiling issue-ops schema: %v", err)
	}

	return envSch, issSch
}

func validateIssueOpVector(t *testing.T, envSch, issSch *jsonschema.Schema, raw []byte) error {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decoding vector: %v", err)
	}
	if err := envSch.Validate(inst); err != nil {
		return fmt.Errorf("envelope schema: %w", err)
	}
	if err := issSch.Validate(inst); err != nil {
		return fmt.Errorf("issue-ops schema: %w", err)
	}
	canon, err := canonicaljson.Marshal(raw)
	if err != nil {
		return fmt.Errorf("canonicalization: %w", err)
	}
	if !bytes.Equal(canon, raw) {
		return fmt.Errorf("instance is not byte-canonical:\n  raw: %q\ncanon: %q", raw, canon)
	}
	return nil
}

func TestIssueOpsSchemaCompiles(t *testing.T) {
	compileIssueOpsSchemas(t)
}

func TestValidIssueOpsVectors(t *testing.T) {
	envSch, issSch := compileIssueOpsSchemas(t)
	for _, name := range readDirNames(t, "testdata/issue-ops/valid") {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/issue-ops/valid/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateIssueOpVector(t, envSch, issSch, raw); err != nil {
				t.Errorf("valid vector rejected: %v", err)
			}
		})
	}
}

func TestInvalidIssueOpsVectors(t *testing.T) {
	_, issSch := compileIssueOpsSchemas(t)

	rawIndex, err := spec.FS.ReadFile("testdata/issue-ops/invalid/index.json")
	if err != nil {
		t.Fatal(err)
	}
	var index []struct {
		File     string `json:"file"`
		Rejects  string `json:"rejects"`
		Category string `json:"category,omitempty"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(rawIndex, &index); err != nil {
		t.Fatalf("decoding index.json: %v", err)
	}

	indexed := make(map[string]bool)
	for _, entry := range index {
		if indexed[entry.File] {
			t.Errorf("index.json lists %s more than once", entry.File)
		}
		indexed[entry.File] = true
	}

	names := readDirNames(t, "testdata/issue-ops/invalid")
	for _, name := range names {
		if name == "index.json" {
			continue
		}
		if !indexed[name] {
			t.Errorf("instance %s is not listed in index.json", name)
		}
	}

	for _, entry := range index {
		t.Run(entry.File, func(t *testing.T) {
			if entry.Reason == "" {
				t.Error("index entry has no reason")
			}
			raw, err := spec.FS.ReadFile("testdata/issue-ops/invalid/" + entry.File)
			if err != nil {
				t.Fatalf("reading instance: %v", err)
			}
			switch entry.Rejects {
			case "schema":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance (a schema-rejected instance must be parseable JSON): %v", err)
				}
				if err := issSch.Validate(inst); err == nil {
					t.Errorf("schema accepted the instance; expected rejection: %s", entry.Reason)
				}
			case "canonicalization":
				canon, err := canonicaljson.Marshal(raw)
				switch entry.Category {
				case "not-canonical":
					if err != nil {
						t.Fatalf("Marshal errored (%v); a not-canonical instance must canonicalize to different bytes", err)
					}
					if bytes.Equal(canon, raw) {
						t.Errorf("bytes are canonical; expected rejection: %s", entry.Reason)
					}
				case "duplicate-key", "lone-surrogate":
					if err == nil {
						t.Fatalf("Marshal accepted the instance; expected a %s rejection: %s", entry.Category, entry.Reason)
					}
					want := map[string]string{
						"duplicate-key":  "duplicate object key",
						"lone-surrogate": "lone surrogate",
					}[entry.Category]
					if !strings.Contains(err.Error(), want) {
						t.Errorf("Marshal rejected with %q, want a %s rejection (containing %q)", err, entry.Category, want)
					}
				default:
					t.Errorf("canonicalization entry has unknown category %q", entry.Category)
				}
			default:
				t.Errorf("index entry has unknown rejects value %q", entry.Rejects)
			}
		})
	}
}

func TestIssueFieldRules(t *testing.T) {
	rawRules, err := spec.FS.ReadFile("testdata/issue-ops/field-rules.json")
	if err != nil {
		t.Fatalf("reading field-rules.json: %v", err)
	}

	var rules []fieldRule
	if err := json.Unmarshal(rawRules, &rules); err != nil {
		t.Fatalf("decoding field-rules.json: %v", err)
	}

	ruleMap := make(map[string]fieldRule)
	for _, r := range rules {
		if r.OpType == "" || r.OpVersion < 1 || r.Field == "" {
			t.Errorf("invalid rule entry: %+v", r)
		}
		if !spec.KnownCatalogueStrategies[r.Strategy] {
			t.Errorf("strategy %q for (%s, %d, %s) is not in the closed catalogue of spec/fold.md", r.Strategy, r.OpType, r.OpVersion, r.Field)
		}
		if r.Strategy == "keyed-lww" && len(r.Key) == 0 {
			t.Errorf("keyed-lww strategy for (%s, %d, %s) must declare a non-empty key", r.OpType, r.OpVersion, r.Field)
		}
		key := fmt.Sprintf("%s:%d:%s", r.OpType, r.OpVersion, r.Field)
		if _, exists := ruleMap[key]; exists {
			t.Errorf("duplicate rule for %s", key)
		}
		ruleMap[key] = r
	}

	// Verify that every property of every body defined in issue-ops.schema.json has a declared rule.
	rawSchema, err := spec.FS.ReadFile("schemas/issue-ops.schema.json")
	if err != nil {
		t.Fatalf("reading issue-ops.schema.json: %v", err)
	}
	var schemaDoc struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(rawSchema, &schemaDoc); err != nil {
		t.Fatalf("decoding schema: %v", err)
	}

	bodyDefs := map[string]string{
		"create_body":    "create",
		"update_body":    "update",
		"set_state_body": "set-state",
		"assign_body":    "assign",
		"label_body":     "label",
		"link_body":      "link",
	}

	for defName, opType := range bodyDefs {
		def, ok := schemaDoc.Defs[defName]
		if !ok {
			t.Fatalf("schema missing %s in $defs", defName)
		}
		for fieldName := range def.Properties {
			key := fmt.Sprintf("%s:1:%s", opType, fieldName)
			if _, ok := ruleMap[key]; !ok {
				t.Errorf("missing field rule for (%s, op_version: 1, field: %s)", opType, fieldName)
			}
		}
	}
}

func TestGitHubIssueOpsConversionVectors(t *testing.T) {
	envSch, issSch := compileIssueOpsSchemas(t)

	for _, name := range readDirNames(t, "testdata/issue-ops/github") {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/issue-ops/github/" + name)
			if err != nil {
				t.Fatal(err)
			}
			var vec struct {
				GitHub json.RawMessage   `json:"github"`
				Ops    []json.RawMessage `json:"ops"`
			}
			if err := json.Unmarshal(raw, &vec); err != nil {
				t.Fatalf("decoding vector: %v", err)
			}
			if len(vec.GitHub) == 0 {
				t.Error("missing github member in vector")
			}
			if len(vec.Ops) == 0 {
				t.Error("missing ops member in vector")
			}
			for i, opRaw := range vec.Ops {
				t.Run(fmt.Sprintf("op_%d", i), func(t *testing.T) {
					canonOp, err := canonicaljson.Marshal(opRaw)
					if err != nil {
						t.Fatalf("canonicalizing op: %v", err)
					}
					if err := validateIssueOpVector(t, envSch, issSch, canonOp); err != nil {
						t.Errorf("op %d failed validation: %v", i, err)
					}
				})
			}
		})
	}
}
