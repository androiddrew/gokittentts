# KittenTTS Mini in Go — Specification

As of 2026-09-30. Shared, commentable copy: https://claude.ai/code/artifact/8e293774-6f99-4c95-bf86-1ecdd7936644

## Overview

KittenTTS Mini 0.8 can run from Go with `github.com/yalue/onnxruntime_go` v1.36.0 and ONNX Runtime 1.29.x. Only the model is ONNX, though. Chunking, phonemization, tokenization and voice-style lookup must be reimplemented in Go, with phonemes still coming from espeak-ng.

**Goals**

- A reusable Go package (`kittentts`) that turns English text into 24 kHz mono audio, matching the Python `kittentts` 0.8.1 pipeline token-for-token.
- One code path for all four 0.8 models (mini, micro, nano-int8, nano-fp32), chosen by model directory. Mini is the primary target.
- A CLI that writes a WAV file.
- An HTTP server that returns synthesized audio.
- CPU and NVIDIA GPU (CUDA execution provider) builds.

**Non-goals**

- Training or fine-tuning.
- Languages other than `en-us`.
- Bit-exact audio: the graph contains random ops, so the audio is not deterministic.
- A full port of the 1,200-line Python text normalizer in phase 1.

## Model artifacts

The Hugging Face repo [KittenML/kitten-tts-mini-0.8](https://huggingface.co/KittenML/kitten-tts-mini-0.8) (80M params, Apache-2.0) holds three files that matter. The facts below come from loading the graph with the `onnx` Python package on 2026-09-30.

| File | Contents |
| --- | --- |
| `config.json` | `type: ONNX2`, `model_file`, `voices`, `speed_priors: {}`, `voice_aliases` |
| `kitten_tts_mini_v0_8.onnx` | 78.3 MB; IR 9, opset 20; producer `onnx.quantize` |
| `voices.npz` | 8 arrays, each `(400, 256) float32` (a zip of `.npy` files) |

**Graph inputs and outputs**

| Name | Direction | Type | Shape |
| --- | --- | --- | --- |
| `input_ids` | input | int64 | `[1, sequence_length]` |
| `style` | input | float32 | `[1, 256]` |
| `speed` | input | float32 | `[1]` |
| `waveform` | output | float32 | `[num_samples]` (dynamic) |
| `duration` | output | int64 | `[n]` (one per input token; unused by Python) |

**Graph properties that shape the design**

- It is dynamically int8-quantized: 135 `MatMulInteger`, 74 `ConvInteger`, 125 `DynamicQuantizeLinear` and 6 `com.microsoft:DynamicQuantizeLSTM` nodes. This matters for GPU placement (see the GPU section).
- It contains `RandomNormalLike` and `RandomUniformLike`, so two runs on the same input produce different samples. Tests cannot compare audio byte-for-byte.
- Both outputs have dynamic length, so outputs cannot be preallocated.

**Voices** (display name to `voices.npz` key, from `config.json`)

| Name | Key | Name | Key |
| --- | --- | --- | --- |
| Bella | expr-voice-2-f | Jasper | expr-voice-2-m |
| Luna | expr-voice-3-f | Bruno | expr-voice-3-m |
| Rosie | expr-voice-4-f | Hugo | expr-voice-4-m |
| Kiki | expr-voice-5-f | Leo | expr-voice-5-m |

## Model variants

All four KittenTTS 0.8 models share the same interface, so the same Go code runs any of them; switching is just a different `-model-dir`. Their inputs, outputs, voice keys, `(400, 256)` voice arrays and the 600-samples-per-duration-frame rule are identical, all checked on 2026-09-30. The prototype ran each one on CPU and CUDA and produced speech-like audio: RMS 0.12–0.14 and about 1,500 zero-crossings per second.

| Model repo | ONNX file | Size | Quantization | Speed priors | CPU warm run | CUDA warm run |
| --- | --- | --- | --- | --- | --- | --- |
| [kitten-tts-mini-0.8](https://huggingface.co/KittenML/kitten-tts-mini-0.8) | `kitten_tts_mini_v0_8.onnx` | 78.3 MB | int8 dynamic | none | 2.82 s | 1.79 s |
| [kitten-tts-micro-0.8](https://huggingface.co/KittenML/kitten-tts-micro-0.8) | `kitten_tts_micro_v0_8.onnx` | 41.4 MB | int8 dynamic | none | 1.77 s | 1.03 s |
| [kitten-tts-nano-0.8-int8](https://huggingface.co/KittenML/kitten-tts-nano-0.8-int8) | `kitten_tts_nano_v0_8.onnx` | 24.4 MB | int8 dynamic | 0.8, or 0.9 for `expr-voice-4-m` | 1.76 s | 0.91 s |
| [kitten-tts-nano-0.8-fp32](https://huggingface.co/KittenML/kitten-tts-nano-0.8-fp32) | `kitten_tts_nano_v0_8.onnx` | 56.8 MB | none (float32) | 0.8, or 0.9 for `expr-voice-4-m` | **0.27 s** | **0.06 s** |

Timings are for one sentence of about 5 s of audio (77 tokens). Each is the fifth warm run of `DynamicAdvancedSession.Run` with ONNX Runtime 1.29.1 on a Ryzen 5 5600X (6 cores) and an RTX 4090.

What this means for the design:

- **Never hard-code file names.** Read `model_file` and `voices` from `config.json`. The two nano repos use the same file name but different contents.
- **Apply `speed_priors`.** Mini and micro have empty priors, so the mini prototype never exercised this path. Nano slows every voice to 0.8× (0.9× for Hugo), and skipping the prior makes nano speak noticeably too fast. The Python rule is `speed = requested_speed × speed_priors.get(voice_key, 1.0)`, looked up after the alias is resolved to its `expr-voice-*` key.
- **`KittenML/kitten-tts-nano-0.8` redirects** to `kitten-tts-nano-0.8-fp32` (HTTP 307). It is also the Python library's default model. `curl -L` follows the redirect.
- **Speed is mostly about quantization, not size.** Dynamic int8 quantization re-quantizes activations on every call, and none of those ops run on CUDA. The float32 nano has no such ops, so the whole graph runs on the GPU: it is 10× faster than mini on CPU and 30× faster on GPU. Whether it sounds good enough is a listening decision; the spec does not rank voice quality.
- **The GPU is only worth it for nano-fp32.** For the three int8 models, CUDA saves under a second per sentence, which rarely justifies the CUDA and cuDNN install.
- **Golden token vectors are shared.** Phonemization and tokenization do not depend on the model, so one `golden.json` covers all four. Only the audio-invariant and benchmark tests run once per model.

## Reference pipeline

The Go port must reproduce these seven steps from [`kittentts/onnx_model.py`](https://github.com/KittenML/KittenTTS/blob/main/kittentts/onnx_model.py) and [`preprocess.py`](https://github.com/KittenML/KittenTTS/blob/main/kittentts/preprocess.py) (package 0.8.1). Steps 3 to 7 run once per chunk.

1. **Normalize (optional).** `TextPreprocessor` expands numbers, currency, times, units and similar forms, then lowercases the text and strips punctuation except where configured. The public `KittenTTS.generate()` passes `clean_text=False`, so this step is **off by default**.
2. **Chunk.** `chunk_text(text, max_len=400)` splits on `.`, `!` or `?` followed by whitespace or end of text. It skips abbreviations, decimals like `3.5` and `a.m.`/`p.m.`. Sentences over 400 characters are split at word boundaries. `ensure_punctuation` appends `,` to any chunk not ending in `.!?,;:`.
3. **Phonemize.** Use phonemizer's `EspeakBackend(language="en-us", preserve_punctuation=True, with_stress=True)`, which produces espeak-ng IPA with stress marks. Punctuation from the set `;:,.!?¡¿—…"«»“”(){}[]` is stripped before espeak and re-inserted after. Verified on this machine with espeak-ng 1.51: `Hello, world! This high-quality TTS model runs without a GPU.` becomes `həlˈoʊ, wˈɜːld! ðɪs hˈaɪkwˈɔlᵻɾi tˌiːtˌiːˈɛs mˈɑːdəl ɹˈʌnz wɪðˌaʊt ɐ dʒˌiːpˌiːjˈuː.`
4. **Re-tokenize.** Python `re.findall(r"\w+|[^\w\s]", phonemes)`, joined with single spaces. This puts exactly one space around every punctuation mark.
5. **Map to ids.** The symbol list is `$`, then `;:,.!?¡¿—…"«»"" ` (16 chars including a trailing space), then `A–Z`, `a–z`, then a fixed IPA string. It builds a dict where a later duplicate overwrites an earlier one: 178 positions, 175 unique symbols, and `"` ends at id 14. Characters not in the dict are **silently dropped**. The result is wrapped as `[0] + ids + [10, 0]`; id 10 is `…`.
6. **Pick the style vector.** `ref_id = min(len(chunk), 399)` counts the characters of the **chunk text, not the phonemes**. The style is row `ref_id` of the voice's `(400, 256)` array, shaped `[1, 256]`. The speed is `[speed × speed_priors[voice]]`, and the prior defaults to 1.0 because the config's map is empty.
7. **Run and trim.** Run the session, take `waveform` and **drop its last 5,000 samples** (≈0.21 s of tail). Concatenate the chunks. The output is float32 PCM at 24,000 Hz, mono; Python writes it with `soundfile`, whose default WAV subtype is PCM_16.

## onnxruntime_go usage

Use `github.com/yalue/onnxruntime_go` **v1.36.0** with ONNX Runtime **1.29.1**, and use a `DynamicAdvancedSession` so ONNX Runtime allocates outputs whose length is only known after the run. A prototype built this way on 2026-09-30 produced 4.84 s of audio (116,200 samples) on CPU. Its token ids matched the Python reference exactly on all 5 golden chunks.

**Version lockstep.** onnxruntime_go ships the C API headers for one ONNX Runtime release, and v1.36.0 targets 1.29.x. Load the versioned library file (`libonnxruntime.so.1.29.1`), not the `libonnxruntime.so` symlink. The [examples repo](https://github.com/yalue/onnxruntime_go_examples) still pins v1.25.0; copy its idioms, not its version.

**API calls used**

| Step | Call | Notes |
| --- | --- | --- |
| Load library | `ort.SetSharedLibraryPath(p)`, `ort.InitializeEnvironment()` | Once per process; `defer ort.DestroyEnvironment()` |
| Startup check | `ort.GetInputOutputInfo(modelPath)` | Assert the 3 input and 2 output names, types and shapes |
| Options | `ort.NewSessionOptions()`, `SetIntraOpNumThreads(n)` | Destroy after the session is created |
| GPU | `ort.NewCUDAProviderOptions()`, `Update(map)`, `opts.AppendExecutionProviderCUDA(co)` | See the GPU section |
| Session | `ort.NewDynamicAdvancedSession(path, []string{"input_ids","style","speed"}, []string{"waveform","duration"}, opts)` | Created once; `Run` is safe to call concurrently |
| Inputs | `ort.NewTensor(ort.NewShape(1, n), ids)` for int64, `NewShape(1,256)` for style, `NewShape(1)` for speed | Per request; `Destroy()` after `Run` |
| Run | `sess.Run(inputs, []ort.Value{nil, nil})` | Nil outputs are allocated by ONNX Runtime; the caller must `Destroy()` them |
| Read | `outputs[0].(*ort.Tensor[float32]).GetData()` | `GetData` returns a Go-owned copy (v1.36.0 copies auto-allocated outputs), so it outlives `Destroy()` |

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

## Go architecture

One package, `kittentts`, owns the whole pipeline; the two commands are thin wrappers around it. It has two native dependencies: `libonnxruntime.so.1.29.1`, loaded at run time by onnxruntime_go, and `libespeak-ng.so.1`, linked by cgo.

```
Input text ──▶ Normalize (optional) ──▶ Chunk (sentences, ≤400 chars)
                                            │
        ┌───────────────── repeated for each chunk ─────────────────┐
        │ Phonemize ─▶ Tokenize ─▶ Style row ─▶ [Model run] ─▶ Trim  │
        │ espeak-ng    ids 0-177   voices.npz   ONNX Runtime   -5000 │
        └────────────────────────────────────────────────────────────┘
                                            │
                  Concatenate (24 kHz mono float32) ──▶ Output (WAV file or HTTP stream)
```

Everything except the bracketed model run is Go code this spec describes; espeak-ng is the only other native dependency.

**Repository layout**

| Path | Responsibility |
| --- | --- |
| `go.mod` | `module github.com/<you>/golang_onnx`, `go 1.26`, requires `github.com/yalue/onnxruntime_go v1.36.0` |
| `kittentts/config.go` | Parse `config.json` (`model_file`, `voices`, `voice_aliases`, `speed_priors`); nothing model-specific is hard-coded |
| `kittentts/voices.go` | Load `voices.npz` into `map[string][]float32` (400×256 each) |
| `kittentts/chunk.go` | Port of `chunk_text`, `ensure_punctuation` and `_is_sentence_boundary` |
| `kittentts/phonemize.go` | cgo wrapper for espeak-ng, plus punctuation preservation |
| `kittentts/tokenizer.go` | Regex re-tokenize and symbol-to-id map |
| `kittentts/normalize.go` | Optional text normalization (phase 1 subset) |
| `kittentts/model.go` | ORT environment, session, `Generate`, `GenerateStream` |
| `kittentts/wav.go` | 16-bit PCM WAV encoder (standard library only) |
| `cmd/kitten-cli/main.go` | Command-line synthesis to a file |
| `cmd/kitten-server/main.go` | HTTP server |
| `scripts/fetch_model.sh` | Download the Hugging Face files and ONNX Runtime archives into `models/` and `third_party/` |
| `scripts/make_golden.py` | Produce `testdata/golden.json` from the Python reference |

**Component rules and Go pitfalls**

- **Regex.** Go's `\w` is ASCII-only, but the IPA stress and length marks (`ˈ ˌ ː ᵻ`) are Unicode letters (category Lm) that Python's `\w` matches. Use `[\p{L}\p{N}_]+|[^\p{L}\p{N}_\s]`. With a plain `\w`, stress marks would split words and change the ids.
- **Symbol map.** Build it by walking `pad + punctuation + letters + ipa` in order, letting later duplicates overwrite earlier ones. Iterate by rune; the IPA string holds multi-byte and combining characters.
- **Style index.** Use `utf8.RuneCountInString(chunk)` to match Python's `len()`. `len(chunk)` counts bytes and picks the wrong row for any non-ASCII text.
- **espeak-ng calls.**
  - Initialize once with `espeak_Initialize(AUDIO_OUTPUT_SYNCHRONOUS=2, 0, NULL, espeakINITIALIZE_DONT_EXIT=0x8000)` and `espeak_SetVoiceByName("en-us")`.
  - Loop `espeak_TextToPhonemes(&ptr, espeakCHARS_UTF8=1, espeakPHONEMES_IPA=0x02)` until `ptr` is NULL, concatenating the clauses.
  - espeak-ng has global state, so serialize calls with a `sync.Mutex`.
- **espeak-ng build.** The prototype declared these three functions in the cgo preamble and linked with `#cgo LDFLAGS: -l:libespeak-ng.so.1`. That works without `libespeak-ng-dev`, but production code should include `<espeak-ng/speak_lib.h>`.
- **Punctuation preservation.** Split each chunk on runs matching `(\s*[;:,.!?¡¿—…"«»“”(){}\[\]]+\s*)+`. Phonemize only the text between runs, keep the marks, and join everything with spaces. Exact spacing does not matter because the re-tokenize step normalizes it; only the order of words and marks does.
- **voices.npz.** Open it with `archive/zip`. Each entry is `.npy` v1 (2-byte header length) or v2 (4-byte). Assert `'descr': '<f4'`, `'fortran_order': False` and `'shape': (400, 256)`, then decode little-endian float32. The file is 3.3 MB.
- **Chunking.** Port `_NON_BOUNDARY_ABBREVIATIONS` verbatim from `preprocess.py`. Cover it with the Python tests from `tests/test_text_normalization.py`, translated to Go.
- **WAV.** Write a 44-byte RIFF header, PCM format 1, mono, 24,000 Hz, 16-bit, with samples clamped to [-1, 1] and scaled by 32767. Encode each header field separately: `binary.Write` rejects `[]any` and silently corrupted the prototype's first header when its error was ignored.

**Phonemizer and tokenizer, as prototyped** (these passed the golden test)

```go
/*
#cgo LDFLAGS: -l:libespeak-ng.so.1
#include <stdlib.h>
int espeak_Initialize(int output, int buflength, const char *path, int options);
int espeak_SetVoiceByName(const char *name);
const char *espeak_TextToPhonemes(const void **textptr, int textmode, int phonememode);
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
		sb.WriteString(C.GoString(C.espeak_TextToPhonemes(&ptr, 1 /*UTF8*/, 0x02 /*IPA*/)))
	}
	return sb.String()
}

const punctMarks = `;:,.!?¡¿—…"«»“”(){}[]`

var punctRe = regexp.MustCompile(`(\s*[` + regexp.QuoteMeta(punctMarks) + `]+\s*)+`)

func phonemize(text string) string {
	var parts []string
	last := 0
	for _, m := range punctRe.FindAllStringIndex(text, -1) {
		if seg := strings.TrimSpace(text[last:m[0]]); seg != "" {
			parts = append(parts, strings.TrimSpace(espeakIPA(seg)))
		}
		parts = append(parts, strings.TrimSpace(text[m[0]:m[1]]))
		last = m[1]
	}
	if seg := strings.TrimSpace(text[last:]); seg != "" {
		parts = append(parts, strings.TrimSpace(espeakIPA(seg)))
	}
	return strings.Join(parts, " ")
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

Copy the IPA string byte-for-byte from `onnx_model.py` rather than retyping it. It contains a combining character (U+0329) and straight apostrophes that are easy to lose.

## API, CLI and server

The library exposes one long-lived `*TTS` per process. The CLI and the server differ only in how text arrives and audio leaves.

**Go API**

```go
package kittentts

type Device string // "cpu" | "cuda"

type Config struct {
	ModelDir       string // holds config.json, the .onnx file and voices.npz
	ORTLibraryPath string // e.g. third_party/onnxruntime/lib/libonnxruntime.so.1.29.1
	Device         Device
	CUDADeviceID   int
	IntraOpThreads int  // 0 = ONNX Runtime default
	CleanText      bool // run the normalizer first; default false, like Python
}

func New(cfg Config) (*TTS, error)
func (t *TTS) Voices() []string // Bella, Jasper, Luna, Bruno, Rosie, Hugo, Kiki, Leo
func (t *TTS) Generate(ctx context.Context, text, voice string, speed float32) ([]float32, error)
func (t *TTS) GenerateStream(ctx context.Context, text, voice string, speed float32) iter.Seq2[[]float32, error]
func (t *TTS) Close() error

const SampleRate = 24000
func EncodeWAV(w io.Writer, samples []float32) error
```

- `New` loads the library, initializes the environment, verifies the model's inputs and outputs, loads the voices and creates one `DynamicAdvancedSession`. If the device is `cuda` and appending the CUDA provider fails, it returns an error. Callers decide whether to retry on CPU; a silent fallback would hide a broken GPU install.
- `Generate` checks `ctx` between chunks; a single ONNX Runtime run cannot be interrupted. `GenerateStream` yields one trimmed chunk at a time.
- Voices can be given as display names or raw keys. Unknown voices return an error that lists the valid names.

**CLI** (`kitten-cli`)

```
kitten-cli -text "Hello there." -voice Bruno -speed 1.0 -out out.wav \
  -model-dir models/kitten-tts-mini-0.8 \
  -onnxruntime_lib third_party/onnxruntime/lib/libonnxruntime.so.1.29.1 \
  -device cpu -threads 0 [-clean] [-in file.txt]
```

`-onnxruntime_lib` follows the examples repo's flag name and defaults to the `ONNXRUNTIME_SHARED_LIBRARY_PATH` environment variable.

**HTTP server** (`kitten-server`)

| Method | Path | Request | Response |
| --- | --- | --- | --- |
| POST | `/v1/tts` | JSON `{"text", "voice", "speed", "format": "wav" or "pcm_f32", "stream": false}` | `audio/wav`, or `application/octet-stream` for little-endian float32 |
| GET | `/v1/voices` | none | `["Bella", …]` |
| GET | `/healthz` | none | `200` once the session is loaded |

- **Limits.** Maximum text length (default 5,000 characters, else 413), a semaphore sized to the CPU budget (else 503 with `Retry-After`), and per-request timeouts through `ctx`.
- **Streaming.** With `stream: true`, send a WAV header with a data size of `0xFFFFFFFF`, then flush each chunk's PCM as soon as it is generated. Most players accept this.
- **Concurrency.** Share one session across requests. A single run already uses every intra-op thread, so more concurrent runs mostly add latency. Start with a semaphore of 1 to 2 on CPU and measure.

## CPU and GPU builds

Both builds use the same Go binary; only the ONNX Runtime library passed at run time and `-device` change. The GPU gives a modest win: warm runs were 1.55× faster on an RTX 4090. The model's int8 operators have no CUDA kernels, so ONNX Runtime keeps them on the CPU.

| Setup (77 tokens, 4.84 s of audio, warm) | Run time | Real-time factor |
| --- | --- | --- |
| CPU, ONNX Runtime 1.29.1, default threads | 2.77 s | 0.57 |
| CUDA provider, ONNX Runtime 1.29.1 gpu_cuda12, RTX 4090 | 1.78 s | 0.37 |

**CPU build**

```bash
# system packages (Debian/Ubuntu)
sudo apt install espeak-ng libespeak-ng-dev

# ONNX Runtime 1.29.1, CPU
curl -LO https://github.com/microsoft/onnxruntime/releases/download/v1.29.1/onnxruntime-linux-x64-1.29.1.tgz
tar xzf onnxruntime-linux-x64-1.29.1.tgz -C third_party/

# model
for f in config.json kitten_tts_mini_v0_8.onnx voices.npz; do
  curl -L --create-dirs -o models/kitten-tts-mini-0.8/$f \
    https://huggingface.co/KittenML/kitten-tts-mini-0.8/resolve/main/$f
done

CGO_ENABLED=1 go build -o bin/ ./cmd/...
bin/kitten-cli -device cpu \
  -onnxruntime_lib third_party/onnxruntime-linux-x64-1.29.1/lib/libonnxruntime.so.1.29.1 \
  -text "Hello from Go." -voice Bruno -out out.wav
```

**NVIDIA GPU build**

1. Download `onnxruntime-linux-x64-gpu_cuda12-1.29.1.tgz` from the same release. Its `lib/` holds `libonnxruntime.so.1.29.1`, `libonnxruntime_providers_shared.so` and `libonnxruntime_providers_cuda.so`; keep them together.
2. Provide CUDA 12.x and cuDNN 9 libraries. On this machine the system CUDA libraries are 12.0 and cuDNN is missing. The sudo-free route that worked mirrors KittenTTS's `requirements_gpu.txt`:

   ```bash
   uv venv .cuda && VIRTUAL_ENV=.cuda uv pip install "nvidia-cudnn-cu12>=9,<10" \
     nvidia-cublas-cu12 nvidia-cuda-runtime-cu12 nvidia-curand-cu12 nvidia-cufft-cu12 \
     nvidia-cusolver-cu12 nvidia-cusparse-cu12 nvidia-nvjitlink-cu12
   export LD_LIBRARY_PATH=$(ls -d $PWD/.cuda/lib/python3*/site-packages/nvidia/*/lib | paste -sd:)
   ```

   This installed cuDNN 9.27 and cuBLAS 12.9. Alternatively, install NVIDIA's CUDA 12 and cuDNN 9 packages system-wide. The `gpu_cuda13` build pairs with CUDA 13 + cuDNN 9.
3. Run with `-device cuda -onnxruntime_lib third_party/onnxruntime-linux-x64-gpu_cuda12-1.29.1/lib/libonnxruntime.so.1.29.1`. In Go this is `NewCUDAProviderOptions()`, then `Update(map[string]string{"device_id": "0"})`, then `opts.AppendExecutionProviderCUDA(co)`.

**Where the nodes land on GPU**, from verbose ORT logs via `ort.InitializeEnvironment(ort.WithLogLevelVerbose())`:

- CPU: 559 nodes, including all 135 `MatMulInteger`, 125 `DynamicQuantizeLinear`, 74 `ConvInteger` and 6 `DynamicQuantizeLSTM`.
- CUDA: 2,511 nodes, including 503 inserted `MemcpyFromHost`/`MemcpyToHost` copies.
- TensorRT was not tested. It would face the same dynamic-quantization operators, so it is not recommended as a first step.
- A float32 export of the model, if KittenML publishes one or it is re-exported from the PyTorch weights, is the lever for a real GPU speed-up. That is out of scope here.

## Testing, risks and references

Phonemes and token ids are deterministic and must match the Python reference exactly. Audio is not deterministic, so audio tests check invariants instead.

**Tests**

- **Golden vectors.** `scripts/make_golden.py` (Appendix A) runs phonemizer (against system `libespeak-ng.so.1`), the reference `chunk_text` and `TextCleaner` over a sentence list. It writes `testdata/golden.json` with `{chunk, phonemes, ids, ref_id}` per chunk, and Go tests require exact equality. The prototype already passes 5 chunks covering commas, quotes, `—`, `…`, parentheses, `Dr.` and `a.m.`.
- **Chunking.** Port the cases from `tests/test_text_normalization.py` plus edge cases: a 400-character sentence, text with no terminal punctuation, and an empty string.
- **Model contract.** At startup and in a test, `GetInputOutputInfo` must report exactly the five names, types and shapes in the Model artifacts table.
- **Audio invariants.** Require `len(waveform) == sum(duration) × 600` (held on CPU and GPU for all four models), a non-empty trimmed output, RMS > 0.01, and the sample rate and length in the WAV header. Run these once per model directory found under `models/`.
- **Speed priors.** A unit test with the nano config must pass `speed = 0.8` for Bella and `0.9` for Hugo into the `speed` tensor, and `1.0` for mini.
- **Server.** Use `httptest` for the 200, 400 (unknown voice), 413 and 503 paths, and check the streamed WAV header.
- **Benchmark.** Add `BenchmarkGenerate` with a fixed sentence and report the real-time factor on CPU and CUDA.

**Risks and open questions**

| Risk | Impact | Mitigation |
| --- | --- | --- |
| espeak-ng version drift | Different phonemes and ids than the Python reference | Record the espeak-ng version in `golden.json`; regenerate on upgrade |
| Bundled Python `espeakng_loader` data path is broken | The Python reference fails with "phontab: No such file" | Point phonemizer at the system `libespeak-ng.so.1` in `make_golden.py` |
| onnxruntime_go and ONNX Runtime drift | Crashes or missing symbols on a version mismatch | Pin both; check `ort.GetVersion()` at startup |
| espeak-ng is GPL-3.0 | Linking it through cgo affects how the binary can be distributed | Decide before shipping: link it, or run `espeak-ng --ipa` as a subprocess |
| GPU gives a small speed-up for int8 models | int8 operators stay on CPU | Default to CPU; treat CUDA as opt-in; use nano-fp32 if GPU latency matters |
| cgo | No simple cross-compiling; `CGO_ENABLED=1` required | Build in a container per target platform |

**Sources** (opened 2026-09-30)

- [yalue/onnxruntime_go](https://github.com/yalue/onnxruntime_go): README and `onnxruntime_go.go` at v1.36.0
- [yalue/onnxruntime_go_examples](https://github.com/yalue/onnxruntime_go_examples): `mnist`, `onnx_list_inputs_and_outputs`
- [KittenML/KittenTTS](https://github.com/KittenML/KittenTTS): `kittentts/onnx_model.py`, `preprocess.py`, `get_model.py`, `requirements_gpu.txt`
- [KittenML/kitten-tts-mini-0.8](https://huggingface.co/KittenML/kitten-tts-mini-0.8): `config.json`, model and voices
- [ONNX Runtime v1.29.1 release](https://github.com/microsoft/onnxruntime/releases/tag/v1.29.1)
- [ONNX Runtime CUDA execution provider](https://onnxruntime.ai/docs/execution-providers/CUDA-ExecutionProvider.html)

## Appendix A: golden-vector script

Run it with `uv run --no-project --with phonemizer python3 scripts/make_golden.py`, with `onnx_model.py` and `preprocess.py` copied from KittenTTS next to it as `onnx_model_ref.py` and `preprocess_ref.py`.

```python
import json, re
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
for line in open("sentences.txt"):
    for chunk in pp.chunk_text(line.strip()):
        ph = b.phonemize([chunk])[0]
        toks = " ".join(re.findall(r"\w+|[^\w\s]", ph))
        out.append({"chunk": chunk, "phonemes": ph,
                    "ids": [0] + tc(toks) + [10, 0],
                    "ref_id": min(len(chunk), 399),
                    "espeak": ".".join(map(str, b.version()))})
json.dump(out, open("testdata/golden.json", "w"), ensure_ascii=False, indent=1)
```
