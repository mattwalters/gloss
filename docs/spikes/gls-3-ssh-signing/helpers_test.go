// Helpers shared by the SSH signing interop tests in this spike. Every
// cryptographic operation shells out to system ssh-keygen — go-git has no
// SSH signature support (it only signs/verifies PGP; see sshsign_test.go
// and README.md for why that's the finding, not a workaround we're hiding).
package sshsignspike

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runGit runs git in dir and fails the test on error, returning combined output.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// genSSHKeyPair generates an ed25519 keypair under dir/name (and
// dir/name.pub) with no passphrase, returning both paths.
func genSSHKeyPair(t *testing.T, dir, name string) (privPath, pubPath string) {
	t.Helper()
	privPath = filepath.Join(dir, name)
	pubPath = privPath + ".pub"
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", name, "-f", privPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen -t ed25519: %v\n%s", err, out)
	}
	return privPath, pubPath
}

// readPubKeyLine returns the trimmed contents of an SSH public key file
// ("<type> <base64> <comment>"), the line format allowed_signers expects
// after a principal.
func readPubKeyLine(t *testing.T, pubPath string) string {
	t.Helper()
	b, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("read %s: %v", pubPath, err)
	}
	return strings.TrimSpace(string(b))
}

// writeAllowedSigners writes a git-format allowed_signers file mapping
// principal to the key at pubPath, returning the file's path.
func writeAllowedSigners(t *testing.T, dir, principal, pubPath string) string {
	t.Helper()
	line := principal + " " + readPubKeyLine(t, pubPath) + "\n"
	path := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write allowed_signers: %v", err)
	}
	return path
}

// signSSH signs payload with `ssh-keygen -Y sign`, using keyFile as the -f
// argument (a private key, or a public key when the private half lives only
// in an ssh-agent — see agentSign in sshsign_test.go). extraEnv is appended
// to the current process environment, e.g. to point at an agent socket.
func signSSH(t *testing.T, keyFile, namespace string, payload []byte, extraEnv []string) []byte {
	t.Helper()
	dataFile := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(dataFile, payload, 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	cmd := exec.Command("ssh-keygen", "-Y", "sign", "-f", keyFile, "-n", namespace, dataFile)
	cmd.Env = append(os.Environ(), extraEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen -Y sign: %v\n%s", err, out)
	}
	sig, err := os.ReadFile(dataFile + ".sig")
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	return sig
}

// verifySSH verifies payload against sig via `ssh-keygen -Y verify`,
// returning an error (rather than failing the test) so callers can assert
// on both success and expected-failure cases.
func verifySSH(t *testing.T, allowedSigners, principal, namespace string, payload, sig []byte) error {
	t.Helper()
	sigFile := filepath.Join(t.TempDir(), "sig")
	if err := os.WriteFile(sigFile, sig, 0o600); err != nil {
		t.Fatalf("write signature: %v", err)
	}
	cmd := exec.Command("ssh-keygen", "-Y", "verify",
		"-f", allowedSigners, "-I", principal, "-n", namespace, "-s", sigFile)
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &verifyError{out: string(out), err: err}
	}
	return nil
}

type verifyError struct {
	out string
	err error
}

func (e *verifyError) Error() string { return e.err.Error() + ": " + e.out }

// startSSHAgent launches a fresh `ssh-agent`, returning its auth socket and
// a cleanup func that kills it. Isolated per-test so tests can run in
// parallel without sharing agent state.
func startSSHAgent(t *testing.T) (sock string) {
	t.Helper()
	cmd := exec.Command("ssh-agent", "-s")
	// -s already forces Bourne-shell output regardless of $SHELL, but pin it
	// explicitly so the regexes below can't silently stop matching on a
	// system where that guess ever changes.
	cmd.Env = append(os.Environ(), "SHELL=/bin/sh")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ssh-agent -s: %v", err)
	}
	sockRe := regexp.MustCompile(`SSH_AUTH_SOCK=([^;]+);`)
	pidRe := regexp.MustCompile(`SSH_AGENT_PID=([^;]+);`)
	sockMatch := sockRe.FindSubmatch(out)
	pidMatch := pidRe.FindSubmatch(out)
	if sockMatch == nil || pidMatch == nil {
		t.Fatalf("could not parse ssh-agent -s output: %s", out)
	}
	sock = string(sockMatch[1])
	pid := string(pidMatch[1])
	t.Cleanup(func() {
		cmd := exec.Command("ssh-agent", "-k")
		cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock, "SSH_AGENT_PID="+pid)
		_ = cmd.Run()
	})
	return sock
}

// agentAdd adds privPath's key to the agent at sock.
func agentAdd(t *testing.T, sock, privPath string) {
	t.Helper()
	cmd := exec.Command("ssh-add", privPath)
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-add: %v\n%s", err, out)
	}
}
