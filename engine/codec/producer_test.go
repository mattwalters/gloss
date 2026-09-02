package codec_test

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/spec"
)

func testAuthor() codec.Identity {
	return codec.Identity{
		Name:  "Alice",
		Email: "alice@example.com",
		When:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestBuildCommitRejectsSchemaInvalidBody pins the producer obligation from
// spec/op-envelope.md: an op whose body violates its vocabulary schema is never
// built, so it is never signed and never appended. The three named cases are
// the instances that reached production as separate tickets (WRIT-114/115/119
// for person identifiers, and the unguarded verdict enum noted on PR #88).
func TestBuildCommitRejectsSchemaInvalidBody(t *testing.T) {
	overLongValue := strings.Repeat("a", 321)

	cases := []struct {
		name string
		env  codec.Envelope
	}{
		{
			name: "over-long person id",
			env: codec.Envelope{
				ObjectID:   "rev-1",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body: json.RawMessage(`{"revision":"` + strings.Repeat("0", 40) +
					`","verdict":"approve","subject":"email:` + overLongValue + `"}`),
			},
		},
		{
			name: "empty person id",
			env: codec.Envelope{
				ObjectID:   "rev-1",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body: json.RawMessage(`{"revision":"` + strings.Repeat("0", 40) +
					`","verdict":"approve","subject":""}`),
			},
		},
		{
			name: "invalid verdict enum",
			env: codec.Envelope{
				ObjectID:   "rev-1",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body: json.RawMessage(`{"revision":"` + strings.Repeat("0", 40) +
					`","verdict":"bogus"}`),
			},
		},
		{
			name: "review create missing title",
			env: codec.Envelope{
				ObjectID:   "rev-1",
				ObjectType: "review",
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(`{"description":"no title"}`),
			},
		},
		{
			name: "comment create missing subject",
			env: codec.Envelope{
				ObjectID:   "c-1",
				ObjectType: "comment",
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(`{"text":"hello"}`),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := codec.BuildCommit(tc.env, testAuthor(), nil)
			if err == nil {
				t.Fatal("BuildCommit accepted a schema-invalid body")
			}
			var rejErr *codec.RejectError
			if !errors.As(err, &rejErr) {
				t.Fatalf("error is not a *codec.RejectError: %v", err)
			}
			if rejErr.Reason != codec.RejectSchemaViolation {
				t.Errorf("reason = %q, want %q", rejErr.Reason, codec.RejectSchemaViolation)
			}
		})
	}
}

// knownCreateBodies is a minimal, schema-valid create body for each vocabulary
// writ registers, so the forward-compatibility cases below can be driven
// against every one of them rather than against whichever one happens to be
// most permissive.
var knownCreateBodies = map[string]string{
	"review":  `{"title":"Initial"}`,
	"comment": `{"subject":{"object_id":"rev-1","object_type":"review"},"text":"hello"}`,
	"issue":   `{"title":"Initial"}`,
	"project": `{"title":"Initial"}`,
	"cycle":   `{"ends_at":"2026-02-01T00:00:00Z","starts_at":"2026-01-01T00:00:00Z","title":"Sprint 1"}`,
	"repo":    `{"slug":"org/repo"}`,
}

// TestBuildCommitAcceptsUnknownFieldsInEveryVocabulary asserts the producer
// check did not narrow the unknown-field tolerance that every vocabulary
// shares (spec/forward-compatibility.md). It runs against all six registered
// vocabularies: an earlier version of this test covered only review, the most
// permissive one, so it could pass without touching the others at all.
func TestBuildCommitAcceptsUnknownFieldsInEveryVocabulary(t *testing.T) {
	for _, objectType := range sortedVocabularies(t) {
		t.Run(objectType, func(t *testing.T) {
			body := knownCreateBodies[objectType]
			if body == "" {
				t.Fatalf("vocabulary %q has no create body in knownCreateBodies — add one so this test covers it", objectType)
			}
			// Splice a field no version of this vocabulary defines into an
			// otherwise valid create body.
			withUnknown := body[:len(body)-1] + `,"future_field":{"x":1}}`

			if _, err := codec.BuildCommit(codec.Envelope{
				ObjectID:   "obj-1",
				ObjectType: objectType,
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(withUnknown),
			}, testAuthor(), nil); err != nil {
				t.Fatalf("BuildCommit rejected an unknown field a producer must still write: %v", err)
			}
		})
	}
}

// TestBuildCommitAcceptsForeignObjectTypes asserts the registry miss is not a
// rejection: an object type this build has never heard of must still build,
// because a reader has to tolerate it and writ's own producer never emits one.
func TestBuildCommitAcceptsForeignObjectTypes(t *testing.T) {
	if _, err := codec.BuildCommit(codec.Envelope{
		ObjectID:   "w-1",
		ObjectType: "widget",
		OpType:     "sprocket",
		OpVersion:  7,
		Body:       json.RawMessage(`{"anything":[1,2,3]}`),
	}, testAuthor(), nil); err != nil {
		t.Fatalf("BuildCommit rejected an object type it has never heard of: %v", err)
	}
}

// TestVocabulariesDisagreeOnUnknownOpTypeAndVersion characterizes a real
// divergence between the shipped vocabulary schemas, found in round 2 of this
// PR's review. It is not an endorsement: it exists so the disagreement is
// visible and so whichever way it is resolved, the resolution is deliberate.
//
// Five vocabularies gate their body rules on `op_version: 1` inside an
// if/then, so an unknown op_type or a future op_version validates — which is
// what spec/testdata/{review-ops,issue-ops,project,cycle,repo}/valid/
// unknown-op-type.json and the future-version vectors assert, five times over.
// comment.schema.json instead pins op_version and an op_type enum in an
// unconditional allOf, so it rejects both — which is what
// spec/testdata/comments/invalid/invalid-op-type.json asserts, and which
// contradicts spec/comments.md §Forward Compatibility ("Unknown op_type values
// under object_type: comment MUST be preserved ... and ignored").
//
// The corpus disagrees with itself, and resolving it means changing normative
// fixtures. Tracked as WRIT-148, which also removes this test; until then it
// pins today's behavior so the change cannot happen by accident.
func TestVocabulariesDisagreeOnUnknownOpTypeAndVersion(t *testing.T) {
	tolerant := []string{"review", "issue", "project", "cycle", "repo"}

	for _, objectType := range tolerant {
		t.Run(objectType+"/unknown op type", func(t *testing.T) {
			if _, err := codec.BuildCommit(codec.Envelope{
				ObjectID:   "obj-1",
				ObjectType: objectType,
				OpType:     "annotate",
				OpVersion:  1,
				Body:       json.RawMessage(`{"note":"from a newer client"}`),
			}, testAuthor(), nil); err != nil {
				t.Fatalf("BuildCommit rejected an unknown op type this vocabulary's corpus declares valid: %v", err)
			}
		})
		t.Run(objectType+"/future op version", func(t *testing.T) {
			if _, err := codec.BuildCommit(codec.Envelope{
				ObjectID:   "obj-1",
				ObjectType: objectType,
				OpType:     "create",
				OpVersion:  2,
				Body:       json.RawMessage(`{"headline":"v2 renamed the field"}`),
			}, testAuthor(), nil); err != nil {
				t.Fatalf("BuildCommit rejected a future op version this vocabulary's corpus declares valid: %v", err)
			}
		})
	}

	t.Run("comment/unknown op type is refused", func(t *testing.T) {
		if _, err := codec.BuildCommit(codec.Envelope{
			ObjectID:   "c-1",
			ObjectType: "comment",
			OpType:     "pin",
			OpVersion:  1,
			Body:       json.RawMessage(`{"pinned":true}`),
		}, testAuthor(), nil); err == nil {
			t.Fatal("comment.schema.json accepted an unknown op type — the divergence this test characterizes has been resolved (WRIT-148); delete this test and the spec note with it")
		}
	})
	t.Run("comment/future op version is refused", func(t *testing.T) {
		if _, err := codec.BuildCommit(codec.Envelope{
			ObjectID:   "c-1",
			ObjectType: "comment",
			OpType:     "create",
			OpVersion:  2,
			Body:       json.RawMessage(`{"headline":"v2 renamed the field"}`),
		}, testAuthor(), nil); err == nil {
			t.Fatal("comment.schema.json accepted a future op version — the divergence this test characterizes has been resolved (WRIT-148); delete this test and the spec note with it")
		}
	})
}

// TestEncodePayloadDoesNotValidateBody guards the read path. EncodePayload is
// used by the projection to re-encode ops fetched from the log whose raw bytes
// it did not keep (engine/projection/refresh.go). If body validation ever moves
// into it, writ starts refusing to project foreign ops it reads perfectly well
// today — so the check lives in BuildCommit and this test says so.
func TestEncodePayloadDoesNotValidateBody(t *testing.T) {
	env := codec.Envelope{
		ObjectID:   "rev-1",
		ObjectType: "review",
		OpType:     "approval",
		OpVersion:  1,
		Body:       json.RawMessage(`{"revision":"not-an-oid","verdict":"bogus"}`),
	}

	if _, err := codec.EncodePayload(env); err != nil {
		t.Fatalf("EncodePayload rejected a body it must still re-encode: %v", err)
	}
	if _, err := codec.BuildCommit(env, testAuthor(), nil); err == nil {
		t.Fatal("BuildCommit accepted the same body EncodePayload re-encodes")
	}
}

// TestEveryShippedVocabularyIsValidated is the exhaustiveness guard. Before
// WRIT-129 the codec registered review and comment and silently validated
// nothing for issue, project, cycle and repo, even though spec/schemas/ ships a
// schema for each. This walks the shipped schemas rather than a hand-written
// list, so adding a vocabulary without registering it fails here.
//
// The probe is an empty create body: every vocabulary requires at least one
// field on create, so a registered schema rejects it and an unregistered one
// returns nil.
func TestEveryShippedVocabularyIsValidated(t *testing.T) {
	for objectType, file := range shippedVocabularies(t) {
		err := codec.ValidateBody(codec.Envelope{
			ObjectID:   "obj-1",
			ObjectType: objectType,
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{}`),
		})
		if err == nil {
			t.Errorf("object type %q has a vocabulary schema in spec/schemas/%s, but the codec validates ops of that type against nothing — register it in vocabularySchemaFiles",
				objectType, file)
		}
	}
}

// shippedVocabularies maps each vocabulary schema shipped in spec/schemas/ to
// its file name, keyed by the object type it governs.
//
// A vocabulary is a schema that extends the op envelope. Identifying them that
// way is the fix for the hole this guard used to have: it previously treated
// "an object_type const was found where I looked" as the definition, so a
// vocabulary that pinned its type in a third place was skipped in silence
// rather than reported. Now the skip and the failure are different outcomes,
// and only support schemas are skipped.
func shippedVocabularies(t *testing.T) map[string]string {
	t.Helper()

	entries, err := spec.FS.ReadDir("schemas")
	if err != nil {
		t.Fatalf("read schemas dir: %v", err)
	}

	vocabularies := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := spec.FS.ReadFile("schemas/" + entry.Name())
		if err != nil {
			t.Fatalf("read schema %s: %v", entry.Name(), err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse schema %s: %v", entry.Name(), err)
		}
		if entry.Name() == envelopeSchemaFileName || !refsEnvelopeSchema(doc) {
			// A support schema: op-envelope itself, identifiers, anchor,
			// resolution. None of them governs an object type.
			continue
		}

		objectType, ok := pinnedObjectType(doc)
		if !ok {
			t.Errorf("spec/schemas/%s extends the op envelope, so it is a vocabulary, but no object_type const was found in it — this test cannot tell which object type it governs, so it cannot tell whether the codec validates that type. Pin object_type with a const, or teach pinnedObjectType where this schema puts it",
				entry.Name())
			continue
		}
		if prior, dup := vocabularies[objectType]; dup {
			t.Errorf("object type %q is governed by two schemas, %s and %s", objectType, prior, entry.Name())
		}
		vocabularies[objectType] = entry.Name()
	}

	if len(vocabularies) == 0 {
		t.Fatal("found no vocabulary schemas to check")
	}
	return vocabularies
}

// sortedVocabularies lists the shipped vocabularies' object types in a stable
// order, so table-driven cases run the same way every time.
func sortedVocabularies(t *testing.T) []string {
	t.Helper()

	vocabularies := shippedVocabularies(t)
	types := make([]string, 0, len(vocabularies))
	for objectType := range vocabularies {
		types = append(types, objectType)
	}
	sort.Strings(types)
	return types
}

// BenchmarkProducerPath measures what the producer check costs against the work
// it sits next to: canonical encoding, which every append already pays for.
func BenchmarkProducerPath(b *testing.B) {
	env := codec.Envelope{
		ObjectID:   "0123456789abcdef0123456789abcdef",
		ObjectType: "review",
		OpType:     "create",
		OpVersion:  1,
		Body:       json.RawMessage(`{"title":"Add calculator functions","description":"Initial draft of addition"}`),
	}
	author := testAuthor()

	b.Run("EncodePayload", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := codec.EncodePayload(env); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ValidateBody", func(b *testing.B) {
		raw, err := codec.EncodePayload(env)
		if err != nil {
			b.Fatal(err)
		}
		withRaw := env
		withRaw.Raw = raw
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := codec.ValidateBody(withRaw); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("BuildCommit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := codec.BuildCommit(env, author, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}

const (
	envelopeSchemaFileName = "op-envelope.schema.json"
	envelopeSchemaRef      = "https://writ.dev/spec/" + envelopeSchemaFileName
)

// refsEnvelopeSchema reports whether a schema document extends the op envelope
// schema, which is what makes it a vocabulary rather than a support schema.
//
// This is the identifying property, and the previous version of this test used
// the wrong one: it treated "an object_type const was found" as the test for
// being a vocabulary, so a vocabulary that pinned its object_type anywhere the
// extractor did not look was silently skipped rather than reported. A schema
// is a vocabulary because of what it extends, and every shipped one says so.
func refsEnvelopeSchema(node any) bool {
	switch n := node.(type) {
	case map[string]any:
		if ref, ok := n["$ref"].(string); ok && ref == envelopeSchemaRef {
			return true
		}
		for _, v := range n {
			if refsEnvelopeSchema(v) {
				return true
			}
		}
	case []any:
		for _, v := range n {
			if refsEnvelopeSchema(v) {
				return true
			}
		}
	}
	return false
}

// pinnedObjectType reports the object type a vocabulary schema fixes with a
// const, wherever in the document it puts it: the shipped schemas use the
// top-level properties map, an allOf branch, and an if branch, and searching
// the whole document costs nothing and cannot be outgrown by a fourth style.
// A vocabulary whose type still cannot be read fails the caller loudly at the
// call site rather than being skipped.
func pinnedObjectType(doc map[string]any) (string, bool) {
	return findConstObjectType(doc)
}

func findConstObjectType(node any) (string, bool) {
	switch n := node.(type) {
	case map[string]any:
		if props, ok := n["properties"].(map[string]any); ok {
			if ot, ok := props["object_type"].(map[string]any); ok {
				if t, ok := ot["const"].(string); ok {
					return t, true
				}
			}
		}
		for _, key := range sortedKeys(n) {
			if t, ok := findConstObjectType(n[key]); ok {
				return t, true
			}
		}
	case []any:
		for _, v := range n {
			if t, ok := findConstObjectType(v); ok {
				return t, true
			}
		}
	}
	return "", false
}

// sortedKeys keeps the walk deterministic, so a schema that somehow pinned two
// different object types reports the same one on every run.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
