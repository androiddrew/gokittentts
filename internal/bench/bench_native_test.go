//go:build native

package bench_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/androiddrew/gokittentts/internal/bench"
	"github.com/androiddrew/gokittentts/kittentts"
)

// Every corpus text synthesizes to audio with a real model, nano-fp32 as the
// fastest, and the measurements are consistent with each other.
func TestRunWithARealModel(t *testing.T) {
	lib, dir := os.Getenv("KITTEN_ORT_LIB"), os.Getenv("KITTEN_MODELS_DIR")
	if lib == "" || dir == "" {
		t.Fatal("native tests need KITTEN_ORT_LIB and KITTEN_MODELS_DIR; run make test-native")
	}
	const name = "kitten-tts-nano-0.8-fp32"
	engine, err := kittentts.NewEngine(lib, []kittentts.ModelConfig{
		{Name: name, Dir: filepath.Join(dir, name, "current"), Device: kittentts.CPU},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	m, err := engine.Model(name)
	if err != nil {
		t.Fatal(err)
	}
	corpus := bench.Corpus()
	res, err := bench.Run(context.Background(), m, corpus, bench.Options{Voice: "Bruno", Runs: 1, Targets: bench.DefaultTargets})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Samples) != len(corpus) {
		t.Fatalf("%d samples, want %d", len(res.Samples), len(corpus))
	}
	for _, s := range res.Samples {
		if s.AudioSeconds <= 0 || s.FirstAudioSeconds <= 0 || s.FirstAudioSeconds > s.SynthesisSeconds || s.RTF <= 0 {
			t.Errorf("text %d: inconsistent sample %+v", s.TextIndex, s)
		}
	}
	if res.RTF.P50 > res.RTF.P95 || res.FirstAudioSeconds.P50 > res.FirstAudioSeconds.P95 {
		t.Errorf("p50 above p95: RTF %+v, first audio %+v", res.RTF, res.FirstAudioSeconds)
	}
}
