// Package bench measures how fast a model synthesizes a fixed corpus: the
// real-time factor and time to first audio of each request, their p50 and p95,
// and whether they meet the release targets.
package bench

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/androiddrew/gokittentts/kittentts"
)

//go:embed corpus.txt
var corpusFile string

// Corpus is the fixed bench corpus, one request per entry, checked in and
// built into the binary so that runs are comparable across releases.
func Corpus() []string {
	var texts []string
	for line := range strings.Lines(corpusFile) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		texts = append(texts, strings.ReplaceAll(line, `\n`, "\n"))
	}
	return texts
}

// Streamer is a model; *kittentts.Model is one.
type Streamer interface {
	Stream(ctx context.Context, r kittentts.Request) iter.Seq2[kittentts.Chunk, error]
}

// Targets are the release gate: a run misses when a p95 is at or above its
// target.
type Targets struct {
	RTF               float64 `json:"rtf"`
	FirstAudioSeconds float64 `json:"first_audio_seconds"`
}

// DefaultTargets are real time with headroom: RTF < 0.8 and first audio < 1 s.
var DefaultTargets = Targets{RTF: 0.8, FirstAudioSeconds: 1}

// Options configure a Run.
type Options struct {
	Voice   string
	Runs    int // passes over the corpus, after one untimed warm-up request; 0 means 1
	Targets Targets
	Now     func() time.Time // nil means time.Now
}

// Sample is one measured request.
type Sample struct {
	Run               int     `json:"run"`
	TextIndex         int     `json:"text_index"` // in the corpus
	AudioSeconds      float64 `json:"audio_seconds"`
	SynthesisSeconds  float64 `json:"synthesis_seconds"`
	FirstAudioSeconds float64 `json:"first_audio_seconds"`
	RTF               float64 `json:"rtf"`
}

// Stats are the percentiles of one measure over every sample.
type Stats struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
}

// Result is what a Run measured.
type Result struct {
	CorpusTexts       int      `json:"corpus_texts"`
	CorpusSHA256      string   `json:"corpus_sha256"`
	Runs              int      `json:"runs"`
	RTF               Stats    `json:"rtf"`
	FirstAudioSeconds Stats    `json:"first_audio_seconds"`
	Targets           Targets  `json:"targets"`
	Misses            []string `json:"misses"` // why the run failed its targets; empty when it passed
	Samples           []Sample `json:"samples"`
}

// Pass reports whether both p95s are under their targets.
func (r Result) Pass() bool { return len(r.Misses) == 0 }

// Run synthesizes each text in corpus once per run, through the markdown pass
// and normalizer at speed 1, as the server does by default. The first text is
// synthesized once beforehand, untimed, so that ONNX Runtime's first-run setup
// isn't measured.
func Run(ctx context.Context, m Streamer, corpus []string, opts Options) (Result, error) {
	if len(corpus) == 0 {
		return Result{}, errors.New("bench: empty corpus")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	runs := max(opts.Runs, 1)
	res := Result{
		CorpusTexts:  len(corpus),
		CorpusSHA256: corpusSHA256(corpus),
		Runs:         runs,
		Targets:      opts.Targets,
		Misses:       []string{},
	}
	if _, err := measure(ctx, m, corpus[0], opts.Voice, now); err != nil {
		return Result{}, err
	}
	for run := range runs {
		for i, text := range corpus {
			s, err := measure(ctx, m, text, opts.Voice, now)
			if err != nil {
				return Result{}, err
			}
			s.Run, s.TextIndex = run, i
			res.Samples = append(res.Samples, s)
		}
	}

	rtfs := make([]float64, len(res.Samples))
	firsts := make([]float64, len(res.Samples))
	for i, s := range res.Samples {
		rtfs[i], firsts[i] = s.RTF, s.FirstAudioSeconds
	}
	res.RTF, res.FirstAudioSeconds = stats(rtfs), stats(firsts)
	if res.RTF.P95 >= opts.Targets.RTF {
		res.Misses = append(res.Misses, fmt.Sprintf("RTF p95 %.3f is not under %.3f", res.RTF.P95, opts.Targets.RTF))
	}
	if res.FirstAudioSeconds.P95 >= opts.Targets.FirstAudioSeconds {
		res.Misses = append(res.Misses, fmt.Sprintf("first audio p95 %.3f s is not under %.3f s", res.FirstAudioSeconds.P95, opts.Targets.FirstAudioSeconds))
	}
	return res, nil
}

// measure synthesizes one text, timing its first chunk and the whole.
func measure(ctx context.Context, m Streamer, text, voice string, now func() time.Time) (Sample, error) {
	if err := ctx.Err(); err != nil {
		return Sample{}, err
	}
	var s Sample
	samples := 0
	start := now()
	for c, err := range m.Stream(ctx, kittentts.Request{Text: text, Voice: voice, Speed: 1, Markdown: true, Normalize: true}) {
		if err != nil {
			return Sample{}, fmt.Errorf("bench: %w", err)
		}
		if samples == 0 && len(c.PCM) > 0 {
			s.FirstAudioSeconds = now().Sub(start).Seconds()
		}
		samples += len(c.PCM)
	}
	s.SynthesisSeconds = now().Sub(start).Seconds()
	if samples == 0 {
		return Sample{}, fmt.Errorf("bench: no audio for %q", text)
	}
	s.AudioSeconds = float64(samples) / kittentts.SampleRate
	s.RTF = s.SynthesisSeconds / s.AudioSeconds
	return s, nil
}

// stats takes nearest-rank percentiles.
func stats(xs []float64) Stats {
	xs = slices.Sorted(slices.Values(xs))
	rank := func(p float64) float64 {
		return xs[max(int(math.Ceil(p*float64(len(xs))))-1, 0)]
	}
	return Stats{P50: rank(0.50), P95: rank(0.95)}
}

func corpusSHA256(corpus []string) string {
	h := sha256.New()
	for _, text := range corpus {
		// Length-prefixed, so ["ab"] and ["a", "b"] differ.
		fmt.Fprintf(h, "%d:%s", len(text), text)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Record is a Result with what was run where, saved per release.
type Record struct {
	Date       time.Time `json:"date"`
	Revision   string    `json:"revision"` // the gokittentts commit
	Host       string    `json:"host"`
	GOOS       string    `json:"goos"`
	GOARCH     string    `json:"goarch"`
	ORTVersion string    `json:"onnxruntime_version"`
	Model      string    `json:"model"`
	Device     string    `json:"device"`
	Voice      string    `json:"voice"`
	Result
}

// WriteJSON writes the record as indented JSON, with pass spelled out.
func (rec Record) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Record
		Pass bool `json:"pass"`
	}{rec, rec.Pass()})
}

// WriteSummary writes the record for a human.
func (rec Record) WriteSummary(w io.Writer) {
	verdict := "PASS"
	if !rec.Pass() {
		verdict = "FAIL"
	}
	fmt.Fprintf(w, "%s on %s, voice %s: %d texts × %d runs (corpus %.12s)\n",
		rec.Model, rec.Device, rec.Voice, rec.CorpusTexts, rec.Runs, rec.CorpusSHA256)
	fmt.Fprintf(w, "  %-13s %8s %8s %8s\n", "", "p50", "p95", "target")
	fmt.Fprintf(w, "  %-13s %8.3f %8.3f %8s\n", "RTF", rec.RTF.P50, rec.RTF.P95, fmt.Sprintf("< %.2f", rec.Targets.RTF))
	fmt.Fprintf(w, "  %-13s %7.3fs %7.3fs %8s\n", "first audio", rec.FirstAudioSeconds.P50, rec.FirstAudioSeconds.P95, fmt.Sprintf("< %.2fs", rec.Targets.FirstAudioSeconds))
	fmt.Fprintln(w, verdict)
	for _, miss := range rec.Misses {
		fmt.Fprintln(w, "  "+miss)
	}
}
