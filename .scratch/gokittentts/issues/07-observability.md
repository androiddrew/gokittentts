# 07: Observability

**What to build:** Every request produces one structured slog JSON line with model, voice, format, input and output sizes, RTF, time to first audio, queue wait and status. `/metrics` exposes Prometheus metrics for requests, synthesis time, RTF, time to first audio, queue depth, loaded models and downloads, and stays open when auth is on.

**Blocked by:** 06 (Load control and auth)

**Status:** resolved

- [x] Fake-engine HTTP test: one log line per request, with every listed field, for both success and error
- [x] `/metrics` serves Prometheus text including all listed metric families
- [x] `/metrics` answers without a key when auth is on
- [x] Queue depth and queue wait reflect the per-model queue from 06

## Comments

**2026-09-30, implementation notes**

- **Log line.** Each `POST /v1/audio/speech` ends with one slog line, `msg: "speech"`, written by a defer in the handler. The defer also runs when a broken stream is aborted.
    - Fields: `model`, `voice`, `format`, `input_chars`, `chunks`, `audio_seconds`, `synthesis_seconds`, `rtf`, `time_to_first_audio_seconds`, `queue_wait_seconds`, `duration_seconds` and `status`, plus `error` when the request failed.
    - `model`, `voice` and `format` are the resolved names, or the requested ones if they don't resolve. Fields that don't apply are 0.
    - `synthesis_seconds` is time spent inside the engine; it excludes queue wait and encoding. `rtf` is that time divided by `audio_seconds`.
    - `time_to_first_audio_seconds` runs from the request's arrival to the first audio byte written to the response, so it covers ffmpeg formats too.
    - The level is ERROR for 5xx and INFO otherwise. The old scattered per-request logs ("synthesis failed", "client disconnected", "request timed out", "stream failed", …) are folded into this line.
- **Status.** Normally this is the HTTP status. A client that disconnects is 499, as in nginx. A stream that fails after its 200 headers is 500, or 503 if it timed out.
- **Not logged.** A 401 from the auth wrapper is not logged, because the line's fields are the speech request's. `/v1/voices`, `/healthz` and `/metrics` aren't logged either.
- **Metrics.** `prometheus/client_golang` v1.24.1 serves them, with one registry per `server.New`. All seven families from ORIGINAL_SPEC §10.6 are present, plus the standard Go and process collectors.
    - Every configured model gets zero series up front: requests per supported format with status 200, the histograms, loaded, queue depth, and downloads with status `success`/`failure`.
    - In `kitten_requests_total`, an unknown model or format gets an empty label, so clients can't create series.
    - The three histograms observe successful (200) requests only.
- **Queue depth.** `kitten_queue_depth` counts requests admitted to ticket 06's queue that are waiting for the run slot. The running request is not counted. `queue_wait_seconds` is the time spent in `queue.enter`.
- **Left for later tickets.**
    - `kitten_model_loaded` is set to 1 by the server once a model has produced a chunk. That stands in for real load state, so ticket 08 (lazy loading) should hook the engine's load event instead.
    - `kitten_model_downloads_total` is registered but never incremented. Ticket 09 should count downloads into it.
- **Config.** `metrics` (default true; false makes `/metrics` 404) and `log_format` (`json`, the default, or `text`; anything else fails validation) come from the spec's config example. `serve` installs the matching slog handler on stderr.
- **Tests** (`internal/server/observability_test.go`). They work through HTTP, scraping `/metrics` and parsing the JSON log lines.
    - The cases are: one line with every field for success, unknown model, unknown voice, malformed body, engine failure, failure mid-stream and a client that left.
    - Field values are checked for a three-chunk stream. All families are present on a fresh server, and request counts and labels are checked.
    - `/metrics` is open with a key set, and returns 404 with metrics off.
    - Queue depth reads 1 while a request waits and 0 after. The queued request logs a wait of at least 50 ms.
    - Mutation checks fail the tests: dropping the waiting counter, putting `/metrics` behind the key, and not counting chunks.
- **Checked end to end** (`serve`, mini, `max_queue: 1`, key set):
    - `/metrics` answered 200 without a key.
    - Of three concurrent wav requests, two gave 200 and one 429.
    - The queued request logged `queue_wait_seconds: 3.70` and an RTF of about 0.57.
    - The counters matched (`200`×3, `429`×1, `400`×1).
