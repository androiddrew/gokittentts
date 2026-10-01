#!/usr/bin/env bash
# CUDA=1 runs on GPU 0 with the CUDA build of ONNX Runtime (ORT_CUDA=12 or
# 13, default 12), which needs the CUDA and cuDNN 9 libraries on
# LD_LIBRARY_PATH. Otherwise it runs on the CPU.
cd ~/experiments/golang_onnx
make build
VOICES=${VOICES:-Bruno Jasper Bella Luna Hugo Rosie Leo Kiki}
CUDA=${CUDA:-0}
# ORT_CUDA=13 CUDA=1 VOICES="Bruno" scripts/check_voices.sh
if [ "$CUDA" = 1 ]; then
    ORT_CUDA=${ORT_CUDA:-12}
    make onnxruntime-gpu ORT_CUDA="$ORT_CUDA" || exit 1
    ORT=$PWD/third_party/onnxruntime-linux-x64-gpu_cuda$ORT_CUDA-1.29.1/lib/libonnxruntime.so.1.29.1
    DEVICE=cuda
else
    ORT=$PWD/third_party/onnxruntime-linux-x64-1.29.1/lib/libonnxruntime.so.1.29.1
    DEVICE=cpu
fi
# One sentence per normalizer feature: years, times, dates, ordinals, money,
# percents, units, scale suffixes, versions, titles, URLs and emails.
TEXT='The 2024 budget passed. Meet me at 3:00 today, on March 3rd, 2025. It is our 20th year.
We raised $3.5 million, up 15% from $2,500.
The 7B model needs 16 GB of memory and runs at 3.5 GHz.
GPT-4 replaced gpt-3.5, and Python 3.12.1 is out.
Dr. Smith et al. read pp. 31-35 at 5 p.m.
Visit https://example.com/docs or email hello@example.com.'
OUT=/tmp/kitten-voices; mkdir -p "$OUT"

for v in $VOICES; do
    f="$OUT/$v.wav"
    bin/gokittentts say --onnxruntime-lib "$ORT" --device "$DEVICE" --voice "$v" --out "$f" "$TEXT" || continue
    echo "▶ $v ($DEVICE)"
    aplay -q "$f"
done
