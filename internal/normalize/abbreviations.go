package normalize

import (
	"regexp"
	"strings"
)

var (
	// \bet\s+al\.
	reEtAl = regexp.MustCompile(`(?i)et[` + space + `]+al\.`)
	// \b(Dr|Prof|Mr|Mrs|Ms|Fig|Figs|pp|p|ch|sec)\.
	reTitle = regexp.MustCompile(`(?i)(Dr|Prof|Mr|Mrs|Ms|Fig|Figs|pp|p|ch|sec)\.`)

	abbreviations = map[string]string{
		"dr": "Doctor", "prof": "Professor", "mr": "Mister", "mrs": "Misses",
		"ms": "Ms", "fig": "Figure", "figs": "Figures", "pp": "pages",
		"p": "page", "ch": "chapter", "sec": "section",
	}
)

// titleBoundary checks a title abbreviation match.
//
// Deviation: "p." is not "page" when it starts "p.m." or "p.m"; Python reads
// "5 p.m." as "five pagem.".
func titleBoundary(s string, loc []int) bool {
	if !wordBefore(s, loc) {
		return false
	}
	pm := strings.EqualFold(s[loc[2]:loc[3]], "p") && loc[1] < len(s) &&
		(s[loc[1]] == 'm' || s[loc[1]] == 'M') && !wordAt(s, loc[1]+1)
	return !pm
}

// titleWords expands a title, case-folded as monthName is.
func titleWords(m []string) string {
	if w, ok := lookupFold(abbreviations, m[1]); ok {
		return w
	}
	return m[0]
}
