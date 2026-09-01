package spec_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/spec"
)

const identifiersSchemaID = "https://writ.dev/spec/identifiers.schema.json"

// compileIdentifiersSchema compiles the identifiers schema; compilation also
// validates it against the draft 2020-12 meta-schema.
func compileIdentifiersSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := spec.FS.ReadFile("schemas/identifiers.schema.json")
	if err != nil {
		t.Fatalf("reading schema: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decoding schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(identifiersSchemaID, doc); err != nil {
		t.Fatalf("adding schema resource: %v", err)
	}
	sch, err := c.Compile(identifiersSchemaID)
	if err != nil {
		t.Fatalf("compiling schema: %v", err)
	}
	return sch
}

type repoRegistryEntry struct {
	RepoID      string   `json:"repo_id"`
	Slug        string   `json:"slug"`
	Remotes     []string `json:"remotes,omitempty"`
	IsWorkspace bool     `json:"is_workspace,omitempty"`
}

type referenceResolutionVector struct {
	Reference string `json:"reference"`
	Context   *struct {
		LocalRepoID string `json:"local_repo_id"`
	} `json:"context,omitempty"`
	Registry []repoRegistryEntry `json:"registry,omitempty"`
	Expected *struct {
		Resolved bool   `json:"resolved"`
		Scope    string `json:"scope"`
		RepoID   string `json:"repo_id,omitempty"`
		ObjectID string `json:"object_id"`
	} `json:"expected,omitempty"`
}

// parseReference parses a reference string into designator and object ID.
func parseReference(ref string) (designator string, objectID string, err error) {
	if ref == "" {
		return "", "", fmt.Errorf("reference is empty")
	}
	parts := strings.Split(ref, "#")
	switch len(parts) {
	case 1:
		return "", parts[0], nil
	case 2:
		if parts[0] == "" {
			return "", "", fmt.Errorf("reference %q has empty repo designator", ref)
		}
		if parts[1] == "" {
			return "", "", fmt.Errorf("reference %q has empty object id", ref)
		}
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("reference %q contains multiple '#' separators", ref)
	}
}

// resolveReference executes the reference resolution algorithm from
// spec/identifiers.md §Reference resolution.
func resolveReference(ref string, localRepoID string, registry []repoRegistryEntry) (resolved bool, scope string, repoID string, objectID string, err error) {
	designator, objID, err := parseReference(ref)
	if err != nil {
		return false, "", "", "", err
	}

	// Same-repo short circuit
	if designator == "" || (localRepoID != "" && designator == localRepoID) {
		targetRepoID := localRepoID
		if targetRepoID == "" && designator != "" {
			targetRepoID = designator
		}
		return true, "local", targetRepoID, objID, nil
	}

	// Cross-repo lookup in registry
	for _, entry := range registry {
		if entry.RepoID == designator {
			return true, "cross-repo", entry.RepoID, objID, nil
		}
	}

	// Unresolvable reference: preserved and surfaced as unresolved
	return false, "unresolved", "", objID, nil
}

// referenceInvariants enforces cross-field rules from spec/identifiers.md
// that JSON Schema cannot express.
func referenceInvariants(raw []byte) error {
	var vec referenceResolutionVector
	if err := json.Unmarshal(raw, &vec); err != nil {
		return fmt.Errorf("decoding JSON: %w", err)
	}

	// Enforce uniqueness of repo_id in registry
	seen := make(map[string]bool)
	for _, entry := range vec.Registry {
		if seen[entry.RepoID] {
			return fmt.Errorf("duplicate repo_id %q in registry", entry.RepoID)
		}
		seen[entry.RepoID] = true
	}

	return nil
}

func TestIdentifiersSchemaCompiles(t *testing.T) {
	compileIdentifiersSchema(t)
}

func TestValidReferenceVectors(t *testing.T) {
	sch := compileIdentifiersSchema(t)
	for _, name := range readDirNames(t, "testdata/references/valid") {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/references/valid/" + name)
			if err != nil {
				t.Fatal(err)
			}

			// Schema validation
			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("decoding vector JSON: %v", err)
			}
			if err := sch.Validate(inst); err != nil {
				t.Fatalf("schema validation failed: %v", err)
			}

			// Invariant validation
			if err := referenceInvariants(raw); err != nil {
				t.Fatalf("invariant validation failed: %v", err)
			}

			// Resolution execution
			var vec referenceResolutionVector
			if err := json.Unmarshal(raw, &vec); err != nil {
				t.Fatalf("unmarshaling vector: %v", err)
			}

			var localRepoID string
			if vec.Context != nil {
				localRepoID = vec.Context.LocalRepoID
			}

			resolved, scope, repoID, objectID, err := resolveReference(vec.Reference, localRepoID, vec.Registry)
			if err != nil {
				t.Fatalf("resolveReference error: %v", err)
			}

			if vec.Expected != nil {
				if resolved != vec.Expected.Resolved {
					t.Errorf("resolved = %v, want %v", resolved, vec.Expected.Resolved)
				}
				if scope != vec.Expected.Scope {
					t.Errorf("scope = %q, want %q", scope, vec.Expected.Scope)
				}
				if vec.Expected.RepoID != "" && repoID != vec.Expected.RepoID {
					t.Errorf("repoID = %q, want %q", repoID, vec.Expected.RepoID)
				}
				if objectID != vec.Expected.ObjectID {
					t.Errorf("objectID = %q, want %q", objectID, vec.Expected.ObjectID)
				}
			}
		})
	}
}

func TestInvalidReferenceVectors(t *testing.T) {
	sch := compileIdentifiersSchema(t)

	rawIndex, err := spec.FS.ReadFile("testdata/references/invalid/index.json")
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

	names := readDirNames(t, "testdata/references/invalid")
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
			raw, err := spec.FS.ReadFile("testdata/references/invalid/" + name)
			if err != nil {
				t.Fatal(err)
			}
			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("decoding vector: %v", err)
			}
			schemaErr := sch.Validate(inst)
			switch entry.Kind {
			case "schema":
				if schemaErr == nil {
					t.Errorf("schema accepted it; expected rejection: %s", entry.Reason)
				}
			case "invariant":
				if schemaErr != nil {
					t.Errorf("schema rejected an invariant-kind vector (%v); expected only invariant to fail: %s", schemaErr, entry.Reason)
				}
				if err := referenceInvariants(raw); err == nil {
					t.Errorf("invariants accepted it; expected rejection: %s", entry.Reason)
				}
			default:
				t.Errorf("index.json kind %q unknown (want schema or invariant)", entry.Kind)
			}
		})
	}
}

func TestPersonIdentifierNormalization(t *testing.T) {
	// Normalization rule: norm(s) = lowercase(trim_whitespace(s))
	// Comparison rule: equal(A, B) <=> norm(A) == norm(B)
	cases := []struct {
		a     string
		b     string
		equal bool
	}{
		{"alice@example.com", "alice@example.com", true},
		{"Alice@Example.COM", "alice@example.com", true},
		{"  alice@example.com  ", "alice@example.com", true},
		{"\t\n Alice@Example.COM \r\n", "alice@example.com", true},
		{"alice@example.com", "bob@example.com", false},
		{"alice@example.com", "alice@sub.example.com", false},
		{"dev+1@example.com", "dev+1@example.com", true},
		{"DEV+1@EXAMPLE.COM", "dev+1@example.com", true},
	}

	// Deliberately spelled out here rather than calling any implementation:
	// this test checks the prose rule in spec/identifiers.md, so it must be
	// able to fail when every implementation agrees on the wrong thing.
	// TestReffoldNormalizePersonMatchesEngine is the test that binds the
	// implementations to each other.
	norm := func(s string) string {
		return strings.ToLower(strings.TrimSpace(s))
	}

	for _, tc := range cases {
		normA := norm(tc.a)
		normB := norm(tc.b)
		gotEqual := normA == normB
		if gotEqual != tc.equal {
			t.Errorf("equal(%q, %q) = %v, want %v (norm(%q)=%q, norm(%q)=%q)",
				tc.a, tc.b, gotEqual, tc.equal, tc.a, normA, tc.b, normB)
		}
	}
}

func TestPersonIdentifierSchema(t *testing.T) {
	sch := compileIdentifiersSchema(t)

	// Test $defs/person-id via a test document referencing it
	c := jsonschema.NewCompiler()
	testDoc := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"person": map[string]any{
				"$ref": "https://writ.dev/spec/identifiers.schema.json#/$defs/person-id",
			},
		},
		"required": []any{"person"},
	}

	rawIdent, err := spec.FS.ReadFile("schemas/identifiers.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	identDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawIdent))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddResource(identifiersSchemaID, identDoc); err != nil {
		t.Fatal(err)
	}
	if err := c.AddResource("https://writ.dev/test/person-wrapper.schema.json", testDoc); err != nil {
		t.Fatal(err)
	}
	compiled, err := c.Compile("https://writ.dev/test/person-wrapper.schema.json")
	if err != nil {
		t.Fatalf("compiling person wrapper schema: %v", err)
	}

	// The RFC 5321 ceiling exactly: 64-octet local part, "@", 255-octet domain.
	atLimit := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 251) + ".com"
	if len(atLimit) != 320 {
		t.Fatalf("test setup: atLimit is %d characters, want 320", len(atLimit))
	}
	overLimit := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 252) + ".com"
	if len(overLimit) != 321 {
		t.Fatalf("test setup: overLimit is %d characters, want 321", len(overLimit))
	}

	validCases := []string{
		"alice@example.com",
		"bob.builder+test@example.co.uk",
		"ci-bot@internal.domain",
		"  alice@example.com  ",
		atLimit,
	}
	for _, v := range validCases {
		inst := map[string]any{"person": v}
		if err := compiled.Validate(inst); err != nil {
			t.Errorf("expected person %q to be valid, got %v", v, err)
		}
	}

	invalidCases := []any{
		"",        // minLength: 1
		overLimit, // maxLength: 320
		12345,     // not a string
		nil,       // null
		[]any{},   // array
	}
	for _, inv := range invalidCases {
		inst := map[string]any{"person": inv}
		if err := compiled.Validate(inst); err == nil {
			t.Errorf("expected person %v to be invalid, but schema accepted it", inv)
		}
	}

	_ = sch
}

func TestWriterIDVsPersonID(t *testing.T) {
	// Writer ID: 16 lowercase hex characters (device-scoped namespace under refs/writ/<writer-id>/)
	// Person ID: collaborative actor identity (email address)
	writerID := "0123456789abcdef"
	personID := "alice@example.com"

	isHex16 := func(s string) bool {
		if len(s) != 16 {
			return false
		}
		for _, c := range s {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				return false
			}
		}
		return true
	}

	if !isHex16(writerID) {
		t.Errorf("writerID %q should match 16-hex format", writerID)
	}
	if isHex16(personID) {
		t.Errorf("personID %q should not match 16-hex format", personID)
	}
}
