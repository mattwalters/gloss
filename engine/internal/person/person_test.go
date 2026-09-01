package person_test

import (
	"strings"
	"testing"

	"github.com/writtendev/writ/engine/internal/person"
)

func TestNormalizePerson(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"email:alice@example.com", "email:alice@example.com"},
		{"Email:Alice@Example.COM", "email:alice@example.com"},
		{"  EMAIL:alice@example.com  ", "email:alice@example.com"},
		{"\t\n Email:Alice@Example.COM \r\n", "email:alice@example.com"},
		{"email:  Alice@Example.COM  ", "email:alice@example.com"},
		{"user:Alice", "user:alice"},
		{"KeyBase:Alice", "keybase:alice"},
		{"", ""},
		{"   ", ""},
		// Colonless strings are not conforming identifiers. Normalization is
		// not where that is decided, so they fold as flat strings.
		{"  ALICE  ", "alice"},
		{"alice@example.com", "alice@example.com"},
	}

	for _, tc := range cases {
		got := person.NormalizePerson(tc.input)
		if got != tc.want {
			t.Errorf("NormalizePerson(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestSplitFirstColon is the whole reason Split exists as its own function. An
// email address may carry a colon inside a quoted local part, so a rule that
// splits on "a colon" — the last one, or that refuses more than one — reads the
// scheme of `email:"a:b"@example.com` as `email:"a`, which is not a scheme, and
// rejects an identifier the format allows.
func TestSplitFirstColon(t *testing.T) {
	cases := []struct {
		in     string
		scheme string
		value  string
		ok     bool
	}{
		{`email:"a:b"@example.com`, "email", `"a:b"@example.com`, true},
		{"email:a:b:c", "email", "a:b:c", true},
		{"user:alice", "user", "alice", true},
		{":alice", "", "alice", true},
		{"email:", "email", "", true},
		{"alice@example.com", "", "alice@example.com", false},
		{"", "", "", false},
	}

	for _, tc := range cases {
		scheme, value, ok := person.Split(tc.in)
		if scheme != tc.scheme || value != tc.value || ok != tc.ok {
			t.Errorf("Split(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, scheme, value, ok, tc.scheme, tc.value, tc.ok)
		}
	}
}

func TestCheck(t *testing.T) {
	atLimitValue := strings.Repeat("a", 320)
	atLimitMultiByte := strings.Repeat("é", 320)
	atLimitScheme := strings.Repeat("a", 32)

	cases := []struct {
		in   string
		want person.Problem
	}{
		{"email:alice@example.com", person.Valid},
		{"user:ci", person.Valid},
		{"keybase:alice", person.Valid},
		{`email:"a:b"@example.com`, person.Valid},
		{"x+ci.bot-2:alice", person.Valid},
		{atLimitScheme + ":alice", person.Valid},
		{"email:" + atLimitValue, person.Valid},
		{"email:" + atLimitMultiByte, person.Valid},

		{"alice@example.com", person.MissingScheme},
		{"alice", person.MissingScheme},
		{"", person.MissingScheme},
		{":alice", person.SchemeCharset},
		{"Email:alice@example.com", person.SchemeCharset},
		{"2fa:alice", person.SchemeCharset},
		{"my_scheme:alice", person.SchemeCharset},
		{strings.Repeat("a", 33) + ":alice", person.SchemeTooLong},
		{"user:", person.EmptyValue},
		{"email:" + atLimitValue + "a", person.ValueTooLong},
		{"email:" + atLimitMultiByte + "é", person.ValueTooLong},
	}

	for _, tc := range cases {
		if got := person.Check(tc.in); got != tc.want {
			t.Errorf("Check(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestValueBoundCountsCodePointsNotBytes pins the unit. A 320-code-point
// multi-byte value is 640 bytes; a byte-counting bound would refuse it, and
// then the engine and the JSON Schema — which counts code points — would
// disagree about the same identifier.
func TestValueBoundCountsCodePointsNotBytes(t *testing.T) {
	id := "email:" + strings.Repeat("é", 320)
	if n := len(id); n != 646 {
		t.Fatalf("test setup: identifier is %d bytes, want 646", n)
	}
	if got := person.Check(id); got != person.Valid {
		t.Errorf("Check(320 code points / 640 bytes) = %v, want Valid", got)
	}
}

// TestCheckRejectsRatherThanTruncates: Check has no repair path at all, which
// is the point. Truncating an over-long identifier to the bound would map it
// onto whatever shorter identifier shares that prefix, making two people one
// for assignment, approval keying and set membership.
func TestCheckRejectsRatherThanTruncates(t *testing.T) {
	victim := "email:" + strings.Repeat("a", 320)
	attacker := victim + "b"

	if person.Check(victim) != person.Valid {
		t.Fatal("test setup: the shorter identifier should be valid")
	}
	if got := person.Check(attacker); got != person.ValueTooLong {
		t.Fatalf("Check(over-long) = %v, want ValueTooLong", got)
	}
	if person.NormalizePerson(attacker) == victim {
		t.Error("normalization mapped an over-long identifier onto a shorter one")
	}
}

func TestDerivedMaxLen(t *testing.T) {
	if person.MaxLen != person.MaxSchemeLen+1+person.MaxValueLen {
		t.Errorf("MaxLen = %d, want %d", person.MaxLen, person.MaxSchemeLen+1+person.MaxValueLen)
	}
	if person.MaxLen != 353 {
		t.Errorf("MaxLen = %d, want 353 (the number spec/identifiers.md and identifiers.schema.json state)", person.MaxLen)
	}
}
