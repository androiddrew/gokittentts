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

# Models for the native tests. The model store (ticket 09) replaces this.
MODELS_DIR := models
MINI_REV := c02725660cea441db4c383af69f1f26f5cd00947
MINI_DIR := $(MODELS_DIR)/kitten-tts-mini-0.8

.PHONY: build test test-native golden golden-test reference

build:
	CGO_ENABLED=1 go build -tags espeak -o bin/gokittentts ./cmd/gokittentts

# Runs anywhere: no ONNX Runtime, espeak-ng or models needed. The native tag
# also builds the cgo espeak-ng backend.
test:
	go test ./...

# Everything, including tests that need ONNX Runtime, espeak-ng and models.
test-native: $(ORT_DIR)/VERSION_NUMBER $(ORT_OTHER_DIR)/VERSION_NUMBER $(MINI_DIR)/voices.npz
	KITTEN_ORT_LIB=$(CURDIR)/$(ORT_LIB) \
	KITTEN_ORT_LIB_OTHER=$(CURDIR)/$(ORT_OTHER_DIR)/lib/libonnxruntime.so.$(ORT_OTHER_VERSION) \
	KITTEN_MODELS_DIR=$(CURDIR)/$(MODELS_DIR) \
		go test -tags native ./...

third_party/onnxruntime-linux-$(ORT_ARCH)-%/VERSION_NUMBER:
	mkdir -p third_party
	curl -sfL https://github.com/microsoft/onnxruntime/releases/download/v$*/onnxruntime-linux-$(ORT_ARCH)-$*.tgz \
		| tar xz -C third_party

# Pinned revision and SHA-256s from docs/ORIGINAL_SPEC.md section 3.1.
$(MINI_DIR)/voices.npz:
	mkdir -p $(MINI_DIR)
	for f in config.json kitten_tts_mini_v0_8.onnx voices.npz; do \
		curl -sfL -o $(MINI_DIR)/$$f https://huggingface.co/KittenML/kitten-tts-mini-0.8/resolve/$(MINI_REV)/$$f || exit 1; \
	done
	printf '%s  %s\n' \
		6b160bc9b19e24ecb21e84bc14f8a7da21fdf47ec72d42450bc5cf514b61804a config.json \
		0f5bbae4fc4800c98dbc544a87ecfa79510de2fb8222db30d12e5bfe9177df91 kitten_tts_mini_v0_8.onnx \
		40ad2638952b77b7b2f30127e2608e169fc69dd256b53bd8aaa3409a33193c42 voices.npz \
		| (cd $(MINI_DIR) && sha256sum -c --quiet) || { rm -f $(MINI_DIR)/*; exit 1; }

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
