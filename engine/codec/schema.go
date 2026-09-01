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

const (
	envelopeSchemaID    = schemaIDPrefix + "op-envelope.schema.json"
	identifiersSchemaID = schemaIDPrefix + "identifiers.schema.json"
	reviewOpsSchemaID   = schemaIDPrefix + "review-ops.schema.json"
	commentSchemaID     = schemaIDPrefix + "comment.schema.json"
	anchorSchemaID      = schemaIDPrefix + "anchor.schema.json"
)

type compiledSchemas struct {
	envelope  *jsonschema.Schema
	reviewOps *jsonschema.Schema
	comment   *jsonschema.Schema
}

var schemasOnce = sync.OnceValue(func() compiledSchemas {
	c := jsonschema.NewCompiler()

	schemaFiles := []struct {
		path string
		id   string
	}{
		{"schemas/op-envelope.schema.json", envelopeSchemaID},
		{"schemas/identifiers.schema.json", identifiersSchemaID},
		{"schemas/anchor.schema.json", anchorSchemaID},
		{"schemas/review-ops.schema.json", reviewOpsSchemaID},
		{"schemas/comment.schema.json", commentSchemaID},
	}

	for _, sf := range schemaFiles {
		raw, err := spec.FS.ReadFile(sf.path)
		if err != nil {
			panic(fmt.Sprintf("codec: read schema %s: %v", sf.path, err))
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			panic(fmt.Sprintf("codec: unmarshal schema %s: %v", sf.path, err))
		}
		if err := c.AddResource(sf.id, doc); err != nil {
			panic(fmt.Sprintf("codec: add schema resource %s: %v", sf.id, err))
		}
	}

	envSch, err := c.Compile(envelopeSchemaID)
	if err != nil {
		panic(fmt.Sprintf("codec: compile envelope schema: %v", err))
	}
	revSch, err := c.Compile(reviewOpsSchemaID)
	if err != nil {
		panic(fmt.Sprintf("codec: compile review-ops schema: %v", err))
	}
	comSch, err := c.Compile(commentSchemaID)
	if err != nil {
		panic(fmt.Sprintf("codec: compile comment schema: %v", err))
	}

	return compiledSchemas{
		envelope:  envSch,
		reviewOps: revSch,
		comment:   comSch,
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

// ValidateBody is an opt-in validator that checks an envelope's body against registered
// vocabulary schemas (review-ops, comment). Unknown object types or op types are ignored
// and return nil per forward compatibility rules.
func ValidateBody(env Envelope) error {
	schs := schemasOnce()
	var sch *jsonschema.Schema
	switch env.ObjectType {
	case "review":
		sch = schs.reviewOps
	case "comment":
		sch = schs.comment
	default:
		return nil
	}

	var raw []byte
	if len(env.Raw) > 0 {
		raw = env.Raw
	} else {
		var err error
		raw, err = EncodePayload(env)
		if err != nil {
			return err
		}
	}

	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return &RejectError{Reason: RejectSchemaViolation, Err: err}
	}
	if err := sch.Validate(inst); err != nil {
		return fmt.Errorf("codec: body validation failed: %w", err)
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
