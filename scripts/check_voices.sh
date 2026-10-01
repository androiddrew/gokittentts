#!/usr/bin/env bash
cd ~/experiments/golang_onnx
make build
ORT=$PWD/third_party/onnxruntime-linux-x64-1.29.1/lib/libonnxruntime.so.1.29.1
TEXT='The 2024 budget passed. Meet me at 3:00 today, on March 3rd, 2025. It is our 20th year.'
OUT=/tmp/kitten-voices; mkdir -p "$OUT"

for v in Jasper Bella Bruno Luna Hugo Rosie Leo Kiki; do
    f="$OUT/$v-normalize-$n.wav"
    bin/gokittentts say --onnxruntime-lib "$ORT" --voice "$v" --normalize=true --out "$f" "$TEXT" || continue
    echo "▶ $v (normalize=$n)"
    aplay -q "$f"
done
