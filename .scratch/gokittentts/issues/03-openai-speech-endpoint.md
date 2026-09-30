# 03: OpenAI speech endpoint (wav/pcm, one model)

**What to build:** `gokittentts serve` reads one YAML config and serves `POST /v1/audio/speech`, so an OpenAI client pointed at it gets `wav` or `pcm` audio back from the configured model. The server holds the engine behind an interface and is tested with `httptest` and a fake engine.

It covers model resolution (name, then `model_aliases`, so `tts-1`, `tts-1-hd` and `gpt-4o-mini-tts` work); voice resolution (Kitten name, then `expr-voice-*` key, then the configurable OpenAI voice map, then 400 listing the valid names), including `voice` given as an object with an `id`; speed validation (0.25–4.0 accepted, anything else 400) and clamping to the configured range; `max_input_chars`; `instructions` accepted and ignored; the `normalize` and `markdown` fields passed through to the library `Request`; `GET /v1/voices`; and OpenAI-shaped error bodies.

**Blocked by:** 02 (Tracer bullet)

**Status:** resolved

- [x] The OpenAI SDK (or curl) gets playable `wav` and `pcm` audio from a running server
- [x] `pcm` is raw 16-bit little-endian 24 kHz mono
- [x] An unknown model returns 400; an unknown voice returns 400 naming the valid voices; both use `{"error": {message, type, param, code}}`
- [x] Out-of-range `speed` returns 400; in-range speed reaches the engine clamped to the configured range (default 0.5–2.0)
- [x] Input over `max_input_chars` (default 4096) returns 400
- [x] `normalize` and `markdown` default to true and reach the engine as sent
- [x] `GET /v1/voices` lists the Kitten names, keys and the OpenAI map
- [x] `KITTEN_DEFAULT_MODEL` and `KITTEN_LISTEN` override the config
- [x] Startup fails on an unknown device, an unconfigured default model, a voice-map entry naming a non-Kitten voice, or an inverted speed range
- [x] All of the above are covered by fake-engine HTTP tests that need no native libraries

## Comments

**2026-09-30, implementation notes**

- **Layout.** `internal/config` (YAML, defaults, `KITTEN_DEFAULT_MODEL`/`KITTEN_LISTEN` overrides, validation), `internal/server` (`server.New(cfg, Engine)` and the `KittenEngine` adapter over `*kittentts.Engine`), `gokittentts serve --config` (default `/etc/gokittentts/config.yaml`, clean shutdown on SIGINT/SIGTERM), `audio.PCM16`, and `kittentts.Voices`, a static table of the eight voices (names and keys) the server and config validation use before any model loads. A native test checks the table against the model's `config.json`.
- **Until later tickets:**
    - A missing `response_format` gives `wav` (OpenAI's default is mp3; ticket 04 switches it). `mp3`, `opus`, `aac` and `flac` return 400 naming `pcm, wav`.
    - `stream_format: "audio"` is accepted but the response is not streamed yet; `sse` returns 400 (ticket 05).
    - A model's directory is `<models_dir>/<name>` (the model store, ticket 09, adds `<revision>/`).
- **Config.** Unknown keys are ignored, so the full example config from `docs/ORIGINAL_SPEC.md` §9 loads today. If `model_aliases` or `voices` is absent, the defaults from §9 apply. Beyond the four required checks, validation also rejects an alias whose target isn't a configured model and a non-positive `max_input_chars`. All problems are reported together. `serve` also requires `onnxruntime_lib`.
- **Errors.** `{"error": {message, type, param, code}}`, with type `invalid_request_error` (400) or `server_error` (500), `param` naming the field (or null) and `code` always null.
- **Checked end to end.** The official `openai` Python SDK, against `serve` with mini on CPU, got playable `wav` and `pcm`, which Whisper (small.en) transcribed correctly. The SDK raised `BadRequestError` with the OpenAI error body for an unknown voice and model. `KITTEN_LISTEN` moved the listen address, and bad configs failed at startup with every problem listed.
- **After review.**
    - The request body is capped at 64 KiB + 12 bytes × `max_input_chars` (room for every character to be a `\uXXXX\uXXXX` escape); a bigger body gets 413 in the OpenAI shape before it is fully read.
    - Anything after the JSON object is a 400.
    - Whitespace-only `input` is accepted (the spec allows 1 to `max_input_chars` characters) and yields empty audio; only a missing or empty `input` is a 400.
    - `instructions` is logged at debug level.
    - `serve` binds before logging "listening", lets a second signal kill it during the 30 s drain, and closes the engine only after a clean shutdown (a timed-out drain leaves it open, since requests may still be using it).
- **Known interim mismatch.** Config validation accepts `device: cuda`, but the library rejects it until ticket 14, so a CUDA config fails at startup with the library's "device cuda is not supported" error.

