# 06: Load control and auth

**What to build:** The server degrades predictably under load and can require a key. Each model runs one synthesis at a time, with a bounded queue; overflow returns 429 with `Retry-After: 1`. A request timeout covers both queue wait and synthesis. When `KITTEN_API_KEY` is set, `/v1/*` requires `Authorization: Bearer <key>`, compared in constant time. `/healthz` stays open. When auth is off and the listen address is beyond loopback, startup logs a warning.

**Blocked by:** 03 (OpenAI speech endpoint)

**Status:** ready-for-agent

- [ ] Fake-engine HTTP test: with the queue full, the next request gets 429 with `Retry-After: 1` in the OpenAI error shape
- [ ] Fake-engine HTTP test: a request stuck past the timeout ends with an error and frees its queue slot
- [ ] Fake-engine HTTP test: with a key set, a missing or wrong key gets 401 on `/v1/*`; the right key succeeds
- [ ] `/healthz` answers without a key when auth is on
- [ ] With auth off and a non-loopback listen address, startup logs a warning; with loopback it doesn't
