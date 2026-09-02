// Package person holds the person-identifier grammar and normalization rule
// from spec/identifiers.md. It exists as its own package so that the fold,
// which must stay free of I/O, and the packages above it can share one
// definition of the rule without any of them importing a package that can
// spawn processes.
//
// Its imports are strings, unicode/utf8, and the two golang.org/x/text
// packages that carry the Unicode tables the normalization rule is defined
// over. All four are pure table-driven computation: no filesystem, no network,
// no process spawning. That is what makes this package's entry in
// engine/internal/fold's import allowlist grant no capability. Keep it that
// way — anything reached from here is reachable from the fold.
package person

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Length bounds from spec/identifiers.md §Person identifiers. MaxLen is
// derived, not an independent number: a scheme, the colon, and a value.
const (
	MaxSchemeLen = 32
	MaxValueLen  = 320
	MaxLen       = MaxSchemeLen + 1 + MaxValueLen // 353
)

// UnicodeVersion is the Unicode version spec/identifiers.md pins the
// normalization algorithm to.
//
// It is stated here so it can be checked. x/text selects its tables by Go
// build tag rather than by module version — go1.27 switches both packages to
// Unicode 17.0.0 — so the same source normalizes differently depending on who
// compiles it. TestPinnedUnicodeVersion binds this constant to the tables
// actually compiled in, and a toolchain bump therefore fails loudly and is
// answered with a deliberate spec amendment, rather than silently
// renormalizing every identifier in every repository.
const UnicodeVersion = "15.0.0"

// Split splits a person identifier into its scheme and value on the FIRST
// colon, per spec/identifiers.md. The first colon and not "a colon": an email
// address may legally carry a colon inside a quoted local part, so
// `email:"a:b"@example.com` is scheme `email` with value `"a:b"@example.com`.
//
// ok is false when s carries no colon at all; such a string is not a
// conforming person identifier (there is no bare form and no implicit
// scheme), and it is returned whole as the value so callers that must
// preserve it can.
func Split(s string) (scheme, value string, ok bool) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", s, false
	}
	return s[:i], s[i+1:], true
}

// NormalizePerson normalizes a person identifier string per
// spec/identifiers.md: the scheme is lowercased, and the value is trimmed of
// leading and trailing whitespace and folded by FoldValue.
//
// The scheme is lowercased with strings.ToLower rather than folded. A
// conforming scheme matches [a-z][a-z0-9+.-]*, so it is ASCII and the two
// agree on every scheme the grammar admits; a scheme they would disagree
// about is not a conforming identifier in the first place.
//
// A string carrying no colon is not a conforming identifier. It is folded as
// a flat string and returned rather than rejected: what a reader does with a
// non-conforming identifier is a separate decision (WRIT-124/126), and
// normalization is not the place to make it.
func NormalizePerson(s string) string {
	s = strings.TrimSpace(s)
	scheme, value, ok := Split(s)
	if !ok {
		return FoldValue(s)
	}
	return strings.ToLower(scheme) + ":" + FoldValue(strings.TrimSpace(value))
}

// FoldValue applies the value half of the normalization rule in
// spec/identifiers.md §Normalization rules, pinned to Unicode UnicodeVersion:
//
//  1. NFC
//  2. Unicode default case folding (UAX #21 §2.3 toCasefold, the full C+F
//     mappings, no locale tailoring)
//  3. NFC again
//
// One algorithm for every scheme. The trailing NFC is not redundant: case
// folding does not preserve a normal form, so folding NFC input can leave a
// composable sequence behind — U+017F followed by U+0301 folds to "s" plus
// U+0301, which is not NFC — and a rule that stopped after folding would not
// be idempotent. Normalization is applied at the producer, in the fold and
// again in the projection, so a rule that changed its answer on the second
// pass would be the same interop defect it exists to remove.
func FoldValue(s string) string {
	// ASCII is already NFC, and folding it is ASCII lowercasing, which is
	// what almost every real identifier needs. TestFoldValueASCIIFastPath
	// checks the shortcut against the general path rather than assuming it.
	if isASCII(s) {
		return lowerASCII(s)
	}
	return nfc(caseFold(nfc(s)))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func lowerASCII(s string) string {
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

// foldCaser performs Unicode default case folding. cases.Fold documents the
// returned Caser as stateless and safe for concurrent use, so one is shared.
var foldCaser = cases.Fold()

// Cherokee uppercase letters case-fold to themselves. Cherokee is the one
// script whose folding maps lowercase *up* — CaseFolding.txt has
// "AB70..ABBF; C; 13A0..13EF" and "13F8..13FD; C; 13F0..13F5" — and x/text
// encodes case mappings as XOR deltas, which cannot express a mapping that is
// not an involution. cases.Fold therefore toggles these code points instead of
// holding them fixed: it answers U+AB70 for U+13A0, where CPython, ICU and
// CaseFolding.txt all answer U+13A0 (golang/go#46101, open since 2021).
//
// Folding is idempotent by definition, so a toggle is a defect rather than a
// tailoring, and a normalization built on it would not settle: U+13A0 and
// U+AB70 would swap places on every pass.
const cherokeeFoldedLo, cherokeeFoldedHi = 0x13A0, 0x13F5

// caseFold applies Unicode default case folding, holding the Cherokee code
// points x/text toggles at their correct fixed points.
//
// Folding runs of the string separately and copying the fixed points through
// gives the same answer as folding the whole string would, because toCasefold
// is context-free: unlike lowercasing, which has the final-sigma rule, no
// case-folding mapping depends on neighbouring characters.
func caseFold(s string) string {
	if !hasCherokeeFoldedRune(s) {
		return foldCaser.String(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	run := 0
	for i, r := range s {
		if r < cherokeeFoldedLo || r > cherokeeFoldedHi {
			continue
		}
		b.WriteString(foldCaser.String(s[run:i]))
		b.WriteRune(r)
		run = i + utf8.RuneLen(r)
	}
	b.WriteString(foldCaser.String(s[run:]))
	return b.String()
}

func hasCherokeeFoldedRune(s string) bool {
	for _, r := range s {
		if r >= cherokeeFoldedLo && r <= cherokeeFoldedHi {
			return true
		}
	}
	return false
}

// nfc composes s in Normalization Form C.
//
// It cannot hand the whole string to norm.NFC and trust the answer. x/text
// packs a composition pair into a 32-bit key as uint16(a)<<16|uint16(b)
// (unicode/norm/forminfo.go), so a starter above U+FFFF is truncated to its
// low 16 bits and matches the BMP entry that shares them: NFC of U+10041
// followed by U+0300 returns "À", where the correct answer leaves both code
// points alone. 16,956 (starter, mark) pairs compose falsely this way. Every
// one of them merges two distinct people into one — the failure
// spec/identifiers.md §Length bounds refuses to accept from truncation,
// arriving by a different road — and it puts this implementation at odds with
// any other, which is the one property the format has to deliver.
//
// Rather than reimplement composition or hardcode the affected ranges, nfc
// checks the invariant. NFD is trie-driven, does not use the packed key, and
// is unaffected, so NFD(NFC(x)) == NFD(x) is an independent verification that
// what NFC returned is canonically equivalent to what it was given. A false
// composition fails it. Checking the invariant rather than the symptom also
// catches any other composition defect, present or future.
//
// The check is per normalization segment, so a bad pair in one segment cannot
// discard the compositions in the segments around it, and no composing pair is
// ever split — Hangul jamo, Kaithi U+11099 U+110BA and Grantha U+11347 U+1133E
// all stay whole. See segmentLen for where a segment ends and why that is not
// the boundary x/text's NextBoundary reports. A segment that fails falls back
// to its decomposed form, which is canonical, so two spellings of the same
// value still normalize alike.
//
// The guard cannot block a legitimate composition: of the 1088 canonical
// composing pairs in Unicode 15.0.0 no two share a truncated key, so a pair
// the guard rejects was never a composition to begin with.
func nfc(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		n := segmentLen(s)
		b.WriteString(nfcSegment(s[:n]))
		s = s[n:]
	}
	return b.String()
}

// segmentLen returns the byte length of the leading normalization segment of
// s: its first rune, plus every following rune that cannot begin a segment of
// its own.
//
// The rule is Properties.BoundaryBefore — ccc == 0 and does not combine
// backwards — applied rune by rune. That is the definition of a position
// nothing can compose across, so cutting there cannot separate a composing
// pair. It is deliberately not norm.NextBoundaryInString, which is a
// *stream-safe* boundary: that loop is driven by streamSafe.next and cuts
// after 30 consecutive non-starters, mid-segment, so a mark past the cut can
// no longer reach the starter before it. x/text carries a TODO at
// unicode/norm/normalize.go saying the two are not the same thing.
//
// The difference is reachable well inside the 320-code-point bound and splits
// one person into two: "a" followed by thirty U+0316 and then U+0301 must fold
// to the same value as U+00E1 followed by thirty U+0316, and under a
// stream-safe cut it does not. TestLengthAxisDifferentialAgainstCPython covers
// the whole axis.
//
// Combining backwards, and not merely being a non-starter, is what keeps
// Hangul whole: V (U+1161..U+1175) and T (U+11A8..U+11C2) have ccc == 0 and
// compose onto the syllable before them, as do spacing marks such as Grantha
// U+1133E and Tamil U+0BBE. All of them report BoundaryBefore false and stay
// with their base.
func segmentLen(s string) int {
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

// nfcSegment composes one normalization segment.
//
// It does not ask x/text to normalize the segment. Three separate defects make
// that answer untrustworthy, and only the first is one the round-trip guard
// can see:
//
//   - Over-composition. The packed 32-bit key composes pairs that are not
//     compositions (see nfc). NFD(NFC(x)) != NFD(x) catches this.
//   - Stream-Safe Text. Past maxNonStarters consecutive non-starters, every
//     x/text entry point — String, Bytes, Iter — inserts U+034F and stops
//     composing. NFD inserts the same joiner, so the guard is blind: it is
//     present on both sides of the comparison.
//   - Composing across a blocker. NFC of U+00C5 U+0BD7 U+0316 U+0301 returns
//     U+01FA U+0BD7 U+0316: the acute composed onto the base across a ccc-0
//     mark that blocks it, and was then dropped from the output. CPython
//     returns the input unchanged. The guard sees this one only because a
//     code point went missing, and falling back to NFD then loses the ring
//     that legitimately composes.
//
// So the composition is done here, over a canonically ordered decomposition,
// with x/text asked only the questions it answers correctly: what a single
// rune decomposes to, what a code point's combining class is, and whether one
// specific pair composes. Those three are swept exhaustively against CPython.
func nfcSegment(seg string) string {
	if !utf8.ValidString(seg) {
		// Not a conforming identifier at all. Return the bytes untouched
		// rather than route them through a decoder that would replace them:
		// normalization is not where malformed input is decided, and identity
		// is at least deterministic and lossless.
		return seg
	}
	return compose(decompose(seg))
}

// decompose returns seg in canonical decomposition, canonically ordered.
//
// Decomposition is applied one rune at a time because it is context-free —
// NFD(xy) is NFD(x) followed by NFD(y), reordered — and because no single
// rune's decomposition is long enough to reach the stream-safe limit, so
// x/text answers each one correctly even where it cannot answer for the whole
// segment.
func decompose(seg string) []rune {
	var out []rune
	for _, r := range seg {
		out = append(out, []rune(norm.NFD.String(string(r)))...)
	}
	canonicalOrder(out)
	return out
}

// canonicalOrder sorts non-starters by canonical combining class, stably, in
// place: UAX #15's Canonical Ordering Algorithm. Runes with ccc 0 are fixed
// points and nothing moves across them.
func canonicalOrder(rs []rune) {
	for i := 1; i < len(rs); i++ {
		c := ccc(rs[i])
		if c == 0 {
			continue
		}
		for j := i; j > 0 && ccc(rs[j-1]) > c; j-- {
			rs[j-1], rs[j] = rs[j], rs[j-1]
		}
	}
}

// compose applies UAX #15's Canonical Composition Algorithm to a canonically
// ordered decomposition. Every Unicode fact it needs — the combining classes,
// and whether a given pair composes — comes from x/text; nothing here is a
// table this repository has to keep current.
func compose(rs []rune) string {
	if len(rs) == 0 {
		return ""
	}
	out := make([]rune, 0, len(rs))
	out = append(out, rs[0])
	// A segment that opens on a non-starter has no starter to compose onto.
	composable := ccc(rs[0]) == 0
	// UAX #15 D115: c is blocked from the base when *any* character already
	// retained between them has ccc 0, or a class at least as high as c's.
	// Looking only at the last one is not enough — canonical ordering leaves
	// ccc-0 marks where they are, so a retained blocker can end up behind a
	// later mark of lower class and would otherwise be forgotten.
	blockedAll := false
	maxRetained := -1
	for _, c := range rs[1:] {
		cc := int(ccc(c))
		blocked := blockedAll || maxRetained >= cc
		if composable && !blocked {
			if p, ok := combine(out[0], c); ok {
				out[0] = p
				continue
			}
		}
		out = append(out, c)
		if cc == 0 {
			blockedAll = true
		} else if cc > maxRetained {
			maxRetained = cc
		}
	}
	return string(out)
}

// combine reports the primary composite of a and b, if there is one. It asks
// x/text, on a two-rune string that cannot reach the stream-safe limit, and
// applies the same round-trip guard as nfcSegment so a composition invented by
// the truncated key is refused here too.
func combine(a, b rune) (rune, bool) {
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

func ccc(r rune) uint8 { return norm.NFC.PropertiesString(string(r)).CCC() }

// Problem names the ways a string can fail to be a conforming person
// identifier. It is an enumeration rather than an error so that this package
// stays free of anything the fold must not reach; callers turn it into a
// message.
type Problem int

const (
	// Valid means the identifier conforms.
	Valid Problem = iota
	// MissingScheme means the identifier carries no colon at all. There is no
	// bare form and no implicit scheme.
	MissingScheme
	// SchemeCharset means the scheme is empty or carries a character outside
	// [a-z][a-z0-9+.-]*.
	SchemeCharset
	// SchemeTooLong means the scheme exceeds MaxSchemeLen.
	SchemeTooLong
	// EmptyValue means the value is empty.
	EmptyValue
	// ValueTooLong means the value exceeds MaxValueLen code points.
	ValueTooLong
)

// String describes the problem for use in a caller's error message.
func (p Problem) String() string {
	switch p {
	case Valid:
		return "valid"
	case MissingScheme:
		return "missing scheme (expected scheme:value, for example email:alice@example.com or user:alice)"
	case SchemeCharset:
		return "scheme must match [a-z][a-z0-9+.-]*"
	case SchemeTooLong:
		return "scheme is longer than 32 characters"
	case EmptyValue:
		return "value is empty"
	case ValueTooLong:
		return "value is longer than 320 characters"
	}
	return "unknown problem"
}

// Check reports whether s is a conforming person identifier per
// spec/identifiers.md. s is expected to be normalized already; Check tests the
// grammar and the bounds, not normalization.
//
// The bounds are enforced by rejection, never by truncation: two distinct
// identifiers truncated to the same string would collapse into one person for
// assignment, approval keying and set membership.
//
// Check is a producer-side guard. The fold does not call it: what a reader
// does with a non-conforming identifier it has already read is decided
// separately (WRIT-124/126).
func Check(s string) Problem {
	scheme, value, ok := Split(s)
	if !ok {
		return MissingScheme
	}
	if !validScheme(scheme) {
		return SchemeCharset
	}
	// A scheme is ASCII by its charset, so bytes and code points coincide.
	if len(scheme) > MaxSchemeLen {
		return SchemeTooLong
	}
	if value == "" {
		return EmptyValue
	}
	if countRunes(value) > MaxValueLen {
		return ValueTooLong
	}
	return Valid
}

// validScheme reports whether scheme matches [a-z][a-z0-9+.-]*.
func validScheme(scheme string) bool {
	if scheme == "" {
		return false
	}
	for i := 0; i < len(scheme); i++ {
		c := scheme[i]
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && c >= '0' && c <= '9':
		case i > 0 && (c == '+' || c == '.' || c == '-'):
		default:
			return false
		}
	}
	return true
}

// countRunes counts code points, the unit JSON Schema maxLength counts, so the
// engine accepts exactly what spec/schemas/identifiers.schema.json accepts.
func countRunes(s string) int {
	return utf8.RuneCountInString(s)
}
