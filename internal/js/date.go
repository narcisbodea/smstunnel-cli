package js

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Dates are time values as in JavaScript: milliseconds since 1970-01-01T00:00:00Z in a float64, NaN when the
// date is invalid.

const (
	msPerSecond = 1000.0
	msPerMinute = 60000.0
	msPerHour   = 3600000.0
	msPerDay    = 86400000.0

	maxTimeMs          = 8.64e15
	maxTimeBeforeUTCMs = maxTimeMs + 864000000
)

// Location is the time zone of "local" times. The CLI uses time.Local; tests set others.
var Location = time.Local

// NaN is an invalid time value.
var NaN = math.NaN()

// IsValid reports whether t is a valid time value.
func IsValid(t float64) bool { return !math.IsNaN(t) }

// TimeClip is ECMAScript TimeClip: NaN outside ±8.64e15 ms, else the integer part.
func TimeClip(t float64) float64 {
	if math.IsNaN(t) || math.Abs(t) > maxTimeMs {
		return NaN
	}
	return math.Trunc(t) + 0 // +0 turns -0 into +0
}

// ParseDate is Date.parse(s): V8's date parser (ES date-time strings, then its legacy formats).
func ParseDate(s string) float64 {
	var out [8]float64
	if !parseDateFields(UTF16(s), &out) {
		return NaN
	}
	day := makeDay(out[fYear], out[fMonth], out[fDay])
	tm := makeTime(out[fHour], out[fMinute], out[fSecond], out[fMillisecond])
	date := makeDate(day, tm)
	if math.IsNaN(out[fUTCOffset]) {
		if math.IsNaN(date) || date < -maxTimeBeforeUTCMs || date > maxTimeBeforeUTCMs {
			return NaN
		}
		date = localToUTC(date)
	} else {
		date -= out[fUTCOffset] * 1000
		if math.IsNaN(date) || date < -maxTimeMs || date > maxTimeMs {
			return NaN
		}
	}
	return TimeClip(date)
}

// Date from a JSON value, as new Date(value) does: a string is parsed, a number is a time value, null is 0,
// true and false are 1 and 0; anything else is first turned into a string.
func DateOf(v Value) float64 {
	switch v.Kind() {
	case String:
		return ParseDate(v.Str())
	case Number:
		return TimeClip(v.Num())
	case Null:
		return 0
	case Bool:
		if v.BoolVal() {
			return 1
		}
		return 0
	case Undefined:
		return NaN
	}
	return ParseDate(v.String())
}

// ---- ES date arithmetic (V8's versions) ----

func makeDay(year, month, date float64) float64 {
	const minYear, maxYear = -1000000, 1000000
	const minMonth, maxMonth = -10000000, 10000000
	if !(minYear <= year && year <= maxYear && minMonth <= month && month <= maxMonth) || math.IsNaN(date) || math.IsInf(date, 0) {
		return NaN
	}
	y := int64(year)
	m := int64(month)
	y += m / 12
	m %= 12
	if m < 0 {
		m += 12
		y--
	}
	const yearDelta = 399999
	const baseDay = 365*(1970+yearDelta) + (1970+yearDelta)/4 - (1970+yearDelta)/100 + (1970+yearDelta)/400
	dayFromYear := 365*(y+yearDelta) + (y+yearDelta)/4 - (y+yearDelta)/100 + (y+yearDelta)/400 - baseDay
	if y%4 != 0 || (y%100 == 0 && y%400 != 0) {
		dayFromYear += [...]int64{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}[m]
	} else {
		dayFromYear += [...]int64{0, 31, 60, 91, 121, 152, 182, 213, 244, 274, 305, 335}[m]
	}
	return float64(dayFromYear-1) + math.Trunc(date)
}

func makeTime(h, m, s, ms float64) float64 {
	for _, v := range []float64{h, m, s, ms} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return NaN
		}
	}
	return math.Trunc(h)*msPerHour + math.Trunc(m)*msPerMinute + math.Trunc(s)*msPerSecond + math.Trunc(ms)
}

func makeDate(day, t float64) float64 {
	if math.IsNaN(day) || math.IsInf(day, 0) || math.IsNaN(t) || math.IsInf(t, 0) {
		return NaN
	}
	if t == 0 && day != 0 {
		return day * msPerDay
	}
	return day*msPerDay + t
}

// offsetAt is the local time zone's offset from UTC at the instant t, in ms.
func offsetAt(t int64) int64 {
	_, seconds := time.UnixMilli(t).In(Location).Zone()
	return int64(seconds) * 1000
}

// LocalOffset is the offset of local time from UTC at the time value t (ms), for t + LocalOffset(t) = local time.
func LocalOffset(t float64) float64 { return float64(offsetAt(int64(t))) }

// localToUTC turns a local time value into UTC the way V8 does with ICU (UCAL_TZ_LOCAL_FORMER): a wall time that
// happens twice is the first one, and one that never happens (a skipped hour) is read with the offset in force
// before the change.
func localToUTC(local float64) float64 {
	l := int64(local)
	const hour = int64(3600000)
	type change struct{ before, after int64 }
	first := offsetAt(l - 16*hour)
	prev := first
	var changes []change
	for k := int64(-15); k <= 16; k++ {
		o := offsetAt(l + k*hour)
		if o != prev {
			changes = append(changes, change{prev, o})
			prev = o
		}
	}
	candidates := []int64{first}
	for _, c := range changes {
		candidates = append(candidates, c.after)
	}
	best, found := int64(0), false
	for _, o := range candidates {
		u := l - o
		if offsetAt(u) == o && (!found || u < best) {
			best, found = u, true
		}
	}
	if found {
		return float64(best)
	}
	for _, c := range changes {
		if c.after > c.before {
			return float64(l - c.before)
		}
	}
	return float64(l - first)
}

// Parts are the calendar fields of a time value.
type Parts struct {
	Year                                     int64
	Month, Day, Hour, Minute, Second, Millis int
}

// UTCParts are the fields of t in UTC (getUTCFullYear and the others).
func UTCParts(t float64) Parts {
	days := int64(math.Floor(t / msPerDay))
	within := int64(t) - days*int64(msPerDay)
	y, m, d := civilFromDays(days)
	return Parts{
		Year: y, Month: m, Day: d,
		Hour:   int(within / 3600000),
		Minute: int(within / 60000 % 60),
		Second: int(within / 1000 % 60),
		Millis: int(within % 1000),
	}
}

// LocalParts are the fields of t in local time (getFullYear and the others).
func LocalParts(t float64) Parts { return UTCParts(t + LocalOffset(t)) }

// civilFromDays turns days since 1970-01-01 into a proleptic Gregorian date (Howard Hinnant's algorithm).
func civilFromDays(z int64) (int64, int, int) {
	z += 719468
	era := z / 146097
	if z < 0 && z%146097 != 0 {
		era--
	}
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := int(doy - (153*mp+2)/5 + 1)
	m := int(mp + 3)
	if mp >= 10 {
		m = int(mp - 9)
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

// ISOString is Date.prototype.toISOString for a valid t: 2026-11-03T08:00:00.000Z, with ±YYYYYY outside the
// years 0 to 9999.
func ISOString(t float64) string {
	p := UTCParts(t)
	var b strings.Builder
	switch {
	case p.Year >= 0 && p.Year <= 9999:
		b.WriteString(pad(p.Year, 4))
	case p.Year < 0:
		b.WriteString("-" + pad(-p.Year, 6))
	default:
		b.WriteString("+" + pad(p.Year, 6))
	}
	b.WriteString("-" + pad(int64(p.Month), 2) + "-" + pad(int64(p.Day), 2))
	b.WriteString("T" + pad(int64(p.Hour), 2) + ":" + pad(int64(p.Minute), 2) + ":" + pad(int64(p.Second), 2))
	b.WriteString("." + pad(int64(p.Millis), 3) + "Z")
	return b.String()
}

func pad(n int64, width int) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// ---- V8's DateParser (src/date/dateparser.h, dateparser-inl.h, dateparser.cc), ported as it is ----

const (
	fYear = iota
	fMonth
	fDay
	fHour
	fMinute
	fSecond
	fMillisecond
	fUTCOffset
)

const kNone = math.MaxInt32
const maxSignificantDigits = 9

func between(x, lo, hi int) bool { return uint32(x-lo) <= uint32(hi-lo) }

type dateReader struct {
	buf   []uint16
	index int
	ch    uint32
}

func newDateReader(buf []uint16) *dateReader {
	r := &dateReader{buf: buf}
	r.next()
	return r
}

func (r *dateReader) next() {
	if r.index < len(r.buf) {
		r.ch = uint32(r.buf[r.index])
	} else {
		r.ch = 0
	}
	r.index++
}

func (r *dateReader) position() int { return r.index }

func (r *dateReader) readUnsignedNumeral() int {
	n, i := 0, 0
	for r.ch == '0' {
		r.next()
	}
	for r.isDigit() {
		if i < maxSignificantDigits {
			n = n*10 + int(r.ch-'0')
		}
		i++
		r.next()
	}
	return n
}

func (r *dateReader) readWord(prefix *[3]uint32) int {
	length := 0
	for ; r.isAlphaOrAbove() && !isWhiteSpaceChar(r.ch); r.next() {
		if length < 3 {
			prefix[length] = r.ch | 0x20
		}
		length++
	}
	for i := length; i < 3; i++ {
		prefix[i] = 0
	}
	return length
}

func (r *dateReader) skip(c uint32) bool {
	if r.ch == c {
		r.next()
		return true
	}
	return false
}

func (r *dateReader) skipWhiteSpace() bool {
	if isWhiteSpaceOrLineTerminator(r.ch) {
		r.next()
		return true
	}
	return false
}

func (r *dateReader) skipParentheses() bool {
	if r.ch != '(' {
		return false
	}
	balance := 0
	for {
		if r.ch == ')' {
			balance--
		} else if r.ch == '(' {
			balance++
		}
		r.next()
		if !(balance > 0 && r.ch != 0) {
			break
		}
	}
	return true
}

func (r *dateReader) isEnd() bool          { return r.ch == 0 }
func (r *dateReader) isDigit() bool        { return r.ch >= '0' && r.ch <= '9' }
func (r *dateReader) isAlphaOrAbove() bool { return r.ch >= 'A' }

func isWhiteSpaceChar(c uint32) bool {
	switch c {
	case '\t', '\v', '\f', ' ', 0xA0, 0x1680, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return c >= 0x2000 && c <= 0x200A
}

func isWhiteSpaceOrLineTerminator(c uint32) bool {
	return isWhiteSpaceChar(c) || c == '\n' || c == '\r' || c == 0x2028 || c == 0x2029
}

// Keyword types.
const (
	kwInvalid = iota
	kwMonthName
	kwTimeZoneName
	kwTimeSeparator
	kwAmPm
)

// Token tags; keywords use their type (0 and up).
const (
	tagInvalid    = -6
	tagUnknown    = -5
	tagWhiteSpace = -4
	tagNumber     = -3
	tagSymbol     = -2
	tagEnd        = -1
)

type dateToken struct{ tag, length, value int }

func (t dateToken) isInvalid() bool                { return t.tag == tagInvalid }
func (t dateToken) isNumber() bool                 { return t.tag == tagNumber }
func (t dateToken) isSymbol() bool                 { return t.tag == tagSymbol }
func (t dateToken) isWhiteSpace() bool             { return t.tag == tagWhiteSpace }
func (t dateToken) isEndOfInput() bool             { return t.tag == tagEnd }
func (t dateToken) isKeyword() bool                { return t.tag >= 0 }
func (t dateToken) isSymbolChar(c int) bool        { return t.isSymbol() && t.value == c }
func (t dateToken) isKeywordType(k int) bool       { return t.tag == k }
func (t dateToken) isFixedLengthNumber(n int) bool { return t.isNumber() && t.length == n }
func (t dateToken) isAsciiSign() bool {
	return t.tag == tagSymbol && (t.value == '-' || t.value == '+')
}
func (t dateToken) asciiSign() int   { return 44 - t.value }
func (t dateToken) isKeywordZ() bool { return t.tag == kwTimeZoneName && t.length == 1 && t.value == 0 }

var keywordTable = []struct {
	prefix [3]uint32
	kind   int
	value  int
}{
	{[3]uint32{'j', 'a', 'n'}, kwMonthName, 1},
	{[3]uint32{'f', 'e', 'b'}, kwMonthName, 2},
	{[3]uint32{'m', 'a', 'r'}, kwMonthName, 3},
	{[3]uint32{'a', 'p', 'r'}, kwMonthName, 4},
	{[3]uint32{'m', 'a', 'y'}, kwMonthName, 5},
	{[3]uint32{'j', 'u', 'n'}, kwMonthName, 6},
	{[3]uint32{'j', 'u', 'l'}, kwMonthName, 7},
	{[3]uint32{'a', 'u', 'g'}, kwMonthName, 8},
	{[3]uint32{'s', 'e', 'p'}, kwMonthName, 9},
	{[3]uint32{'o', 'c', 't'}, kwMonthName, 10},
	{[3]uint32{'n', 'o', 'v'}, kwMonthName, 11},
	{[3]uint32{'d', 'e', 'c'}, kwMonthName, 12},
	{[3]uint32{'a', 'm', 0}, kwAmPm, 0},
	{[3]uint32{'p', 'm', 0}, kwAmPm, 12},
	{[3]uint32{'u', 't', 0}, kwTimeZoneName, 0},
	{[3]uint32{'u', 't', 'c'}, kwTimeZoneName, 0},
	{[3]uint32{'z', 0, 0}, kwTimeZoneName, 0},
	{[3]uint32{'g', 'm', 't'}, kwTimeZoneName, 0},
	{[3]uint32{'c', 'd', 't'}, kwTimeZoneName, -5},
	{[3]uint32{'c', 's', 't'}, kwTimeZoneName, -6},
	{[3]uint32{'e', 'd', 't'}, kwTimeZoneName, -4},
	{[3]uint32{'e', 's', 't'}, kwTimeZoneName, -5},
	{[3]uint32{'m', 'd', 't'}, kwTimeZoneName, -6},
	{[3]uint32{'m', 's', 't'}, kwTimeZoneName, -7},
	{[3]uint32{'p', 'd', 't'}, kwTimeZoneName, -7},
	{[3]uint32{'p', 's', 't'}, kwTimeZoneName, -8},
	{[3]uint32{'t', 0, 0}, kwTimeSeparator, 0},
}

func lookupKeyword(prefix [3]uint32, length int) (kind, value int) {
	for _, k := range keywordTable {
		if k.prefix == prefix && (length <= 3 || k.kind == kwMonthName) {
			return k.kind, k.value
		}
	}
	return kwInvalid, 0
}

type dateScanner struct {
	in   *dateReader
	peek dateToken
}

func newDateScanner(in *dateReader) *dateScanner {
	s := &dateScanner{in: in}
	s.peek = s.scan()
	return s
}

func (s *dateScanner) Next() dateToken {
	t := s.peek
	s.peek = s.scan()
	return t
}

func (s *dateScanner) Peek() dateToken { return s.peek }

func (s *dateScanner) SkipSymbol(c int) bool {
	if s.peek.isSymbolChar(c) {
		s.peek = s.scan()
		return true
	}
	return false
}

func (s *dateScanner) scan() dateToken {
	in := s.in
	pre := in.position()
	if in.isEnd() {
		return dateToken{tagEnd, 0, -1}
	}
	if in.isDigit() {
		n := in.readUnsignedNumeral()
		return dateToken{tagNumber, in.position() - pre, n}
	}
	for _, c := range []uint32{':', '-', '+', '.', ')'} {
		if in.skip(c) {
			return dateToken{tagSymbol, 1, int(c)}
		}
	}
	if in.isAlphaOrAbove() && !isWhiteSpaceChar(in.ch) {
		var prefix [3]uint32
		length := in.readWord(&prefix)
		kind, value := lookupKeyword(prefix, length)
		return dateToken{kind, length, value}
	}
	if in.skipWhiteSpace() {
		return dateToken{tagWhiteSpace, in.position() - pre, 0}
	}
	if in.skipParentheses() {
		return dateToken{tagUnknown, 1, -1}
	}
	in.next()
	return dateToken{tagUnknown, 1, -1}
}

type tzComposer struct{ sign, hour, minute int }

func newTZComposer() tzComposer { return tzComposer{kNone, kNone, kNone} }

func (t *tzComposer) set(offsetInHours int) {
	if offsetInHours < 0 {
		t.sign = -1
	} else {
		t.sign = 1
	}
	t.hour = offsetInHours * t.sign
	t.minute = 0
}

func (t *tzComposer) setSign(sign int) {
	if sign < 0 {
		t.sign = -1
	} else {
		t.sign = 1
	}
}

func (t *tzComposer) isExpecting(n int) bool {
	return t.hour != kNone && t.minute == kNone && isMinute(n)
}
func (t *tzComposer) isUTC() bool   { return t.hour == 0 && t.minute == 0 }
func (t *tzComposer) isEmpty() bool { return t.hour == kNone }

func (t *tzComposer) write(out *[8]float64) bool {
	if t.sign != kNone {
		if t.hour == kNone {
			t.hour = 0
		}
		if t.minute == kNone {
			t.minute = 0
		}
		total := uint32(t.hour)*3600 + uint32(t.minute)*60
		if total > math.MaxInt32 {
			return false
		}
		seconds := int(total)
		if t.sign < 0 {
			seconds = -seconds
		}
		out[fUTCOffset] = float64(seconds)
	} else {
		out[fUTCOffset] = NaN
	}
	return true
}

func isMinute(x int) bool      { return between(x, 0, 59) }
func isHour(x int) bool        { return between(x, 0, 23) }
func isSecond(x int) bool      { return between(x, 0, 59) }
func isHour12(x int) bool      { return between(x, 0, 12) }
func isMillisecond(x int) bool { return between(x, 0, 999) }
func isMonth(x int) bool       { return between(x, 1, 12) }
func isDay(x int) bool         { return between(x, 1, 31) }

type timeComposer struct {
	comp       [4]int
	index      int
	hourOffset int
}

func newTimeComposer() timeComposer { return timeComposer{hourOffset: kNone} }

func (t *timeComposer) isEmpty() bool { return t.index == 0 }
func (t *timeComposer) isExpecting(n int) bool {
	return (t.index == 1 && isMinute(n)) || (t.index == 2 && isSecond(n)) || (t.index == 3 && isMillisecond(n))
}
func (t *timeComposer) add(n int) bool {
	if t.index < 4 {
		t.comp[t.index] = n
		t.index++
		return true
	}
	return false
}
func (t *timeComposer) addFinal(n int) bool {
	if !t.add(n) {
		return false
	}
	for t.index < 4 {
		t.comp[t.index] = 0
		t.index++
	}
	return true
}

func (t *timeComposer) write(out *[8]float64) bool {
	for t.index < 4 {
		t.comp[t.index] = 0
		t.index++
	}
	hour, minute, second, ms := t.comp[0], t.comp[1], t.comp[2], t.comp[3]
	if t.hourOffset != kNone {
		if !isHour12(hour) {
			return false
		}
		hour %= 12
		hour += t.hourOffset
	}
	if !isHour(hour) || !isMinute(minute) || !isSecond(second) || !isMillisecond(ms) {
		if hour != 24 || minute != 0 || second != 0 || ms != 0 {
			return false
		}
	}
	out[fHour], out[fMinute], out[fSecond], out[fMillisecond] = float64(hour), float64(minute), float64(second), float64(ms)
	return true
}

type dayComposer struct {
	comp       [3]int
	index      int
	namedMonth int
	isoDate    bool
}

func newDayComposer() dayComposer { return dayComposer{namedMonth: kNone} }

func (d *dayComposer) isEmpty() bool { return d.index == 0 }
func (d *dayComposer) add(n int) bool {
	if d.index < 3 {
		d.comp[d.index] = n
		d.index++
		return true
	}
	return false
}

func (d *dayComposer) write(out *[8]float64) bool {
	if d.index < 1 {
		return false
	}
	for d.index < 3 {
		d.comp[d.index] = 1
		d.index++
	}
	year, month, day := 0, kNone, kNone
	if d.namedMonth == kNone {
		if d.isoDate || (d.index == 3 && !isDay(d.comp[0])) {
			year, month, day = d.comp[0], d.comp[1], d.comp[2]
		} else {
			month, day = d.comp[0], d.comp[1]
			if d.index == 3 {
				year = d.comp[2]
			}
		}
	} else {
		month = d.namedMonth
		if d.index == 1 {
			day = d.comp[0]
		} else if !isDay(d.comp[0]) {
			year, day = d.comp[0], d.comp[1]
		} else {
			day, year = d.comp[0], d.comp[1]
		}
	}
	if !d.isoDate {
		if between(year, 0, 49) {
			year += 2000
		} else if between(year, 50, 99) {
			year += 1900
		}
	}
	if !isMonth(month) || !isDay(day) {
		return false
	}
	out[fYear], out[fMonth], out[fDay] = float64(year), float64(month-1), float64(day)
	return true
}

func readMilliseconds(t dateToken) int {
	number, length := t.value, t.length
	if length < 3 {
		if length == 1 {
			number *= 100
		} else if length == 2 {
			number *= 10
		}
	} else if length > 3 {
		if length > maxSignificantDigits {
			length = maxSignificantDigits
		}
		factor := 1
		for {
			factor *= 10
			length--
			if length <= 3 {
				break
			}
		}
		number /= factor
	}
	return number
}

func parseDateFields(str []uint16, out *[8]float64) bool {
	in := newDateReader(str)
	scanner := newDateScanner(in)
	tz := newTZComposer()
	tm := newTimeComposer()
	day := newDayComposer()

	next := parseES5DateTime(scanner, &day, &tm, &tz)
	if next.isInvalid() {
		return false
	}
	hasReadNumber := !day.isEmpty()
	for token := next; !token.isEndOfInput(); token = scanner.Next() {
		switch {
		case token.isNumber():
			hasReadNumber = true
			n := token.value
			if scanner.SkipSymbol(':') {
				if scanner.SkipSymbol(':') {
					if !tm.isEmpty() {
						return false
					}
					tm.add(n)
					tm.add(0)
				} else {
					if !tm.add(n) {
						return false
					}
					if scanner.Peek().isSymbolChar('.') {
						scanner.Next()
					}
				}
			} else if scanner.SkipSymbol('.') && tm.isExpecting(n) {
				tm.add(n)
				if !scanner.Peek().isNumber() {
					return false
				}
				ms := readMilliseconds(scanner.Next())
				if ms < 0 {
					return false
				}
				tm.addFinal(ms)
			} else if tz.isExpecting(n) {
				tz.minute = n
			} else if tm.isExpecting(n) {
				tm.addFinal(n)
				peek := scanner.Peek()
				if !peek.isEndOfInput() && !peek.isWhiteSpace() && !peek.isKeywordZ() && !peek.isAsciiSign() {
					return false
				}
			} else {
				if !day.add(n) {
					return false
				}
				scanner.SkipSymbol('-')
			}
		case token.isKeyword():
			if token.isKeywordType(kwAmPm) && !tm.isEmpty() {
				tm.hourOffset = token.value
			} else if token.isKeywordType(kwMonthName) {
				day.namedMonth = token.value
				scanner.SkipSymbol('-')
			} else if token.isKeywordType(kwTimeZoneName) && hasReadNumber {
				tz.set(token.value)
			} else {
				// Garbage words are illegal if a number has been read.
				if hasReadNumber {
					return false
				}
				// The first number has to be separated from garbage words by whitespace or other separators.
				if scanner.Peek().isNumber() {
					return false
				}
			}
		case token.isAsciiSign() && (tz.isUTC() || !tm.isEmpty()):
			tz.setSign(token.asciiSign())
			n, length := 0, 0
			if scanner.Peek().isNumber() {
				t := scanner.Next()
				length = t.length
				n = t.value
			}
			hasReadNumber = true
			if scanner.Peek().isSymbolChar(':') {
				tz.hour = n
				tz.minute = kNone
			} else if length == 2 || length == 1 {
				tz.hour = n
				tz.minute = 0
			} else if length == 4 || length == 3 {
				tz.hour = n / 100
				tz.minute = n % 100
			} else {
				return false
			}
		case (token.isAsciiSign() || token.isSymbolChar(')')) && hasReadNumber:
			return false
		}
	}
	return day.write(out) && tm.write(out) && tz.write(out)
}

func parseES5DateTime(scanner *dateScanner, day *dayComposer, tm *timeComposer, tz *tzComposer) dateToken {
	// Mandatory date: [('-'|'+')yy]yyyy[-MM[-DD]]
	if scanner.Peek().isAsciiSign() {
		signToken := scanner.Next()
		if !scanner.Peek().isFixedLengthNumber(6) {
			return signToken
		}
		sign := signToken.asciiSign()
		year := scanner.Next().value
		if sign < 0 && year == 0 {
			return signToken
		}
		day.add(sign * year)
	} else if scanner.Peek().isFixedLengthNumber(4) {
		day.add(scanner.Next().value)
	} else {
		return scanner.Next()
	}
	if scanner.SkipSymbol('-') {
		if !scanner.Peek().isFixedLengthNumber(2) || !isMonth(scanner.Peek().value) {
			return scanner.Next()
		}
		day.add(scanner.Next().value)
		if scanner.SkipSymbol('-') {
			if !scanner.Peek().isFixedLengthNumber(2) || !isDay(scanner.Peek().value) {
				return scanner.Next()
			}
			day.add(scanner.Next().value)
		}
	}
	// Optional time: 'T'HH':'mm[':'ss['.'sss]]Z
	if !scanner.Peek().isKeywordType(kwTimeSeparator) {
		if !scanner.Peek().isEndOfInput() {
			return scanner.Next()
		}
	} else {
		scanner.Next()
		if !scanner.Peek().isFixedLengthNumber(2) || !between(scanner.Peek().value, 0, 24) {
			return dateToken{tagInvalid, 0, -1}
		}
		// 24:00[:00[.000]] is allowed, no other time starting with 24.
		hourIs24 := scanner.Peek().value == 24
		tm.add(scanner.Next().value)
		if !scanner.SkipSymbol(':') {
			return dateToken{tagInvalid, 0, -1}
		}
		if !scanner.Peek().isFixedLengthNumber(2) || !isMinute(scanner.Peek().value) || (hourIs24 && scanner.Peek().value > 0) {
			return dateToken{tagInvalid, 0, -1}
		}
		tm.add(scanner.Next().value)
		if scanner.SkipSymbol(':') {
			if !scanner.Peek().isFixedLengthNumber(2) || !isSecond(scanner.Peek().value) || (hourIs24 && scanner.Peek().value > 0) {
				return dateToken{tagInvalid, 0, -1}
			}
			tm.add(scanner.Next().value)
			if scanner.SkipSymbol('.') {
				if !scanner.Peek().isNumber() || (hourIs24 && scanner.Peek().value > 0) {
					return dateToken{tagInvalid, 0, -1}
				}
				// More or fewer than the three digits are allowed.
				tm.add(readMilliseconds(scanner.Next()))
			}
		}
		// Optional time zone: 'Z' | ('+'|'-')hh':'mm
		if scanner.Peek().isKeywordZ() {
			scanner.Next()
			tz.set(0)
		} else if scanner.Peek().isSymbolChar('+') || scanner.Peek().isSymbolChar('-') {
			if scanner.Next().value == '+' {
				tz.setSign(1)
			} else {
				tz.setSign(-1)
			}
			if scanner.Peek().isFixedLengthNumber(4) {
				// hhmm extension syntax.
				hourmin := scanner.Next().value
				hour, min := hourmin/100, hourmin%100
				if !isHour(hour) || !isMinute(min) {
					return dateToken{tagInvalid, 0, -1}
				}
				tz.hour, tz.minute = hour, min
			} else {
				if !scanner.Peek().isFixedLengthNumber(2) || !isHour(scanner.Peek().value) {
					return dateToken{tagInvalid, 0, -1}
				}
				tz.hour = scanner.Next().value
				if !scanner.SkipSymbol(':') {
					return dateToken{tagInvalid, 0, -1}
				}
				if !scanner.Peek().isFixedLengthNumber(2) || !isMinute(scanner.Peek().value) {
					return dateToken{tagInvalid, 0, -1}
				}
				tz.minute = scanner.Next().value
			}
		}
		if !scanner.Peek().isEndOfInput() {
			return dateToken{tagInvalid, 0, -1}
		}
	}
	// A date-only form without an offset is UTC; a date-time form without one is local time.
	if tz.isEmpty() && tm.isEmpty() {
		tz.set(0)
	}
	day.isoDate = true
	return dateToken{tagEnd, 0, -1}
}
