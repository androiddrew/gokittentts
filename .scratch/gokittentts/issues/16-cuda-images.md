# 16: CUDA 12 and CUDA 13 images

**What to build:** `make image-cuda12` and `make image-cuda13` build amd64 images on NVIDIA cuDNN runtime bases, using the matching ONNX Runtime GPU archive. Both bake nano-fp32 and default to it with `device: cuda`, so the GPU gives a real speedup out of the box.

**Blocked by:** 14 (CUDA device support), 15 (CPU Docker image)

**Status:** resolved

- [x] Both images serve audio with `docker run --gpus all`
- [x] The default model in both is nano-fp32 on the GPU
- [x] Without a usable GPU the container fails loudly rather than falling back to the CPU

## Comments

**2026-10-01, implementation notes**

- **Failing loudly.** A model can now set `preload: true`, as the user chose over making all CUDA models eager or relying on 503s.
    - `serve` loads preloaded models, in name order, before it listens. If one fails it exits non-zero with "preloading <name>: …". Other models still load on first use.
    - A SIGTERM or interrupt during preloading stops it, for example while a slim image downloads nano-fp32.
    - `server.KittenEngine.Load(ctx, name)` downloads the model if it's missing, then loads it. `Stream` now uses the same path.
    - `docker/config.cuda.yaml` runs all four models with `device: cuda`, defaults to nano-fp32, and preloads only nano-fp32.
- **Docs.** `spec.md`'s config section records `preload` as the one exception to loading on first use.
- **Dockerfile.** One `docker/Dockerfile.cuda`, with `CUDA=12|13` choosing the stage `FROM base-${CUDA}`.
    - Bases are pinned to `nvidia/cuda:12.9.1-cudnn-runtime-ubuntu24.04` and `nvidia/cuda:13.0.3-cudnn-runtime-ubuntu24.04`. 13.0 keeps the spec's "CUDA 13 needs driver ≥ 580".
    - The ONNX Runtime `gpu_cuda12`/`gpu_cuda13` 1.29.1 archives are checked against pinned SHA-256s. The image keeps `libonnxruntime.so.1.29.1`, `libonnxruntime_providers_shared.so` and `libonnxruntime_providers_cuda.so`; TensorRT is dropped.
    - The binary is built on `ubuntu:24.04`, with Go copied from `golang:1.26-trixie`, so it links the runtime's glibc and espeak-ng.
    - espeak-ng is pinned at `1.51+dfsg-12build1`. That is Ubuntu 24.04's version and the one the goldens were made with, so unlike the CPU image's 1.52, these images match the goldens.
    - Baking, the models volume, uid 10001, the licenses directory and the OCI revision label work as in the CPU image. The licenses directory adds a link to NVIDIA's `NGC-DL-CONTAINER-LICENSE`.
- **Make.** `make image-cuda12` and `make image-cuda13` build `gokittentts:cuda12`/`:cuda13` for linux/amd64. They bake nano-fp32 unless `BAKE_MODELS` is given on the command line.
- **Tests.**
    - The config test checks the `preload` field.
    - `TestDockerConfigs` checks that both image configs load, and their default, devices and preloads.
    - The native server test `TestLoad` covers a load, and ErrModelUnavailable for a model that can't be got.
- **Checked by hand** (RTX 4090, driver 595):
    - Both images with `--gpus all` logged "preloaded … device cuda" and served mp3. Warm requests took about 0.1 s, `kitten_model_loaded{device="cuda",model="kitten-tts-nano-0.8-fp32"}` was 1, and `gokittentts` showed in `nvidia-smi`.
    - Without `--gpus`, both exited 1 with "preloading kitten-tts-nano-0.8-fp32: … enabling CUDA on device 0: … Failed to load library".
- **Sizes.** cuda12 is 6.1 GB and cuda13 is 3.8 GB, mostly NVIDIA's base images.
