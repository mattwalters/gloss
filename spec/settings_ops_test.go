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
	settingsOpsSchemaID = "https://writ.dev/spec/settings-ops.schema.json"
)

var settingsIDRegex = regexp.MustCompile(`^[0-9a-f]{32}$`)

func compileSettingsOpsSchemas(t *testing.T) (*jsonschema.Schema, *jsonschema.Schema) {
	t.Helper()
	envRaw, err := spec.FS.ReadFile("schemas/op-envelope.schema.json")
	if err != nil {
		t.Fatalf("reading envelope schema: %v", err)
	}
	envDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(envRaw))
	if err != nil {
		t.Fatalf("decoding envelope schema: %v", err)
	}

	settRaw, err := spec.FS.ReadFile("schemas/settings-ops.schema.json")
	if err != nil {
		t.Fatalf("reading settings-ops schema: %v", err)
	}
	settDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(settRaw))
	if err != nil {
		t.Fatalf("decoding settings-ops schema: %v", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(envelopeSchemaID, envDoc); err != nil {
		t.Fatalf("adding envelope schema resource: %v", err)
	}
	if err := c.AddResource(settingsOpsSchemaID, settDoc); err != nil {
		t.Fatalf("adding settings-ops schema resource: %v", err)
	}

	envSch, err := c.Compile(envelopeSchemaID)
	if err != nil {
		t.Fatalf("compiling envelope schema: %v", err)
	}
	settSch, err := c.Compile(settingsOpsSchemaID)
	if err != nil {
		t.Fatalf("compiling settings-ops schema: %v", err)
	}

	return envSch, settSch
}

func settingsInvariants(payload map[string]any) error {
	objectType, _ := payload["object_type"].(string)
	if objectType != "settings" {
		return nil
	}
	objectID, _ := payload["object_id"].(string)
	if !settingsIDRegex.MatchString(objectID) {
		return fmt.Errorf("settings object_id (%q) must be 32 lowercase hex characters", objectID)
	}
	return nil
}

func validateSettingsOpVector(t *testing.T, envSch, settSch *jsonschema.Schema, raw []byte) error {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decoding vector: %v", err)
	}
	if err := envSch.Validate(inst); err != nil {
		return fmt.Errorf("envelope schema: %w", err)
	}
	if err := settSch.Validate(inst); err != nil {
		return fmt.Errorf("settings-ops schema: %w", err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("re-decoding vector: %v", err)
	}
	if err := settingsInvariants(p); err != nil {
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

func TestSettingsOpsSchemaCompiles(t *testing.T) {
	compileSettingsOpsSchemas(t)
}

func TestValidSettingsOpsVectors(t *testing.T) {
	envSch, settSch := compileSettingsOpsSchemas(t)
	for _, name := range readDirNames(t, "testdata/settings/valid") {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/settings/valid/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateSettingsOpVector(t, envSch, settSch, raw); err != nil {
				t.Errorf("valid vector rejected: %v", err)
			}
		})
	}
}

func TestInvalidSettingsOpsVectors(t *testing.T) {
	_, settSch := compileSettingsOpsSchemas(t)

	rawIndex, err := spec.FS.ReadFile("testdata/settings/invalid/index.json")
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

	names := readDirNames(t, "testdata/settings/invalid")
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
			raw, err := spec.FS.ReadFile("testdata/settings/invalid/" + entry.File)
			if err != nil {
				t.Fatalf("reading instance: %v", err)
			}
			switch entry.Rejects {
			case "schema":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance (a schema-rejected instance must be parseable JSON): %v", err)
				}
				if err := settSch.Validate(inst); err == nil {
					t.Errorf("schema accepted the instance; expected rejection: %s", entry.Reason)
				}
			case "invariant":
				inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parsing instance: %v", err)
				}
				if err := settSch.Validate(inst); err != nil {
					t.Errorf("schema rejected an invariant-kind vector (%v); expected only invariant to fail: %s", err, entry.Reason)
				}
				var p map[string]any
				if err := json.Unmarshal(raw, &p); err != nil {
					t.Fatal(err)
				}
				if err := settingsInvariants(p); err == nil {
					t.Errorf("invariant accepted the instance; expected rejection: %s", entry.Reason)
				}
			case "canonicalization":
				switch entry.Category {
				case "not-canonical":
					canon, err := canonicaljson.Marshal(raw)
					if err != nil {
						return
					}
					if bytes.Equal(canon, raw) {
						t.Errorf("instance was already byte-canonical; expected non-canonical form")
					}
				case "duplicate-key":
					if _, err := canonicaljson.Marshal(raw); err == nil {
						t.Errorf("canonicalizer accepted duplicate key; expected rejection: %s", entry.Reason)
					} else if !strings.Contains(err.Error(), "duplicate") {
						t.Errorf("unexpected error for duplicate key vector: %v", err)
					}
				case "lone-surrogate":
					if _, err := canonicaljson.Marshal(raw); err == nil {
						t.Errorf("canonicalizer accepted lone surrogate; expected rejection: %s", entry.Reason)
					} else if !strings.Contains(err.Error(), "surrogate") {
						t.Errorf("unexpected error for lone surrogate vector: %v", err)
					}
				default:
					t.Fatalf("unknown canonicalization category: %s", entry.Category)
				}
			default:
				t.Fatalf("unknown rejects value in index.json: %s", entry.Rejects)
			}
		})
	}
}
