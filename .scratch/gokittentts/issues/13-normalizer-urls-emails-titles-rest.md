# 13: Normalizer: URLs, emails, titles and the rest

**What to build:** Finishes the port of `normalize_text`: URLs and email addresses are spelled out rather than deleted, titles like "Dr." are expanded, and every remaining substitution in the list is ported in order. After this ticket nothing in the corpus is known-pending: every case equals the Python golden or has an override.

**Blocked by:** 11 (Normalizer harness plus numbers, dates and times)

**Status:** resolved

- [x] URLs and email addresses in the corpus are spelled out, not removed
- [x] "Dr." and the other titles expand as in the golden
- [x] The whole normalizer corpus passes with no known-pending cases left
- [x] Every deliberate difference from Python is in the overrides file with a reason and `approved: false`

## Comments

**2026-10-01, implementation notes**

- **Substitutions.** Python's HTML, URL, email, et al. and title rules are ported. Every substitution in `normalize_text_result` now runs in Python's order.
    - The code is in `internal/normalize/web.go` and `abbreviations.go`.
    - The test's pending list and skip are gone: every one of the 476 corpus inputs equals Python's output or its override's `expected`.
- **Spelling.** URLs and emails are spelled with Python's `_spell_characters` rules.
    - An email's `\b` at the start is checked as Python's (the first character may be "." or "+").
    - Where the end fails, shorter domains are tried, as Python's backtracking would: "bob@a.org.uk1" → "b o b at a dot o r g.uk1".
- **Deviations.** 16 new overrides (108 in total), all `approved: false`:
    - **URL endings.** Punctuation ending a URL is left in the text, and closing brackets and quotes there are dropped. Python spells the sentence's full stop ("… c o m dot") and a final "?" ("question mark").
    - **URL case.** URLs match case-insensitively. Python leaves "HTTPS://EXAMPLE.COM" and "WWW.EXAMPLE.COM" as written.
    - **"www."** is spelled "w w w dot". Python rewrites it to "www dot " first and then spells that ("w w w d o t").
    - **Email domains** can have several dots: "a@b.co.uk" → "a at b dot c o dot u k". Python stops at "c o.uk".
    - **"p.m."** "p." starting "p.m." or "p.m" is not the title "page". Python reads "5 p.m." as "five pagem.".
    - **HTML.** A tag needs a letter, "/" or "!" after "<", so "3 < 5 and 6 > 2" isn't deleted. "x<y and y>z" still is.
    - **Unicode digits.** A non-ASCII digit in a URL is read. Python raises KeyError.
- **Tests.** `TestExamples` adds Python's own URL/email test sentence and "Dr. Rivera paid $12.50 at 3:05 p.m.". Mutation checks kill each new boundary check, the email back-off, every deviation and the case folding.
- **For the review (issue 19).**
    - Lone "a.m."/"p.m." without a time stay as written, and espeak-ng reads them. The time rule's "a m"/"p m" only applies after h:mm.
    - "Dr.Who" → "DoctorWho", and "x<y and y>z" is deleted as a tag, as in Python.
