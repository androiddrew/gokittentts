# 19: Normalizer deviation review

**What to build:** One review pass over the overrides file at the end of the project. Every deliberate difference between the Go normalizer and Python is approved, edited or reverted, so the final behavior is a deliberate choice.

**Blocked by:** 12 (Normalizer: money, percents, versions and units), 13 (Normalizer: URLs, emails, titles and the rest)

**Status:** ready-for-human

- [ ] Every override is either `approved: true` or removed (with the Go behavior reverted to match Python)
- [ ] The normalizer corpus test still passes afterwards
