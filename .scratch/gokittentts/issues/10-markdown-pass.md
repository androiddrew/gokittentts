# 10: Markdown pass

**What to build:** Markdown in LLM chat replies is read as prose. A goldmark pass (with the GFM table extension) runs before normalization and speaks each construct as set by the table in `docs/ORIGINAL_SPEC.md` §6.1: headings, list items and table rows become separate sentences; code blocks are skipped; inline code and bare URLs are kept; link targets are dropped in favor of link text; emphasis markers disappear; emoji are removed. The pass is on by default and the `Request.Markdown` flag (the `markdown` request field) turns it off.

**Blocked by:** 02 (Tracer bullet)

**Status:** resolved

- [x] Table-driven tests, one case per construct in the §6.1 table
- [x] Property test: plain text without markdown passes through unchanged apart from emoji removal
- [x] With `Markdown: false`, literal symbols reach the next stage untouched
- [x] A markdown-heavy reply synthesized through the library is spoken without asterisks, hashes or link URLs

## Comments

**2026-09-30, implementation notes**

- **Package.** `internal/markdown` exposes `ToSpeech(md string) string`. It parses with goldmark v1.8.6 (CommonMark plus the GFM `Table` and `Strikethrough` extensions), walks the AST, and joins the blocks it emits with blank lines.
    - Strikethrough was added because §6.1 lists `~~strike~~`.
    - goldmark's GFM linkify isn't on. Bare URLs are ordinary text, so they reach the normalizer as written.
- **Sentences.** Headings, list items (each paragraph in an item) and table rows become sentences of their own.
    - A trailing `:`, `;` or `,` becomes `.`, and `.` is added unless the sentence ends in `.!?…`.
    - The colon rule goes beyond §6.1's "`.` added if missing". It is needed because the chunker splits only at `.!?`, so "# Steps:" would otherwise run into the next block.
- **Paragraphs and block quotes** are kept exactly as written, with soft and hard line breaks as `\n`. This is what keeps plain text unchanged.
- **Inline content.** Emphasis and strikethrough keep their text. Links keep their text. Images keep their alt text. Code spans keep their content. Autolinks keep their URL or email.
    - Inline HTML tags are dropped, except that `<br>` becomes a line break.
    - HTML blocks have `<br>` turned into a line break, their other tags stripped, and their text kept.
    - Backslash escapes and entities are resolved as goldmark's renderer does.
- **Skipped.** Fenced and indented code blocks, and thematic breaks, aren't spoken.
- **Emoji.** `Extended_Pictographic` comes from Unicode 17.0.0's emoji-data.txt, through `scripts/gen_emoji_table.py`; `make emoji-table` regenerates it.
    - §6.1 lists Extended_Pictographic plus variation selectors and ZWJ. The pass also removes skin-tone modifiers, regional indicators (flags), tag characters and the keycap mark. Without that, flags and skin tones would leave stray code points. A keycap's digit stays.
- **Wiring.** `Model.Stream` runs the pass before chunking when `Request.Markdown` is set. The server already defaults `markdown` to true. `say` gains `--markdown`, default true, so the CLI matches.
- **Tests.**
    - `TestToSpeech` has one or more cases for every §6.1 row, plus escapes, entities, thematic breaks, `<br>`, colon-ended headings and items, and emoji in sentences.
    - `TestPlainTextPassesThrough` generates 2,000 plain texts from a fixed seed: letters, digits, prose punctuation, accented letters and scattered emoji, including ZWJ, skin-tone, flag and variation-selector sequences. Each must come back equal to its input with the emoji removed.
    - Mutation checks fail the tests: trimming paragraphs, joining blocks with one newline, skipping unescaping, and keeping emoji in paragraphs.
    - The native `TestMarkdownIsReadAsProse` streams a markdown reply with `Markdown: true` and requires the same token count per chunk as the hand-written prose run with `Markdown: false`. Phonemizing and tokenizing are deterministic. The reply with `Markdown: false` must differ.
    - With `say`, a sample reply gave 6.85 s of audio with the pass on and 13.22 s with it off, which read out the hashes, asterisks and URL.
- **Known limits.** These follow CommonMark, or §6.1 to the letter, and were left as they are; each is open to a decision.
    - A line starting "2024. A year…" is an ordered list, so the number is dropped.
    - A tab- or four-space-indented line is a code block, so it is skipped.
    - `a*b*c` and `_private_var_` are emphasis.
    - A `|` inside a code span in a table cell splits the cell, as GFM says.
    - `©`, `®`, `™`, `‼` and `⁉` are Extended_Pictographic, so they are removed. `‼` and `⁉` then lose their sentence ending.
    - The plain-text property only generates lines that start with a letter. Lines starting with digits, or with leading or trailing whitespace, can be markdown syntax and aren't covered.
