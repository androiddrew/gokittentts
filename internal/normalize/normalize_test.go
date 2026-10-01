package normalize_test

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/androiddrew/gokittentts/internal/normalize"
)

// golden is one normalize_golden.json entry: Python's normalize_text output
// for a corpus input, or the error it raised.
type golden struct {
	Input  string  `json:"input"`
	Output *string `json:"output"`
	Error  string  `json:"error"`
}

// python is what Python gave: its output, or the error it raised.
func (g golden) python() string {
	if g.Output != nil {
		return *g.Output
	}
	return g.Error
}

// override is a deliberate difference from Python, reviewed at the end of the
// project (issue 19).
type override struct {
	Input    string `yaml:"input"`
	Python   string `yaml:"python"`
	Expected string `yaml:"expected"`
	Reason   string `yaml:"reason"`
	Approved bool   `yaml:"approved"`
}

// pending lists the substitutions not ported yet, with the issue that ports
// them. A corpus case that differs from Python and contains one of them is
// skipped rather than failed.
var pending = []struct {
	what string
	re   *regexp.Regexp
}{
	{"money (issue 12)", regexp.MustCompile(`[$€£¥₹₩₿]`)},
	{"percent (issue 12)", regexp.MustCompile(`%`)},
	{"dotted version (issue 12)", regexp.MustCompile(`\d+(?:\.\d+){2,}`)},
	{"model version (issue 12)", regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9]*-\d|\d-\d+-\d`)},
	{"URL (issue 13)", regexp.MustCompile(`https?://|www\.`)},
	{"email (issue 13)", regexp.MustCompile(`@`)},
	{"HTML (issue 13)", regexp.MustCompile(`<[^>]+>`)},
	{"et al. (issue 13)", regexp.MustCompile(`(?i)\bet\s+al\.`)},
	{"title (issue 13)", regexp.MustCompile(`(?i)\b(?:Dr|Prof|Mr|Mrs|Ms|Fig|Figs|pp|p|ch|sec)\.(?:[^m]|$)`)},
}

func TestCorpus(t *testing.T) {
	var cases []golden
	readJSON(t, "../../testdata/normalize_golden.json", &cases)
	overrides := loadOverrides(t, cases)

	for i, c := range cases {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			got := normalize.Text(c.Input)
			if o, ok := overrides[c.Input]; ok {
				if o.Python != c.python() {
					t.Errorf("override for %q is stale: it says Python gives %q, the golden says %q", c.Input, o.Python, c.python())
				}
				if got != o.Expected {
					t.Errorf("Text(%q)\n got: %q\nwant: %q (override: %s)", c.Input, got, o.Expected, o.Reason)
				}
				return
			}
			if c.Output == nil {
				t.Fatalf("Python raised %s on %q; the case needs an override", c.Error, c.Input)
			}
			if got == *c.Output {
				return
			}
			for _, p := range pending {
				if p.re.MatchString(c.Input) {
					t.Skipf("pending %s: Text(%q) = %q, Python gives %q", p.what, c.Input, got, *c.Output)
				}
			}
			t.Errorf("Text(%q)\n got: %q\nwant: %q (Python)", c.Input, got, *c.Output)
		})
	}
}

// loadOverrides reads normalize_overrides.yaml, checking that every entry
// is complete and names a corpus input.
func loadOverrides(t *testing.T, cases []golden) map[string]override {
	t.Helper()
	b, err := os.ReadFile("../../testdata/normalize_overrides.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var list []override
	if err := yaml.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	inCorpus := make(map[string]bool, len(cases))
	for _, c := range cases {
		inCorpus[c.Input] = true
	}
	byInput := make(map[string]override, len(list))
	for _, o := range list {
		switch {
		case !inCorpus[o.Input]:
			t.Errorf("override for %q: not in the corpus", o.Input)
		case o.Reason == "":
			t.Errorf("override for %q: no reason", o.Input)
		case o.Python == "":
			t.Errorf("override for %q: no python output", o.Input)
		case o.Expected == "":
			t.Errorf("override for %q: no expected output", o.Input)
		}
		if _, dup := byInput[o.Input]; dup {
			t.Errorf("override for %q: listed twice", o.Input)
		}
		byInput[o.Input] = o
	}
	return byInput
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func TestYearsAreReadAsYears(t *testing.T) {
	if got, want := normalize.Text("2024 budget"), "twenty twenty-four budget"; got != want {
		t.Errorf("Text(%q) = %q, want %q", "2024 budget", got, want)
	}
}
