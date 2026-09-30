# 11: Normalizer harness plus numbers, dates and times

**What to build:** The start of the Go port of Python's `normalize_text` (read-aloud mode, not `TextPreprocessor`; span tracking isn't ported). The golden generator gains a corpus of about 300 sentences with Python's normalizer output. A test compares the Go normalizer against that golden for every case, except cases listed in an overrides file, which must equal the override's `expected`. Each override carries a reason and `approved: false`. This ticket ports the substitution list in order up to and including numbers, years, ordinals, dates and times; later categories may be temporarily listed as known-pending. RE2 lookarounds and backreferences are rewritten as a match plus a Go check. The normalizer is on by default and `Request.Normalize` (the `normalize` request field) turns it off.

**Blocked by:** 01 (Golden reference generator), 02 (Tracer bullet)

**Status:** ready-for-agent

- [ ] `make golden` also writes the normalizer corpus golden
- [ ] The overrides file format holds the input, the Python output, the expected output, a reason and `approved`
- [ ] Every numbers, years, ordinals, dates and times case equals the Python golden or its override
- [ ] "2024 budget" normalizes to "twenty twenty-four budget"
- [ ] With `Normalize: false`, text passes through unnormalized, and "3.5" is still spoken as "three point five" (through the phonemizer rule)
