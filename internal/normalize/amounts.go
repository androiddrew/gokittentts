package normalize

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	// ([$€£¥₹₩₿])\s*([\d,]+(?:\.\d+)?)\s*([KMBT])?(?![a-zA-Z\d]); the rest
	// after the integer digits is matched by hand.
	reCurrency = regexp.MustCompile(`([$€£¥₹₩₿])[` + space + `]*([\p{Nd},]+)`)
	// The deviation for a scale word after an amount: "$3.5 million".
	reCurrencyScaleWord = regexp.MustCompile(`^(\p{Nd}(?:[\p{Nd},]*\p{Nd})?(?:\.\p{Nd}+)?)[` + space + `]+((?i:thousand|million|billion|trillion))`)
	reDecimals          = regexp.MustCompile(`^\.\p{Nd}+`)
	// (-?[\d,]+(?:\.\d+)?)\s*%
	rePercent = regexp.MustCompile(`(-?[\p{Nd},]+(?:\.\p{Nd}+)?)[` + space + `]*%`)
	// Not in normalize_text; ported from TextPreprocessor's _RE_UNIT.
	reUnit = regexp.MustCompile(`(?i)(\p{Nd}+(?:\.\p{Nd}+)?)[` + space + `]*(km|kg|mg|ml|gb|mb|kb|tb|hz|khz|mhz|ghz|mph|kph|°[cf]|[cf]°|ms|ns|µs)`)
	// Not in normalize_text; ported from TextPreprocessor's _RE_SCALE
	// without its \s*, so "3 T-shirts" isn't "three trillion-shirts".
	reScaleSuffix = regexp.MustCompile(`(\p{Nd}+(?:\.\p{Nd}+)?)([KMBT])`)
)

var (
	currencyUnits = map[string]string{
		"$": "dollar", "€": "euro", "£": "pound", "¥": "yen",
		"₹": "rupee", "₩": "won", "₿": "bitcoin",
	}
	scaleWords = map[string]string{"K": "thousand", "M": "million", "B": "billion", "T": "trillion"}

	units = map[string]unitForms{
		"km": {"kilometer", "kilometers"}, "kg": {"kilogram", "kilograms"}, "mg": {"milligram", "milligrams"},
		"ml": {"milliliter", "milliliters"}, "gb": {"gigabyte", "gigabytes"}, "mb": {"megabyte", "megabytes"},
		"kb": {"kilobyte", "kilobytes"}, "tb": {"terabyte", "terabytes"},
		"hz": {"hertz", "hertz"}, "khz": {"kilohertz", "kilohertz"}, "mhz": {"megahertz", "megahertz"}, "ghz": {"gigahertz", "gigahertz"},
		"mph": {"mile per hour", "miles per hour"}, "kph": {"kilometer per hour", "kilometers per hour"},
		"ms": {"millisecond", "milliseconds"}, "ns": {"nanosecond", "nanoseconds"}, "µs": {"microsecond", "microseconds"},
		"°c": {"degree Celsius", "degrees Celsius"}, "c°": {"degree Celsius", "degrees Celsius"},
		"°f": {"degree Fahrenheit", "degrees Fahrenheit"}, "f°": {"degree Fahrenheit", "degrees Fahrenheit"},
	}
)

// unitForms is a unit's singular and plural.
type unitForms struct{ one, many string }

// currency is Python's expand_currency over _RE_CURRENCY. Its lookahead can
// make Python back off the scale letter, the spaces before it, the decimals
// or even integer digits ("$1,000x" is "$1" then ",000x"), so the match is
// finished by hand, trying the choices in the order Python's backtracking
// does.
//
// Deviations:
//   - An amount followed by a scale word is read with the word before the
//     unit: Python reads "$3.5 million" as "three dollars and fifty cents
//     million".
//   - Commas after a whole amount stay in the text, as for plain numbers.
//   - An amount with no digits ("$,") is left alone; Python raises.
//   - One whole unit is singular ("one dollar and fifty cents"); Python says
//     "one dollars" when there are cents.
//   - Yen and won don't take an "s".
func currency(s string) string {
	return scan(s, reCurrency, func(s string, loc []int) (int, string, bool) {
		unit := currencyUnits[s[loc[2]:loc[3]]]
		if m := reCurrencyScaleWord.FindStringSubmatchIndex(s[loc[4]:]); m != nil && !wordAt(s, loc[4]+m[1]) {
			raw := strings.ReplaceAll(s[loc[4]:loc[4]+m[3]], ",", "")
			scale := strings.ToLower(s[loc[4]+m[4] : loc[4]+m[5]])
			return loc[4] + m[1], amountWords(raw) + " " + scale + " " + unitName(unit, false), true
		}
		for intEnd := loc[5]; intEnd > loc[4]; intEnd -= lastRuneLen(s[:intEnd]) {
			// Fewer decimals would leave a digit next, which the lookahead
			// rejects, so the decimals are all or nothing.
			numEnds := []int{intEnd}
			if d := reDecimals.FindString(s[intEnd:]); d != "" && intEnd == loc[5] {
				numEnds = []int{intEnd + len(d), intEnd}
			}
			if digits(s[loc[4]:intEnd]) == "" {
				return 0, "", false
			}
			for _, numEnd := range numEnds {
				spaces := spacesFrom(s, numEnd)
				for i := len(spaces) - 1; i >= 0; i-- {
					at := spaces[i]
					if at < len(s) && strings.ContainsRune("KMBT", rune(s[at])) && !alnumAt(s, at+1) {
						return at + 1, currencyWords(s[loc[4]:intEnd], s[intEnd:numEnd], s[at:at+1], unit), true
					}
					if !alnumAt(s, at) {
						return at, currencyWords(s[loc[4]:intEnd], s[intEnd:numEnd], "", unit), true
					}
				}
			}
		}
		return 0, "", false
	})
}

// currencyWords reads an amount: its integer digits and commas, its
// decimals with the point, and its scale letter.
func currencyWords(whole, decimals, scale, unit string) string {
	trimmed := strings.TrimRight(whole, ",")
	commas := whole[len(trimmed):]
	ds := digits(trimmed)
	if scale != "" {
		return amountWords(ds+decimals) + " " + scaleWords[scale] + " " + unitName(unit, false) + commas
	}
	n := intWords(ds)
	if decimals == "" {
		return n + " " + unitName(unit, isOne(ds)) + commas
	}
	cents := (digits(decimals) + "0")[:2]
	w := n + " " + unitName(unit, isOne(ds))
	switch c := atoi(cents); {
	case c == 1:
		w += " and one cent"
	case c > 1:
		w += " and " + numberToWords(c) + " cents"
	}
	return w
}

// amountWords reads digits with an optional decimal part.
func amountWords(raw string) string {
	if strings.Contains(raw, ".") {
		return floatWords(raw)
	}
	return intWords(digits(raw))
}

// unitName is a currency unit, singular or plural.
func unitName(unit string, singular bool) string {
	if singular || unit == "yen" || unit == "won" {
		return unit
	}
	return unit + "s"
}

// isOne reports whether ASCII digits ds are the number one.
func isOne(ds string) bool { return strings.TrimLeft(ds, "0") == "1" }

// percent is Python's expand_percentages. Python reads a decimal through
// float(), so "3.50%" is "three point five percent".
//
// Deviations:
//   - A percent with no digits ("-,%") is left alone, and a decimal whose
//     float Python writes in exponent form ("0.00001%") is read as written;
//     Python raises on both.
//   - A minus right after a word character is a hyphen, not a sign: Python
//     reads "5-10%" as "fivenegative ten percent".
func percent(s string) string {
	return scan(s, rePercent, func(s string, loc []int) (int, string, bool) {
		start := loc[2]
		if s[start] == '-' && wordAt(s, start-1) {
			start++
		}
		return loc[1], s[loc[0]:start] + percentWords(s[start:loc[3]], s[start:loc[1]]), true
	})
}

// percentWords reads a percent's number; whole is the match to leave if it
// has no digits.
func percentWords(number, whole string) string {
	raw := strings.ReplaceAll(number, ",", "")
	ds := digits(raw)
	if ds == "" {
		return whole
	}
	if strings.Contains(raw, ".") {
		return floatWords(pythonFloat(raw)) + " percent"
	}
	if strings.HasPrefix(raw, "-") && strings.TrimLeft(ds, "0") != "" {
		return "negative " + intWords(ds) + " percent"
	}
	return intWords(ds) + " percent"
}

// pythonFloat is Python's str(float(raw)) for a decimal, or raw itself where
// Python would use exponent form.
func pythonFloat(raw string) string {
	neg := strings.HasPrefix(raw, "-")
	intPart, decPart, _ := strings.Cut(strings.TrimPrefix(raw, "-"), ".")
	f, err := strconv.ParseFloat(digits(intPart)+"."+digits(decPart), 64)
	if err != nil {
		return raw
	}
	// Shortest round-trip digits and exponent, as Python's repr finds them.
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mant, expStr, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(expStr)
	ds := strings.Replace(mant, ".", "", 1)
	if exp < -4 || exp >= 16 {
		return raw
	}
	var out string
	if exp >= 0 {
		for len(ds) < exp+1 {
			ds += "0"
		}
		out = ds[:exp+1] + "." + ds[exp+1:]
		if strings.HasSuffix(out, ".") {
			out += "0"
		}
	} else {
		out = "0." + strings.Repeat("0", -exp-1) + ds
	}
	if neg {
		out = "-" + out
	}
	return out
}

// unitWords spells out the unit after a number, singular after 1. Python's
// normalize_text leaves "3 GB" as "three GB"; this ports TextPreprocessor's
// expand_units, with two differences: the number is left for the number
// rules, so "5-10 km" still reads as a range and "1.50 km" keeps its zero,
// and a number right after a word character ("x5kg") is left alone, as the
// number rule leaves it.
func unitWords(m []string) string {
	forms, ok := units[strings.ToLower(strings.ReplaceAll(m[2], "μ", "µ"))]
	if !ok {
		return m[0]
	}
	if isOne(digits(m[1])) {
		return m[1] + " " + forms.one
	}
	return m[1] + " " + forms.many
}

// spacesFrom returns the ends of the runs of Python whitespace starting at
// i, shortest first: i itself, then after each space.
func spacesFrom(s string, i int) []int {
	ends := []int{i}
	for j, r := range s[i:] {
		if !isSpace(r) {
			break
		}
		ends = append(ends, i+j+utf8.RuneLen(r))
	}
	return ends
}

// alnumAt reports whether the character at byte i matches [a-zA-Z\d].
func alnumAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	c := s[i]
	if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return digits(string(r)) != ""
}

// lastRuneLen is the length in bytes of s's last character.
func lastRuneLen(s string) int {
	_, n := utf8.DecodeLastRuneInString(s)
	return n
}
