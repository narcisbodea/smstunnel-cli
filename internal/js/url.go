package js

import (
	"errors"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// URL is what new URL(input) gives for an absolute URL, reduced to the parts the CLI reads.
type URL struct {
	Scheme      string // without ":"
	HasUserinfo bool   // a user name or a password that is not empty
	Host        string // serialized host: a domain, a dotted IPv4 address or [IPv6]; "" when there is none
	Port        string // "" for none or the scheme's default port
	Pathname    string
	HasQuery    bool // a query that is not empty
	HasFragment bool // a fragment that is not empty
}

// HostPort is url.host: the host and, when not the default, ":port".
func (u *URL) HostPort() string {
	if u.Port == "" {
		return u.Host
	}
	return u.Host + ":" + u.Port
}

// ErrInvalidURL is returned for input the WHATWG URL parser refuses.
var ErrInvalidURL = errors.New("invalid URL")

var specialPorts = map[string]string{"ftp": "21", "file": "", "http": "80", "https": "443", "ws": "80", "wss": "443"}

func isSpecial(scheme string) bool { _, ok := specialPorts[scheme]; return ok }

// ParseURL runs the WHATWG basic URL parser (no base URL) far enough to give the parts of URL: the same
// failures, hosts, ports and paths as new URL(input) in Node.js. Domains with characters outside ASCII get a
// lighter mapping than full IDNA (lower case, full-width forms, then Punycode).
func ParseURL(input string) (*URL, error) {
	cps := urlCodePoints(input)
	p := &urlParser{in: cps}
	return p.parse()
}

// urlCodePoints drops leading and trailing C0 controls and spaces, and every tab and newline.
func urlCodePoints(input string) []rune {
	cps := CodePoints(input)
	start, end := 0, len(cps)
	for start < end && cps[start] <= 0x20 {
		start++
	}
	for end > start && cps[end-1] <= 0x20 {
		end--
	}
	out := make([]rune, 0, end-start)
	for _, r := range cps[start:end] {
		if r != '\t' && r != '\n' && r != '\r' {
			out = append(out, r)
		}
	}
	return out
}

type urlParser struct {
	in  []rune
	pos int
	url URL

	path []string
}

const eof = rune(-1)

func (p *urlParser) at(i int) rune {
	if i < 0 || i >= len(p.in) {
		return eof
	}
	return p.in[i]
}

func isASCIIAlpha(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }
func isASCIIHexDigit(r rune) bool {
	return isASCIIDigit(r) || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}

func (p *urlParser) parse() (*URL, error) {
	// Scheme.
	if !isASCIIAlpha(p.at(0)) {
		return nil, ErrInvalidURL
	}
	i := 0
	for ; i < len(p.in); i++ {
		r := p.in[i]
		if isASCIIAlpha(r) || isASCIIDigit(r) || r == '+' || r == '-' || r == '.' {
			continue
		}
		break
	}
	if p.at(i) != ':' {
		return nil, ErrInvalidURL // no scheme, and there is no base URL
	}
	p.url.Scheme = strings.ToLower(string(p.in[:i]))
	p.pos = i + 1
	special := isSpecial(p.url.Scheme)

	switch {
	case p.url.Scheme == "file":
		return p.parseFile()
	case special:
		// Special authority slashes, then ignore any further slashes and backslashes.
		for p.at(p.pos) == '/' || p.at(p.pos) == '\\' {
			p.pos++
		}
		if err := p.authority(true); err != nil {
			return nil, err
		}
		return p.pathStart(true)
	case p.at(p.pos) == '/' && p.at(p.pos+1) == '/':
		p.pos += 2
		if err := p.authority(false); err != nil {
			return nil, err
		}
		return p.pathStart(false)
	case p.at(p.pos) == '/':
		return p.pathStart(false)
	}
	// An opaque path ("mailto:x"): nothing in it can fail.
	p.url.Pathname = string(p.in[p.pos:])
	p.queryAndFragment(len(p.in))
	return &p.url, nil
}

// authority reads [userinfo@]host[:port] up to the first /, ?, # (or \ for special schemes).
func (p *urlParser) authority(special bool) error {
	end := p.pos
	for end < len(p.in) {
		r := p.in[end]
		if r == '/' || r == '?' || r == '#' || (special && r == '\\') {
			break
		}
		end++
	}
	section := p.in[p.pos:end]
	if at := lastIndexRune(section, '@'); at >= 0 {
		// The first ":" separates the user name from the password; every other code point is part of one of them.
		userinfo := section[:at]
		separators := 0
		for _, r := range userinfo {
			if r == ':' {
				separators = 1
				break
			}
		}
		p.url.HasUserinfo = len(userinfo)-separators > 0
		section = section[at+1:]
		if len(section) == 0 {
			return ErrInvalidURL // "user@" with no host
		}
	}
	// The host ends at the first ":" outside brackets.
	hostEnd, inside := len(section), false
	for k, r := range section {
		if r == '[' {
			inside = true
		} else if r == ']' {
			inside = false
		} else if r == ':' && !inside {
			hostEnd = k
			break
		}
	}
	hostText := string(section[:hostEnd])
	if hostEnd < len(section) && hostText == "" {
		return ErrInvalidURL
	}
	if special && hostText == "" {
		return ErrInvalidURL
	}
	host, err := parseHost(hostText, !special)
	if err != nil {
		return err
	}
	p.url.Host = host
	if hostEnd < len(section) {
		port := section[hostEnd+1:]
		for _, r := range port {
			if !isASCIIDigit(r) {
				return ErrInvalidURL
			}
		}
		if len(port) > 0 {
			n := new(big.Int)
			n.SetString(string(port), 10)
			if n.Cmp(big.NewInt(65535)) > 0 {
				return ErrInvalidURL
			}
			text := n.String()
			if text != specialPorts[p.url.Scheme] || !special {
				p.url.Port = text
			}
		}
	}
	p.pos = end
	return nil
}

func lastIndexRune(s []rune, r rune) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == r {
			return i
		}
	}
	return -1
}

func (p *urlParser) parseFile() (*URL, error) {
	// file: hosts matter only for failures here: the CLI refuses the scheme anyway.
	if (p.at(p.pos) == '/' || p.at(p.pos) == '\\') && (p.at(p.pos+1) == '/' || p.at(p.pos+1) == '\\') {
		p.pos += 2
		end := p.pos
		for end < len(p.in) {
			r := p.in[end]
			if r == '/' || r == '\\' || r == '?' || r == '#' {
				break
			}
			end++
		}
		buf := string(p.in[p.pos:end])
		if !isWindowsDriveLetter(buf) && buf != "" {
			host, err := parseHost(buf, false)
			if err != nil {
				return nil, err
			}
			if host != "localhost" {
				p.url.Host = host
			}
			p.pos = end
		}
	}
	return p.pathStart(true)
}

func isWindowsDriveLetter(s string) bool {
	return len(s) == 2 && isASCIIAlpha(rune(s[0])) && (s[1] == ':' || s[1] == '|')
}

// pathStart reads the path, then the query and the fragment.
func (p *urlParser) pathStart(special bool) (*URL, error) {
	end := p.pos
	for end < len(p.in) && p.in[end] != '?' && p.in[end] != '#' {
		end++
	}
	segmentsText := p.in[p.pos:end]
	if special && len(segmentsText) > 0 && (segmentsText[0] == '/' || segmentsText[0] == '\\') {
		segmentsText = segmentsText[1:]
	} else if !special && len(segmentsText) > 0 && segmentsText[0] == '/' {
		segmentsText = segmentsText[1:]
	} else if !special && len(segmentsText) == 0 {
		// A non-special URL with an empty path keeps it empty.
		p.queryAndFragment(end)
		return &p.url, nil
	}
	var buf []rune
	flush := func(last bool) {
		segment := string(buf)
		switch {
		case isDoubleDot(segment):
			if len(p.path) > 0 && !(p.url.Scheme == "file" && len(p.path) == 1 && isNormalizedDriveLetter(p.path[0])) {
				p.path = p.path[:len(p.path)-1]
			}
			if last {
				p.path = append(p.path, "")
			}
		case isSingleDot(segment):
			if last {
				p.path = append(p.path, "")
			}
		default:
			p.path = append(p.path, encodePath(buf))
		}
		buf = buf[:0]
	}
	for i, r := range segmentsText {
		if r == '/' || (special && r == '\\') {
			flush(false)
			continue
		}
		buf = append(buf, r)
		_ = i
	}
	flush(true)
	var b strings.Builder
	for _, segment := range p.path {
		b.WriteByte('/')
		b.WriteString(segment)
	}
	p.url.Pathname = b.String()
	p.queryAndFragment(end)
	return &p.url, nil
}

func (p *urlParser) queryAndFragment(from int) {
	rest := p.in[from:]
	if len(rest) == 0 {
		return
	}
	if rest[0] == '?' {
		end := len(rest)
		for k, r := range rest {
			if r == '#' {
				end = k
				break
			}
		}
		p.url.HasQuery = end > 1
		rest = rest[end:]
	}
	if len(rest) > 0 && rest[0] == '#' {
		p.url.HasFragment = len(rest) > 1
	}
}

func isNormalizedDriveLetter(s string) bool {
	return len(s) == 2 && isASCIIAlpha(rune(s[0])) && s[1] == ':'
}

func isSingleDot(s string) bool { return s == "." || strings.EqualFold(s, "%2e") }

func isDoubleDot(s string) bool {
	switch strings.ToLower(s) {
	case "..", ".%2e", "%2e.", "%2e%2e":
		return true
	}
	return false
}

// encodePath percent-encodes a path segment with the path percent-encode set (as Node.js 22 applies it).
func encodePath(segment []rune) string {
	var b strings.Builder
	for _, r := range segment {
		if r < 0x80 && !inPathSet(r) {
			b.WriteRune(r)
			continue
		}
		var tmp [4]byte
		n := utf8.EncodeRune(tmp[:], r)
		for _, c := range tmp[:n] {
			b.WriteString("%" + strings.ToUpper(strconv.FormatUint(uint64(c)>>4, 16)) + strings.ToUpper(strconv.FormatUint(uint64(c)&0xF, 16)))
		}
	}
	return b.String()
}

func inPathSet(r rune) bool {
	if r <= 0x1F || r > 0x7E {
		return true
	}
	switch r {
	case ' ', '"', '#', '<', '>', '?', '`', '{', '}':
		return true
	}
	return false
}

// ---- hosts ----

func parseHost(input string, opaque bool) (string, error) {
	if strings.HasPrefix(input, "[") {
		if !strings.HasSuffix(input, "]") {
			return "", ErrInvalidURL
		}
		addr, ok := parseIPv6(input[1 : len(input)-1])
		if !ok {
			return "", ErrInvalidURL
		}
		return "[" + serializeIPv6(addr) + "]", nil
	}
	if opaque {
		for _, r := range input {
			if isForbiddenHostCodePoint(r) {
				return "", ErrInvalidURL
			}
		}
		return input, nil
	}
	domain := DecodeUTF8(percentDecode(input))
	ascii, ok := domainToASCII(domain)
	if !ok {
		return "", ErrInvalidURL
	}
	for _, r := range ascii {
		if isForbiddenDomainCodePoint(r) {
			return "", ErrInvalidURL
		}
	}
	if endsInANumber(ascii) {
		v, ok := parseIPv4(ascii)
		if !ok {
			return "", ErrInvalidURL
		}
		return strconv.FormatUint(v>>24, 10) + "." + strconv.FormatUint(v>>16&0xFF, 10) + "." +
			strconv.FormatUint(v>>8&0xFF, 10) + "." + strconv.FormatUint(v&0xFF, 10), nil
	}
	return ascii, nil
}

func percentDecode(s string) []byte {
	b := []byte(s)
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '%' && i+2 < len(b) && isASCIIHexDigit(rune(b[i+1])) && isASCIIHexDigit(rune(b[i+2])) {
			v, _ := strconv.ParseUint(string(b[i+1:i+3]), 16, 8)
			out = append(out, byte(v))
			i += 2
			continue
		}
		out = append(out, b[i])
	}
	return out
}

func isForbiddenHostCodePoint(r rune) bool {
	switch r {
	case 0, '\t', '\n', '\r', ' ', '#', '/', ':', '<', '>', '?', '@', '[', '\\', ']', '^', '|':
		return true
	}
	return false
}

func isForbiddenDomainCodePoint(r rune) bool {
	return isForbiddenHostCodePoint(r) || r <= 0x1F || r == '%' || r == 0x7F
}

func endsInANumber(s string) bool {
	parts := strings.Split(s, ".")
	if parts[len(parts)-1] == "" {
		if len(parts) == 1 {
			return false
		}
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := parseIPv4Number(last)
	return ok
}

func parseIPv4Number(s string) (*big.Int, bool) {
	if s == "" {
		return nil, false
	}
	base := 10
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		base, s = 16, s[2:]
	} else if len(s) >= 2 && s[0] == '0' {
		base, s = 8, s[1:]
	}
	if s == "" {
		return big.NewInt(0), true
	}
	for _, r := range s {
		ok := false
		switch base {
		case 10:
			ok = isASCIIDigit(r)
		case 16:
			ok = isASCIIHexDigit(r)
		case 8:
			ok = r >= '0' && r <= '7'
		}
		if !ok {
			return nil, false
		}
	}
	n, ok := new(big.Int).SetString(s, base)
	return n, ok
}

func parseIPv4(s string) (uint64, bool) {
	parts := strings.Split(s, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return 0, false
	}
	numbers := make([]*big.Int, 0, len(parts))
	for _, part := range parts {
		n, ok := parseIPv4Number(part)
		if !ok {
			return 0, false
		}
		numbers = append(numbers, n)
	}
	limit255 := big.NewInt(255)
	for _, n := range numbers[:len(numbers)-1] {
		if n.Cmp(limit255) > 0 {
			return 0, false
		}
	}
	last := numbers[len(numbers)-1]
	max := new(big.Int).Exp(big.NewInt(256), big.NewInt(int64(5-len(numbers))), nil)
	if last.Cmp(max) >= 0 {
		return 0, false
	}
	v := last.Uint64()
	for i, n := range numbers[:len(numbers)-1] {
		v += n.Uint64() << (8 * uint(3-i))
	}
	return v, true
}

func parseIPv6(s string) ([8]uint16, bool) {
	var addr [8]uint16
	in := []rune(s)
	piece, compress := 0, -1
	i := 0
	at := func(k int) rune {
		if k >= len(in) {
			return eof
		}
		return in[k]
	}
	if at(i) == ':' {
		if at(i+1) != ':' {
			return addr, false
		}
		i += 2
		piece++
		compress = piece
	}
	for at(i) != eof {
		if piece == 8 {
			return addr, false
		}
		if at(i) == ':' {
			if compress != -1 {
				return addr, false
			}
			i++
			piece++
			compress = piece
			continue
		}
		value, length := 0, 0
		for length < 4 && isASCIIHexDigit(at(i)) {
			d, _ := strconv.ParseUint(string(at(i)), 16, 8)
			value = value*16 + int(d)
			i++
			length++
		}
		if at(i) == '.' {
			if length == 0 {
				return addr, false
			}
			i -= length
			if piece > 6 {
				return addr, false
			}
			seen := 0
			for at(i) != eof {
				v4 := -1
				if seen > 0 {
					if at(i) == '.' && seen < 4 {
						i++
					} else {
						return addr, false
					}
				}
				if !isASCIIDigit(at(i)) {
					return addr, false
				}
				for isASCIIDigit(at(i)) {
					n := int(at(i) - '0')
					if v4 == -1 {
						v4 = n
					} else if v4 == 0 {
						return addr, false
					} else {
						v4 = v4*10 + n
					}
					if v4 > 255 {
						return addr, false
					}
					i++
				}
				addr[piece] = addr[piece]*0x100 + uint16(v4)
				seen++
				if seen == 2 || seen == 4 {
					piece++
				}
			}
			if seen != 4 {
				return addr, false
			}
			break
		} else if at(i) == ':' {
			i++
			if at(i) == eof {
				return addr, false
			}
		} else if at(i) != eof {
			return addr, false
		}
		addr[piece] = uint16(value)
		piece++
	}
	if compress != -1 {
		swaps := piece - compress
		piece = 7
		for piece != 0 && swaps > 0 {
			addr[piece], addr[compress+swaps-1] = addr[compress+swaps-1], addr[piece]
			piece--
			swaps--
		}
	} else if piece != 8 {
		return addr, false
	}
	return addr, true
}

func serializeIPv6(addr [8]uint16) string {
	// The first longest run of two or more zero pieces is compressed.
	bestStart, bestLen := -1, 1
	for i := 0; i < 8; {
		if addr[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && addr[j] == 0 {
			j++
		}
		if j-i > bestLen {
			bestStart, bestLen = i, j-i
		}
		i = j
	}
	var b strings.Builder
	ignore0 := false
	for i := 0; i < 8; i++ {
		if ignore0 && addr[i] == 0 {
			continue
		}
		ignore0 = false
		if i == bestStart {
			if i == 0 {
				b.WriteString("::")
			} else {
				b.WriteString(":")
			}
			ignore0 = true
			continue
		}
		b.WriteString(strconv.FormatUint(uint64(addr[i]), 16))
		if i != 7 {
			b.WriteByte(':')
		}
	}
	return b.String()
}

// domainToASCII: ASCII domains are lower-cased (and their xn-- labels checked); others are mapped (full-width
// forms, the ideographic full stop, lower case, a few characters IDNA ignores) and Punycode-encoded.
func domainToASCII(domain string) (string, bool) {
	var mapped strings.Builder
	for _, r := range domain {
		switch {
		case r >= 'A' && r <= 'Z':
			mapped.WriteRune(r + 32)
		case r < 0x80:
			mapped.WriteRune(r)
		case r == 0x3002 || r == 0xFF0E || r == 0xFF61:
			mapped.WriteByte('.')
		case r >= 0xFF01 && r <= 0xFF5E:
			mapped.WriteRune(unicode.ToLower(r - 0xFF01 + 0x21))
		case r == 0xAD || r == 0x34F || r == 0x200B || r == 0x2060 || r == 0xFEFF || (r >= 0x180B && r <= 0x180D) ||
			(r >= 0xFE00 && r <= 0xFE0F):
			// Ignored by IDNA's mapping.
		case IsSpace(r) || r == utf8.RuneError:
			return "", false
		default:
			mapped.WriteRune(unicode.ToLower(r))
		}
	}
	labels := strings.Split(mapped.String(), ".")
	for i, label := range labels {
		if strings.HasPrefix(label, "xn--") {
			decoded, ok := punycodeDecode(label[4:])
			if !ok || isASCIIString(decoded) || !validIDNALabel(decoded) {
				return "", false
			}
			if encoded, ok := punycodeEncode(decoded); !ok || encoded != label[4:] {
				return "", false
			}
			continue
		}
		if !isASCIIString(label) {
			encoded, ok := punycodeEncode(label)
			if !ok {
				return "", false
			}
			labels[i] = "xn--" + encoded
		}
	}
	return strings.Join(labels, "."), true
}

// validIDNALabel is a light version of IDNA's validity check for a decoded xn-- label: no controls, spaces,
// format or private-use characters, nothing unassigned, and nothing that IDNA's mapping would still change
// (upper case).
func validIDNALabel(label string) bool {
	for _, r := range label {
		if r < 0x80 {
			if r < 0x20 || r == 0x7F {
				return false
			}
			continue
		}
		if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs, unicode.Zs, unicode.Zl, unicode.Zp) ||
			!unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S) || unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

func isASCIIString(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Punycode (RFC 3492).
const (
	pcBase        = 36
	pcTMin        = 1
	pcTMax        = 26
	pcSkew        = 38
	pcDamp        = 700
	pcInitialBias = 72
	pcInitialN    = 128
)

func pcAdapt(delta, numPoints int, first bool) int {
	if first {
		delta /= pcDamp
	} else {
		delta /= 2
	}
	delta += delta / numPoints
	k := 0
	for delta > ((pcBase-pcTMin)*pcTMax)/2 {
		delta /= pcBase - pcTMin
		k += pcBase
	}
	return k + (pcBase-pcTMin+1)*delta/(delta+pcSkew)
}

func pcDigit(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
}

func punycodeEncode(s string) (string, bool) {
	input := []rune(s)
	var out []byte
	for _, r := range input {
		if r < 0x80 {
			out = append(out, byte(r))
		}
	}
	b := len(out)
	h := b
	if b > 0 {
		out = append(out, '-')
	}
	n, delta, bias := pcInitialN, 0, pcInitialBias
	for h < len(input) {
		m := int(^uint(0) >> 1)
		for _, r := range input {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		delta += (m - n) * (h + 1)
		n = m
		for _, r := range input {
			if int(r) < n {
				delta++
			}
			if int(r) == n {
				q := delta
				for k := pcBase; ; k += pcBase {
					t := k - bias
					if t < pcTMin {
						t = pcTMin
					} else if t > pcTMax {
						t = pcTMax
					}
					if q < t {
						break
					}
					out = append(out, pcDigit(t+(q-t)%(pcBase-t)))
					q = (q - t) / (pcBase - t)
				}
				out = append(out, pcDigit(q))
				bias = pcAdapt(delta, h+1, h == b)
				delta = 0
				h++
			}
		}
		delta++
		n++
	}
	return string(out), true
}

func punycodeDecode(s string) (string, bool) {
	var output []rune
	basic := strings.LastIndexByte(s, '-')
	if basic > 0 {
		for _, c := range s[:basic] {
			if c >= 0x80 {
				return "", false
			}
			output = append(output, c)
		}
		s = s[basic+1:]
	} else if basic == 0 {
		s = s[1:]
	}
	n, i, bias := pcInitialN, 0, pcInitialBias
	for pos := 0; pos < len(s); {
		oldI, w := i, 1
		for k := pcBase; ; k += pcBase {
			if pos >= len(s) {
				return "", false
			}
			c := s[pos]
			pos++
			var digit int
			switch {
			case c >= 'a' && c <= 'z':
				digit = int(c - 'a')
			case c >= 'A' && c <= 'Z':
				digit = int(c - 'A')
			case c >= '0' && c <= '9':
				digit = int(c-'0') + 26
			default:
				return "", false
			}
			i += digit * w
			t := k - bias
			if t < pcTMin {
				t = pcTMin
			} else if t > pcTMax {
				t = pcTMax
			}
			if digit < t {
				break
			}
			w *= pcBase - t
			if w > 1<<30 {
				return "", false
			}
		}
		bias = pcAdapt(i-oldI, len(output)+1, oldI == 0)
		n += i / (len(output) + 1)
		i %= len(output) + 1
		if n > unicode.MaxRune {
			return "", false
		}
		output = append(output, 0)
		copy(output[i+1:], output[i:])
		output[i] = rune(n)
		i++
	}
	return string(output), true
}

// FormEncode is how URLSearchParams writes a name or a value (application/x-www-form-urlencoded): space as "+",
// letters, digits and *-._ as they are, every other byte of the UTF-8 form as %XX.
func FormEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(ForOutput(s)) {
		switch {
		case c == ' ':
			b.WriteByte('+')
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '*', c == '-', c == '.', c == '_':
			b.WriteByte(c)
		default:
			b.WriteString("%" + strings.ToUpper(hex2(c)))
		}
	}
	return b.String()
}

func hex2(c byte) string { return string([]byte{hexDigits[c>>4], hexDigits[c&0xF]}) }
