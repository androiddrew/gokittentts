package normalize

import (
	"strings"
	"unicode"
)

var (
	ones = []string{
		"", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
		"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen",
		"seventeen", "eighteen", "nineteen",
	}
	tens   = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}
	scales = []string{"", "thousand", "million", "billion", "trillion"}

	ordinalExceptions = map[string]string{
		"one": "first", "two": "second", "three": "third", "four": "fourth",
		"five": "fifth", "six": "sixth", "seven": "seventh", "eight": "eighth",
		"nine": "ninth", "twelve": "twelfth",
	}

	digitWords = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

	months = map[string]string{
		"jan": "January", "january": "January",
		"feb": "February", "february": "February",
		"mar": "March", "march": "March",
		"apr": "April", "april": "April",
		"may": "May",
		"jun": "June", "june": "June",
		"jul": "July", "july": "July",
		"aug": "August", "august": "August",
		"sep": "September", "sept": "September", "september": "September",
		"oct": "October", "october": "October",
		"nov": "November", "november": "November",
		"dec": "December", "december": "December",
	}
)

// maxScaleDigits is the most digits the scale words reach (999 trillion).
const maxScaleDigits = 15

// digits returns s's decimal digits as ASCII, as Python's int() reads any
// Unicode decimal digit.
func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Nd, r) {
			b.WriteByte(byte('0' + digitValue(r)))
		}
	}
	return b.String()
}

// digitValue is the value of a Unicode decimal digit. Decimal digits come in
// runs of ten from zero, some of them back to back.
func digitValue(r rune) int {
	zero := r
	for unicode.Is(unicode.Nd, zero-1) {
		zero--
	}
	return int(r-zero) % 10
}

// atoi reads ASCII digits short enough to fit; callers check the length.
func atoi(ds string) int {
	n := 0
	for _, c := range ds {
		n = n*10 + int(c-'0')
	}
	return n
}

// intWords is number_to_words for a run of ASCII digits.
//
// Deviation: Python drops the digits above the trillions, so it reads
// 1,000,000,000,000,000 as nothing. A number with more than maxScaleDigits
// digits is read digit by digit.
func intWords(ds string) string {
	ds = strings.TrimLeft(ds, "0")
	if len(ds) > maxScaleDigits {
		words := make([]string, len(ds))
		for i, c := range ds {
			words[i] = digitWords[c-'0']
		}
		return strings.Join(words, " ")
	}
	return numberToWords(atoi(ds))
}

// numberToWords is Python's number_to_words for 0 <= n < 10^15.
func numberToWords(n int) string {
	if n == 0 {
		return "zero"
	}
	// 1200 is "twelve hundred", but 1000 stays "one thousand".
	if 100 <= n && n <= 9999 && n%100 == 0 && n%1000 != 0 && n/100 < 20 {
		return ones[n/100] + " hundred"
	}
	var parts []string
	for _, scale := range scales {
		if chunk := n % 1000; chunk != 0 {
			w := threeDigitsToWords(chunk)
			if scale != "" {
				w += " " + scale
			}
			parts = append(parts, w)
		}
		n /= 1000
		if n == 0 {
			break
		}
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " ")
}

func threeDigitsToWords(n int) string {
	var parts []string
	if h := n / 100; h != 0 {
		parts = append(parts, ones[h]+" hundred")
	}
	switch r := n % 100; {
	case r == 0:
	case r < 20:
		parts = append(parts, ones[r])
	case r%10 == 0:
		parts = append(parts, tens[r/10])
	default:
		parts = append(parts, tens[r/10]+"-"+ones[r%10])
	}
	return strings.Join(parts, " ")
}

// floatWords is Python's float_to_words for a number with a decimal point,
// optionally signed: the decimal digits are read one by one.
func floatWords(s string) string {
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, decPart, _ := strings.Cut(s, ".")
	w := "zero"
	if intPart != "" {
		w = intWords(digits(intPart))
	}
	w += " point"
	for _, c := range digits(decPart) {
		w += " " + digitWords[c-'0']
	}
	if negative {
		return "negative " + w
	}
	return w
}

// yearWords is Python's _year_to_words: 1900 to 2099 are read as years.
func yearWords(year int) string {
	rest := year % 100
	switch {
	case 1900 <= year && year <= 1999:
		if rest == 0 {
			return "nineteen hundred"
		}
		return "nineteen " + numberToWords(rest)
	case 2000 <= year && year <= 2009:
		if rest == 0 {
			return "two thousand"
		}
		return "two thousand " + numberToWords(rest)
	case 2010 <= year && year <= 2099:
		if rest == 0 {
			return "twenty hundred"
		}
		return "twenty " + numberToWords(rest)
	}
	return numberToWords(year)
}

// isYear reports whether ASCII digits ds are 1900 to 2099.
func isYear(ds string) bool {
	ds = strings.TrimLeft(ds, "0")
	if len(ds) != 4 {
		return false
	}
	n := atoi(ds)
	return 1900 <= n && n <= 2099
}

// numberOrYearWords reads ASCII digits as a year if they are 1900 to 2099,
// and as a number otherwise.
func numberOrYearWords(ds string) string {
	if isYear(ds) {
		return yearWords(atoi(strings.TrimLeft(ds, "0")))
	}
	return intWords(ds)
}

// ordinalWords is Python's _ordinal_suffix for ASCII digits: the last word of
// the number becomes its ordinal. As in Python, a number with a hyphen is cut
// at its last hyphen, not its last space.
//
// Deviation: Python turns a last word ending in "y" into "-yth" ("twentyth").
// It becomes "-ieth" ("twentieth").
func ordinalWords(ds string) string {
	w := intWords(ds)
	cut := strings.LastIndex(w, "-")
	if cut < 0 {
		cut = strings.LastIndex(w, " ")
	}
	prefix, last := w[:cut+1], w[cut+1:]
	if o, ok := ordinalExceptions[last]; ok {
		return prefix + o
	}
	switch {
	case strings.HasSuffix(last, "t"):
		last += "h"
	case strings.HasSuffix(last, "e"):
		last = last[:len(last)-1] + "th"
	case strings.HasSuffix(last, "y"):
		last = last[:len(last)-1] + "ieth"
	default:
		last += "th"
	}
	return prefix + last
}
