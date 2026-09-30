# KittenTTS main at setup.py version 0.8.1. The 0.8.1 git tag predates
# preprocess.chunk_text and normalize_text, so the reference is pinned by commit.
KITTENTTS_REF := be5758500b731b8fc674acc62ea480d3022b7ebe
KITTENTTS_RAW := https://raw.githubusercontent.com/KittenML/KittenTTS/$(KITTENTTS_REF)/kittentts
GOLDEN_PY := uv run --no-project --with "phonemizer>=3.4.0"

.PHONY: golden golden-test reference

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
