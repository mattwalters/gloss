package resolve

import (
	"sort"
)

// Match type constants from spec/resolution.md.
const (
	MatchExactPathBlob  = "exact-path-blob"
	MatchExactBlobMoved = "exact-blob-moved"
	MatchContextExact   = "context-exact"
	MatchContextFuzzy   = "context-fuzzy"
)

// Orphan reason constants from spec/resolution.md.
const (
	ReasonPathAbsent         = "path-absent"
	ReasonNoCandidate        = "no-candidate"
	ReasonBelowThreshold     = "below-threshold"
	ReasonAmbiguous          = "ambiguous"
	ReasonUnsupportedVersion = "unsupported-version"
)

// Outcome constants.
const (
	OutcomeResolved = "resolved"
	OutcomeOrphaned = "orphaned"
)

// Resolve evaluates an Anchor against a target Tree and returns its Resolution outcome.
// Resolve is a deterministic, pure function with no I/O.
func Resolve(a Anchor, t *Tree) Resolution {
	res := Resolution{
		Anchor: a,
	}
	if a.Old != nil {
		res.Old = resolveSide(a.Version, a.Old, t)
	}
	if a.New != nil {
		res.New = resolveSide(a.Version, a.New, t)
	}
	return res
}

func resolveSide(version int, s *SideAnchor, t *Tree) *SideResult {
	if version != 1 {
		return &SideResult{
			Outcome: OutcomeOrphaned,
			Reason:  ReasonUnsupportedVersion,
		}
	}
	if s == nil {
		return nil
	}

	// Whole-file anchor (no range specified)
	if s.Range == nil {
		return resolveWholeFileSide(s, t)
	}

	// Ranged anchor
	return resolveRangedSide(s, t)
}

func resolveWholeFileSide(s *SideAnchor, t *Tree) *SideResult {
	// Rung 1: exact-path-blob
	if f, ok := t.files[s.Path]; ok && f.Blob == s.Blob {
		return &SideResult{
			Outcome: OutcomeResolved,
			Match:   MatchExactPathBlob,
			Path:    s.Path,
		}
	}

	// Rung 2: exact-blob-moved (lexicographically earliest matching path)
	for _, p := range t.paths {
		if p != s.Path && t.files[p].Blob == s.Blob {
			return &SideResult{
				Outcome: OutcomeResolved,
				Match:   MatchExactBlobMoved,
				Path:    p,
			}
		}
	}

	// Orphan: whole-file anchors carry no context lines
	if _, ok := t.files[s.Path]; ok {
		return &SideResult{
			Outcome: OutcomeOrphaned,
			Reason:  ReasonNoCandidate,
		}
	}
	return &SideResult{
		Outcome: OutcomeOrphaned,
		Reason:  ReasonPathAbsent,
	}
}

func resolveRangedSide(s *SideAnchor, t *Tree) *SideResult {
	// Rung 1: exact-path-blob
	if f, ok := t.files[s.Path]; ok && f.Blob == s.Blob {
		return &SideResult{
			Outcome: OutcomeResolved,
			Match:   MatchExactPathBlob,
			Path:    s.Path,
			Range:   &Range{Start: s.Range.Start, End: s.Range.End},
		}
	}

	// Rung 2: exact-blob-moved
	for _, p := range t.paths {
		if p != s.Path && t.files[p].Blob == s.Blob {
			return &SideResult{
				Outcome: OutcomeResolved,
				Match:   MatchExactBlobMoved,
				Path:    p,
				Range:   &Range{Start: s.Range.Start, End: s.Range.End},
			}
		}
	}

	rangeLen := s.Range.End - s.Range.Start + 1
	if rangeLen <= 0 {
		if _, ok := t.files[s.Path]; ok {
			return &SideResult{Outcome: OutcomeOrphaned, Reason: ReasonNoCandidate}
		}
		return &SideResult{Outcome: OutcomeOrphaned, Reason: ReasonPathAbsent}
	}

	// Candidate paths scope for Rungs 3 and 4
	var candidatePaths []string
	if _, ok := t.files[s.Path]; ok {
		candidatePaths = []string{s.Path}
	} else {
		candidatePaths = t.paths
	}

	var ctxLines []string
	var beforeLines []string
	var afterLines []string
	var isElided bool
	if s.Context != nil {
		ctxLines = s.Context.Lines
		beforeLines = s.Context.Before
		afterLines = s.Context.After
		isElided = s.Context.Omitted > 0
	}

	// Rung 3: context-exact
	type exactCandidate struct {
		path        string
		start       int
		collarScore int
		distance    int
	}
	var exactMatches []exactCandidate

	for _, p := range candidatePaths {
		lines := t.files[p].Lines
		if len(lines) < rangeLen {
			continue
		}
		for start := 1; start <= len(lines)-rangeLen+1; start++ {
			matched := true
			if !isElided {
				if len(ctxLines) < rangeLen {
					matched = false
				} else {
					for i := 0; i < rangeLen; i++ {
						if lines[start+i-1] != ctxLines[i] {
							matched = false
							break
						}
					}
				}
			} else {
				if len(ctxLines) < 64 {
					matched = false
				} else {
					for i := 0; i < 32; i++ {
						if lines[start+i-1] != ctxLines[i] {
							matched = false
							break
						}
					}
					if matched {
						for j := 0; j < 32; j++ {
							if lines[start+rangeLen-32+j-1] != ctxLines[32+j] {
								matched = false
								break
							}
						}
					}
				}
			}

			if matched {
				collarScore := 0
				beforeLen := len(beforeLines)
				for k := 1; k <= beforeLen; k++ {
					if start-k >= 1 && lines[start-k-1] == beforeLines[beforeLen-k] {
						collarScore++
					}
				}
				afterLen := len(afterLines)
				for k := 0; k < afterLen; k++ {
					if start+rangeLen+k <= len(lines) && lines[start+rangeLen+k-1] == afterLines[k] {
						collarScore++
					}
				}
				exactMatches = append(exactMatches, exactCandidate{
					path:        p,
					start:       start,
					collarScore: collarScore,
					distance:    abs(start - s.Range.Start),
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
		return &SideResult{
			Outcome: OutcomeResolved,
			Match:   MatchContextExact,
			Path:    best.path,
			Range:   &Range{Start: best.start, End: best.start + rangeLen - 1},
		}
	}

	// Rung 4: context-fuzzy
	maxAnchored := 2 * rangeLen
	if isElided {
		maxAnchored = 2 * 64
	}
	maxScore := maxAnchored + len(beforeLines) + len(afterLines)

	type fuzzyWindow struct {
		path  string
		start int
		score int
	}
	var windows []fuzzyWindow
	totalWindowsChecked := 0

	for _, p := range candidatePaths {
		lines := t.files[p].Lines
		if len(lines) < rangeLen {
			continue
		}
		for start := 1; start <= len(lines)-rangeLen+1; start++ {
			totalWindowsChecked++
			anchoredMatches := 0
			if !isElided {
				for i := 0; i < rangeLen && i < len(ctxLines); i++ {
					if lines[start+i-1] == ctxLines[i] {
						anchoredMatches++
					}
				}
			} else {
				if len(ctxLines) >= 64 {
					for i := 0; i < 32; i++ {
						if lines[start+i-1] == ctxLines[i] {
							anchoredMatches++
						}
					}
					for j := 0; j < 32; j++ {
						if lines[start+rangeLen-32+j-1] == ctxLines[32+j] {
							anchoredMatches++
						}
					}
				}
			}
			anchoredScore := 2 * anchoredMatches

			collarScore := 0
			beforeLen := len(beforeLines)
			for k := 1; k <= beforeLen; k++ {
				if start-k >= 1 && lines[start-k-1] == beforeLines[beforeLen-k] {
					collarScore++
				}
			}
			afterLen := len(afterLines)
			for k := 0; k < afterLen; k++ {
				if start+rangeLen+k <= len(lines) && lines[start+rangeLen+k-1] == afterLines[k] {
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
		if _, ok := t.files[s.Path]; ok {
			return &SideResult{Outcome: OutcomeOrphaned, Reason: ReasonNoCandidate}
		}
		return &SideResult{Outcome: OutcomeOrphaned, Reason: ReasonPathAbsent}
	}

	sort.Slice(windows, func(i, j int) bool {
		return windows[i].score > windows[j].score
	})

	bestScore := windows[0].score
	if bestScore == 0 {
		if _, ok := t.files[s.Path]; ok {
			return &SideResult{Outcome: OutcomeOrphaned, Reason: ReasonNoCandidate}
		}
		return &SideResult{Outcome: OutcomeOrphaned, Reason: ReasonPathAbsent}
	}

	// 60% threshold check: integer arithmetic
	if maxScore > 0 && bestScore*100 < 60*maxScore {
		return &SideResult{Outcome: OutcomeOrphaned, Reason: ReasonBelowThreshold}
	}

	// Strict best check: bestScore must strictly beat second-best
	if len(windows) > 1 && windows[1].score == bestScore {
		return &SideResult{Outcome: OutcomeOrphaned, Reason: ReasonAmbiguous}
	}

	best := windows[0]
	return &SideResult{
		Outcome: OutcomeResolved,
		Match:   MatchContextFuzzy,
		Path:    best.path,
		Range:   &Range{Start: best.start, End: best.start + rangeLen - 1},
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
