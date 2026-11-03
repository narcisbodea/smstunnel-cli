package js

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Kind is the type of a Value, as typeof and Array.isArray see it.
type Kind uint8

// The kinds of value. Undefined is a missing member: JSON.parse never makes it.
const (
	Undefined Kind = iota
	Null
	Bool
	Number
	String
	Array
	Object
)

// Value is a JavaScript value made by JSON.parse (or built to be written with JSON.stringify).
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	arr  []Value
	obj  *object
}

type object struct {
	keys  []string // in creation order
	index map[string]int
	vals  []Value
}

// Constructors.
var (
	UndefinedValue = Value{}
	NullValue      = Value{kind: Null}
)

func BoolValue(b bool) Value         { return Value{kind: Bool, b: b} }
func NumberValue(n float64) Value    { return Value{kind: Number, n: n} }
func IntValue(n int) Value           { return Value{kind: Number, n: float64(n)} }
func StringValue(s string) Value     { return Value{kind: String, s: s} }
func ArrayValue(items []Value) Value { return Value{kind: Array, arr: items} }

// NewObject returns an empty object to fill with Set.
func NewObject() Value { return Value{kind: Object, obj: &object{index: map[string]int{}}} }

// Set adds or replaces a member (keeping its place when it exists, as JavaScript does) and returns v.
// Setting Undefined leaves a member that JSON.stringify skips.
func (v Value) Set(key string, value Value) Value {
	if v.kind != Object {
		panic("js: Set on a value that is not an object")
	}
	if i, ok := v.obj.index[key]; ok {
		v.obj.vals[i] = value
		return v
	}
	v.obj.index[key] = len(v.obj.keys)
	v.obj.keys = append(v.obj.keys, key)
	v.obj.vals = append(v.obj.vals, value)
	return v
}

// Kind is the value's kind.
func (v Value) Kind() Kind { return v.kind }

// IsUndefined, IsNull and IsNullish (null or undefined, what ?? replaces).
func (v Value) IsUndefined() bool { return v.kind == Undefined }
func (v Value) IsNull() bool      { return v.kind == Null }
func (v Value) IsNullish() bool   { return v.kind == Undefined || v.kind == Null }

// Str is the string of a String value ("" otherwise).
func (v Value) Str() string { return v.s }

// Num is the number of a Number value (0 otherwise).
func (v Value) Num() float64 { return v.n }

// BoolVal is the boolean of a Bool value (false otherwise).
func (v Value) BoolVal() bool { return v.b }

// IsString reports whether v is a string (typeof v === "string").
func (v Value) IsString() bool { return v.kind == String }

// Items are the elements of an Array (nil otherwise).
func (v Value) Items() []Value {
	if v.kind != Array {
		return nil
	}
	return v.arr
}

// Get is v[key] for the members JSON can make: Undefined when v is not an object or has no such member.
func (v Value) Get(key string) Value {
	if v.kind != Object {
		return UndefinedValue
	}
	if i, ok := v.obj.index[key]; ok {
		return v.obj.vals[i]
	}
	return UndefinedValue
}

// Keys are the object's member names in JavaScript's order: array indexes ("0", "1", …) first, ascending, then
// the others in the order they were created.
func (v Value) Keys() []string {
	if v.kind != Object {
		return nil
	}
	var indexes []string
	var others []string
	for _, k := range v.obj.keys {
		if isArrayIndex(k) {
			indexes = append(indexes, k)
		} else {
			others = append(others, k)
		}
	}
	if len(indexes) == 0 {
		return others
	}
	sort.Slice(indexes, func(i, j int) bool {
		a, _ := strconv.ParseUint(indexes[i], 10, 64)
		b, _ := strconv.ParseUint(indexes[j], 10, 64)
		return a < b
	})
	return append(indexes, others...)
}

// isArrayIndex: the canonical decimal form of an integer from 0 to 2^32 - 2.
func isArrayIndex(k string) bool {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n <= math.MaxUint32-1
}

// Truthy is JavaScript's truthiness: false, 0, NaN, "", null and undefined are false.
func (v Value) Truthy() bool {
	switch v.kind {
	case Bool:
		return v.b
	case Number:
		return v.n != 0 && !math.IsNaN(v.n)
	case String:
		return v.s != ""
	case Array, Object:
		return true
	}
	return false
}

// String is String(v), the text a template literal puts in.
func (v Value) String() string {
	switch v.kind {
	case Undefined:
		return "undefined"
	case Null:
		return "null"
	case Bool:
		if v.b {
			return "true"
		}
		return "false"
	case Number:
		return NumberToString(v.n)
	case String:
		return v.s
	case Array:
		parts := make([]string, len(v.arr))
		for i, item := range v.arr {
			if !item.IsNullish() {
				parts[i] = item.String()
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// StrictEquals is v === s for a string s.
func (v Value) StrictEquals(s string) bool { return v.kind == String && v.s == s }

// ErrSyntax is the error of Parse for text that is not JSON.
var ErrSyntax = errors.New("not JSON")

// Parse is JSON.parse.
func Parse(text string) (Value, error) {
	p := parser{s: text}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return UndefinedValue, err
	}
	p.space()
	if p.i != len(p.s) {
		return UndefinedValue, ErrSyntax
	}
	return v, nil
}

type parser struct {
	s string
	i int
}

const maxDepth = 10000

func (p *parser) space() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, error) {
	if depth > maxDepth || p.i >= len(p.s) {
		return UndefinedValue, ErrSyntax
	}
	switch c := p.s[p.i]; {
	case c == '{':
		p.i++
		obj := NewObject()
		p.space()
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return obj, nil
		}
		for {
			p.space()
			if p.i >= len(p.s) || p.s[p.i] != '"' {
				return UndefinedValue, ErrSyntax
			}
			key, err := p.str()
			if err != nil {
				return UndefinedValue, err
			}
			p.space()
			if p.i >= len(p.s) || p.s[p.i] != ':' {
				return UndefinedValue, ErrSyntax
			}
			p.i++
			p.space()
			val, err := p.value(depth + 1)
			if err != nil {
				return UndefinedValue, err
			}
			obj.Set(key, val)
			p.space()
			if p.i >= len(p.s) {
				return UndefinedValue, ErrSyntax
			}
			if p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.s[p.i] == '}' {
				p.i++
				return obj, nil
			}
			return UndefinedValue, ErrSyntax
		}
	case c == '[':
		p.i++
		items := []Value{}
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return ArrayValue(items), nil
		}
		for {
			p.space()
			val, err := p.value(depth + 1)
			if err != nil {
				return UndefinedValue, err
			}
			items = append(items, val)
			p.space()
			if p.i >= len(p.s) {
				return UndefinedValue, ErrSyntax
			}
			if p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.s[p.i] == ']' {
				p.i++
				return ArrayValue(items), nil
			}
			return UndefinedValue, ErrSyntax
		}
	case c == '"':
		s, err := p.str()
		if err != nil {
			return UndefinedValue, err
		}
		return StringValue(s), nil
	case c == 't':
		return p.literal("true", BoolValue(true))
	case c == 'f':
		return p.literal("false", BoolValue(false))
	case c == 'n':
		return p.literal("null", NullValue)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return UndefinedValue, ErrSyntax
}

func (p *parser) literal(word string, v Value) (Value, error) {
	if strings.HasPrefix(p.s[p.i:], word) {
		p.i += len(word)
		return v, nil
	}
	return UndefinedValue, ErrSyntax
}

func (p *parser) number() (Value, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.i >= len(p.s) {
		return UndefinedValue, ErrSyntax
	}
	if p.s[p.i] == '0' {
		p.i++
	} else if digits() == 0 {
		return UndefinedValue, ErrSyntax
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if digits() == 0 {
			return UndefinedValue, ErrSyntax
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			return UndefinedValue, ErrSyntax
		}
	}
	n, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return UndefinedValue, ErrSyntax
	}
	return NumberValue(n), nil
}

func (p *parser) str() (string, error) {
	p.i++ // the opening quote
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", ErrSyntax
		}
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return b.String(), nil
		case c < 0x20:
			return "", ErrSyntax
		case c == '\\':
			p.i++
			if p.i >= len(p.s) {
				return "", ErrSyntax
			}
			e := p.s[p.i]
			p.i++
			switch e {
			case '"', '\\', '/':
				b.WriteByte(e)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				u, ok := p.hex4()
				if !ok {
					return "", ErrSyntax
				}
				r := rune(u)
				if r >= 0xD800 && r <= 0xDBFF && strings.HasPrefix(p.s[p.i:], "\\u") {
					save := p.i
					p.i += 2
					if low, ok := p.hex4(); ok && low >= 0xDC00 && low <= 0xDFFF {
						b.WriteRune(0x10000 + (r-0xD800)<<10 + (rune(low) - 0xDC00))
						continue
					}
					p.i = save
				}
				writeCodePoint(&b, r)
			default:
				return "", ErrSyntax
			}
		default:
			start := p.i
			for p.i < len(p.s) && p.s[p.i] != '"' && p.s[p.i] != '\\' && p.s[p.i] >= 0x20 {
				p.i++
			}
			b.WriteString(p.s[start:p.i])
		}
	}
}

func (p *parser) hex4() (uint16, bool) {
	if p.i+4 > len(p.s) {
		return 0, false
	}
	var u uint16
	for k := 0; k < 4; k++ {
		c := p.s[p.i+k]
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		u = u<<4 | uint16(d)
	}
	p.i += 4
	return u, true
}

// Stringify is JSON.stringify(v) (indent "") or JSON.stringify(v, null, 2) (indent "  "). Members whose value is
// Undefined are left out; numbers that are not finite are written as null.
func Stringify(v Value, indent string) string {
	var b strings.Builder
	writeJSON(&b, v, indent, "")
	return b.String()
}

func writeJSON(b *strings.Builder, v Value, indent, current string) {
	switch v.kind {
	case Undefined, Null:
		b.WriteString("null")
	case Bool:
		if v.b {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case Number:
		if math.IsNaN(v.n) || math.IsInf(v.n, 0) {
			b.WriteString("null")
		} else {
			b.WriteString(NumberToString(v.n))
		}
	case String:
		Quote(b, v.s)
	case Array:
		if len(v.arr) == 0 {
			b.WriteString("[]")
			return
		}
		inner := current + indent
		b.WriteByte('[')
		for i, item := range v.arr {
			if i > 0 {
				b.WriteByte(',')
			}
			if indent != "" {
				b.WriteByte('\n')
				b.WriteString(inner)
			}
			writeJSON(b, item, indent, inner)
		}
		if indent != "" {
			b.WriteByte('\n')
			b.WriteString(current)
		}
		b.WriteByte(']')
	case Object:
		inner := current + indent
		wrote := false
		for _, k := range v.Keys() {
			member := v.Get(k)
			if member.kind == Undefined {
				continue
			}
			if wrote {
				b.WriteByte(',')
			} else {
				b.WriteByte('{')
			}
			wrote = true
			if indent != "" {
				b.WriteByte('\n')
				b.WriteString(inner)
			}
			Quote(b, k)
			b.WriteByte(':')
			if indent != "" {
				b.WriteByte(' ')
			}
			writeJSON(b, member, indent, inner)
		}
		if !wrote {
			b.WriteString("{}")
			return
		}
		if indent != "" {
			b.WriteByte('\n')
			b.WriteString(current)
		}
		b.WriteByte('}')
	}
}

const hexDigits = "0123456789abcdef"

// Quote writes s as a JSON string the way JSON.stringify does: only ", \ and control characters are escaped,
// and a lone surrogate as \uXXXX.
func Quote(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' && c != 0xED {
			b.WriteByte(c)
			i++
			continue
		}
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case 0xED:
			r, n := decode(s, i)
			if r >= 0xD800 && r <= 0xDFFF {
				b.WriteString(`\u`)
				b.WriteByte(hexDigits[r>>12&0xF])
				b.WriteByte(hexDigits[r>>8&0xF])
				b.WriteByte(hexDigits[r>>4&0xF])
				b.WriteByte(hexDigits[r&0xF])
			} else {
				b.WriteString(s[i : i+n])
			}
			i += n
			continue
		default:
			b.WriteString(`\u00`)
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0xF])
		}
		i++
	}
	b.WriteByte('"')
}
