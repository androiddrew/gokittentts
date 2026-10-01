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

`make image-cpu` builds `gokittentts:cpu-amd64` and `gokittentts:cpu-arm64` with `docker buildx`, and tags the host's one `gokittentts:cpu`. The other platform's packages install under QEMU, so it needs `binfmt` support. Use `IMAGE_PLATFORMS=linux/arm64` to build only one. Models the image didn't bake are downloaded on first use into `/var/lib/gokittentts/models`. Mount a volume there so they survive restarts. A new named volume starts with a copy of the baked models. A bind-mounted directory must be writable by uid 10001. The image's config is [`docker/config.yaml`](docker/config.yaml). Third-party licenses are in `/usr/share/doc/gokittentts/LICENSES`.

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

The CUDA images are linux/amd64 only. They run every model with `device: cuda` ([`docker/config.cuda.yaml`](docker/config.cuda.yaml)), and load nano-fp32 before listening because it sets `preload: true`. Without a usable GPU, for example without `--gpus all`, the container exits with the CUDA error rather than falling back to the CPU. `BAKE_MODELS` and the models volume work as in the CPU image. Any model can set `preload: true` to load before `serve` listens.

For a plain binary, use the `gpu_cuda12` (or `gpu_cuda13`) ONNX Runtime archive (`make onnxruntime-gpu ORT_CUDA=12`), provide CUDA and cuDNN 9 on the loader path, and set `device: cuda` (and optionally `cuda_device_id`) for the model in `config.yaml`, or pass `say --device cuda`. If the CUDA provider can't be enabled, the model fails to load; it never falls back to the CPU. `make test-cuda` checks nano-fp32 on GPU 0. The GPU only speeds up nano-fp32 significantly. The int8 models keep their quantized ops on the CPU (see [docs/ORIGINAL_SPEC.md](docs/ORIGINAL_SPEC.md) section 11.4).

## Use it

```bash
curl http://localhost:8880/v1/audio/speech \
  -H "Content-Type: application/json" \
  -d '{"model": "tts-1", "input": "Hello from gokittentts!", "voice": "nova", "response_format": "mp3"}' \
  -o hello.mp3
```

Set `KITTEN_API_KEY` to require `Authorization: Bearer <key>`.
