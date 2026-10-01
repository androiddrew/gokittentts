# 11: Normalizer harness plus numbers, dates and times

**What to build:** The start of the Go port of Python's `normalize_text` (read-aloud mode, not `TextPreprocessor`; span tracking isn't ported). The golden generator gains a corpus of about 300 sentences with Python's normalizer output. A test compares the Go normalizer against that golden for every case, except cases listed in an overrides file, which must equal the override's `expected`. Each override carries a reason and `approved: false`. This ticket ports the substitution list in order up to and including numbers, years, ordinals, dates and times; later categories may be temporarily listed as known-pending. RE2 lookarounds and backreferences are rewritten as a match plus a Go check. The normalizer is on by default and `Request.Normalize` (the `normalize` request field) turns it off.

**Blocked by:** 01 (Golden reference generator), 02 (Tracer bullet)

**Status:** resolved

- [x] `make golden` also writes the normalizer corpus golden
- [x] The overrides file format holds the input, the Python output, the expected output, a reason and `approved`
- [x] Every numbers, years, ordinals, dates and times case equals the Python golden or its override
- [x] "2024 budget" normalizes to "twenty twenty-four budget"
- [x] With `Normalize: false`, text passes through unnormalized, and "3.5" is still spoken as "three point five" (through the phonemizer rule)

## Comments

**2026-10-01, implementation notes**

- **Package.** `internal/normalize` exposes `Text(s string) string`. It runs NFC, then Python's substitutions in order: month-day-year, month-year, times, ordinals, ranges and plain numbers (with years). After those come the punctuation and whitespace cleanup.
    - Not ported yet, in Python's order: HTML, URL and email (13), currency and percent (12), et al. and titles (13), dotted versions and model versions (12).
- **Unicode and RE2.**
    - Python's `\w`, `\d` and `\s` are Unicode-aware. They are written as `[\p{L}\p{N}_]`, `\p{Nd}` and Python's `str.isspace` set.
    - Digits go through `int()` semantics, so "٣" and "１２３" are read.
    - Each lookaround and `\b` is a Go check on the match. A rejected match is retried one character on.
    - The time rule's trailing `\b` can make Python back off the seconds, the spaces or the am/pm suffix. Its tail is matched by hand in Python's backtracking order.
- **Corpus.** `testdata/normalize_corpus.txt` has 378 inputs (`//` comments allowed). They are Python's own tests plus numbers, years, ranges, ordinals, dates, times, money, percents, versions, units, URLs, titles, HTML, markdown, Unicode, LLM-style replies, and a lookaround/boundary section.
    - `make golden` writes `testdata/normalize_golden.json`. An input Python raises on is recorded as `error`.
- **Harness.** `TestCorpus` requires Python's output or the override's `expected`.
    - An override's `python` must match the golden (stale check). It needs a reason and an expected value, and must name a corpus input.
    - Cases that differ from Python and contain a not-yet-ported category are skipped as pending, naming issue 12 or 13. There are 67 now. Issue 13 removes the mechanism.
- **Deviations** (in `normalize_overrides.yaml`, all `approved: false`, 37 entries):
    - Commas after a number are kept. Python's `[\d,]+` eats them ("In 2024, the" → "…four the").
    - A match that is only commas is left alone. Python raises `ValueError` on `int("")`, so "(404), not" or `"hi", then` crash it.
    - The spaces after a time with no am/pm are kept. Python gives "threetoday".
    - Tens ordinals end in "-ieth". Python gives "twentyth".
    - Numbers over 15 digits are read digit by digit. Python silently drops the higher digits ("1,000,000,000,000,000 dollars" → "dollars").
    - Month names are looked up case-folded. Python raises `KeyError` on "ſept".
- **Wiring.**
    - `Model.Stream` runs `normalize.Text` after the markdown pass when `Request.Normalize` is set. The server already defaults `normalize` to true.
    - `say` gains `--normalize`, default true.
- **Tests.**
    - Mutation checks kill every boundary check, the time back-off order, `\p{Nd}` vs `\d`, NFC, the whitespace class and the kept-spaces fix.
    - The native `TestNormalizeIsOptional` checks that "The 2024 budget passed." normalized runs as "twenty twenty-four", and differs with `Normalize: false`.
    - It also checks that unnormalized "Version 3.5 shipped on time." runs as the Python reference's token count, which reads "θɹˈiː pɔɪnt fˈaɪv". This is a count, not the ids. The exact phonemes are pinned by the espeak golden test.
- **Noticed for later.**
    - "Count 1,2,3" is read as one hundred twenty-three, as Python does.
    - "5-6x" gives "fivenegative sixx", as Python does.
    - Python's title rule turns "20 ms." into "Ms" (issue 13).
    - Python's model-version rule raises on a trailing dot, as in "GPT-4." (issue 12).
- `go.mod` now says `go 1.26.0`, because `golang.org/x/text` v0.42.0 requires it (`go mod tidy` restores it).
