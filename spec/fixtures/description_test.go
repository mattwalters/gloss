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

func TestLoadRejectsInvalidCommitDescriptors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "duplicate commit id",
			yaml: `
name: dup-id
refs:
  - name: refs/heads/main
    history:
      - commits:
          - id: c1
            author: alice
            timestamp: 2026-01-01T00:00:00Z
            message: m1
            files: {f: "1"}
          - id: c1
            author: alice
            timestamp: 2026-01-01T00:01:00Z
            message: m2
            files: {f: "2"}
`,
		},
		{
			name: "unknown parent label",
			yaml: `
name: unknown-parent
refs:
  - name: refs/heads/main
    history:
      - commits:
          - parents: [nonexistent]
            author: alice
            timestamp: 2026-01-01T00:00:00Z
            message: m
            files: {f: "1"}
`,
		},
		{
			name: "forward parent reference",
			yaml: `
name: forward-parent
refs:
  - name: refs/heads/main
    history:
      - commits:
          - parents: [c2]
            author: alice
            timestamp: 2026-01-01T00:00:00Z
            message: m1
            files: {f: "1"}
          - id: c2
            author: alice
            timestamp: 2026-01-01T00:01:00Z
            message: m2
            files: {f: "2"}
`,
		},
		{
			name: "both op and files specified",
			yaml: `
name: op-and-files
refs:
  - name: refs/heads/main
    history:
      - commits:
          - author: alice
            timestamp: 2026-01-01T00:00:00Z
            files: {f: "1"}
            op:
              object_id: r1
              object_type: review
              op_type: create
              op_version: 1
              body: {}
`,
		},
		{
			name: "both op and message specified",
			yaml: `
name: op-and-message
refs:
  - name: refs/heads/main
    history:
      - commits:
          - author: alice
            timestamp: 2026-01-01T00:00:00Z
            message: custom message
            op:
              object_id: r1
              object_type: review
              op_type: create
              op_version: 1
              body: {}
`,
		},
		{
			name: "invalid sign_as identity",
			yaml: `
name: invalid-sign-as
refs:
  - name: refs/heads/main
    history:
      - commits:
          - author: alice
            sign_as: unknown_identity
            timestamp: 2026-01-01T00:00:00Z
            message: m
            files: {f: "1"}
`,
		},
		{
			name: "invalid tamper enum",
			yaml: `
name: invalid-tamper
refs:
  - name: refs/heads/main
    history:
      - commits:
          - author: alice
            tamper: arbitrary-unsupported-tamper
            timestamp: 2026-01-01T00:00:00Z
            message: m
            files: {f: "1"}
`,
		},
		{
			name: "invalid reject reason",
			yaml: `
name: invalid-reject-reason
refs:
  - name: refs/heads/main
    history:
      - commits:
          - author: alice
            timestamp: 2026-01-01T00:00:00Z
            message: m
            files: {f: "1"}
            expect:
              reject: made-up-rejection-reason
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load([]byte(tc.yaml)); err == nil {
				t.Fatalf("expected Load to reject %s, got nil error", tc.name)
			}
		})
	}
}
