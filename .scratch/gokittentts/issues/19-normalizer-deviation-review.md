# 19: Normalizer deviation review

**What to build:** One review pass over the overrides file at the end of the project. Every deliberate difference between the Go normalizer and Python is approved, edited or reverted, so the final behavior is a deliberate choice.

**Blocked by:** 12 (Normalizer: money, percents, versions and units), 13 (Normalizer: URLs, emails, titles and the rest)

**Status:** resolved

- [x] Every override is either `approved: true` or removed (with the Go behavior reverted to match Python)
- [x] The normalizer corpus test still passes afterwards

## Comments

**2026-10-01, review (owner, with the agent).** The 108 overrides were grouped by reason into 24 groups in `.scratch/gokittentts/19-deviation-review.md`, each showing input, Python and Go. The owner approved all of them as they are, and every entry in `testdata/normalize_overrides.yaml` is now `approved: true`. `TestCorpus` and `go test ./...` pass.

Two approved behaviors were flagged during the review and kept on purpose:

- **G11, scale letters.** "7B parameters" reads "seven billion", but "Room 4B" reads "four billion" and "A 4K screen" reads "four thousand screen".
- **G17, percent ranges.** "5-10%" reads "five-ten percent", while "5-10 km" reads "five to ten kilometers".

Two misreads that aren't overrides, because Go matches Python, were found while probing: "3D printer" reads "threeD printer", and "1080p" reads "one thousand eightyp". They are out of scope here.
