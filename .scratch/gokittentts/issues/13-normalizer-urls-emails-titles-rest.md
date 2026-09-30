# 13: Normalizer: URLs, emails, titles and the rest

**What to build:** Finishes the port of `normalize_text`: URLs and email addresses are spelled out rather than deleted, titles like "Dr." are expanded, and every remaining substitution in the list is ported in order. After this ticket nothing in the corpus is known-pending: every case equals the Python golden or has an override.

**Blocked by:** 11 (Normalizer harness plus numbers, dates and times)

**Status:** ready-for-agent

- [ ] URLs and email addresses in the corpus are spelled out, not removed
- [ ] "Dr." and the other titles expand as in the golden
- [ ] The whole normalizer corpus passes with no known-pending cases left
- [ ] Every deliberate difference from Python is in the overrides file with a reason and `approved: false`
