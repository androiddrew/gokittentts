# 04: mp3 default and ffmpeg formats

**What to build:** A request with no `response_format` gets mp3, as it does from OpenAI, encoded in pure Go with shine-mp3. `opus`, `aac` and `flac` are produced by piping PCM through ffmpeg when it is available. When ffmpeg is missing (or disabled for tests), those formats return a 400 naming the supported formats.

**Blocked by:** 03 (OpenAI speech endpoint)

**Status:** ready-for-agent

- [ ] A request without `response_format` returns `audio/mpeg` that decodes back to a plausible length (leaf table test)
- [ ] `opus`, `aac` and `flac` return playable audio with the right content types when ffmpeg is present
- [ ] With ffmpeg disabled, those formats return a 400 listing the supported formats (fake-engine HTTP test)
- [ ] The shine-mp3 (LGPL-2.0) and ffmpeg license notes are recorded for the image's licenses directory
