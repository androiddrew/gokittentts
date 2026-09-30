# 02: Tracer bullet: `say` writes a WAV with mini on CPU

**What to build:** The first end-to-end path. `gokittentts say --onnxruntime-lib <versioned lib> --voice Bruno --out out.wav "Hello from Go."` produces a correct, audible WAV from `kitten-tts-mini-0.8` on CPU. It goes through the public `kittentts` library (`NewEngine`, `Engine.Model`, `Model.Voices`, `Model.Synthesize`, `Model.Stream`, `Engine.Close`, `SampleRate`). The model directory can be a local path passed in by hand; the model store comes later.

The pipeline covers the chunker port (`chunk_text` at 400 characters, `ensure_punctuation`, the sentence-boundary rules), the espeak-ng phonemizer through cgo behind the `Phonemizer` interface (with the process-wide mutex and the hand-written punctuation scanner), the tokenizer, the `voices.npz` reader, the style row, the speed prior, the 5,000-sample trim and a WAV writer. `Request` carries `Normalize` and `Markdown` flags from the start; they do nothing until tickets 10 and 11.

**Blocked by:** 01 (Golden reference generator)

**Status:** resolved

- [x] `say` writes a 24 kHz mono 16-bit WAV that plays back as the input sentence
- [x] Startup fails immediately when the loaded ONNX Runtime is not 1.29.1
- [x] Loading a model asserts the model contract (inputs `input_ids`, `style`, `speed`; outputs `waveform`, `duration`, with the right types) and fails at load time on a mismatch
- [x] Phonemes and token ids equal the goldens from 01 for every golden chunk (needs espeak-ng, `native` tag)
- [x] The punctuation scanner's table tests cover decimal and thousands separators, trailing periods, runs of marks, and leading and trailing marks
- [x] The chunker runs the ported Python tests plus the 400-character split, missing terminal punctuation and empty input
- [x] Voices-file header parsing has table tests (v1 and v2 headers; rejects anything not `<f4`, C order, `(400, 256)`)
- [x] WAV byte layout has a table test
- [x] Native library tests: `len(waveform) == sum(duration) × 600`; trimmed output is non-empty with RMS > 0.01; `Stream` yields one trimmed chunk at a time
- [x] `go test ./...` passes without native libraries; `make test-native` runs everything

## Comments

**2026-09-30, implementation notes**

- **Layout.** `kittentts` (public library), `internal/{tokenize,phonemize,phonemize/espeak,chunk,npz,audio}`, `cmd/gokittentts` with `say`.
- **espeak build tag (owner's choice).** Go compiles every package during `go test ./...`, even packages with no untagged tests, so the cgo backend (`#include <espeak-ng/speak_lib.h>`, `-lespeak-ng`) is built only with `-tags espeak` (or `native`). Without the tag, a stub's `espeak.New()` returns "built without espeak-ng; build with -tags espeak". `make build` passes the tag, and the README's plain-binary `go build` line now does too. `docs/ORIGINAL_SPEC.md` §11.1 and §11.3 still show `go build` without it; the Dockerfiles (ticket 15) must pass it. `go test ./...` was checked in a clean `golang:1.26` container with no espeak-ng or ONNX Runtime.
- **Phoneme parity.** Phoneme *strings* match the golden byte for byte, as well as the ids. `internal/phonemize` ports phonemizer's `Punctuation.preserve`/`restore`, with the regex rewritten as a rune scanner for the decimal rule. The espeak backend uses phonemizer's `_` phoneme-separator mode and its line post-processing. The scanner's table-test expectations were produced by running upstream phonemizer's own preserve/restore over the same fake backend.
- **Chunker expectations** come from the vendored `preprocess_ref.chunk_text`.
- **Makefile.** `make test-native` fetches ONNX Runtime 1.29.1 and 1.30.0 into `third_party/` (1.30.0 is only for the "not 1.29.1" test, since onnxruntime_go can still load a newer runtime) and mini into `models/`, pinned to its revision and checked against the §3.1 SHA-256s. All three are gitignored. The model store (ticket 09) replaces the mini download.
- **Contract fixture.** `kittentts/testdata/bad-contract/model.onnx` (331 bytes, built by the `make_model.py` next to it) has the right names but a float32 `duration`.
- **`say` flags.** `--onnxruntime-lib` (required), `--models-dir` (default `models`), `--model` (default `kitten-tts-mini-0.8`, a directory under `--models-dir`), `--voice` (default `Leo`, Python's default), `--speed` (0.5–2.0), `--out` (default `out.wav`).
- **Library details.** `Request.Speed` of 0 means 1.0. `Normalize` and `Markdown` are accepted and ignored until tickets 10 and 11. `ModelConfig.Device` must be `""` or `cpu` until ticket 14. A process can open one `Engine`, because ONNX Runtime's environment is process-wide.
- **Listening check.** Whisper (small.en) transcribes `say` output correctly, for example "Hello from Go!". It hears "1,500 dollars" as "$500", but the Python reference run with the same ids and style row gives the same transcript, so that is the mini model's delivery, not the port.

