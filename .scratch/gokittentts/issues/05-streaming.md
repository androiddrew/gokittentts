# 05: Streaming

**What to build:** Audio starts playing before synthesis ends. By default the response is raw chunked audio, flushed after each synthesized chunk. A streamed `wav` starts with a header whose sizes are `0xFFFFFFFF`. With `stream_format: "sse"`, the server sends `speech.audio.delta` events with base64 audio and a closing `speech.audio.done` whose usage has input tokens = token ids, output tokens = sum of durations, and total = their sum. The chunker splits a first chunk over 120 characters at its first comma, so the first model run is short (a recorded deviation). A client disconnect cancels the remaining chunks and frees the model.

**Blocked by:** 03 (OpenAI speech endpoint)

**Status:** ready-for-agent

- [ ] Fake-engine HTTP test: raw streaming writes and flushes one body chunk per synthesized chunk
- [ ] Fake-engine HTTP test: streamed `wav` begins with a header whose RIFF and data sizes are `0xFFFFFFFF`
- [ ] Fake-engine HTTP test: SSE event names, base64 payloads and the `done` usage numbers match the contract
- [ ] Fake-engine HTTP test: disconnecting mid-stream stops the engine from producing further chunks
- [ ] Native library test: `Stream` stops between chunks when its context is cancelled
- [ ] Chunker test: a first chunk over 120 characters splits at its first comma; shorter first chunks don't
- [ ] The first-comma split is recorded as a deviation from Python
