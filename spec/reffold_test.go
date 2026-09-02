package spec_test

import (
	"testing"

	"github.com/writtendev/writ/engine/state"
	"github.com/writtendev/writ/spec"
)

// normalizePersonInputs pins the axes where two spellings of the rule could
// plausibly disagree: leading and trailing whitespace of several kinds, mixed
// case in either half, the empty and all-whitespace strings, non-ASCII case
// folding, where the split falls when the value carries its own colon, and the
// colonless strings that are not conforming identifiers at all.
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
