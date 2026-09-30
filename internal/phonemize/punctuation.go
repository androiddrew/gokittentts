// Package phonemize turns text into espeak-ng IPA the way Python phonemizer's
// EspeakBackend(preserve_punctuation=True, with_stress=True) does.
package phonemize

import (
	"strings"
	"unicode"
)

// Phonemizer turns text into IPA phonemes.
type Phonemizer interface {
	Phonemize(text string) (string, error)
}

// phonemizer's default punctuation marks.
const marks = `;:,.!?¡¿—…"«»“”(){}[]`

type markPosition int

const (
	markBegin markPosition = iota
	markInner
	markEnd
	markAlone
)

type mark struct {
	text     string
	position markPosition
}

// PreservePunctuation wraps a backend that is given only the text between
// punctuation runs, and puts the marks back into its output, as phonemizer's
// Punctuation.preserve and restore do.
func PreservePunctuation(backend Phonemizer) Phonemizer {
	return punctuated{backend}
}

type punctuated struct{ backend Phonemizer }

func (p punctuated) Phonemize(text string) (string, error) {
	parts, found := splitMarks(text)
	phonemized := make([]string, 0, len(parts))
	for _, part := range parts {
		ph, err := p.backend.Phonemize(part)
		if err != nil {
			return "", err
		}
		phonemized = append(phonemized, ph)
	}
	return restoreMarks(phonemized, found), nil
}

// isMark reports whether the rune at i is punctuation. "." and "," between two
// ASCII digits are decimal or thousands separators, not punctuation
// (phonemizer >= 3.4.0), so "3.5" reaches espeak whole.
func isMark(rs []rune, i int) bool {
	r := rs[i]
	if !strings.ContainsRune(marks, r) {
		return false
	}
	if r == '.' || r == ',' {
		return i == 0 || i == len(rs)-1 || !isASCIIDigit(rs[i-1]) || !isASCIIDigit(rs[i+1])
	}
	return true
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

// splitMarks is phonemizer's _preserve_line, with the regex
// (\s*(?:marks)+\s*)+ written as a scanner, since RE2 has no lookarounds for
// the decimal rule. A match is a maximal run of whitespace and marks that
// holds at least one mark. The text is cut at the match spans, and empty
// parts are dropped.
func splitMarks(line string) ([]string, []mark) {
	rs := []rune(line)
	type span struct{ start, end int }
	var spans []span
	for i := 0; i < len(rs); {
		if !unicode.IsSpace(rs[i]) && !isMark(rs, i) {
			i++
			continue
		}
		start, hasMark := i, false
		for i < len(rs) && (unicode.IsSpace(rs[i]) || isMark(rs, i)) {
			hasMark = hasMark || isMark(rs, i)
			i++
		}
		if hasMark {
			spans = append(spans, span{start, i})
		}
	}
	if len(spans) == 0 {
		return nonEmpty([]string{line}), nil
	}
	if len(spans) == 1 && spans[0].start == 0 && spans[0].end == len(rs) {
		return nil, []mark{{line, markAlone}}
	}

	var parts []string
	var found []mark
	cursor := 0
	for i, s := range spans {
		pos := markInner
		if i == 0 && s.start == 0 {
			pos = markBegin
		} else if i == len(spans)-1 && s.end == len(rs) {
			pos = markEnd
		}
		found = append(found, mark{string(rs[s.start:s.end]), pos})
		parts = append(parts, string(rs[cursor:s.start]))
		cursor = s.end
	}
	parts = append(parts, string(rs[cursor:]))
	return nonEmpty(parts), found
}

func nonEmpty(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// restoreMarks is phonemizer's Punctuation.restore for a single line, with
// " " as the word separator and strip=False.
func restoreMarks(text []string, found []mark) string {
	var out []string
	markLine := true // every mark belongs to line 0 until an end mark closes it
	for len(text) > 0 || len(found) > 0 {
		switch {
		case len(found) == 0:
			for _, line := range text {
				if !strings.HasSuffix(line, " ") {
					line += " "
				}
				out = append(out, line)
			}
			text = nil
		case len(text) == 0:
			var sb strings.Builder
			for _, m := range found {
				sb.WriteString(m.text)
			}
			out = append(out, sb.String())
			found = nil
		case !markLine:
			out = append(out, text[0])
			text = text[1:]
		default:
			m := found[0]
			found = found[1:]
			text[0] = strings.TrimSuffix(text[0], " ")
			switch m.position {
			case markBegin:
				text[0] = m.text + text[0]
			case markEnd:
				out = append(out, text[0]+m.text+trailingSeparator(m.text))
				text = text[1:]
				markLine = false
			case markAlone:
				out = append(out, m.text+trailingSeparator(m.text))
				markLine = false
			case markInner:
				if len(text) == 1 {
					text[0] += m.text
				} else {
					text[1] = text[0] + m.text + text[1]
					text = text[1:]
				}
			}
		}
	}
	if len(out) == 0 {
		return ""
	}
	return out[0]
}

func trailingSeparator(mark string) string {
	if strings.HasSuffix(mark, " ") {
		return ""
	}
	return " "
}
