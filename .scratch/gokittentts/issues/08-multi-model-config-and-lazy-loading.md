# 08: Multi-model config and lazy loading

**What to build:** All four KittenTTS 0.8 models (mini, micro, nano-int8, nano-fp32) can be listed in the config and picked per request with the `model` field. A model loads on first use, so startup is fast and unused models cost no memory. Each model's file names come from its own `config.json` (the two nano repos share a file name), and its voices from its own directory. `GET /v1/models` lists the configured models and aliases. Models still come from directories already on disk; downloading is ticket 09.

**Blocked by:** 03 (OpenAI speech endpoint)

**Status:** resolved

- [x] Requests naming each of the four models return audio from that model
- [x] A model is not loaded until its first request, and concurrent first requests load it once
- [x] Native library test: every model in the models directory passes the contract check on load
- [x] Native library test: speed priors apply (nano: 0.8 for Bella, 0.9 for Hugo; mini: 1.0)
- [x] Native library test: voices come from the model's own directory
- [x] `GET /v1/models` lists the configured models and the aliases

## Comments

**2026-09-30, implementation notes**

- **Already on main.** Several pieces predate this ticket:
    - `Engine.Model` already loaded lazily.
    - File names already came from each model's `config.json`.
    - The contract test already looped over every model in `KITTEN_MODELS_DIR`.
    - Routing by the request's `model` field already worked.
- **Loading.** The engine now has one slot per configured model, each with its own lock. Before, one engine-wide lock was held through a load, so a request for a loaded model waited while another model loaded.
    - Concurrent first calls load the model once.
    - A failed load is not remembered: the next request tries again, re-reading the model's files. This suits ticket 09, where a download may fill the directory later.
    - `Close` waits for loads in progress, and a load that starts after `Close` is refused.
- **`Engine.Loaded(name)`** is new on the library and on the server's `Engine` interface. `kitten_model_loaded` is now a gauge read from it, replacing ticket 07's "set to 1 after a chunk" approximation.
- **Speed priors.** The prior moved from `run` into `Stream`, still looked up by the resolved `expr-voice-*` key. `run` and the test-only `RunChunk(m, text, voice, speed)` now run at exactly the speed given.
    - `TestSpeedPriors` checks that a stream at speed 1 has the same summed durations as a raw run at the prior: 0.8 for Bella and 0.9 for Hugo on both nanos, 1.0 on mini and micro. Durations are deterministic, unlike the samples.
    - The test also checks that a raw run at speed 1 differs from one at the prior, so it can see the prior at all.
    - It fails, rather than skips, if a model is missing.
    - Mutation check: dropping the prior fails it (187 frames against 230).
- **Own voices.** `TestVoicesComeFromTheModelsOwnDirectory` builds a model in a temporary directory. Its `.onnx` is linked from a real model, its `config.json` names `solo.npz`, and that file holds only `expr-voice-2-f` as "Solo".
    - The model's `Voices()` must be `[Solo]`.
    - `Solo` and `expr-voice-2-f` must synthesize, and `Bella` and `expr-voice-3-m` must not.
- **Lazy loading test.** `TestModelsLoadOnFirstUseAndOnce` checks three things:
    - `NewEngine` accepts a model whose directory doesn't exist, which shows nothing loads up front.
    - That model reports not loaded, both before and after its load fails.
    - 8 concurrent first calls on an unused model all get the same `*Model`, and it then reports loaded.
- **`GET /v1/models`** returns OpenAI's `{"object":"list","data":[{"id","object":"model","created":0,"owned_by":"kittenml"}]}`. It lists the configured models, sorted, then the aliases, each with an extra `alias_for` naming the model it resolves to. It sits behind the API key like the rest of `/v1/*`.
- **Makefile.** `make test-native` fetches all four models through one pattern rule. Each model's revision and SHA-256s, from ORIGINAL_SPEC §3.1, live in `REV_<model>` and `SHA_<model>` variables. The model store (ticket 09) replaces this.
- **Checked end to end** (`serve` with all four models):
    - Ready in 0.12 s at 39 MB RSS, with `kitten_model_loaded` at 0 for all four.
    - A WAV request to each model returned 200, and all four then showed loaded, at 825 MB RSS.
    - The nano requests gave 3.17 s of audio against 2.3 s for mini and micro, which is the 0.8 prior.
