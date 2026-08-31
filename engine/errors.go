package writ

import (
	"errors"

	"github.com/writtendev/writ/engine/projection"
)

var (
	// ErrNoIdentity is returned when a write operation is attempted but no writer identity
	// is configured in git config (run 'writ init' to configure).
	ErrNoIdentity = errors.New("identity: no writer identity configured (run 'writ init' to configure)")

	// ErrNoSigningKey is returned when a write operation is attempted but no SSH signing key
	// is configured in git config (run 'writ init' to configure).
	ErrNoSigningKey = errors.New("identity: no signing key configured (run 'writ init' to configure)")

	// ErrNotFound is returned when an object is not found.
	ErrNotFound = projection.ErrNotFound
)
