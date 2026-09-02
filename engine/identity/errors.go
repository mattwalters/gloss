package identity

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrMissing indicates a required git configuration key is unset or empty.
	ErrMissing = errors.New("missing configuration")

	// ErrInvalid indicates a git configuration key has an invalid value.
	ErrInvalid = errors.New("invalid configuration")

	// ErrUnsupportedFormat indicates a git configuration key specifies an unsupported format.
	ErrUnsupportedFormat = errors.New("unsupported format")
)

// ConfigError records a failure while reading or validating git configuration.
type ConfigError struct {
	Key     string
	Value   string
	Problem error
}

// initHint is the remediation ConfigError.Error appends to the two problems a
// user fixes by configuring something. It is correct for every emitter except
// one: writ init prints these errors itself, and telling the reader to run the
// command they are running is circular. Worse, it implies init failed at
// something it never attempts — writ does not write signing configuration for
// anyone; it prints the git config lines and expects the user to run them.
// Message renders the same text without this clause for that caller.
const initHint = " (run 'writ init' to configure)"

func (e *ConfigError) Error() string {
	return e.message(initHint)
}

// Message returns the same text as Error without the "run 'writ init' to
// configure" remediation. It is for writ init, which is the one emitter the
// remediation cannot help — it prints the git config lines to run directly
// below. Every other caller wants Error.
func (e *ConfigError) Message() string {
	return e.message("")
}

func (e *ConfigError) message(hint string) string {
	if errors.Is(e.Problem, ErrMissing) {
		// Carry e.Problem's detail through, the way the ErrInvalid and
		// ErrUnsupportedFormat arms below already do. Short-circuiting on the
		// sentinel discarded whatever the caller wrapped into it, which made
		// guidance like "set writ.personId (for example user:alice)"
		// unreachable at exactly the moment a user needed it.
		detail := strings.TrimSpace(strings.TrimPrefix(e.Problem.Error(), ErrMissing.Error()))
		detail = strings.TrimPrefix(detail, ":")
		detail = strings.TrimSpace(detail)
		if e.Key != "" {
			if detail != "" {
				return fmt.Sprintf("identity: missing git config %q: %s%s", e.Key, detail, hint)
			}
			return fmt.Sprintf("identity: missing git config %q%s", e.Key, hint)
		}
		if detail != "" {
			return fmt.Sprintf("identity: missing git configuration: %s%s", detail, hint)
		}
		return "identity: missing git configuration" + hint
	}
	if errors.Is(e.Problem, ErrUnsupportedFormat) {
		if e.Key != "" && e.Value != "" {
			return fmt.Sprintf("identity: unsupported git config %q=%q: %v%s", e.Key, e.Value, e.Problem, hint)
		}
		if e.Key != "" {
			return fmt.Sprintf("identity: unsupported git config %q: %v%s", e.Key, e.Problem, hint)
		}
		return fmt.Sprintf("identity: unsupported format: %v%s", e.Problem, hint)
	}
	if errors.Is(e.Problem, ErrInvalid) {
		if e.Key != "" && e.Value != "" {
			return fmt.Sprintf("identity: invalid git config %q=%q: %v", e.Key, e.Value, e.Problem)
		}
		if e.Key != "" {
			return fmt.Sprintf("identity: invalid git config %q: %v", e.Key, e.Problem)
		}
		if e.Value != "" {
			return fmt.Sprintf("identity: invalid value %q: %v", e.Value, e.Problem)
		}
		return fmt.Sprintf("identity: invalid configuration: %v", e.Problem)
	}
	if e.Key != "" {
		return fmt.Sprintf("identity: git config error for %q: %v", e.Key, e.Problem)
	}
	return fmt.Sprintf("identity: %v", e.Problem)
}

func (e *ConfigError) Unwrap() error {
	return e.Problem
}
