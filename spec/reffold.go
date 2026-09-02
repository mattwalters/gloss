package spec

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// personUnicodeVersion is the Unicode version spec/identifiers.md pins the
// person-identifier normalization algorithm to. x/text selects its tables by
// Go build tag rather than by module version, so this is checked against the
// tables actually compiled in rather than assumed.
const personUnicodeVersion = "15.0.0"

// splitPerson splits a person identifier into scheme and value on the FIRST
// colon, per spec/identifiers.md. The first colon and not "a colon": an email
// address may legally carry a colon inside a quoted local part, so
// `email:"a:b"@example.com` is scheme `email` with value `"a:b"@example.com`.
func splitPerson(s string) (scheme, value string, ok bool) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", s, false
	}
	return s[:i], s[i+1:], true
}

// normalizePerson normalizes a person identifier string per
// spec/identifiers.md: the scheme is lowercased, and the value is trimmed of
// leading and trailing whitespace and folded by foldPersonValue.
//
// A string carrying no colon is not a conforming identifier; it is folded as
// a flat string and preserved rather than rejected, because what a reader
// does with a non-conforming identifier is a separate decision
// (WRIT-124/126).
//
// The reference fold is deliberately standalone — it is what independent
// implementations read — so it carries its own copy of the rule rather than
// importing the engine's, which lives under engine/internal and is not
// reachable from here in any case. TestReffoldNormalizePersonMatchesEngine
// binds the two together so they cannot drift.
func normalizePerson(s string) string {
	s = strings.TrimSpace(s)
	scheme, value, ok := splitPerson(s)
	if !ok {
		return foldPersonValue(s)
	}
	return strings.ToLower(scheme) + ":" + foldPersonValue(strings.TrimSpace(value))
}

// foldPersonValue applies the value half of the normalization rule in
// spec/identifiers.md §Normalization rules, pinned to Unicode
// personUnicodeVersion: NFC, then Unicode default case folding (UAX #21 §2.3
// toCasefold, the full C+F mappings, no locale tailoring), then NFC again. One
// algorithm for every scheme.
//
// The trailing NFC is not redundant. Case folding does not preserve a normal
// form — U+017F followed by U+0301 folds to "s" plus U+0301, which is not NFC
// — so a rule that stopped after folding would give a different answer on a
// second pass, and normalization runs at more than one layer.
func foldPersonValue(s string) string {
	if personIsASCII(s) {
		return personLowerASCII(s)
	}
	return personNFC(personCaseFold(personNFC(s)))
}

func personIsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func personLowerASCII(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if b == nil {
				b = []byte(s)
			}
			b[i] = c + ('a' - 'A')
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}

var personFoldCaser = cases.Fold()

// Cherokee uppercase folds to itself: CaseFolding.txt maps Cherokee lowercase
// *up* ("AB70..ABBF; C; 13A0..13EF"), and x/text encodes case mappings as XOR
// deltas, which cannot express a mapping that is not an involution, so
// cases.Fold toggles these code points rather than holding them fixed
// (golang/go#46101). Folding is idempotent by definition, so the toggle is a
// defect; an implementation that inherited it would never settle.
const personCherokeeLo, personCherokeeHi = 0x13A0, 0x13F5

// personCaseFold applies Unicode default case folding, holding the Cherokee
// code points at their correct fixed points. Folding runs separately is exact
// because toCasefold is context-free.
func personCaseFold(s string) string {
	if !personHasCherokee(s) {
		return personFoldCaser.String(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	run := 0
	for i, r := range s {
		if r < personCherokeeLo || r > personCherokeeHi {
			continue
		}
		b.WriteString(personFoldCaser.String(s[run:i]))
		b.WriteRune(r)
		run = i + utf8.RuneLen(r)
	}
	b.WriteString(personFoldCaser.String(s[run:]))
	return b.String()
}

func personHasCherokee(s string) bool {
	for _, r := range s {
		if r >= personCherokeeLo && r <= personCherokeeHi {
			return true
		}
	}
	return false
}

// personNFC returns s in Normalization Form C.
//
// It does not ask x/text to normalize anything larger than a single rune or a
// single pair, because x/text's answer for a whole string is wrong in four
// ways. The first three are defects; the fourth is a deliberate behaviour that
// this specification does not want:
//
//  1. Over-composition. A composition pair is packed into a 32-bit key as
//     uint16(a)<<16|uint16(b) (unicode/norm/forminfo.go), so a starter above
//     U+FFFF is truncated to its low 16 bits and matches the BMP entry sharing
//     them: NFC of U+10041 U+0300 returns "À". 16,956 (starter, mark) pairs
//     compose falsely this way, and each one merges two distinct people.
//  2. Stream-Safe Text, applied unconditionally through String, Bytes and Iter
//     alike: past 30 consecutive non-starters it inserts U+034F and stops
//     composing. spec/identifiers.md forbids that, and it is reachable well
//     inside the 320-code-point bound.
//  3. Composing across a blocker. NFC of U+00C5 U+0BD7 U+0316 U+0301 returns
//     U+01FA U+0BD7 U+0316: the acute composed onto the base across a ccc-0
//     mark that blocks it, and was then dropped from the output.
//  4. Stream-safe *boundaries*. norm.NextBoundaryInString is driven by
//     streamSafe.next, so it reports a cut after 30 non-starters rather than a
//     position nothing composes across. x/text carries a TODO at
//     unicode/norm/normalize.go saying the two are not the same thing.
//
// A round-trip guard alone cannot cover this. NFD(NFC(x)) == NFD(x) catches
// (1), because a false composition is not canonically equivalent. It is blind
// to (2), because NFD inserts the same joiner and it is present on both sides
// of the comparison. It sees (3) only because a code point goes missing, and
// the only repair available to a guard — fall back to NFD — then discards the
// composition that was legitimate.
//
// So the composition is done here, segment by segment, over a canonically
// ordered decomposition, with x/text asked only the three questions it answers
// correctly: what one rune decomposes to, what a code point's combining class
// is, and whether one specific pair composes. All three are swept exhaustively
// against CPython, which shares no code with x/text.
func personNFC(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		n := personSegmentLen(s)
		b.WriteString(personNFCSegment(s[:n]))
		s = s[n:]
	}
	return b.String()
}

// personSegmentLen returns the byte length of the leading normalization segment of
// s: its first rune, plus every following rune that cannot begin a segment of
// its own.
//
// The rule is Properties.BoundaryBefore — ccc == 0 and does not combine
// backwards — applied rune by rune. That is the definition of a position
// nothing can compose across, so cutting there cannot separate a composing
// pair, and it has no length limit. See personNFC (4) for why this is not
// norm.NextBoundaryInString.
//
// Combining backwards, and not merely being a non-starter, is what keeps
// Hangul whole: V (U+1161..U+1175) and T (U+11A8..U+11C2) have ccc == 0 and
// compose onto the syllable before them, as do spacing marks such as Grantha
// U+1133E and Tamil U+0BBE. All of them report BoundaryBefore false and stay
// with their base.
func personSegmentLen(s string) int {
	for i := 0; i < len(s); {
		p := norm.NFC.PropertiesString(s[i:])
		if i > 0 && p.BoundaryBefore() {
			return i
		}
		n := p.Size()
		if n <= 0 {
			// Invalid UTF-8. Advance a byte so this cannot loop; a person
			// identifier is not required to be well formed for the fold to
			// terminate on it.
			n = 1
		}
		i += n
	}
	return len(s)
}

// personNFCSegment composes one normalization segment. See personNFC for why it does the
// composing itself.
func personNFCSegment(seg string) string {
	if !utf8.ValidString(seg) {
		// Not a conforming identifier at all. Return the bytes untouched
		// rather than route them through a decoder that would replace them:
		// normalization is not where malformed input is decided, and identity
		// is at least deterministic and lossless.
		return seg
	}
	return personCompose(personDecompose(seg))
}

// personDecompose returns seg canonically decomposed and canonically ordered,
// alongside each rune's combining class, which the caller needs too and which
// is measurably worth computing once.
//
// Decomposition is applied one rune at a time because it is context-free —
// NFD(xy) is NFD(x) followed by NFD(y), reordered — and because no single
// rune's decomposition is long enough to reach the stream-safe limit, so
// x/text answers each one correctly even where it cannot answer for the whole
// segment.
func personDecompose(seg string) ([]rune, []uint8) {
	rs := make([]rune, 0, len(seg))
	for _, r := range seg {
		rs = append(rs, []rune(norm.NFD.String(string(r)))...)
	}
	cc := make([]uint8, len(rs))
	for i, r := range rs {
		cc[i] = personCCC(r)
	}
	personCanonicalOrder(rs, cc)
	return rs, cc
}

// personCanonicalOrder applies UAX #15's Canonical Ordering Algorithm in place:
// non-starters are sorted by combining class, stably, and runes with ccc 0 are
// fixed points that nothing moves across.
//
// It sorts each maximal run of non-starters rather than the whole slice, and
// it is not the obvious insertion sort. A person identifier's segment length
// is bounded only by what an op body carries — Check is a producer-side guard
// and the fold deliberately does not call it — so this runs on attacker-chosen
// input on every reader of the repository, and the rule it replaced was
// linear. An O(m^2) sort here is an amplification vector, not a slow path.
func personCanonicalOrder(rs []rune, cc []uint8) {
	for i := 0; i < len(rs); {
		if cc[i] == 0 {
			i++
			continue
		}
		j := i
		for j < len(rs) && cc[j] != 0 {
			j++
		}
		personSortByCCC(rs[i:j], cc[i:j])
		i = j
	}
}

// personSortRunInsertionMax is the run length below which an insertion sort wins:
// almost every real combining sequence is one or two marks, and a counting
// sort's 256-entry histogram costs more than the whole run.
const personSortRunInsertionMax = 32

// personSortByCCC stably sorts one run of non-starters by combining class. Insertion
// sort for the short runs that occur in practice, counting sort — linear, and
// stable because it walks the run in order — for the long ones that make a
// quadratic sort worth attacking.
func personSortByCCC(rs []rune, cc []uint8) {
	if len(rs) < 2 {
		return
	}
	if len(rs) <= personSortRunInsertionMax {
		for i := 1; i < len(rs); i++ {
			r, c := rs[i], cc[i]
			j := i
			for ; j > 0 && cc[j-1] > c; j-- {
				rs[j], cc[j] = rs[j-1], cc[j-1]
			}
			rs[j], cc[j] = r, c
		}
		return
	}
	var counts [256]int
	for _, c := range cc {
		counts[c]++
	}
	sum := 0
	for i := range counts {
		counts[i], sum = sum, sum+counts[i]
	}
	outR := make([]rune, len(rs))
	outC := make([]uint8, len(cc))
	for i, c := range cc {
		outR[counts[c]], outC[counts[c]] = rs[i], c
		counts[c]++
	}
	copy(rs, outR)
	copy(cc, outC)
}

// personCompose applies UAX #15's Canonical Composition Algorithm to a canonically
// ordered decomposition. Every Unicode fact it needs — the combining classes,
// and whether a given pair composes — comes from x/text; nothing here is a
// table this repository has to keep current.
//
// It composes only onto rs[0] and never promotes a later starter to be a new
// base, which UAX #15 permits in general. That is sound here because of an
// empirical property of the pinned Unicode version rather than anything about
// the algorithm: of the 75 starters in 15.0.0 that combine backwards — and so
// stay inside a segment rather than beginning one — exactly zero are the first
// element of any composition. A later starter therefore has nothing to compose
// with even if it were promoted. TestPinnedUnicodeVersion is what keeps this
// true: a Unicode version bump has to re-establish it.
func personCompose(rs []rune, cc []uint8) string {
	if len(rs) == 0 {
		return ""
	}
	out := make([]rune, 0, len(rs))
	out = append(out, rs[0])
	// A segment that opens on a non-starter has no starter to compose onto.
	composable := cc[0] == 0
	// UAX #15 D115: c is blocked from the base when *any* character already
	// retained between them has ccc 0, or a class at least as high as c's.
	// Looking only at the last one is not enough — canonical ordering leaves
	// ccc-0 marks where they are, so a retained blocker can end up behind a
	// later mark of lower class and would otherwise be forgotten.
	blockedAll := false
	maxRetained := -1
	for i, c := range rs[1:] {
		n := int(cc[i+1])
		blocked := blockedAll || maxRetained >= n
		if composable && !blocked {
			if p, ok := personCombine(out[0], c); ok {
				out[0] = p
				continue
			}
		}
		out = append(out, c)
		if n == 0 {
			blockedAll = true
		} else if n > maxRetained {
			maxRetained = n
		}
	}
	return string(out)
}

// personCombine reports the primary composite of a and b, if there is one. It asks
// x/text, on a two-rune string that cannot reach the stream-safe limit, and
// applies the round-trip guard so a composition invented by the truncated key
// (personNFC (1)) is refused.
func personCombine(a, b rune) (rune, bool) {
	in := string([]rune{a, b})
	out := norm.NFC.String(in)
	if norm.NFD.String(out) != norm.NFD.String(in) {
		return 0, false
	}
	rs := []rune(out)
	if len(rs) == 1 {
		return rs[0], true
	}
	return 0, false
}

// personCCC reports a rune's canonical combining class. It encodes into a stack
// buffer rather than calling PropertiesString(string(r)), which heap-allocates
// on every call and is invoked once per code point of every identifier folded.
func personCCC(r rune) uint8 {
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return norm.NFC.Properties(buf[:n]).CCC()
}

// EffectiveTimes computes the causality-monotone effective timestamp
// t*(u) = max(u.time, max_{p in Parents_S(u)} t*(p))
// for all ops in the restricted input set for target objectID.
func EffectiveTimes(ops []OrderOp, objectID string) map[string]int64 {
	inSet := make(map[string]bool)
	for _, op := range ops {
		if op.ObjectID == objectID {
			inSet[op.ID] = true
		}
	}
	tStar, _ := EffectiveTimesInSet(ops, inSet)
	return tStar
}

// EffectiveTimesInSet computes effective timestamps for ops within an explicit inSet.
// Returns an error if a directed cycle is detected within the restricted DAG.
func EffectiveTimesInSet(ops []OrderOp, inSet map[string]bool) (map[string]int64, error) {
	tStar := make(map[string]int64, len(inSet))
	opMap := make(map[string]OrderOp, len(ops))
	for _, op := range ops {
		opMap[op.ID] = op
	}

	state := make(map[string]int, len(inSet)) // 0: unvisited, 1: visiting, 2: visited

	var getTStar func(id string) (int64, error)
	getTStar = func(id string) (int64, error) {
		if t, ok := tStar[id]; ok {
			return t, nil
		}
		if state[id] == 1 {
			return 0, fmt.Errorf("cycle detected involving op %q", id)
		}
		if state[id] == 2 {
			return tStar[id], nil
		}

		state[id] = 1
		op := opMap[id]
		res := op.Time
		for _, p := range op.Parents {
			if inSet[p] {
				pt, err := getTStar(p)
				if err != nil {
					return 0, err
				}
				if pt > res {
					res = pt
				}
			}
		}
		state[id] = 2
		tStar[id] = res
		return res, nil
	}

	for id := range inSet {
		if state[id] == 0 {
			if _, err := getTStar(id); err != nil {
				return nil, err
			}
		}
	}
	return tStar, nil
}

// TotalOrder produces the deterministic total order sequence L of ops for target objectID
// using Kahn's algorithm with a priority queue ordered by (t*, id).
//
// This is the spec's reference implementation of the total order algorithm defined in spec/fold.md §4.
func TotalOrder(ops []OrderOp, objectID string) ([]string, error) {
	inSet := make(map[string]bool)
	for _, op := range ops {
		if op.ObjectID == objectID {
			inSet[op.ID] = true
		}
	}
	if len(inSet) == 0 {
		return nil, nil
	}

	tStar, err := EffectiveTimesInSet(ops, inSet)
	if err != nil {
		return nil, fmt.Errorf("computing effective timestamps: %w", err)
	}

	inDegree := make(map[string]int, len(inSet))
	children := make(map[string][]string, len(inSet))
	for _, op := range ops {
		if !inSet[op.ID] {
			continue
		}
		var parentsInSet int
		for _, p := range op.Parents {
			if inSet[p] {
				parentsInSet++
				children[p] = append(children[p], op.ID)
			}
		}
		inDegree[op.ID] = parentsInSet
	}

	// Ready queue
	var ready []string
	for id, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}

	var order []string
	for len(ready) > 0 {
		// Pick ready op with minimal (t*, id)
		bestIdx := 0
		bestID := ready[0]
		bestT := tStar[bestID]

		for i := 1; i < len(ready); i++ {
			candID := ready[i]
			candT := tStar[candID]
			if candT < bestT || (candT == bestT && candID < bestID) {
				bestIdx = i
				bestID = candID
				bestT = candT
			}
		}

		// Remove chosen from ready
		ready = append(ready[:bestIdx], ready[bestIdx+1:]...)

		order = append(order, bestID)

		// Unblock children
		for _, ch := range children[bestID] {
			inDegree[ch]--
			if inDegree[ch] == 0 {
				ready = append(ready, ch)
			}
		}
	}

	if len(order) != len(inSet) {
		return nil, fmt.Errorf("cycle detected in restricted DAG: emitted %d of %d ops", len(order), len(inSet))
	}

	return order, nil
}

// BuildReachabilityMap computes transitive reachability (isAncestor) in the restricted DAG.
func BuildReachabilityMap(ops []MergeOp, inSet map[string]bool) map[string]map[string]bool {
	parentsMap := make(map[string][]string, len(inSet))
	for _, op := range ops {
		if !inSet[op.ID] {
			continue
		}
		var pList []string
		for _, p := range op.Parents {
			if inSet[p] {
				pList = append(pList, p)
			}
		}
		parentsMap[op.ID] = pList
	}

	ancestors := make(map[string]map[string]bool, len(inSet))
	for id := range inSet {
		ancestors[id] = make(map[string]bool)
	}

	var dfs func(curr, target string)
	dfs = func(curr, target string) {
		for _, p := range parentsMap[curr] {
			if !ancestors[target][p] {
				ancestors[target][p] = true
				dfs(p, target)
			}
		}
	}

	for id := range inSet {
		dfs(id, id)
	}

	return ancestors
}

// Fold is the spec's reference fold reducer. It executes deterministic fold reduction
// on an input set of operations against the declared catalogue field rules.
//
// This is the normative reference reducer used to produce and check golden fold outputs.
// Engine reducers (WRIT-25/26/27) are independent implementations validated against the same goldens.
func Fold(ops []MergeOp, rules []FieldRule) (map[string]any, error) {
	if len(ops) == 0 {
		return make(map[string]any), nil
	}

	objectID := ops[0].ObjectID
	inSet := make(map[string]bool)
	var orderOps []OrderOp
	opMap := make(map[string]MergeOp, len(ops))

	for _, op := range ops {
		opMap[op.ID] = op
		if op.ObjectID == objectID {
			inSet[op.ID] = true
		}
		orderOps = append(orderOps, OrderOp{
			ID:       op.ID,
			Parents:  op.Parents,
			Time:     op.Time,
			ObjectID: op.ObjectID,
		})
	}

	totalOrder, err := TotalOrder(orderOps, objectID)
	if err != nil {
		return nil, fmt.Errorf("spec: ordering ops: %w", err)
	}

	ancestors := BuildReachabilityMap(ops, inSet)
	isAncestor := func(a, b string) bool {
		return ancestors[b][a]
	}

	// Helper to check if an op matches a rule
	opMatchesRule := func(op MergeOp, r FieldRule) bool {
		if r.OpType != "" && r.OpType != op.OpType {
			return false
		}
		if r.OpVersion != 0 && op.OpVersion != 0 && r.OpVersion != op.OpVersion {
			return false
		}
		return true
	}

	// Find all rules that match ops actually present in the input set
	matchedRulesByField := make(map[string][]FieldRule)
	for _, r := range rules {
		for _, op := range ops {
			if !inSet[op.ID] {
				continue
			}
			if opMatchesRule(op, r) {
				if _, present := op.Body[r.Field]; present || op.OpType == "delete" || op.OpType == "undelete" {
					matchedRulesByField[r.Field] = append(matchedRulesByField[r.Field], r)
					break
				}
			}
		}
	}

	state := make(map[string]any)

	// Iterate fields in deterministic order
	var fieldNames []string
	for f := range matchedRulesByField {
		fieldNames = append(fieldNames, f)
	}
	sort.Strings(fieldNames)

	for _, fieldName := range fieldNames {
		frs := matchedRulesByField[fieldName]
		if len(frs) == 0 {
			continue
		}
		primaryRule := frs[0]

		switch primaryRule.Strategy {
		case "lww":
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if val, present := op.Body[fieldName]; present && val != nil {
							if s, ok := val.(string); ok && fieldName == "resolved_by" && op.OpType == "resolve" {
								val = normalizePerson(s)
							}
							state[fieldName] = val
						}
					}
				}
			}

		case "create-once":
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if _, alreadySet := state[fieldName]; !alreadySet {
							if val, present := op.Body[fieldName]; present && val != nil {
								state[fieldName] = val
							}
						}
					}
				}
			}

		case "set-union":
			unionSet := make(map[string]bool)
			hasSet := false
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if raw, present := op.Body[fieldName]; present {
							hasSet = true
							// Elements that are the empty string are dropped (spec/fold.md §5.3).
							add := func(item string) {
								if item != "" {
									unionSet[item] = true
								}
							}
							switch val := raw.(type) {
							case []any:
								for _, item := range val {
									add(fmt.Sprint(item))
								}
							case []string:
								for _, item := range val {
									add(item)
								}
							case string:
								add(val)
							}
						}
					}
				}
			}
			if hasSet {
				result := make([]string, 0, len(unionSet))
				for k := range unionSet {
					result = append(result, k)
				}
				sort.Strings(result)
				state[fieldName] = result
			}

		case "set-observed-remove":
			type addRecord struct {
				opID string
				item string
			}
			var adds []addRecord
			type removeRecord struct {
				opID string
				item string
			}
			var removes []removeRecord
			hasOps := false

			for _, op := range ops {
				if !inSet[op.ID] {
					continue
				}
				for _, r := range frs {
					if opMatchesRule(op, r) {
						var addRaw, remRaw any
						if bodyMap, ok := op.Body[fieldName].(map[string]any); ok {
							addRaw = bodyMap["add"]
							remRaw = bodyMap["remove"]
						} else {
							if raw, ok := op.Body[fieldName]; ok && raw != nil {
								if fieldName == "add" {
									addRaw = raw
									remRaw = op.Body["remove"]
								} else if fieldName == "remove" {
									remRaw = raw
									addRaw = op.Body["add"]
								}
							}
						}

						if addRaw != nil || remRaw != nil {
							hasOps = true
							// Person-valued fields normalize per spec/identifiers.md;
							// every other item is taken verbatim. Items that are empty
							// after normalization are dropped from both sides of the
							// OR-set, whatever the op type (spec/fold.md §5.4).
							normalizeItem := func(it string) string {
								if op.OpType == "assign" {
									return normalizePerson(it)
								}
								return it
							}

							if slice, ok := addRaw.([]any); ok {
								for _, it := range slice {
									if item := normalizeItem(fmt.Sprint(it)); item != "" {
										adds = append(adds, addRecord{opID: op.ID, item: item})
									}
								}
							} else if slice, ok := addRaw.([]string); ok {
								for _, it := range slice {
									if item := normalizeItem(it); item != "" {
										adds = append(adds, addRecord{opID: op.ID, item: item})
									}
								}
							}
							if slice, ok := remRaw.([]any); ok {
								for _, it := range slice {
									if item := normalizeItem(fmt.Sprint(it)); item != "" {
										removes = append(removes, removeRecord{opID: op.ID, item: item})
									}
								}
							} else if slice, ok := remRaw.([]string); ok {
								for _, it := range slice {
									if item := normalizeItem(it); item != "" {
										removes = append(removes, removeRecord{opID: op.ID, item: item})
									}
								}
							}
						}
					}
				}
			}

			if hasOps {
				presentSet := make(map[string]bool)
				for _, add := range adds {
					removed := false
					for _, rem := range removes {
						if rem.item == add.item && isAncestor(add.opID, rem.opID) {
							removed = true
							break
						}
					}
					if !removed {
						presentSet[add.item] = true
					}
				}
				result := make([]string, 0, len(presentSet))
				for k := range presentSet {
					result = append(result, k)
				}
				sort.Strings(result)
				state[fieldName] = result
			}

		case "append":
			var list []any
			hasAppend := false
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if raw, present := op.Body[fieldName]; present {
							hasAppend = true
							if slice, ok := raw.([]any); ok {
								list = append(list, slice...)
							} else {
								list = append(list, raw)
							}
						}
					}
				}
			}
			if hasAppend {
				state[fieldName] = list
			}

		case "tombstone":
			var deletes []string
			var undeletes []string
			hasTombstone := false
			for _, op := range ops {
				if !inSet[op.ID] {
					continue
				}
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if op.OpType == "delete" || op.Body[fieldName] == true {
							deletes = append(deletes, op.ID)
							hasTombstone = true
						} else if op.OpType == "undelete" || op.Body[fieldName] == false {
							undeletes = append(undeletes, op.ID)
							hasTombstone = true
						}
					}
				}
			}

			if hasTombstone {
				isDeleted := false
				for _, d := range deletes {
					cleared := false
					for _, u := range undeletes {
						if isAncestor(d, u) {
							cleared = true
							break
						}
					}
					if !cleared {
						isDeleted = true
						break
					}
				}
				state[fieldName] = isDeleted
			}

		case "lattice":
			rankMap := make(map[string]int, len(primaryRule.Lattice))
			for i, elem := range primaryRule.Lattice {
				rankMap[elem] = i
			}
			currentRank := -1
			var currentVal string
			hasLattice := false
			for _, id := range totalOrder {
				op := opMap[id]
				for _, r := range frs {
					if opMatchesRule(op, r) {
						if raw, present := op.Body[fieldName]; present {
							valStr := fmt.Sprint(raw)
							if rk, ok := rankMap[valStr]; ok {
								if rk > currentRank {
									currentRank = rk
									currentVal = valStr
									hasLattice = true
								}
							}
						}
					}
				}
			}
			if hasLattice {
				state[fieldName] = currentVal
			}

		case "keyed-lww":
			type keyedEntry struct {
				key   []string
				value any
			}
			latest := make(map[string]*keyedEntry)
			hasKeyed := false
			for _, id := range totalOrder {
				op := opMap[id]
				for _, rule := range frs {
					if opMatchesRule(op, rule) {
						val, present := op.Body[fieldName]
						if !present {
							continue
						}
						hasKeyed = true
						// The stored value is normalized on the same terms as the
						// key component it mirrors: a person identifier reads back
						// normalized per spec/identifiers.md.
						if fieldName == "subject" && op.OpType == "approval" {
							if s, isStr := val.(string); isStr {
								val = normalizePerson(s)
							}
						}
						key := make([]string, 0, len(rule.Key))
						for _, kf := range rule.Key {
							if val, ok := op.Body[kf]; ok && val != nil {
								vStr := fmt.Sprint(val)
								if kf == "subject" && op.OpType == "approval" {
									vStr = normalizePerson(vStr)
								}
								key = append(key, vStr)
							} else {
								key = append(key, "")
							}
						}
						latest[fmt.Sprintf("%q", key)] = &keyedEntry{key: key, value: val}
					}
				}
			}

			if hasKeyed {
				entries := make([]*keyedEntry, 0, len(latest))
				for _, e := range latest {
					entries = append(entries, e)
				}
				sort.Slice(entries, func(i, j int) bool {
					a, b := entries[i].key, entries[j].key
					for x := range a {
						if a[x] != b[x] {
							return a[x] < b[x]
						}
					}
					return false
				})
				keyed := make([]any, 0, len(entries))
				for _, e := range entries {
					keyed = append(keyed, map[string]any{"key": e.key, "value": e.value})
				}
				state[fieldName] = keyed
			}
		}
	}

	return state, nil
}
