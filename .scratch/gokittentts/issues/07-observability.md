# 07: Observability

**What to build:** Every request produces one structured slog JSON line with model, voice, format, input and output sizes, RTF, time to first audio, queue wait and status. `/metrics` exposes Prometheus metrics for requests, synthesis time, RTF, time to first audio, queue depth, loaded models and downloads, and stays open when auth is on.

**Blocked by:** 06 (Load control and auth)

**Status:** ready-for-agent

- [ ] Fake-engine HTTP test: one log line per request, with every listed field, for both success and error
- [ ] `/metrics` serves Prometheus text including all listed metric families
- [ ] `/metrics` answers without a key when auth is on
- [ ] Queue depth and queue wait reflect the per-model queue from 06
