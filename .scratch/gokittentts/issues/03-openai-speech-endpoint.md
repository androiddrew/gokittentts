# 03: OpenAI speech endpoint (wav/pcm, one model)

**What to build:** `gokittentts serve` reads one YAML config and serves `POST /v1/audio/speech`, so an OpenAI client pointed at it gets `wav` or `pcm` audio back from the configured model. The server holds the engine behind an interface and is tested with `httptest` and a fake engine.

It covers model resolution (name, then `model_aliases`, so `tts-1`, `tts-1-hd` and `gpt-4o-mini-tts` work); voice resolution (Kitten name, then `expr-voice-*` key, then the configurable OpenAI voice map, then 400 listing the valid names), including `voice` given as an object with an `id`; speed validation (0.25–4.0 accepted, anything else 400) and clamping to the configured range; `max_input_chars`; `instructions` accepted and ignored; the `normalize` and `markdown` fields passed through to the library `Request`; `GET /v1/voices`; and OpenAI-shaped error bodies.

**Blocked by:** 02 (Tracer bullet)

**Status:** ready-for-agent

- [ ] The OpenAI SDK (or curl) gets playable `wav` and `pcm` audio from a running server
- [ ] `pcm` is raw 16-bit little-endian 24 kHz mono
- [ ] An unknown model returns 400; an unknown voice returns 400 naming the valid voices; both use `{"error": {message, type, param, code}}`
- [ ] Out-of-range `speed` returns 400; in-range speed reaches the engine clamped to the configured range (default 0.5–2.0)
- [ ] Input over `max_input_chars` (default 4096) returns 400
- [ ] `normalize` and `markdown` default to true and reach the engine as sent
- [ ] `GET /v1/voices` lists the Kitten names, keys and the OpenAI map
- [ ] `KITTEN_DEFAULT_MODEL` and `KITTEN_LISTEN` override the config
- [ ] Startup fails on an unknown device, an unconfigured default model, a voice-map entry naming a non-Kitten voice, or an inverted speed range
- [ ] All of the above are covered by fake-engine HTTP tests that need no native libraries
