# 04: mp3 default and ffmpeg formats

**What to build:** A request with no `response_format` gets mp3, as it does from OpenAI, encoded in pure Go with shine-mp3. `opus`, `aac` and `flac` are produced by piping PCM through ffmpeg when it is available. When ffmpeg is missing (or disabled for tests), those formats return a 400 naming the supported formats.

**Blocked by:** 03 (OpenAI speech endpoint)

**Status:** resolved

- [x] A request without `response_format` returns `audio/mpeg` that decodes back to a plausible length (leaf table test)
- [x] `opus`, `aac` and `flac` return playable audio with the right content types when ffmpeg is present
- [x] With ffmpeg disabled, those formats return a 400 listing the supported formats (fake-engine HTTP test)
- [x] The shine-mp3 (LGPL-2.0) and ffmpeg license notes are recorded for the image's licenses directory

## Comments

**2026-09-30, implementation notes**

- **Layout.** `audio.WriteMP3` (shine-mp3 `pkg/mp3`, mono, 64 kbps, which at 24 kHz is MPEG-2 layer III) and `audio.EncodeFFmpeg` (PCM16 on stdin, the ORIGINAL_SPEC §10.3 arguments, stdout to the writer, stderr in the error, killed when the request context ends). The server's format table records each content type and whether it needs ffmpeg: `mp3` `audio/mpeg`, `opus` `audio/ogg`, `aac` `audio/aac`, `flac` `audio/flac`.
- **Config.** `ffmpeg` is a path or a name on `PATH`, `ffmpeg` by default. An empty string disables it. `server.New` resolves it once with `exec.LookPath`. A name that can't be found logs a warning and counts as disabled, so a missing ffmpeg never stops startup.
- **Errors.** Without ffmpeg, `opus`, `aac` and `flac` return 400 with `response_format "opus" needs ffmpeg, which is not available; supported formats are mp3, pcm, wav` before synthesis runs. An unknown format lists only the formats this server can produce. If an encoder fails after synthesis, the response is a 500 `server_error`.
- **shine-mp3 details.**
    - `NewEncoder` always picks 128 kbps, so `WriteMP3` resets the bitrate fields it derives from that (index and slots per frame) to 64 kbps.
    - The encoder delays its output by about 1,057 samples, so `WriteMP3` appends 1,152 samples of silence to flush the tail. The output starts with about 44 ms of silence and has no gapless header.
    - Empty input (whitespace-only text) still writes a valid mp3 of two silent frames, as the ffmpeg formats write a valid empty container.
- **Tests.**
    - The leaf test decodes with `hajimehoshi/go-mp3`, a test-only dependency (Apache-2.0).
    - The fake-engine tests set `ffmpeg: ""`, and they also cover a configured path that doesn't exist.
    - `TestFFmpegFormats` uses the real ffmpeg (it skips if ffmpeg isn't on `PATH`) and checks the content type, the container magic, and that ffmpeg decodes each body back to the right length.
- **Licenses.** `docker/LICENSES/` holds the shine-mp3 LGPL-2.0 text and a README. The README covers relinking under static Go linking, how ffmpeg's license depends on its build (Debian's is GPL because it's built with `--enable-gpl`, and it's only run as a subprocess), the Apache-2.0 models and the test-only decoder. Ticket 15 copies the directory into the images.
- **Checked end to end.** `serve` with mini on CPU returned `audio/mpeg` for a request with no `response_format`, and `audio/ogg`, `audio/aac` and `audio/flac` for the others. ffprobe and ffmpeg read each file as the expected codec at about 4 s, and Whisper (small.en) transcribed all four as "The quick brown fox jumps over the lazy dog."
- **Known quirk.** Flac piped out of ffmpeg can't seek back to write its total sample count, so ffprobe shows the duration as N/A. The file still decodes completely.
