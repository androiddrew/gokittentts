"""Golden phoneme and token-id vectors from the KittenTTS Python reference.

Run through `make golden`, which writes testdata/golden.json and, from
testdata/normalize_corpus.txt, testdata/normalize_golden.json: Python's
normalize_text output for each corpus input. An optional argument writes
golden.json somewhere else, with normalize_golden.json beside it. See
docs/ORIGINAL_SPEC.md Appendix A.

onnx_model_ref.py and preprocess_ref.py are KittenTTS's kittentts/onnx_model.py
and kittentts/preprocess.py (Apache-2.0), pinned by `make reference`.
"""
import json, re, sys
import importlib.metadata as md
import importlib.util
import pathlib

# The reference is upstream phonemizer >= 3.4.0; phonemizer-fork splits decimals ("3.5" -> "three . five").
try:
    md.version("phonemizer-fork")
    raise SystemExit("phonemizer-fork is installed; use upstream phonemizer>=3.4.0 for goldens")
except md.PackageNotFoundError:
    pass
_v = tuple(int(x) for x in md.version("phonemizer").split(".")[:2])
if _v < (3, 4):
    raise SystemExit(f"phonemizer {md.version('phonemizer')} < 3.4.0 lacks the decimal-separator rule")

from phonemizer.backend.espeak.wrapper import EspeakWrapper
EspeakWrapper.set_library("/usr/lib/x86_64-linux-gnu/libespeak-ng.so.1")
import phonemizer
from phonemizer.punctuation import Punctuation, _MarkIndex


# Recorded deviation from upstream phonemizer 3.4.0: its _preserve_line finds
# marks with the decimal-aware regex but cuts the line with str.split(mark),
# which splits at the first "." anywhere, including a decimal point. Any chunk
# with a decimal and a later "." mark loses the words after the decimal point
# ("Version 3.5 shipped on time." -> "vˈɜːʒən θɹˈiː."). Cut at the regex match
# spans instead, which is what the decimal rule intends and what the Go
# punctuation scanner does.
def _preserve_line(self, line, num):
    matches = list(re.finditer(self._marks_re, line))
    if not matches:
        return [line], []
    if len(matches) == 1 and matches[0].group() == line:
        return [], [_MarkIndex(num, line, "A")]
    marks, parts, cursor = [], [], 0
    for match in matches:
        position = "I"
        if match is matches[0] and match.start() == 0:
            position = "B"
        elif match is matches[-1] and match.end() == len(line):
            position = "E"
        marks.append(_MarkIndex(num, match.group(), position))
        parts.append(line[cursor:match.start()])
        cursor = match.end()
    return parts + [line[cursor:]], marks


Punctuation._preserve_line = _preserve_line

here = pathlib.Path(__file__).resolve().parent
repo = here.parent
out_path = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else repo / "testdata" / "golden.json"

src = (here / "onnx_model_ref.py").read_text()
ns = {}
exec(src[src.index("class TextCleaner"):src.index("class KittenTTS_1_Onnx")], ns)
tc = ns["TextCleaner"]()
spec = importlib.util.spec_from_file_location("pp", here / "preprocess_ref.py")
pp = importlib.util.module_from_spec(spec); spec.loader.exec_module(pp)

b = phonemizer.backend.EspeakBackend(language="en-us", preserve_punctuation=True, with_stress=True)
out = []
for line in open(repo / "testdata" / "sentences.txt", encoding="utf-8"):
    for chunk in pp.chunk_text(line.strip()):
        ph = b.phonemize([chunk])[0]
        toks = " ".join(re.findall(r"\w+|[^\w\s]", ph))
        out.append({"chunk": chunk, "phonemes": ph,
                    "ids": [0] + tc(toks) + [10, 0],
                    "ref_id": min(len(chunk), 399),
                    "espeak": ".".join(map(str, b.version())),
                    "phonemizer": md.version("phonemizer")})
with open(out_path, "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, indent=1)
    f.write("\n")

# normalize_text raises on some inputs (a lone "," after a non-letter reaches
# int(""), and "ſept" misses the month table); the golden records the error
# so the override can name it.
norm = []
for line in open(repo / "testdata" / "normalize_corpus.txt", encoding="utf-8"):
    line = line.rstrip("\n")
    if not line.strip() or line.startswith("//"):
        continue
    try:
        norm.append({"input": line, "output": pp.normalize_text(line)})
    except (ValueError, KeyError) as e:
        norm.append({"input": line, "error": f"{type(e).__name__}: {e}"})
with open(out_path.parent / "normalize_golden.json", "w", encoding="utf-8") as f:
    json.dump(norm, f, ensure_ascii=False, indent=1)
    f.write("\n")
