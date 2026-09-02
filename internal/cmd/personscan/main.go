// Command personscan fails when a person identifier anywhere in the tree is
// written without its scheme.
//
// WRIT-102 gave person identifiers a mandatory scheme — `email:alice@x` or
// `user:alice`, never bare `alice`. The migration missed instances twice. The
// first pass converted spec/ and stopped, leaving eight behind in engine/ and
// cmd/. An adversarial review of that pass found six of the eight; a
// mechanical, field-scoped sweep found the other two, one of them in
// engine/issue_test.go — the same sibling-file miss as engine/review_test.go,
// where the engine/state twin had been converted at matching line numbers and
// the outer file had not. Neither a careful read nor a name-based grep was
// complete. The field-scoped scan was, and this is that scan, kept.
//
// Usage:
//
//	personscan [-list] [DIR]
//
// DIR defaults to the current directory. -list prints the derived field table
// and the allowlist and exits, which is how to check what the gate believes
// without reading it.
//
// # Where the field list comes from
//
// Not from memory, and not from grepping for likely names — from
// spec/schemas/, which already maintains the enumeration as a side effect of
// being the format. Every place a schema writes
//
//	{"$ref": ".../identifiers.schema.json#/$defs/person-id"}
//
// is a person position, and every schema states its own op_type -> body
// dispatch, so the (op type, field) pairs fall out of the same walk. A field
// name that is a person somewhere and something else elsewhere — `add` and
// `remove` are people under `assign` and label names under `label` — is
// detected by that walk too, and is only checked inside an op of the right
// type.
//
// The one thing the schemas cannot supply is the folded-state spelling of
// those fields. `assign`'s `add`/`remove` accumulate into a state field named
// `assignees` (engine/state.Review, engine/state.Issue), and that mapping
// lives in the fold's Go code, not in any schema. stateAliases below is that
// short list, written down with its reason rather than derived, because there
// is nothing to derive it from.
//
// # What it looks at
//
// JSON files are parsed and walked, carrying the nearest enclosing `op_type`
// so op-scoped fields are judged in context. A `subject` holding an object —
// a comment's subject is {object_type, object_id} — is not a person position
// and is skipped by shape, not by name.
//
// Go files are parsed with go/parser, and two things are checked. Every
// string literal that parses as JSON is walked exactly like a JSON file,
// which covers the raw-string op bodies the fixtures are built from. And four
// syntactic shapes are treated as person positions when they are named for a
// person field: a composite-literal key, an assignment, a var or const
// declaration of plain string type, and an equality comparison — plus a call
// argument beside a composite literal, which is the equality-assertion shape
// (`reflect.DeepEqual(got.Assignees, []string{...})`).
//
// Naming matches on suffix, so `expectedAssignees := []string{"alice"}` is
// caught: a variable rather than a field, and invisible to any scan keyed on
// struct members. That is the shape the WRIT-102 review missed.
//
// The shapes are deliberately narrow. A wider rule — "any call that mentions
// a person field" — drags in every format string and error message beside
// one, and a gate that cries wolf gets an exception added rather than a fix.
// The unit tests in main_test.go pin both directions: each shape that must be
// caught, and each near-miss that must not be.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// schemedRe recognizes an identifier that carries a scheme. It is the
// person-id grammar's scheme production plus the colon, matched
// case-insensitively on purpose: this gate asks whether a scheme is present,
// not whether the identifier is normalized, so the deliberately denormalized
// `Email:Alice@Example.COM` fixtures pass it without needing an entry in the
// allowlist. Whether such a value is *valid* is the schema's question, and
// spec/testdata answers it.
var schemedRe = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]{0,31}:.`)

// personIDRef is the $ref every person position in spec/schemas/ points at.
const personIDRef = "https://writ.dev/spec/identifiers.schema.json#/$defs/person-id"

// stateAliases are the folded-state spellings of op-body person fields. The
// fold's op-field -> state-field mapping is Go code in engine/state, so
// unlike everything else in the field table these cannot be read out of a
// schema. Each entry names the derived field it is the folded form of.
var stateAliases = map[string]string{
	"assignees": "the set `assign`.add/.remove fold into (state.Review, state.Issue)",
	"assignee":  "the singular of assignees, as clients and CLI output spell it",
}

// allowedPrefixes are paths whose person values are bare on purpose.
//
// Only one kind of directory qualifies: a rejection corpus. A vector under
// invalid/ exists to be refused, and "a bare identifier is not a person
// identifier" is one of the things the corpus has to state by exhibiting it.
// Excusing those by pattern — "skip anything that looks deliberate" — would
// be the hole this gate exists to close, so they are listed.
var allowedPrefixes = []struct {
	prefix string
	reason string
}{
	{"spec/testdata/persons/invalid/", "rejection vectors: bare identifiers the schema must refuse"},
	{"spec/testdata/comments/invalid/", "rejection vectors for comment op bodies"},
	{"spec/testdata/review-ops/invalid/", "rejection vectors for review op bodies"},
	{"spec/testdata/issue-ops/invalid/", "rejection vectors for issue op bodies"},
	{"internal/cmd/personscan/main_test.go", "this command's own test input: bare identifiers it must find"},
}

// skipDirs are directories with nothing to scan: version control, build
// output, and the docs site's generated tree.
var skipDirs = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"public":        true,
	"dist":          true,
	".demo":         true,
	"testdata_tmp":  true,
	"writ-api-temp": true,
}

// personField is one position the schemas put a person identifier in. It is
// keyed by JSON property name in the field table.
type personField struct {
	// opTypes is empty when the field is a person wherever it appears, and
	// otherwise lists the op types under which it is one. A field that means
	// something else under another op type is always scoped.
	opTypes map[string]bool
	// origin says where the entry came from, for -list.
	origin string
}

func (f personField) scoped() bool { return len(f.opTypes) > 0 }

type finding struct {
	path  string
	where string
	value string
}

func main() {
	list := flag.Bool("list", false, "print the derived field table and the allowlist, then exit")
	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	fields, err := deriveFields(filepath.Join(root, "spec", "schemas"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "personscan: %v\n", err)
		os.Exit(2)
	}
	if len(fields) == 0 {
		fmt.Fprintln(os.Stderr, "personscan: no person-id $ref sites found in spec/schemas — the field table would be empty, which cannot be right")
		os.Exit(2)
	}

	if *list {
		printTable(fields)
		return
	}

	findings, err := scan(root, fields)
	if err != nil {
		fmt.Fprintf(os.Stderr, "personscan: %v\n", err)
		os.Exit(2)
	}
	if len(findings) == 0 {
		fmt.Printf("personscan: no bare person identifiers (%d person fields checked)\n", len(fields))
		return
	}
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "%s: %s is a bare person identifier: %q\n", f.path, f.where, f.value)
	}
	fmt.Fprintf(os.Stderr, "\npersonscan: %d bare person identifier(s).\n", len(findings))
	fmt.Fprintln(os.Stderr, "personscan: person identifiers carry a scheme (WRIT-102) — email:alice@example.com,")
	fmt.Fprintln(os.Stderr, "personscan: user:alice. There is no bare form and no implicit scheme; see")
	fmt.Fprintln(os.Stderr, "personscan: spec/identifiers.md. If a value is bare on purpose, it belongs in a")
	fmt.Fprintln(os.Stderr, "personscan: rejection corpus under invalid/ — add the directory to allowedPrefixes")
	fmt.Fprintln(os.Stderr, "personscan: in internal/cmd/personscan/main.go, with the reason.")
	os.Exit(1)
}

func printTable(fields map[string]personField) {
	names := make([]string, 0, len(fields))
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Println("person fields:")
	for _, n := range names {
		f := fields[n]
		scope := "any op type"
		if f.scoped() {
			ops := make([]string, 0, len(f.opTypes))
			for op := range f.opTypes {
				ops = append(ops, op)
			}
			sort.Strings(ops)
			scope = "op_type in {" + strings.Join(ops, ", ") + "}"
		}
		fmt.Printf("  %-14s %-34s %s\n", n, scope, f.origin)
	}
	fmt.Println("\nallowed prefixes:")
	for _, a := range allowedPrefixes {
		fmt.Printf("  %-38s %s\n", a.prefix, a.reason)
	}
}

// ---------------------------------------------------------------------------
// Deriving the field table from spec/schemas/
// ---------------------------------------------------------------------------

// deriveFields walks every schema in dir and returns the person fields it
// declares, keyed by JSON property name.
func deriveFields(dir string) (map[string]personField, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	// person[name] collects the op types under which name is a person.
	// collides[name] records that name is also used for a value of the same
	// JSON shape somewhere else, which is what makes it ambiguous.
	person := make(map[string]map[string]bool)
	collides := make(map[string]bool)
	origins := make(map[string]map[string]bool)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema.json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("decoding %s: %w", e.Name(), err)
		}
		defs, _ := doc["$defs"].(map[string]any)
		if defs == nil {
			continue
		}
		// Every body def is walked, whether or not the dispatch reaches it, so
		// a property name used non-personally anywhere still counts. The
		// dispatch then says which op type each def belongs to.
		bodyOps := dispatch(doc)
		for defName, def := range defs {
			ops := bodyOps[defName]
			walkProperties(def, defs, nil, 0, func(propPath []string, isPerson bool, typ string) {
				name := propPath[len(propPath)-1]
				if !isPerson {
					// Only a non-person use that a person value could be
					// confused with makes the name ambiguous. A person value
					// is a string, so an object- or number-valued property of
					// the same name is told apart by shape at scan time and
					// needs no op scoping: a comment's `subject` is an object
					// and an approval's is a person, and nothing has to know
					// which op it is in to see the difference. `add` and
					// `remove` under `label` are strings like the ones under
					// `assign`, and that is the case op scoping exists for.
					if typ == "string" {
						collides[name] = true
					}
					return
				}
				if person[name] == nil {
					person[name] = make(map[string]bool)
					origins[name] = make(map[string]bool)
				}
				for _, op := range ops {
					person[name][op] = true
				}
				origins[name][e.Name()+" $defs/"+defName] = true
			})
		}
	}

	fields := make(map[string]personField, len(person)+len(stateAliases))
	for name, ops := range person {
		f := personField{origin: joinSorted(origins[name])}
		if collides[name] {
			f.opTypes = ops
		}
		fields[name] = f
	}
	for name, reason := range stateAliases {
		if _, taken := fields[name]; taken {
			continue
		}
		fields[name] = personField{origin: "state alias: " + reason}
	}
	return fields, nil
}

// dispatch reads a vocabulary schema's op_type -> body definition mapping out
// of the if/then chain the schemas are written with, returning def name -> op
// types.
func dispatch(doc map[string]any) map[string][]string {
	out := make(map[string][]string)
	then, _ := doc["then"].(map[string]any)
	if then == nil {
		return out
	}
	all, _ := then["allOf"].([]any)
	for _, e := range all {
		clause, _ := e.(map[string]any)
		if clause == nil {
			continue
		}
		op := stringAt(clause, "if", "properties", "op_type", "const")
		ref := stringAt(clause, "then", "properties", "body", "$ref")
		if op == "" || !strings.HasPrefix(ref, "#/$defs/") {
			continue
		}
		def := strings.TrimPrefix(ref, "#/$defs/")
		out[def] = append(out[def], op)
	}
	return out
}

func stringAt(m map[string]any, path ...string) string {
	cur := any(m)
	for _, p := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[p]
	}
	s, _ := cur.(string)
	return s
}

// maxRefDepth bounds local $ref following, so a schema that refers to itself
// cannot make this loop.
const maxRefDepth = 16

// walkProperties visits every declared leaf property in a schema subtree,
// reporting the property-name path to it, whether its schema is the person-id
// $ref, and its declared JSON type when it has one.
//
// "items" is transparent: a property holding an array of person ids reports
// the property's own name, and a property holding an array of plain strings
// reports type "string" under its own name. That is deliberate — it is what
// makes `add` under `label` collide with `add` under `assign`, which is the
// collision op scoping exists to resolve.
//
// Local `#/$defs/...` references are followed, because a name's shape is
// often one indirection away: a comment's `subject` is a $ref to an object
// definition, and without resolving it the walk would see no type and could
// not tell it apart from an approval's person-valued `subject`.
func walkProperties(node any, defs map[string]any, propPath []string, depth int, visit func([]string, bool, string)) {
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	if ref, _ := obj["$ref"].(string); ref != "" {
		if len(propPath) == 0 {
			return
		}
		if ref == personIDRef {
			visit(propPath, true, "string")
			return
		}
		if local, ok := strings.CutPrefix(ref, "#/$defs/"); ok && depth < maxRefDepth {
			if target, ok := defs[local]; ok {
				walkProperties(target, defs, propPath, depth+1, visit)
				return
			}
		}
		// A reference this command cannot resolve — another document, or a
		// missing definition. Its shape is unknown, so it is recorded as a
		// non-person use of unknown type, which collides with nothing.
		visit(propPath, false, "")
		return
	}

	branched := false
	if props, ok := obj["properties"].(map[string]any); ok {
		branched = true
		for name, sub := range props {
			walkProperties(sub, defs, append(append([]string{}, propPath...), name), depth, visit)
		}
	}
	if items, ok := obj["items"]; ok {
		branched = true
		walkProperties(items, defs, propPath, depth, visit)
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if arr, ok := obj[key].([]any); ok {
			for _, sub := range arr {
				branched = true
				walkProperties(sub, defs, propPath, depth, visit)
			}
		}
	}
	if branched || len(propPath) == 0 {
		return
	}
	typ, _ := obj["type"].(string)
	visit(propPath, false, typ)
}

func joinSorted(set map[string]bool) string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// ---------------------------------------------------------------------------
// Scanning
// ---------------------------------------------------------------------------

func scan(root string, fields map[string]personField) ([]finding, error) {
	var findings []finding
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if allowed(rel) {
			return nil
		}
		switch filepath.Ext(p) {
		case ".json":
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			var doc any
			if err := json.Unmarshal(raw, &doc); err != nil {
				// Not every .json in the tree is well formed on purpose;
				// malformed-payload fixtures exist. Nothing to walk.
				return nil
			}
			findings = append(findings, walkJSON(rel, "", doc, "", fields)...)
		case ".go":
			f, err := scanGo(rel, p, fields)
			if err != nil {
				return err
			}
			findings = append(findings, f...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].path != findings[j].path {
			return findings[i].path < findings[j].path
		}
		return findings[i].where < findings[j].where
	})
	return findings, nil
}

func allowed(rel string) bool {
	for _, a := range allowedPrefixes {
		if strings.HasPrefix(rel, a.prefix) {
			return true
		}
	}
	return false
}

// walkJSON walks a decoded JSON document. opType is the nearest enclosing
// "op_type" value, which is what scopes `add`/`remove`. where is a path for
// the message.
func walkJSON(path, where string, node any, opType string, fields map[string]personField) []finding {
	var out []finding
	switch v := node.(type) {
	case map[string]any:
		if s, ok := v["op_type"].(string); ok {
			opType = s
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := v[k]
			sub := where + "." + k
			if f, ok := fields[k]; ok && (!f.scoped() || f.opTypes[opType]) {
				out = append(out, checkValue(path, sub, child)...)
			}
			out = append(out, walkJSON(path, sub, child, opType, fields)...)
		}
	case []any:
		for i, child := range v {
			out = append(out, walkJSON(path, fmt.Sprintf("%s[%d]", where, i), child, opType, fields)...)
		}
	}
	return out
}

// checkValue reports a bare identifier in a person position. Only strings and
// arrays of strings are person values: a `subject` holding an object is a
// comment's {object_type, object_id} and is not one.
func checkValue(path, where string, v any) []finding {
	switch t := v.(type) {
	case string:
		if bare(t) {
			return []finding{{path: path, where: strings.TrimPrefix(where, "."), value: t}}
		}
	case []any:
		var out []finding
		for i, e := range t {
			out = append(out, checkValue(path, fmt.Sprintf("%s[%d]", where, i), e)...)
		}
		return out
	}
	return nil
}

// bare reports whether s is a person identifier written without its scheme.
//
// A value with no letter or digit in it names nobody. That covers the empty
// string and the whitespace-only WRIT-116 fixtures, which exist to be dropped
// by the fold, and it covers the punctuation a client substitutes for an
// absent value ("-", ", "). None of them is an identifier somebody forgot to
// put a scheme on, which is the only thing this gate is looking for.
func bare(s string) bool {
	t := strings.TrimSpace(s)
	if !hasAlphanumeric(t) {
		return false
	}
	return !schemedRe.MatchString(t)
}

func hasAlphanumeric(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Go files
// ---------------------------------------------------------------------------

func scanGo(rel, path string, fields map[string]personField) ([]finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		// A file that does not parse cannot be scanned, and the build has
		// more to say about it than this gate does. Reporting nothing here is
		// safe: a file that does not compile is not a file whose person
		// identifiers have shipped.
		return nil, nil
	}

	var out []finding
	pos := func(n ast.Node) string { return strconv.Itoa(fset.Position(n.Pos()).Line) }

	// 1. Raw JSON bodies. Every string literal that decodes as a JSON object
	//    or array is walked exactly as a .json file would be.
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil || !looksLikeJSON(s) {
			return true
		}
		var doc any
		if err := json.Unmarshal([]byte(s), &doc); err != nil {
			return true
		}
		for _, f := range walkJSON(rel, "", doc, "", fields) {
			f.where = "line " + pos(lit) + " (JSON body) " + f.where
			out = append(out, f)
		}
		return true
	})

	// 2. Typed person positions. Four syntactic shapes, each narrow on
	//    purpose: a wider rule — "any call that mentions a person field" —
	//    drags in every format string and error message beside it, and a gate
	//    that cries wolf gets an exception added instead of a fix.
	ast.Inspect(file, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.KeyValueExpr:
			// Assignees: []string{"alice"}
			if id, ok := e.Key.(*ast.Ident); ok && namesPersonField(id.Name, fields) {
				out = append(out, checkGoValue(rel, "line "+pos(e)+" "+id.Name, e.Value)...)
			}
		case *ast.AssignStmt:
			// expectedAssignees := []string{"alice"}
			for _, lhs := range e.Lhs {
				if !directlyNamesPersonField(lhs, fields) {
					continue
				}
				for _, rhs := range e.Rhs {
					out = append(out, checkGoValue(rel, "line "+pos(e)+" assignment", rhs)...)
				}
				break
			}
		case *ast.ValueSpec:
			// var wantResolvedBy = "alice"
			//
			// A declaration with a named type is that type's value, not a
			// person identifier: `GroupByAssignee GroupKey = "assignee"` is a
			// grouping dimension that happens to be spelled after the field it
			// groups on. Only an untyped declaration or a plain string /
			// []string one can be holding a person.
			if !holdsPlainString(e.Type) {
				return true
			}
			for _, id := range e.Names {
				if namesPersonField(id.Name, fields) {
					for _, v := range e.Values {
						out = append(out, checkGoValue(rel, "line "+pos(e)+" "+id.Name, v)...)
					}
				}
			}
		case *ast.BinaryExpr:
			// c.ResolvedBy != "alice"
			if e.Op != token.EQL && e.Op != token.NEQ {
				return true
			}
			if directlyNamesPersonField(e.X, fields) {
				out = append(out, checkGoValue(rel, "line "+pos(e)+" comparison", e.Y)...)
			}
			if directlyNamesPersonField(e.Y, fields) {
				out = append(out, checkGoValue(rel, "line "+pos(e)+" comparison", e.X)...)
			}
		case *ast.CallExpr:
			// reflect.DeepEqual(res.Issue.Assignees, []string{"alice"})
			//
			// Restricted to a call one of whose arguments names a person field
			// and another of which is a composite literal. That is the
			// equality-assertion shape and almost nothing else: a t.Errorf
			// beside a person field passes a format string, not a slice.
			if !argNamesPersonField(e.Args, fields) {
				return true
			}
			for _, arg := range e.Args {
				if _, ok := arg.(*ast.CompositeLit); ok {
					out = append(out, checkGoValue(rel, "line "+pos(e)+" call argument", arg)...)
				}
			}
		}
		return true
	})
	return out, nil
}

func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

// namesPersonField reports whether a Go identifier is named for a person
// field. Matching on suffix rather than equality is what catches
// `expectedAssignees`, which is a variable rather than a struct member and so
// is invisible to any scan keyed on field names alone.
func namesPersonField(name string, fields map[string]personField) bool {
	lower := strings.ToLower(strings.ReplaceAll(name, "_", ""))
	for f := range fields {
		target := strings.ReplaceAll(f, "_", "")
		if !strings.HasSuffix(lower, target) {
			continue
		}
		// A bare `add`/`remove` in Go is a function, not an op body field;
		// those only reach this command inside raw JSON, handled above.
		if len(target) < 6 {
			continue
		}
		return true
	}
	return false
}

// directlyNamesPersonField reports whether an expression *is* a person field:
// the identifier itself, or a selector ending in one (`res.Issue.Assignees`).
// It deliberately does not look inside call arguments or index expressions —
// naming a person field somewhere within a larger expression is not the same
// as being one.
func directlyNamesPersonField(e ast.Expr, fields map[string]personField) bool {
	switch t := e.(type) {
	case *ast.Ident:
		return namesPersonField(t.Name, fields)
	case *ast.SelectorExpr:
		return namesPersonField(t.Sel.Name, fields)
	case *ast.IndexExpr:
		return directlyNamesPersonField(t.X, fields)
	case *ast.ParenExpr:
		return directlyNamesPersonField(t.X, fields)
	}
	return false
}

// holdsPlainString reports whether a declared type could hold a person
// identifier: absent (so the value's own type decides), `string`, or
// `[]string`.
func holdsPlainString(t ast.Expr) bool {
	switch e := t.(type) {
	case nil:
		return true
	case *ast.Ident:
		return e.Name == "string"
	case *ast.ArrayType:
		id, ok := e.Elt.(*ast.Ident)
		return ok && id.Name == "string"
	}
	return false
}

func argNamesPersonField(args []ast.Expr, fields map[string]personField) bool {
	for _, a := range args {
		if directlyNamesPersonField(a, fields) {
			return true
		}
	}
	return false
}

// checkGoValue reports bare identifiers in an expression that is being used as
// a person value. It looks one level deep and no further: a string literal, or
// the elements of a slice literal of string literals. It does not descend into
// a struct literal, because a struct in a person-named position is a different
// thing wearing the same name — `Subject: CommentSubject{ObjectType: "review"}`
// is a comment's subject, not a person's identifier, and its fields are not
// person values.
func checkGoValue(path, where string, e ast.Expr) []finding {
	switch t := e.(type) {
	case *ast.BasicLit:
		if s, ok := stringLit(t); ok && bare(s) {
			return []finding{{path: path, where: where, value: s}}
		}
	case *ast.CompositeLit:
		var out []finding
		for _, el := range t.Elts {
			lit, ok := el.(*ast.BasicLit)
			if !ok {
				// A keyed element or a nested literal: not a flat list of
				// person identifiers.
				continue
			}
			if s, ok := stringLit(lit); ok && bare(s) {
				out = append(out, finding{path: path, where: where, value: s})
			}
		}
		return out
	}
	return nil
}

// stringLit unquotes a string literal, rejecting the ones that are messages
// rather than values: a literal carrying a format verb, and one that is itself
// JSON (walked separately, as JSON).
func stringLit(lit *ast.BasicLit) (string, bool) {
	if lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil || strings.Contains(s, "%") || looksLikeJSON(s) {
		return "", false
	}
	return s, true
}
