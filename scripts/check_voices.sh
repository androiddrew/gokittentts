#!/usr/bin/env bash
cd ~/experiments/golang_onnx
make build
VOICES=${VOICES:-Bruno Jasper Bella Luna Hugo Rosie Leo Kiki}
ORT=$PWD/third_party/onnxruntime-linux-x64-1.29.1/lib/libonnxruntime.so.1.29.1
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
    bin/gokittentts say --onnxruntime-lib "$ORT" --voice "$v" --out "$f" "$TEXT" || continue
    echo "▶ $v"
    aplay -q "$f"
done
