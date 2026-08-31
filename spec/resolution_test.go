package spec_test

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/spec"
)

const resolutionSchemaID = "https://writ.dev/spec/resolution.schema.json"

func compileResolutionSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	rawAnchor, err := spec.FS.ReadFile("schemas/anchor.schema.json")
	if err != nil {
		t.Fatalf("reading anchor schema: %v", err)
	}
	anchorDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawAnchor))
	if err != nil {
		t.Fatalf("decoding anchor schema: %v", err)
	}

	rawRes, err := spec.FS.ReadFile("schemas/resolution.schema.json")
	if err != nil {
		t.Fatalf("reading resolution schema: %v", err)
	}
	resDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawRes))
	if err != nil {
		t.Fatalf("decoding resolution schema: %v", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(anchorSchemaID, anchorDoc); err != nil {
		t.Fatalf("adding anchor schema: %v", err)
	}
	if err := c.AddResource(resolutionSchemaID, resDoc); err != nil {
		t.Fatalf("adding resolution schema: %v", err)
	}
	sch, err := c.Compile(resolutionSchemaID)
	if err != nil {
		t.Fatalf("compiling resolution schema: %v", err)
	}
	return sch
}

func gitBlobOID(content string) string {
	b := []byte(content)
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

type resolutionRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type resolutionContext struct {
	Before  []string `json:"before"`
	Lines   []string `json:"lines"`
	Omitted int      `json:"omitted,omitempty"`
	After   []string `json:"after"`
}

type resolutionSideAnchor struct {
	Commit  string             `json:"commit"`
	Path    string             `json:"path"`
	Blob    string             `json:"blob"`
	Range   *resolutionRange   `json:"range,omitempty"`
	Context *resolutionContext `json:"context,omitempty"`
}

type resolutionAnchor struct {
	Version int                   `json:"version"`
	Old     *resolutionSideAnchor `json:"old,omitempty"`
	New     *resolutionSideAnchor `json:"new,omitempty"`
}

type resolutionTarget struct {
	Files map[string]string `json:"files"`
}

type resolutionSideResult struct {
	Outcome string           `json:"outcome"`
	Match   string           `json:"match,omitempty"`
	Path    string           `json:"path,omitempty"`
	Range   *resolutionRange `json:"range,omitempty"`
	Reason  string           `json:"reason,omitempty"`
}

type resolutionOutcome struct {
	Anchor any                   `json:"anchor"`
	Old    *resolutionSideResult `json:"old,omitempty"`
	New    *resolutionSideResult `json:"new,omitempty"`
}

type resolutionCase struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Anchor      resolutionAnchor  `json:"anchor"`
	Target      resolutionTarget  `json:"target"`
	Expect      resolutionOutcome `json:"expect"`
}

func prepareTargetLines(content string) []string {
	rawLines := strings.Split(content, "\n")
	if len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}
	var lines []string
	for _, l := range rawLines {
		runes := []rune(l)
		if len(runes) > 1000 {
			runes = runes[:1000]
		}
		lines = append(lines, string(runes))
	}
	return lines
}

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func executeResolveSide(version int, s *resolutionSideAnchor, target resolutionTarget) *resolutionSideResult {
	if version != 1 {
		return &resolutionSideResult{Outcome: "orphaned", Reason: "unsupported-version"}
	}
	if s == nil {
		return nil
	}

	targetBlobs := make(map[string]string)
	for p, content := range target.Files {
		targetBlobs[p] = gitBlobOID(content)
	}

	// Whole-file anchor
	if s.Range == nil {
		if blob, ok := targetBlobs[s.Path]; ok && blob == s.Blob {
			return &resolutionSideResult{Outcome: "resolved", Match: "exact-path-blob", Path: s.Path}
		}
		var movedPaths []string
		for p, blob := range targetBlobs {
			if blob == s.Blob && p != s.Path {
				movedPaths = append(movedPaths, p)
			}
		}
		if len(movedPaths) > 0 {
			sort.Strings(movedPaths)
			return &resolutionSideResult{Outcome: "resolved", Match: "exact-blob-moved", Path: movedPaths[0]}
		}
		if _, ok := target.Files[s.Path]; ok {
			return &resolutionSideResult{Outcome: "orphaned", Reason: "no-candidate"}
		}
		return &resolutionSideResult{Outcome: "orphaned", Reason: "path-absent"}
	}

	// Ranged anchor:
	// Rung 1: exact-path-blob
	if blob, ok := targetBlobs[s.Path]; ok && blob == s.Blob {
		return &resolutionSideResult{
			Outcome: "resolved",
			Match:   "exact-path-blob",
			Path:    s.Path,
			Range:   &resolutionRange{Start: s.Range.Start, End: s.Range.End},
		}
	}

	// Rung 2: exact-blob-moved
	var movedPaths []string
	for p, blob := range targetBlobs {
		if blob == s.Blob && p != s.Path {
			movedPaths = append(movedPaths, p)
		}
	}
	if len(movedPaths) > 0 {
		sort.Strings(movedPaths)
		return &resolutionSideResult{
			Outcome: "resolved",
			Match:   "exact-blob-moved",
			Path:    movedPaths[0],
			Range:   &resolutionRange{Start: s.Range.Start, End: s.Range.End},
		}
	}

	// Candidate paths scope for Rung 3 and 4
	var candidatePaths []string
	if _, ok := target.Files[s.Path]; ok {
		candidatePaths = []string{s.Path}
	} else {
		for p := range target.Files {
			candidatePaths = append(candidatePaths, p)
		}
		sort.Strings(candidatePaths)
	}

	rangeLen := s.Range.End - s.Range.Start + 1
	isElided := s.Context.Omitted > 0

	// Rung 3: context-exact
	type exactCandidate struct {
		path        string
		start       int
		collarScore int
		distance    int
	}
	var exactMatches []exactCandidate

	for _, p := range candidatePaths {
		lines := prepareTargetLines(target.Files[p])
		if len(lines) < rangeLen {
			continue
		}
		for start := 1; start <= len(lines)-rangeLen+1; start++ {
			matched := true
			if !isElided {
				for i := 0; i < rangeLen; i++ {
					if lines[start+i-1] != s.Context.Lines[i] {
						matched = false
						break
					}
				}
			} else {
				for i := 0; i < 32; i++ {
					if lines[start+i-1] != s.Context.Lines[i] {
						matched = false
						break
					}
				}
				if matched {
					for j := 0; j < 32; j++ {
						if lines[start+rangeLen-32+j-1] != s.Context.Lines[32+j] {
							matched = false
							break
						}
					}
				}
			}
			if matched {
				collarScore := 0
				beforeLen := len(s.Context.Before)
				for k := 1; k <= beforeLen; k++ {
					if start-k >= 1 && lines[start-k-1] == s.Context.Before[beforeLen-k] {
						collarScore++
					}
				}
				afterLen := len(s.Context.After)
				for k := 0; k < afterLen; k++ {
					if start+rangeLen+k <= len(lines) && lines[start+rangeLen+k-1] == s.Context.After[k] {
						collarScore++
					}
				}
				exactMatches = append(exactMatches, exactCandidate{
					path:        p,
					start:       start,
					collarScore: collarScore,
					distance:    absInt(start - s.Range.Start),
				})
			}
		}
	}

	if len(exactMatches) > 0 {
		sort.Slice(exactMatches, func(i, j int) bool {
			a, b := exactMatches[i], exactMatches[j]
			if a.collarScore != b.collarScore {
				return a.collarScore > b.collarScore
			}
			if a.distance != b.distance {
				return a.distance < b.distance
			}
			if a.start != b.start {
				return a.start < b.start
			}
			return a.path < b.path
		})
		best := exactMatches[0]
		return &resolutionSideResult{
			Outcome: "resolved",
			Match:   "context-exact",
			Path:    best.path,
			Range:   &resolutionRange{Start: best.start, End: best.start + rangeLen - 1},
		}
	}

	// Rung 4: context-fuzzy
	maxAnchored := 2 * rangeLen
	if isElided {
		maxAnchored = 2 * 64
	}
	maxScore := maxAnchored + len(s.Context.Before) + len(s.Context.After)

	type fuzzyWindow struct {
		path  string
		start int
		score int
	}
	var windows []fuzzyWindow
	totalWindowsChecked := 0

	for _, p := range candidatePaths {
		lines := prepareTargetLines(target.Files[p])
		if len(lines) < rangeLen {
			continue
		}
		for start := 1; start <= len(lines)-rangeLen+1; start++ {
			totalWindowsChecked++
			anchoredMatches := 0
			if !isElided {
				for i := 0; i < rangeLen; i++ {
					if lines[start+i-1] == s.Context.Lines[i] {
						anchoredMatches++
					}
				}
			} else {
				for i := 0; i < 32; i++ {
					if lines[start+i-1] == s.Context.Lines[i] {
						anchoredMatches++
					}
				}
				for j := 0; j < 32; j++ {
					if lines[start+rangeLen-32+j-1] == s.Context.Lines[32+j] {
						anchoredMatches++
					}
				}
			}
			anchoredScore := 2 * anchoredMatches

			collarScore := 0
			beforeLen := len(s.Context.Before)
			for k := 1; k <= beforeLen; k++ {
				if start-k >= 1 && lines[start-k-1] == s.Context.Before[beforeLen-k] {
					collarScore++
				}
			}
			afterLen := len(s.Context.After)
			for k := 0; k < afterLen; k++ {
				if start+rangeLen+k <= len(lines) && lines[start+rangeLen+k-1] == s.Context.After[k] {
					collarScore++
				}
			}

			totalScore := anchoredScore + collarScore
			windows = append(windows, fuzzyWindow{
				path:  p,
				start: start,
				score: totalScore,
			})
		}
	}

	if totalWindowsChecked == 0 {
		if _, ok := target.Files[s.Path]; ok {
			return &resolutionSideResult{Outcome: "orphaned", Reason: "no-candidate"}
		}
		return &resolutionSideResult{Outcome: "orphaned", Reason: "path-absent"}
	}

	sort.Slice(windows, func(i, j int) bool {
		return windows[i].score > windows[j].score
	})

	bestScore := windows[0].score
	if bestScore == 0 {
		if _, ok := target.Files[s.Path]; ok {
			return &resolutionSideResult{Outcome: "orphaned", Reason: "no-candidate"}
		}
		return &resolutionSideResult{Outcome: "orphaned", Reason: "path-absent"}
	}

	// 60% threshold check
	if bestScore*100 < 60*maxScore {
		return &resolutionSideResult{Outcome: "orphaned", Reason: "below-threshold"}
	}

	// Strict winner check
	if len(windows) > 1 && windows[1].score == bestScore {
		return &resolutionSideResult{Outcome: "orphaned", Reason: "ambiguous"}
	}

	best := windows[0]
	return &resolutionSideResult{
		Outcome: "resolved",
		Match:   "context-fuzzy",
		Path:    best.path,
		Range:   &resolutionRange{Start: best.start, End: best.start + rangeLen - 1},
	}
}

func executeResolve(a resolutionAnchor, t resolutionTarget) resolutionOutcome {
	out := resolutionOutcome{
		Anchor: a,
	}
	if a.Old != nil {
		out.Old = executeResolveSide(a.Version, a.Old, t)
	}
	if a.New != nil {
		out.New = executeResolveSide(a.Version, a.New, t)
	}
	return out
}

func TestResolutionSchemaCompiles(t *testing.T) {
	compileResolutionSchema(t)
}

func TestResolutionVectorsValidateAndExecute(t *testing.T) {
	anchorSch := compileAnchorSchema(t)
	resSch := compileResolutionSchema(t)

	caseFiles := readDirNames(t, "testdata/resolution/cases")
	if len(caseFiles) == 0 {
		t.Fatal("no resolution test cases found")
	}

	for _, name := range caseFiles {
		t.Run(name, func(t *testing.T) {
			raw, err := spec.FS.ReadFile("testdata/resolution/cases/" + name)
			if err != nil {
				t.Fatalf("reading case file %s: %v", name, err)
			}

			var c resolutionCase
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Fatalf("unmarshaling case %s: %v", name, err)
			}

			// Validate anchor against anchor.schema.json if version == 1
			if c.Anchor.Version == 1 {
				anchorRaw, err := json.Marshal(c.Anchor)
				if err != nil {
					t.Fatalf("marshaling anchor: %v", err)
				}
				if err := validateVector(t, anchorSch, anchorRaw); err != nil {
					t.Errorf("anchor schema validation failed for %s: %v", name, err)
				}
			}

			// Validate expect against resolution.schema.json
			expectRaw, err := json.Marshal(c.Expect)
			if err != nil {
				t.Fatalf("marshaling expect: %v", err)
			}
			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(expectRaw))
			if err != nil {
				t.Fatalf("decoding expect instance: %v", err)
			}
			if err := resSch.Validate(inst); err != nil {
				t.Errorf("expect schema validation failed for %s: %v", name, err)
			}

			// Verify blob OID integrity for exact-blob matches
			for path, content := range c.Target.Files {
				computed := gitBlobOID(content)
				if c.Expect.New != nil && c.Expect.New.Match == "exact-path-blob" && c.Expect.New.Path == path {
					if c.Anchor.New.Blob != computed {
						t.Errorf("%s: anchor.new.blob %q does not match computed blob %q of %s", name, c.Anchor.New.Blob, computed, path)
					}
				}
				if c.Expect.New != nil && c.Expect.New.Match == "exact-blob-moved" && c.Expect.New.Path == path {
					if c.Anchor.New.Blob != computed {
						t.Errorf("%s: anchor.new.blob %q does not match computed blob %q of %s", name, c.Anchor.New.Blob, computed, path)
					}
				}
			}

			// Execute reference resolver and assert outcome matches Expect
			actual := executeResolve(c.Anchor, c.Target)
			actualRaw, _ := json.Marshal(actual)
			var actualMap, expectMap map[string]any
			_ = json.Unmarshal(actualRaw, &actualMap)
			_ = json.Unmarshal(expectRaw, &expectMap)

			if !reflect.DeepEqual(actualMap, expectMap) {
				t.Errorf("%s:\nactual: %s\nexpect: %s", name, string(actualRaw), string(expectRaw))
			}
		})
	}
}

func TestResolutionIndexCoverage(t *testing.T) {
	rawIndex, err := spec.FS.ReadFile("testdata/resolution/index.json")
	if err != nil {
		t.Fatal(err)
	}

	var index map[string]struct {
		Description string `json:"description"`
		Outcome     string `json:"outcome"`
		Match       string `json:"match"`
		Reason      string `json:"reason"`
	}
	if err := json.Unmarshal(rawIndex, &index); err != nil {
		t.Fatalf("decoding index.json: %v", err)
	}

	caseFiles := readDirNames(t, "testdata/resolution/cases")
	filesMap := make(map[string]bool)
	for _, f := range caseFiles {
		filesMap[f] = true
	}

	for f := range index {
		if !filesMap[f] {
			t.Errorf("index.json lists %s but file does not exist in cases/", f)
		}
	}

	for _, f := range caseFiles {
		entry, ok := index[f]
		if !ok {
			t.Errorf("case file %s is missing from index.json", f)
			continue
		}
		if entry.Description == "" {
			t.Errorf("case file %s in index.json has empty description", f)
		}
		if entry.Outcome == "" {
			t.Errorf("case file %s in index.json has empty outcome", f)
		}
	}

	// Assert every ladder rung and every orphan reason has coverage
	rungsCovered := make(map[string]bool)
	reasonsCovered := make(map[string]bool)

	for _, entry := range index {
		if entry.Match != "" {
			rungsCovered[entry.Match] = true
		}
		if entry.Reason != "" {
			reasonsCovered[entry.Reason] = true
		}
	}

	allRungs := []string{"exact-path-blob", "exact-blob-moved", "context-exact", "context-fuzzy"}
	for _, r := range allRungs {
		if !rungsCovered[r] {
			t.Errorf("ladder rung %q has no coverage in index.json", r)
		}
	}

	allReasons := []string{"path-absent", "no-candidate", "below-threshold", "ambiguous", "unsupported-version"}
	for _, r := range allReasons {
		if !reasonsCovered[r] {
			t.Errorf("orphan reason %q has no coverage in index.json", r)
		}
	}
}
