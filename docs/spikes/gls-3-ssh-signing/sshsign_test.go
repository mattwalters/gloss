// Package sshsignspike is the GLS-3 spike prototype: can we sign gloss ops
// with a user's existing SSH key via go-git, and verify portably against
// system git and vice versa? See README.md for the findings this prototype
// backs; this file is the evidence, not documentation of intent.
package sshsignspike

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const (
	namespace = "git" // the reserved namespace git itself uses for commit signing
	principal = "spike@example.com"
)

// TestSignInGo_VerifyWithSystemGit builds an SSH-signed commit entirely
// through go-git (construct, sign, write the object, move the branch) and
// checks system `git verify-commit` accepts it. go-git has no SSH signer,
// so signing shells out to ssh-keygen -Y sign; go-git's role is
// constructing the exact byte-stable payload and writing the resulting
// object into its own store.
func TestSignInGo_VerifyWithSystemGit(t *testing.T) {
	tmp := t.TempDir()

	keyDir := filepath.Join(tmp, "keys")
	if err := os.Mkdir(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	priv, pub := genSSHKeyPair(t, keyDir, "id_signer")

	repoDir := filepath.Join(tmp, "repo")
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}

	// An empty tree is enough to exercise signing; the spike isn't about
	// tree/blob construction.
	treeObj := repo.Storer.NewEncodedObject()
	treeObj.SetType(plumbing.TreeObject)
	if err := (&object.Tree{}).Encode(treeObj); err != nil {
		t.Fatalf("encode empty tree: %v", err)
	}
	treeHash, err := repo.Storer.SetEncodedObject(treeObj)
	if err != nil {
		t.Fatalf("store tree: %v", err)
	}

	sig := object.Signature{Name: "Gloss Spike", Email: principal, When: time.Now()}
	commit := &object.Commit{
		Author:    sig,
		Committer: sig,
		Message:   "gls-3 spike: sign-in-go\n",
		TreeHash:  treeHash,
	}

	// The payload git (and ssh-keygen -Y sign) signs is the commit encoding
	// with every header except gpgsig — this is exactly what
	// EncodeWithoutSignature produces.
	payloadObj := repo.Storer.NewEncodedObject()
	if err := commit.EncodeWithoutSignature(payloadObj); err != nil {
		t.Fatalf("EncodeWithoutSignature: %v", err)
	}
	payloadReader, err := payloadObj.Reader()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(payloadReader)
	if err != nil {
		t.Fatal(err)
	}

	armored := signSSH(t, priv, namespace, payload, nil)
	commit.PGPSignature = string(armored)

	commitObj := repo.Storer.NewEncodedObject()
	commitObj.SetType(plumbing.CommitObject)
	if err := commit.Encode(commitObj); err != nil {
		t.Fatalf("Encode (with signature): %v", err)
	}
	commitHash, err := repo.Storer.SetEncodedObject(commitObj)
	if err != nil {
		t.Fatalf("store commit: %v", err)
	}

	branch := plumbing.NewBranchReferenceName("main")
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branch, commitHash)); err != nil {
		t.Fatalf("set branch ref: %v", err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branch)); err != nil {
		t.Fatalf("set HEAD: %v", err)
	}

	allowedSigners := writeAllowedSigners(t, tmp, principal, pub)
	runGit(t, repoDir, "config", "gpg.format", "ssh")
	runGit(t, repoDir, "config", "gpg.ssh.allowedSignersFile", allowedSigners)

	out := runGit(t, repoDir, "verify-commit", commitHash.String())
	if !strings.Contains(out, "Good \"git\" signature") {
		t.Fatalf("verify-commit did not report a good signature:\n%s", out)
	}
}

// TestSignWithSystemGit_VerifyInGo does the reverse: system git creates the
// signed commit (normal `git commit -S` with gpg.format=ssh), and this test
// reads it back through go-git, reconstructs the signed payload with
// EncodeWithoutSignature, and verifies the extracted signature. go-git's
// Commit.Verify only understands PGP (it calls straight into
// ProtonMail/go-crypto/openpgp — see README.md finding #1), so "verify in
// Go" here means the Go program drives the verification, delegating the
// actual signature check to ssh-keygen -Y verify the same way it would to
// any other crypto primitive it doesn't implement itself.
func TestSignWithSystemGit_VerifyInGo(t *testing.T) {
	tmp := t.TempDir()

	keyDir := filepath.Join(tmp, "keys")
	if err := os.Mkdir(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_, pub := genSSHKeyPair(t, keyDir, "id_signer")

	repoDir := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "init", "-q")
	runGit(t, repoDir, "config", "user.name", "Gloss Spike")
	runGit(t, repoDir, "config", "user.email", principal)
	runGit(t, repoDir, "config", "gpg.format", "ssh")
	runGit(t, repoDir, "config", "user.signingkey", pub)

	if err := os.WriteFile(filepath.Join(repoDir, "op.txt"), []byte("gls-3 spike op\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", "op.txt")
	runGit(t, repoDir, "commit", "-q", "-S", "-m", "gls-3 spike: sign-with-system-git")

	headOut := runGit(t, repoDir, "rev-parse", "HEAD")
	commitHash := plumbing.NewHash(strings.TrimSpace(headOut))

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	commit, err := repo.CommitObject(commitHash)
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if commit.PGPSignature == "" {
		t.Fatal("go-git did not decode a signature off the system-git commit")
	}

	payloadObj := repo.Storer.NewEncodedObject()
	if err := commit.EncodeWithoutSignature(payloadObj); err != nil {
		t.Fatalf("EncodeWithoutSignature: %v", err)
	}
	payloadReader, err := payloadObj.Reader()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(payloadReader)
	if err != nil {
		t.Fatal(err)
	}

	allowedSigners := writeAllowedSigners(t, tmp, principal, pub)
	if err := verifySSH(t, allowedSigners, principal, namespace, payload, []byte(commit.PGPSignature)); err != nil {
		t.Fatalf("verifySSH: %v", err)
	}
}

// TestAgentHeldKeySigning exercises signing when the private key never
// touches disk in this process: it's generated, loaded into ssh-agent, and
// the private key file is then deleted, leaving only the public key. This
// is the shape real usage takes when the key is agent-forwarded or
// hardware-backed (YubiKey, Secure Enclave) — the case the ticket calls out
// because a design that assumes a readable private-key file on disk breaks
// for it.
func TestAgentHeldKeySigning(t *testing.T) {
	tmp := t.TempDir()
	sock := startSSHAgent(t)

	keyDir := filepath.Join(tmp, "keys")
	if err := os.Mkdir(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	priv, pub := genSSHKeyPair(t, keyDir, "id_signer")
	agentAdd(t, sock, priv)

	if err := os.Remove(priv); err != nil {
		t.Fatalf("remove private key: %v", err)
	}
	if _, err := os.Stat(priv); !os.IsNotExist(err) {
		t.Fatalf("private key still on disk: %v", err)
	}

	payload := []byte("gls-3 spike: agent-held key\n")
	armored := signSSH(t, pub, namespace, payload, []string{"SSH_AUTH_SOCK=" + sock})

	allowedSigners := writeAllowedSigners(t, tmp, principal, pub)
	if err := verifySSH(t, allowedSigners, principal, namespace, payload, armored); err != nil {
		t.Fatalf("verifySSH: %v", err)
	}
}
