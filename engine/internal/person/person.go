// Package person holds the person-identifier normalization rule from
// spec/identifiers.md. It exists as its own package so that the fold, which
// must stay free of I/O, and the packages above it can share one definition of
// the rule without any of them importing a package that can spawn processes.
// It imports strings and nothing else; keep it that way.
package person

import "strings"

// NormalizePerson normalizes a person identifier string per spec/identifiers.md
// (trimmed leading/trailing whitespace, lowercase).
func NormalizePerson(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
