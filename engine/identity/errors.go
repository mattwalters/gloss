package identity

import (
	"errors"
	"fmt"
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
		if e.Key != "" {
			return fmt.Sprintf("identity: missing git config %q (run 'writ init' to configure)", e.Key)
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
