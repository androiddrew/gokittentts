# 18: Release gate run and platform docs

**What to build:** The first real gate run on hardware only a human has. Run `bench` for nano-fp32 on a Raspberry Pi 5 and for mini on x86_64 (plus the CUDA images' default), and record the results for the release. Document the plain Linux binary build and runtime dependencies, the Pi 5 build that bakes and defaults to nano-fp32, and best-effort macOS arm64 CPU build notes.

**Blocked by:** 15 (CPU Docker image), 16 (CUDA images), 17 (`bench` subcommand)

**Status:** ready-for-human

- [ ] Pi 5 with nano-fp32: RTF < 0.8 and first audio < 1 s, recorded
- [ ] x86_64 with mini: RTF < 0.8 and first audio < 1 s, recorded
- [x] CUDA images' default model benchmarked and recorded
- [x] Plain-binary, Pi 5 and macOS notes are in the README

## Comments

**2026-10-01, agent pass (branch `gokittentts/18-release-gate-run-and-platform-docs`).** I did the parts that don't need a Pi. The Pi run and the decision on the mini miss are left for a human.

- **Recorded** in `bench/v0.1.0/`, at commit `f37842a` with 5 runs each, from images rebuilt at that commit and summarized in README "v0.1.0 results":
    - x86_64 mini (Ryzen 5 5600X, plain binary): RTF p95 0.659, first audio p95 **3.86 s**, which **fails** the gate.
    - `gokittentts:cuda12` nano-fp32 (RTX 4090): RTF p95 0.080, first audio p95 0.25 s, which passes.
    - `gokittentts:cuda13` nano-fp32 (RTX 4090): RTF p95 0.047, first audio p95 0.16 s, which passes.
- **The mini miss is the model's speed, not chunking.** At an RTF of about 0.58, any first chunk longer than about 1.7 s of audio takes over 1 s, and even "Hello from Go." (1.9 s of audio) took 1.23 s. To pass, one of these needs deciding:
    - make the x86_64 gate's model micro or nano;
    - relax the first-audio target for mini;
    - accept the miss for v0.1.0.
- **Pi 5 build.**
    - `make image-pi5` builds `gokittentts:pi5` (linux/arm64). It bakes nano-fp32 and makes it the default through a new `DEFAULT_MODEL` build arg on `Dockerfile.cpu`, which sets `KITTEN_DEFAULT_MODEL`.
    - Under QEMU, the image had nano-fp32 baked in, and `bench --config` picked nano-fp32 without a download (`--network none`).
    - QEMU timings are meaningless, so the real gate run still has to happen on a Pi 5:
      `docker run --rm --hostname "$(hostname)" gokittentts:pi5 bench --config /etc/gokittentts/config.yaml --runs 5 --out - > bench/v0.1.0/<host>-nano-fp32-pi5.json`
- **Docs.** The README has new sections: "Plain Linux binary" (dependency table, build, ONNX Runtime install, example config, serve), "Raspberry Pi 5" and "macOS arm64 (best effort)".
    - The example config parses: `pull --config` accepts it.
    - The macOS steps are untested. `onnxruntime_go` needs cgo, so even a darwin compile can't be checked from Linux.
- **Docker bench examples** now pass `--hostname "$(hostname)"`, because a record made in a container otherwise names the container's ID as the host.
