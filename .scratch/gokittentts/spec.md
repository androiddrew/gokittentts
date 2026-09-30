# gokittentts: self-hosted, OpenAI-compatible KittenTTS in Go

Status: ready-for-agent

This is the definitive specification for gokittentts. It records the problem, the user stories and the decisions settled in the 2026-09-30 design review. `docs/ORIGINAL_SPEC.md` is the earlier design document, kept as reference data for things this spec doesn't repeat: model hashes and revisions, the tokenizer symbol table, the onnxruntime_go call table, the example config, the Go pitfalls, prototype code and the golden-vector script. Where the two conflict, this spec wins.

## Problem Statement

I run a homelab and want good local text-to-speech that my existing tools can use. KittenTTS 0.8 has small, good-sounding ONNX models, but the only way to use them is a Python package. The released 0.8.1 wheel pulls in misaki → spaCy → torch (112 packages) for a model that only needs ONNX Runtime and espeak-ng. It has no HTTP server, so clients like Open WebUI and the OpenAI SDK can't talk to it. Its public `generate()` skips text normalization entirely, so LLM chat replies full of markdown, money, dates, URLs and units come out garbled or silently deleted. It doesn't give me a clean way to pick CPU or GPU per model, a Docker image per device, or a Raspberry Pi 5 target that is actually real time.

## Solution

gokittentts is a single Go binary (plus Docker images) that serves the four KittenTTS 0.8 models through an OpenAI-compatible `POST /v1/audio/speech`, and through a CLI. Only the neural network runs in ONNX Runtime (through `onnxruntime_go`). Everything else is Go: the markdown pass, a normalizer ported from Python's `normalize_text` with fixes, sentence chunking, espeak-ng phonemization through cgo, tokenization, voice-style lookup, audio encoding, streaming and HTTP.

The phonemes and token ids match the Python reference token for token. Models are listed in a YAML config, picked per request, loaded on first use and downloaded from pinned Hugging Face revisions with SHA-256 checks. It runs on Linux x86_64 and arm64, on CPU, CUDA 12 or CUDA 13. It ships as a CPU image, two CUDA images and a plain binary. A `bench` subcommand gates releases on real-time targets, including a Raspberry Pi 5.

## User Stories

### Using it from OpenAI clients

1. As a homelab owner, I want to point an OpenAI TTS client at my server's `/v1/audio/speech`, so that my existing tools speak without code changes.
2. As an Open WebUI user, I want to pick gokittentts as the TTS provider, so that chat replies are read aloud locally.
3. As an OpenAI SDK user, I want `model: "tts-1"`, `"tts-1-hd"` or `"gpt-4o-mini-tts"` to work, so that client defaults don't break.
4. As an admin, I want OpenAI model names to map to a configured model through `model_aliases`, so that I control which Kitten model answers them.
5. As an OpenAI SDK user, I want OpenAI voice names (alloy, nova, onyx, …) to map to Kitten voices, so that client presets still produce speech.
6. As an admin, I want to edit the OpenAI-to-Kitten voice map in the config, so that I can tune the voices by ear.
7. As a client developer, I want to use Kitten voice names (Bella, Bruno, …) directly, so that I can pick the exact voice.
8. As a client developer, I want `expr-voice-*` keys to work as voices too, so that scripts written for the Python library keep working.
9. As a client developer, I want `voice` to also accept an object with an `id`, so that newer OpenAI client shapes work.
10. As a client developer, I want an unknown voice to return 400 listing the valid names, so that I can fix my request.
11. As a client developer, I want an unknown model to return 400, so that typos fail loudly.
12. As a client developer, I want errors in OpenAI's `{"error": {message, type, param, code}}` shape, so that my client's error handling works.
13. As a client developer, I want `mp3` as the default format, so that I get OpenAI's default behavior.
14. As a client developer, I want `wav` and `pcm` output, so that I can feed audio to local players or pipelines without decoding.
15. As a client developer, I want `pcm` to be raw 16-bit little-endian 24 kHz mono, as OpenAI's is, so that existing PCM consumers work.
16. As a client developer, I want `opus`, `aac` and `flac` when ffmpeg is available, so that I get the full OpenAI format set.
17. As a client developer, I want a clear 400 naming the supported formats when ffmpeg is missing, so that I know why a format failed.
18. As a client developer, I want `speed` from 0.25 to 4.0 to be accepted, so that OpenAI-valid requests never fail.
19. As a client developer, I want out-of-range `speed` to return 400, so that invalid requests are rejected the way OpenAI rejects them.
20. As an admin, I want the effective speed clamped to a configurable range (default 0.5–2.0), so that extreme values don't produce garbage audio.
21. As a client developer, I want `instructions` accepted and ignored, so that clients that send it don't break.
22. As a client developer, I want input over `max_input_chars` (default 4096) rejected, so that one request can't monopolize the model.
23. As a client developer, I want `GET /v1/models` to list the configured models and aliases, so that clients can discover them.
24. As a client developer, I want `GET /v1/voices` to list the Kitten names, keys and OpenAI map, so that I can build a voice picker.

### Streaming and latency

25. As a listener, I want audio to start playing within a second, so that spoken replies feel responsive.
26. As a client developer, I want raw chunked audio streaming by default, flushed after each synthesized chunk, so that playback starts before synthesis ends.
27. As a client developer, I want a streamed `wav` to begin with a header whose sizes are `0xFFFFFFFF`, so that players accept a WAV of unknown length.
28. As a client developer, I want `stream_format: "sse"` to send `speech.audio.delta` events with base64 audio and a closing `speech.audio.done` with usage, so that OpenAI SSE clients work.
29. As a listener, I want a long first sentence split at its first comma, so that the first model run is short and audio starts sooner.
30. As an admin, I want a client disconnect to cancel the remaining chunks and free the queue slot, so that abandoned requests don't waste compute.

### Speech quality on real input

31. As a listener, I want markdown from LLM replies read as prose, so that I don't hear asterisks, hashes or URLs from links.
32. As a listener, I want headings, list items and table rows read as separate sentences, so that structure comes through as pauses.
33. As a listener, I want code blocks skipped, so that I'm not read source code.
34. As a listener, I want inline code and bare URLs read aloud, so that important identifiers aren't lost.
35. As a listener, I want emoji removed, so that they don't turn into noise or dropped symbols.
36. As a listener, I want numbers, years, dates, times, money, percents, ordinals and versions read naturally, so that "2024 budget" is "twenty twenty-four budget".
37. As a listener, I want URLs and email addresses spelled out, not deleted, so that I hear what was written.
38. As a listener, I want titles like "Dr." expanded, so that abbreviations sound right.
39. As a listener, I want "$3.5 million" read as "three point five million dollars", so that money with a scale word is correct.
40. As a listener, I want "3 GB" read as "three gigabytes", so that units are spoken.
41. As a listener, I want "3.5" read as "three point five" even with normalization off, so that decimals survive phonemization.
42. As a client developer, I want `normalize: false` to turn off the normalizer, so that I can send pre-normalized text.
43. As a client developer, I want `markdown: false` to turn off the markdown pass, so that literal symbols reach the normalizer.
44. As a listener, I want plain text without markdown to pass through the markdown pass unchanged (apart from emoji), so that ordinary sentences aren't altered.
45. As the maintainer, I want every place the Go normalizer deliberately differs from Python recorded with a reason, so that deviations are visible and reviewable.
46. As the maintainer, I want one review pass at the end of the project that approves, edits or reverts each deviation, so that the final behavior is a deliberate choice.

### Parity with the Python reference

47. As the maintainer, I want phonemes and token ids to equal the Python reference for every golden chunk, so that the model receives exactly what it was trained on.
48. As the maintainer, I want the golden reference to use upstream `phonemizer` ≥ 3.4.0 and never `phonemizer-fork`, so that the goldens encode the decimal rule.
49. As the maintainer, I want the golden generator to refuse to run when the fork is installed, so that goldens can't be regenerated wrongly by accident.
50. As the maintainer, I want the espeak-ng and phonemizer versions recorded in the goldens, so that version drift explains failures.
51. As the maintainer, I want the style row chosen by the chunk's character count (capped at 399), so that voice style matches Python.
52. As a listener, I want each model's `speed_priors` applied, so that nano doesn't speak too fast.
53. As a listener, I want the last 5,000 samples of each chunk trimmed, so that chunks don't end in artifacts.

### Models and model store

54. As an admin, I want all four KittenTTS 0.8 models (mini, micro, nano-int8, nano-fp32) available, so that I can trade quality for speed.
55. As a client developer, I want to choose the model per request, so that different apps can use different models.
56. As an admin, I want models loaded on first use, so that startup is fast and unused models cost no memory.
57. As an admin, I want a missing model downloaded from a pinned Hugging Face revision and checked against a SHA-256, so that I get exactly the tested files.
58. As an admin, I want concurrent requests for the same missing model to share one download, so that it isn't fetched twice.
59. As an admin, I want a failed download to return 503 without a half-written file, so that a retry starts clean.
60. As an admin on an offline network, I want `download: false` with a 503 that suggests `gokittentts pull <name>`, so that I know how to get the model.
61. As an admin, I want `gokittentts pull` to prefetch models into the models directory, so that I can prepare offline hosts.
62. As an admin, I want to add a custom model with a repo and revision, downloaded with a warning and no hash check, so that I can try new models.
63. As an admin, I want each model's file names read from its `config.json`, so that the two nano repos with the same file name don't collide.
64. As an admin, I want each model's voices loaded from its own directory, so that style vectors always match the model.
65. As an admin, I want every loaded model checked against the expected inputs and outputs, so that a bad file fails at load time, not in the middle of a request.

### Devices and deployment

66. As an admin with an NVIDIA GPU, I want to set `device: cuda` per model, so that fast models run on the GPU.
67. As an admin, I want a failure to enable CUDA to be an error, never a silent CPU fallback, so that I know when the GPU isn't being used.
68. As an admin, I want a CPU image for amd64 and arm64, so that it runs on desktops, servers and a Pi 5.
69. As an admin, I want CUDA 12 and CUDA 13 images, so that I can match my host driver.
70. As an admin, I want the CUDA images to default to nano-fp32, so that the GPU gives real speedups out of the box.
71. As an admin, I want to bake models into an image with `BAKE_MODELS`, or build a slim image with none, so that I choose image size against first-request latency.
72. As an admin, I want downloaded models kept on a mounted volume, so that they survive container restarts.
73. As a Pi 5 owner, I want a documented build that bakes and defaults to nano-fp32, so that I get real-time speech on the Pi.
74. As a user who doesn't use Docker, I want a plain Linux binary with documented dependencies, so that I can run it directly.
75. As a macOS arm64 user, I want best-effort CPU build notes, so that I can try it locally.
76. As an admin, I want startup to check the ONNX Runtime version, so that a mismatched library fails immediately.

### Configuration and operations

77. As an admin, I want one YAML config with environment overrides for the API key, default model and listen address, so that containers are easy to configure.
78. As an admin, I want startup to fail on an unknown device, an unconfigured default model, a voice map entry naming a non-Kitten voice, or an inverted speed range, so that misconfiguration is caught early.
79. As an admin, I want an optional `KITTEN_API_KEY` Bearer token on `/v1/*`, compared in constant time, so that LAN clients must authenticate.
80. As an admin, I want a warning when auth is off and the server listens beyond loopback, so that I notice an open server.
81. As an admin, I want `/healthz` and `/metrics` open even with auth on, so that probes and scrapers work.
82. As an admin, I want one synthesis at a time per model with a bounded queue and 429 plus `Retry-After` on overflow, so that the server degrades predictably under load.
83. As an admin, I want a request timeout covering queue wait and synthesis, so that stuck requests end.
84. As an admin, I want one structured log line per request with model, voice, format, sizes, RTF, time to first audio, queue wait and status, so that I can debug and tune.
85. As an admin, I want Prometheus metrics for requests, synthesis time, RTF, time to first audio, queue depth, loaded models and downloads, so that I can graph the service.

### CLI and library

86. As a user, I want `gokittentts say` to write a WAV from text, so that I can test voices without a server.
87. As a Go developer, I want a public `kittentts` package (Engine, Model, Synthesize, Stream), so that I can embed TTS in my own program.
88. As a Go developer, I want `Stream` to yield one trimmed chunk at a time and honor context cancellation between chunks, so that I can stream audio myself.
89. As a Go developer, I want the library to have no queue, auth or encoding, so that I can add my own policy.

### Performance gates

90. As the maintainer, I want `gokittentts bench` to report p50/p95 RTF and time to first audio over a fixed corpus and exit non-zero on a miss, so that releases are gated on real-time targets.
91. As the maintainer, I want RTF < 0.8 and first audio < 1 s for the default model on each supported device, including nano-fp32 on a Pi 5, so that "real time" is measured, not assumed.
92. As the maintainer, I want benchmark results recorded per release, so that regressions are visible.

## Implementation Decisions

**Decision record** (settled 2026-09-30):

| Area | Decision |
| --- | --- |
| Purpose | Self-hosted service for a homelab and LAN clients |
| Platforms | Linux x86_64 and arm64 required; macOS arm64 best effort (CPU only, documented, not in CI) |
| Default model | `kitten-tts-mini-0.8`; the CUDA images default to `kitten-tts-nano-0.8-fp32` |
| Model selection | A YAML config lists models; the request's `model` field picks one; models load on first use |
| espeak-ng | Linked through cgo, behind a `Phonemizer` interface |
| Text normalization | A port of Python `normalize_text` plus fixes; on by default, and a request field turns it off |
| Markdown | A pass before normalization; on by default, and a request field turns it off |
| Normalizer deviations | Recorded in an overrides file; reviewed in one pass at the end of the project |
| API | OpenAI `POST /v1/audio/speech` and `GET /v1/models`, plus `/v1/voices`, `/healthz`, `/metrics`; extra fields `normalize` and `markdown`; `instructions` ignored |
| Voices | Kitten names, `expr-voice-*` keys and a configurable OpenAI-name map; unknown names return 400 |
| Formats | `wav`, `pcm`, `mp3` in pure Go (shine-mp3); `opus`, `aac`, `flac` through ffmpeg, and 400 if ffmpeg is missing |
| Streaming | Raw chunked audio by default; SSE with `stream_format: "sse"` |
| Speed | Accept 0.25–4.0; clamp to a configurable 0.5–2.0; then multiply by the model's speed prior |
| Concurrency | One run at a time per model, with a bounded queue; overflow returns 429 |
| Auth | Optional `KITTEN_API_KEY` Bearer token |
| Observability | slog JSON logs and Prometheus `/metrics` |
| Devices | CPU, CUDA 12, CUDA 13; no ROCm |
| Deployment | Docker images and a plain binary; `BAKE_MODELS` build arg plus ad-hoc Hugging Face downloads |
| Build/publish | Local `make` and `docker buildx`; CI-ready but no CI yet |
| Pi 5 gate | RTF < 0.8 and first audio < 1 s, checked by `bench` on the release checklist |
| Build order | A vertical slice first, then the milestones below |

**Runtime and pinned versions**

- `onnxruntime_go` v1.36.0 with ONNX Runtime 1.29.1. Always load the versioned library file, never the symlink, and check the runtime version at startup.
- Use a dynamic session so ONNX Runtime allocates the variable-length `waveform` and `duration` outputs.
- The model contract is the same for all four models: inputs `input_ids` int64 `[1, n]`, `style` float32 `[1, 256]` and `speed` float32 `[1]`; outputs `waveform` float32 `[samples]` and `duration` int64 `[n]`. It is asserted on every model load.
- Model files are pinned by Hugging Face revision and SHA-256 in a manifest shipped in the model store module. The revisions and hashes are the ones in `docs/ORIGINAL_SPEC.md` §3.1.

**Modules** (deep modules with small interfaces):

- **Public library (`kittentts`).** `NewEngine(ortLibPath, []ModelConfig)`, `Engine.Model(name)` (loads on first use), `Model.Voices()`, `Model.Synthesize(ctx, Request) ([]float32, error)`, `Model.Stream(ctx, Request) iter.Seq2[[]float32, error]`, `Engine.Close()`, and `SampleRate = 24000`.
    - `ModelConfig` holds the name, dir, device, CUDA device id and intra-op threads.
    - `Request` holds the text, voice, speed (already clamped), and the `Normalize` and `Markdown` flags.
    - The library owns the whole text-to-PCM pipeline. It has no queue, auth or encoding.
- **Text front end.** Markdown pass, normalizer, chunker, phonemizer and tokenizer. Each is a pure function of its input except the phonemizer, which sits behind `Phonemizer { Phonemize(text) (string, error) }`. The cgo implementation is the default; a subprocess implementation is a drop-in alternative.
- **Markdown pass.** goldmark with the GFM table extension. How each construct is spoken is set by the table in `docs/ORIGINAL_SPEC.md` §6.1.
- **Normalizer.** A port of Python's `normalize_text` (read-aloud mode), not `TextPreprocessor`, covering the substitution list in order and its helpers. Span tracking is not ported.
    - RE2 lookarounds and backreferences are rewritten as a match plus a Go check.
    - The first deliberate fixes are currency with a scale word, and units after numbers (ported from `TextPreprocessor`'s `expand_units` and `expand_scale_suffixes`).
- **Chunker.** A port of `chunk_text` (400 characters), `ensure_punctuation` and the sentence-boundary rules. Recorded deviation: a first chunk over 120 characters is split at its first comma.
- **Phonemizer.**
    - espeak-ng IPA with stress marks, with a process-wide mutex around espeak's global state.
    - The punctuation splitter is a hand-written rune scanner: `.` and `,` between two ASCII digits are ordinary characters (the phonemizer ≥ 3.4.0 rule).
- **Tokenizer.**
    - Unicode-aware re-tokenization (IPA marks are letters).
    - Symbol map built in order, with later duplicates overwriting earlier ones. Unknown symbols are dropped silently.
    - Output is wrapped as `[0] + ids + [10, 0]`.
- **Pipeline rules.**
    - Style row = min(rune count of the chunk, 399) of the voice's `(400, 256)` array.
    - Speed = clamp(requested) × `speed_priors[voice key]`.
    - Trim the last 5,000 samples of each chunk.
    - Output is float32 PCM at 24 kHz, mono.
- **Voices reader.** Reads `voices.npz` as a zip of `.npy` files, v1 or v2 headers, asserting `<f4`, C order and `(400, 256)`.
- **Audio.** WAV and PCM writers, the shine-mp3 encoder, and an ffmpeg pipe encoder for opus, aac and flac. Streamed WAV uses `0xFFFFFFFF` sizes.
- **Model store.**
    - Layout: `<models_dir>/<name>/<revision>/`, plus a `current` pointer.
    - Downloads are atomic: write a temporary file, verify, rename. Concurrent requests for one model share a single download.
    - `download: false` gives offline mode.
    - Custom models may set a repo and revision; they get no hash check and a warning is logged.
    - File names are read from each model's `config.json`.
- **Config.**
    - One YAML file, with `KITTEN_API_KEY`, `KITTEN_DEFAULT_MODEL` and `KITTEN_LISTEN` overrides.
    - Validated at startup: device, default model, voice map targets, speed range.
- **Server.**
    - Holds an engine behind an interface, so it can be tested with a fake.
    - Resolves models (name → alias) and voices (Kitten name → key → OpenAI map → 400).
    - Validates and clamps speed and enforces the input length limit.
    - Runs one worker and a bounded queue per model (429 with `Retry-After: 1`).
    - Applies the request timeout, auth, raw and SSE streaming, and disconnect cancellation.
    - Emits logs and metrics.
- **CLI.** One binary with `serve`, `say`, `pull` and `bench`.
- **Images.**
    - `cpu` (amd64 and arm64, Debian trixie-slim, bakes mini).
    - `cuda12` and `cuda13` (amd64, NVIDIA cuDNN runtime bases, bake and default to nano-fp32).
    - Multi-stage cgo build. `BAKE_MODELS` runs `pull` with the same manifest checks.

**API contracts.** `POST /v1/audio/speech` follows OpenAI's `CreateSpeechRequest`, plus `normalize` and `markdown`, both boolean and defaulting to true. The SSE events are `speech.audio.delta` (base64 audio) and `speech.audio.done`. In `done`, usage input tokens = token ids, output tokens = sum of durations, and total is their sum. The error bodies use OpenAI's shape.

**Milestones** (a vertical slice first):
1. Library and CLI slice: `say` writes correct WAVs with mini on CPU, and the golden tests pass.
2. OpenAI server: an OpenAI client plays audio from it.
3. Multi-model config and model store: requests for all four models work from an empty models directory.
4. Text front end: the normalizer suite passes, and every deviation is listed with `approved: false`.
5. Images and CUDA: all three images serve audio, and the CUDA images run nano-fp32 on the GPU.
6. Performance and platforms: the bench gates pass on a Pi 5 (nano-fp32) and on x86_64 (mini).
7. Normalizer review: every override is approved or removed.

## Testing Decisions

**What a good test is here.** It exercises behavior through a public seam and asserts on outputs a user or client could observe: HTTP status codes, headers and bodies; PCM samples and their invariants; phoneme strings and token ids. It never asserts on private helpers, regexes or intermediate structs. Audio can't be compared byte for byte, because the graphs contain random ops, so the model-level tests assert invariants.

**Seams** (three, agreed with the owner):

1. **HTTP API.** `httptest` against the server, with a fake engine in place of the real one. It covers:
    - every error code and error body shape;
    - model alias and voice resolution;
    - speed validation and clamping;
    - input length limits;
    - auth on and off, and open `/healthz` and `/metrics`;
    - 429 on a full queue, and the request timeout;
    - raw streaming chunks and the streamed WAV header;
    - SSE event shapes and usage;
    - ffmpeg-format rejection when ffmpeg is disabled;
    - disconnect cancelling the remaining chunks;
    - `/v1/models` and `/v1/voices`;
    - config validation errors, surfaced at startup.

    No native libraries needed.
2. **Public library.** `Engine` → `Model` → `Synthesize`/`Stream` against real models and ONNX Runtime, behind the `native` build tag. It covers:
    - the model contract on load, for every model in the models directory;
    - `len(waveform) == sum(duration) × 600`;
    - non-empty trimmed output with RMS > 0.01;
    - the nano speed prior (0.8 for Bella, 0.9 for Hugo, 1.0 for mini);
    - voices loaded from the model's own directory;
    - `Stream` stopping on context cancellation between chunks;
    - a CUDA append failure being an error;
    - model store downloads and hash checks (with a local HTTP fixture for failure cases).

    `bench` also runs here.
3. **Text front end.** The markdown → normalize → chunk → phonemize → tokenize pipeline, compared with golden files from the Python reference:
    - Phonemes and ids equal the phonemizer golden, including decimals, punctuation, quotes, dashes, ellipses and abbreviations. The espeak-ng and phonemizer versions are recorded in the golden, and this part needs espeak-ng.
    - The normalizer output equals the Python golden over the corpus of about 300 sentences, unless the case is in the overrides file, in which case it equals the override's `expected`.
    - The markdown pass is table-driven per construct, with a property test that plain text passes through unchanged apart from emoji.
    - The chunker runs the ported Python tests plus the 400-character split, missing terminal punctuation, empty input and the short-first-chunk rule.
    - The punctuation scanner covers decimal and thousands separators, trailing periods, runs of marks, and leading and trailing marks.

Small leaf behaviors that can't be reached cleanly through a seam get small table tests of their own: voices-file header parsing, WAV/PCM byte layout, and an mp3 that decodes back to a plausible length.

**Build tags.** `go test ./...` runs anywhere without native libraries. Tests needing ONNX Runtime, espeak-ng or models use `//go:build native`, and `make test-native` runs everything. The cgo espeak-ng backend builds only with `-tags espeak` (or `native`), so every build of the binary passes `-tags espeak`; `make build` does.

**Golden generation.** `make golden` runs the Python reference script with upstream `phonemizer` ≥ 3.4.0, and the script refuses to run if `phonemizer-fork` is present. It uses KittenTTS 0.8.1's `onnx_model.py` and `preprocess.py`, and points phonemizer at the system `libespeak-ng.so.1`.

**Prior art.** The repo has no code yet. The prototype's tokenizer and phonemizer passed the golden comparison and are the starting point. The golden-generator script in `docs/ORIGINAL_SPEC.md` Appendix A is the reference for fixtures.

## Out of Scope

- Training or fine-tuning.
- Languages other than `en-us`.
- Bit-exact audio reproduction.
- ROCm/MIGraphX, CoreML and TensorRT execution providers.
- OpenAI `instructions` and custom voice cloning.
- CI pipelines (the project is CI-ready, but nothing is set up).
- Converting the int8 models to fp32/fp16 variants. That is a separate project.
- Upstream KittenTTS packaging fixes (the misaki/phonemizer-fork issue).

## Further Notes

- Speed comes from quantization more than from model size. Dynamic int8 ops stay on the CPU even with CUDA (mini: 559 CPU nodes, 503 inserted copies). That's why the CUDA images and the Pi default to nano-fp32.
- Risks to watch:
    - espeak-ng version drift changes the ids. Mitigation: the version is recorded in the goldens and the images pin the package.
    - `onnxruntime_go` and ONNX Runtime can drift apart. Mitigation: pin both and check the version at startup.
    - The normalizer port is large, about 1,200 lines of Python regex.
    - Multi-arch cgo builds under QEMU are slow.
    - shine-mp3 quality is below LAME.
    - The default OpenAI voice map is arbitrary.
- License notes: shine-mp3 is LGPL-2.0, ffmpeg's license depends on the build, and the models are Apache-2.0. Record them in the image's licenses directory.
