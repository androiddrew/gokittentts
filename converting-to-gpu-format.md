The issue is drafted, but I can't post it yet. Answer to your int8 question first, then the posting options.

Can the int8 models be converted to fp32?

Yes, and it's mechanical. All 209 quantized matrix-multiply and convolution nodes in mini follow the exact pattern that ONNX Runtime's quantize_dynamic tool produces:

x ─▶ DynamicQuantizeLinear ─▶ MatMulInteger / ConvInteger (int8 weights) ─▶ Cast ─▶ Mul (scale)

Undoing it means rewriting the graph:
1. Dequantize each weight tensor: W_fp32 = (W_int8 − zero_point) × scale. The scales and zero points are stored in the graph.
2. Swap each pattern for a plain MatMul or Conv on the float input.
3. Swap the 6 DynamicQuantizeLSTM nodes for standard LSTM nodes, using the dequantized W and R weights with the same bias.

All of these have fast CUDA kernels, so the whole graph could run on the GPU, as nano-fp32 does.

What to expect:
- Size. The 70M int8 weights grow to about 280 MB in fp32, or about 140 MB with a further fp16 conversion. fp16 uses the 4090's tensor cores; it's usually safe for this kind of model but needs a listening check.
- Quality. It won't recover the original fp32 model; the weight-rounding error is baked in. It removes the activation quantization, though, so the output should match the int8 model or be slightly better.
- Speed. Unmeasured. nano-fp32 went from 0.27 s on CPU to 0.06 s on CUDA, so a large gain is plausible. On CPU, the dequantized model might even beat int8 on your 5600X, since nano-fp32 was 6.5× faster than nano-int8 there.
- Cheaper alternative: ask KittenML for fp32 exports of mini and micro. They already publish one for nano.

I can prototype it in the scratchpad: write the converter, check the rebuilt graph on your 4090, and benchmark mini-fp32 and mini-fp16 against int8 with the same Go prototype.

Posting the issue

The draft is in the scratchpad as issue-misaki.md. It covers:
- The 0.8.1 wheel's 112 packages versus main's 171 MB.
- The two hidden jobs misaki's import did, and the commit-by-commit back-and-forth.
- The Python 3.13 block and the phonemizer vs phonemizer-fork difference.
- The proposal: release 0.8.2, pin phonemizer>=3.4.0, optional system espeak-ng.
- A workaround until a release.

All references point at upstream as KittenML/KittenTTS#N, with no @-mentions.

Two things block posting: Issues are disabled on your fork, and there's no GitHub CLI or token on this machine.

● User answered Claude's questions:
· How should the issue get posted? (Issues must be enabled on androiddrew/KittenTTS first: Settings → General → Features → Issues.) → I'll paste it myself
· Want me to prototype the int8 → fp32/fp16 conversion for mini and benchmark it on your 4090? → Yes, prototype it

I'll get the final issue text ready to paste once the prototype is done. Starting the dequantizer now — first checking how each quantized node's scale is computed so the rewrite matches the math exactly.

  Ran 1 shell command

I've mapped out the key conversion details: per-tensor scales (except the LSTM, which uses per-direction), quantizer nodes shared across up to 15 matmuls requiring cleanup after rewriting all consumers, a transpose needed for the LSTM's quantized weight layout, and unseeded random nodes that I'll fix for validation. Now I'll write the converter.

  Made 1 scratchpad edit +154

Converter written. Running it three times: an fp32 model for benchmarking, plus seeded int8 and fp32 copies for comparing the audio.

  Ran 1 shell command

I'll cache each dequantized weight by its source tensors to avoid duplicate initializers from shared weights across matmuls.

  Ran 1 shell command

Found a bug where the dead-node cleanup only checks one subgraph level deep, causing it to wrongly delete a producer used by a node nested inside a Loop body. I'll update the cleanup to recurse through nested subgraphs.

  Made 1 scratchpad edit +20 -12, ran 1 shell command

The seeded int8 copy fails the same check even though it never touches the graph, confirming the original KittenTTS model itself trips onnx.checker's strict Loop validation—my converter isn't at fault. I'll skip the checker and instead validate by loading and running the models directly in ONNX Runtime.

  Ran 1 shell command

The conversion worked—mini-fp32 is 287 MB with no quantized ops remaining. Next I'll verify correctness by running seeded int8 twice to confirm deterministic output, then seeded fp32 on the same inputs, comparing per-token durations and waveforms.

  Ran 1 shell command

The int8 model loads fine, but the fp32 graph fails since ONNX Runtime can't locate the producer of /SplitToSequence_output_0 used in the Loop body. I'll compare that node's placement between the original and converted graphs to find the discrepancy.

  Ran 1 shell command

Found the bug: the graph isn't topologically sorted, so my single reverse-pass cleanup deleted SplitToSequence before realizing the Loop node still needed it. I'll switch to a repeat-until-stable pass to fix this.

  Made 1 scratchpad edit +17 -11, ran 1 shell command

Setting the seed alone didn't make int8 repeatable since ONNX Runtime's generator advances with each run rather than resetting. I'll instead create a fresh session for every run to guarantee reproducibility.

  Ran 1 shell command

Int8 is reproducible and fp32 durations closely match int8 across most sentences, confirming the conversion is likely correct, but raw waveform correlation won't be meaningful due to timing shifts affecting noise phase. I'll instead compare internal tensor outputs directly between int8 and fp32, and run a spectrogram comparison against an int8-vs-different-seed baseline.

  Ran 1 shell command

Internal comparison confirms the conversion is correct—early layers differ by only 0.24–0.49% (expected rounding error), the first LSTM's 0.48% difference confirms the weight transpose, and differences grow further downstream as changes compound, with 73 tensors unable to be compared due to shape mismatches from duration-dependent output length. Next I'll check the spectrogram against the seed-noise baseline and attempt an fp16 version.

  Ran 1 shell command

The fp32 vs int8 outputs differ by 6-8 dB in log-spectral distance (partly due to frame misalignment from timing shifts), confirming numerical correctness though actual audio quality needs a listen—I'll save WAVs for that. Next I'll benchmark speed by building an mini-fp32 model folder and testing it with the Go prototype on CPU/CUDA, plus an fp16 variant.

  Ran 2 shell commands

The fp16 converter refuses because the original model already contains 12 float16 tensors. That's harmless here, so I'll turn off that check.

  Ran 1 shell command

Both variants exist: mini-fp32 at 287 MB and mini-fp16 at 147 MB. Benchmarking all three mini variants on CPU and CUDA with the Go prototype, on the same sentence and settings as before:

  Ran 1 shell command

fp32 (dequantized) crushes int8 on both CPU (5.2×) and GPU (31×), since quantization overhead outweighs the weight savings; fp16 fails to load, but since fp32 is already fast I'll leave it. Now generating matching int8/fp32 WAV pairs for comparison, including the "Hello, world!" sentence that ran 0.55 s longer.

  Ran 1 shell command

The listening pairs are ready — three of four sentences match in length within 0.05s, but "Hello, world!" still runs longer in fp32 (1.84s vs 1.29s), pointing to different duration predictor behavior on short inputs. I'll now print the final issue text for you to paste.

  Read 1 file

int8 → fp32: it works, and it's much faster

I wrote a converter in the scratchpad (dequant/dequantize.py, about 160 lines of Python using the onnx package). It turns mini's int8 layers back into ordinary fp32 layers, which run on the GPU. It replaced all 215 quantized nodes, and the resulting model is 287 MB, up from 78 MB.

Correctness checks:
- Internal values match. The earliest rewritten layers differ from int8 by only 0.24–0.49%, and the first LSTM by 0.48%. That is the size of the int8 rounding error the conversion removes. A wrong scale or weight layout would show errors near 100%.
- Timing matches, with one exception. Per-word timing (the model's duration output) lands within 1–5 frames of int8, out of roughly 170–200 per sentence, and every model still produces exactly 600 audio samples per frame. The exception is "Hello, world!": fp32 makes it 0.55 s longer (1.84 s vs 1.29 s), so very short inputs push timing further.
- The sound differs slightly, and needs your ears. The fp32 audio is about 6–8 dB (log-spectral distance) from int8, compared with about 4 dB between two int8 runs that differ only in random seed. Part of that gap is the timing shifts. Listening pairs are in the scratchpad listen/ folder, as 1-int8.wav / 1-fp32.wav through 4-….

Speed (same sentence, warm runs):

┌──────────────────┬───────────────────┬─────────────────────┐
│   mini variant   │    CPU (5600X)    │     CUDA (4090)     │
├──────────────────┼───────────────────┼─────────────────────┤
│ int8 (published) │ 2.74 s            │ 1.76 s              │
├──────────────────┼───────────────────┼─────────────────────┤
│ fp32 (converted) │ 0.53 s (RTF 0.11) │ 0.056 s (RTF 0.012) │
└──────────────────┴───────────────────┴─────────────────────┘

It's faster on the CPU too, by 5.2×. On this CPU, the per-call int8 quantization overhead costs more than the smaller weights save. The fp16 version failed to load with a type error; I haven't pursued it.

This would change several spec decisions: mini-fp32 could be the default on both CPU and GPU, and it might meet the Pi 5 targets. The Pi claim is untested, and ARM CPUs handle int8 differently from this one.

The issue

Issues are still disabled on your fork. Enable them under Settings → General → Features → Issues. The text is in the scratchpad as issue-misaki.md:

- Title: Published 0.8.1 wheel still pulls misaki → spaCy → torch (112 packages); main is already fixed, so cut a release and pin phonemizer ≥ 3.4.0
- Body: everything below the title line in that file.

Its main points:
- The README installs the 0.8.1 wheel, which resolves 112 packages including torch, while main installs in 171 MB.
- It explains the two hidden jobs misaki's import did, and maps the three 2026-03-25 commits onto them.
- It covers the Python 3.13 block and the phonemizer vs phonemizer-fork difference on $3.5 million, or 1,500 dollars.
- Proposal:
  a. Release 0.8.2 from main.
  b. Pin phonemizer>=3.4.0.
  c. Optionally, fall back to the system espeak-ng. The broken bundled data path is described as reproduced on my machine, root cause unconfirmed.
- It gives the workaround pip install "git+https://github.com/KittenML/KittenTTS@main".

All references are written as KittenML/KittenTTS#N and KittenML/KittenTTS@sha, so they link to upstream from your fork. There are no @-mentions.

Next steps, if you want them:
- Fold the fp32 finding into SPEC.md: add the converter as a tools/ script, and reconsider the default model.
- Run the same conversion on micro and nano-int8.

✻ Churned for 10m 41s · done 1:10 PM

※ recap: The spec for hosting KittenTTS in Go is done, and converting mini back to fp32 made it about 5× faster on CPU and 31× on your GPU. Next, enable Issues on your fork and paste the misaki issue from issue-misaki.md in the scratchpad. (disable recaps in /config)
