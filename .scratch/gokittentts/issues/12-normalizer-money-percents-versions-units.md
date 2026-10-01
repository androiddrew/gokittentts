# 12: Normalizer: money, percents, versions and units

**What to build:** Ports the money, percent and version rules of `normalize_text`, plus the first two deliberate fixes: currency with a scale word, and units after numbers (ported from `TextPreprocessor`'s `expand_scale_suffixes` and `expand_units`). Each fix is recorded as an override with its reason and `approved: false`.

**Blocked by:** 11 (Normalizer harness plus numbers, dates and times)

**Status:** resolved

- [x] Every money, percent and version corpus case equals the Python golden or its override
- [x] "$3.5 million" normalizes to "three point five million dollars"
- [x] "3 GB" normalizes to "three gigabytes"
- [x] Both fixes appear in the overrides file with reasons and `approved: false`

## Comments

**2026-10-01, implementation notes**

- **Substitutions.** Python's currency, percent, dotted-version and model-version rules are ported in order. The ticket-12 entries are gone from the test's pending list. 24 cases remain pending, all for issue 13.
    - TextPreprocessor's units and scale suffixes run after percents. The code is in `internal/normalize/amounts.go` and `versions.go`.
- **Backtracking.** Python's currency lookahead can back off the scale letter, the spaces, the decimals or even integer digits ("$1,000x" → "one dollar,zerox"). The match is finished by hand in Python's order.
    - A dotted version gives back trailing parts at a failed `\b` ("v1.2.3.4x").
    - Percents go through Python's `str(float(raw))`, so "3.50%" reads "three point five".
- **The two named fixes:**
    - "$3.5 million" → "three point five million dollars". So do "$2 billion" and "$10 thousand".
    - "3 GB" → "three gigabytes". The unit is spelled out and the number left to the number rules, so "5-10 km" reads "five to ten kilometers". A number right after a word character ("x5kg") is left alone, as the number rule leaves it.
    - "7B" → "seven billion". There's no space before the letter, unlike TextPreprocessor, so "3 T-shirts" stays.
- **Other deviations found in the corpus.** All are recorded, 55 new overrides (92 in total), all `approved: false`:
    - Python raises on "GPT-4." and "gpt-3.5." at the end of a sentence, and on "Model-3..5". The leading digits-and-dots version is read and the rest stays.
    - Python raises on "$," and on percents whose float prints in exponent form ("0.00001%", "12345678901234567.5%").
    - "one dollar and fifty cents" instead of "one dollars"; "yen" and "won" instead of "yens" and "wons".
    - Units are singular after 1, including "01".
    - Commas after a whole currency amount stay, as for plain numbers.
    - A minus after a word character is a hyphen, not a sign: "5-10%" → "five-ten percent", not "fivenegative ten percent".
- **Refactor.** `sub` and `times` now share `scan`, which the currency and dotted-version rules also use.
- **Tests.** The corpus grew to 447 inputs. `TestExamples` pins the ticket's two examples and "2024 budget". Mutation checks kill every new boundary check, the backtracking steps, the float repr and both fixes.
- **For the review (issue 19).**
    - "4K screen" → "four thousand screen", and "Plan 9B" → "nine billion". This is faithful to `expand_scale_suffixes`.
    - Units match case-insensitively ("5 MS" → milliseconds), as in TextPreprocessor.
    - "$3 millions" still reads "three dollars millions", as in Python.
    - "$5 . Cheap" loses the space before the period, as in Python.
