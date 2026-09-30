# 01: Golden reference generator

**What to build:** `make golden` runs the KittenTTS 0.8.1 Python reference (`onnx_model.py` and `preprocess.py`) with upstream `phonemizer` ≥ 3.4.0 against the system `libespeak-ng.so.1`. It writes golden fixtures (the input chunk, its phoneme string and its token ids) that the Go text front end is compared against. The script is the one in `docs/ORIGINAL_SPEC.md` Appendix A. The fixtures are checked in so that `go test` never needs Python.

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] `make golden` regenerates the phoneme and token-id goldens from a fixed input list covering decimals, thousands separators, punctuation runs, quotes, dashes, ellipses and abbreviations
- [x] The script refuses to run, with a clear message, when `phonemizer-fork` is installed
- [x] The script refuses to run when `phonemizer` is older than 3.4.0
- [x] The goldens record the espeak-ng and phonemizer versions used
- [x] The golden for "3.5" shows the decimal read as one number, not "three . five"
- [x] The generated fixtures are committed

## Comments

**2026-09-30, implementation notes**

- **Reference pin.** At the `0.8.1` git tag (and in the 0.8.1 release wheel), `chunk_text` is still in `onnx_model.py` as a plain `re.split(r'[.!?]+')`, and `normalize_text` doesn't exist. The behaviour the spec describes (decimal and abbreviation-aware `preprocess.chunk_text`, `normalize_text`, dependency on `phonemizer`) is on `main`, whose `setup.py` says `version="0.8.1"`. The reference is therefore pinned to `main` at `be5758500b731b8fc674acc62ea480d3022b7ebe` (`KITTENTTS_REF` in the `Makefile`; `make reference` re-vendors `scripts/onnx_model_ref.py` and `scripts/preprocess_ref.py`).
- **Recorded deviation: phonemizer 3.4.0 punctuation split.** Upstream `Punctuation._preserve_line` finds marks with the decimal-aware regex but cuts the line with `str.split(mark)`. That splits at the first `.` anywhere, decimal points included, so a chunk with a decimal and a later `.` mark loses every word after the decimal point (`Version 3.5 shipped on time.` → `vˈɜːʒən θɹˈiː.`). `make_golden.py` monkeypatches it to cut at the regex match spans, which is what the Go scanner does. On `testdata/sentences.txt` this changes only the two affected chunks. Worth an upstream bug report.
- `make golden-test` runs end-to-end pytest checks of the script: fork refusal, <3.4 refusal, decimals read as one number, versions recorded.
- Goldens were generated with espeak-ng 1.51 and phonemizer 3.4.0.
