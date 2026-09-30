# 12: Normalizer: money, percents, versions and units

**What to build:** Ports the money, percent and version rules of `normalize_text`, plus the first two deliberate fixes: currency with a scale word, and units after numbers (ported from `TextPreprocessor`'s `expand_scale_suffixes` and `expand_units`). Each fix is recorded as an override with its reason and `approved: false`.

**Blocked by:** 11 (Normalizer harness plus numbers, dates and times)

**Status:** ready-for-agent

- [ ] Every money, percent and version corpus case equals the Python golden or its override
- [ ] "$3.5 million" normalizes to "three point five million dollars"
- [ ] "3 GB" normalizes to "three gigabytes"
- [ ] Both fixes appear in the overrides file with reasons and `approved: false`
