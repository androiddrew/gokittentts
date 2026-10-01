# 15: CPU Docker image

**What to build:** `make image-cpu` builds a multi-stage cgo image on Debian trixie-slim for linux/amd64 and linux/arm64. It bakes mini by default. `BAKE_MODELS` picks which models to bake (it runs `pull` with the same manifest checks), and an empty value gives a slim image. Downloaded models live on a mounted volume so they survive restarts. The espeak-ng package version is pinned, and the image carries a licenses directory (shine-mp3, ffmpeg, the Apache-2.0 models).

**Blocked by:** 09 (Model store and `pull`)

**Status:** resolved

- [x] `make image-cpu` produces a working amd64 and arm64 image that serves audio with `docker run -p 8880:8880`
- [x] `BAKE_MODELS=kitten-tts-nano-0.8-fp32` bakes nano-fp32; `BAKE_MODELS=` builds a slim image that downloads on first use
- [x] Models downloaded at runtime land on the mounted volume and survive a container restart
- [x] The image contains the licenses directory

## Comments

**2026-10-01, implementation notes**

- **Files.** `docker/Dockerfile.cpu`, `docker/config.yaml` (all four models on the CPU, default mini), `.dockerignore`, the `image-cpu` target, and `internal/config/docker_test.go`, which checks that the image's config loads and matches the Dockerfile's paths and the manifest.
- **Build.**
    - The binary is cross-compiled with cgo on the build platform, using Debian's `crossbuild-essential-<arch>` and multiarch `libespeak-ng-dev`, so only the runtime stage's `apt-get` runs under QEMU (about 6.5 minutes for arm64).
    - The ONNX Runtime archives are checked against pinned SHA-256s.
    - `BAKE_MODELS` runs `gokittentts pull` straight into the image's models dir, on the build platform.
- **Pins.** `libespeak-ng1` and `espeak-ng-data` are pinned at `1.52.0+dfsg-5`, trixie's only version. ffmpeg isn't pinned.
- **Tags.** The local Docker uses the classic image store, which can't `--load` a multi-platform image. So `make image-cpu` builds `gokittentts:cpu-amd64` and `gokittentts:cpu-arm64`, and tags the host's one `gokittentts:cpu`. `IMAGE_PLATFORMS` limits the platforms. A single multi-arch `:cpu` needs `--push` to a registry, or the containerd image store; that's for the publishing step.
- **Runtime.**
    - The container runs as uid 10001 and exposes 8880.
    - `/var/lib/gokittentts/models` is a `VOLUME`. A new named volume starts with a copy of the baked models, and a bind mount must be writable by uid 10001.
    - The image carries an OCI `revision` label from `VCS_REF` (HEAD, plus `-dirty` for tracked changes), for the LGPL "recorded commit".
- **Licenses.** `/usr/share/doc/gokittentts/LICENSES` holds `docker/LICENSES` (now with the Apache-2.0 text), ONNX Runtime's `LICENSE` and `ThirdPartyNotices.txt`, and links to the Debian copyright files of ffmpeg and espeak-ng. The README there now also covers espeak-ng (GPL-3.0-or-later, linked into the binary) and ONNX Runtime (MIT).
- **Checked by hand.**
    - amd64 and arm64 (under QEMU) both served mp3 from `docker run -p 8880:8880`, and opus worked through ffmpeg.
    - `BAKE_MODELS=kitten-tts-nano-0.8-fp32` baked only nano-fp32, which served as the default with `KITTEN_DEFAULT_MODEL` and no downloads.
    - `BAKE_MODELS=` gave an empty models dir, and mini downloaded on first use.
    - nano-int8 downloaded at run time into a named volume. A restart, then a new container on the same volume, both served it with `downloads_total` 0.
- **Open: espeak-ng drift.** `testdata/golden.json` was generated with espeak-ng 1.51. Under the image's 1.52, `TestPhonemesAndIDsMatchGolden` fails on two sentences: "four" becomes `fˈɔːɹ` instead of `fˈoːɹ`. Both symbols are in the vocabulary, so synthesis works, but the image doesn't match the goldens token for token. This needs a decision: regenerate the goldens under 1.52 (the dev machine has 1.51), build 1.51 into the image, or accept and record the drift.
