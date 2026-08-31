package spec_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/spec"
)

const (
	projectOpsSchemaID = "https://writ.dev/spec/project-ops.schema.json"
	cycleOpsSchemaID   = "https://writ.dev/spec/cycle-ops.schema.json"
)

func compileProjectCycleSchemas(t *testing.T) (*jsonschema.Schema, *jsonschema.Schema, *jsonschema.Schema) {
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

	projRaw, err := spec.FS.ReadFile("schemas/project-ops.schema.json")
	if err != nil {
		t.Fatalf("reading project-ops schema: %v", err)
	}
	projDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(projRaw))
	if err != nil {
		t.Fatalf("decoding project-ops schema: %v", err)
	}

	cycRaw, err := spec.FS.ReadFile("schemas/cycle-ops.schema.json")
	if err != nil {
		t.Fatalf("reading cycle-ops schema: %v", err)
	}
	cycDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(cycRaw))
	if err != nil {
		t.Fatalf("decoding cycle-ops schema: %v", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(envelopeSchemaID, envDoc); err != nil {
		t.Fatalf("adding envelope schema resource: %v", err)
	}
	if err := c.AddResource(identifiersSchemaID, idDoc); err != nil {
		t.Fatalf("adding identifiers schema resource: %v", err)
	}
	if err := c.AddResource(projectOpsSchemaID, projDoc); err != nil {
		t.Fatalf("adding project-ops schema resource: %v", err)
	}
	if err := c.AddResource(cycleOpsSchemaID, cycDoc); err != nil {
		t.Fatalf("adding cycle-ops schema resource: %v", err)
	}

	envSch, err := c.Compile(envelopeSchemaID)
	if err != nil {
		t.Fatalf("compiling envelope schema: %v", err)
	}
	projSch, err := c.Compile(projectOpsSchemaID)
	if err != nil {
		t.Fatalf("compiling project-ops schema: %v", err)
	}
	cycSch, err := c.Compile(cycleOpsSchemaID)
	if err != nil {
		t.Fatalf("compiling cycle-ops schema: %v", err)
	}

	return envSch, projSch, cycSch
}

func projectInvariants(payload map[string]any) error {
	opType, _ := payload["op_type"].(string)
	body, ok := payload["body"].(map[string]any)
	if !ok {
		return nil
	}

	if opType == "update" {
		_, hasTitle := body["title"]
		_, hasDesc := body["description"]
		if !hasTitle && !hasDesc {
			return fmt.Errorf("update op body must contain at least title or description")
		}
	}

	return nil
}

func cycleInvariants(payload map[string]any) error {
	opType, _ := payload["op_type"].(string)
	body, ok := payload["body"].(map[string]any)
	if !ok {
		return nil
	}

	if opType == "update" {
		_, hasTitle := body["title"]
		_, hasDesc := body["description"]
		if !hasTitle && !hasDesc {
			return fmt.Errorf("update op body must contain at least title or description")
		}
	}

	startsStr, hasStarts := body["starts_at"].(string)
	endsStr, hasEnds := body["ends_at"].(string)
	if hasStarts && hasEnds {
		starts, err := time.Parse(time.RFC3339, startsStr)
		if err != nil {
			return fmt.Errorf("parsing starts_at: %w", err)
		}
		ends, err := time.Parse(time.RFC3339, endsStr)
		if err != nil {
			return fmt.Errorf("parsing ends_at: %w", err)
		}
		if !ends.After(starts) {
			return fmt.Errorf("ends_at (%s) must be strictly after starts_at (%s)", endsStr, startsStr)
		}
	}

	return nil
}

func validateProjectOpVector(t *testing.T, envSch, projSch *jsonschema.Schema, raw []byte) error {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decoding vector: %v", err)
	}
	if err := envSch.Validate(inst); err != nil {
		return fmt.Errorf("envelope schema: %w", err)
	}
	if err := projSch.Validate(inst); err != nil {
		return fmt.Errorf("project-ops schema: %w", err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("re-decoding vector: %v", err)
	}
	if err := projectInvariants(p); err != nil {
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

func validateCycleOpVector(t *testing.T, envSch, cycSch *jsonschema.Schema, raw []byte) error {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decoding vector: %v", err)
	}
	if err := envSch.Validate(inst); err != nil {
		return fmt.Errorf("envelope schema: %w", err)
	}
	if err := cycSch.Validate(inst); err != nil {
		return fmt.Errorf("cycle-ops schema: %w", err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("re-decoding vector: %v", err)
	}
	if err := cycleInvariants(p); err != nil {
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

func TestProjectCycleSchemasCompile(t *testing.T) {
	compileProjectCycleSchemas(t)
}

func TestValidProjectVectors(t *testing.T) {
	envSch, projSch, _ := compileProjectCycleSchemas(t)
	for _, name := range readDirNames(t, "testdata/project/valid") {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/project/valid/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateProjectOpVector(t, envSch, projSch, raw); err != nil {
				t.Errorf("valid vector rejected: %v", err)
			}
		})
	}
}

func TestInvalidProjectVectors(t *testing.T) {
	_, projSch, _ := compileProjectCycleSchemas(t)

	rawIndex, err := spec.FS.ReadFile("testdata/project/invalid/index.json")
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

	names := readDirNames(t, "testdata/project/invalid")
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
			raw, err := spec.FS.ReadFile("testdata/project/invalid/" + entry.File)
			if err != nil {
				t.Fatalf("reading instance: %v", err)
			}
			switch entry.Rejects {
			case "schema":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance (a schema-rejected instance must be parseable JSON): %v", err)
				}
				if err := projSch.Validate(inst); err == nil {
					t.Errorf("schema accepted the instance; expected rejection: %s", entry.Reason)
				}
			case "invariant":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance: %v", err)
				}
				if err := projSch.Validate(inst); err != nil {
					t.Errorf("schema rejected an invariant-kind vector (%v); expected only invariant to fail: %s", err, entry.Reason)
				}
				var p map[string]any
				if err := json.Unmarshal(raw, &p); err != nil {
					t.Fatal(err)
				}
				if err := projectInvariants(p); err == nil {
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

func TestValidCycleVectors(t *testing.T) {
	envSch, _, cycSch := compileProjectCycleSchemas(t)
	for _, name := range readDirNames(t, "testdata/cycle/valid") {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/cycle/valid/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateCycleOpVector(t, envSch, cycSch, raw); err != nil {
				t.Errorf("valid vector rejected: %v", err)
			}
		})
	}
}

func TestInvalidCycleVectors(t *testing.T) {
	_, _, cycSch := compileProjectCycleSchemas(t)

	rawIndex, err := spec.FS.ReadFile("testdata/cycle/invalid/index.json")
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

	names := readDirNames(t, "testdata/cycle/invalid")
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
			raw, err := spec.FS.ReadFile("testdata/cycle/invalid/" + entry.File)
			if err != nil {
				t.Fatalf("reading instance: %v", err)
			}
			switch entry.Rejects {
			case "schema":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance (a schema-rejected instance must be parseable JSON): %v", err)
				}
				if err := cycSch.Validate(inst); err == nil {
					t.Errorf("schema accepted the instance; expected rejection: %s", entry.Reason)
				}
			case "invariant":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance: %v", err)
				}
				if err := cycSch.Validate(inst); err != nil {
					t.Errorf("schema rejected an invariant-kind vector (%v); expected only invariant to fail: %s", err, entry.Reason)
				}
				var p map[string]any
				if err := json.Unmarshal(raw, &p); err != nil {
					t.Fatal(err)
				}
				if err := cycleInvariants(p); err == nil {
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

func TestProjectCycleFieldRules(t *testing.T) {
	// 1. Verify project field rules
	rawProjRules, err := spec.FS.ReadFile("testdata/project/field-rules.json")
	if err != nil {
		t.Fatalf("reading project/field-rules.json: %v", err)
	}
	var projRules []spec.FieldRule
	if err := json.Unmarshal(rawProjRules, &projRules); err != nil {
		t.Fatalf("decoding project/field-rules.json: %v", err)
	}
	projRuleMap := make(map[string]spec.FieldRule)
	for _, r := range projRules {
		if r.OpType == "" || r.OpVersion < 1 || r.Field == "" {
			t.Errorf("invalid project rule entry: %+v", r)
		}
		if !spec.KnownCatalogueStrategies[r.Strategy] {
			t.Errorf("strategy %q for (%s, %d, %s) is not in the closed catalogue", r.Strategy, r.OpType, r.OpVersion, r.Field)
		}
		key := fmt.Sprintf("%s:%d:%s", r.OpType, r.OpVersion, r.Field)
		if _, exists := projRuleMap[key]; exists {
			t.Errorf("duplicate project rule for %s", key)
		}
		projRuleMap[key] = r
	}

	rawProjSchema, err := spec.FS.ReadFile("schemas/project-ops.schema.json")
	if err != nil {
		t.Fatalf("reading project-ops.schema.json: %v", err)
	}
	var projSchemaDoc struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(rawProjSchema, &projSchemaDoc); err != nil {
		t.Fatalf("decoding project schema: %v", err)
	}

	projBodyDefs := map[string]string{
		"create_body":       "create",
		"update_body":       "update",
		"set_status_body":   "set-status",
		"add_issue_body":    "add-issue",
		"remove_issue_body": "remove-issue",
	}

	for defName, opType := range projBodyDefs {
		def, ok := projSchemaDoc.Defs[defName]
		if !ok {
			t.Fatalf("project schema missing %s in $defs", defName)
		}
		for fieldName := range def.Properties {
			key := fmt.Sprintf("%s:1:%s", opType, fieldName)
			if _, ok := projRuleMap[key]; !ok {
				t.Errorf("missing project field rule for (%s, op_version: 1, field: %s)", opType, fieldName)
			}
		}
	}

	// 2. Verify cycle field rules
	rawCycRules, err := spec.FS.ReadFile("testdata/cycle/field-rules.json")
	if err != nil {
		t.Fatalf("reading cycle/field-rules.json: %v", err)
	}
	var cycRules []spec.FieldRule
	if err := json.Unmarshal(rawCycRules, &cycRules); err != nil {
		t.Fatalf("decoding cycle/field-rules.json: %v", err)
	}
	cycRuleMap := make(map[string]spec.FieldRule)
	for _, r := range cycRules {
		if r.OpType == "" || r.OpVersion < 1 || r.Field == "" {
			t.Errorf("invalid cycle rule entry: %+v", r)
		}
		if !spec.KnownCatalogueStrategies[r.Strategy] {
			t.Errorf("strategy %q for (%s, %d, %s) is not in the closed catalogue", r.Strategy, r.OpType, r.OpVersion, r.Field)
		}
		key := fmt.Sprintf("%s:%d:%s", r.OpType, r.OpVersion, r.Field)
		if _, exists := cycRuleMap[key]; exists {
			t.Errorf("duplicate cycle rule for %s", key)
		}
		cycRuleMap[key] = r
	}

	rawCycSchema, err := spec.FS.ReadFile("schemas/cycle-ops.schema.json")
	if err != nil {
		t.Fatalf("reading cycle-ops.schema.json: %v", err)
	}
	var cycSchemaDoc struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(rawCycSchema, &cycSchemaDoc); err != nil {
		t.Fatalf("decoding cycle schema: %v", err)
	}

	cycBodyDefs := map[string]string{
		"create_body":       "create",
		"update_body":       "update",
		"set_dates_body":    "set-dates",
		"add_issue_body":    "add-issue",
		"remove_issue_body": "remove-issue",
	}

	for defName, opType := range cycBodyDefs {
		def, ok := cycSchemaDoc.Defs[defName]
		if !ok {
			t.Fatalf("cycle schema missing %s in $defs", defName)
		}
		for fieldName := range def.Properties {
			key := fmt.Sprintf("%s:1:%s", opType, fieldName)
			if _, ok := cycRuleMap[key]; !ok {
				t.Errorf("missing cycle field rule for (%s, op_version: 1, field: %s)", opType, fieldName)
			}
		}
	}

	// 3. Verify that spec.FieldRules() parses all field rules across the entire spec
	allRules, err := spec.FieldRules()
	if err != nil {
		t.Fatalf("spec.FieldRules() failed: %v", err)
	}
	if len(allRules) == 0 {
		t.Fatal("spec.FieldRules() returned no rules")
	}
}
