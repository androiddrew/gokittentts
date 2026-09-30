# 10: Markdown pass

**What to build:** Markdown in LLM chat replies is read as prose. A goldmark pass (with the GFM table extension) runs before normalization and speaks each construct as set by the table in `docs/ORIGINAL_SPEC.md` §6.1: headings, list items and table rows become separate sentences; code blocks are skipped; inline code and bare URLs are kept; link targets are dropped in favor of link text; emphasis markers disappear; emoji are removed. The pass is on by default and the `Request.Markdown` flag (the `markdown` request field) turns it off.

**Blocked by:** 02 (Tracer bullet)

**Status:** ready-for-agent

- [ ] Table-driven tests, one case per construct in the §6.1 table
- [ ] Property test: plain text without markdown passes through unchanged apart from emoji removal
- [ ] With `Markdown: false`, literal symbols reach the next stage untouched
- [ ] A markdown-heavy reply synthesized through the library is spoken without asterisks, hashes or link URLs
