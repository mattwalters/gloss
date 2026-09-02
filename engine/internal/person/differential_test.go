package person_test

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/unicode/rangetable"

	"github.com/writtendev/writ/engine/internal/person"
)

// reference is the folding algorithm spec/identifiers.md specifies, written in
// somebody else's Unicode implementation. CPython carries its own tables and
// shares no code with x/text, so agreeing with it is evidence that the
// algorithm is portable rather than evidence that one library is
// self-consistent — which is the only claim worth making about a rule whose
// whole job is for two independent implementations to reach the same answer.
const reference = `
import sys, unicodedata
out = []
for line in sys.stdin.buffer.read().decode('utf-8').splitlines():
    if not line:
        out.append('')
        continue
    s = ''.join(chr(int(h, 16)) for h in line.split())
    f = unicodedata.normalize('NFC', unicodedata.normalize('NFC', s).casefold())
    out.append(' '.join('%X' % ord(c) for c in f))
sys.stdout.write('\n'.join(out))
`

// TestDifferentialAgainstCPython folds every code point assigned in the pinned
// Unicode version, every canonical composition, and every truncated-key
// impostor pair, and requires CPython to agree on all of them.
//
// Inputs are restricted to what Unicode 15.0.0 had assigned, because the host
// CPython is generally a later version: for characters assigned in the pinned
// version, Unicode's stability policies make composition and case folding
// fixed, so a disagreement is this implementation's defect and not a version
// skew. Skipped where python3 is unavailable.
func TestDifferentialAgainstCPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available; the differential reference cannot be run")
	}

	assigned := rangetable.Assigned(person.UnicodeVersion)
	if assigned == nil {
		t.Skipf("x/text carries no range table for Unicode %s", person.UnicodeVersion)
	}

	var inputs []string
	// Every code point in an input must be assigned in the pinned version.
	// The filter is not decoration: the impostor sweep below reaches into
	// higher planes, and a code point CPython knows about and Unicode 15.0.0
	// does not would report a version difference as an implementation defect.
	// U+10D57 (Garay, assigned in 16.0.0) is one such, and did.
	add := func(rs ...rune) {
		hex := make([]string, len(rs))
		for i, r := range rs {
			if !unicode.Is(assigned, r) {
				return
			}
			hex[i] = strconv.FormatInt(int64(r), 16)
		}
		inputs = append(inputs, strings.Join(hex, " "))
	}

	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		add(r)
	}
	singles := len(inputs)

	pairs := canonicalCompositions(t)
	for _, p := range pairs {
		add(p.a, p.b)
		for plane := rune(1); plane <= 16; plane++ {
			if a := p.a + plane<<16; a <= 0x10FFFF {
				add(a, p.b)
			}
			if b := p.b + plane<<16; b <= 0x10FFFF {
				add(p.a, b)
			}
		}
	}
	t.Logf("differential inputs: %d single code points, %d pair cases, %d total",
		singles, len(inputs)-singles, len(inputs))

	cmd := exec.Command(python, "-c", reference)
	cmd.Stdin = strings.NewReader(strings.Join(inputs, "\n"))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the reference implementation: %v\n%s", err, stderr.String())
	}

	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	mismatches := 0
	for i := 0; sc.Scan(); i++ {
		if i >= len(inputs) {
			t.Fatalf("reference returned more lines than it was given")
		}
		want, err := decodeHexRunes(sc.Text())
		if err != nil {
			t.Fatalf("decoding reference output for %q: %v", inputs[i], err)
		}
		in, err := decodeHexRunes(inputs[i])
		if err != nil {
			t.Fatalf("decoding input %q: %v", inputs[i], err)
		}
		if got := person.FoldValue(in); got != want {
			mismatches++
			if mismatches <= 20 {
				t.Errorf("FoldValue(%U) = %U, CPython says %U",
					[]rune(in), []rune(got), []rune(want))
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading reference output: %v", err)
	}
	if mismatches > 0 {
		t.Errorf("%d of %d inputs disagree with the reference implementation", mismatches, len(inputs))
	}
}

// TestDifferentialCatchesTheDefects is the differential's own test. A
// reference that agrees with everything proves nothing, so run the raw x/text
// pipeline — the one without the Cherokee fixed points and without the
// composition guard — past the same reference and require it to disagree.
func TestDifferentialCatchesTheDefects(t *testing.T) {
	naive := func(s string) string {
		return norm.NFC.String(person.CaseFoldRaw(norm.NFC.String(s)))
	}
	cases := []struct {
		in  string
		why string
	}{
		{"Ꭰ", "Cherokee fold toggle"},
		{"\U00010041̀", "truncated composition key"},
	}
	for _, tc := range cases {
		if naive(tc.in) == person.FoldValue(tc.in) {
			t.Errorf("the unguarded pipeline agrees with the guarded one on %U (%s); "+
				"the guard is not being exercised", []rune(tc.in), tc.why)
		}
	}
}

func decodeHexRunes(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	var b strings.Builder
	for _, h := range strings.Fields(s) {
		v, err := strconv.ParseInt(h, 16, 32)
		if err != nil {
			return "", fmt.Errorf("bad code point %q: %w", h, err)
		}
		b.WriteRune(rune(v))
	}
	return b.String(), nil
}
