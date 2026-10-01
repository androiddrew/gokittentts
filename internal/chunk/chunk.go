// Package chunk splits text into the sentence-sized chunks the model is run
// on. It is a port of KittenTTS preprocess.py's chunk_text,
// _is_sentence_boundary and ensure_punctuation, with one recorded deviation:
// a first chunk over FirstMaxLen characters is split at its first comma, so
// the first model run is short and streamed audio starts sooner.
package chunk

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxLen is the longest chunk, in characters, before a sentence is split at
// word boundaries.
const MaxLen = 400

// FirstMaxLen is the longest first chunk, in characters, before it is split
// at its first comma. Python has no such rule.
const FirstMaxLen = 120

var nonBoundaryAbbreviations = []string{
	"dr", "prof", "mr", "mrs", "ms", "fig", "figs", "pp", "p", "ch", "sec",
	"jan", "feb", "mar", "apr", "jun", "jul", "aug", "sep", "sept", "oct",
	"nov", "dec", "al",
}

// Split cuts text into sentences at ".", "!" or "?" followed by whitespace or
// the end of the text, skipping decimals, common abbreviations and a.m./p.m.
// Sentences longer than MaxLen characters are split at word boundaries. Every
// chunk ends in one of .!?,;: (a "," is appended if needed). A first chunk
// longer than FirstMaxLen is split after its first comma that is followed by
// whitespace (so "1,000" stays whole).
func Split(text string) []string {
	rs := []rune(text)
	var sentences []string
	start := 0
	for i := range rs {
		if isSentenceBoundary(rs, i) {
			sentences = append(sentences, string(rs[start:i+1]))
			start = i + 1
		}
	}
	if start < len(rs) {
		sentences = append(sentences, string(rs[start:]))
	}

	var chunks []string
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		if utf8.RuneCountInString(sentence) <= MaxLen {
			chunks = append(chunks, ensurePunctuation(sentence))
			continue
		}
		current, currentLen := "", 0
		for word := range strings.FieldsSeq(sentence) {
			wordLen := utf8.RuneCountInString(word)
			if currentLen+wordLen+1 <= MaxLen {
				if current == "" {
					current, currentLen = word, wordLen
				} else {
					current, currentLen = current+" "+word, currentLen+1+wordLen
				}
				continue
			}
			if current != "" {
				chunks = append(chunks, ensurePunctuation(current))
			}
			current, currentLen = word, wordLen
		}
		if current != "" {
			chunks = append(chunks, ensurePunctuation(current))
		}
	}
	if len(chunks) > 0 && utf8.RuneCountInString(chunks[0]) > FirstMaxLen {
		first := chunks[0]
		for i, r := range first {
			if r == ',' && i+1 < len(first) {
				if next, _ := utf8.DecodeRuneInString(first[i+1:]); unicode.IsSpace(next) {
					chunks = slices.Insert(chunks[1:], 0, first[:i+1], strings.TrimSpace(first[i+1:]))
					break
				}
			}
		}
	}
	return chunks
}

func ensurePunctuation(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	last, _ := utf8.DecodeLastRuneInString(text)
	if !strings.ContainsRune(".!?,;:", last) {
		text += ","
	}
	return text
}

func isSentenceBoundary(rs []rune, i int) bool {
	c := rs[i]
	if c != '.' && c != '!' && c != '?' {
		return false
	}
	if c == '.' {
		if i > 0 && i < len(rs)-1 && unicode.IsDigit(rs[i-1]) && unicode.IsDigit(rs[i+1]) {
			return false
		}
		token := strings.ToLower(trailingASCIILetters(rs[:i]))
		if slices.Contains(nonBoundaryAbbreviations, token) {
			return false
		}
		if (token == "a" || token == "p") && i+1 < len(rs) && unicode.ToLower(rs[i+1]) == 'm' {
			return false
		}
		if token == "m" && endsWithAMPM(rs[:i]) {
			next := strings.TrimSpace(string(rs[i+1:]))
			if next == "" {
				return true
			}
			first, _ := utf8.DecodeRuneInString(next)
			return unicode.IsUpper(first)
		}
	}
	if i+1 == len(rs) {
		return true
	}
	return unicode.IsSpace(rs[i+1])
}

func trailingASCIILetters(rs []rune) string {
	j := len(rs)
	for j > 0 && (rs[j-1] >= 'a' && rs[j-1] <= 'z' || rs[j-1] >= 'A' && rs[j-1] <= 'Z') {
		j--
	}
	return string(rs[j:])
}

// endsWithAMPM is Python's re.search(r"\b[ap]\.m$", before, re.IGNORECASE).
func endsWithAMPM(before []rune) bool {
	n := len(before)
	if n < 3 || before[n-2] != '.' || unicode.ToLower(before[n-1]) != 'm' {
		return false
	}
	if ap := unicode.ToLower(before[n-3]); ap != 'a' && ap != 'p' {
		return false
	}
	return n == 3 || !isWordRune(before[n-4])
}

// isWordRune is Python's Unicode \w: str.isalnum() or "_".
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.Is(unicode.N, r)
}
