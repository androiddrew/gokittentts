package markdown

import (
	"strings"
	"unicode"
)

// emojiComponents are the code points that build emoji out of others:
// variation selectors, the zero-width joiner, skin-tone modifiers, regional
// indicators (flags), tags (subdivision flags) and the keycap mark. A
// keycap's digit stays, so "1️⃣" reads as "1".
var emojiComponents = &unicode.RangeTable{
	R16: []unicode.Range16{
		{0x200d, 0x200d, 1}, // zero-width joiner
		{0x20e3, 0x20e3, 1}, // combining enclosing keycap
		{0xfe0e, 0xfe0f, 1}, // text and emoji variation selectors
	},
	R32: []unicode.Range32{
		{0x1f1e6, 0x1f1ff, 1}, // regional indicators
		{0x1f3fb, 0x1f3ff, 1}, // skin-tone modifiers
		{0xe0020, 0xe007f, 1}, // tags
	},
}

// removeEmoji deletes Extended_Pictographic code points and emoji
// components from s.
func removeEmoji(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.In(r, extendedPictographic, emojiComponents) {
			return -1
		}
		return r
	}, s)
}
