package codec

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"golang.org/x/crypto/ssh"

	"github.com/writtendev/writ/engine/codec/sshsig"
)

// VerificationOutcome represents the outcome of verifying an op commit's signature.
type VerificationOutcome string

const (
	// OutcomeValid indicates the signature is cryptographically valid and the key is authorized in the trust store.
	OutcomeValid VerificationOutcome = "valid"

	// OutcomeUnsigned indicates the commit has no signature header.
	OutcomeUnsigned VerificationOutcome = "unsigned"

	// OutcomeWrongKey indicates the signature is cryptographically valid, but the key is not authorized for the author.
	OutcomeWrongKey VerificationOutcome = "wrong-key"

	// OutcomePayloadMutated indicates the signature does not match the commit payload bytes.
	OutcomePayloadMutated VerificationOutcome = "payload-mutated"

	// OutcomeCorruptedSignature indicates the signature header is malformed or unparseable.
	OutcomeCorruptedSignature VerificationOutcome = "corrupted-signature"
)

// Verification represents the structured outcome of validating an op commit's signature.
type Verification struct {
	Valid          bool                `json:"valid"`
	Outcome        VerificationOutcome `json:"outcome"`
	KeyFingerprint string              `json:"key_fingerprint,omitempty"`
	Principal      string              `json:"principal,omitempty"`
	Err            error               `json:"-"`
}

// TrustStore authorizes public keys for given principals, namespaces, and timestamps.
type TrustStore interface {
	IsAuthorized(pubKey ssh.PublicKey, principal, namespace string, when time.Time) bool
}

// LoadTrustStore loads an OpenSSH allowed_signers file from path into a TrustStore.
// If path is empty, it returns a nil TrustStore and no error.
func LoadTrustStore(filePath string) (*sshsig.TrustStore, error) {
	if filePath == "" {
		return nil, nil
	}
	return sshsig.ParseAllowedSignersFile(filePath)
}

// Verify verifies the signature on an op Commit against an explicit TrustStore.
// It is pure and does no I/O or subprocess execution.
func Verify(c Commit, ts TrustStore) Verification {
	if c.Signature == "" {
		return Verification{
			Valid:   false,
			Outcome: OutcomeUnsigned,
		}
	}

	sig, err := sshsig.ParseSignature(c.Signature)
	if err != nil {
		return Verification{
			Valid:   false,
			Outcome: OutcomeCorruptedSignature,
			Err:     err,
		}
	}

	fp := sshsig.KeyFingerprint(sig.PublicKey)
	principal := c.Author.Email

	payload := c.Payload
	if len(payload) == 0 {
		gitCommit, err := ToGitCommit(c)
		if err != nil {
			return Verification{
				Valid:          false,
				Outcome:        OutcomePayloadMutated,
				KeyFingerprint: fp,
				Principal:      principal,
				Err:            fmt.Errorf("codec: reconstruct payload: %w", err),
			}
		}
		payloadObj := &plumbing.MemoryObject{}
		if err := gitCommit.EncodeWithoutSignature(payloadObj); err != nil {
			return Verification{
				Valid:          false,
				Outcome:        OutcomePayloadMutated,
				KeyFingerprint: fp,
				Principal:      principal,
				Err:            fmt.Errorf("codec: encode without signature: %w", err),
			}
		}
		r, err := payloadObj.Reader()
		if err != nil {
			return Verification{
				Valid:          false,
				Outcome:        OutcomePayloadMutated,
				KeyFingerprint: fp,
				Principal:      principal,
				Err:            fmt.Errorf("codec: read payload: %w", err),
			}
		}
		p, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			return Verification{
				Valid:          false,
				Outcome:        OutcomePayloadMutated,
				KeyFingerprint: fp,
				Principal:      principal,
				Err:            fmt.Errorf("codec: read payload bytes: %w", err),
			}
		}
		payload = p
	}

	if err := sshsig.Verify(sig, payload, "git"); err != nil {
		return Verification{
			Valid:          false,
			Outcome:        OutcomePayloadMutated,
			KeyFingerprint: fp,
			Principal:      principal,
			Err:            err,
		}
	}

	if ts == nil || !ts.IsAuthorized(sig.PublicKey, principal, "git", c.Author.When) {
		return Verification{
			Valid:          false,
			Outcome:        OutcomeWrongKey,
			KeyFingerprint: fp,
			Principal:      principal,
			Err:            errors.New("codec: key not authorized for author principal in trust store"),
		}
	}

	return Verification{
		Valid:          true,
		Outcome:        OutcomeValid,
		KeyFingerprint: fp,
		Principal:      principal,
	}
}
