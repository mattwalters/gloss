package fixtures

import (
	"encoding/json"
	"fmt"

	"github.com/writtendev/writ/engine/codec/canonicaljson"
)

// OpDesc defines the structured payload of a Writ operation within a
// fixture commit. The generator canonicalizes it into op.json at mode
// 100644 and derives the commit message per the producer rules.
type OpDesc struct {
	ObjectID   string         `yaml:"object_id"`
	ObjectType string         `yaml:"object_type"`
	OpType     string         `yaml:"op_type"`
	OpVersion  uint64         `yaml:"op_version"`
	Body       any            `yaml:"body"`
	Extra      map[string]any `yaml:",inline"`
}

// BuildOpPayload canonicalizes the op description into byte-stable JSON
// per spec/canonicalization.md and spec/op-envelope.md.
func BuildOpPayload(op *OpDesc) ([]byte, error) {
	if op == nil {
		return nil, fmt.Errorf("fixtures: op is nil")
	}

	payloadMap := make(map[string]any)
	for k, v := range op.Extra {
		payloadMap[k] = v
	}

	payloadMap["object_id"] = op.ObjectID
	payloadMap["object_type"] = op.ObjectType
	payloadMap["op_type"] = op.OpType
	payloadMap["op_version"] = op.OpVersion
	if op.Body != nil {
		payloadMap["body"] = op.Body
	} else {
		payloadMap["body"] = map[string]any{}
	}

	raw, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("fixtures: marshal op payload: %w", err)
	}

	canon, err := canonicaljson.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("fixtures: canonicalize op payload: %w", err)
	}

	return canon, nil
}

// DeriveMessage derives the commit message for an op commit per the
// producer rules in spec/op-envelope.md: `writ: <op_type> <object_type>/<object_id>\n`.
func DeriveMessage(op *OpDesc) string {
	if op == nil {
		return ""
	}
	return fmt.Sprintf("writ: %s %s/%s\n", op.OpType, op.ObjectType, op.ObjectID)
}
