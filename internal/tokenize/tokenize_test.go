package tokenize_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/androiddrew/gokittentts/internal/tokenize"
)

type golden struct {
	Chunk    string  `json:"chunk"`
	Phonemes string  `json:"phonemes"`
	IDs      []int64 `json:"ids"`
}

func TestIDsMatchGolden(t *testing.T) {
	b, err := os.ReadFile("../../testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []golden
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := tokenize.IDs(c.Phonemes); !slices.Equal(got, c.IDs) {
			t.Errorf("%q\nphonemes %q\n got %v\nwant %v", c.Chunk, c.Phonemes, got, c.IDs)
		}
	}
}
