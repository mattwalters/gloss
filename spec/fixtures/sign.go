package fixtures

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed keys/*_ed25519 keys/*_ed25519.pub
var keyFS embed.FS

// sshNamespace is the reserved namespace git itself uses for commit
// signing, so op-commits signed this way stay verifiable with plain
// `git verify-commit`, not just gloss's own code.
const sshNamespace = "git"

// signer signs commit payloads with ssh-keygen -Y sign. go-git has no SSH
// signing support (only PGP), so this shells out — the same boring,
// direct approach the house rules ask for anywhere git already does the
// job. ed25519 signatures are deterministic (RFC 8032), which is what
// lets a signed commit's SHA be reproducible across regenerations; an
// RSA or ECDSA key here would break byte-determinism.
type signer struct {
	keyDir string // scratch dir holding 0600 copies of the embedded private keys
}

// newSigner extracts the embedded keyring into a fresh 0600-permissioned
// temp directory. ssh-keygen refuses to sign with a private key file that
// isn't private (0600 or tighter), and a key checked out of git normally
// lands at the umask's default (0644) since git only tracks the
// executable bit — so every signing call needs its own known-good copy
// rather than trusting the working tree's permissions.
func newSigner() (*signer, error) {
	dir, err := os.MkdirTemp("", "gloss-fixture-keys-")
	if err != nil {
		return nil, fmt.Errorf("fixtures: create key scratch dir: %w", err)
	}
	entries, err := keyFS.ReadDir("keys")
	if err != nil {
		return nil, fmt.Errorf("fixtures: read embedded keyring: %w", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".pub" {
			continue // signing only needs the private half
		}
		data, err := keyFS.ReadFile(filepath.Join("keys", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("fixtures: read embedded key %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o600); err != nil {
			return nil, fmt.Errorf("fixtures: extract embedded key %s: %w", e.Name(), err)
		}
	}
	return &signer{keyDir: dir}, nil
}

func (s *signer) close() {
	os.RemoveAll(s.keyDir)
}

// sign returns an armored SSH signature over payload, produced by the
// named identity's key.
func (s *signer) sign(id identity, payload []byte) (string, error) {
	keyPath := filepath.Join(s.keyDir, id.KeyFile)
	if _, err := os.Stat(keyPath); err != nil {
		return "", fmt.Errorf("fixtures: signing key for %s: %w", id.Name, err)
	}

	scratch, err := os.MkdirTemp("", "gloss-fixture-sign-")
	if err != nil {
		return "", fmt.Errorf("fixtures: create sign scratch dir: %w", err)
	}
	defer os.RemoveAll(scratch)

	dataFile := filepath.Join(scratch, "payload")
	if err := os.WriteFile(dataFile, payload, 0o600); err != nil {
		return "", fmt.Errorf("fixtures: write signing payload: %w", err)
	}

	cmd := exec.Command("ssh-keygen", "-Y", "sign", "-f", keyPath, "-n", sshNamespace, dataFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("fixtures: ssh-keygen -Y sign: %w\n%s", err, out)
	}

	sig, err := os.ReadFile(dataFile + ".sig")
	if err != nil {
		return "", fmt.Errorf("fixtures: read signature: %w", err)
	}
	return string(sig), nil
}
