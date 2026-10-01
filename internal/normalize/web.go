package normalize

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// <[^>]+>
	reHTML = regexp.MustCompile(`</?[a-zA-Z!][^<>]*>`)
	// https?://\S+|www\.\S+
	reURL = regexp.MustCompile(`(?i)https?://[^` + space + `]+|www\.[^` + space + `]+`)
	// \b[\w.+-]+@[\w-]+\.[a-z]{2,}\b
	reEmail      = regexp.MustCompile(`(?i)[` + word + `.+-]+@[` + word + `-]+(?:\.[` + word + `-]+)*\.[a-z]{2,}`)
	reEmailWhole = regexp.MustCompile(`(?i)^[` + word + `.+-]+@[` + word + `-]+(?:\.[` + word + `-]+)*\.[a-z]{2,}$`)
	// ^https?://
	reURLScheme = regexp.MustCompile(`(?i)^https?://`)
)

// Deviations from Python's URL and email patterns:
//   - The HTML pattern needs a letter, "/" or "!" right after "<", so "3 < 5
//     and 6 > 2" is not taken for a tag and deleted ("x<y and y>z" still is).
//   - URLs match case-insensitively; Python leaves "HTTPS://EXAMPLE.COM".
//   - Punctuation ending a URL ends the sentence instead of being spelled:
//     Python reads "see https://example.com." as "... dot c o m dot".
//     Closing brackets and quotes there are dropped.
//   - An email domain can have several dots; Python stops at the first
//     ("a at b dot c o.uk").
//   - "www." is spelled like the rest, "w w w dot"; Python first rewrites it
//     to "www dot " and then spells that ("w w w d o t").
//   - A non-ASCII digit is read; Python raises KeyError.

// urlWords is Python's _url_to_words, returning the punctuation that ends
// the URL after its words.
func urlWords(raw string) string {
	trimmed := strings.TrimRight(raw, `.,;:!?)]}'"`)
	rest := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`)]}'"`, r) {
			return -1
		}
		return r
	}, raw[len(trimmed):])
	return spell(reURLScheme.ReplaceAllString(trimmed, "")) + rest
}

// emailWords is Python's _email_to_words.
func emailWords(raw string) string {
	local, domain, _ := strings.Cut(raw, "@")
	return spell(local) + " at " + spell(domain)
}

// email finishes an email match, checking Python's \b at both ends: its
// first character may be "." or "+", which aren't word characters. Where the
// end fails, Python's pattern would have stopped at an earlier dot of the
// domain ("bob@a.org.uk1" is "bob@a.org" then ".uk1"), so shorter domains
// are tried.
func email(s string, loc []int) (int, string, bool) {
	if wordAt(s, loc[0]-1) == wordAt(s, loc[0]) {
		return 0, "", false
	}
	for end := loc[1]; end > loc[0]; end = loc[0] + strings.LastIndex(s[loc[0]:end], ".") {
		if !wordAt(s, end) && reEmailWhole.MatchString(s[loc[0]:end]) {
			return end, emailWords(s[loc[0]:end]), true
		}
		if !strings.Contains(s[loc[0]:end], ".") {
			break
		}
	}
	return 0, "", false
}

// spell is Python's _spell_characters: letters and digits one by one, a few
// symbols as words, everything else dropped.
func spell(text string) string {
	var parts []string
	for _, r := range text {
		switch {
		case unicode.IsLetter(r):
			parts = append(parts, string(unicode.ToLower(r)))
		case unicode.Is(unicode.Nd, r):
			parts = append(parts, digitWords[digitValue(r)])
		default:
			if w, ok := symbolWords[r]; ok {
				parts = append(parts, w)
			}
		}
	}
	return strings.Join(parts, " ")
}

var symbolWords = map[rune]string{
	'.': "dot", '-': "dash", '_': "underscore", '@': "at", '/': "slash",
	'?': "question mark", '&': "and", '=': "equals",
}
