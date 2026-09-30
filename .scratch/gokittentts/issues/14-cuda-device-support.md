# 14: CUDA device support

**What to build:** An admin with an NVIDIA GPU sets `device: cuda` (and optionally a CUDA device id and intra-op threads) per model in the config, and that model runs on the GPU. If the CUDA execution provider can't be enabled, loading the model is an error; there is never a silent CPU fallback.

**Blocked by:** 08 (Multi-model config and lazy loading)

**Status:** ready-for-agent

- [ ] With a CUDA ONNX Runtime build and a GPU, nano-fp32 with `device: cuda` serves audio and runs on the GPU
- [ ] Native library test: a CUDA append failure surfaces as a load error
- [ ] An unknown device value fails config validation at startup
