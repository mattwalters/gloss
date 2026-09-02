package spec_test

import (
	"testing"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/writtendev/writ/engine/state"
	"github.com/writtendev/writ/spec"
)

// normalizePersonInputs pins the axes where two spellings of the rule could
// plausibly disagree: leading and trailing whitespace of several kinds, mixed
// case in either half, the empty and all-whitespace strings, non-ASCII case
// folding, where the split falls when the value carries its own colon, and the
// colonless strings that are not conforming identifiers at all.
//
// Since WRIT-117 it also pins every step of the folding algorithm, because two
// copies of a three-step rule have three times as many ways to drift as two
// copies of strings.ToLower did: composition, the case fold itself, the second
// composition, and the two upstream defects each copy has to work around.
var normalizePersonInputs = []string{
	"",
	" ",
	"\t\r\n",
	":",
	"email:",
	":alice@example.com",
	"email:alice@example.com",
	"Email:Alice@Example.COM",
	"  email:alice@example.com  ",
	"\t\n EMAIL:Alice@Example.COM \r\n",
	"email:  Alice@Example.COM  ",
	"EMAIL:DEV+1@EXAMPLE.COM",
	"user:alice",
	"USER:Alice",
	"keybase:Alice",
	`email:"a:b"@example.com`,
	`EMAIL:"A:B"@Example.COM`,
	"email:a:b:c",
	"email:ÉLODIE@Example.COM",
	// Colonless: not conforming identifiers, but both copies of the rule must
	// still agree on what they fold to.
	"alice@example.com",
	"Alice@Example.COM",
	"  alice@example.com  ",
	"\t\n Alice@Example.COM \r\n",
	"DEV+1@EXAMPLE.COM",
	"Alice Example",
	"ÉLODIE@Example.COM",
	"Ünïcodé Nàme",
	"ΣΊΣΥΦΟΣ@example.com",
	"ИВАН@ПРИМЕР.РФ",
	"  日本語  ",
	"\u00a0NBSP@Example.COM\u00a0",
	// The folding algorithm, step by step.
	"user:\u0130",                             // the pinned case fold, on the character that motivated pinning it
	"user:\u00df",                             // full folding, not simple
	"user:\u1e9e",                             // the capital, which simple folding would send to U+00DF instead
	"user:Jos\u0065\u0301",                    // NFC composes a decomposed value
	"user:Jos\u00e9",                          // and leaves the precomposed spelling alone
	"user:\u017f\u0301",                       // folding leaves s+U+0301, which the second NFC composes
	"user:\u13a0",                             // Cherokee uppercase: a fold fixed point x/text toggles
	"user:\uab70",                             // Cherokee lowercase, which folds up
	"user:\U00010041\u0300",                   // a supplementary starter that must not compose with its mark
	"user:\U00011099\U000110ba",               // a supplementary pair that must
	"user:\U00011347\U0001133e",               // and one whose second element is a starter
	"user:\u1100\u1161",                       // Hangul, the composition between two starters
	"user:\u00e9\U00010041\u0300\u0065\u0301", // a false composition must not cost its neighbours
	"\u0130:alice",                            // a non-conforming scheme, where the two copies still must agree
	"\U00010041\u0300@example.com",            // colonless, and past the ASCII fast path
}

// TestReffoldPinnedUnicodeVersion binds the reference fold's copy of the rule
// to the Unicode tables x/text actually compiled in. x/text selects tables by
// Go build tag rather than by module version, so a toolchain bump would
// otherwise change the reference implementation's answers — and with them the
// conformance goldens every other implementation is checked against — with no
// change to this repository at all.
func TestReffoldPinnedUnicodeVersion(t *testing.T) {
	if norm.Version != spec.PersonUnicodeVersion {
		t.Errorf("x/text/unicode/norm is Unicode %s, but spec/identifiers.md pins %s",
			norm.Version, spec.PersonUnicodeVersion)
	}
	if cases.UnicodeVersion != spec.PersonUnicodeVersion {
		t.Errorf("x/text/cases is Unicode %s, but spec/identifiers.md pins %s",
			cases.UnicodeVersion, spec.PersonUnicodeVersion)
	}
}

// TestReffoldNormalizePersonMatchesEngine binds the reference fold's local copy
// of the person-identifier normalization rule to the engine's one definition,
// which lives in engine/internal/person and is reached here through the
// exported state.NormalizePerson — the name that holds folded person values.
// reffold.go deliberately does not import engine code: it is the standalone
// reference independent implementations read, and engine/internal is not
// reachable from spec/ in any case. This test is what stops the two copies
// drifting.
func TestReffoldNormalizePersonMatchesEngine(t *testing.T) {
	for _, in := range normalizePersonInputs {
		if got, want := spec.NormalizePerson(in), state.NormalizePerson(in); got != want {
			t.Errorf("reffold normalizePerson(%q) = %q, state.NormalizePerson(%q) = %q", in, got, in, want)
		}
	}
}

// FuzzReffoldNormalizePersonMatchesEngine covers the inputs the table above
// cannot enumerate.
func FuzzReffoldNormalizePersonMatchesEngine(f *testing.F) {
	for _, in := range normalizePersonInputs {
		f.Add(in)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if got, want := spec.NormalizePerson(in), state.NormalizePerson(in); got != want {
			t.Errorf("reffold normalizePerson(%q) = %q, state.NormalizePerson(%q) = %q", in, got, in, want)
		}
	})
}
