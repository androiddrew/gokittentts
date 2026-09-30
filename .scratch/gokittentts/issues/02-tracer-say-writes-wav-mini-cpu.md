# 02: Tracer bullet: `say` writes a WAV with mini on CPU

**What to build:** The first end-to-end path. `gokittentts say --onnxruntime-lib <versioned lib> --voice Bruno --out out.wav "Hello from Go."` produces a correct, audible WAV from `kitten-tts-mini-0.8` on CPU. It goes through the public `kittentts` library (`NewEngine`, `Engine.Model`, `Model.Voices`, `Model.Synthesize`, `Model.Stream`, `Engine.Close`, `SampleRate`). The model directory can be a local path passed in by hand; the model store comes later.

The pipeline covers the chunker port (`chunk_text` at 400 characters, `ensure_punctuation`, the sentence-boundary rules), the espeak-ng phonemizer through cgo behind the `Phonemizer` interface (with the process-wide mutex and the hand-written punctuation scanner), the tokenizer, the `voices.npz` reader, the style row, the speed prior, the 5,000-sample trim and a WAV writer. `Request` carries `Normalize` and `Markdown` flags from the start; they do nothing until tickets 10 and 11.

**Blocked by:** 01 (Golden reference generator)

**Status:** ready-for-agent

- [ ] `say` writes a 24 kHz mono 16-bit WAV that plays back as the input sentence
- [ ] Startup fails immediately when the loaded ONNX Runtime is not 1.29.1
- [ ] Loading a model asserts the model contract (inputs `input_ids`, `style`, `speed`; outputs `waveform`, `duration`, with the right types) and fails at load time on a mismatch
- [ ] Phonemes and token ids equal the goldens from 01 for every golden chunk (needs espeak-ng, `native` tag)
- [ ] The punctuation scanner's table tests cover decimal and thousands separators, trailing periods, runs of marks, and leading and trailing marks
- [ ] The chunker runs the ported Python tests plus the 400-character split, missing terminal punctuation and empty input
- [ ] Voices-file header parsing has table tests (v1 and v2 headers; rejects anything not `<f4`, C order, `(400, 256)`)
- [ ] WAV byte layout has a table test
- [ ] Native library tests: `len(waveform) == sum(duration) × 600`; trimmed output is non-empty with RMS > 0.01; `Stream` yields one trimmed chunk at a time
- [ ] `go test ./...` passes without native libraries; `make test-native` runs everything
