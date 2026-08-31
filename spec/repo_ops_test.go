package spec_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/spec"
)

const (
	repoOpsSchemaID = "https://writ.dev/spec/repo-ops.schema.json"
)

var repoIDRegex = regexp.MustCompile(`^[0-9a-f]{32}$`)

func compileRepoOpsSchemas(t *testing.T) (*jsonschema.Schema, *jsonschema.Schema) {
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

	repoRaw, err := spec.FS.ReadFile("schemas/repo-ops.schema.json")
	if err != nil {
		t.Fatalf("reading repo-ops schema: %v", err)
	}
	repoDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(repoRaw))
	if err != nil {
		t.Fatalf("decoding repo-ops schema: %v", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(envelopeSchemaID, envDoc); err != nil {
		t.Fatalf("adding envelope schema resource: %v", err)
	}
	if err := c.AddResource(identifiersSchemaID, idDoc); err != nil {
		t.Fatalf("adding identifiers schema resource: %v", err)
	}
	if err := c.AddResource(repoOpsSchemaID, repoDoc); err != nil {
		t.Fatalf("adding repo-ops schema resource: %v", err)
	}

	envSch, err := c.Compile(envelopeSchemaID)
	if err != nil {
		t.Fatalf("compiling envelope schema: %v", err)
	}
	repoSch, err := c.Compile(repoOpsSchemaID)
	if err != nil {
		t.Fatalf("compiling repo-ops schema: %v", err)
	}

	return envSch, repoSch
}

func repoInvariants(payload map[string]any) error {
	objectType, _ := payload["object_type"].(string)
	if objectType != "repo" {
		return nil
	}
	objectID, _ := payload["object_id"].(string)
	if !repoIDRegex.MatchString(objectID) {
		return fmt.Errorf("repo object_id (%q) must be 32 lowercase hex characters (repo-id)", objectID)
	}
	return nil
}

func validateRepoOpVector(t *testing.T, envSch, repoSch *jsonschema.Schema, raw []byte) error {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decoding vector: %v", err)
	}
	if err := envSch.Validate(inst); err != nil {
		return fmt.Errorf("envelope schema: %w", err)
	}
	if err := repoSch.Validate(inst); err != nil {
		return fmt.Errorf("repo-ops schema: %w", err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("re-decoding vector: %v", err)
	}
	if err := repoInvariants(p); err != nil {
		return fmt.Errorf("invariant: %w", err)
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

func TestRepoOpsSchemaCompiles(t *testing.T) {
	compileRepoOpsSchemas(t)
}

func TestValidRepoOpsVectors(t *testing.T) {
	envSch, repoSch := compileRepoOpsSchemas(t)
	for _, name := range readDirNames(t, "testdata/repo/valid") {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/repo/valid/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateRepoOpVector(t, envSch, repoSch, raw); err != nil {
				t.Errorf("valid vector rejected: %v", err)
			}
		})
	}
}

func TestInvalidRepoOpsVectors(t *testing.T) {
	_, repoSch := compileRepoOpsSchemas(t)

	rawIndex, err := spec.FS.ReadFile("testdata/repo/invalid/index.json")
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

	names := readDirNames(t, "testdata/repo/invalid")
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
			raw, err := spec.FS.ReadFile("testdata/repo/invalid/" + entry.File)
			if err != nil {
				t.Fatalf("reading instance: %v", err)
			}
			switch entry.Rejects {
			case "schema":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance (a schema-rejected instance must be parseable JSON): %v", err)
				}
				if err := repoSch.Validate(inst); err == nil {
					t.Errorf("schema accepted the instance; expected rejection: %s", entry.Reason)
				}
			case "invariant":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance: %v", err)
				}
				if err := repoSch.Validate(inst); err != nil {
					t.Errorf("schema rejected an invariant-kind vector (%v); expected only invariant to fail: %s", err, entry.Reason)
				}
				var p map[string]any
				if err := json.Unmarshal(raw, &p); err != nil {
					t.Fatal(err)
				}
				if err := repoInvariants(p); err == nil {
					t.Errorf("invariants accepted it; expected rejection: %s", entry.Reason)
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

func TestRepoFieldRules(t *testing.T) {
	rawRules, err := spec.FS.ReadFile("testdata/repo/field-rules.json")
	if err != nil {
		t.Fatalf("reading repo/field-rules.json: %v", err)
	}

	var rules []spec.FieldRule
	if err := json.Unmarshal(rawRules, &rules); err != nil {
		t.Fatalf("decoding repo/field-rules.json: %v", err)
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

	// Verify that every property of every body defined in repo-ops.schema.json has a declared rule.
	rawSchema, err := spec.FS.ReadFile("schemas/repo-ops.schema.json")
	if err != nil {
		t.Fatalf("reading repo-ops.schema.json: %v", err)
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
		"create_body":     "create",
		"set_slug_body":   "set-slug",
		"add_remote_body": "add-remote",
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

	// Verify that spec.FieldRules() parses all field rules across the entire spec
	allRules, err := spec.FieldRules()
	if err != nil {
		t.Fatalf("spec.FieldRules() failed: %v", err)
	}
	if len(allRules) == 0 {
		t.Fatal("spec.FieldRules() returned no rules")
	}
}
