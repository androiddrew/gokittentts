package bench_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/androiddrew/gokittentts/internal/bench"
	"github.com/androiddrew/gokittentts/kittentts"
)

// clock is a fake clock that the fake model advances as it "synthesizes".
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// timing is how one text synthesizes: each chunk takes its duration and
// yields its seconds of audio.
type timing struct {
	chunks []time.Duration
	audio  []float64 // seconds per chunk
}

// fakeModel synthesizes each text with its timing, advancing the clock. If
// set, firstCall replaces the timing of the first request.
type fakeModel struct {
	clock     *clock
	timings   map[string]timing
	firstCall *timing
	err       error
	requests  []kittentts.Request
}

func (m *fakeModel) Stream(_ context.Context, r kittentts.Request) iter.Seq2[kittentts.Chunk, error] {
	m.requests = append(m.requests, r)
	return func(yield func(kittentts.Chunk, error) bool) {
		if m.err != nil {
			yield(kittentts.Chunk{}, m.err)
			return
		}
		tm := m.timings[r.Text]
		if m.firstCall != nil && len(m.requests) == 1 {
			tm = *m.firstCall
		}
		for i, d := range tm.chunks {
			m.clock.t = m.clock.t.Add(d)
			pcm := make([]float32, int(tm.audio[i]*kittentts.SampleRate))
			if !yield(kittentts.Chunk{PCM: pcm}, nil) {
				return
			}
		}
	}
}

// oneChunk is a text that takes synth to make audio seconds in one chunk.
func oneChunk(synth time.Duration, audio float64) timing {
	return timing{chunks: []time.Duration{synth}, audio: []float64{audio}}
}

func newModel(timings map[string]timing) (*fakeModel, bench.Options) {
	c := &clock{t: time.Unix(0, 0)}
	return &fakeModel{clock: c, timings: timings}, bench.Options{
		Voice:   "Bruno",
		Runs:    1,
		Targets: bench.Targets{RTF: 0.8, FirstAudioSeconds: 1},
		Now:     c.now,
	}
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func TestRunReportsP50AndP95(t *testing.T) {
	// Twenty texts, each 10 s of audio taking i/10 s in one chunk, so the
	// RTFs are 0.01 … 0.20 and the first-audio times 0.1 … 2.0 s.
	timings := map[string]timing{}
	var corpus []string
	for i := 1; i <= 20; i++ {
		text := strings.Repeat("a", i)
		corpus = append(corpus, text)
		timings[text] = oneChunk(time.Duration(i)*100*time.Millisecond, 10)
	}
	m, opts := newModel(timings)
	opts.Targets.FirstAudioSeconds = 10
	res, err := bench.Run(context.Background(), m, corpus, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Nearest rank: p50 is the 10th of 20, p95 the 19th.
	if !near(res.RTF.P50, 0.10) || !near(res.RTF.P95, 0.19) {
		t.Errorf("RTF = %+v, want p50 0.10, p95 0.19", res.RTF)
	}
	if !near(res.FirstAudioSeconds.P50, 1.0) || !near(res.FirstAudioSeconds.P95, 1.9) {
		t.Errorf("first audio = %+v, want p50 1.0, p95 1.9", res.FirstAudioSeconds)
	}
	if len(res.Samples) != 20 {
		t.Errorf("%d samples, want 20", len(res.Samples))
	}
	if !res.Pass() {
		t.Errorf("misses %v, want none", res.Misses)
	}
}

func TestFirstAudioIsTheFirstChunkAndRTFTheWholeText(t *testing.T) {
	m, opts := newModel(map[string]timing{
		"long": {
			chunks: []time.Duration{300 * time.Millisecond, 900 * time.Millisecond},
			audio:  []float64{2, 4},
		},
	})
	res, err := bench.Run(context.Background(), m, []string{"long"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	s := res.Samples[0]
	if !near(s.FirstAudioSeconds, 0.3) || !near(s.SynthesisSeconds, 1.2) || !near(s.AudioSeconds, 6) || !near(s.RTF, 0.2) {
		t.Errorf("sample = %+v, want first audio 0.3, synthesis 1.2, audio 6, RTF 0.2", s)
	}
}

func TestMissesAtOrAboveTheTargets(t *testing.T) {
	tests := []struct {
		name       string
		synth      time.Duration
		audio      float64
		wantMisses int
	}{
		{"both met", 790 * time.Millisecond, 1, 0},
		{"RTF at target", 800 * time.Millisecond, 1, 1},
		{"first audio at target", 1000 * time.Millisecond, 10, 1},
		{"both missed", 2 * time.Second, 1, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, opts := newModel(map[string]timing{"x": oneChunk(tt.synth, tt.audio)})
			res, err := bench.Run(context.Background(), m, []string{"x"}, opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Misses) != tt.wantMisses || res.Pass() != (tt.wantMisses == 0) {
				t.Errorf("misses %q, pass %v; want %d misses", res.Misses, res.Pass(), tt.wantMisses)
			}
		})
	}
}

func TestTargetsAreConfigurable(t *testing.T) {
	m, opts := newModel(map[string]timing{"x": oneChunk(1500*time.Millisecond, 1)})
	opts.Targets = bench.Targets{RTF: 2, FirstAudioSeconds: 2}
	res, err := bench.Run(context.Background(), m, []string{"x"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pass() {
		t.Errorf("misses %q with RTF 1.5 against a target of 2", res.Misses)
	}
	if res.Targets != opts.Targets {
		t.Errorf("recorded targets %+v, want %+v", res.Targets, opts.Targets)
	}
}

func TestWarmUpIsNotMeasured(t *testing.T) {
	m, opts := newModel(map[string]timing{"x": oneChunk(100*time.Millisecond, 1)})
	slow := oneChunk(time.Minute, 1)
	m.firstCall = &slow
	opts.Runs = 3
	res, err := bench.Run(context.Background(), m, []string{"x"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.requests) != 4 {
		t.Errorf("%d requests, want a warm-up and 3 runs", len(m.requests))
	}
	if len(res.Samples) != 3 || res.Runs != 3 {
		t.Errorf("%d samples over %d runs, want 3 over 3", len(res.Samples), res.Runs)
	}
	if !near(res.RTF.P95, 0.1) {
		t.Errorf("RTF p95 = %v, want 0.1 without the warm-up", res.RTF.P95)
	}
}

func TestRequestsUseTheVoiceAndTheTextFrontEnd(t *testing.T) {
	m, opts := newModel(map[string]timing{"x": oneChunk(time.Millisecond, 1)})
	opts.Voice = "Luna"
	if _, err := bench.Run(context.Background(), m, []string{"x"}, opts); err != nil {
		t.Fatal(err)
	}
	for _, r := range m.requests {
		if r.Voice != "Luna" || !r.Markdown || !r.Normalize || r.Speed != 1 {
			t.Errorf("request %+v, want voice Luna, speed 1, markdown and normalize on", r)
		}
	}
}

func TestRunFails(t *testing.T) {
	t.Run("synthesis error", func(t *testing.T) {
		m, opts := newModel(nil)
		m.err = errors.New("boom")
		if _, err := bench.Run(context.Background(), m, []string{"x"}, opts); err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("err = %v, want boom", err)
		}
	})
	t.Run("no audio", func(t *testing.T) {
		m, opts := newModel(map[string]timing{})
		if _, err := bench.Run(context.Background(), m, []string{"x"}, opts); err == nil {
			t.Error("no error for a text with no audio")
		}
	})
	t.Run("empty corpus", func(t *testing.T) {
		m, opts := newModel(nil)
		if _, err := bench.Run(context.Background(), m, nil, opts); err == nil {
			t.Error("no error for an empty corpus")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		m, opts := newModel(map[string]timing{"x": oneChunk(time.Millisecond, 1)})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := bench.Run(ctx, m, []string{"x"}, opts); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestCorpusIsFixed(t *testing.T) {
	corpus := bench.Corpus()
	if len(corpus) < 10 {
		t.Fatalf("corpus has %d texts, want at least 10", len(corpus))
	}
	for i, text := range corpus {
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
			t.Errorf("text %d is %q, want prose", i, text)
		}
	}
	// The record names the corpus by hash, so runs over a different corpus
	// aren't mistaken for comparable ones.
	m, opts := newModel(map[string]timing{"a": oneChunk(time.Millisecond, 1), "b": oneChunk(time.Millisecond, 1)})
	ra, _ := bench.Run(context.Background(), m, []string{"a"}, opts)
	ra2, _ := bench.Run(context.Background(), m, []string{"a"}, opts)
	rb, _ := bench.Run(context.Background(), m, []string{"b"}, opts)
	if ra.CorpusSHA256 == "" || ra.CorpusSHA256 != ra2.CorpusSHA256 || ra.CorpusSHA256 == rb.CorpusSHA256 {
		t.Errorf("corpus hashes %q, %q, %q: want equal for the same corpus only", ra.CorpusSHA256, ra2.CorpusSHA256, rb.CorpusSHA256)
	}
	if ra.CorpusTexts != 1 {
		t.Errorf("corpus texts = %d, want 1", ra.CorpusTexts)
	}
}

func TestRecordSavesAndSummarizes(t *testing.T) {
	m, opts := newModel(map[string]timing{"x": oneChunk(900*time.Millisecond, 1)})
	res, err := bench.Run(context.Background(), m, []string{"x"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	rec := bench.Record{
		Date:     time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		Revision: "abc123",
		Model:    "kitten-tts-mini-0.8",
		Device:   "cpu",
		Voice:    "Bruno",
		Result:   res,
	}

	var buf bytes.Buffer
	if err := rec.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"date", "revision", "model", "device", "voice", "goos", "goarch", "rtf", "first_audio_seconds", "targets", "pass", "corpus_sha256", "samples"} {
		if _, ok := got[key]; !ok {
			t.Errorf("JSON record has no %q: %s", key, buf.String())
		}
	}
	if got["pass"] != false {
		t.Errorf("pass = %v, want false for RTF 0.9", got["pass"])
	}

	buf.Reset()
	rec.WriteSummary(&buf)
	summary := buf.String()
	for _, want := range []string{"kitten-tts-mini-0.8", "cpu", "p50", "p95", "RTF", "first audio", "FAIL"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary has no %q:\n%s", want, summary)
		}
	}
}
