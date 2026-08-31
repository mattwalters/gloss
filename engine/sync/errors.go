package sync

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrNonFastForward indicates that a push or fetch update was rejected because
	// it was not a fast-forward.
	ErrNonFastForward = errors.New("non-fast-forward update rejected")

	// ErrUnknownRemote indicates that the requested git remote is not configured
	// or could not be found.
	ErrUnknownRemote = errors.New("unknown remote")
)

// GitError records a failure when executing a system git command.
type GitError struct {
	Remote   string
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *GitError) Error() string {
	var parts []string
	if e.Remote != "" {
		parts = append(parts, fmt.Sprintf("git (remote %s) %s (exit %d)", e.Remote, strings.Join(e.Args, " "), e.ExitCode))
	} else {
		parts = append(parts, fmt.Sprintf("git %s (exit %d)", strings.Join(e.Args, " "), e.ExitCode))
	}
	if e.Err != nil && !errors.Is(e.Err, e) {
		parts = append(parts, e.Err.Error())
	}
	if e.Stderr != "" {
		trimmed := strings.TrimSpace(e.Stderr)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, ": ")
}

// Unwrap returns the underlying classified error (such as ErrNonFastForward or ErrUnknownRemote), if any.
func (e *GitError) Unwrap() error {
	return e.Err
}
