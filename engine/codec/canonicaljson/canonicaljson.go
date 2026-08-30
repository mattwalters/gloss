// Package canonicaljson encodes arbitrary JSON as byte-stable canonical
// JSON: the same logical value always produces the same bytes, which is
// what op signing and content-addressing need. See
// docs/decisions/0001-canonical-json-encoding.md for why this is a small
// bespoke encoder rather than an RFC 8785 library, and what it
// deliberately does not guarantee.
package canonicaljson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Marshal parses src as a single JSON value and re-encodes it in
// canonical form: object members sorted by UTF-16 code unit order of
// their keys, numbers formatted per the ECMAScript Number::toString
// algorithm, and strings re-escaped with the minimal required escapes.
//
// Numbers are decoded as IEEE 754 double-precision floats, so integers
// beyond 2^53 lose precision exactly as they would in JavaScript or any
// other double-based JSON consumer — see the decision doc for why op
// payload fields that need exact large integers should carry them as
// strings instead.
func Marshal(src []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("canonicaljson: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("canonicaljson: trailing data after JSON value")
	}
	var buf bytes.Buffer
	if err := encodeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeValue(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		return encodeNumber(buf, t)
	case string:
		encodeString(buf, t)
	case []any:
		return encodeArray(buf, t)
	case map[string]any:
		return encodeObject(buf, t)
	default:
		return fmt.Errorf("canonicaljson: unsupported value type %T", v)
	}
	return nil
}

func encodeArray(buf *bytes.Buffer, arr []any) error {
	buf.WriteByte('[')
	for i, elem := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encodeValue(buf, elem); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

func encodeObject(buf *bytes.Buffer, obj map[string]any) error {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return lessUTF16(keys[i], keys[j]) })

	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encodeString(buf, k)
		buf.WriteByte(':')
		if err := encodeValue(buf, obj[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

// lessUTF16 orders strings by their UTF-16 code unit sequence (RFC 8785
// §3.2.3), which is not the same as Go's native UTF-8 byte order once a
// key contains a character outside the Basic Multilingual Plane: UTF-16
// encodes those as a surrogate pair in the D800-DFFF range, which sorts
// before U+E000 even though the character it represents is above
// U+FFFF. See the "supplementary-plane vs BMP" test vector.
func lessUTF16(a, b string) bool {
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

func encodeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}

func encodeNumber(buf *bytes.Buffer, n json.Number) error {
	f, err := n.Float64()
	if err != nil {
		return fmt.Errorf("canonicaljson: invalid number %q: %w", n, err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("canonicaljson: %v is not representable in JSON", f)
	}
	buf.WriteString(formatNumber(f))
	return nil
}

// formatNumber implements the ECMAScript Number::toString algorithm
// (the encoding RFC 8785 mandates), the shortest decimal string that
// round-trips to f, switching to exponential notation outside
// [1e-6, 1e21) the same way JavaScript's Number.prototype.toString
// does. strconv.AppendFloat with the 'e' verb and precision -1 supplies
// the shortest round-trip digit string; this reformats it to match.
func formatNumber(f float64) string {
	if f == 0 {
		// Also collapses -0 to "0", which RFC 8785 requires.
		return "0"
	}
	neg := f < 0
	if neg {
		f = -f
	}

	digits, exp := shortestDigits(f)
	k := len(digits)
	n := exp + 1 // position of the decimal point within digits, 1-based

	var out string
	switch {
	case k <= n && n <= 21:
		out = digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		out = digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		out = "0." + strings.Repeat("0", -n) + digits
	default:
		mantissa := digits[:1]
		if k > 1 {
			mantissa += "." + digits[1:]
		}
		e := n - 1
		sign := "+"
		if e < 0 {
			sign = "-"
			e = -e
		}
		out = mantissa + "e" + sign + strconv.Itoa(e)
	}
	if neg {
		return "-" + out
	}
	return out
}

// shortestDigits returns the shortest round-trip significant-digit
// string for f (no leading/trailing zeros beyond a lone "0", f > 0) and
// exp such that digits * 10^(exp-len(digits)+1) == f.
func shortestDigits(f float64) (digits string, exp int) {
	sci := strconv.AppendFloat(nil, f, 'e', -1, 64)
	s := string(sci)
	eIdx := strings.IndexByte(s, 'e')
	mantissa := strings.Replace(s[:eIdx], ".", "", 1)
	exp, _ = strconv.Atoi(s[eIdx+1:])
	return mantissa, exp
}
