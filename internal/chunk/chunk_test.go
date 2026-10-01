package chunk_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/androiddrew/gokittentts/internal/chunk"
)

// Expected values come from KittenTTS preprocess.chunk_text
// (scripts/preprocess_ref.py); the first two are its own tests.
func TestSplit(t *testing.T) {
	var words []string
	for i := range 120 {
		words = append(words, fmt.Sprintf("word%03d", i))
	}
	longWords := strings.Join(words, " ")
	// 50 seven-character words and their spaces are 399 characters; the
	// comma ensure_punctuation appends makes 400.
	longChunks := []string{
		strings.Join(words[:50], " ") + ",",
		strings.Join(words[50:100], " ") + ",",
		strings.Join(words[100:], " ") + ",",
	}

	// 15 + 108 = 123 characters, over FirstMaxLen.
	longFirstHead := "When it rains,"
	longFirstTail := strings.Repeat("it pours ", 12) + "on the town."
	longFirst := longFirstHead + " " + longFirstTail + " Done."
	// 119 characters and a period.
	first120 := "So, " + strings.Repeat("x", 115) + "."

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"ported: abbreviations, money and p.m.", "Dr. Rivera paid $12.50 at 3:05 p.m.", []string{"Dr. Rivera paid $12.50 at 3:05 p.m."}},
		{"ported: et al. and pp.", "Smith et al. 2024, pp. 31-35", []string{"Smith et al. 2024, pp. 31-35,"}},
		{"empty", "", nil},
		{"whitespace only", "  \n\t ", nil},
		{"missing terminal punctuation gets a comma", "Hello world", []string{"Hello world,"}},
		{"keeps ;:, endings", "Wait; first:", []string{"Wait; first:"}},
		{"splits sentences", "Hello there. How are you? Fine!", []string{"Hello there.", "How are you?", "Fine!"}},
		{"runs of marks stay with the sentence", "Really?! No way!!! Are you sure??", []string{"Really?!", "No way!!!", "Are you sure??"}},
		{"ellipsis", "Wait... what happened?", []string{"Wait...", "what happened?"}},
		{"decimal is not a boundary", "It costs 3.5 dollars. Cheap.", []string{"It costs 3.5 dollars.", "Cheap."}},
		{"period without following space is not a boundary", "See example.com for more.", []string{"See example.com for more."}},
		{"a.m. mid-sentence", "Meet at 10 a.m. on Monday.", []string{"Meet at 10 a.m. on Monday."}},
		{"p.m. before a capital ends the sentence", "The meeting ends at 5 p.m. Bring your notes.", []string{"The meeting ends at 5 p.m.", "Bring your notes."}},
		{"month abbreviation", "Due Jan. 5 at noon. Thanks.", []string{"Due Jan. 5 at noon.", "Thanks."}},
		{"400-character split at word boundaries", longWords, longChunks},
		{"sentence of exactly 400 characters stays whole", strings.Repeat("x", 399) + ".", []string{strings.Repeat("x", 399) + "."}},
		{"non-ASCII counts characters, not bytes", strings.Repeat("é", 399) + ".", []string{strings.Repeat("é", 399) + "."}},
		{"long sentence then short", longWords + ". Short one.", []string{
			longChunks[0], longChunks[1], strings.Join(words[100:], " ") + ".", "Short one.",
		}},

		// Deviation from Python: a first chunk over FirstMaxLen characters is
		// split at its first comma, so the first model run is short.
		{"deviation: long first chunk splits at its first comma", longFirst, []string{longFirstHead, longFirstTail, "Done."}},
		{"deviation: first chunk of exactly 120 characters stays whole", first120, []string{first120}},
		{"deviation: only the first chunk splits", "Short. " + longFirst, []string{"Short.", longFirst[:len(longFirst)-len(" Done.")], "Done."}},
		{"deviation: long first chunk without a comma stays whole", strings.Repeat("a ", 70) + "end.", []string{strings.Repeat("a ", 70) + "end."}},
		{"deviation: digit-grouping commas don't split", strings.Repeat("a ", 60) + "1,000 cats, and dogs.", []string{strings.Repeat("a ", 60) + "1,000 cats,", "and dogs."}},
		{"deviation: a comma before a newline splits", "When it rains,\n" + longFirstTail, []string{longFirstHead, longFirstTail}},
		{"deviation: a trailing comma doesn't split", strings.Repeat("a ", 70) + "end", []string{strings.Repeat("a ", 70) + "end,"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chunk.Split(c.in); !slices.Equal(got, c.want) {
				t.Errorf("Split(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}
