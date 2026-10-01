package normalize

import (
	"regexp"
	"strings"
)

var (
	// \b[vV]?\d+(?:\.\d+){2,}\b
	reDottedVersion = regexp.MustCompile(`[vV]?\p{Nd}+(?:\.\p{Nd}+){2,}`)
	// \b([a-zA-Z][a-zA-Z0-9]*)-(\d[\d.]*)(?=[^\d.]|$); the greedy [\d.]*
	// always satisfies the lookahead.
	reModelVersion = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9]*)-(\p{Nd}[\p{Nd}.]*)`)
	reVersionParts = regexp.MustCompile(`^\p{Nd}+(?:\.\p{Nd}+)*`)
)

// versionWords is Python's _version_to_words: "v1.2.3" is "v one point two
// point three".
func versionWords(raw string) string {
	prefix := ""
	if raw[0] == 'v' || raw[0] == 'V' {
		prefix, raw = "v ", raw[1:]
	}
	parts := strings.Split(raw, ".")
	for i, p := range parts {
		parts[i] = intWords(digits(p))
	}
	return prefix + strings.Join(parts, " point ")
}

// dottedVersion finishes a dotted version match. Python's \b at the end can
// make it give back trailing parts: "1.2.3.4x" is "1.2.3" then ".4x".
func dottedVersion(s string, loc []int) (int, string, bool) {
	if !wordBefore(s, loc) {
		return 0, "", false
	}
	for end := loc[1]; ; {
		if !wordAt(s, end) {
			return end, versionWords(s[loc[0]:end]), true
		}
		dot := strings.LastIndex(s[loc[0]:end], ".")
		if strings.Count(s[loc[0]:loc[0]+dot], ".") < 2 {
			return 0, "", false
		}
		end = loc[0] + dot
	}
}

// modelVersion reads "GPT-4" as "GPT four".
//
// Deviation: Python raises on a version with a trailing or doubled dot
// ("GPT-4." at the end of a sentence). Only the leading digits-and-dots
// version is read; the rest stays in the text.
func modelVersion(m []string) string {
	v := reVersionParts.FindString(m[2])
	return m[1] + " " + versionWords(v) + m[2][len(v):]
}
