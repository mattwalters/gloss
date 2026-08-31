package sync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// buildEnv constructs the subprocess environment inheriting os.Environ(),
// forcing LC_ALL=C for locale-independent porcelain/error output, and appending
// any configured custom environment variables.
func (c *Client) buildEnv() []string {
	env := append(os.Environ(), "LC_ALL=C")
	if len(c.env) > 0 {
		env = append(env, c.env...)
	}
	return env
}

// runGit executes the git binary in the client's repository directory.
// It returns captured stdout, stderr, and any execution error.
func (c *Client) runGit(ctx context.Context, args ...string) ([]byte, []byte, error) {
	bin := c.gitBin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = c.repoDir
	cmd.Env = c.buildEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// classifyGitError constructs a *GitError from an exec failure, matching sentinels
// for known failure modes like non-fast-forward rejection or unknown remote.
func (c *Client) classifyGitError(remote string, args []string, err error, stderr []byte, stdout []byte) *GitError {
	stderrStr := string(stderr)
	stdoutStr := string(stdout)
	combined := stderrStr + "\n" + stdoutStr

	exitCode := -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	}

	var sentinel error
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		sentinel = err
	case strings.Contains(combined, "non-fast-forward") ||
		strings.Contains(combined, "[rejected]"):
		sentinel = ErrNonFastForward
	case strings.Contains(combined, "does not appear to be a git repository") ||
		strings.Contains(combined, "fatal: No such remote") ||
		strings.Contains(combined, "fatal: '"+remote+"' does not exist") ||
		(strings.Contains(combined, "remote") && strings.Contains(combined, "not found")):
		sentinel = ErrUnknownRemote
	default:
		sentinel = err
	}

	return &GitError{
		Remote:   remote,
		Args:     args,
		ExitCode: exitCode,
		Stderr:   strings.TrimSpace(stderrStr),
		Err:      sentinel,
	}
}
