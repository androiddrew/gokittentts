package markdown_test

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/androiddrew/gokittentts/internal/markdown"
)

// One case per construct in docs/ORIGINAL_SPEC.md section 6.1, plus the
// details around them. Blocks come out separated by a blank line.
func TestToSpeech(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		// Headings
		{"ATX heading", "# Getting started", "Getting started."},
		{"heading with punctuation", "## Are we done?", "Are we done?"},
		{"setext heading", "Title\n=====", "Title."},
		{"heading then text", "# Setup\nInstall it first.", "Setup.\n\nInstall it first."},
		// The chunker splits sentences only at .!?, so a heading or item
		// ending in a colon would run into the next one.
		{"heading ending in a colon", "# Steps:\nDo it.", "Steps.\n\nDo it."},
		{"heading ending in an ellipsis", "# Wait…", "Wait…"},
		{"closing hashes", "## Install ##", "Install."},

		// Paragraphs and block quotes
		{"paragraph", "Hello world.", "Hello world."},
		{"paragraphs", "First one.\n\nSecond one.", "First one.\n\nSecond one."},
		{"soft line break", "line one\nline two", "line one\nline two"},
		{"hard line break", "line one  \nline two", "line one\nline two"},
		{"block quote", "> Quoted text.", "Quoted text."},
		{"nested block quote", "> Outer.\n>\n> > Inner.", "Outer.\n\nInner."},

		// Emphasis
		{"bold, em and strike", "This is **bold**, *em* and ~~gone~~ text.", "This is bold, em and gone text."},
		{"underscores", "Some __strong__ and _em_ words.", "Some strong and em words."},
		{"nested emphasis", "***very*** important", "very important"},

		// Lists
		{"bullet list", "- one\n- two\n- three", "one.\n\ntwo.\n\nthree."},
		{"ordered list", "1. First\n2. Second", "First.\n\nSecond."},
		{"items keep their punctuation", "* Done?\n* Yes!", "Done?\n\nYes!"},
		{"items ending in a colon or semicolon", "- Step one:\n- Step two;", "Step one.\n\nStep two."},
		{"nested list", "- outer\n  - inner", "outer.\n\ninner."},
		{"loose item", "- First paragraph\n\n  Second paragraph\n- Next", "First paragraph.\n\nSecond paragraph.\n\nNext."},
		{"list after text", "You need:\n\n- Go\n- espeak-ng", "You need:\n\nGo.\n\nespeak-ng."},
		{"task list markers stay as text", "- [x] shipped", "[x] shipped."},

		// Links
		{"link", "Read [the docs](https://example.com/docs) first.", "Read the docs first."},
		{"reference link", "See [the guide][g].\n\n[g]: https://example.com/guide", "See the guide."},
		{"emphasis in a link", "[**bold** link](https://x.y)", "bold link"},
		{"autolink", "Go to <https://example.com/a>.", "Go to https://example.com/a."},
		{"email autolink", "Mail <me@example.com>.", "Mail me@example.com."},
		{"bare URL", "Visit https://example.com today.", "Visit https://example.com today."},

		// Images
		{"image", "![a cat on a mat](cat.png)", "a cat on a mat"},
		{"image without alt text", "Look: ![](cat.png)", "Look: "},

		// Code
		{"inline code", "Run `go test ./...` now.", "Run go test ./... now."},
		{"fenced code block", "Before.\n\n```go\nfmt.Println(\"hi\")\n```\n\nAfter.", "Before.\n\nAfter."},
		{"tilde fence", "Before.\n\n~~~\ncode\n~~~", "Before."},
		{"indented code block", "Text.\n\n    code here\n\nMore.", "Text.\n\nMore."},

		// Tables
		{"table", "| Name | Age |\n| --- | --- |\n| Bob | 42 |\n| Ann | 7 |", "Name, Age.\n\nBob, 42.\n\nAnn, 7."},
		{"table with inline markup", "| Tool | Link |\n|---|---|\n| **Go** | [site](https://go.dev) |", "Tool, Link.\n\nGo, site."},
		{"table with an empty cell", "| a | b |\n|---|---|\n| x |  |", "a, b.\n\nx."},

		// Raw HTML
		{"inline HTML", "Some <b>bold</b> text.", "Some bold text."},
		{"line break tag", "Text<br>more<br/>lines", "Text\nmore\nlines"},
		{"HTML block", "<div>\nHi there\n</div>", "Hi there"},
		{"HTML comment", "Text.\n\n<!-- hidden -->", "Text."},
		{"line break tag in an HTML block", "<p>one<br>two</p>", "one\ntwo"},

		// Emoji
		{"emoji", "Great job 🎉!", "Great job !"},
		{"emoji with a variation selector", "I ❤️ Go", "I  Go"},
		{"ZWJ sequence", "Family: 👨‍👩‍👧.", "Family: ."},
		{"skin tone", "Thumbs 👍🏽 up", "Thumbs  up"},
		{"flag", "Made in 🇨🇦.", "Made in ."},
		{"emoji in a heading", "# 🚀 Launch", "Launch."},
		{"emoji ending a list item", "- Shipped 🎉", "Shipped."},

		// Everything else
		{"escapes", `\*not emphasis\* and 1\. not a list`, "*not emphasis* and 1. not a list"},
		{"entities", "Tom &amp; Jerry &eacute; &#36;5", "Tom & Jerry é $5"},
		{"thematic break", "Above.\n\n---\n\nBelow.", "Above.\n\nBelow."},
		{"empty", "", ""},
		{"only code", "```\nx := 1\n```", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := markdown.ToSpeech(c.in); got != c.want {
				t.Errorf("ToSpeech(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

// Emoji the property test scatters through plain text.
var emoji = []string{"😀", "🎉", "👍🏽", "👨‍👩‍👧", "❤️", "🇨🇦", "✅", "🚀"}

// plainText makes text with no markdown syntax: words of letters, digits
// and prose punctuation, single spaces, lines that start with a letter,
// and paragraphs separated by a blank line. Some words are emoji.
func plainText(r *rand.Rand) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZéüñ")
	middle := append(slices.Clone(letters), []rune("0123456789'-/")...)
	const before = `"('$`
	const after = `.,;:?"')%`
	word := func(first bool) string {
		if !first && r.IntN(12) == 0 {
			return emoji[r.IntN(len(emoji))]
		}
		var b strings.Builder
		if !first && r.IntN(8) == 0 {
			b.WriteByte(before[r.IntN(len(before))])
		}
		b.WriteRune(letters[r.IntN(len(letters))])
		for range r.IntN(8) {
			b.WriteRune(middle[r.IntN(len(middle))])
		}
		if r.IntN(4) == 0 {
			b.WriteByte(after[r.IntN(len(after))])
		}
		return b.String()
	}
	var paragraphs []string
	for range 1 + r.IntN(3) {
		var lines []string
		for range 1 + r.IntN(3) {
			words := []string{word(true)}
			for range r.IntN(12) {
				words = append(words, word(false))
			}
			lines = append(lines, strings.Join(words, " "))
		}
		paragraphs = append(paragraphs, strings.Join(lines, "\n"))
	}
	return strings.Join(paragraphs, "\n\n")
}

func withoutEmoji(s string) string {
	for _, e := range emoji {
		s = strings.ReplaceAll(s, e, "")
	}
	return s
}

func TestPlainTextPassesThrough(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		in := plainText(r)
		if got, want := markdown.ToSpeech(in), withoutEmoji(in); got != want {
			t.Fatalf("ToSpeech(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
