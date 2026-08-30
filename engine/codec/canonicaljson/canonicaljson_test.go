package canonicaljson

import (
	"encoding/json"
	"os"
	"testing"
)

type vector struct {
	Name      string `json:"name"`
	Input     string `json:"input"`
	Canonical string `json:"canonical"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	data, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatalf("reading testdata/vectors.json: %v", err)
	}
	var vecs []vector
	if err := json.Unmarshal(data, &vecs); err != nil {
		t.Fatalf("parsing testdata/vectors.json: %v", err)
	}
	if len(vecs) == 0 {
		t.Fatal("testdata/vectors.json has no vectors")
	}
	return vecs
}

func TestMarshalVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			got, err := Marshal([]byte(v.Input))
			if err != nil {
				t.Fatalf("Marshal(%q): %v", v.Input, err)
			}
			if string(got) != v.Canonical {
				t.Errorf("Marshal(%q) = %q, want %q", v.Input, got, v.Canonical)
			}
		})
	}
}

// A canonical encoding must be a fixed point of itself: canonicalizing
// already-canonical bytes changes nothing. Signing relies on this.
func TestMarshalIdempotent(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			again, err := Marshal([]byte(v.Canonical))
			if err != nil {
				t.Fatalf("Marshal(%q): %v", v.Canonical, err)
			}
			if string(again) != v.Canonical {
				t.Errorf("Marshal(%q) = %q, want %q (not idempotent)", v.Canonical, again, v.Canonical)
			}
		})
	}
}

func TestMarshalRejectsTrailingData(t *testing.T) {
	if _, err := Marshal([]byte(`{"a":1} garbage`)); err == nil {
		t.Fatal("Marshal accepted trailing data after the JSON value")
	}
}

func TestMarshalRejectsInvalidJSON(t *testing.T) {
	if _, err := Marshal([]byte(`{"a":`)); err == nil {
		t.Fatal("Marshal accepted truncated JSON")
	}
}

func TestMarshalRejectsNonFiniteNumbers(t *testing.T) {
	// Not valid JSON syntax, but exercise the guard directly in case a
	// future caller feeds decoded values through a different path.
	if _, err := Marshal([]byte(`1e400`)); err == nil {
		t.Fatal("Marshal accepted a number that overflows to +Inf")
	}
}
