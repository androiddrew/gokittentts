# 16: CUDA 12 and CUDA 13 images

**What to build:** `make image-cuda12` and `make image-cuda13` build amd64 images on NVIDIA cuDNN runtime bases, using the matching ONNX Runtime GPU archive. Both bake nano-fp32 and default to it with `device: cuda`, so the GPU gives a real speedup out of the box.

**Blocked by:** 14 (CUDA device support), 15 (CPU Docker image)

**Status:** ready-for-agent

- [ ] Both images serve audio with `docker run --gpus all`
- [ ] The default model in both is nano-fp32 on the GPU
- [ ] Without a usable GPU the container fails loudly rather than falling back to the CPU
