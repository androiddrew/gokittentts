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

Without Docker, see [Plain Linux binary](#plain-linux-binary). For a Raspberry Pi 5, see [Raspberry Pi 5](#raspberry-pi-5).

## GPU option

The NVIDIA images need the NVIDIA Container Toolkit. CUDA 13 needs host driver 580 or newer.

```bash
make image-cuda12        # or image-cuda13; both bake and default to nano-fp32
docker run --gpus all -p 8880:8880 gokittentts:cuda12
```

The CUDA images are linux/amd64 only. They run every model with `device: cuda` ([`docker/config.cuda.yaml`](docker/config.cuda.yaml)), and load nano-fp32 before listening because it sets `preload: true`. Without a usable GPU, for example without `--gpus all`, the container exits with the CUDA error rather than falling back to the CPU. `BAKE_MODELS` and the models volume work as in the CPU image. Any model can set `preload: true` to load before `serve` listens.

For a plain binary, use the `gpu_cuda12` (or `gpu_cuda13`) ONNX Runtime archive (`make onnxruntime-gpu ORT_CUDA=12`), provide CUDA and cuDNN 9 on the loader path, and set `device: cuda` (and optionally `cuda_device_id`) for the model in `config.yaml`, or pass `say --device cuda`. If the CUDA provider can't be enabled, the model fails to load; it never falls back to the CPU. `make test-cuda` checks nano-fp32 on GPU 0. The GPU only speeds up nano-fp32 significantly. The int8 models keep their quantized ops on the CPU (see [docs/ORIGINAL_SPEC.md](docs/ORIGINAL_SPEC.md) section 11.4).

## Plain Linux binary

On Linux x86_64 or arm64, gokittentts is one binary plus two shared libraries: ONNX Runtime and espeak-ng.

| | Needed for | Package (Debian/Ubuntu) |
| --- | --- | --- |
| Go 1.26, gcc | building | `golang` from [go.dev](https://go.dev/dl/), `build-essential` |
| espeak-ng 1.51 or 1.52 | building (headers) and running | `libespeak-ng-dev` to build; `libespeak-ng1` and `espeak-ng-data` to run |
| ONNX Runtime 1.29.1 | running | the `onnxruntime-linux-x64` or `-aarch64` archive from GitHub, not a distro package |
| ffmpeg | running, optional | `ffmpeg`; without it, `opus`, `aac` and `flac` are refused and `mp3`, `wav` and `pcm` still work |
| CUDA 12 or 13, cuDNN 9 | running on a GPU, optional | see [GPU option](#gpu-option) |

The phoneme goldens were made with espeak-ng 1.51 (Ubuntu 24.04), and the CPU image uses 1.52 (Debian trixie). Other versions build, but they may phonemize some words differently.

Build:

```bash
sudo apt install build-essential libespeak-ng-dev
make build                                       # CGO_ENABLED=1 go build -tags espeak -o bin/gokittentts ./cmd/gokittentts
```

The `espeak` build tag is required. Without it, the binary still builds, but `say`, `serve` and `bench` exit with `built without espeak-ng; build with -tags espeak`.

Install ONNX Runtime (use `linux-aarch64` instead of `linux-x64` on arm64) and the runtime packages:

```bash
sudo apt install libespeak-ng1 espeak-ng-data ffmpeg
sudo mkdir -p /opt/onnxruntime
curl -fsSL https://github.com/microsoft/onnxruntime/releases/download/v1.29.1/onnxruntime-linux-x64-1.29.1.tgz \
  | sudo tar xz --strip-components=1 -C /opt/onnxruntime
sudo install -m 755 bin/gokittentts /usr/local/bin/
```

Always point gokittentts at the versioned file, `/opt/onnxruntime/lib/libonnxruntime.so.1.29.1`. It checks the version at startup and exits if it is wrong. ONNX Runtime is loaded by path at run time, so it doesn't need to be on the loader path, but espeak-ng does.

Try it, then serve with a config such as:

```bash
gokittentts say --onnxruntime-lib /opt/onnxruntime/lib/libonnxruntime.so.1.29.1 --out hello.wav "Hello from Go."
```

`say` keeps its models in `./models` unless you pass `--models-dir`. `serve` uses the config's `models_dir`.

```yaml
# /etc/gokittentts/config.yaml
listen: "127.0.0.1:8880"                  # ":8880" to serve the LAN; set KITTEN_API_KEY then
onnxruntime_lib: /opt/onnxruntime/lib/libonnxruntime.so.1.29.1
models_dir: /var/lib/gokittentts/models   # must be writable to download models
default_model: kitten-tts-mini-0.8
models:
  kitten-tts-mini-0.8:      { device: cpu, preload: true }
  kitten-tts-nano-0.8-fp32: { device: cpu }
log_format: text
```

```bash
gokittentts pull --config /etc/gokittentts/config.yaml    # optional: download the models now
gokittentts serve --config /etc/gokittentts/config.yaml
```

[`docker/config.yaml`](docker/config.yaml) lists every setting the images use. Check that the machine is fast enough with [`bench`](#bench).

## Raspberry Pi 5

The Pi 5 target is nano-fp32, by far the fastest model on a CPU (see [Models](#models)), so the Pi build bakes it and makes it the default. Use 64-bit Raspberry Pi OS or another arm64 Linux, and the active cooler: without it, the Pi throttles under sustained synthesis.

With Docker, build on the Pi itself, or on another machine with `binfmt` support for arm64:

```bash
make image-pi5                                   # gokittentts:pi5, linux/arm64, bakes and defaults to nano-fp32
docker run -d --restart unless-stopped -p 8880:8880 -v kitten-models:/var/lib/gokittentts/models gokittentts:pi5
docker save gokittentts:pi5 | ssh pi docker load    # if you built it elsewhere
```

`make image-pi5` is the CPU image built with `BAKE_MODELS` and `DEFAULT_MODEL` both set to `kitten-tts-nano-0.8-fp32`. `-e KITTEN_DEFAULT_MODEL=…` still overrides the default at run time.

Without Docker, follow [Plain Linux binary](#plain-linux-binary) on the Pi, with the `onnxruntime-linux-aarch64-1.29.1.tgz` archive, and set `default_model: kitten-tts-nano-0.8-fp32` with `preload: true` for it in the config.

Check that the Pi is real time with [`bench`](#raspberry-pi-5-1) before relying on it.

## macOS arm64 (best effort)

macOS on Apple silicon builds and runs on the CPU, but it isn't tested in CI or before releases. There is no CUDA, and the CoreML provider isn't used. These steps are expected to work but are untested:

```bash
brew install go espeak-ng ffmpeg
CGO_ENABLED=1 CGO_CFLAGS="-I$(brew --prefix)/include" CGO_LDFLAGS="-L$(brew --prefix)/lib" \
  go build -tags espeak -o bin/gokittentts ./cmd/gokittentts
mkdir -p third_party
curl -fsSL https://github.com/microsoft/onnxruntime/releases/download/v1.29.1/onnxruntime-osx-arm64-1.29.1.tgz | tar xz -C third_party
bin/gokittentts say --onnxruntime-lib third_party/onnxruntime-osx-arm64-1.29.1/lib/libonnxruntime.1.29.1.dylib \
  --out hello.wav "Hello from Go."
```

On macOS, the versioned library is `libonnxruntime.1.29.1.dylib`. If macOS refuses to load it because it came from the internet, run `xattr -d com.apple.quarantine` on it. Homebrew's espeak-ng is 1.52, so phonemes can differ slightly from the goldens. `serve`, `pull` and `bench` take the same flags and config as on Linux.

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
RELEASE=v0.1.0; mkdir -p bench/$RELEASE
bin/gokittentts bench --onnxruntime-lib third_party/onnxruntime-linux-aarch64-1.29.1/lib/libonnxruntime.so.1.29.1 \
  --model kitten-tts-nano-0.8-fp32 --runs 5 --out bench/$RELEASE/$(hostname)-nano-fp32-pi5.json
```

or the Pi image, which bakes nano-fp32 and makes it the default (see [Raspberry Pi 5](#raspberry-pi-5) above):

```bash
make image-pi5
RELEASE=v0.1.0; mkdir -p bench/$RELEASE
docker run --rm --hostname "$(hostname)" gokittentts:pi5 \
  bench --config /etc/gokittentts/config.yaml --runs 5 --out - > bench/$RELEASE/$(hostname)-nano-fp32-pi5.json
```

After the run, check that the Pi didn't throttle: `vcgencmd get_throttled` should print `throttled=0x0`. If it didn't print that, later runs were slowed, so fix the cooling and run again.

### CPU Docker image

The image's config runs every model on the CPU and defaults to mini:

```bash
docker run --rm --hostname "$(hostname)" gokittentts:cpu bench --config /etc/gokittentts/config.yaml --out - > bench-mini-cpu.json
docker run --rm --hostname "$(hostname)" -v kitten-models:/var/lib/gokittentts/models gokittentts:cpu \
  bench --config /etc/gokittentts/config.yaml --model kitten-tts-micro-0.8
```

A model the image didn't bake is downloaded before the run. Mount the models volume, as in the second command, so the download is kept. Don't pass `-t` with `--out -`: a TTY merges stderr into stdout and corrupts the JSON. `--hostname "$(hostname)"` records the machine's name instead of the container's ID.

### CUDA

With the CUDA images, which run every model on the GPU and default to nano-fp32:

```bash
docker run --rm --hostname "$(hostname)" --gpus all gokittentts:cuda12 bench --config /etc/gokittentts/config.yaml \
  --out - > bench-nano-fp32-cuda12.json
docker run --rm --hostname "$(hostname)" --gpus '"device=1"' gokittentts:cuda13 bench --config /etc/gokittentts/config.yaml \
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

### v0.1.0 results

Recorded at commit `f37842a` with 5 runs, in [`bench/v0.1.0/`](bench/v0.1.0/). The targets are RTF p95 < 0.8 and first audio p95 < 1 s.

| Target | Model | Machine | RTF p50 / p95 | First audio p50 / p95 | Gate |
| --- | --- | --- | --- | --- | --- |
| Raspberry Pi 5 | nano-fp32 | Pi 5 | not run yet | not run yet | pending |
| x86_64 CPU | mini | Ryzen 5 5600X, plain binary | 0.574 / 0.659 | 1.89 s / 3.86 s | **fail** (first audio) |
| CUDA 12 image | nano-fp32 | RTX 4090, `gokittentts:cuda12` | 0.039 / 0.080 | 0.18 s / 0.25 s | pass |
| CUDA 13 image | nano-fp32 | RTX 4090, `gokittentts:cuda13` | 0.024 / 0.047 | 0.10 s / 0.16 s | pass |

Mini is faster than real time on the Ryzen, but it can't start speaking within 1 s. At an RTF near 0.58, any first chunk over about 1.7 s of audio takes more than a second, and even "Hello from Go." takes 1.2 s.

### Reading the results

- **Noise.** Close other heavy programs, and use `--runs 5` or more for a release record. The p95 of 3 runs × 19 requests is about the third-worst sample, so one slow request moves it.
- **High first audio with a good RTF** means the first chunk takes long to synthesize. That is a sentence of up to 400 characters, so long opening sentences cost the most. Mini on a desktop CPU typically misses here: in one run on a Ryzen 5 5600X it had RTF p95 0.69 but first-audio p95 4 s. A smaller model or the GPU fixes it.
- **High RTF on the shortest requests** is fixed per-request overhead over little audio. The samples show whether short texts are what pushes the p95 up.
