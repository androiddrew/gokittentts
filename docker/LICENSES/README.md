# Third-party licenses

The images copy this directory to `/usr/share/doc/gokittentts/LICENSES`, and add ONNX Runtime's `LICENSE` and `ThirdPartyNotices.txt` under `onnxruntime/`, plus links to the Debian copyright files of ffmpeg and espeak-ng. Every Debian package in an image keeps its copyright file at `/usr/share/doc/<package>/copyright`.

## shine-mp3

- **What:** [`github.com/braheezy/shine-mp3`](https://github.com/braheezy/shine-mp3) v0.2.0 (`pkg/mp3`), a pure Go port of the shine fixed-point mp3 encoder. It is compiled into the `gokittentts` binary and encodes `mp3`, the default `response_format`.
- **License:** GNU Library General Public License, version 2 ([`shine-mp3-LGPL-2.0.txt`](shine-mp3-LGPL-2.0.txt)).
- **Obligations:** Go links statically. To meet the LGPL's relinking terms, every image ships a binary built from the public gokittentts source at a recorded commit, and anyone can rebuild it with a modified shine-mp3 (a `replace` directive in `go.mod` followed by `make build`). The shine-mp3 source is at the URL above.

## ffmpeg

- **What:** the distribution's `ffmpeg` package, run as a separate process (never linked) to encode `opus` (libopus in Ogg), `aac` (ADTS) and `flac`. Without it, or with `ffmpeg: ""` in the config, those formats return 400.
- **License:** it depends on the build. Debian's `ffmpeg` is built with `--enable-gpl`, which makes it GPL-2.0-or-later as a whole. Its individual libraries (libavcodec and so on) are LGPL-2.1-or-later, and libopus is BSD-3-Clause. The package's own copyright file, `/usr/share/doc/ffmpeg/copyright`, is authoritative, and the images keep it.
- **Obligations:** gokittentts only calls the binary, so the GPL doesn't reach gokittentts. Anyone distributing an image redistributes ffmpeg and has to offer its source, which Debian publishes for every package version.

## espeak-ng

- **What:** Debian's `libespeak-ng1` and `espeak-ng-data`, at the version the Dockerfiles pin. The binary links the library through cgo and phonemizes with it.
- **License:** GPL-3.0-or-later. The package's copyright file (`espeak-ng-copyright` here) is authoritative.
- **Obligations:** Anyone distributing an image redistributes espeak-ng and has to offer its source, which Debian publishes for every package version. The gokittentts binary links it, so a distributed binary is a combined work under the GPL's terms.

## ONNX Runtime

- **What:** Microsoft's prebuilt `libonnxruntime.so` from the GitHub release, loaded at run time.
- **License:** MIT ([`LICENSE`](https://github.com/microsoft/onnxruntime/blob/v1.29.1/LICENSE)), with its bundled components listed in [`ThirdPartyNotices.txt`](https://github.com/microsoft/onnxruntime/blob/v1.29.1/ThirdPartyNotices.txt).
- **Obligations:** keep both notices with the library. The images copy them from the release archive into `onnxruntime/`.

## KittenTTS models

The KittenTTS 0.8 models are Apache-2.0 ([`Apache-2.0.txt`](Apache-2.0.txt)). They are downloaded from Hugging Face or baked into the image.

## Test-only

[`github.com/hajimehoshi/go-mp3`](https://github.com/hajimehoshi/go-mp3) (Apache-2.0) decodes mp3 in tests. It isn't in the binary.
