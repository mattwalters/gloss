package codec

import "fmt"

// RejectReason represents the machine-readable reason an op commit or payload is rejected.
type RejectReason string

const (
	RejectMissingOpJSON       RejectReason = "missing-op-json"
	RejectExtraTreeEntry      RejectReason = "extra-tree-entry"
	RejectOpJSONSubdirectory  RejectReason = "op-json-subdirectory"
	RejectInvalidOpJSONMode   RejectReason = "invalid-op-json-mode"
	RejectCommitterMismatch   RejectReason = "committer-mismatch"
	RejectNonCanonicalPayload RejectReason = "non-canonical-payload"
	RejectDuplicateKey        RejectReason = "duplicate-key"
	RejectLoneSurrogate       RejectReason = "lone-surrogate"
	RejectSchemaViolation     RejectReason = "schema-violation"
)

// RejectError is returned when an op commit or payload fails reader validation.
type RejectError struct {
	Reason RejectReason
	Err    error
}

func (e *RejectError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("codec: reject %s: %v", e.Reason, e.Err)
	}
	return fmt.Sprintf("codec: reject %s", e.Reason)
}

func (e *RejectError) Unwrap() error {
	return e.Err
}
