package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine/codec/canonicaljson"
)

// Message derives the canonical git commit message for an op envelope:
// writ: <op_type> <object_type>/<object_id>\n
func Message(env Envelope) string {
	return fmt.Sprintf("writ: %s %s/%s\n", env.OpType, env.ObjectType, env.ObjectID)
}

// EncodePayload serializes an Envelope to canonical JSON bytes per spec/canonicalization.md
// and spec/op-envelope.md. Unknown top-level fields are preserved. The resulting bytes
// are verified against the canonical fixed-point requirement and the envelope schema.
func EncodePayload(env Envelope) ([]byte, error) {
	m := make(map[string]any, len(env.Unknown)+5)
	for k, v := range env.Unknown {
		m[k] = v
	}
	m["object_id"] = env.ObjectID
	m["object_type"] = env.ObjectType
	m["op_type"] = env.OpType
	m["op_version"] = env.OpVersion
	if len(env.Body) == 0 {
		m["body"] = json.RawMessage("{}")
	} else {
		m["body"] = env.Body
	}

	intermediate, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("codec: marshal payload intermediate: %w", err)
	}

	canon, err := canonicaljson.Marshal(intermediate)
	if err != nil {
		return nil, fmt.Errorf("codec: canonicalize payload: %w", err)
	}

	// Self-check byte-equality rule (canonical fixed-point check)
	recanon, err := canonicaljson.Marshal(canon)
	if err != nil || !bytes.Equal(recanon, canon) {
		return nil, errors.New("codec: payload failed canonical fixed-point check")
	}

	// Self-check schema validity
	if err := ValidateEnvelope(canon); err != nil {
		return nil, fmt.Errorf("codec: encoded payload failed schema validation: %w", err)
	}

	return canon, nil
}

// BuildCommit constructs an unsigned Commit for an op, with a single op.json entry
// at mode 100644, committer byte-identical to author, and timestamp recorded in UTC (+0000).
//
// BuildCommit is the producer boundary, so it is where the payload is checked
// against the vocabulary schema for its object type (spec/op-envelope.md
// §Producer validation). The check belongs here and not in EncodePayload:
// EncodePayload is also on the read path — the projection re-encodes ops it
// fetched from the log whose raw bytes it did not keep — and a foreign op writ
// reads perfectly well today must keep projecting.
//
// The cost is one JSON Schema validation per appended op, on the same order as
// the canonical encoding directly above it and orders of magnitude below the
// signature and the object write that follow on the same path
// (BenchmarkProducerPath measures the three). It is paid once per write and
// never on a read, which is the trade this check is worth.
func BuildCommit(env Envelope, author Identity, parents []string) (*Commit, error) {
	raw, err := EncodePayload(env)
	if err != nil {
		return nil, fmt.Errorf("codec: build commit payload: %w", err)
	}

	if err := validateBody(env.ObjectType, raw); err != nil {
		return nil, fmt.Errorf("codec: build commit body: %w", err)
	}

	utcAuthor := Identity{
		Name:  author.Name,
		Email: author.Email,
		When:  author.When.UTC(),
	}

	c := &Commit{
		Parents:   parents,
		Author:    utcAuthor,
		Committer: utcAuthor,
		Message:   Message(env),
		Tree: []TreeEntry{
			{
				Name: "op.json",
				Mode: "100644",
				Data: raw,
			},
		},
	}

	gitCommit, err := ToGitCommit(*c)
	if err == nil {
		c.ID = gitCommit.Hash.String()
		payloadObj := &plumbing.MemoryObject{}
		if err := gitCommit.EncodeWithoutSignature(payloadObj); err == nil {
			if r, err := payloadObj.Reader(); err == nil {
				payload, _ := io.ReadAll(r)
				_ = r.Close()
				c.Payload = payload
			}
		}
	}

	return c, nil
}
