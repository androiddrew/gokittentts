# 09: Model store and `pull`

**What to build:** A model that isn't on disk is downloaded on first use from its pinned Hugging Face revision and checked against its SHA-256. The revisions and hashes are the ones in `docs/ORIGINAL_SPEC.md` §3.1, shipped as a manifest. The layout is `<models_dir>/<name>/<revision>/` with a `current` pointer. Downloads are atomic (write a temporary file, verify, rename), and concurrent requests for one model share a single download. A failed download returns 503 and leaves no partial file. `download: false` makes a missing model return 503 suggesting `gokittentts pull <name>`. `gokittentts pull` prefetches models. A custom model may set a repo and revision; it is downloaded without a hash check and a warning is logged.

**Blocked by:** 08 (Multi-model config and lazy loading)

**Status:** resolved

- [x] From an empty models directory, requests for all four models succeed
- [x] Native test with a local HTTP fixture: a hash mismatch or broken download returns 503 and leaves no partial file; a retry starts clean
- [x] Concurrent requests for the same missing model trigger exactly one download
- [x] With `download: false`, a missing model returns 503 whose message suggests `gokittentts pull <name>`
- [x] `gokittentts pull <name>...` fills the models directory using the same manifest checks
- [x] A custom model with a repo and revision downloads with a logged warning and no hash check

## Comments

**2026-09-30, implementation notes**

- **Package.** `internal/modelstore` holds the `Manifest`, which pins ORIGINAL_SPEC §3.1: four revisions and twelve SHA-256s, checked against the spec in review. It also holds `Store`, whose API is `New(dir, sources, download)`, `Path(name)`, `Ensure(ctx, name)` and `Downloads(name)`.
- **Layout.** `<models_dir>/<name>/<revision>/` holds the files, and `<name>/current` is a relative symlink to the revision. The engine's model directory is always `<name>/current`, so the library didn't change.
- **Download.**
    - Files go into a staging directory, `<name>/.<revision>.partial-*`. Each is hashed as it streams, fsynced, and checked against the manifest.
    - The directory is made 0755 and renamed to `<revision>`. Then `current` is replaced atomically by renaming a uniquely named temporary link.
    - Any failure removes the staging directory, and an empty `<name>` directory too.
    - Staging directories and temporary links older than 24 h are swept on the next download, as leftovers from processes that died.
    - Redirects are followed, because Hugging Face redirects to its CDN. The client timeout is 30 minutes.
- **Sharing.** Concurrent `Ensure` calls for a model share one download, which runs on its own goroutine. A caller whose request times out or disconnects stops waiting, but the download finishes for the next request.
    - In the server, a model's queue already lets only one request at a time reach the store. Sharing still matters for `pull` and for any other caller.
- **Revisions.** With downloads on, a pinned or custom model whose `current` points at another revision gets the configured revision, so a manifest or `revision:` change takes effect.
    - With `download: false`, whatever revision is on disk is used.
    - A revision already on disk is only re-pointed to. That doesn't count as a download.
- **Errors.** `download: false` with the model missing gives "model X is not in DIR and downloads are off; run gokittentts pull X".
    - `KittenEngine` wraps any store failure in `server.ErrModelUnavailable`, which the handler turns into 503 `server_error` with the store's message.
    - A request whose own context ended still gets the timeout or disconnect handling.
- **Custom models.** `repo:` and `revision:` must be set together. A model named `repo:` and `revision:` downloads its `config.json` first, then the `model_file` and `voices` it names, with no hash checks and a WARN log.
    - Those names must be bare file names.
    - `revision` must not be a path.
    - Setting `repo` on one of the four pinned names is a config error, so it can't silently turn off hash checks.
- **Config.** `download` defaults to true. `config.DefaultModelsDir` is shared with `pull`.
- **Metrics.** `kitten_model_downloads_total{model,status}` with `success`/`failure` now reads `Downloads` through a new `Engine.Downloads` method.
- **CLI.** `gokittentts pull [--config file] [--dir models-dir] [model[,model]...]`.
    - Names may be comma-separated, and flags may come after them, as in ORIGINAL_SPEC's `pull $BAKE_MODELS --dir …`.
    - With `--config` and no names, it pulls every configured model.
    - It always downloads, whatever `download:` says, and prints "downloaded" or "already present" per model.
    - `say` now resolves its model through the store as well, so it downloads a missing pinned model into `--models-dir`.
- **Makefile.** `make test-native` pulls the four models with `go run ./cmd/gokittentts pull`. The per-model revision and hash variables from ticket 08 are gone, since the manifest is the one copy. The native library tests read `<name>/current`.
- **Tests.**
    - `internal/modelstore` runs with plain `go test` against an `httptest` fake Hugging Face that redirects to a `/cdn` path. It needs no native libraries, so it runs everywhere, although the parent spec lists it under the native seam. It covers:
        - download and layout, including permissions, and a model on disk not being fetched again;
        - a hash mismatch, a cut-off body, or a 404 leaving nothing behind, with a clean retry succeeding;
        - 8 concurrent callers making one request per file;
        - a caller that gives up not stopping the download;
        - downloads off, and an unknown model;
        - a custom model, with its warning and no request for unlisted files;
        - a revision change, offline use of another revision, a revision on disk not being re-downloaded, and stale staging being swept.
    - Mutation checks fail the tests: skipping the hash check, not sharing downloads, and keeping staging on failure.
    - The new `internal/server/native_test.go` serves real HTTP through `server.New` and `KittenEngine` with the store.
        - All four models return over half a second of speech.
        - A configured model that is neither on disk nor downloadable gets a 503 with the pull hint. Dropping the `ErrModelUnavailable` wrap fails it with a 500.
- **Checked end to end** (`serve` from an empty models directory, real Hugging Face):
    - Three concurrent first requests for mini all returned 200, and the model was downloaded once.
    - micro, nano-int8 and nano-fp32 each returned 200, and `downloads_total{status="success"}` was 1 for each.
    - With `download: false` the response was 503 with the pull hint.
    - `pull` reported "downloaded" or "already present", and an unknown model or no names exited 1 with a message.
