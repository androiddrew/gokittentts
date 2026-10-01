"""End-to-end tests for make_golden.py, run through `make golden-test`.

Each test runs the script in a fresh `uv run` environment, so they need uv,
network access on first run, and the system libespeak-ng.so.1.
"""
import json
import pathlib
import subprocess

REPO = pathlib.Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "make_golden.py"


def run_golden(requirements, out):
    cmd = ["uv", "run", "--no-project"]
    for req in requirements:
        cmd += ["--with", req]
    cmd += ["python3", str(SCRIPT), str(out)]
    return subprocess.run(cmd, cwd=REPO, capture_output=True, text=True)


def test_refuses_to_run_with_phonemizer_fork(tmp_path):
    out = tmp_path / "golden.json"
    result = run_golden(["phonemizer-fork"], out)
    assert result.returncode != 0
    assert "phonemizer-fork is installed" in result.stderr
    assert not out.exists()


def test_refuses_to_run_with_phonemizer_older_than_3_4(tmp_path):
    out = tmp_path / "golden.json"
    result = run_golden(["phonemizer==3.3.0"], out)
    assert result.returncode != 0
    assert "phonemizer 3.3.0 < 3.4.0" in result.stderr
    assert not out.exists()


def generate(tmp_path):
    out = tmp_path / "golden.json"
    result = run_golden(["phonemizer>=3.4.0"], out)
    assert result.returncode == 0, result.stderr
    return json.loads(out.read_text())


def test_golden_reads_decimal_as_one_number(tmp_path):
    golden = generate(tmp_path)
    decimal = [g for g in golden if "3.5" in g["chunk"]]
    assert decimal, "no golden chunk contains 3.5"
    for g in decimal:
        assert "θɹˈiː pɔɪnt fˈaɪv" in g["phonemes"], g["phonemes"]


def test_golden_records_espeak_and_phonemizer_versions(tmp_path):
    golden = generate(tmp_path)
    for g in golden:
        assert g["espeak"].startswith("1."), g["espeak"]
        major, minor = (int(x) for x in g["phonemizer"].split(".")[:2])
        assert (major, minor) >= (3, 4)


def test_golden_keeps_words_after_a_decimal_in_a_chunk_ending_in_a_period(tmp_path):
    golden = generate(tmp_path)
    [g] = [g for g in golden if "2.718." in g["chunk"]]
    assert g["phonemes"].endswith("tˈuː pɔɪnt sˈɛvən wˈʌn ˈeɪt. "), g["phonemes"]


def test_normalizer_golden_reads_years_and_records_errors(tmp_path):
    generate(tmp_path)
    golden = json.loads((tmp_path / "normalize_golden.json").read_text())
    by_input = {g["input"]: g for g in golden}
    assert by_input["2024 budget"]["output"] == "twenty twenty-four budget"
    assert by_input["Wait , what?"]["error"].startswith("ValueError")
    assert not any(g["input"].startswith("//") for g in golden)
