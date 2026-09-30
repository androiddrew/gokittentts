# Golang Onnx

An example repo trying to run the Kitten TTS onnx model using golang

See [SPEC.md](SPEC.md) for the full design: the model contract, the text-to-token pipeline that must be ported from Python, and the onnxruntime_go usage.

Pinned versions: `github.com/yalue/onnxruntime_go` v1.36.0 with ONNX Runtime 1.29.1. Always pass the versioned `libonnxruntime.so.1.29.1`, not the `libonnxruntime.so` symlink.

## Other model sizes

The same binary runs all four KittenTTS 0.8 models. To switch, download a different repo into its own directory and pass `-model-dir`:

```bash
M=kitten-tts-nano-0.8-fp32   # or kitten-tts-micro-0.8, kitten-tts-nano-0.8-int8
for f in config.json voices.npz kitten_tts_nano_v0_8.onnx; do   # micro: kitten_tts_micro_v0_8.onnx
  curl -L --create-dirs -o models/$M/$f https://huggingface.co/KittenML/$M/resolve/main/$f
done
```

In the prototype, nano-fp32 was the fastest by far: 0.27 s on CPU and 0.06 s on GPU per sentence, versus 2.8 s and 1.8 s for mini. See "Model variants" in SPEC.md.

## CPU option

```bash
# espeak-ng provides the phonemes (linked through cgo)
sudo apt install espeak-ng libespeak-ng-dev

# ONNX Runtime 1.29.1, CPU build
mkdir -p third_party
curl -L https://github.com/microsoft/onnxruntime/releases/download/v1.29.1/onnxruntime-linux-x64-1.29.1.tgz \
  | tar xz -C third_party/

# model files from Hugging Face
for f in config.json kitten_tts_mini_v0_8.onnx voices.npz; do
  curl -L --create-dirs -o models/kitten-tts-mini-0.8/$f \
    https://huggingface.co/KittenML/kitten-tts-mini-0.8/resolve/main/$f
done

CGO_ENABLED=1 go build -o bin/ ./cmd/...
bin/kitten-cli -device cpu \
  -onnxruntime_lib third_party/onnxruntime-linux-x64-1.29.1/lib/libonnxruntime.so.1.29.1 \
  -model-dir models/kitten-tts-mini-0.8 \
  -text "Hello from Go." -voice Bruno -out out.wav
```

Expect a real-time factor of about 0.57 on CPU: 4.84 s of audio took 2.77 s in the prototype. Use `-threads` to tune the intra-op threads.

## GPU option

The same binary runs on an NVIDIA GPU when it is given the CUDA build of ONNX Runtime and `-device cuda`.

```bash
# ONNX Runtime 1.29.1, CUDA 12 build (keep the providers_*.so files next to libonnxruntime)
curl -L https://github.com/microsoft/onnxruntime/releases/download/v1.29.1/onnxruntime-linux-x64-gpu_cuda12-1.29.1.tgz \
  | tar xz -C third_party/

# CUDA 12 + cuDNN 9 runtime libraries without sudo (skip if installed system-wide)
uv venv .cuda && VIRTUAL_ENV=.cuda uv pip install "nvidia-cudnn-cu12>=9,<10" \
  nvidia-cublas-cu12 nvidia-cuda-runtime-cu12 nvidia-curand-cu12 nvidia-cufft-cu12 \
  nvidia-cusolver-cu12 nvidia-cusparse-cu12 nvidia-nvjitlink-cu12
export LD_LIBRARY_PATH=$(ls -d $PWD/.cuda/lib/python3*/site-packages/nvidia/*/lib | paste -sd:)

bin/kitten-cli -device cuda \
  -onnxruntime_lib third_party/onnxruntime-linux-x64-gpu_cuda12-1.29.1/lib/libonnxruntime.so.1.29.1 \
  -model-dir models/kitten-tts-mini-0.8 \
  -text "Hello from Go." -voice Bruno -out out.wav
```

Don't expect a large speed-up. The model is int8 dynamically quantized, and ONNX Runtime has no CUDA kernels for its `MatMulInteger`, `ConvInteger` and `DynamicQuantizeLSTM` nodes, so those stay on the CPU. On an RTX 4090 the GPU was only 1.55× faster than CPU (1.78 s vs 2.77 s). See the GPU section of SPEC.md for details.
