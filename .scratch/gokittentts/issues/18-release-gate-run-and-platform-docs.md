# 18: Release gate run and platform docs

**What to build:** The first real gate run on hardware only a human has. Run `bench` for nano-fp32 on a Raspberry Pi 5 and for mini on x86_64 (plus the CUDA images' default), and record the results for the release. Document the plain Linux binary build and runtime dependencies, the Pi 5 build that bakes and defaults to nano-fp32, and best-effort macOS arm64 CPU build notes.

**Blocked by:** 15 (CPU Docker image), 16 (CUDA images), 17 (`bench` subcommand)

**Status:** ready-for-human

- [ ] Pi 5 with nano-fp32: RTF < 0.8 and first audio < 1 s, recorded
- [ ] x86_64 with mini: RTF < 0.8 and first audio < 1 s, recorded
- [ ] CUDA images' default model benchmarked and recorded
- [ ] Plain-binary, Pi 5 and macOS notes are in the README
