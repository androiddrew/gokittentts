# 17: `bench` subcommand

**What to build:** `gokittentts bench` runs a fixed corpus through a chosen model and device and reports p50/p95 real-time factor and time to first audio. It exits non-zero when RTF ≥ 0.8 or first audio ≥ 1 s (thresholds configurable), so releases can be gated on real time. Its output is suitable for recording per release.

**Blocked by:** 08 (Multi-model config and lazy loading)

**Status:** ready-for-agent

- [ ] `bench` prints p50 and p95 RTF and time to first audio for the chosen model
- [ ] It exits non-zero when a target is missed and zero when both are met
- [ ] The corpus is fixed and checked in, so runs are comparable across releases
- [ ] Output can be saved as a per-release results record
