# gokittentts — Specification

Self-hosted, OpenAI-compatible text-to-speech in Go, running the KittenTTS 0.8 ONNX models through `github.com/yalue/onnxruntime_go`.

As of 2026-09-30. Module: `github.com/androiddrew/gokittentts`. Shared, commentable copy: https://claude.ai/code/artifact/8e293774-6f99-4c95-bf86-1ecdd7936644

## 1. Overview

gokittentts is a self-hosted TTS service for a homelab. It serves the four KittenTTS 0.8 models through an OpenAI-compatible HTTP API and a CLI. It runs on Linux x86_64 and arm64, on CPU or NVIDIA CUDA. Only the neural network runs in ONNX Runtime. Everything else is Go: normalizing text for speech, phonemizing it with espeak-ng, tokenizing, looking up voice styles, encoding audio and serving HTTP.

**Goals**

- Accurate-sounding speech from real-world input, including LLM chat replies with markdown, numbers, money, dates and URLs.
- Drop-in compatibility with OpenAI TTS clients (`POST /v1/audio/speech`).
- Token-for-token parity with the Python `kittentts` 0.8.1 phonemizer and tokenizer.
- Every model is selectable per request; mini is the default.
- Real time on a Raspberry Pi 5 with a suitable model (section 12).
- Docker images for CPU (amd64 and arm64), CUDA 12 and CUDA 13, plus a plain binary.

**Non-goals**

- Training or fine-tuning.
- Languages other than `en-us`.
- Bit-exact audio: the graphs contain random ops.
- ROCm/MIGraphX, CoreML and TensorRT. The device abstraction leaves room for them, but none is planned.
- OpenAI features Kitten can't honor: `instructions` and custom voice cloning.

### 1.1 Decision record

These decisions were settled in a design review on 2026-09-30.

| Area | Decision |
| --- | --- |
| Purpose | Self-hosted service (homelab, LAN clients) |
| Platforms | Linux x86_64 and arm64 are required. macOS arm64 is best effort: CPU only, documented, not in CI |
| Default model | `kitten-tts-mini-0.8`. The CUDA images default to `kitten-tts-nano-0.8-fp32` |
| Model selection | A YAML config lists models; the request's `model` field picks one; models load on first use |
| espeak-ng | Linked through cgo, behind a `Phonemizer` interface |
| Text normalization | A port of Python `normalize_text` plus fixes. **On by default**, and a request field turns it off |
| Markdown | A markdown pass before normalization. **On by default**, and a request field turns it off |
| Normalizer deviations | Recorded in `testdata/normalize_overrides.yaml`; reviewed in one pass at the end of the project |
| API | OpenAI-compatible `POST /v1/audio/speech` and `GET /v1/models`, plus `/v1/voices`, `/healthz` and `/metrics`. Extra JSON fields `normalize` and `markdown`. `instructions` is ignored |
| Voices | Kitten names, `expr-voice-*` keys and a configurable OpenAI-name map. Unknown names return 400 |
| Formats | `wav`, `pcm` and `mp3` in pure Go (`shine-mp3`). `opus`, `aac` and `flac` go through `ffmpeg`, and return 400 if it is missing |
| Streaming | Raw chunked audio by default. SSE when `stream_format: "sse"` |
| Speed | Accepts 0.25–4.0 as OpenAI does; the effective value is clamped to a configurable 0.5–2.0 |
| Concurrency | One run at a time per model, with a bounded queue; overflow returns 429 |
| Auth | An optional `KITTEN_API_KEY` Bearer token |
| Observability | slog JSON logs and a Prometheus `/metrics` |
| Devices | CPU, CUDA 12 and CUDA 13. No ROCm |
| Deployment | Docker images and a plain binary. `BAKE_MODELS` build arg, plus ad-hoc Hugging Face downloads |
| Build/publish | Local `make` and `docker buildx`; CI-ready but no CI yet |
| Pi 5 gate | RTF < 0.8 and first audio < 1 s, checked by `gokittentts bench` on the release checklist |
| Build order | A vertical slice first (section 14) |

## 2. Model artifacts

The Hugging Face repo [KittenML/kitten-tts-mini-0.8](https://huggingface.co/KittenML/kitten-tts-mini-0.8) (80M params, Apache-2.0) holds three files that matter. The facts below come from loading the graph with the `onnx` Python package on 2026-09-30.

| File | Contents |
| --- | --- |
| `config.json` | `type: ONNX2`, `model_file`, `voices`, `speed_priors`, `voice_aliases` |
| `kitten_tts_mini_v0_8.onnx` | 78.3 MB; IR 9, opset 20; producer `onnx.quantize` |
| `voices.npz` | 8 arrays, each `(400, 256) float32` (a zip of `.npy` files) |

**Graph inputs and outputs** (identical for all four models)

| Name | Direction | Type | Shape |
| --- | --- | --- | --- |
| `input_ids` | input | int64 | `[1, sequence_length]` |
| `style` | input | float32 | `[1, 256]` |
| `speed` | input | float32 | `[1]` |
| `waveform` | output | float32 | `[num_samples]` (dynamic) |
| `duration` | output | int64 | `[n]` (one per input token; unused by Python) |

**Graph properties that shape the design**

- Mini is dynamically int8-quantized: 135 `MatMulInteger`, 74 `ConvInteger`, 125 `DynamicQuantizeLinear` and 6 `com.microsoft:DynamicQuantizeLSTM` nodes. This matters for GPU placement (section 11.4).
- Every model contains `RandomNormalLike` and `RandomUniformLike`, so two runs on the same input produce different samples. Tests cannot compare audio byte-for-byte.
- Both outputs have dynamic length, so outputs cannot be preallocated.
- `len(waveform) == sum(duration) × 600` held exactly for every model on CPU and CUDA. It's a free test invariant.

**Voices** (display name to `voices.npz` key, from `config.json`)

| Name | Key | Name | Key |
| --- | --- | --- | --- |
| Bella | expr-voice-2-f | Jasper | expr-voice-2-m |
| Luna | expr-voice-3-f | Bruno | expr-voice-3-m |
| Rosie | expr-voice-4-f | Hugo | expr-voice-4-m |
| Kiki | expr-voice-5-f | Leo | expr-voice-5-m |

The voice *names* are shared, but each model ships its own `voices.npz`: the SHA-256 hashes differ between mini, micro and nano. Style vectors must always come from the same model directory as the `.onnx` file.

## 3. Model variants

All four KittenTTS 0.8 models share the same interface. Their inputs, outputs, voice keys, `(400, 256)` voice arrays and the 600-samples-per-duration-frame rule are identical, all checked on 2026-09-30. The prototype ran each one on CPU and CUDA and produced speech-like audio: RMS 0.12–0.14 and about 1,500 zero-crossings per second.

| Model | Size | Quantization | Speed priors | CPU warm run | CUDA warm run |
| --- | --- | --- | --- | --- | --- |
| [kitten-tts-mini-0.8](https://huggingface.co/KittenML/kitten-tts-mini-0.8) | 78.3 MB | int8 dynamic | none | 2.82 s | 1.79 s |
| [kitten-tts-micro-0.8](https://huggingface.co/KittenML/kitten-tts-micro-0.8) | 41.4 MB | int8 dynamic | none | 1.77 s | 1.03 s |
| [kitten-tts-nano-0.8-int8](https://huggingface.co/KittenML/kitten-tts-nano-0.8-int8) | 24.4 MB | int8 dynamic | 0.8, or 0.9 for `expr-voice-4-m` | 1.76 s | 0.91 s |
| [kitten-tts-nano-0.8-fp32](https://huggingface.co/KittenML/kitten-tts-nano-0.8-fp32) | 56.8 MB | none (float32) | 0.8, or 0.9 for `expr-voice-4-m` | **0.27 s** | **0.06 s** |

Timings are for one sentence of about 5 s of audio (77 tokens). Each is the fifth warm run of `DynamicAdvancedSession.Run` with ONNX Runtime 1.29.1 on a Ryzen 5 5600X (6 cores) and an RTX 4090.

- **Read file names from `config.json`.** The two nano repos use the same file name, `kitten_tts_nano_v0_8.onnx`, but the contents differ.
- **Apply `speed_priors`.** The Python rule is `speed = requested_speed × speed_priors.get(voice_key, 1.0)`, looked up after the alias is resolved to its `expr-voice-*` key. Skipping it makes nano speak noticeably too fast.
- **`KittenML/kitten-tts-nano-0.8` redirects** to `kitten-tts-nano-0.8-fp32` (HTTP 307). It is also the Python library's default model.
- **Speed is mostly about quantization, not size.** Dynamic int8 re-quantizes activations on every call, and none of those ops run on CUDA. The float32 nano has no such ops, so the whole graph runs on the GPU: it is 10× faster than mini on CPU and 30× faster on GPU. That is why the CUDA images default to nano-fp32.

### 3.1 Pinned revisions

`internal/modelstore` ships this manifest. Downloads use `https://huggingface.co/<repo>/resolve/<revision>/<file>` and are verified against these hashes.

| Model | Repo revision | File | SHA-256 |
| --- | --- | --- | --- |
| kitten-tts-mini-0.8 | `c02725660cea441db4c383af69f1f26f5cd00947` | `config.json` | `6b160bc9b19e24ecb21e84bc14f8a7da21fdf47ec72d42450bc5cf514b61804a` |
| | | `kitten_tts_mini_v0_8.onnx` | `0f5bbae4fc4800c98dbc544a87ecfa79510de2fb8222db30d12e5bfe9177df91` |
| | | `voices.npz` | `40ad2638952b77b7b2f30127e2608e169fc69dd256b53bd8aaa3409a33193c42` |
| kitten-tts-micro-0.8 | `1ccf72b2c2048fd17efac7de2fab32d10e225084` | `config.json` | `1f0bd2208348f9211cb0da64fcd1536eb28228571cc6b09e767eb6e203a0a532` |
| | | `kitten_tts_micro_v0_8.onnx` | `95481626fee1ba70ce683e69c534fc7cb38433c46ce42d3abbeafb4b9f1a4123` |
| | | `voices.npz` | `112710c1be8ad0e967c190fb0fd95cbe5848ec4791b93209f20b28b7da20dac1` |
| kitten-tts-nano-0.8-int8 | `84781d74e29ee25217551556398b42f80593a813` | `config.json` | `b66006ccbeccd4de5fc3c9272059c47f5725df7215fd889785c03602652fab64` |
| | | `kitten_tts_nano_v0_8.onnx` | `f7b0afcbee92870b32b8e0276d855b954dc25470c9f051b376ac7eee537c76fc` |
| | | `voices.npz` | `8aa7cee235abb0739cb51e6559685f65a4dacd95568833d05699b1633f519b3f` |
| kitten-tts-nano-0.8-fp32 | `7a1db645b1f3ab9420761d87428e042b9cec3f26` | `config.json` | `b66006ccbeccd4de5fc3c9272059c47f5725df7215fd889785c03602652fab64` |
| | | `kitten_tts_nano_v0_8.onnx` | `320564d2615f235de972ca27a7f39551c94185cfa24ca85b07a29084135f1e5e` |
| | | `voices.npz` | `8aa7cee235abb0739cb51e6559685f65a4dacd95568833d05699b1633f519b3f` |

The two nano repos share `config.json` and `voices.npz`; only the `.onnx` differs.

## 4. Reference pipeline

The Go port must reproduce these steps from [`kittentts/onnx_model.py`](https://github.com/KittenML/KittenTTS/blob/main/kittentts/onnx_model.py) and [`preprocess.py`](https://github.com/KittenML/KittenTTS/blob/main/kittentts/preprocess.py) (package 0.8.1). gokittentts adds two stages in front (markdown, normalization) that Python either lacks or leaves off. Steps 4–8 run once per chunk.

1. **Markdown pass** (gokittentts, on by default; section 6.1).
2. **Normalize** (gokittentts, on by default; section 6.2). Python's public `KittenTTS.generate()` passes `clean_text=False`, which skips this step. With `clean_text=True`, Python runs `TextPreprocessor`, not the better `normalize_text`.
3. **Chunk.** `chunk_text(text, max_len=400)` splits on `.`, `!` or `?` followed by whitespace or end of text. It skips abbreviations, decimals like `3.5` and `a.m.`/`p.m.`. Sentences over 400 characters are split at word boundaries. `ensure_punctuation` appends `,` to any chunk not ending in `.!?,;:`. gokittentts also splits a first chunk over 120 characters at its first comma, to meet the time-to-first-audio target (section 12). This is a recorded deviation.
4. **Phonemize.** Use phonemizer's `EspeakBackend(language="en-us", preserve_punctuation=True, with_stress=True)`, which produces espeak-ng IPA with stress marks.
    - Punctuation from the set `;:,.!?¡¿—…"«»“”(){}[]` is stripped before espeak and re-inserted after. **Exception:** `.` and `,` are not punctuation when they sit between two digits, so `3.5` reaches espeak whole and is read "three point five".
    - **The reference is upstream `phonemizer` ≥ 3.4.0**, the package `main` depends on. The released 0.8.1 wheel doesn't list `phonemizer`; it gets `phonemizer-fork` 3.3.2 transitively through its (unused) `misaki[en]` dependency. That fork lacks the decimal exception: on `$3.5 million, or 1,500 dollars.` it produces "θɹˈiː.fˈaɪv" and "wˈʌn,fˈaɪvhˈʌndɹɪd", where 3.4.0 produces "θɹˈiː pɔɪnt fˈaɪv" and "wˈʌn θˈaʊzənd fˈaɪvhˈʌndɹɪd" (checked 2026-09-30). gokittentts deliberately follows 3.4.0. With normalization on, digits are already words, so the difference only shows up with `normalize: false`.
    - Verified with espeak-ng 1.51: `Hello, world! This high-quality TTS model runs without a GPU.` becomes `həlˈoʊ, wˈɜːld! ðɪs hˈaɪkwˈɔlᵻɾi tˌiːtˌiːˈɛs mˈɑːdəl ɹˈʌnz wɪðˌaʊt ɐ dʒˌiːpˌiːjˈuː.`
5. **Re-tokenize.** Python `re.findall(r"\w+|[^\w\s]", phonemes)`, joined with single spaces.
6. **Map to ids.** The symbol list is `$`, then `;:,.!?¡¿—…"«»"" ` (16 chars including a trailing space), then `A–Z`, `a–z`, then a fixed IPA string. The dict lets later duplicates overwrite earlier ones: 178 positions, 175 unique symbols, and `"` ends at id 14. Characters not in the dict are **silently dropped**. The result is wrapped as `[0] + ids + [10, 0]`; id 10 is `…`.
7. **Pick the style vector.** `ref_id = min(len(chunk), 399)` counts the characters of the **chunk text, not the phonemes**. The style is row `ref_id` of the voice's `(400, 256)` array. The speed is `[clamp(requested) × speed_priors[voice]]`.
8. **Run and trim.** Run the session, take `waveform` and **drop its last 5,000 samples** (≈0.21 s). Emit or concatenate the chunks. The output is float32 PCM at 24,000 Hz, mono.

## 5. onnxruntime_go usage

Use `github.com/yalue/onnxruntime_go` **v1.36.0** with ONNX Runtime **1.29.1**, and use a `DynamicAdvancedSession` so ONNX Runtime allocates outputs whose length is only known after the run. The prototype produced correct audio on CPU and CUDA with this setup, and its token ids matched Python on every golden chunk.

**Version lockstep.** onnxruntime_go ships the C API headers for one ONNX Runtime release, and v1.36.0 targets 1.29.x. Load the versioned library file (`libonnxruntime.so.1.29.1`), not the symlink. Check `ort.GetVersion()` at startup. The [examples repo](https://github.com/yalue/onnxruntime_go_examples) still pins v1.25.0; copy its idioms, not its version.

**ONNX Runtime 1.29.1 release assets used**

| Target | Archive |
| --- | --- |
| Linux x86_64 CPU | `onnxruntime-linux-x64-1.29.1.tgz` |
| Linux arm64 CPU | `onnxruntime-linux-aarch64-1.29.1.tgz` |
| Linux x86_64 CUDA 12 | `onnxruntime-linux-x64-gpu_cuda12-1.29.1.tgz` |
| Linux x86_64 CUDA 13 | `onnxruntime-linux-x64-gpu_cuda13-1.29.1.tgz` |
| macOS arm64 (best effort) | `onnxruntime-osx-arm64-1.29.1.tgz` |

**API calls used**

| Step | Call | Notes |
| --- | --- | --- |
| Load library | `ort.SetSharedLibraryPath(p)`, `ort.InitializeEnvironment()` | Once per process; `ort.WithLogLevelVerbose()` shows node placement |
| Model contract | `ort.GetInputOutputInfo(modelPath)` | Assert the 3 inputs and 2 outputs from section 2 on every model load |
| Options | `ort.NewSessionOptions()`, `SetIntraOpNumThreads(n)` | One per model; destroy after session creation |
| CUDA | `ort.NewCUDAProviderOptions()`, `Update({"device_id": "N"})`, `opts.AppendExecutionProviderCUDA(co)` | An append failure is an error, never a silent CPU fallback |
| Session | `ort.NewDynamicAdvancedSession(path, []string{"input_ids","style","speed"}, []string{"waveform","duration"}, opts)` | One per loaded model |
| Inputs | `ort.NewTensor(ort.NewShape(1, n), ids)`, `NewShape(1,256)`, `NewShape(1)` | Per chunk; `Destroy()` after `Run` |
| Run | `sess.Run(inputs, []ort.Value{nil, nil})` | Nil outputs are allocated by ONNX Runtime; `Destroy()` them |
| Read | `outputs[0].(*ort.Tensor[float32]).GetData()` | v1.36.0 copies auto-allocated outputs into Go memory, so the slice outlives `Destroy()` |

**Core run, as prototyped**

```go
ids := tokenize(phonemize(chunk))
idsT, _ := ort.NewTensor(ort.NewShape(1, int64(len(ids))), ids)
defer idsT.Destroy()
row := min(utf8.RuneCountInString(chunk), 399)
styleT, _ := ort.NewTensor(ort.NewShape(1, 256), voice[row*256:(row+1)*256])
defer styleT.Destroy()
speedT, _ := ort.NewTensor(ort.NewShape(1), []float32{speed})
defer speedT.Destroy()

outs := []ort.Value{nil, nil}
if err := sess.Run([]ort.Value{idsT, styleT, speedT}, outs); err != nil {
	return nil, err
}
defer outs[0].Destroy()
defer outs[1].Destroy()
wave := outs[0].(*ort.Tensor[float32]).GetData()
wave = wave[:max(0, len(wave)-5000)] // Go-owned; safe after Destroy
```

## 6. Text front end

Accuracy on real input is the priority. Two passes run before chunking, both on by default and both switchable per request.

### 6.1 Markdown pass (`internal/markdown`)

Parse with [goldmark](https://github.com/yuin/goldmark) (pure Go, CommonMark plus the GFM table extension) and walk the AST to produce plain sentences.

| Markdown | Spoken as |
| --- | --- |
| Headings | The heading text, ending with `.` if it has no terminal punctuation |
| Paragraphs, block quotes | Their text |
| `**bold**`, `*em*`, `~~strike~~` | The text, markers removed |
| List items | Each item as its own sentence, `.` added if missing; numbering dropped |
| `[text](url)` | `text` only |
| Bare autolinks | Passed to the normalizer, which reads URLs aloud |
| Images | Alt text if present, else nothing |
| Inline `code` | Its content, passed to the normalizer |
| Fenced and indented code blocks | Skipped silently |
| Tables | Each row as a sentence, cells joined with `, `; the header row is included |
| Raw HTML | Tags removed, text kept |
| Emoji | Removed (Unicode `Extended_Pictographic`, plus variation selectors and ZWJ) |

Plain text without markdown syntax must pass through unchanged apart from emoji removal. A property test checks this.

### 6.2 Normalizer (`internal/normalize`)

Port Python's `normalize_text` (read-aloud mode, [`preprocess.py`](https://github.com/KittenML/KittenTTS/blob/main/kittentts/preprocess.py) `normalize_text_result`), not `TextPreprocessor`. On 2026-09-30 the two Python normalizers handled real input like this:

| Input | `TextPreprocessor` (`clean_text=True`) | `normalize_text` (ported) |
| --- | --- | --- |
| `2024 budget` | "two thousand twenty-four" | "twenty twenty-four" ✓ |
| `https://example.com`, `bob@example.com` | **deleted** | spelled out ✓ |
| `4:30 p.m.` | "four thirtyp.m." (bug) | "four thirty p m" ✓ |
| `Dr.`, `Jan 5th, 2025` | "dr.", "jan fifth, two thousand twenty-five" | "Doctor", "January fifth, twenty twenty-five" ✓ |
| `3 GB` | "three gigabytes" ✓ | "three GB" ✗ (to fix) |
| `$3.5 million` | "three dollars and fifty cents million" ✗ | the same ✗ (to fix) |

**Port rules**

- Port the substitution list in order: HTML, URL, email, month-day-year dates, month-year, times, currency, percent, ordinals, `et al.`, title abbreviations, dotted versions, ranges, model versions, and plain numbers. Also port the final punctuation and whitespace cleanup. Port the helper functions they call (`number_to_words`, `_year_to_words`, `_url_to_words`, `expand_currency` and so on).
- Go's RE2 has no lookarounds or backreferences. Where a Python regex uses them, rewrite it as a match plus a Go check, and cover that case with a test.
- Span tracking (`NormalizedSpan`) is not needed; port only the text output.

**Known fixes, the first deviations**

- Currency followed by a scale word: `$3.5 million` → "three point five million dollars".
- Units after numbers: port `expand_units` and `expand_scale_suffixes` from `TextPreprocessor`, so `3 GB` → "three gigabytes".
- Any other defect found while building the corpus.

**Overrides process.** `testdata/normalize_corpus.txt` holds about 300 sentences: Python's `tests/test_text_normalization.py` cases, plus LLM-style text with numbers, units, money, dates, times, URLs, emails, versions, abbreviations and markdown. `scripts/make_golden.py` records Python's `normalize_text` output for each one. The Go test requires equality with that output, **unless** the case appears in `testdata/normalize_overrides.yaml`:

```yaml
- input: "The budget was $3.5 million."
  python: "The budget was three dollars and fifty cents million."
  expected: "The budget was three point five million dollars."
  reason: currency followed by a scale word
  approved: false   # set to true during the end-of-project review
```

Overrides accumulate during development with `approved: false`. The end-of-project review (milestone 7) goes through the file once, and approves, edits or reverts each entry.

## 7. Go architecture

Only the model run is ONNX Runtime; everything else is Go. The native dependencies are `libonnxruntime` (loaded at run time), `libespeak-ng` (linked through cgo) and optionally `ffmpeg` (a subprocess).

```
Request text ─▶ Markdown pass ─▶ Normalize ─▶ Chunk (sentences, first chunk short)
                                                   │
        ┌──────────────────── for each chunk ─────────────────────┐
        │ Phonemize ─▶ Tokenize ─▶ Style row ─▶ [Model run] ─▶ Trim │
        │ espeak-ng    ids 0-177   voices.npz   ONNX Runtime   -5000│
        └──────────────────────────────────────────────────────────┘
                                                   │
                  Encoder (wav / pcm / mp3, or ffmpeg) ─▶ HTTP stream, SSE, or file
```

**Repository layout**

| Path | Responsibility |
| --- | --- |
| `go.mod` | `module github.com/androiddrew/gokittentts`, `go 1.26` |
| `kittentts/` | Public library: `Engine` (a model registry), `Synthesize`, `Stream` |
| `internal/phonemize` | espeak-ng cgo wrapper, the punctuation splitter with the decimal rule, and the `Phonemizer` interface |
| `internal/tokenize` | Regex re-tokenize and the symbol-to-id map |
| `internal/chunk` | Port of `chunk_text`, `ensure_punctuation` and `_is_sentence_boundary`, plus the short-first-chunk rule |
| `internal/normalize` | The `normalize_text` port and its fixes (section 6.2) |
| `internal/markdown` | The goldmark-based markdown pass (section 6.1) |
| `internal/npz` | `voices.npz` reader |
| `internal/audio` | WAV and PCM writers, the mp3 encoder (`shine-mp3`) and the ffmpeg pipe encoder |
| `internal/modelstore` | Pinned manifest, Hugging Face download, SHA-256 checks, `pull` |
| `internal/config` | YAML config loading and validation, with environment overrides |
| `internal/server` | OpenAI handlers, SSE, per-model queues, auth and metrics |
| `cmd/gokittentts` | A single binary with the subcommands `serve`, `say`, `pull` and `bench` |
| `docker/` | `Dockerfile.cpu` and `Dockerfile.cuda`, plus a default `config.yaml` |
| `Makefile` | `build`, `test`, `golden`, `image-cpu`, `image-cuda12`, `image-cuda13` |
| `scripts/make_golden.py` | Golden phoneme, id and normalizer vectors from the Python reference |
| `testdata/` | `golden.json`, `normalize_corpus.txt`, `normalize_golden.json` and `normalize_overrides.yaml` |

**Component rules and Go pitfalls**

- **Regex.** Go's `\w` is ASCII-only, but IPA stress and length marks (`ˈ ˌ ː ᵻ`) are Unicode letters (Lm) that Python's `\w` matches. Use `[\p{L}\p{N}_]+|[^\p{L}\p{N}_\s]`.
- **Symbol map.** Walk `pad + punctuation + letters + ipa` in order by rune, letting later duplicates overwrite. Copy the IPA string byte-for-byte from `onnx_model.py`; it contains U+0329 and straight apostrophes.
- **Style index.** Use `utf8.RuneCountInString(chunk)`, not `len(chunk)`.
- **espeak-ng.** Initialize once with `espeak_Initialize(AUDIO_OUTPUT_SYNCHRONOUS=2, 0, NULL, espeakINITIALIZE_DONT_EXIT=0x8000)` and `espeak_SetVoiceByName("en-us")`. Loop `espeak_TextToPhonemes(&ptr, espeakCHARS_UTF8=1, espeakPHONEMES_IPA=0x02)` until `ptr` is NULL. espeak-ng has global state, so a process-wide `sync.Mutex` guards every call. Include `<espeak-ng/speak_lib.h>` in production code.
- **Punctuation splitter.** Split on runs of the mark set, but treat `.` and `,` as ordinary characters when the character before **and** the character after are both ASCII digits. RE2 can't express that, so use a hand-written rune scanner. Phonemize only the text between runs, keep the marks, and join with spaces. The re-tokenize step normalizes the spacing. (The prototype's regex splitter got `$3.5` wrong; the scanner is the fix.)
- **Phonemizer interface.** `type Phonemizer interface { Phonemize(text string) (string, error) }`, with the cgo implementation as the default. A subprocess implementation (`espeak-ng -q --ipa -v en-us`) matched the library on every test case and is a drop-in for troublesome platforms.
- **voices.npz.** Read it with `archive/zip`. Accept `.npy` v1 or v2 headers. Assert `'<f4'`, `fortran_order False` and shape `(400, 256)`.
- **WAV.** Write each header field separately. `binary.Write` rejects `[]any`, and that silently corrupted the prototype's first header when its error was ignored.

**Phonemizer and tokenizer, as prototyped** (these passed the golden test; add the decimal-aware scanner)

```go
/*
#cgo LDFLAGS: -lespeak-ng
#include <stdlib.h>
#include <espeak-ng/speak_lib.h>
*/
import "C"

var espeakMu sync.Mutex

func espeakIPA(text string) string {
	espeakMu.Lock()
	defer espeakMu.Unlock()
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	ptr := unsafe.Pointer(cs)
	var sb strings.Builder
	for ptr != nil {
		sb.WriteString(C.GoString(C.espeak_TextToPhonemes(&ptr, C.espeakCHARS_UTF8, C.espeakPHONEMES_IPA)))
	}
	return sb.String()
}

var tokRe = regexp.MustCompile(`[\p{L}\p{N}_]+|[^\p{L}\p{N}_\s]`)

var symbolIDs = func() map[rune]int64 {
	pad := "$"
	punct := ";:,.!?¡¿—…\"«»\"\" "
	letters := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	ipa := "ɑɐɒæɓʙβɔɕçɗɖðʤəɘɚɛɜɝɞɟʄɡɠɢʛɦɧħɥʜɨɪʝɭɬɫɮʟɱɯɰŋɳɲɴøɵɸθœɶʘɹɺɾɻʀʁɽʂʃʈʧʉʊʋⱱʌɣɤʍχʎʏʑʐʒʔʡʕʢǀǁǂǃˈˌːˑʼʴʰʱʲʷˠˤ˞↓↑→↗↘'̩'ᵻ"
	m := map[rune]int64{}
	i := int64(0)
	for _, r := range pad + punct + letters + ipa {
		m[r] = i // later duplicates overwrite, as in Python
		i++
	}
	return m
}()

func tokenize(phonemes string) []int64 {
	joined := strings.Join(tokRe.FindAllString(phonemes, -1), " ")
	ids := []int64{0}
	for _, r := range joined {
		if id, ok := symbolIDs[r]; ok {
			ids = append(ids, id)
		}
	}
	return append(ids, 10, 0)
}
```

## 8. Public Go API

```go
package kittentts

type Device string // "cpu" | "cuda"

type ModelConfig struct {
	Name           string // e.g. "kitten-tts-mini-0.8"
	Dir            string // holds config.json, the .onnx file and voices.npz
	Device         Device
	CUDADeviceID   int
	IntraOpThreads int // 0 = ONNX Runtime default
}

type Request struct {
	Text      string
	Voice     string  // Kitten name, expr-voice-* key (OpenAI names are mapped by the server)
	Speed     float32 // already clamped by the caller
	Normalize bool
	Markdown  bool
}

func NewEngine(ortLibPath string, models []ModelConfig) (*Engine, error)
func (e *Engine) Model(name string) (*Model, error) // loads on first use
func (m *Model) Voices() []string
func (m *Model) Synthesize(ctx context.Context, r Request) ([]float32, error)
func (m *Model) Stream(ctx context.Context, r Request) iter.Seq2[[]float32, error]
func (e *Engine) Close() error

const SampleRate = 24000
```

- `Model` loads the library once, then checks the model contract, loads the voices and creates the session. A failure to append the CUDA provider is an error.
- `Stream` yields one trimmed chunk at a time and checks `ctx` between chunks; a single run can't be interrupted.
- The library has no queue. Queueing, auth and format encoding belong to the server.

## 9. Configuration

YAML at `/etc/gokittentts/config.yaml` (or `--config`). The `KITTEN_API_KEY` environment variable sets the API key. `KITTEN_DEFAULT_MODEL` and `KITTEN_LISTEN` override those fields.

```yaml
listen: ":8880"
onnxruntime_lib: /opt/onnxruntime/lib/libonnxruntime.so.1.29.1
models_dir: /var/lib/gokittentts/models   # baked and downloaded models live here
download: true                           # fetch missing models from Hugging Face on first use
default_model: kitten-tts-mini-0.8

models:
  kitten-tts-mini-0.8:      { device: cpu, intra_op_threads: 0, max_queue: 8 }
  kitten-tts-micro-0.8:     { device: cpu }
  kitten-tts-nano-0.8-int8: { device: cpu }
  kitten-tts-nano-0.8-fp32: { device: cpu }

# OpenAI model names clients send; each maps to a configured model
model_aliases:
  tts-1: default
  tts-1-hd: default
  gpt-4o-mini-tts: default

# OpenAI voice names → Kitten voices (defaults chosen by rough gender match; tune by ear)
voices:
  alloy: Bella
  ash: Jasper
  ballad: Leo
  cedar: Bruno
  coral: Rosie
  echo: Bruno
  fable: Jasper
  marin: Luna
  nova: Luna
  onyx: Hugo
  sage: Kiki
  shimmer: Kiki
  verse: Leo

speed: { min: 0.5, max: 2.0 }       # effective clamp; requests may send 0.25–4.0
limits:
  max_input_chars: 4096              # OpenAI's limit
  request_timeout: 120s
ffmpeg: ffmpeg                       # path or empty to disable opus/aac/flac
metrics: true
log_format: json
```

Validation fails at startup on: an unknown device, a `default_model` that isn't in `models`, a voice map entry naming a non-Kitten voice, or `speed.min > speed.max`. Custom models not in the pinned manifest may set `repo:` and `revision:`; they are downloaded without hash checks, and a warning is logged.

## 10. HTTP API

### 10.1 Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/v1/audio/speech` | OpenAI-compatible synthesis |
| GET | `/v1/models` | OpenAI-style list of configured models and aliases |
| GET | `/v1/voices` | Kitten voice names, `expr-voice-*` keys and the OpenAI voice map |
| GET | `/healthz` | 200 once the config is valid and ONNX Runtime has loaded |
| GET | `/metrics` | Prometheus metrics (section 10.6) |

### 10.2 `POST /v1/audio/speech`

The request follows OpenAI's `CreateSpeechRequest` ([openai-openapi](https://github.com/openai/openai-openapi)), plus two gokittentts fields.

| Field | Type | Handling |
| --- | --- | --- |
| `model` | string, required | A configured model name, an alias from `model_aliases`, or 400 |
| `input` | string, required | 1–`max_input_chars` characters, else 400 or 413 |
| `voice` | string, or object `{"id"}`, required | Resolved as: Kitten name → `expr-voice-*` key → OpenAI name map → 400 listing the valid names. For an object, `id` is resolved the same way |
| `response_format` | `mp3` (default), `opus`, `aac`, `flac`, `wav`, `pcm` | See section 10.3 |
| `speed` | number, 0.25–4.0, default 1.0 | Outside 0.25–4.0 → 400. Inside, clamped to `speed.min`–`speed.max`, then multiplied by the model's speed prior |
| `stream_format` | `audio` (default) or `sse` | See section 10.4 |
| `instructions` | string | Accepted and ignored; logged at debug level |
| `normalize` | bool, default true | gokittentts extension: turns off the normalizer |
| `markdown` | bool, default true | gokittentts extension: turns off the markdown pass |

Errors use OpenAI's shape: `{"error": {"message", "type", "param", "code"}}`.

### 10.3 Formats

| Format | Encoder | Content-Type |
| --- | --- | --- |
| `pcm` | Raw 16-bit little-endian, 24 kHz mono, as OpenAI does | `audio/pcm` |
| `wav` | 16-bit PCM WAV | `audio/wav` |
| `mp3` | [`braheezy/shine-mp3`](https://github.com/braheezy/shine-mp3), pure Go, MPEG-2 at 24 kHz mono, 64 kbps | `audio/mpeg` |
| `opus` | `ffmpeg -f s16le -ar 24000 -ac 1 -i - -c:a libopus -f ogg -` | `audio/ogg` |
| `aac` | `ffmpeg … -c:a aac -f adts -` | `audio/aac` |
| `flac` | `ffmpeg … -c:a flac -f flac -` | `audio/flac` |

- If `ffmpeg` is disabled or not found, `opus`, `aac` and `flac` return 400 with a message naming the supported formats.
- shine-mp3 is LGPL-2.0 and ffmpeg's licenses vary by build. Both are fine for self-hosting; record them in the image's `LICENSES/`.

### 10.4 Streaming

- **`stream_format: "audio"` (default).** The response uses chunked transfer and is flushed after each synthesized chunk.
    - `pcm`, `mp3` and the ffmpeg formats stream naturally; the ffmpeg process reads PCM on stdin and its stdout is copied to the response.
    - `wav` sends a header whose RIFF and data sizes are `0xFFFFFFFF`, then PCM.
- **`stream_format: "sse"`.** `Content-Type: text/event-stream`. Each chunk's encoded bytes are sent as `data: {"type":"speech.audio.delta","audio":"<base64>"}`. The stream ends with `data: {"type":"speech.audio.done","usage":{"input_tokens":<token ids>,"output_tokens":<sum(duration)>,"total_tokens":<sum>}}`.
- **Client disconnects.** They cancel the request context. The in-flight run finishes, then the remaining chunks are skipped and the queue slot is released.

### 10.5 Concurrency, limits and auth

- **Queueing.** Each loaded model has a worker that runs one synthesis at a time and a queue that holds up to `max_queue` requests. A full queue returns **429** with `Retry-After: 1`.
- **Timeouts.** The request timeout covers queue wait plus synthesis.
- **Auth.** When `KITTEN_API_KEY` is set, every `/v1/*` request must send `Authorization: Bearer <key>`, compared in constant time, else 401. `/healthz` and `/metrics` stay open. When it is unset, there is no auth, and a warning is logged if the listen address isn't loopback.
- **Model downloads.** A request for a model that isn't on disk triggers a download when `download: true` (section 11.2). Concurrent requests share the same download, and a failure returns 503.

### 10.6 Observability

- **Logs.** One slog line per request: model, voice, format, input characters, chunks, audio seconds, synthesis seconds, RTF, time to first audio, queue wait and status.
- **Metrics:**
    - `kitten_requests_total{model,format,status}`
    - `kitten_synthesis_seconds{model}` (histogram)
    - `kitten_rtf{model}` (histogram)
    - `kitten_time_to_first_audio_seconds{model}` (histogram)
    - `kitten_queue_depth{model}` (gauge)
    - `kitten_model_loaded{model,device}` (gauge)
    - `kitten_model_downloads_total{model,status}`

## 11. Deployment

### 11.1 Docker images

| Tag | Platforms | Base | ONNX Runtime | Default `BAKE_MODELS` | Default model |
| --- | --- | --- | --- | --- | --- |
| `gokittentts:cpu` | linux/amd64, linux/arm64 | `debian:trixie-slim` + `espeak-ng`, `ffmpeg` | `linux-x64` or `linux-aarch64` 1.29.1 | `kitten-tts-mini-0.8` | mini |
| `gokittentts:cuda12` | linux/amd64 | `nvidia/cuda:12.x-cudnn-runtime-ubuntu24.04` + `espeak-ng`, `ffmpeg` | `gpu_cuda12` 1.29.1 | `kitten-tts-nano-0.8-fp32` | nano-fp32 |
| `gokittentts:cuda13` | linux/amd64 | `nvidia/cuda:13.x-cudnn-runtime-ubuntu24.04` + `espeak-ng`, `ffmpeg` | `gpu_cuda13` 1.29.1 | `kitten-tts-nano-0.8-fp32` | nano-fp32 |

- The build is multi-stage. A `golang:1.26` builder with `libespeak-ng-dev` runs `CGO_ENABLED=1 go build -tags espeak`. The runtime stage copies the binary, the ONNX Runtime `lib/` (with the `providers_*.so` files for CUDA) and `docker/config.yaml`.
- `BAKE_MODELS` is a comma-separated list, and an empty value gives a slim image. The build stage runs `gokittentts pull $BAKE_MODELS --dir /var/lib/gokittentts/models`, which uses the same manifest and SHA-256 checks as run-time downloads.
- Models not baked in are downloaded on first use into `/var/lib/gokittentts/models`. Mount that path as a volume so they persist.
- A Pi 5 build sets both: `make image-cpu BAKE_MODELS=kitten-tts-nano-0.8-fp32`, and `KITTEN_DEFAULT_MODEL=kitten-tts-nano-0.8-fp32` at run time.
- For the CUDA images, the host needs the NVIDIA Container Toolkit (`--gpus all`). CUDA 12 works with most current drivers; CUDA 13 needs driver ≥ 580. Pin exact base tags in the Dockerfile at build time.

```
make image-cpu                          # buildx, linux/amd64 + linux/arm64
make image-cuda12
make image-cuda13
make image-cpu BAKE_MODELS=             # slim: no models baked
docker run -p 8880:8880 -v kitten-models:/var/lib/gokittentts/models gokittentts:cpu
docker run --gpus all -p 8880:8880 gokittentts:cuda12
```

### 11.2 Model store

- **Layout.** `models_dir/<name>/<revision>/{config.json,<model_file>,voices.npz}`, plus `models_dir/<name>/current` pointing at the active revision.
- **Download.** Write to a temporary file in the same directory, verify SHA-256, then rename into place. Pin the Hugging Face `revision` from the manifest (section 3.1), and follow redirects.
- **Offline.** `download: false` turns downloads off. A missing model then returns 503 with a message suggesting `gokittentts pull <name>`.

### 11.3 Plain binary

- **Linux.**
    1. `apt install espeak-ng libespeak-ng-dev ffmpeg`.
    2. Unpack the matching ONNX Runtime archive.
    3. Run `CGO_ENABLED=1 go build -tags espeak ./cmd/gokittentts`. Without `-tags espeak` the binary is built without espeak-ng and fails at startup.
    4. Set `onnxruntime_lib` in the config.
- **macOS arm64 (best effort, CPU only, not in CI).**
    1. `brew install espeak-ng ffmpeg`.
    2. Unpack `onnxruntime-osx-arm64-1.29.1.tgz` and point `onnxruntime_lib` at `libonnxruntime.1.29.1.dylib`.
    3. Build with `CGO_CFLAGS=-I$(brew --prefix)/include CGO_LDFLAGS=-L$(brew --prefix)/lib`.
    4. CoreML is a later experiment.

### 11.4 GPU notes

- **CUDA libraries.** The CUDA images ship CUDA and cuDNN in their base images. For a plain binary, provide CUDA 12.x (or 13.x) and cuDNN 9. On the dev machine, a sudo-free route worked: install NVIDIA's pip wheels (`nvidia-cudnn-cu12`, `-cublas-`, `-cuda-runtime-`, `-curand-`, `-cufft-`, `-cusolver-`, `-cusparse-`, `-nvjitlink-`) with `uv` and add their `lib/` dirs to `LD_LIBRARY_PATH`.
- **Node placement for mini on CUDA,** from verbose ONNX Runtime logs:
    - 559 nodes stay on the CPU, including all 135 `MatMulInteger`, 125 `DynamicQuantizeLinear`, 74 `ConvInteger` and 6 `DynamicQuantizeLSTM`.
    - 2,511 nodes run on CUDA, including 503 inserted memory-copy nodes.
    - That is why CUDA barely helps int8 models and why the CUDA images default to nano-fp32.

## 12. Performance targets

| Target | Threshold | Where |
| --- | --- | --- |
| Throughput | RTF < 0.8 (synthesis seconds ÷ audio seconds) | The configured default model on each supported device |
| Time to first audio | < 1 s from request receipt to the first byte of audio | Same |

- The short-first-chunk rule (section 4, step 3) keeps the first model run small.
- `gokittentts bench --model <name> [--device cuda]` runs a fixed 20-sentence corpus three times after one warm-up. It reports the p50 and p95 of RTF and of time to first audio, and exits non-zero if the thresholds are missed.
- **Release checklist:** run `bench` on a Raspberry Pi 5 (arm64 image, nano-fp32) and on an x86_64 host (cpu image, mini), then record the results in `docs/benchmarks.md`.
- Reference numbers from the prototype are in section 3. Mini's 0.57 RTF on a 5600X passes on desktop CPUs. It is not expected to pass on a Pi 5, which is why the Pi uses nano-fp32.

## 13. Testing

| Suite | What it checks | Needs native libs |
| --- | --- | --- |
| Tokenizer and phonemizer golden | Phonemes and ids equal `testdata/golden.json` from Python, including decimals, punctuation, quotes, dashes, ellipses and abbreviations; records the espeak-ng version | espeak-ng |
| Punctuation scanner | Decimal and thousands separators, trailing periods, runs of marks, leading and trailing marks | no |
| Chunker | Ported Python tests, the 400-character split, missing terminal punctuation, empty input, the short-first-chunk rule | no |
| Normalizer | Corpus output equals Python output, or the matching override's `expected` | no |
| Markdown | A table per construct from section 6.1; plain text passes through unchanged | no |
| npz and config | Header parsing, shape and dtype checks, config validation errors | no |
| Model contract | `GetInputOutputInfo` matches section 2 for every model in `models_dir` | ONNX Runtime |
| Audio invariants | `len(waveform) == sum(duration) × 600`, non-empty trimmed output, RMS > 0.01 per model; nano speed prior applied (0.8 for Bella, 0.9 for Hugo, 1.0 for mini) | ONNX Runtime and the models |
| Encoders | WAV and PCM byte layout; an mp3 that decodes back with plausible length; ffmpeg formats when ffmpeg is present | ffmpeg (optional) |
| Server | `httptest`: every error code, voice and model resolution, auth, 429 on a full queue, raw streaming chunks, SSE event shapes, `speed` clamping, disconnect cancelling | no (fake engine) |
| Bench | Section 12 | ONNX Runtime and the models |

Tests needing native libraries or models use a build tag (`//go:build native`) so that `go test ./...` runs anywhere. `make test-native` runs everything.

## 14. Milestones

1. **Library and CLI slice.**
    - Deliverables: `kittentts` package, `internal/{phonemize,tokenize,chunk,npz,audio(wav)}`, `gokittentts say`, and the golden tests including the decimal scanner fix.
    - Done when: `say` writes correct WAVs with mini on CPU, and the golden tests pass.
2. **OpenAI server.**
    - Deliverables: `serve`, `/v1/audio/speech` with wav, pcm and mp3 plus the ffmpeg formats, raw and SSE streaming, voice map, speed clamp, queue and 429, auth, logs and metrics.
    - Done when: an OpenAI client (for example the `openai` Python SDK, or Open WebUI) plays audio from it.
3. **Multi-model config and model store.**
    - Deliverables: YAML config, lazy loading, `model_aliases`, the pinned manifest, `pull`, and ad-hoc downloads.
    - Done when: requests for all four models work from an empty `models_dir`.
4. **Text front end.**
    - Deliverables: the markdown pass and the `normalize_text` port, with the corpus, golden and override files.
    - Done when: the normalizer suite passes, and the overrides file lists every deviation with `approved: false`.
5. **Images and CUDA.**
    - Deliverables: Dockerfiles, `BAKE_MODELS`, the `make image-*` targets, and the CUDA device.
    - Done when: all three images serve audio, and the CUDA images run nano-fp32 on the GPU.
6. **Performance and platforms.**
    - Deliverables: `bench`, the Pi 5 run, `docs/benchmarks.md`, and the macOS build notes.
    - Done when: the section 12 gates pass on the Pi 5 (nano-fp32) and on x86_64 (mini).
7. **Normalizer review.**
    - Deliverables: one pass through `normalize_overrides.yaml` with the owner, approving, editing or reverting each entry.
    - Done when: every override is `approved: true` or removed.

## 15. Risks and open questions

| Risk | Impact | Mitigation |
| --- | --- | --- |
| espeak-ng version drift (Debian, Homebrew and the dev machine may differ) | Phonemes and ids change | Record the espeak-ng version in `golden.json`; the images pin the distro package; regenerate the goldens on upgrade |
| Reference drift between `phonemizer` and `phonemizer-fork` | The released 0.8.1 wheel phonemizes decimals differently from `main`; goldens made from the wheel would encode the fork's behavior | Goldens use upstream `phonemizer` ≥ 3.4.0 and record its version; `make_golden.py` refuses to run with the fork |
| Bundled Python `espeakng_loader` data path is broken | The Python reference fails with "phontab: No such file" | `make_golden.py` points phonemizer at the system `libespeak-ng.so.1` |
| onnxruntime_go and ONNX Runtime drift | Crashes or missing symbols | Pin both; check `ort.GetVersion()` at startup |
| Normalizer port size (about 1,200 lines of Python regex) | Slow milestone 4; RE2 can't express lookarounds | The corpus and golden files catch regressions; lookaround cases are rewritten as match plus check |
| Mini is not real time on a Pi 5 | A Pi can't use the default | The Pi default is nano-fp32; `bench` gates the release |
| cgo with multi-arch buildx | Slow QEMU builds for arm64 | Use native arm64 builders when available; the build is cached |
| shine-mp3 quality and LGPL | A lower-quality mp3 than LAME | Speech at 64 kbps is fine; ffmpeg's `libmp3lame` is a possible upgrade path |
| OpenAI voice map defaults are arbitrary | Unexpected voices | The config is editable and documented; tune by ear |

## 16. Sources

Opened 2026-09-30.

- [yalue/onnxruntime_go](https://github.com/yalue/onnxruntime_go): README and `onnxruntime_go.go` at v1.36.0
- [yalue/onnxruntime_go_examples](https://github.com/yalue/onnxruntime_go_examples): `mnist`, `onnx_list_inputs_and_outputs`
- [KittenML/KittenTTS](https://github.com/KittenML/KittenTTS): `kittentts/onnx_model.py`, `preprocess.py`, `get_model.py`, `requirements_gpu.txt`
- Hugging Face models: [mini](https://huggingface.co/KittenML/kitten-tts-mini-0.8), [micro](https://huggingface.co/KittenML/kitten-tts-micro-0.8), [nano-int8](https://huggingface.co/KittenML/kitten-tts-nano-0.8-int8), [nano-fp32](https://huggingface.co/KittenML/kitten-tts-nano-0.8-fp32)
- [ONNX Runtime v1.29.1 release](https://github.com/microsoft/onnxruntime/releases/tag/v1.29.1) and [CUDA execution provider](https://onnxruntime.ai/docs/execution-providers/CUDA-ExecutionProvider.html)
- [ONNX Runtime ROCm EP removal](https://onnxruntime.ai/docs/execution-providers/ROCm-ExecutionProvider.html) ([PR #25181](https://github.com/microsoft/onnxruntime/pull/25181)), the context for dropping ROCm
- [openai/openai-openapi](https://github.com/openai/openai-openapi): `CreateSpeechRequest`, `SpeechAudioDeltaEvent`, `SpeechAudioDoneEvent`
- [phonemizer `punctuation.py`](https://github.com/bootphon/phonemizer): the decimal-separator exception
- [braheezy/shine-mp3](https://github.com/braheezy/shine-mp3): the pure-Go MP3 encoder (LGPL-2.0; supports 24 kHz)
- [yuin/goldmark](https://github.com/yuin/goldmark): the markdown parser

## Appendix A: golden-vector script

`scripts/make_golden.py`, run through `make golden` (`uv run --no-project --with "phonemizer>=3.4.0" python3 scripts/make_golden.py`). Never generate goldens with `phonemizer-fork` or by installing the `kittentts` wheel, which pulls the fork in through `misaki[en]` (section 4, step 4); the script refuses to run if it detects the fork. It expects KittenTTS's `onnx_model.py` and `preprocess.py` copied next to it as `onnx_model_ref.py` and `preprocess_ref.py`, pinned to the 0.8.1 tag. The normalizer golden extends it with `pp.normalize_text(line)` over `testdata/normalize_corpus.txt`.

```python
import json, re
import importlib.metadata as md

# The reference is upstream phonemizer >= 3.4.0; phonemizer-fork splits decimals ("3.5" -> "three . five").
try:
    md.version("phonemizer-fork")
    raise SystemExit("phonemizer-fork is installed; use upstream phonemizer>=3.4.0 for goldens")
except md.PackageNotFoundError:
    pass
_v = tuple(int(x) for x in md.version("phonemizer").split(".")[:2])
if _v < (3, 4):
    raise SystemExit(f"phonemizer {md.version('phonemizer')} < 3.4.0 lacks the decimal-separator rule")

from phonemizer.backend.espeak.wrapper import EspeakWrapper
EspeakWrapper.set_library("/usr/lib/x86_64-linux-gnu/libespeak-ng.so.1")
import phonemizer
import importlib.util

src = open("onnx_model_ref.py").read()
ns = {}
exec(src[src.index("class TextCleaner"):src.index("class KittenTTS_1_Onnx")], ns)
tc = ns["TextCleaner"]()
spec = importlib.util.spec_from_file_location("pp", "preprocess_ref.py")
pp = importlib.util.module_from_spec(spec); spec.loader.exec_module(pp)

b = phonemizer.backend.EspeakBackend(language="en-us", preserve_punctuation=True, with_stress=True)
out = []
for line in open("testdata/sentences.txt"):
    for chunk in pp.chunk_text(line.strip()):
        ph = b.phonemize([chunk])[0]
        toks = " ".join(re.findall(r"\w+|[^\w\s]", ph))
        out.append({"chunk": chunk, "phonemes": ph,
                    "ids": [0] + tc(toks) + [10, 0],
                    "ref_id": min(len(chunk), 399),
                    "espeak": ".".join(map(str, b.version())),
                    "phonemizer": md.version("phonemizer")})
json.dump(out, open("testdata/golden.json", "w"), ensure_ascii=False, indent=1)
```
