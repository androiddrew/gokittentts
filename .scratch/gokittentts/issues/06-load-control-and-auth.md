# 06: Load control and auth

**What to build:** The server degrades predictably under load and can require a key. Each model runs one synthesis at a time, with a bounded queue; overflow returns 429 with `Retry-After: 1`. A request timeout covers both queue wait and synthesis. When `KITTEN_API_KEY` is set, `/v1/*` requires `Authorization: Bearer <key>`, compared in constant time. `/healthz` stays open. When auth is off and the listen address is beyond loopback, startup logs a warning.

**Blocked by:** 03 (OpenAI speech endpoint)

**Status:** resolved

- [x] Fake-engine HTTP test: with the queue full, the next request gets 429 with `Retry-After: 1` in the OpenAI error shape
- [x] Fake-engine HTTP test: a request stuck past the timeout ends with an error and frees its queue slot
- [x] Fake-engine HTTP test: with a key set, a missing or wrong key gets 401 on `/v1/*`; the right key succeeds
- [x] `/healthz` answers without a key when auth is on
- [x] With auth off and a non-loopback listen address, startup logs a warning; with loopback it doesn't

## Comments

**2026-09-30, implementation notes**

- **Config.**
    - `models.<name>.max_queue` is the number of requests that may wait behind the running one. It defaults to 8 when unset, `0` means no waiting, and a negative value fails validation.
    - `limits.request_timeout` is a Go duration (default `120s`) and must be positive.
    - `KITTEN_API_KEY` is read only from the environment, into `Config.APIKey`. It is not a YAML key.
- **Queue.** There is one per configured model, built in `server.New`. A request first takes an admission place, of which there are 1 + `max_queue`. If none is free it gets 429 with `Retry-After: 1` and type `rate_limit_exceeded`. Otherwise it waits for the model's single run slot until its context ends. Both places are released only after the response is finished, including encoding and any ffmpeg tail. A slow client therefore holds the model, and a disconnect frees it after the in-flight chunk.
- **Timeout.** `request_timeout` is a context deadline set before the queue wait, so it covers waiting, synthesis and encoding.
    - If it ends before any audio, either in the queue or before the first chunk, the response is a 503 `server_error` ("request timed out after …").
    - If it ends mid-stream, the response is cut off, as with other mid-stream failures from ticket 05.
    - A model run in progress can't be interrupted, so a request whose deadline passes during a run gets its 503 when that run ends. In the end-to-end check, a queued request with a 6 s timeout answered at 7.8 s.
    - A client disconnect is told apart from a timeout by the request's own context. A disconnected client gets nothing written.
- **Auth.** `/v1/*` is wrapped by `requireKey`. It compares SHA-256 digests with `subtle.ConstantTimeCompare`, so the key's length doesn't leak either. The `Bearer` scheme is case-insensitive. A failure returns 401 `invalid_request_error` with `WWW-Authenticate: Bearer`. `/healthz` (`{"status":"ok"}`) is outside the wrapper. `/metrics` comes with ticket 07.
- **Warning.** `server.New` logs it when there is no API key and `listen` isn't loopback. Loopback means `localhost` or a loopback IP. An empty host (`:8880`), `0.0.0.0`, `[::]`, LAN addresses and other hostnames all warn.
- **Tests.** Everything goes through HTTP, with no hooks into the server's state.
    - `assertQueueOfOne` holds one request in a run that ignores its timeout, then sends two more under a 100 ms timeout. Whatever order those two arrive in, exactly one must get a queue place and then time out (503), and the other must get a 429. Afterwards, a normal request must succeed.
    - `TestRequestTimeout` and `TestDisconnectWhileQueuedFreesThePlace` call it again after their timeouts and disconnects, to prove no place leaked.
    - Mutation checks fail the tests: leaking a timed-out waiter's place, removing the admission limit, and waiting in the queue without the request's context.
    - An earlier version read the queue depth through `export_test.go`. The standards review rejected it as asserting on private state.
- **Config.** A `KITTEN_API_KEY` that is only whitespace fails validation rather than turning on auth with a blank key.
- **Checked end to end** (`serve`, mini, `max_queue: 1`, `request_timeout: 6s`, `KITTEN_API_KEY` set):
    - `/healthz` answered 200 without a key.
    - `/v1/voices` returned 401 without a key or with a wrong one, and 200 with the right one.
    - Four concurrent speech requests gave 200, then 503 (timed out in the queue behind the first), then 429 and 429 with `Retry-After: 1`.
    - With no key, `serve` warned on `:18881` and stayed silent on `127.0.0.1:18881`.
