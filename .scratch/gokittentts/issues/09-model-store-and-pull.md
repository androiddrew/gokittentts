# 09: Model store and `pull`

**What to build:** A model that isn't on disk is downloaded on first use from its pinned Hugging Face revision and checked against its SHA-256. The revisions and hashes are the ones in `docs/ORIGINAL_SPEC.md` §3.1, shipped as a manifest. The layout is `<models_dir>/<name>/<revision>/` with a `current` pointer. Downloads are atomic (write a temporary file, verify, rename), and concurrent requests for one model share a single download. A failed download returns 503 and leaves no partial file. `download: false` makes a missing model return 503 suggesting `gokittentts pull <name>`. `gokittentts pull` prefetches models. A custom model may set a repo and revision; it is downloaded without a hash check and a warning is logged.

**Blocked by:** 08 (Multi-model config and lazy loading)

**Status:** ready-for-agent

- [ ] From an empty models directory, requests for all four models succeed
- [ ] Native test with a local HTTP fixture: a hash mismatch or broken download returns 503 and leaves no partial file; a retry starts clean
- [ ] Concurrent requests for the same missing model trigger exactly one download
- [ ] With `download: false`, a missing model returns 503 whose message suggests `gokittentts pull <name>`
- [ ] `gokittentts pull <name>...` fills the models directory using the same manifest checks
- [ ] A custom model with a repo and revision downloads with a logged warning and no hash check
