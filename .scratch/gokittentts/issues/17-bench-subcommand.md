# 17: `bench` subcommand

**What to build:** `gokittentts bench` runs a fixed corpus through a chosen model and device and reports p50/p95 real-time factor and time to first audio. It exits non-zero when RTF ≥ 0.8 or first audio ≥ 1 s (thresholds configurable), so releases can be gated on real time. Its output is suitable for recording per release.

**Blocked by:** 08 (Multi-model config and lazy loading)

**Status:** resolved

- [x] `bench` prints p50 and p95 RTF and time to first audio for the chosen model
- [x] It exits non-zero when a target is missed and zero when both are met
- [x] The corpus is fixed and checked in, so runs are comparable across releases
- [x] Output can be saved as a per-release results record

## Comments

**2026-10-01, implemented (merged as `f37842a`).**

- **What's done:**
    - `gokittentts bench` synthesizes the checked-in corpus, which is built into the binary from `internal/bench/corpus.txt`, after one untimed warm-up request.
    - It prints the p50 and p95 of RTF and of time to first audio.
    - It exits 1 when either p95 is at or above its target: `--max-rtf` (default 0.8) or `--max-first-audio` (default 1 s). It exits 0 when both are met.
    - `--out` writes a JSON record with the corpus's SHA-256, so runs can be compared across releases.
    - `--config` takes the runtime, models and device from a serve config.
- **Tests:** `internal/bench` is tested with a fake model and clock, and its native test runs the whole corpus through nano-fp32.
- **Release records:** the first ones are in `bench/v0.1.0/` (issue 18).
