// Package normalize rewrites text the way it should be read aloud: numbers,
// years, ordinals, dates, times, money, percents, units and versions become
// words. It is a port of KittenTTS
// preprocess.py's normalize_text (read-aloud mode, scripts/preprocess_ref.py),
// without its span tracking, plus the deviations recorded in
// testdata/normalize_overrides.yaml.
//
// The substitutions run in Python's order. Units and scale suffixes ("3 GB",
// "7B"), which normalize_text lacks, come from TextPreprocessor and run after
// percents. Not ported yet: HTML, URLs and emails, et al. and titles.
//
// Python's regexes are Unicode-aware and some use lookarounds, which RE2
// lacks. Here \w is [\p{L}\p{N}_], \d is \p{Nd} and \s is Python's
// whitespace, and each lookaround or \b is a Go check on the match.
package normalize

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Python's \s: str.isspace.
const space = `\t-\r\x1c-\x1f\x{85}\p{Z}`

const monthNames = `(Jan\.?|January|Feb\.?|February|Mar\.?|March|Apr\.?|April|May|` +
	`Jun\.?|June|Jul\.?|July|Aug\.?|August|Sep\.?|Sept\.?|September|` +
	`Oct\.?|October|Nov\.?|November|Dec\.?|December)`

var (
	// \b(month)\s+(\d{1,2})(?:st|nd|rd|th)?(?:,)?\s+(\d{4})\b
	reMonthDayYear = regexp.MustCompile(`(?i)` + monthNames + `[` + space + `]+(\p{Nd}{1,2})(?:st|nd|rd|th)?,?[` + space + `]+(\p{Nd}{4})`)
	// \b(month)\s+(\d{4})\b
	reMonthYear = regexp.MustCompile(`(?i)` + monthNames + `[` + space + `]+(\p{Nd}{4})`)
	// \b(\d{1,2}):(\d{2}), the rest of the time is matched by hand.
	reClock   = regexp.MustCompile(`(\p{Nd}{1,2}):(\p{Nd}{2})`)
	reSeconds = regexp.MustCompile(`^:\p{Nd}{2}`)
	// \b(\d+)(st|nd|rd|th)\b
	reOrdinal = regexp.MustCompile(`(\p{Nd}+)(?i:st|nd|rd|th)`)
	// (?<!\w)(\d+)-(\d+)(?!\w)
	reRange = regexp.MustCompile(`(\p{Nd}+)-(\p{Nd}+)`)
	// (?<![a-zA-Z])-?[\d,]+(?:\.\d+)?
	reNumber = regexp.MustCompile(`-?[\p{Nd},]+(?:\.\p{Nd}+)?`)

	rePunctuation = regexp.MustCompile(`[^\p{L}\p{N}_` + space + `.,?!;:\-\x{2014}\x{2013}\x{2026}]`)
	reSpaces      = regexp.MustCompile(`[` + space + `]+`)
)

// Text normalizes s for reading aloud.
func Text(s string) string {
	s = norm.NFC.String(s)
	s = sub(s, reMonthDayYear, wordBefore, wordAfter, func(m []string) string {
		return monthName(m[1]) + " " + ordinalWords(digits(m[2])) + ", " + yearWords(atoi(digits(m[3])))
	})
	s = sub(s, reMonthYear, wordBefore, wordAfter, func(m []string) string {
		return monthName(m[1]) + " " + yearWords(atoi(digits(m[2])))
	})
	s = times(s)
	s = currency(s)
	s = percent(s)
	s = sub(s, reUnit, wordBefore, wordAfter, unitWords)
	s = sub(s, reScaleSuffix, wordBefore, wordAfter, func(m []string) string {
		return m[1] + " " + scaleWords[m[2]]
	})
	s = sub(s, reOrdinal, wordBefore, wordAfter, func(m []string) string {
		return ordinalWords(digits(m[1]))
	})
	s = scan(s, reDottedVersion, dottedVersion)
	s = sub(s, reRange, wordBefore, wordAfter, rangeWords)
	s = sub(s, reModelVersion, wordBefore, nil, modelVersion)
	s = sub(s, reNumber, asciiLetterBefore, nil, numberWords)
	s = rePunctuation.ReplaceAllString(s, " ")
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.TrimFunc(s, isSpace)
}

// monthName is Python's _month_name. Go's (?i) folds "ſ" to "s" as Python's
// does, but Python's lower() leaves it, so "ſept" raises KeyError there.
//
// Deviation: any spelling the pattern matched is looked up case-folded.
func monthName(raw string) string {
	raw = strings.TrimRight(raw, ".")
	for k, v := range months {
		if strings.EqualFold(k, raw) {
			return v
		}
	}
	return raw
}

// rangeWords reads "2020-2024" as two years and anything else as two numbers.
func rangeWords(m []string) string {
	lo, hi := digits(m[1]), digits(m[2])
	if isYear(lo) && isYear(hi) {
		return numberOrYearWords(lo) + " to " + numberOrYearWords(hi)
	}
	return intWords(lo) + " to " + intWords(hi)
}

// numberWords is Python's _number_or_year_to_words.
//
// Deviation: Python's number pattern takes the commas after a number ("In
// 2024, the" loses its comma) and raises ValueError on a match that is only
// commas ("(404), not"). Trailing commas are left in the text, and a match
// with no digits is left alone.
func numberWords(m []string) string {
	raw := strings.TrimRight(m[0], ",")
	commas := m[0][len(raw):]
	cleaned := strings.ReplaceAll(raw, ",", "")
	ds := digits(cleaned)
	if ds == "" {
		return m[0]
	}
	if strings.Contains(cleaned, ".") {
		return floatWords(cleaned) + commas
	}
	if !strings.HasPrefix(cleaned, "-") || strings.TrimLeft(ds, "0") == "" {
		return numberOrYearWords(ds) + commas
	}
	return "negative " + intWords(ds) + commas
}

// times is the read-aloud time substitution:
//
//	\b(\d{1,2}):(\d{2})(?::(\d{2}))?\s*(a\.?m\.?|p\.?m\.?)?\b
//
// The \b at the end can make Python back off the seconds, the spaces or the
// suffix, so the tail is matched by hand, trying the choices in the order
// Python's backtracking does.
//
// Deviation: when there is no suffix, Python still takes the spaces after
// the time, so "3:00 today" becomes "threetoday". The spaces are kept.
func times(s string) string {
	return scan(s, reClock, func(s string, loc []int) (int, string, bool) {
		if !wordBefore(s, loc) {
			return 0, "", false
		}
		return timeTail(s, loc)
	})
}

// timeTail finishes a time whose hours and minutes are at loc, returning the
// end of the match and its words.
func timeTail(s string, loc []int) (end int, words string, ok bool) {
	hour, mins := atoi(digits(s[loc[2]:loc[3]])), atoi(digits(s[loc[4]:loc[5]]))
	afterMins := []int{loc[1]}
	if sec := reSeconds.FindString(s[loc[1]:]); sec != "" {
		afterMins = []int{loc[1] + len(sec), loc[1]}
	}
	for _, e := range afterMins {
		spaces := []int{e}
		for i, r := range s[e:] {
			if !isSpace(r) {
				break
			}
			spaces = append(spaces, e+i+utf8.RuneLen(r))
		}
		for i := len(spaces) - 1; i >= 0; i-- {
			at := spaces[i]
			for _, suffix := range suffixes(s[at:]) {
				end := at + len(suffix)
				if wordAt(s, end-1) == wordAt(s, end) {
					continue
				}
				seconds := ""
				if e > loc[1] {
					seconds = s[loc[1]+1 : e]
				}
				return end, timeWords(hour, mins, seconds, suffix, s[e:at]), true
			}
		}
	}
	return 0, "", false
}

// suffixes are the am/pm suffixes at the start of s, in the order Python
// tries them, ending with no suffix.
func suffixes(s string) []string {
	var out []string
	if len(s) > 0 && strings.ContainsRune("aApP", rune(s[0])) {
		for _, form := range []string{"x.m.", "x.m", "xm.", "xm"} {
			if len(s) >= len(form) && strings.EqualFold(s[1:len(form)], form[1:]) {
				out = append(out, s[:len(form)])
			}
		}
	}
	return append(out, "")
}

// timeWords is Python's _replace_read_aloud_time.
func timeWords(hour, mins int, seconds, suffix, spaces string) string {
	if suffix != "" && hour > 12 {
		hour -= 12
	}
	w := numberToWords(hour)
	switch {
	case mins == 0:
	case mins < 10:
		w += " oh " + numberToWords(mins)
	default:
		w += " " + numberToWords(mins)
	}
	if seconds != "" {
		w += " and " + numberToWords(atoi(digits(seconds))) + " seconds"
	}
	switch {
	case suffix == "":
		return w + spaces
	case strings.EqualFold(suffix[:1], "a"):
		return w + " a m"
	default:
		return w + " p m"
	}
}

// sub replaces each match of re that before and after accept (either may be
// nil) with repl of its submatches.
func sub(s string, re *regexp.Regexp, before, after func(s string, loc []int) bool, repl func(m []string) string) string {
	return scan(s, re, func(s string, loc []int) (int, string, bool) {
		if (before != nil && !before(s, loc)) || (after != nil && !after(s, loc)) {
			return 0, "", false
		}
		m := make([]string, len(loc)/2)
		for i := range m {
			if loc[2*i] >= 0 {
				m[i] = s[loc[2*i]:loc[2*i+1]]
			}
		}
		return loc[1], repl(m), true
	})
}

// scan finds each match of re and lets try decide where the match really
// ends and what replaces it. Like Python's finditer after a failed
// lookaround, a rejected match is retried one character on.
func scan(s string, re *regexp.Regexp, try func(s string, loc []int) (end int, repl string, ok bool)) string {
	var b strings.Builder
	last, pos := 0, 0
	for pos < len(s) {
		loc := re.FindStringSubmatchIndex(s[pos:])
		if loc == nil {
			break
		}
		for i := range loc {
			if loc[i] >= 0 {
				loc[i] += pos
			}
		}
		end, repl, ok := try(s, loc)
		if !ok {
			pos = loc[0] + runeLen(s, loc[0])
			continue
		}
		b.WriteString(s[last:loc[0]])
		b.WriteString(repl)
		last, pos = end, end
	}
	b.WriteString(s[last:])
	return b.String()
}

// wordBefore is \b or (?<!\w) at the start of a match that starts with a
// word character: the character before is not one.
func wordBefore(s string, loc []int) bool { return !wordAt(s, loc[0]-1) }

// wordAfter is \b or (?!\w) at the end of a match that ends with a word
// character: the character after is not one.
func wordAfter(s string, loc []int) bool { return !wordAt(s, loc[1]) }

// asciiLetterBefore is (?<![a-zA-Z]).
func asciiLetterBefore(s string, loc []int) bool {
	if loc[0] == 0 {
		return true
	}
	c := s[loc[0]-1]
	return !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z')
}

// wordAt reports whether the character at or ending at byte i is a Python \w
// character. i is the start of a character, or one byte before it for the
// character before.
func wordAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	var r rune
	if utf8.RuneStart(s[i]) {
		r, _ = utf8.DecodeRuneInString(s[i:])
	} else {
		r, _ = utf8.DecodeLastRuneInString(s[:i+1])
	}
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
}

// isSpace is Python's str.isspace.
func isSpace(r rune) bool {
	return '\t' <= r && r <= '\r' || 0x1c <= r && r <= 0x1f || r == 0x85 || unicode.In(r, unicode.Z)
}

func runeLen(s string, i int) int {
	if i >= len(s) {
		return 1
	}
	_, n := utf8.DecodeRuneInString(s[i:])
	return n
}
