// Package markdown turns markdown, such as an LLM's chat reply, into plain
// text to be read aloud, as set by docs/ORIGINAL_SPEC.md section 6.1.
//
// Headings, list items and table rows become sentences of their own. Code
// blocks are skipped, link targets are dropped for the link text, markup is
// removed, and emoji are deleted. Plain text without markdown syntax comes
// out unchanged apart from its emoji.
package markdown

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var parser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough)).Parser()

var (
	htmlTag   = regexp.MustCompile(`<[^>]*>`)
	lineBreak = regexp.MustCompile(`(?i)<br\s*/?>`)
)

// ToSpeech returns md as plain text, its blocks separated by blank lines.
func ToSpeech(md string) string {
	src := []byte(md)
	w := &walker{src: src}
	w.blocks(parser.Parse(text.NewReader(src)))
	return strings.Join(w.out, "\n\n")
}

type walker struct {
	src []byte
	out []string
}

// blocks walks n's block children in order.
func (w *walker) blocks(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.block(c)
	}
}

func (w *walker) block(n ast.Node) {
	switch n := n.(type) {
	case *ast.Heading:
		w.sentence(w.inline(n))
	case *ast.Paragraph, *ast.TextBlock:
		// A paragraph in a list item is a sentence of its own; elsewhere it
		// is kept as written.
		if _, inItem := n.Parent().(*ast.ListItem); inItem {
			w.sentence(w.inline(n))
		} else {
			w.paragraph(w.inline(n))
		}
	case *extast.Table:
		for row := n.FirstChild(); row != nil; row = row.NextSibling() {
			var cells []string
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				if s := strings.TrimSpace(removeEmoji(w.inline(cell))); s != "" {
					cells = append(cells, s)
				}
			}
			w.sentence(strings.Join(cells, ", "))
		}
	case *ast.HTMLBlock:
		text := lineBreak.ReplaceAllString(w.htmlBlockText(n), "\n")
		w.paragraph(strings.TrimSpace(htmlTag.ReplaceAllString(text, "")))
	case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.ThematicBreak:
		// Not spoken.
	default:
		// Block quotes, lists and list items hold blocks.
		w.blocks(n)
	}
}

// paragraph adds text as written, less its emoji.
func (w *walker) paragraph(s string) {
	if s = removeEmoji(s); strings.TrimSpace(s) != "" {
		w.out = append(w.out, s)
	}
}

// sentence adds text as a sentence of its own. The chunker ends sentences
// only at .!?, so a trailing colon, semicolon or comma becomes a period, and
// one is added if there is none.
func (w *walker) sentence(s string) {
	s = strings.TrimRight(strings.TrimSpace(removeEmoji(s)), ",;:")
	if s == "" {
		return
	}
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") && !strings.HasSuffix(s, "…") {
		s += "."
	}
	w.out = append(w.out, s)
}

// inline returns the text of n's inline content.
func (w *walker) inline(n ast.Node) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			b.Write(unescape(c.Segment.Value(w.src)))
			if c.SoftLineBreak() || c.HardLineBreak() {
				b.WriteByte('\n')
			}
		case *ast.String:
			b.Write(c.Value)
		case *ast.CodeSpan:
			// Its content is read; the normalizer decides how.
			for t := c.FirstChild(); t != nil; t = t.NextSibling() {
				if t, ok := t.(*ast.Text); ok {
					b.Write(t.Segment.Value(w.src))
				}
			}
		case *ast.AutoLink:
			b.Write(c.Label(w.src))
		case *ast.RawHTML:
			// Tags are dropped, except that <br> breaks the line; the text
			// between tags is a sibling.
			if lineBreak.Match(c.Segments.Value(w.src)) {
				b.WriteByte('\n')
			}
		default:
			// Emphasis, strikethrough, links and images: their text, so a
			// link's target and an image's URL are dropped.
			b.WriteString(w.inline(c))
		}
	}
	return b.String()
}

// htmlBlockText returns an HTML block's source, tags and all.
func (w *walker) htmlBlockText(n *ast.HTMLBlock) string {
	var b strings.Builder
	lines := n.Lines()
	for i := range lines.Len() {
		line := lines.At(i)
		b.Write(line.Value(w.src))
	}
	if n.HasClosure() {
		b.Write(n.ClosureLine.Value(w.src))
	}
	return b.String()
}

// unescape undoes backslash escapes and character references, as goldmark's
// HTML renderer does.
func unescape(b []byte) []byte {
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(b)))
}
