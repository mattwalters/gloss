package fixtures

import "testing"

// TestLoadRejectsDuplicateRefNames covers the collision Generate can't
// safely resolve on its own: two refs (or a ref and a keep_as) sharing a
// name would make one silently overwrite the other in the real repo
// while both still ended up in the manifest as if they'd landed.
func TestLoadRejectsDuplicateRefNames(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "duplicate top-level ref",
			yaml: `
name: dup
refs:
  - name: refs/heads/main
    history:
      - commits:
          - {author: alice, timestamp: 2026-01-01T00:00:00Z, message: m, files: {f: "1"}}
  - name: refs/heads/main
    history:
      - commits:
          - {author: alice, timestamp: 2026-01-01T00:00:00Z, message: m, files: {f: "1"}}
`,
		},
		{
			name: "keep_as collides with another ref",
			yaml: `
name: dup
refs:
  - name: refs/heads/main
    history:
      - keep_as: refs/heads/other
        commits:
          - {author: alice, timestamp: 2026-01-01T00:00:00Z, message: m, files: {f: "1"}}
      - commits:
          - {author: alice, timestamp: 2026-01-01T00:00:01Z, message: m2, files: {f: "2"}}
  - name: refs/heads/other
    history:
      - commits:
          - {author: alice, timestamp: 2026-01-01T00:00:00Z, message: m, files: {f: "1"}}
`,
		},
		{
			name: "two keep_as values collide",
			yaml: `
name: dup
refs:
  - name: refs/heads/main
    history:
      - keep_as: refs/fixture-history/shared
        commits:
          - {author: alice, timestamp: 2026-01-01T00:00:00Z, message: m, files: {f: "1"}}
      - keep_as: refs/fixture-history/shared
        commits:
          - {author: alice, timestamp: 2026-01-01T00:00:01Z, message: m2, files: {f: "2"}}
      - commits:
          - {author: alice, timestamp: 2026-01-01T00:00:02Z, message: m3, files: {f: "3"}}
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load([]byte(tc.yaml)); err == nil {
				t.Fatal("expected Load to reject a duplicate ref name, got nil error")
			}
		})
	}
}
