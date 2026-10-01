# 05: Streaming

**What to build:** Audio starts playing before synthesis ends. By default the response is raw chunked audio, flushed after each synthesized chunk. A streamed `wav` starts with a header whose sizes are `0xFFFFFFFF`. With `stream_format: "sse"`, the server sends `speech.audio.delta` events with base64 audio and a closing `speech.audio.done` whose usage has input tokens = token ids, output tokens = sum of durations, and total = their sum. The chunker splits a first chunk over 120 characters at its first comma, so the first model run is short (a recorded deviation). A client disconnect cancels the remaining chunks and frees the model.

**Blocked by:** 03 (OpenAI speech endpoint)

**Status:** resolved

- [x] Fake-engine HTTP test: raw streaming writes and flushes one body chunk per synthesized chunk
- [x] Fake-engine HTTP test: streamed `wav` begins with a header whose RIFF and data sizes are `0xFFFFFFFF`
- [x] Fake-engine HTTP test: SSE event names, base64 payloads and the `done` usage numbers match the contract
- [x] Fake-engine HTTP test: disconnecting mid-stream stops the engine from producing further chunks
- [x] Native library test: `Stream` stops between chunks when its context is cancelled
- [x] Chunker test: a first chunk over 120 characters splits at its first comma; shorter first chunks don't
- [x] The first-comma split is recorded as a deviation from Python

## Comments

**2026-09-30, implementation notes**

- **Library API change.** `Model.Stream` now yields `kittentts.Chunk{PCM, Tokens, Frames}` instead of `[]float32`. The spec's signature had no way to carry the SSE usage numbers, so the spec's public-library line is updated to match. `Tokens` is the number of token ids the run used. `Frames` is the sum of the run's durations (600 samples each, before trimming). `Synthesize` is unchanged.
- **Every response streams.** OpenAI's default `stream_format` is `audio`, so the server always streams through `iter.Pull2` over `Engine.Stream`. It pulls the first chunk before sending headers, so an unknown model, a bad voice or a first-run failure is still an OpenAI-shaped 500. As a result, `wav` always has `0xFFFFFFFF` sizes, and `Synthesize` is used only by `say`.
- **Encoders.** `audio.Encoder` has `Write(samples)` and `Close()`, and each in-process `Write` causes at most one write downstream. The server writes through a writer that flushes the response on every write. That gives one flush per chunk for pcm, wav and mp3, and one flush per ffmpeg stdout read for the ffmpeg formats. The encoders:
    - `NewPCMEncoder` writes raw 16-bit PCM.
    - `NewWAVStreamEncoder` sends the header together with the first chunk.
    - `NewMP3Encoder` encodes whole frames and holds the remainder, so chunking doesn't change the bytes (a leaf test checks this), and flushes on `Close`.
    - `NewFFmpegEncoder` keeps one ffmpeg process per request. PCM goes to its stdin, and its stdout is copied to the response as it arrives. If that copy fails, ffmpeg is killed, so it can't block the handler. `EncodeFFmpeg` is gone.
- **Headers.** Status 200 and the headers are written and flushed before the encoder starts. ffmpeg's output is copied from its own goroutine, so this ordering is what avoids a race on the response.
- **SSE.** Each downstream write becomes `data: {"type":"speech.audio.delta","audio":"<base64>"}`. For mp3 that means one delta per chunk plus the final flush. For ffmpeg formats it is whatever ffmpeg emits. After the encoder closes, `data: {"type":"speech.audio.done","usage":{...}}` follows. Any `response_format` works with SSE.
- **Disconnects.** The request context reaches the engine. After writing each chunk, the server stops if the context is done, so the in-flight run finishes and nothing more is pulled. ffmpeg is killed through the context and reaped in `Close`.
- **Errors after the headers.** These are a synthesis error, an encoder write or close error, or an encoder that fails to start. If the client's disconnect doesn't explain the failure, the server panics with `http.ErrAbortHandler`, so the client sees a truncated chunked body rather than audio that looks complete. On every early exit, the encoder's tail (mp3 flush frames, ffmpeg's trailer) is discarded, but the encoder is still closed so ffmpeg is reaped. Tests cover a failing engine and an "ffmpeg" (`false`) that exits immediately.
- **Chunker deviation.** It is recorded in the `chunk` package doc and `FirstMaxLen = 120`, and the tests' `deviation:` cases cover it. The split point is the first comma *followed by whitespace*, so "1,000" never splits. A first chunk with no such comma stays whole, and only the first chunk is ever split.
- **Checked end to end** (`serve`, mini on CPU, a 4-chunk paragraph):
    - First byte arrived after about 2.4 s and the full response after about 10.5 s, for every format and for SSE.
    - The SSE usage (775 frames) matched the PCM received.
    - Whisper (small.en) transcribed streamed pcm, mp3, wav, opus and SSE-reassembled mp3 correctly.
    - When curl gave up 2.5 s in, the log showed the stream ending one chunk later, and no ffmpeg processes were left.
    - With the official `openai` SDK, `with_streaming_response` received raw PCM incrementally, and `stream_format="sse"` gave delta, delta, delta, done with usage.
- **For review.** Because every response streams, a client that wanted a non-streaming `wav` now gets `0xFFFFFFFF` sizes rather than real ones. That matches OpenAI's streaming default, but it is a behavior change from ticket 03.
- **Test note.** The disconnect test gates the fake engine's chunks. A mutation check confirmed it fails (rather than hangs) if the server ignores the cancelled context.
