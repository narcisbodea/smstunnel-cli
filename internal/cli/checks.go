package cli

import (
	"strings"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

const oneNumberPerText = "One number per text."

// checkRecipient checks one number the way SMSTunnel takes it and returns it trimmed, or the message saying what
// is wrong. SMSTunnel completes a number without + or 00 with the country code.
func checkRecipient(input string) (string, string) {
	value := js.Trim(input)
	if value == "" {
		return "", "Give the number to text, like +40712345678."
	}
	if strings.ContainsAny(value, ",;|&\n\r\t") {
		return "", oneNumberPerText
	}
	plus := strings.Count(value, "+")
	if plus > 1 {
		return "", oneNumberPerText
	}
	if plus == 1 && !strings.HasPrefix(value, "+") {
		return "", "Put the + only at the start of the number, like +40712345678."
	}
	digits := 0
	letters := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= '0' && c <= '9' {
			digits++
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			letters = true
		}
	}
	if digits > 17 {
		return "", "That looks like more than one number. " + oneNumberPerText
	}
	if letters {
		return "", `"` + value + `" is not a phone number: write it with digits, like +40712345678.`
	}
	if !serverNumberPattern(value) || digits < 6 {
		return "", "Write the number with 7 to 25 characters: digits, spaces, dots, dashes, brackets and a leading +, like +40712345678."
	}
	return value, ""
}

// serverNumberPattern is /^[\d\s.\-()+]{7,25}$/ (SMSTunnel's own check of "to").
func serverNumberPattern(value string) bool {
	cps := js.CodePoints(value)
	if len(cps) < 7 || len(cps) > 25 {
		return false
	}
	for _, r := range cps {
		if r >= '0' && r <= '9' || r == '.' || r == '-' || r == '(' || r == ')' || r == '+' || js.IsSpace(r) {
			continue
		}
		return false
	}
	return true
}

// checkCountryCode: white space removed, then /^(\+|00)?[1-9]\d{0,3}$/.
func checkCountryCode(input string) (string, string) {
	var b strings.Builder
	for _, r := range js.CodePoints(input) {
		if !js.IsSpace(r) {
			b.WriteString(string(r))
		}
	}
	value := b.String()
	rest := value
	if strings.HasPrefix(rest, "+") {
		rest = rest[1:]
	} else if strings.HasPrefix(rest, "00") && countryDigits(rest[2:]) {
		rest = rest[2:]
	}
	if countryDigits(rest) {
		return value, ""
	}
	return "", "Give the country calling code in digits, like --country +40 or --country 44 (not letters such as RO)."
}

func countryDigits(s string) bool {
	if len(s) < 1 || len(s) > 4 || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// containsSignInCode is SMSTunnel's check for /sms/send-2fa: one of
//
//	/(?<![\w+]|\d[.,:/])\d{4,10}(?!\w|[.,:/]\d)/
//	/(?<![\w+-]|\d[.,:/])\d{3}-\d{3}(?!\w|-\d|[.,:/]\d)/
//
// matches. Go's regexp has no look-around, so the two are matched by hand (on UTF-16 code units, like JavaScript;
// every class is ASCII, so code points give the same answers).
func containsSignInCode(message string) bool {
	s := js.CodePoints(message)
	at := func(i int) rune {
		if i < 0 || i >= len(s) {
			return -1
		}
		return s[i]
	}
	digit := func(r rune) bool { return r >= '0' && r <= '9' }
	word := func(r rune) bool { return digit(r) || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' }
	sep := func(r rune) bool { return r == '.' || r == ',' || r == ':' || r == '/' }
	digitThenSep := func(i int) bool { return digit(at(i-2)) && sep(at(i-1)) } // \d[.,:/] ending at i
	for start := 0; start <= len(s); start++ {
		// First pattern.
		if !(word(at(start-1)) || at(start-1) == '+') && !digitThenSep(start) {
			for length := 10; length >= 4; length-- {
				all := true
				for k := 0; k < length; k++ {
					if !digit(at(start + k)) {
						all = false
						break
					}
				}
				if !all {
					continue
				}
				end := start + length
				if word(at(end)) || (sep(at(end)) && digit(at(end+1))) {
					continue
				}
				return true
			}
		}
		// Second pattern.
		if !(word(at(start-1)) || at(start-1) == '+' || at(start-1) == '-') && !digitThenSep(start) {
			if digit(at(start)) && digit(at(start+1)) && digit(at(start+2)) && at(start+3) == '-' &&
				digit(at(start+4)) && digit(at(start+5)) && digit(at(start+6)) {
				end := start + 7
				if !(word(at(end)) || (at(end) == '-' && digit(at(end+1))) || (sep(at(end)) && digit(at(end+1)))) {
					return true
				}
			}
		}
	}
	return false
}

const gsm7Basic = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?" +
	"¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"

const gsm7Extension = "\f^{}\\[~]|€"

var gsm7BasicSet, gsm7ExtensionSet = runeSet(gsm7Basic), runeSet(gsm7Extension)

func runeSet(s string) map[rune]bool {
	m := map[rune]bool{}
	for _, r := range s {
		m[r] = true
	}
	return m
}

// smsParts estimates the parts a text takes: 160 / 153 per part in GSM-7, 70 / 67 in UCS-2.
func smsParts(text string) (encoding string, characters, parts int) {
	cps := js.CodePoints(text)
	if len(cps) == 0 {
		return "GSM-7", 0, 0
	}
	units, gsm := 0, true
	for _, r := range cps {
		switch {
		case gsm7BasicSet[r]:
			units++
		case gsm7ExtensionSet[r]:
			units += 2
		default:
			gsm = false
		}
		if !gsm {
			break
		}
	}
	if gsm {
		if units <= 160 {
			return "GSM-7", len(cps), 1
		}
		return "GSM-7", len(cps), (units + 152) / 153
	}
	codeUnits := js.Length(text)
	if codeUnits <= 70 {
		return "UCS-2", len(cps), 1
	}
	return "UCS-2", len(cps), (codeUnits + 66) / 67
}

// checkIdempotencyKey: trimmed, 1 to 255 printable ASCII characters (the server's rule).
func checkIdempotencyKey(input string) (string, string) {
	value := js.Trim(input)
	ok := value != "" && js.Length(value) <= maxIdempotencyKeyLength
	for i := 0; ok && i < len(value); i++ {
		ok = value[i] >= 0x20 && value[i] <= 0x7E
	}
	if ok {
		return value, ""
	}
	return "", "--idempotency-key takes 1 to 255 printable ASCII characters (letters, digits and signs such as - _ . :), like --idempotency-key order-1042-ready. A UUID is a good choice."
}
