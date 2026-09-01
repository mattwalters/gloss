// Package person holds the person-identifier grammar and normalization rule
// from spec/identifiers.md. It exists as its own package so that the fold,
// which must stay free of I/O, and the packages above it can share one
// definition of the rule without any of them importing a package that can
// spawn processes. It imports strings and nothing else; keep it that way —
// that is what makes its entry in engine/internal/fold's import allowlist
// grant no capability.
package person

import "strings"

// Length bounds from spec/identifiers.md §Person identifiers. MaxLen is
// derived, not an independent number: a scheme, the colon, and a value.
const (
	MaxSchemeLen = 32
	MaxValueLen  = 320
	MaxLen       = MaxSchemeLen + 1 + MaxValueLen // 353
)

// Split splits a person identifier into its scheme and value on the FIRST
// colon, per spec/identifiers.md. The first colon and not "a colon": an email
// address may legally carry a colon inside a quoted local part, so
// `email:"a:b"@example.com` is scheme `email` with value `"a:b"@example.com`.
//
// ok is false when s carries no colon at all; such a string is not a
// conforming person identifier (there is no bare form and no implicit
// scheme), and it is returned whole as the value so callers that must
// preserve it can.
func Split(s string) (scheme, value string, ok bool) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", s, false
	}
	return s[:i], s[i+1:], true
}

// NormalizePerson normalizes a person identifier string per
// spec/identifiers.md: the scheme is lowercased, and the value is trimmed of
// leading and trailing whitespace and case-folded.
//
// The exact case-folding algorithm is deliberately not pinned here and may
// come to differ per scheme (WRIT-117); today both halves fold with
// strings.ToLower, which is what spec/identifiers.md states.
//
// A string carrying no colon is not a conforming identifier. It is folded as
// a flat string and returned rather than rejected: what a reader does with a
// non-conforming identifier is a separate decision (WRIT-124/126), and
// normalization is not the place to make it.
func NormalizePerson(s string) string {
	s = strings.TrimSpace(s)
	scheme, value, ok := Split(s)
	if !ok {
		return strings.ToLower(s)
	}
	return strings.ToLower(scheme) + ":" + strings.ToLower(strings.TrimSpace(value))
}

// Problem names the ways a string can fail to be a conforming person
// identifier. It is an enumeration rather than an error so that this package
// keeps importing strings and nothing else; callers turn it into a message.
type Problem int

const (
	// Valid means the identifier conforms.
	Valid Problem = iota
	// MissingScheme means the identifier carries no colon at all. There is no
	// bare form and no implicit scheme.
	MissingScheme
	// SchemeCharset means the scheme is empty or carries a character outside
	// [a-z][a-z0-9+.-]*.
	SchemeCharset
	// SchemeTooLong means the scheme exceeds MaxSchemeLen.
	SchemeTooLong
	// EmptyValue means the value is empty.
	EmptyValue
	// ValueTooLong means the value exceeds MaxValueLen code points.
	ValueTooLong
)

// String describes the problem for use in a caller's error message.
func (p Problem) String() string {
	switch p {
	case Valid:
		return "valid"
	case MissingScheme:
		return "missing scheme (expected scheme:value, for example email:alice@example.com or user:alice)"
	case SchemeCharset:
		return "scheme must match [a-z][a-z0-9+.-]*"
	case SchemeTooLong:
		return "scheme is longer than 32 characters"
	case EmptyValue:
		return "value is empty"
	case ValueTooLong:
		return "value is longer than 320 characters"
	}
	return "unknown problem"
}

// Check reports whether s is a conforming person identifier per
// spec/identifiers.md. s is expected to be normalized already; Check tests the
// grammar and the bounds, not normalization.
//
// The bounds are enforced by rejection, never by truncation: two distinct
// identifiers truncated to the same string would collapse into one person for
// assignment, approval keying and set membership.
//
// Check is a producer-side guard. The fold does not call it: what a reader
// does with a non-conforming identifier it has already read is decided
// separately (WRIT-124/126).
func Check(s string) Problem {
	scheme, value, ok := Split(s)
	if !ok {
		return MissingScheme
	}
	if !validScheme(scheme) {
		return SchemeCharset
	}
	// A scheme is ASCII by its charset, so bytes and code points coincide.
	if len(scheme) > MaxSchemeLen {
		return SchemeTooLong
	}
	if value == "" {
		return EmptyValue
	}
	if countRunes(value) > MaxValueLen {
		return ValueTooLong
	}
	return Valid
}

// validScheme reports whether scheme matches [a-z][a-z0-9+.-]*.
func validScheme(scheme string) bool {
	if scheme == "" {
		return false
	}
	for i := 0; i < len(scheme); i++ {
		c := scheme[i]
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && c >= '0' && c <= '9':
		case i > 0 && (c == '+' || c == '.' || c == '-'):
		default:
			return false
		}
	}
	return true
}

// countRunes counts code points, the unit JSON Schema maxLength counts, so the
// engine accepts exactly what spec/schemas/identifiers.schema.json accepts.
// Spelled out rather than reached for in unicode/utf8 to keep this package's
// import list at one entry.
func countRunes(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
