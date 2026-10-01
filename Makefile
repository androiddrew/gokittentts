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

# Models for the native tests, pinned by revision and SHA-256 from
# docs/ORIGINAL_SPEC.md section 3.1. The model store (ticket 09) replaces this.
MODELS_DIR := models
MODELS := kitten-tts-mini-0.8 kitten-tts-micro-0.8 kitten-tts-nano-0.8-int8 kitten-tts-nano-0.8-fp32
REV_kitten-tts-mini-0.8 := c02725660cea441db4c383af69f1f26f5cd00947
SHA_kitten-tts-mini-0.8 := \
	6b160bc9b19e24ecb21e84bc14f8a7da21fdf47ec72d42450bc5cf514b61804a config.json \
	0f5bbae4fc4800c98dbc544a87ecfa79510de2fb8222db30d12e5bfe9177df91 kitten_tts_mini_v0_8.onnx \
	40ad2638952b77b7b2f30127e2608e169fc69dd256b53bd8aaa3409a33193c42 voices.npz
REV_kitten-tts-micro-0.8 := 1ccf72b2c2048fd17efac7de2fab32d10e225084
SHA_kitten-tts-micro-0.8 := \
	1f0bd2208348f9211cb0da64fcd1536eb28228571cc6b09e767eb6e203a0a532 config.json \
	95481626fee1ba70ce683e69c534fc7cb38433c46ce42d3abbeafb4b9f1a4123 kitten_tts_micro_v0_8.onnx \
	112710c1be8ad0e967c190fb0fd95cbe5848ec4791b93209f20b28b7da20dac1 voices.npz
REV_kitten-tts-nano-0.8-int8 := 84781d74e29ee25217551556398b42f80593a813
SHA_kitten-tts-nano-0.8-int8 := \
	b66006ccbeccd4de5fc3c9272059c47f5725df7215fd889785c03602652fab64 config.json \
	f7b0afcbee92870b32b8e0276d855b954dc25470c9f051b376ac7eee537c76fc kitten_tts_nano_v0_8.onnx \
	8aa7cee235abb0739cb51e6559685f65a4dacd95568833d05699b1633f519b3f voices.npz
REV_kitten-tts-nano-0.8-fp32 := 7a1db645b1f3ab9420761d87428e042b9cec3f26
SHA_kitten-tts-nano-0.8-fp32 := \
	b66006ccbeccd4de5fc3c9272059c47f5725df7215fd889785c03602652fab64 config.json \
	320564d2615f235de972ca27a7f39551c94185cfa24ca85b07a29084135f1e5e kitten_tts_nano_v0_8.onnx \
	8aa7cee235abb0739cb51e6559685f65a4dacd95568833d05699b1633f519b3f voices.npz

.PHONY: build test test-native golden golden-test reference

build:
	CGO_ENABLED=1 go build -tags espeak -o bin/gokittentts ./cmd/gokittentts

# Runs anywhere: no ONNX Runtime, espeak-ng or models needed. The native tag
# also builds the cgo espeak-ng backend.
test:
	go test ./...

# Everything, including tests that need ONNX Runtime, espeak-ng and models.
test-native: $(ORT_DIR)/VERSION_NUMBER $(ORT_OTHER_DIR)/VERSION_NUMBER $(MODELS:%=$(MODELS_DIR)/%/voices.npz)
	KITTEN_ORT_LIB=$(CURDIR)/$(ORT_LIB) \
	KITTEN_ORT_LIB_OTHER=$(CURDIR)/$(ORT_OTHER_DIR)/lib/libonnxruntime.so.$(ORT_OTHER_VERSION) \
	KITTEN_MODELS_DIR=$(CURDIR)/$(MODELS_DIR) \
		go test -count=1 -tags native ./...

third_party/onnxruntime-linux-$(ORT_ARCH)-%/VERSION_NUMBER:
	mkdir -p third_party
	curl -sfL https://github.com/microsoft/onnxruntime/releases/download/v$*/onnxruntime-linux-$(ORT_ARCH)-$*.tgz \
		| tar xz -C third_party

# Fetches a model's files at its pinned revision and checks their SHA-256s.
$(MODELS_DIR)/%/voices.npz:
	mkdir -p $(@D)
	for f in $(filter %.json %.onnx %.npz,$(SHA_$*)); do \
		curl -sfL -o $(@D)/$$f https://huggingface.co/KittenML/$*/resolve/$(REV_$*)/$$f || exit 1; \
	done
	printf '%s  %s\n' $(SHA_$*) | (cd $(@D) && sha256sum -c --quiet) || { rm -f $(@D)/*; exit 1; }

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
