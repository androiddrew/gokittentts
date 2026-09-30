//go:build native

package kittentts_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/androiddrew/gokittentts/kittentts"
)

// ONNX Runtime's environment is process-wide, so every test shares one
// Engine. `make test-native` sets the environment variables.
var (
	engine *kittentts.Engine
	models []string // every model in KITTEN_MODELS_DIR
)

const badContract = "bad-contract"

func TestMain(m *testing.M) {
	// Child mode for TestNewEngineRejectsOtherORTVersions.
	if lib := os.Getenv("KITTEN_TEST_NEW_ENGINE"); lib != "" {
		_, err := kittentts.NewEngine(lib, nil)
		fmt.Print(err)
		os.Exit(0)
	}

	lib, dir := os.Getenv("KITTEN_ORT_LIB"), os.Getenv("KITTEN_MODELS_DIR")
	if lib == "" || dir == "" {
		fmt.Fprintln(os.Stderr, "native tests need KITTEN_ORT_LIB and KITTEN_MODELS_DIR; run make test-native")
		os.Exit(1)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	configs := []kittentts.ModelConfig{{Name: badContract, Dir: filepath.Join("testdata", badContract)}}
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "config.json")); err == nil {
			models = append(models, e.Name())
			configs = append(configs, kittentts.ModelConfig{Name: e.Name(), Dir: filepath.Join(dir, e.Name()), Device: kittentts.CPU})
		}
	}
	if len(models) == 0 {
		fmt.Fprintln(os.Stderr, "no models in", dir)
		os.Exit(1)
	}
	engine, err = kittentts.NewEngine(lib, configs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	if err := engine.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "close:", err)
		code = 1
	}
	os.Exit(code)
}

func TestNewEngineRejectsOtherORTVersions(t *testing.T) {
	other := os.Getenv("KITTEN_ORT_LIB_OTHER")
	if other == "" {
		t.Skip("KITTEN_ORT_LIB_OTHER not set")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "KITTEN_TEST_NEW_ENGINE="+other)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "1.29.1") || !strings.Contains(string(out), "1.30.0") {
		t.Fatalf("NewEngine with ONNX Runtime 1.30.0: got %q, want an error naming 1.29.1 and 1.30.0", out)
	}
}

func TestModelContractIsCheckedOnLoad(t *testing.T) {
	for _, name := range models {
		if _, err := engine.Model(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err := engine.Model(badContract)
	if err == nil || !strings.Contains(err.Error(), "duration") {
		t.Fatalf("loading a model whose duration is float32: err = %v, want a contract error naming duration", err)
	}
}

func TestUnknownModel(t *testing.T) {
	if _, err := engine.Model("no-such-model"); err == nil {
		t.Fatal("want an error")
	}
}

func TestVoices(t *testing.T) {
	m := mustModel(t, models[0])
	want := []string{"Bella", "Bruno", "Hugo", "Jasper", "Kiki", "Leo", "Luna", "Rosie"}
	if got := m.Voices(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Voices() = %v, want %v", got, want)
	}
}

func TestWaveformLengthMatchesDurations(t *testing.T) {
	for _, name := range models {
		wave, durations, err := kittentts.RunChunk(mustModel(t, name), "Hello from Go.", "Bruno")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var sum int64
		for _, d := range durations {
			sum += d
		}
		if int64(len(wave)) != sum*600 {
			t.Errorf("%s: len(waveform) = %d, sum(duration) × 600 = %d", name, len(wave), sum*600)
		}
	}
}

func TestSynthesizeProducesSpeech(t *testing.T) {
	for _, name := range models {
		m := mustModel(t, name)
		for _, voice := range []string{"Bruno", "expr-voice-2-f"} {
			pcm, err := m.Synthesize(context.Background(), kittentts.Request{Text: "Hello from Go. This is a test.", Voice: voice, Speed: 1})
			if err != nil {
				t.Fatalf("%s %s: %v", name, voice, err)
			}
			if len(pcm) == 0 {
				t.Fatalf("%s %s: no audio", name, voice)
			}
			if r := rms(pcm); r <= 0.01 {
				t.Errorf("%s %s: RMS %.4f, want > 0.01", name, voice, r)
			}
		}
	}
}

func TestSynthesizeRejectsUnknownVoice(t *testing.T) {
	_, err := mustModel(t, models[0]).Synthesize(context.Background(), kittentts.Request{Text: "Hi.", Voice: "alloy", Speed: 1})
	if err == nil || !strings.Contains(err.Error(), "Bruno") {
		t.Fatalf("err = %v, want an error listing the valid voices", err)
	}
}

func TestStreamYieldsOneTrimmedChunkAtATime(t *testing.T) {
	m := mustModel(t, models[0])
	req := kittentts.Request{Text: "First sentence. Second sentence! Third one?", Voice: "Leo", Speed: 1}
	var chunks [][]float32
	for pcm, err := range m.Stream(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, pcm)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	for i, c := range chunks {
		if len(c) == 0 || rms(c) <= 0.01 {
			t.Errorf("chunk %d: %d samples, RMS %.4f", i, len(c), rms(c))
		}
	}

	// The sample values are random, but the durations are not, so each
	// streamed chunk is exactly an untrimmed run less 5,000 samples.
	for i, text := range []string{"First sentence.", "Second sentence!", "Third one?"} {
		wave, _, err := kittentts.RunChunk(m, text, "Leo")
		if err != nil {
			t.Fatal(err)
		}
		if len(chunks[i]) != len(wave)-5000 {
			t.Errorf("chunk %d: %d samples, want %d (untrimmed %d)", i, len(chunks[i]), len(wave)-5000, len(wave))
		}
	}
}

func TestStreamStopsWhenContextIsCanceled(t *testing.T) {
	m := mustModel(t, models[0])
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := kittentts.Request{Text: "One. Two. Three.", Voice: "Kiki", Speed: 1}
	var audio, errs int
	for pcm, err := range m.Stream(ctx, req) {
		if err != nil {
			errs++
			if err != context.Canceled {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			continue
		}
		audio++
		if len(pcm) > 0 {
			cancel()
		}
	}
	if audio != 1 || errs != 1 {
		t.Fatalf("got %d audio chunks and %d errors, want 1 and 1", audio, errs)
	}
}

func mustModel(t *testing.T, name string) *kittentts.Model {
	t.Helper()
	m, err := engine.Model(name)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func rms(pcm []float32) float64 {
	var s float64
	for _, v := range pcm {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(pcm)))
}
