//go:build native

package espeak_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/androiddrew/gokittentts/internal/phonemize"
	"github.com/androiddrew/gokittentts/internal/phonemize/espeak"
	"github.com/androiddrew/gokittentts/internal/tokenize"
)

type golden struct {
	Chunk      string  `json:"chunk"`
	Phonemes   string  `json:"phonemes"`
	IDs        []int64 `json:"ids"`
	Espeak     string  `json:"espeak"`
	Phonemizer string  `json:"phonemizer"`
}

func TestPhonemesAndIDsMatchGolden(t *testing.T) {
	b, err := os.ReadFile("../../../testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []golden
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	backend, err := espeak.New()
	if err != nil {
		t.Fatal(err)
	}
	p := phonemize.PreservePunctuation(backend)
	for _, c := range cases {
		ph, err := p.Phonemize(c.Chunk)
		if err != nil {
			t.Fatalf("%q: %v", c.Chunk, err)
		}
		if ph != c.Phonemes {
			t.Errorf("%q phonemes\n got %q\nwant %q", c.Chunk, ph, c.Phonemes)
		}
		if ids := tokenize.IDs(ph); !slices.Equal(ids, c.IDs) {
			t.Errorf("%q ids\n got %v\nwant %v", c.Chunk, ids, c.IDs)
		}
	}
	if t.Failed() {
		t.Logf("golden made with espeak-ng %s and phonemizer %s; this machine has espeak-ng %s",
			cases[0].Espeak, cases[0].Phonemizer, espeak.Version())
	}
}
