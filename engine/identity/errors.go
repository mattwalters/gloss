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

func (e *ConfigError) Error() string {
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
				return fmt.Sprintf("identity: missing git config %q: %s (run 'writ init' to configure)", e.Key, detail)
			}
			return fmt.Sprintf("identity: missing git config %q (run 'writ init' to configure)", e.Key)
		}
		if detail != "" {
			return fmt.Sprintf("identity: missing git configuration: %s (run 'writ init' to configure)", detail)
		}
		return "identity: missing git configuration (run 'writ init' to configure)"
	}
	if errors.Is(e.Problem, ErrUnsupportedFormat) {
		if e.Key != "" && e.Value != "" {
			return fmt.Sprintf("identity: unsupported git config %q=%q: %v (run 'writ init' to configure)", e.Key, e.Value, e.Problem)
		}
		if e.Key != "" {
			return fmt.Sprintf("identity: unsupported git config %q: %v (run 'writ init' to configure)", e.Key, e.Problem)
		}
		return fmt.Sprintf("identity: unsupported format: %v (run 'writ init' to configure)", e.Problem)
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
