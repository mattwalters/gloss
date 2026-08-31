package fixtures

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Verifier manages an allowed-signers trust store constructed from the
// embedded fixture public keys (keys/*.pub) and executes ssh-keygen -Y verify
// against commit payloads (WRIT-3 finding 3: explicit trust store, never repo git config).
type Verifier struct {
	dir                string
	allowedSignersFile string
	keyFingerprints    map[string]string // identity -> fingerprint
}

// NewVerifier extracts the embedded public keys and generates an allowed_signers
// file in a temporary directory.
func NewVerifier() (*Verifier, error) {
	dir, err := os.MkdirTemp("", "writ-fixture-verify-")
	if err != nil {
		return nil, fmt.Errorf("fixtures: create verify scratch dir: %w", err)
	}

	var allowedSignersBuf strings.Builder
	fingerprints := make(map[string]string)

	entries, err := keyFS.ReadDir("keys")
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("fixtures: read embedded keys: %w", err)
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		data, err := keyFS.ReadFile(filepath.Join("keys", e.Name()))
		if err != nil {
			os.RemoveAll(dir)
			return nil, fmt.Errorf("fixtures: read embedded pubkey %s: %w", e.Name(), err)
		}
		pubLine := strings.TrimSpace(string(data))
		// e.g. alice_ed25519.pub -> alice
		idName := strings.TrimSuffix(e.Name(), "_ed25519.pub")
		id, ok := identities[idName]
		if ok {
			allowedSignersBuf.WriteString(fmt.Sprintf("%s %s\n", id.Email, pubLine))
			fingerprints[idName] = FingerprintFromPubKeyLine(pubLine)
		}
	}

	allowedSignersPath := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowedSignersPath, []byte(allowedSignersBuf.String()), 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("fixtures: write allowed_signers: %w", err)
	}

	return &Verifier{
		dir:                dir,
		allowedSignersFile: allowedSignersPath,
		keyFingerprints:    fingerprints,
	}, nil
}

// Close cleans up the temporary allowed_signers directory.
func (v *Verifier) Close() {
	os.RemoveAll(v.dir)
}

// VerificationResult summarizes the outcome of verifying an op commit's signature.
type VerificationResult struct {
	Valid          bool   `json:"valid"`
	Outcome        string `json:"outcome"`
	KeyFingerprint string `json:"key_fingerprint,omitempty"`
	Error          string `json:"error,omitempty"`
}

// VerifyCommit extracts the commit payload and signature and verifies it via
// ssh-keygen -Y verify using authorEmail as the principal.
func (v *Verifier) VerifyCommit(commit *object.Commit, authorIdentity, authorEmail string) (*VerificationResult, error) {
	if commit.PGPSignature == "" {
		return &VerificationResult{
			Valid:   false,
			Outcome: "unsigned",
		}, nil
	}

	fp := FingerprintFromSignature(commit.PGPSignature)

	// Reconstruct the signed payload: commit encoding without signature
	payloadObj := &plumbing.MemoryObject{}
	if err := commit.EncodeWithoutSignature(payloadObj); err != nil {
		return nil, fmt.Errorf("fixtures: encode commit without signature: %w", err)
	}
	r, err := payloadObj.Reader()
	if err != nil {
		return nil, fmt.Errorf("fixtures: read commit payload: %w", err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("fixtures: read commit payload: %w", err)
	}

	sigFile := filepath.Join(v.dir, "sig.tmp")
	if err := os.WriteFile(sigFile, []byte(commit.PGPSignature), 0o600); err != nil {
		return nil, fmt.Errorf("fixtures: write temp signature file: %w", err)
	}

	cmd := exec.Command("ssh-keygen", "-Y", "verify",
		"-f", v.allowedSignersFile,
		"-I", authorEmail,
		"-n", sshNamespace,
		"-s", sigFile)
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return &VerificationResult{
			Valid:          true,
			Outcome:        "valid",
			KeyFingerprint: fp,
		}, nil
	}

	outStr := string(out)
	outcome := "payload-mutated"

	if strings.Contains(outStr, "Couldn't parse signature") || strings.Contains(outStr, "invalid format") || fp == "" {
		outcome = "corrupted-signature"
	} else if authorIdentity != "" && v.keyFingerprints[authorIdentity] != "" && fp != v.keyFingerprints[authorIdentity] {
		outcome = "wrong-key"
	} else if strings.Contains(outStr, "key not found") || strings.Contains(outStr, "no principal matched") || strings.Contains(outStr, "Key not allowed") {
		outcome = "wrong-key"
	}

	return &VerificationResult{
		Valid:          false,
		Outcome:        outcome,
		KeyFingerprint: fp,
		Error:          strings.TrimSpace(outStr),
	}, nil
}

// KeyFingerprint returns the SHA256 fingerprint for the given raw public key wire bytes ("SHA256:...").
func KeyFingerprint(pubKeyBytes []byte) string {
	h := sha256.Sum256(pubKeyBytes)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:])
}

// FingerprintFromPubKeyLine computes the fingerprint from a standard OpenSSH public key line ("ssh-ed25519 AAAA... comment").
func FingerprintFromPubKeyLine(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return ""
	}
	return KeyFingerprint(raw)
}

// FingerprintFromSignature parses the embedded public key from an armored SSHSIG string and returns its fingerprint.
func FingerprintFromSignature(armoredSig string) string {
	var b64 strings.Builder
	inArmor := false
	for _, line := range strings.Split(armoredSig, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-----BEGIN SSH SIGNATURE-----") {
			inArmor = true
			continue
		}
		if strings.HasPrefix(line, "-----END SSH SIGNATURE-----") {
			break
		}
		if inArmor {
			b64.WriteString(line)
		}
	}
	if b64.Len() == 0 {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil || len(raw) < 14 || string(raw[:6]) != "SSHSIG" {
		return ""
	}
	pubKeyLen := int(binary.BigEndian.Uint32(raw[10:14]))
	if len(raw) < 14+pubKeyLen {
		return ""
	}
	pubKeyBytes := raw[14 : 14+pubKeyLen]
	return KeyFingerprint(pubKeyBytes)
}
