package codec_test

import (
	"encoding/json"
	"errors"
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

// TestBuildCommitAcceptsForeignAndUnknownShapes asserts the producer check did
// not narrow forward compatibility (spec/forward-compatibility.md): an object
// type this build has never heard of, an unknown op type inside a known
// vocabulary, and unknown fields inside a known body all still build.
func TestBuildCommitAcceptsForeignAndUnknownShapes(t *testing.T) {
	cases := []struct {
		name string
		env  codec.Envelope
	}{
		{
			name: "object type with no registered vocabulary",
			env: codec.Envelope{
				ObjectID:   "w-1",
				ObjectType: "widget",
				OpType:     "sprocket",
				OpVersion:  7,
				Body:       json.RawMessage(`{"anything":[1,2,3]}`),
			},
		},
		{
			name: "unknown op type in a known vocabulary",
			env: codec.Envelope{
				ObjectID:   "rev-1",
				ObjectType: "review",
				OpType:     "annotate",
				OpVersion:  1,
				Body:       json.RawMessage(`{"note":"from a newer client"}`),
			},
		},
		{
			name: "unknown fields in a known body",
			env: codec.Envelope{
				ObjectID:   "rev-1",
				ObjectType: "review",
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(`{"title":"Initial","future_field":{"x":1}}`),
			},
		},
		{
			name: "future op version in a known vocabulary",
			env: codec.Envelope{
				ObjectID:   "rev-1",
				ObjectType: "review",
				OpType:     "create",
				OpVersion:  2,
				Body:       json.RawMessage(`{"headline":"v2 renamed the field"}`),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := codec.BuildCommit(tc.env, testAuthor(), nil); err != nil {
				t.Fatalf("BuildCommit rejected a shape a producer must still write: %v", err)
			}
		})
	}
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
	entries, err := spec.FS.ReadDir("schemas")
	if err != nil {
		t.Fatalf("read schemas dir: %v", err)
	}

	found := 0
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
		objectType, ok := pinnedObjectType(doc)
		if !ok {
			// Not a vocabulary: op-envelope, identifiers, anchor, resolution.
			continue
		}
		found++

		err = codec.ValidateBody(codec.Envelope{
			ObjectID:   "obj-1",
			ObjectType: objectType,
			OpType:     "create",
			OpVersion:  1,
			Body:       json.RawMessage(`{}`),
		})
		if err == nil {
			t.Errorf("object type %q has a vocabulary schema in spec/schemas/%s, but the codec validates ops of that type against nothing — register it in vocabularySchemaFiles",
				objectType, entry.Name())
		}
	}

	if found == 0 {
		t.Fatal("found no vocabulary schemas to check")
	}
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

// pinnedObjectType reports the object type a vocabulary schema fixes with a
// const, looking in the two places the shipped schemas put it: the top-level
// properties map, and the properties map of an allOf branch. A schema that
// pins its type anywhere else is not recognized, which fails the caller loudly
// rather than skipping the type quietly.
func pinnedObjectType(doc map[string]any) (string, bool) {
	if t, ok := constObjectType(doc); ok {
		return t, true
	}
	branches, ok := doc["allOf"].([]any)
	if !ok {
		return "", false
	}
	for _, branch := range branches {
		b, ok := branch.(map[string]any)
		if !ok {
			continue
		}
		if t, ok := constObjectType(b); ok {
			return t, true
		}
	}
	return "", false
}

func constObjectType(node map[string]any) (string, bool) {
	props, ok := node["properties"].(map[string]any)
	if !ok {
		return "", false
	}
	ot, ok := props["object_type"].(map[string]any)
	if !ok {
		return "", false
	}
	t, ok := ot["const"].(string)
	return t, ok
}
