// Package js reproduces the few JavaScript behaviours the smstunnel CLI's output depends on, so that this Go
// version prints what the Node.js version prints: strings as JavaScript sees them (UTF-16 lengths, trim,
// toLowerCase, \s), JSON.parse and JSON.stringify, Number to string, Date.parse (V8's parser) and local time,
// and the WHATWG URL rules for an API address.
//
// Strings are Go strings holding UTF-8. A lone UTF-16 surrogate, which a JavaScript string can hold (JSON.parse
// of "\ud800"), is kept as its three-byte WTF-8 form (ED A0..BF 80..BF) and written out as U+FFFD, the way Node.js
// encodes it.
package js

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// DecodeUTF8 decodes bytes the way Buffer.toString("utf8") and TextDecoder do (WHATWG): every maximal invalid
// subsequence becomes one U+FFFD. The result is valid UTF-8. A byte-order mark is kept.
func DecodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	out.Grow(len(b) + 8)
	i := 0
	for i < len(b) {
		c := b[i]
		if c < 0x80 {
			out.WriteByte(c)
			i++
			continue
		}
		// The WHATWG UTF-8 decoder: the lead byte sets the number of continuation bytes and the range of the first one.
		need, lower, upper := 0, byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c >= 0xE0 && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lower = 0xA0
			} else if c == 0xED {
				upper = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lower = 0x90
			} else if c == 0xF4 {
				upper = 0x8F
			}
		default:
			out.WriteString("�")
			i++
			continue
		}
		j := i + 1
		ok := true
		for k := 0; k < need; k++ {
			if j >= len(b) || b[j] < lower || b[j] > upper {
				ok = false
				break
			}
			lower, upper = 0x80, 0xBF
			j++
		}
		if !ok {
			out.WriteString("�")
			i = j // the bytes read so far form the maximal subpart; the one that broke it is read again
			continue
		}
		out.Write(b[i:j])
		i = j
	}
	return out.String()
}

// decode returns the code point at s[i:] and its length in bytes, reading a WTF-8 surrogate as its code point
// (U+D800..U+DFFF). Invalid bytes come back as U+FFFD, one byte long.
func decode(s string, i int) (rune, int) {
	c := s[i]
	if c < 0x80 {
		return rune(c), 1
	}
	if c == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
		return 0xD000 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F), 3
	}
	r, n := utf8.DecodeRuneInString(s[i:])
	return r, n
}

// CodePoints are the code points of s, as Array.from(s) lists them (a lone surrogate is one).
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := decode(s, i)
		out = append(out, r)
		i += n
	}
	return out
}

// CodePointCount is Array.from(s).length.
func CodePointCount(s string) int {
	count := 0
	for i := 0; i < len(s); {
		_, n := decode(s, i)
		count++
		i += n
	}
	return count
}

// Length is s.length: UTF-16 code units (a code point above U+FFFF takes two).
func Length(s string) int {
	units := 0
	for i := 0; i < len(s); {
		r, n := decode(s, i)
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
		i += n
	}
	return units
}

// UTF16 returns the UTF-16 code units of s.
func UTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, n := decode(s, i)
		if r > 0xFFFF {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
		i += n
	}
	return out
}

// FromUTF16 builds a string from UTF-16 code units; a lone surrogate is kept (WTF-8).
func FromUTF16(units []uint16) string {
	var b strings.Builder
	for i := 0; i < len(units); i++ {
		u := rune(units[i])
		if u >= 0xD800 && u <= 0xDBFF && i+1 < len(units) && units[i+1] >= 0xDC00 && units[i+1] <= 0xDFFF {
			b.WriteRune(0x10000 + (u-0xD800)<<10 + (rune(units[i+1]) - 0xDC00))
			i++
			continue
		}
		writeCodePoint(&b, u)
	}
	return b.String()
}

// writeCodePoint appends r, a surrogate as WTF-8.
func writeCodePoint(b *strings.Builder, r rune) {
	if r >= 0xD800 && r <= 0xDFFF {
		b.WriteByte(0xED)
		b.WriteByte(byte(0xA0 | (r>>6)&0x1F))
		b.WriteByte(byte(0x80 | r&0x3F))
		return
	}
	b.WriteRune(r)
}

// ForOutput is s as Node.js writes it to a stream: lone surrogates become U+FFFD.
func ForOutput(s string) string {
	if !strings.Contains(s, "\xED") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := decode(s, i)
		if r >= 0xD800 && r <= 0xDFFF {
			b.WriteString("�")
		} else {
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// IsSpace reports whether r is JavaScript white space or a line terminator: what \s matches and trim removes.
func IsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// Trim is String.prototype.trim.
func Trim(s string) string {
	start := 0
	for start < len(s) {
		r, n := decode(s, start)
		if !IsSpace(r) {
			break
		}
		start += n
	}
	end := len(s)
	for end > start {
		// Step back one code point: continuation bytes are 10xxxxxx.
		i := end - 1
		for i > start && s[i]&0xC0 == 0x80 {
			i--
		}
		r, n := decode(s, i)
		if i+n != end || !IsSpace(r) {
			break
		}
		end = i
	}
	return s[start:end]
}

// IsBlank reports whether Trim(s) is empty.
func IsBlank(s string) bool { return Trim(s) == "" }

// ToLower is String.prototype.toLowerCase: Unicode's default lower-casing, with the two rules beyond one code
// point at a time that it applies (İ becomes i + U+0307, and Σ becomes ς at the end of a word).
func ToLower(s string) string {
	cps := CodePoints(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range cps {
		switch {
		case r == 0x130:
			b.WriteString("i̇")
		case r == 0x3A3:
			if finalSigma(cps, i) {
				b.WriteRune(0x3C2)
			} else {
				b.WriteRune(0x3C3)
			}
		case r >= 0xD800 && r <= 0xDFFF:
			writeCodePoint(&b, r)
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// finalSigma is Unicode's Final_Sigma condition for the Σ at i: a cased letter before it (case-ignorable code points
// between are skipped) and none after it.
func finalSigma(cps []rune, i int) bool {
	before := false
	for j := i - 1; j >= 0; j-- {
		if caseIgnorable(cps[j]) {
			continue
		}
		before = cased(cps[j])
		break
	}
	if !before {
		return false
	}
	for j := i + 1; j < len(cps); j++ {
		if caseIgnorable(cps[j]) {
			continue
		}
		return !cased(cps[j])
	}
	return true
}

func cased(r rune) bool {
	return unicode.In(r, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Other_Lowercase, unicode.Other_Uppercase)
}

func caseIgnorable(r rune) bool {
	switch r {
	// Word_Break MidLetter, MidNumLet and Single_Quote.
	case 0x27, 0x2E, 0x3A, 0xB7, 0x387, 0x55F, 0x5F4, 0x2018, 0x2019, 0x2024, 0x2027, 0xFE13, 0xFE52, 0xFE55, 0xFF07,
		0xFF0E, 0xFF1A:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// PadEnd is s.padEnd(width): spaces until s is width UTF-16 code units long.
func PadEnd(s string, width int) string {
	if n := Length(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
