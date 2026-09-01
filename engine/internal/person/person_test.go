package person_test

import (
	"testing"

	"github.com/writtendev/writ/engine/internal/person"
)

func TestNormalizePerson(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"alice@example.com", "alice@example.com"},
		{"Alice@Example.COM", "alice@example.com"},
		{"  alice@example.com  ", "alice@example.com"},
		{"\t\n Alice@Example.COM \r\n", "alice@example.com"},
		{"", ""},
		{"   ", ""},
		{"  ALICE  ", "alice"},
		{"user.name+tag@sub.domain.org", "user.name+tag@sub.domain.org"},
	}

	for _, tc := range cases {
		got := person.NormalizePerson(tc.input)
		if got != tc.want {
			t.Errorf("NormalizePerson(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
