# 0001: Canonical JSON encoding approach

Status: decided (spike). Feeds WRIT-6 (op envelope schema & canonicalization
rules), which owns the actual spec text and fixture corpus.

## Problem

Op signing and content-addressing need byte-stable encoding: two producers
building the logically-same op payload must produce identical bytes, or
signatures won't verify and content hashes won't match. JSON has no
canonical form on its own — key order, number formatting, and string
escaping are all underspecified, so `json.Marshal` twice on equivalent data
isn't guaranteed to agree even within one language, let alone across
implementations.

## Candidates surveyed

**RFC 8785 (JSON Canonicalization Scheme / JCS).** The existing standard for
this problem: sorts object members by UTF-16 code unit order, formats
numbers via the ECMAScript `Number::toString` algorithm, and defines minimal
string escaping. Go implementations exist —
[`gowebpki/jcs`](https://github.com/gowebpki/jcs) and
[`gibson042/canonicaljson-go`](https://github.com/gibson042/canonicaljson-go)
are the two with any real adoption; `cyberphone/json-canonicalization`
ships a Go port alongside the reference implementations in several other
languages. All three implement essentially the same ~150-300 line
algorithm; none carries transitive dependencies of its own, so pulling one
in would cost little in dependency-surface terms. Maintenance signal is
thin either way — these are small, stable-by-nature libraries (the RFC
doesn't change), not actively-developed frameworks, so "last commit date"
isn't a meaningful health signal the way it would be for a larger project.

**Bespoke encoder, following RFC 8785's algorithm shape.** Same output
contract (UTF-16 key order, ES-style number formatting, minimal escaping),
implemented directly against `encoding/json`, `unicode/utf16`, and
`strconv` — no third-party code.

## Decision

Bespoke, implemented in `engine/codec/canonicaljson`. Reasoning:

- **House rule is stdlib-first, and this algorithm doesn't strain the
  standard library.** `encoding/json` with `UseNumber()` gives a
  duplicate-key-collapsing decode; `unicode/utf16.Encode` gives exact
  UTF-16 code-unit ordering; `strconv.AppendFloat(f, 'e', -1, 64)` gives
  the shortest round-trip digit string, which the ES `Number::toString`
  reformatting (decimal vs. exponential, threshold at 1e21 / 1e-6) is a
  straightforward transform of. None of this needed a dependency to get
  right — see the "supplementary-plane vs BMP" test vector, which is the
  one genuinely easy-to-get-wrong case (naive Go string comparison sorts
  by UTF-8 byte order, which disagrees with UTF-16 code-unit order exactly
  where it matters) and which the stdlib-only implementation passes.
- **We don't need JS interop.** RFC 8785's number-formatting fidelity to
  `Number::toString` exists so a canonicalizer can agree with a JavaScript
  engine byte-for-byte. Writ has no JS runtime canonicalizing payloads
  anywhere in its architecture — every producer and verifier is this same
  Go package. Matching the RFC's algorithm is still useful (it's a
  well-specified, already-debugged design to copy), but strict RFC 8785
  compliance isn't load-bearing, so a dependency bought us conformance to a
  constraint we don't actually have.
- **Small enough to own.** ~200 lines, and it's exactly the kind of code
  the house rules call out as needing to stay "boring and correct" — a
  vendored dependency doesn't reduce the review burden much below owning it
  directly, since the failure mode (a subtly wrong float or key-order edge
  case) is caught the same way either path: fixtures.

## What this does and doesn't guarantee

- Numbers are IEEE-754 doubles, per JSON's (and RFC 8785's) numeric model.
  Integers beyond 2^53 silently lose precision — see the `9007199254740993`
  test vector. **Recommendation for WRIT-6:** any op envelope field needing
  an exact large integer (sequence numbers, anything used as a tiebreak)
  should be typed as a JSON string, not a JSON number, in the schema —
  sidesteps this rather than relying on producers never overflowing 2^53.
- Duplicate object keys collapse to the last value during decode (Go's
  `encoding/json` behavior), before canonicalization ever sees them. That
  means canonical bytes can't distinguish "no duplicate keys in the
  original" from "duplicate keys, last one implicitly chosen." If the op
  envelope's signature is meant to attest to the *original* producer-side
  bytes rather than just the canonical form, WRIT-6 should decide whether
  op decoding rejects duplicate keys outright rather than silently
  resolving them.
- Non-finite numbers (`NaN`, `Infinity`) are rejected — they have no JSON
  or RFC 8785 representation.
- No opinion yet on where canonicalization sits relative to signing in the
  op envelope pipeline (that's WRIT-6's schema to define); this package's
  contract is just `Marshal(json []byte) (canonical []byte, error)`.

## Prototype and test vectors

`engine/codec/canonicaljson/canonicaljson.go`, with
`engine/codec/canonicaljson/testdata/vectors.json` covering key ordering
(ASCII, nested, duplicate keys, prefix-key tie-breaks, the UTF-16-vs-UTF-8
surrogate-pair case), string escaping (all five shorthand escapes, other
control characters, unescaped forward slash, raw non-ASCII passthrough),
and numbers (negative zero, plain negatives, the 1e-6/1e21 notation
thresholds exactly at the boundary, scientific-notation input
normalization, precision loss beyond 2^53). `canonicaljson_test.go` checks
each vector round-trips to its expected canonical form and that
canonicalization is idempotent (canonical bytes are a fixed point), which
is what signing depends on.

These vectors are meant to seed WRIT-6's fixture corpus directly; the
`{name, input, canonical}` shape was chosen so they can be lifted in
largely as-is.
