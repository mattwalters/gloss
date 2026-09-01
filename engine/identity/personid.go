package identity

import (
	"fmt"
	"strings"

	"github.com/writtendev/writ/engine/internal/person"
)

// PersonIDKey is the git config key that overrides the derived person
// identifier. Reading git config lowercases keys; this is the spelling a user
// types.
const PersonIDKey = "writ.personId"

// DerivePersonID returns the local writer's person identifier from git config,
// per spec/identifiers.md §Relationship to writer-id: writ.personId when set,
// otherwise "email:" followed by the normalized user.email.
//
// The derivation is deliberately not a guess. A person identifier carries a
// scheme, and the only scheme a git config can supply on its own is email:, so
// that is the only one derived. A workspace that identifies people by handle
// sets writ.personId explicitly — which is also the escape hatch for anyone
// who does not want their address in a public, unretractable op log.
//
// The result is normalized and checked. An identifier the schema would reject
// is an error here rather than a truncation or a repair: an op body is written
// once into a signed commit and never rewritten, so the only place to stop a
// malformed identifier is before the write.
func DerivePersonID(cfg map[string]string) (string, error) {
	if raw, ok := cfg["writ.personid"]; ok && raw != "" {
		norm := person.NormalizePerson(raw)
		if p := person.Check(norm); p != person.Valid {
			return "", &ConfigError{
				Key:     PersonIDKey,
				Value:   raw,
				Problem: fmt.Errorf("%w: %s", ErrInvalid, p),
			}
		}
		return norm, nil
	}

	// Trim before the emptiness check. A whitespace-only user.email is a set
	// key with nothing in it; treating it as configured would derive "email:",
	// an identifier with an empty value, and report it as a bad address rather
	// than as an address that was never given.
	email := strings.TrimSpace(cfg["user.email"])
	if email == "" {
		return "", &ConfigError{
			Key: PersonIDKey,
			Problem: fmt.Errorf("%w: set %s (for example %q), or set user.email so it can be derived as email:<user.email>",
				ErrMissing, PersonIDKey, "user:alice"),
		}
	}

	norm := person.NormalizePerson("email:" + email)
	if p := person.Check(norm); p != person.Valid {
		return "", &ConfigError{
			Key:     "user.email",
			Value:   email,
			Problem: fmt.Errorf("%w: derived person identifier is not conforming: %s (set %s to override)", ErrInvalid, p, PersonIDKey),
		}
	}
	return norm, nil
}
