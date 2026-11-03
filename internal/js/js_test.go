package js

import (
	"math"
	"testing"
)

func TestDecodeUTF8LikeNode(t *testing.T) {
	// Buffer.from([...]).toString("utf8") in Node.js: one U+FFFD per maximal invalid subsequence.
	for in, want := range map[string]string{
		"ok":                   "ok",
		"\xe2\x82A":            "�A",
		"\xed\xa0\x80":         "���",
		"\xf0\x80":             "��",
		"\xc0\x80":             "��",
		"\xf4\x90\x80\x80":     "����",
		"\xe2\x82":             "�",
		"a\xffb":               "a�b",
		"\xef\xbb\xbf{}":       "\xef\xbb\xbf{}",
		"\xf0\x9f\x98\x80\x80": "😀�",
	} {
		if got := DecodeUTF8([]byte(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestLoneSurrogates(t *testing.T) {
	v, err := Parse(`"a\ud800b\udc00😀"`)
	if err != nil {
		t.Fatal(err)
	}
	if got := Stringify(v, ""); got != `"a\ud800b\udc00😀"` {
		t.Errorf("stringify: %s", got)
	}
	if got := ForOutput(v.Str()); got != "a�b�😀" {
		t.Errorf("output: %q", got)
	}
	if Length(v.Str()) != 6 || CodePointCount(v.Str()) != 5 {
		t.Errorf("length %d, code points %d", Length(v.Str()), CodePointCount(v.Str()))
	}
	if got := FromUTF16(UTF16(v.Str())); got != v.Str() {
		t.Errorf("UTF-16 round trip: %q", got)
	}
	if got := Trim(" \xe3\x80\x80" + v.Str() + "\xef\xbb\xbf "); got != v.Str() {
		t.Errorf("trim: %q", got)
	}
}

func TestRound(t *testing.T) {
	for in, want := range map[float64]float64{2.5: 3, -2.5: -2, 0.49999999999999994: 0, -0.4: math.Copysign(0, -1), 1.5: 2, -1.5: -1} {
		if got := Round(in); got != want || math.Signbit(got) != math.Signbit(want) {
			t.Errorf("Math.round(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestObjectOrder(t *testing.T) {
	o := NewObject().Set("b", IntValue(1)).Set("2", IntValue(2)).Set("a", IntValue(3)).Set("b", IntValue(4)).Set("u", UndefinedValue)
	if got := Stringify(o, ""); got != `{"2":2,"b":4,"a":3}` {
		t.Errorf("%s", got)
	}
	if got := Stringify(NewObject().Set("u", UndefinedValue), "  "); got != "{}" {
		t.Errorf("only undefined: %s", got)
	}
}
