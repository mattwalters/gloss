package codec

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/writtendev/writ/spec"
)

// schemaIDPrefix is the schema identity convention from WRIT-73: every
// schema in spec/schemas/ declares a $id of prefix + filename.
const schemaIDPrefix = "https://writ.dev/spec/"

// envelopeSchemaFile is the payload schema every op satisfies, whatever its
// object type.
const envelopeSchemaFile = "op-envelope.schema.json"

// supportSchemaFiles are compiled as resources only: the vocabularies $ref
// them, but they are not themselves op payload schemas.
var supportSchemaFiles = []string{
	"identifiers.schema.json",
	"anchor.schema.json",
}

// vocabularySchemaFiles maps an object type to the vocabulary schema that
// governs ops on it. It is the producer's registry: every object type writ
// can emit MUST appear here, or ops of that type are signed against nothing.
//
// The map is exhaustive over the vocabularies shipped in spec/schemas/, and
// TestEveryShippedVocabularyIsValidated fails when a schema file is added
// without an entry — so "writ ships a schema for this type but never wired it
// up" cannot recur silently. That is what lets a lookup miss below mean one
// thing only: an object type this implementation has never heard of.
var vocabularySchemaFiles = map[string]string{
	"review":  "review-ops.schema.json",
	"comment": "comment.schema.json",
	"issue":   "issue-ops.schema.json",
	"project": "project-ops.schema.json",
	"cycle":   "cycle-ops.schema.json",
	"repo":    "repo-ops.schema.json",
}

type compiledSchemas struct {
	envelope *jsonschema.Schema
	vocab    map[string]*jsonschema.Schema
}

var schemasOnce = sync.OnceValue(func() compiledSchemas {
	c := jsonschema.NewCompiler()

	add := func(file string) {
		raw, err := spec.FS.ReadFile("schemas/" + file)
		if err != nil {
			panic(fmt.Sprintf("codec: read schema %s: %v", file, err))
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			panic(fmt.Sprintf("codec: unmarshal schema %s: %v", file, err))
		}
		if err := c.AddResource(schemaIDPrefix+file, doc); err != nil {
			panic(fmt.Sprintf("codec: add schema resource %s: %v", file, err))
		}
	}
	compile := func(file string) *jsonschema.Schema {
		sch, err := c.Compile(schemaIDPrefix + file)
		if err != nil {
			panic(fmt.Sprintf("codec: compile schema %s: %v", file, err))
		}
		return sch
	}

	add(envelopeSchemaFile)
	for _, file := range supportSchemaFiles {
		add(file)
	}
	for _, file := range vocabularySchemaFiles {
		add(file)
	}

	vocab := make(map[string]*jsonschema.Schema, len(vocabularySchemaFiles))
	for objectType, file := range vocabularySchemaFiles {
		vocab[objectType] = compile(file)
	}

	return compiledSchemas{
		envelope: compile(envelopeSchemaFile),
		vocab:    vocab,
	}
})

// ValidateEnvelope validates raw JSON payload bytes against the op-envelope schema.
func ValidateEnvelope(raw []byte) error {
	sch := schemasOnce().envelope
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return &RejectError{Reason: RejectSchemaViolation, Err: err}
	}
	if err := sch.Validate(inst); err != nil {
		return &RejectError{Reason: RejectSchemaViolation, Err: err}
	}
	return nil
}

// ValidateBody checks an envelope's payload against the vocabulary schema
// registered for its object type. This is the producer's obligation from
// spec/op-envelope.md §Producer validation, and BuildCommit calls it, so no op
// writ appends is signed without passing through here.
//
// An object type with no registered vocabulary passes. That is forward
// compatibility, not a hole: a type this implementation has never heard of is
// something a reader must tolerate (spec/forward-compatibility.md) and
// something writ's own producer never emits — every type it does emit is in
// vocabularySchemaFiles, which is tested exhaustive over spec/schemas/.
func ValidateBody(env Envelope) error {
	sch, ok := schemasOnce().vocab[env.ObjectType]
	if !ok {
		return nil
	}
	raw := env.Raw
	if len(raw) == 0 {
		var err error
		raw, err = EncodePayload(env)
		if err != nil {
			return err
		}
	}
	return validateAgainst(sch, raw)
}

// validateBody validates payload bytes that are already encoded, so the append
// path does not canonicalize the same envelope twice.
func validateBody(objectType string, raw []byte) error {
	sch, ok := schemasOnce().vocab[objectType]
	if !ok {
		return nil
	}
	return validateAgainst(sch, raw)
}

func validateAgainst(sch *jsonschema.Schema, raw []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return &RejectError{Reason: RejectSchemaViolation, Err: err}
	}
	if err := sch.Validate(inst); err != nil {
		return &RejectError{Reason: RejectSchemaViolation, Err: err}
	}
	return nil
}

// Disposition represents whether an op is interpretable or opaque to a reader.
type Disposition string

const (
	DispositionInterpretable Disposition = "interpretable"
	DispositionOpaque        Disposition = "opaque"
)

// KnownOp defines an (object_type, op_type) pair and its supported versions.
type KnownOp struct {
	ObjectType string  `json:"object_type"`
	OpType     string  `json:"op_type"`
	Versions   []int64 `json:"versions"`
}

// Profile represents a reader capability profile defining known operations.
type Profile struct {
	Name     string    `json:"profile,omitempty"`
	KnownOps []KnownOp `json:"known_ops"`
}

// Classify determines whether an envelope is interpretable or opaque under the profile.
func (p Profile) Classify(env Envelope) Disposition {
	for _, k := range p.KnownOps {
		if k.ObjectType == env.ObjectType && k.OpType == env.OpType {
			for _, v := range k.Versions {
				if v == env.OpVersion {
					return DispositionInterpretable
				}
			}
			return DispositionOpaque
		}
	}
	return DispositionOpaque
}
