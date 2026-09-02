package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoRoot is where this command's package sits relative to the tree it
// scans.
const repoRoot = "../../.."

// TestFieldTableIsDerivedFromTheSchemas pins what the walk over
// spec/schemas/ produces. The point of the table is that nobody types it, so
// the thing worth checking is that the derivation lands on the right answer —
// including the two classifications that are easy to get backwards.
//
// `subject` is a person on an approval and an object on a comment. It is
// unscoped, because the two are told apart by shape: this command only ever
// treats a string as a person value, so a comment's subject cannot be
// mistaken for one and does not need an op type to disambiguate it.
//
// `add` and `remove` are people on `assign` and label names on `label`, and
// both are strings, so shape settles nothing and the op type has to.
func TestFieldTableIsDerivedFromTheSchemas(t *testing.T) {
	fields, err := deriveFields(filepath.Join(repoRoot, "spec", "schemas"))
	if err != nil {
		t.Fatalf("deriveFields: %v", err)
	}

	want := map[string][]string{
		"subject":     nil, // any op type: shape distinguishes it
		"resolved_by": nil,
		"add":         {"assign"},
		"remove":      {"assign"},
		"assignees":   nil, // state alias
		"assignee":    nil, // state alias
	}

	for name := range fields {
		if _, ok := want[name]; !ok {
			t.Errorf("derived an unexpected person field %q from %s — if a schema gained a "+
				"person-id $ref, add it here; if not, the walk is over-reporting",
				name, fields[name].origin)
		}
	}
	for name, wantOps := range want {
		f, ok := fields[name]
		if !ok {
			t.Errorf("person field %q was not derived; the gate would stop checking it", name)
			continue
		}
		var gotOps []string
		for op := range f.opTypes {
			gotOps = append(gotOps, op)
		}
		sort.Strings(gotOps)
		if strings.Join(gotOps, ",") != strings.Join(wantOps, ",") {
			t.Errorf("%q op scope = %v, want %v", name, gotOps, wantOps)
		}
	}
}

// planted is one file written into a synthetic tree, and what the gate is
// expected to say about it.
type planted struct {
	path string
	body string
	// want is the set of bare values the gate must report from this file.
	// Empty means the file must produce nothing — every entry with an empty
	// want is a false positive this command has already been made to stop
	// producing.
	want []string
}

func TestScanFindsPlantedIdentifiers(t *testing.T) {
	cases := []planted{
		{
			path: "spec/testdata/comments/valid/resolve.json",
			body: `{"op_type":"resolve","body":{"resolved":true,"resolved_by":"alice"}}`,
			want: []string{"alice"},
		},
		{
			path: "spec/testdata/review-ops/valid/assign.json",
			body: `{"op_type":"assign","body":{"add":["alice"],"remove":["email:bob@example.com"]}}`,
			want: []string{"alice"},
		},
		{
			// The same field name under a different op type. Labels are not
			// people, and scoping is the only thing that knows it.
			path: "spec/testdata/review-ops/valid/label.json",
			body: `{"op_type":"label","body":{"add":["bug","needs-triage"],"remove":["wontfix"]}}`,
		},
		{
			// A comment's subject is the object it hangs off, not a person.
			path: "spec/testdata/comments/valid/create.json",
			body: `{"op_type":"create","body":{"subject":{"object_type":"review","object_id":"r-1"},"text":"hi"}}`,
		},
		{
			// An approval's subject is a person, with no op_type in scope at
			// all — this is the shape a folded golden has.
			path: "spec/fixtures/testdata/golden/review/x.json",
			body: `{"state":{"approvals":[{"subject":"alice","verdict":"approve"}]}}`,
			want: []string{"alice"},
		},
		{
			path: "engine/planted_a_test.go",
			body: "package engine\n\nvar x = Review{Assignees: []string{\"alice\", \"user:bob\"}}\n",
			want: []string{"alice"},
		},
		{
			// A variable, not a field. This is the shape the WRIT-102
			// adversarial review missed in engine/issue_test.go.
			path: "engine/planted_b_test.go",
			body: "package engine\n\nfunc f() { expectedAssignees := []string{\"user:alice\", \"charlie\"}; _ = expectedAssignees }\n",
			want: []string{"charlie"},
		},
		{
			// A raw-string op body, walked as JSON.
			path: "engine/planted_c_test.go",
			body: "package engine\n\nconst body = `{\"op_type\":\"resolve\",\"body\":{\"resolved_by\":\"dave\"}}`\n",
			want: []string{"dave"},
		},
		{
			path: "engine/planted_d_test.go",
			body: "package engine\n\nfunc g(c Comment) bool { return c.ResolvedBy == \"erin\" }\n",
			want: []string{"erin"},
		},
		{
			// A struct in a person-named position is not a person value.
			path: "engine/planted_e_test.go",
			body: "package engine\n\nvar y = Comment{Subject: CommentSubject{ObjectType: \"review\", ObjectID: \"r-1\"}}\n",
		},
		{
			// A named type's constant that happens to be spelled after the
			// field it groups on.
			path: "engine/planted_f.go",
			body: "package engine\n\ntype GroupKey string\n\nconst GroupByAssignee GroupKey = \"assignee\"\n",
		},
		{
			// A message beside a person field is a message.
			path: "engine/planted_g_test.go",
			body: "package engine\n\nfunc h(t T, got Review) { t.Errorf(\"unexpected assignees: %v\", got.Assignees) }\n",
		},
		{
			// The rejection corpus is bare on purpose and is allowlisted by
			// path, not excused by pattern.
			path: "spec/testdata/persons/invalid/bare.json",
			body: `{"op_type":"resolve","body":{"resolved_by":"frank"}}`,
		},
	}

	root := t.TempDir()
	// The gate derives its field table from the real schemas, so the
	// synthetic tree carries them.
	copySchemas(t, filepath.Join(repoRoot, "spec", "schemas"), filepath.Join(root, "spec", "schemas"))

	wantAll := make(map[string][]string)
	for _, c := range cases {
		full := filepath.Join(root, filepath.FromSlash(c.path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if len(c.want) > 0 {
			wantAll[c.path] = c.want
		}
	}

	fields, err := deriveFields(filepath.Join(root, "spec", "schemas"))
	if err != nil {
		t.Fatalf("deriveFields: %v", err)
	}
	findings, err := scan(root, fields)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	got := make(map[string][]string)
	for _, f := range findings {
		got[f.path] = append(got[f.path], f.value)
	}
	for path, want := range wantAll {
		sort.Strings(want)
		g := got[path]
		sort.Strings(g)
		if strings.Join(g, "|") != strings.Join(want, "|") {
			t.Errorf("%s: reported %v, want %v", path, g, want)
		}
		delete(got, path)
	}
	for path, values := range got {
		t.Errorf("%s: false positive, reported %v and should have reported nothing", path, values)
	}
}

// TestTreeIsClean is the gate itself, so `go test ./...` catches a bare
// identifier without waiting for CI to say so.
func TestTreeIsClean(t *testing.T) {
	fields, err := deriveFields(filepath.Join(repoRoot, "spec", "schemas"))
	if err != nil {
		t.Fatalf("deriveFields: %v", err)
	}
	findings, err := scan(repoRoot, fields)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range findings {
		t.Errorf("%s: %s is a bare person identifier: %q", f.path, f.where, f.value)
	}
}

func copySchemas(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
