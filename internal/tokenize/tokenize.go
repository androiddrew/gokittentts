// Package tokenize turns espeak-ng IPA phonemes into KittenTTS token ids.
package tokenize

import (
	"regexp"
	"strings"
)

// The symbol table from KittenTTS onnx_model.py TextCleaner, in order. Copied
// byte for byte: the IPA string contains U+0329 and straight apostrophes, and
// the punctuation string repeats '"', so later duplicates overwrite earlier ids.
const (
	pad         = "$"
	punctuation = ";:,.!?¡¿—…\"«»\"\" "
	letters     = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	lettersIPA  = "ɑɐɒæɓʙβɔɕçɗɖðʤəɘɚɛɜɝɞɟʄɡɠɢʛɦɧħɥʜɨɪʝɭɬɫɮʟɱɯɰŋɳɲɴøɵɸθœɶʘɹɺɾɻʀʁɽʂʃʈʧʉʊʋⱱʌɣɤʍχʎʏʑʐʒʔʡʕʢǀǁǂǃˈˌːˑʼʴʰʱʲʷˠˤ˞↓↑→↗↘'\u0329'ᵻ"
)

var symbolIDs = func() map[rune]int64 {
	m := map[rune]int64{}
	var i int64
	for _, r := range pad + punctuation + letters + lettersIPA {
		m[r] = i
		i++
	}
	return m
}()

// Python's re.findall(r"\w+|[^\w\s]"). Go's \w is ASCII-only, but IPA stress
// and length marks are Unicode letters that Python's \w matches.
var tokenRe = regexp.MustCompile(`[\p{L}\p{N}_]+|[^\p{L}\p{N}_\s]`)

// endID is the id of "…", appended before the final pad as Python does.
const endID = 10

// IDs re-tokenizes phonemes the way KittenTTS does, maps each rune to its
// symbol id, silently dropping unknown runes, and wraps the result as
// [0] + ids + [endID, 0].
func IDs(phonemes string) []int64 {
	joined := strings.Join(tokenRe.FindAllString(phonemes, -1), " ")
	ids := []int64{0}
	for _, r := range joined {
		if id, ok := symbolIDs[r]; ok {
			ids = append(ids, id)
		}
	}
	return append(ids, endID, 0)
}
