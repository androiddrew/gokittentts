# KittenTTS main at setup.py version 0.8.1. The 0.8.1 git tag predates
# preprocess.chunk_text and normalize_text, so the reference is pinned by commit.
KITTENTTS_REF := be5758500b731b8fc674acc62ea480d3022b7ebe
KITTENTTS_RAW := https://raw.githubusercontent.com/KittenML/KittenTTS/$(KITTENTTS_REF)/kittentts
GOLDEN_PY := uv run --no-project --with "phonemizer>=3.4.0"

ORT_VERSION := 1.29.1
HOST_ARCH := $(if $(filter aarch64 arm64,$(shell uname -m)),arm64,amd64)
ORT_ARCH := $(if $(filter arm64,$(HOST_ARCH)),aarch64,x64)
ORT_DIR := third_party/onnxruntime-linux-$(ORT_ARCH)-$(ORT_VERSION)
ORT_LIB := $(ORT_DIR)/lib/libonnxruntime.so.$(ORT_VERSION)
# A newer runtime that onnxruntime_go can load, for the version-check test.
ORT_OTHER_VERSION := 1.30.0
ORT_OTHER_DIR := third_party/onnxruntime-linux-$(ORT_ARCH)-$(ORT_OTHER_VERSION)
# The CUDA build (x64 only) for device: cuda, against CUDA 12 or 13. It also
# needs the CUDA and cuDNN 9 libraries on the loader path.
ORT_CUDA ?= 12
ORT_GPU_DIR := third_party/onnxruntime-linux-x64-gpu_cuda$(ORT_CUDA)-$(ORT_VERSION)
ORT_GPU_LIB := $(ORT_GPU_DIR)/lib/libonnxruntime.so.$(ORT_VERSION)

# Models for the native tests, fetched by `gokittentts pull` into the model
# store layout, <name>/<revision>/ with a current symlink.
MODELS_DIR := models
MODELS := kitten-tts-mini-0.8 kitten-tts-micro-0.8 kitten-tts-nano-0.8-int8 kitten-tts-nano-0.8-fp32

.PHONY: build test test-native test-cuda onnxruntime-gpu image-cpu image-cuda12 image-cuda13 golden golden-test reference emoji-table

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

# Runs nano-fp32 on GPU 0 with the CUDA build of ONNX Runtime.
test-cuda: $(ORT_GPU_DIR)/VERSION_NUMBER $(MODELS_DIR)/kitten-tts-nano-0.8-fp32/current/config.json
	KITTEN_ORT_LIB=$(CURDIR)/$(ORT_LIB) \
	KITTEN_ORT_LIB_CUDA=$(CURDIR)/$(ORT_GPU_LIB) \
	KITTEN_MODELS_DIR=$(CURDIR)/$(MODELS_DIR) \
		go test -count=1 -tags native -run '^TestCUDA$$' -v ./kittentts

onnxruntime-gpu: $(ORT_GPU_DIR)/VERSION_NUMBER

$(ORT_GPU_DIR)/VERSION_NUMBER:
	mkdir -p third_party
	curl -sfL https://github.com/microsoft/onnxruntime/releases/download/v$(ORT_VERSION)/onnxruntime-linux-x64-gpu_cuda$(ORT_CUDA)-$(ORT_VERSION).tgz \
		| tar xz -C third_party

third_party/onnxruntime-linux-$(ORT_ARCH)-%/VERSION_NUMBER:
	mkdir -p third_party
	curl -sfL https://github.com/microsoft/onnxruntime/releases/download/v$*/onnxruntime-linux-$(ORT_ARCH)-$*.tgz \
		| tar xz -C third_party

# Pulls a model at its pinned revision, checking its SHA-256s.
$(MODELS_DIR)/%/current/config.json:
	go run ./cmd/gokittentts pull --dir $(MODELS_DIR) $*

# Docker images. Each platform is built and loaded as <IMAGE>:cpu-<arch>,
# since the classic image store can't hold a multi-platform image, and the
# host's platform is also tagged <IMAGE>:cpu. Other platforms run under QEMU.
# BAKE_MODELS is comma-separated; `BAKE_MODELS=` builds a slim image.
IMAGE ?= gokittentts
BAKE_MODELS ?= kitten-tts-mini-0.8
IMAGE_PLATFORMS ?= linux/amd64 linux/arm64
VCS_REF := $(shell git rev-parse HEAD)$(shell git diff --quiet HEAD || echo -dirty)

image-cpu:
	set -e; for p in $(IMAGE_PLATFORMS); do \
		docker buildx build --platform $$p -f docker/Dockerfile.cpu \
			--build-arg BAKE_MODELS=$(BAKE_MODELS) --build-arg VCS_REF=$(VCS_REF) \
			--load -t $(IMAGE):cpu-$${p#linux/} . ; \
	done
	$(if $(filter linux/$(HOST_ARCH),$(IMAGE_PLATFORMS)),docker tag $(IMAGE):cpu-$(HOST_ARCH) $(IMAGE):cpu)

# The CUDA images are amd64 only, and bake and default to nano-fp32.
image-cuda12 image-cuda13: BAKE_MODELS = kitten-tts-nano-0.8-fp32
image-cuda12 image-cuda13: image-cuda%:
	docker buildx build --platform linux/amd64 -f docker/Dockerfile.cuda \
		--build-arg CUDA=$* --build-arg BAKE_MODELS=$(BAKE_MODELS) --build-arg VCS_REF=$(VCS_REF) \
		--load -t $(IMAGE):cuda$* .

# Regenerate testdata/golden.json and testdata/normalize_golden.json from the
# Python reference.
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
