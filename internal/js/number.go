package js

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// NumberToString is Number.prototype.toString() (ECMAScript Number::toString with radix 10): the shortest digits
// that read back as n, in plain notation from 1e-7 to below 1e21 and in exponent notation outside.
func NumberToString(n float64) string {
	switch {
	case math.IsNaN(n):
		return "NaN"
	case n == 0:
		return "0"
	case math.IsInf(n, 1):
		return "Infinity"
	case math.IsInf(n, -1):
		return "-Infinity"
	case n < 0:
		return "-" + NumberToString(-n)
	}
	// strconv's shortest form is d.ddddde±xx: the same digits ECMAScript asks for.
	e := strconv.FormatFloat(n, 'e', -1, 64)
	mant, exp, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	x, _ := strconv.Atoi(exp)
	k := len(digits)
	point := x + 1 // n = 0.digits × 10^point
	switch {
	case k <= point && point <= 21:
		return digits + strings.Repeat("0", point-k)
	case 0 < point && point <= 21:
		return digits[:point] + "." + digits[point:]
	case -6 < point && point <= 0:
		return "0." + strings.Repeat("0", -point) + digits
	}
	sign := "+"
	if point-1 < 0 {
		sign = "-"
	}
	expText := strconv.Itoa(abs(point - 1))
	if k == 1 {
		return digits + "e" + sign + expText
	}
	return digits[:1] + "." + digits[1:] + "e" + sign + expText
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ToNumber is Number(s) for a string: white space around is ignored, "" is 0, and besides decimal numbers it
// takes Infinity and the 0x, 0o and 0b forms. Anything else is NaN.
func ToNumber(s string) float64 {
	t := Trim(s)
	if t == "" {
		return 0
	}
	switch t {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if len(t) > 2 && t[0] == '0' {
		base := 0
		switch t[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			v, ok := new(big.Int).SetString(t[2:], base)
			if !ok || strings.ContainsAny(t[2:], "_+-") {
				return math.NaN()
			}
			f, _ := new(big.Float).SetInt(v).Float64()
			return f
		}
	}
	if !isDecimalLiteral(t) {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f
		}
		return math.NaN()
	}
	return f
}

// isDecimalLiteral matches StrDecimalLiteral: [+-] (digits [. [digits]] | . digits) [(e|E) [+-] digits].
func isDecimalLiteral(t string) bool {
	i := 0
	if i < len(t) && (t[i] == '+' || t[i] == '-') {
		i++
	}
	digits := func() int {
		n := 0
		for i < len(t) && t[i] >= '0' && t[i] <= '9' {
			i++
			n++
		}
		return n
	}
	whole := digits()
	frac := 0
	if i < len(t) && t[i] == '.' {
		i++
		frac = digits()
	}
	if whole == 0 && frac == 0 {
		return false
	}
	if i < len(t) && (t[i] == 'e' || t[i] == 'E') {
		i++
		if i < len(t) && (t[i] == '+' || t[i] == '-') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return i == len(t)
}

// Round is Math.round: halves go up (towards +∞).
func Round(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	f := math.Floor(x)
	if x-f >= 0.5 {
		f++
	}
	if f == 0 && math.Signbit(x) || f == 0 && x < 0 {
		return math.Copysign(0, -1)
	}
	return f
}
