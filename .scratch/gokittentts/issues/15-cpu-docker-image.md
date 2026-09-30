# 15: CPU Docker image

**What to build:** `make image-cpu` builds a multi-stage cgo image on Debian trixie-slim for linux/amd64 and linux/arm64. It bakes mini by default. `BAKE_MODELS` picks which models to bake (it runs `pull` with the same manifest checks), and an empty value gives a slim image. Downloaded models live on a mounted volume so they survive restarts. The espeak-ng package version is pinned, and the image carries a licenses directory (shine-mp3, ffmpeg, the Apache-2.0 models).

**Blocked by:** 09 (Model store and `pull`)

**Status:** ready-for-agent

- [ ] `make image-cpu` produces a working amd64 and arm64 image that serves audio with `docker run -p 8880:8880`
- [ ] `BAKE_MODELS=kitten-tts-nano-0.8-fp32` bakes nano-fp32; `BAKE_MODELS=` builds a slim image that downloads on first use
- [ ] Models downloaded at runtime land on the mounted volume and survive a container restart
- [ ] The image contains the licenses directory
