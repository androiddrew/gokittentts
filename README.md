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

## Bench

`bench` checks whether a model is fast enough to speak in real time on a given machine. It synthesizes a fixed corpus of 19 requests ([`internal/bench/corpus.txt`](internal/bench/corpus.txt)), from "Yes." to multi-sentence chat replies with markdown, money, dates and units. The corpus is built into the binary, and the markdown pass and normalizer are on, as the server has them by default. One untimed warm-up request runs first, so ONNX Runtime's first-run setup isn't measured. Then each request is timed, `--runs` times over (3 by default), and `bench` reports two measures per request:

- **Real-time factor (RTF)**: synthesis time divided by the length of the audio. 0.5 means a 4 s reply takes 2 s to make.
- **Time to first audio**: from the request to its first chunk of audio, about a sentence. This is how long a listener waits before speech starts.

It prints the p50 and p95 of each and exits 1 when either p95 is at or above its target. The targets are RTF 0.8 and first audio 1 s, the release gate in the spec. Every request counts equally, so a one-word reply weighs as much as a long one.

```text
kitten-tts-nano-0.8-fp32 on cpu, voice Bruno: 19 texts × 3 runs (corpus 009fb35cf3bc)
                     p50      p95   target
  RTF              0.048    0.055   < 0.80
  first audio     0.172s   0.386s  < 1.00s
PASS
```

| Flag | Default | |
| --- | --- | --- |
| `--onnxruntime-lib` | | the versioned `libonnxruntime.so.1.29.1`; required without `--config` |
| `--config` | | take the library, models directory, default model and its device, CUDA device and threads from a config file; flags still override |
| `--model` | `kitten-tts-mini-0.8` | the model to measure |
| `--models-dir` | `models` | where models live; a missing pinned model is downloaded into it |
| `--device`, `--cuda-device-id` | `cpu`, `0` | `cuda` needs a CUDA build of ONNX Runtime |
| `--intra-op-threads` | `0` | ONNX Runtime's intra-op threads; 0 is its default |
| `--voice` | `Bruno` | |
| `--runs` | `3` | passes over the corpus; more runs give steadier percentiles |
| `--max-rtf`, `--max-first-audio` | `0.8`, `1s` | the targets |
| `--out` | | write the JSON record to a file, or to stdout with `-` (the summary then goes to stderr) |

Exit status: 0 when both targets are met, 1 when one is missed or the run fails. The last line on stderr says which: `bench: missed the targets` or the error.

### Plain binary on x86_64 or arm64

Build the binary and fetch ONNX Runtime as in [CPU option](#cpu-option), then:

```bash
ORT=third_party/onnxruntime-linux-x64-1.29.1/lib/libonnxruntime.so.1.29.1   # linux-aarch64 on arm64
bin/gokittentts bench --onnxruntime-lib $ORT                                  # mini, the default model
bin/gokittentts bench --onnxruntime-lib $ORT --model kitten-tts-nano-0.8-fp32
```

Models not in `--models-dir` are downloaded first, outside the timing. To compare all four models on one machine:

```bash
for m in kitten-tts-mini-0.8 kitten-tts-micro-0.8 kitten-tts-nano-0.8-int8 kitten-tts-nano-0.8-fp32; do
  bin/gokittentts bench --onnxruntime-lib $ORT --model $m --out bench-$m-cpu.json
done
```

The loop keeps going after a miss, and each record says whether that model passed.

### Raspberry Pi 5

The Pi 5 target is nano-fp32. Benchmark the binary, built natively on the Pi with the `linux-aarch64` ONNX Runtime archive:

```bash
bin/gokittentts bench --onnxruntime-lib third_party/onnxruntime-linux-aarch64-1.29.1/lib/libonnxruntime.so.1.29.1 \
  --model kitten-tts-nano-0.8-fp32 --runs 5 --out bench-nano-fp32-pi5.json
```

or the CPU image that bakes nano-fp32. `KITTEN_DEFAULT_MODEL` picks the model, as it does for `serve`:

```bash
make image-cpu BAKE_MODELS=kitten-tts-nano-0.8-fp32 IMAGE_PLATFORMS=linux/arm64
docker run --rm -e KITTEN_DEFAULT_MODEL=kitten-tts-nano-0.8-fp32 gokittentts:cpu \
  bench --config /etc/gokittentts/config.yaml --runs 5 --out - > bench-nano-fp32-pi5.json
```

Cooling matters: a Pi 5 without a fan or heatsink throttles under sustained load, and later runs get slower. Use the active cooler, and check `vcgencmd get_throttled` (it should print `throttled=0x0`) after the run.

### CPU Docker image

The image's config runs every model on the CPU and defaults to mini:

```bash
docker run --rm gokittentts:cpu bench --config /etc/gokittentts/config.yaml --out - > bench-mini-cpu.json
docker run --rm -v kitten-models:/var/lib/gokittentts/models gokittentts:cpu \
  bench --config /etc/gokittentts/config.yaml --model kitten-tts-micro-0.8
```

A model the image didn't bake is downloaded before the run. Mount the models volume, as in the second command, so the download is kept. Don't pass `-t` with `--out -`: a TTY merges stderr into stdout and corrupts the JSON.

### CUDA

With the CUDA images, which run every model on the GPU and default to nano-fp32:

```bash
docker run --rm --gpus all gokittentts:cuda12 bench --config /etc/gokittentts/config.yaml \
  --out - > bench-nano-fp32-cuda12.json
docker run --rm --gpus '"device=1"' gokittentts:cuda13 bench --config /etc/gokittentts/config.yaml \
  --out - > bench-nano-fp32-cuda13.json
```

With a plain binary, use the GPU build of ONNX Runtime and have CUDA and cuDNN 9 on the loader path (see [GPU option](#gpu-option)):

```bash
make onnxruntime-gpu ORT_CUDA=12
bin/gokittentts bench --onnxruntime-lib third_party/onnxruntime-linux-x64-gpu_cuda12-1.29.1/lib/libonnxruntime.so.1.29.1 \
  --model kitten-tts-nano-0.8-fp32 --device cuda --cuda-device-id 0
```

If the CUDA provider can't be enabled, `bench` fails with the CUDA error instead of measuring the CPU. Only nano-fp32 gains much from the GPU, because the int8 models keep their quantized ops on the CPU.

### Your own config and targets

`--config` benchmarks a model exactly as `serve` would run it, with the same device, `cuda_device_id` and `intra_op_threads`. `--model` must be one the config lists. The targets are only defaults, so you can try a stricter gate or a slower machine:

```bash
bin/gokittentts bench --config config.yaml --model kitten-tts-micro-0.8
bin/gokittentts bench --onnxruntime-lib $ORT --max-rtf 0.5 --max-first-audio 500ms
bin/gokittentts bench --onnxruntime-lib $ORT --model kitten-tts-nano-0.8-fp32 --intra-op-threads 2
```

The last command shows how a model does with fewer cores, for example when it shares the machine with other services.

### Recording results per release

Every release records one run per supported target. The targets are nano-fp32 on a Pi 5, mini on x86_64 and the CUDA images' default. Keep the records together, named by release, host, model and device:

```bash
mkdir -p bench/v0.1.0
bin/gokittentts bench --onnxruntime-lib $ORT --runs 5 --out bench/v0.1.0/$(hostname)-mini-cpu.json
```

Each record has the date, the gokittentts commit (`-dirty` for an uncommitted tree, or the image's `VCS_REF`), the host, OS and architecture, the ONNX Runtime version, the model, device and voice, the targets, the percentiles, `pass` and every sample. It also has `corpus_sha256`: two records are comparable only when it matches, so changing the corpus starts a new baseline. To compare releases:

```bash
jq -r '[.revision[:12], .host, .model, .device, .rtf.p95, .first_audio_seconds.p95, .pass] | @tsv' bench/*/*.json
jq '.samples | sort_by(-.first_audio_seconds) | .[:3]' bench/v0.1.0/$(hostname)-mini-cpu.json   # the slowest requests
```

`text_index` in a sample is the request's position in the corpus, not counting comments.

### Reading the results

- **Noise.** Close other heavy programs, and use `--runs 5` or more for a release record. The p95 of 3 runs × 19 requests is about the third-worst sample, so one slow request moves it.
- **High first audio with a good RTF** means the first chunk takes long to synthesize. That is a sentence of up to 400 characters, so long opening sentences cost the most. Mini on a desktop CPU typically misses here: in one run on a Ryzen 5 5600X it had RTF p95 0.69 but first-audio p95 4 s. A smaller model or the GPU fixes it.
- **High RTF on the shortest requests** is fixed per-request overhead over little audio. The samples show whether short texts are what pushes the p95 up.
