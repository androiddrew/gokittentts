# gokittentts

Self-hosted, OpenAI-compatible text-to-speech in Go, running the [KittenTTS](https://github.com/KittenML/KittenTTS) 0.8 ONNX models through [onnxruntime_go](https://github.com/yalue/onnxruntime_go).

See [the specification](.scratch/gokittentts/spec.md) for the full design: the user stories, the decisions, the test seams and the milestones. [docs/ORIGINAL_SPEC.md](docs/ORIGINAL_SPEC.md) keeps the reference detail (model hashes, the symbol table, the ONNX Runtime calls).

Pinned versions: `github.com/yalue/onnxruntime_go` v1.36.0 with ONNX Runtime 1.29.1. Always point the program at the versioned `libonnxruntime.so.1.29.1`, not the `libonnxruntime.so` symlink.

## Models

| Model | Size | CPU (Ryzen 5 5600X) | GPU (RTX 4090) | Default in |
| --- | --- | --- | --- | --- |
| `kitten-tts-mini-0.8` | 78 MB | 2.82 s | 1.79 s | CPU image |
| `kitten-tts-micro-0.8` | 41 MB | 1.77 s | 1.03 s | |
| `kitten-tts-nano-0.8-int8` | 24 MB | 1.76 s | 0.91 s | |
| `kitten-tts-nano-0.8-fp32` | 57 MB | 0.27 s | 0.06 s | CUDA images, Raspberry Pi 5 |

Times are per sentence of about 5 s of audio. Any model not on disk is downloaded from Hugging Face on first use, pinned to a commit and checked against a SHA-256.

## CPU option

Docker (linux/amd64 and linux/arm64):

```bash
make image-cpu                                   # bakes mini; BAKE_MODELS= for a slim image
docker run -p 8880:8880 -v kitten-models:/var/lib/gokittentts/models gokittentts:cpu
```

Plain binary (Linux):

```bash
sudo apt install espeak-ng libespeak-ng-dev ffmpeg
mkdir -p third_party
curl -L https://github.com/microsoft/onnxruntime/releases/download/v1.29.1/onnxruntime-linux-x64-1.29.1.tgz \
  | tar xz -C third_party/                       # linux-aarch64 on arm64
CGO_ENABLED=1 go build -tags espeak -o bin/gokittentts ./cmd/gokittentts
bin/gokittentts say --onnxruntime-lib third_party/onnxruntime-linux-x64-1.29.1/lib/libonnxruntime.so.1.29.1 \
  --voice Bruno --out out.wav "Hello from Go."
```

For a Raspberry Pi 5, bake nano-fp32 and make it the default:

```bash
make image-cpu BAKE_MODELS=kitten-tts-nano-0.8-fp32
docker run -e KITTEN_DEFAULT_MODEL=kitten-tts-nano-0.8-fp32 -p 8880:8880 gokittentts:cpu
```

## GPU option

The NVIDIA images need the NVIDIA Container Toolkit. CUDA 13 needs host driver 580 or newer.

```bash
make image-cuda12        # or image-cuda13; both bake and default to nano-fp32
docker run --gpus all -p 8880:8880 gokittentts:cuda12
```

For a plain binary, use the `gpu_cuda12` (or `gpu_cuda13`) ONNX Runtime archive, provide CUDA and cuDNN 9, and set `device: cuda` for the model in `config.yaml`. The GPU only speeds up nano-fp32 significantly. The int8 models keep their quantized ops on the CPU (see [docs/ORIGINAL_SPEC.md](docs/ORIGINAL_SPEC.md) section 11.4).

## Use it

```bash
curl http://localhost:8880/v1/audio/speech \
  -H "Content-Type: application/json" \
  -d '{"model": "tts-1", "input": "Hello from gokittentts!", "voice": "nova", "response_format": "mp3"}' \
  -o hello.mp3
```

Set `KITTEN_API_KEY` to require `Authorization: Bearer <key>`.
