"""Times the KittenTTS Python reference the way `gokittentts bench` times Go.

Run through `make bench-python-ref`. It reads the same corpus
(internal/bench/corpus.txt), synthesizes one untimed warm-up request, then
times each text over --runs passes through the reference's generate_stream with
text cleaning on: time to the first chunk, and RTF (synthesis seconds per
second of audio). It writes a JSON record with the same percentiles and sample
fields as a bench record, for comparing the two on one machine.

The reference has no markdown pass, so the corpus's markdown texts are read as
written. PyPI has no onnxruntime 1.29.1, so the Makefile uses 1.29.0.
onnx_model_ref.py and preprocess_ref.py are pinned by `make reference`.
"""
import argparse, hashlib, importlib.util, json, math, pathlib, platform, socket, sys, time, types
from datetime import datetime, timezone

import onnxruntime as ort

here = pathlib.Path(__file__).resolve().parent
repo = here.parent

# onnx_model_ref.py imports .preprocess, so load both as one package.
pkg = types.ModuleType("kittentts_ref")
pkg.__path__ = []
sys.modules["kittentts_ref"] = pkg
for name, path in (("preprocess", "preprocess_ref.py"), ("onnx_model", "onnx_model_ref.py")):
    spec = importlib.util.spec_from_file_location(f"kittentts_ref.{name}", here / path)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = mod
    spec.loader.exec_module(mod)
KittenTTS = sys.modules["kittentts_ref.onnx_model"].KittenTTS_1_Onnx

ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
ap.add_argument("model_dir", help="a model directory with config.json, e.g. models/kitten-tts-mini-0.8/current")
ap.add_argument("--runs", type=int, default=3)
ap.add_argument("--voice", default="Bruno")
ap.add_argument("--out", help="write the record here instead of stdout")
args = ap.parse_args()

model_dir = pathlib.Path(args.model_dir)
cfg = json.loads((model_dir / "config.json").read_text())
model = KittenTTS(str(model_dir / cfg["model_file"]), str(model_dir / cfg["voices"]),
                  speed_priors=cfg.get("speed_priors", {}), voice_aliases=cfg.get("voice_aliases", {}),
                  backend="cpu")

# Parsed as bench.Corpus does: one request per line, \n for a newline.
corpus = []
for line in (repo / "internal" / "bench" / "corpus.txt").read_text(encoding="utf-8").splitlines():
    line = line.strip()
    if line and not line.startswith("#"):
        corpus.append(line.replace("\\n", "\n"))


def measure(text):
    start = time.perf_counter()
    first, samples = None, 0
    for chunk in model.generate_stream(text, voice=args.voice, speed=1.0, clean_text=True):
        if first is None and len(chunk) > 0:
            first = time.perf_counter() - start
        samples += len(chunk)
    synthesis = time.perf_counter() - start
    audio = samples / 24000
    return {"audio_seconds": audio, "synthesis_seconds": synthesis,
            "first_audio_seconds": first, "rtf": synthesis / audio}


def stats(xs):
    xs = sorted(xs)
    rank = lambda p: xs[max(math.ceil(p * len(xs)) - 1, 0)]
    return {"p50": rank(0.50), "p95": rank(0.95)}


measure(corpus[0])
samples = []
for run in range(args.runs):
    for i, text in enumerate(corpus):
        samples.append({"run": run, "text_index": i, **measure(text)})

h = hashlib.sha256()
for text in corpus:
    h.update(f"{len(text.encode())}:{text}".encode())
record = {
    "implementation": "kittentts python reference",
    "date": datetime.now(timezone.utc).isoformat(),
    "host": socket.gethostname(),
    "machine": platform.machine(),
    "onnxruntime_version": ort.__version__,
    "model": cfg["model"],
    "device": "cpu",
    "voice": args.voice,
    "corpus_texts": len(corpus),
    "corpus_sha256": h.hexdigest(),
    "runs": args.runs,
    "rtf": stats([s["rtf"] for s in samples]),
    "first_audio_seconds": stats([s["first_audio_seconds"] for s in samples]),
    "samples": samples,
}
out = open(args.out, "w") if args.out else sys.stdout
json.dump(record, out, indent=2)
out.write("\n")
r = record
print(f"{r['model']} (python reference, onnxruntime {r['onnxruntime_version']}): "
      f"RTF p50 {r['rtf']['p50']:.3f} p95 {r['rtf']['p95']:.3f}, "
      f"first audio p50 {r['first_audio_seconds']['p50']:.3f}s p95 {r['first_audio_seconds']['p95']:.3f}s",
      file=sys.stderr)
