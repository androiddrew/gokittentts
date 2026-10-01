//go:build native

package kittentts_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/androiddrew/gokittentts/kittentts"
)

// ONNX Runtime's environment is process-wide, so every test shares one
// Engine. `make test-native` sets the environment variables.
var (
	engine *kittentts.Engine
	models []string // every model in KITTEN_MODELS_DIR
)

// Extra models the tests configure: one with a broken contract, one built
// from a real model with its own config.json and voices file, one that
// nothing loads before TestModelsLoadOnFirstUseAndOnce, one whose
// directory doesn't exist, and one on CUDA, which the CPU-only ONNX Runtime
// can't enable.
const (
	badContract = "bad-contract"
	ownVoices   = "own-voices"
	lazy        = "lazy"
	missing     = "missing"
	onCUDA      = "on-cuda"
)

func TestMain(m *testing.M) {
	// Child mode for TestNewEngineRejectsOtherORTVersions.
	if lib := os.Getenv("KITTEN_TEST_NEW_ENGINE"); lib != "" {
		_, err := kittentts.NewEngine(lib, nil)
		fmt.Print(err)
		os.Exit(0)
	}
	// Child mode for TestCUDA.
	if lib := os.Getenv("KITTEN_TEST_CUDA"); lib != "" {
		if err := synthesizeOnCUDA(lib, os.Getenv("KITTEN_MODELS_DIR")); err != nil {
			fmt.Print(err)
		} else {
			fmt.Print("ok")
		}
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
	// Models are in the model store's layout: <name>/current/config.json.
	configs := []kittentts.ModelConfig{{Name: badContract, Dir: filepath.Join("testdata", badContract)}}
	for _, e := range entries {
		modelDir := filepath.Join(dir, e.Name(), "current")
		if _, err := os.Stat(filepath.Join(modelDir, "config.json")); err == nil {
			models = append(models, e.Name())
			configs = append(configs, kittentts.ModelConfig{Name: e.Name(), Dir: modelDir, Device: kittentts.CPU})
		}
	}
	if len(models) == 0 {
		fmt.Fprintln(os.Stderr, "no models in", dir)
		os.Exit(1)
	}
	tmp, err := os.MkdirTemp("", "kittentts-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	first := filepath.Join(dir, models[0], "current")
	own := filepath.Join(tmp, ownVoices)
	if err := makeOwnVoicesModel(own, first); err != nil {
		fmt.Fprintln(os.Stderr, "making the own-voices model:", err)
		os.Exit(1)
	}
	configs = append(configs,
		kittentts.ModelConfig{Name: ownVoices, Dir: own},
		kittentts.ModelConfig{Name: lazy, Dir: first},
		kittentts.ModelConfig{Name: missing, Dir: filepath.Join(tmp, missing)},
		kittentts.ModelConfig{Name: onCUDA, Dir: first, Device: kittentts.CUDA},
	)
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
	os.RemoveAll(tmp)
	os.Exit(code)
}

// makeOwnVoicesModel makes a model in dir from the one in src: the same
// .onnx file, but a config.json naming solo.npz, which holds only
// expr-voice-2-f, under the name Solo.
func makeOwnVoicesModel(dir, src string) error {
	b, err := os.ReadFile(filepath.Join(src, "config.json"))
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	onnx, _ := cfg["model_file"].(string)
	if err := os.Symlink(filepath.Join(src, onnx), filepath.Join(dir, onnx)); err != nil {
		return err
	}
	srcVoices, _ := cfg["voices"].(string)
	cfg["voices"] = "solo.npz"
	cfg["voice_aliases"] = map[string]string{"Solo": "expr-voice-2-f"}
	cfg["speed_priors"] = map[string]float32{}
	if b, err = json.Marshal(cfg); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o644); err != nil {
		return err
	}

	r, err := zip.OpenReader(filepath.Join(src, srcVoices))
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := os.Create(filepath.Join(dir, "solo.npz"))
	if err != nil {
		return err
	}
	w := zip.NewWriter(f)
	for _, entry := range r.File {
		if entry.Name == "expr-voice-2-f.npy" {
			if err := w.Copy(entry); err != nil {
				return err
			}
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	return f.Close()
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

func TestCUDAFailureIsALoadError(t *testing.T) {
	_, err := engine.Model(onCUDA)
	if err == nil || !strings.Contains(err.Error(), "CUDA") {
		t.Fatalf("loading a CUDA model with the CPU-only ONNX Runtime: err = %v, want an error naming CUDA", err)
	}
	if engine.Loaded(onCUDA) {
		t.Error("a model whose CUDA provider failed reports loaded; it must not fall back to the CPU")
	}
}

// TestCUDA runs nano-fp32 on GPU 0 with a CUDA build of ONNX Runtime, in a
// child process, since a process can load only one ONNX Runtime.
func TestCUDA(t *testing.T) {
	lib := os.Getenv("KITTEN_ORT_LIB_CUDA")
	if lib == "" {
		t.Skip("KITTEN_ORT_LIB_CUDA not set")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "KITTEN_TEST_CUDA="+lib)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if !strings.HasSuffix(string(out), "ok") {
		t.Fatalf("nano-fp32 on CUDA: %s", out)
	}
}

// synthesizeOnCUDA loads nano-fp32 on GPU 0, synthesizes a sentence and
// checks with nvidia-smi that this process holds a CUDA context.
func synthesizeOnCUDA(lib, dir string) error {
	const name = "kitten-tts-nano-0.8-fp32"
	e, err := kittentts.NewEngine(lib, []kittentts.ModelConfig{
		{Name: name, Dir: filepath.Join(dir, name, "current"), Device: kittentts.CUDA},
	})
	if err != nil {
		return err
	}
	defer e.Close()
	m, err := e.Model(name)
	if err != nil {
		return err
	}
	pcm, err := m.Synthesize(context.Background(), kittentts.Request{Text: "Hello from the GPU.", Voice: "Bruno", Speed: 1})
	if err != nil {
		return err
	}
	if r := rms(pcm); r < 0.01 {
		return fmt.Errorf("%d samples with RMS %.4f; want speech", len(pcm), r)
	}
	out, err := exec.Command("nvidia-smi", "--query-compute-apps=pid", "--format=csv,noheader").Output()
	if err != nil {
		return fmt.Errorf("nvidia-smi: %w", err)
	}
	if !slices.Contains(strings.Fields(string(out)), fmt.Sprint(os.Getpid())) {
		return fmt.Errorf("pid %d is not among nvidia-smi's compute apps (%q)", os.Getpid(), out)
	}
	return nil
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

// The static Voices table must agree with every model's config.json.
func TestVoicesTableMatchesModels(t *testing.T) {
	for _, name := range models {
		m := mustModel(t, name)
		for _, v := range kittentts.Voices {
			if key, err := kittentts.VoiceKey(m, v.Name); err != nil || key != v.Key {
				t.Errorf("%s: %s resolves to %q (%v), want %s", name, v.Name, key, err, v.Key)
			}
		}
		if len(m.Voices()) != len(kittentts.Voices) {
			t.Errorf("%s has %d voices, the table %d", name, len(m.Voices()), len(kittentts.Voices))
		}
	}
}

func TestWaveformLengthMatchesDurations(t *testing.T) {
	for _, name := range models {
		wave, durations, err := kittentts.RunChunk(mustModel(t, name), "Hello from Go.", "Bruno", 1)
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
	var chunks []kittentts.Chunk
	for c, err := range m.Stream(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, c)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	for i, c := range chunks {
		if len(c.PCM) == 0 || rms(c.PCM) <= 0.01 {
			t.Errorf("chunk %d: %d samples, RMS %.4f", i, len(c.PCM), rms(c.PCM))
		}
	}

	// The sample values are random, but the durations are not, so each
	// streamed chunk is exactly an untrimmed run less 5,000 samples, and it
	// reports the run's token count and summed durations.
	for i, text := range []string{"First sentence.", "Second sentence!", "Third one?"} {
		wave, durations, err := kittentts.RunChunk(m, text, "Leo", speedPrior(models[0], "Leo"))
		if err != nil {
			t.Fatal(err)
		}
		if len(chunks[i].PCM) != len(wave)-5000 {
			t.Errorf("chunk %d: %d samples, want %d (untrimmed %d)", i, len(chunks[i].PCM), len(wave)-5000, len(wave))
		}
		var frames int
		for _, d := range durations {
			frames += int(d)
		}
		if chunks[i].Tokens != len(durations) || chunks[i].Frames != frames {
			t.Errorf("chunk %d: %d tokens, %d frames; want %d and %d", i, chunks[i].Tokens, chunks[i].Frames, len(durations), frames)
		}
	}
}

func TestStreamStopsWhenContextIsCanceled(t *testing.T) {
	m := mustModel(t, models[0])
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := kittentts.Request{Text: "One. Two. Three.", Voice: "Kiki", Speed: 1}
	var audio, errs int
	for c, err := range m.Stream(ctx, req) {
		if err != nil {
			errs++
			if err != context.Canceled {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			continue
		}
		audio++
		if len(c.PCM) > 0 {
			cancel()
		}
	}
	if audio != 1 || errs != 1 {
		t.Fatalf("got %d audio chunks and %d errors, want 1 and 1", audio, errs)
	}
}

// speedPrior is the prior each model's config.json sets for a voice.
func speedPrior(model, voice string) float32 {
	if !strings.HasPrefix(model, "kitten-tts-nano-") {
		return 1
	}
	if voice == "Hugo" {
		return 0.9
	}
	return 0.8
}

func TestSpeedPriors(t *testing.T) {
	const text = "The quick brown fox jumps over the lazy dog."
	for _, c := range []struct {
		model, voice string
		prior        float32
	}{
		{"kitten-tts-nano-0.8-fp32", "Bella", 0.8},
		{"kitten-tts-nano-0.8-fp32", "Hugo", 0.9},
		{"kitten-tts-nano-0.8-int8", "Bella", 0.8},
		{"kitten-tts-nano-0.8-int8", "Hugo", 0.9},
		{"kitten-tts-mini-0.8", "Bella", 1},
		{"kitten-tts-micro-0.8", "Hugo", 1},
	} {
		t.Run(c.model+"/"+c.voice, func(t *testing.T) {
			if !slices.Contains(models, c.model) {
				t.Fatalf("%s is not in KITTEN_MODELS_DIR; make test-native fetches all four models", c.model)
			}
			m := mustModel(t, c.model)
			// Durations are deterministic, so a stream at speed 1 has the
			// frames of a raw run at the prior, and only those.
			frames := func(speed float32) int {
				_, durations, err := kittentts.RunChunk(m, text, c.voice, speed)
				if err != nil {
					t.Fatal(err)
				}
				var n int
				for _, d := range durations {
					n += int(d)
				}
				return n
			}
			var streamed int
			for chunk, err := range m.Stream(context.Background(), kittentts.Request{Text: text, Voice: c.voice, Speed: 1}) {
				if err != nil {
					t.Fatal(err)
				}
				streamed += chunk.Frames
			}
			if want := frames(c.prior); streamed != want {
				t.Errorf("speed 1 streamed %d frames, want %d (a raw run at %v)", streamed, want, c.prior)
			}
			if c.prior != 1 && frames(1) == frames(c.prior) {
				t.Errorf("raw runs at 1 and %v have the same frames, so this test can't see the prior", c.prior)
			}
		})
	}
}

func TestVoicesComeFromTheModelsOwnDirectory(t *testing.T) {
	m := mustModel(t, ownVoices)
	if got := m.Voices(); !slices.Equal(got, []string{"Solo"}) {
		t.Fatalf("Voices() = %v, want [Solo] from the model's own config.json", got)
	}
	for _, voice := range []string{"Solo", "expr-voice-2-f"} {
		if _, err := m.Synthesize(context.Background(), kittentts.Request{Text: "Hi.", Voice: voice, Speed: 1}); err != nil {
			t.Errorf("%s: %v", voice, err)
		}
	}
	for _, voice := range []string{"Bella", "expr-voice-3-m"} {
		if _, err := m.Synthesize(context.Background(), kittentts.Request{Text: "Hi.", Voice: voice, Speed: 1}); err == nil {
			t.Errorf("%s: want an error, since solo.npz doesn't have it", voice)
		}
	}
}

func TestModelsLoadOnFirstUseAndOnce(t *testing.T) {
	// NewEngine accepted a model whose directory doesn't exist, so it
	// loaded nothing up front.
	if engine.Loaded(missing) {
		t.Error("a model that can't load reports loaded")
	}
	if _, err := engine.Model(missing); err == nil {
		t.Fatal("loading a missing model: want an error")
	}
	if engine.Loaded(missing) {
		t.Error("a model that failed to load reports loaded")
	}

	if engine.Loaded(lazy) {
		t.Fatal("a model nothing has used reports loaded")
	}
	got := make([]*kittentts.Model, 8)
	var wg sync.WaitGroup
	for i := range got {
		wg.Go(func() {
			m, err := engine.Model(lazy)
			if err != nil {
				t.Error(err)
			}
			got[i] = m
		})
	}
	wg.Wait()
	for i, m := range got {
		if m == nil || m != got[0] {
			t.Fatalf("concurrent first loads returned %v; want one model (index %d differs)", got, i)
		}
	}
	if !engine.Loaded(lazy) {
		t.Error("a loaded model reports not loaded")
	}
}

func TestMarkdownIsReadAsProse(t *testing.T) {
	const reply = "# Release notes\n\n**Big** news: read [the docs](https://example.com/docs) 🎉\n\n" +
		"- Faster *startup*\n- Run `gokittentts pull`\n\n```sh\nrm -rf /tmp/cache\n```\n"
	// What the reply should be spoken as.
	const prose = "Release notes.\n\nBig news: read the docs \n\nFaster startup.\n\nRun gokittentts pull."
	m := mustModel(t, models[0])
	// Token ids come from espeak-ng and the tokenizer, which are
	// deterministic, so equal counts per chunk mean the same text reached
	// the model.
	tokens := func(text string, markdown bool) []int {
		var counts []int
		for c, err := range m.Stream(context.Background(), kittentts.Request{Text: text, Voice: "Leo", Speed: 1, Markdown: markdown}) {
			if err != nil {
				t.Fatal(err)
			}
			counts = append(counts, c.Tokens)
		}
		return counts
	}
	spoken := tokens(reply, true)
	if want := tokens(prose, false); !slices.Equal(spoken, want) {
		t.Errorf("the reply ran as %v tokens per chunk; the prose it should read as runs as %v", spoken, want)
	}
	if literal := tokens(reply, false); slices.Equal(literal, spoken) {
		t.Errorf("with Markdown off the reply ran as %v tokens per chunk, the same as with it on", literal)
	}
}

// goldenChunk is a testdata/golden.json entry from the Python reference.
type goldenChunk struct {
	Chunk string `json:"chunk"`
	IDs   []int  `json:"ids"`
}

func TestNormalizeIsOptional(t *testing.T) {
	m := mustModel(t, models[0])
	tokens := func(text string, normalize bool) []int {
		var counts []int
		for c, err := range m.Stream(context.Background(), kittentts.Request{Text: text, Voice: "Leo", Speed: 1, Normalize: normalize}) {
			if err != nil {
				t.Fatal(err)
			}
			counts = append(counts, c.Tokens)
		}
		return counts
	}
	const year, spoken = "The 2024 budget passed.", "The twenty twenty-four budget passed."
	if got, want := tokens(year, true), tokens(spoken, false); !slices.Equal(got, want) {
		t.Errorf("normalized, %q ran as %v tokens per chunk; %q runs as %v", year, got, spoken, want)
	}
	if got, normalized := tokens(year, false), tokens(year, true); slices.Equal(got, normalized) {
		t.Errorf("with Normalize off, %q ran as %v tokens per chunk, the same as with it on", year, got)
	}
	// The phonemizer keeps a decimal whole, so espeak-ng reads it as one
	// number even with the normalizer off: the chunk runs as the Python
	// reference's ids for "θɹˈiː pɔɪnt fˈaɪv".
	const decimal = "Version 3.5 shipped on time."
	b, err := os.ReadFile("../testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden []goldenChunk
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(golden, func(g goldenChunk) bool { return g.Chunk == decimal })
	if i < 0 {
		t.Fatalf("no golden chunk %q", decimal)
	}
	if got, want := tokens(decimal, false), []int{len(golden[i].IDs)}; !slices.Equal(got, want) {
		t.Errorf("with Normalize off, %q ran as %v tokens per chunk; the Python reference has %v", decimal, got, want)
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
