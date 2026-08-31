package sshsig_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/writtendev/writ/engine/codec/sshsig"
)

func TestAllowedSigners_ParsingAndAuthorization(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub1, _ := ssh.NewPublicKey(pub1)
	pubLine1 := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub1)))

	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub2, _ := ssh.NewPublicKey(pub2)
	pubLine2 := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub2)))

	allowedSignersContent := `
# Comment line

alice@example.com ` + pubLine1 + ` alice-comment
*@company.test namespaces="git" ` + pubLine2 + `
dev@example.com namespaces="file" ` + pubLine1 + `
temporal@example.com valid-after="20260101000000",valid-before="20260102000000" ` + pubLine1 + `
untrusted@example.com cert-authority ` + pubLine1 + `
!blocked@example.com,*@example.com ` + pubLine2 + `
`

	ts, err := sshsig.ParseAllowedSigners(strings.NewReader(allowedSignersContent))
	if err != nil {
		t.Fatalf("ParseAllowedSigners failed: %v", err)
	}

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// 1. Direct match alice
	if !ts.IsAuthorized(sshPub1, "alice@example.com", "git", now) {
		t.Error("expected alice@example.com to be authorized with pub1")
	}

	// Wrong key for alice (pub3 is not in allowed_signers)
	pub3, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub3, _ := ssh.NewPublicKey(pub3)
	if ts.IsAuthorized(sshPub3, "alice@example.com", "git", now) {
		t.Error("alice@example.com should not be authorized with pub3")
	}

	// 2. Wildcard match on company.test
	if !ts.IsAuthorized(sshPub2, "bob@company.test", "git", now) {
		t.Error("expected bob@company.test to be authorized with pub2 under git")
	}
	// Wrong namespace for company.test
	if ts.IsAuthorized(sshPub2, "bob@company.test", "other", now) {
		t.Error("bob@company.test should not be authorized under namespace other")
	}

	// 3. Namespace mismatch on dev@example.com (only "file")
	if ts.IsAuthorized(sshPub1, "dev@example.com", "git", now) {
		// Note: alice@example.com rule does not match dev@example.com
		t.Error("dev@example.com should not be authorized under namespace git")
	}

	// 4. Temporal validity
	if !ts.IsAuthorized(sshPub1, "temporal@example.com", "git", now) {
		t.Error("temporal@example.com should be authorized at 2026-01-01 12:00:00")
	}
	beforeTime := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	if ts.IsAuthorized(sshPub1, "temporal@example.com", "git", beforeTime) {
		t.Error("temporal@example.com should not be authorized before valid-after")
	}
	afterTime := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	if ts.IsAuthorized(sshPub1, "temporal@example.com", "git", afterTime) {
		t.Error("temporal@example.com should not be authorized after valid-before")
	}

	// 5. cert-authority line skipped
	if ts.IsAuthorized(sshPub1, "untrusted@example.com", "git", now) {
		t.Error("untrusted@example.com should not be authorized (cert-authority skipped)")
	}

	// 6. Negated pattern !blocked@example.com,*@example.com
	if !ts.IsAuthorized(sshPub2, "user@example.com", "git", now) {
		t.Error("user@example.com should match *@example.com")
	}
	if ts.IsAuthorized(sshPub2, "blocked@example.com", "git", now) {
		t.Error("blocked@example.com should be rejected due to !blocked@example.com")
	}
}

func TestAllowedSigners_MalformedLines(t *testing.T) {
	malformedInputs := []string{
		"alice@example.com ssh-ed25519",                    // 2 fields, no options, missing key
		"alice@example.com namespaces=\"git\" ssh-ed25519", // 3 fields with options, missing key
		"alice@example.com",                                // 1 field
	}

	for _, input := range malformedInputs {
		_, err := sshsig.ParseAllowedSigners(strings.NewReader(input))
		if err == nil {
			t.Errorf("ParseAllowedSigners(%q) expected error, got nil", input)
		}
	}
}
