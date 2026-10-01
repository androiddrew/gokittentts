# KittenTTS main at setup.py version 0.8.1. The 0.8.1 git tag predates
# preprocess.chunk_text and normalize_text, so the reference is pinned by commit.
KITTENTTS_REF := be5758500b731b8fc674acc62ea480d3022b7ebe
KITTENTTS_RAW := https://raw.githubusercontent.com/KittenML/KittenTTS/$(KITTENTTS_REF)/kittentts
GOLDEN_PY := uv run --no-project --with "phonemizer>=3.4.0"

ORT_VERSION := 1.29.1
ORT_ARCH := $(if $(filter aarch64 arm64,$(shell uname -m)),aarch64,x64)
ORT_DIR := third_party/onnxruntime-linux-$(ORT_ARCH)-$(ORT_VERSION)
ORT_LIB := $(ORT_DIR)/lib/libonnxruntime.so.$(ORT_VERSION)
# A newer runtime that onnxruntime_go can load, for the version-check test.
ORT_OTHER_VERSION := 1.30.0
ORT_OTHER_DIR := third_party/onnxruntime-linux-$(ORT_ARCH)-$(ORT_OTHER_VERSION)

# Models for the native tests, fetched by `gokittentts pull` into the model
# store layout, <name>/<revision>/ with a current symlink.
MODELS_DIR := models
MODELS := kitten-tts-mini-0.8 kitten-tts-micro-0.8 kitten-tts-nano-0.8-int8 kitten-tts-nano-0.8-fp32

.PHONY: build test test-native golden golden-test reference emoji-table

build:
	CGO_ENABLED=1 go build -tags espeak -o bin/gokittentts ./cmd/gokittentts

# Runs anywhere: no ONNX Runtime, espeak-ng or models needed. The native tag
# also builds the cgo espeak-ng backend.
test:
	go test ./...

# Everything, including tests that need ONNX Runtime, espeak-ng and models.
test-native: $(ORT_DIR)/VERSION_NUMBER $(ORT_OTHER_DIR)/VERSION_NUMBER $(MODELS:%=$(MODELS_DIR)/%/current/config.json)
	KITTEN_ORT_LIB=$(CURDIR)/$(ORT_LIB) \
	KITTEN_ORT_LIB_OTHER=$(CURDIR)/$(ORT_OTHER_DIR)/lib/libonnxruntime.so.$(ORT_OTHER_VERSION) \
	KITTEN_MODELS_DIR=$(CURDIR)/$(MODELS_DIR) \
		go test -count=1 -tags native ./...

third_party/onnxruntime-linux-$(ORT_ARCH)-%/VERSION_NUMBER:
	mkdir -p third_party
	curl -sfL https://github.com/microsoft/onnxruntime/releases/download/v$*/onnxruntime-linux-$(ORT_ARCH)-$*.tgz \
		| tar xz -C third_party

# Pulls a model at its pinned revision, checking its SHA-256s.
$(MODELS_DIR)/%/current/config.json:
	go run ./cmd/gokittentts pull --dir $(MODELS_DIR) $*

# Regenerate testdata/golden.json from the Python reference.
golden:
	$(GOLDEN_PY) python3 scripts/make_golden.py

# End-to-end tests of the golden generator (needs uv, network, libespeak-ng).
golden-test:
	uv run --no-project --with pytest python3 -m pytest scripts/test_make_golden.py -q

# Re-vendor the reference sources at KITTENTTS_REF.
reference:
	curl -sfL -o scripts/onnx_model_ref.py $(KITTENTTS_RAW)/onnx_model.py
	curl -sfL -o scripts/preprocess_ref.py $(KITTENTTS_RAW)/preprocess.py

# Regenerate the markdown pass's Extended_Pictographic table from Unicode's
# emoji-data.txt (the version is pinned in the script).
emoji-table:
	python3 scripts/gen_emoji_table.py
	gofmt -w internal/markdown/emoji_table.go
