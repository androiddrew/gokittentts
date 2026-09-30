# 08: Multi-model config and lazy loading

**What to build:** All four KittenTTS 0.8 models (mini, micro, nano-int8, nano-fp32) can be listed in the config and picked per request with the `model` field. A model loads on first use, so startup is fast and unused models cost no memory. Each model's file names come from its own `config.json` (the two nano repos share a file name), and its voices from its own directory. `GET /v1/models` lists the configured models and aliases. Models still come from directories already on disk; downloading is ticket 09.

**Blocked by:** 03 (OpenAI speech endpoint)

**Status:** ready-for-agent

- [ ] Requests naming each of the four models return audio from that model
- [ ] A model is not loaded until its first request, and concurrent first requests load it once
- [ ] Native library test: every model in the models directory passes the contract check on load
- [ ] Native library test: speed priors apply (nano: 0.8 for Bella, 0.9 for Hugo; mini: 1.0)
- [ ] Native library test: voices come from the model's own directory
- [ ] `GET /v1/models` lists the configured models and the aliases
