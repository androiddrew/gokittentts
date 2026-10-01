package server_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/go-mp3"

	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/internal/server"
	"github.com/androiddrew/gokittentts/kittentts"
)

// fakeEngine records what reaches it and streams fixed chunks.
type fakeEngine struct {
	mu     sync.Mutex
	calls  []call
	pcm    []float32         // shorthand for one chunk, if chunks is nil
	chunks []kittentts.Chunk // streamed in order
	err    error             // yielded after the chunks

	// gate, if set, paces the stream: each chunk after the first waits for
	// a receive. produced counts the chunks yielded, and finished, if set,
	// is closed when the stream returns.
	gate     chan struct{}
	produced int
	finished chan struct{}

	stall *stall
}

// stall holds a stream before chunk at until release is closed or, unless
// ignoresContext, the context ends, which yields the context's error. A
// stall that ignores the context is like a model run that can't be
// interrupted.
type stall struct {
	release        chan struct{}
	at             int
	ignoresContext bool
}

func (f *fakeEngine) setStall(st *stall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stall = st
}

type call struct {
	model string
	req   kittentts.Request
	ctx   context.Context
}

func (f *fakeEngine) Stream(ctx context.Context, model string, r kittentts.Request) iter.Seq2[kittentts.Chunk, error] {
	f.mu.Lock()
	f.calls = append(f.calls, call{model, r, ctx})
	chunks, err, st := f.chunks, f.err, f.stall
	if chunks == nil && err == nil {
		chunks = []kittentts.Chunk{{PCM: f.pcm}}
	}
	f.mu.Unlock()
	return func(yield func(kittentts.Chunk, error) bool) {
		if f.finished != nil {
			defer close(f.finished)
		}
		for i, c := range chunks {
			if i > 0 && f.gate != nil {
				<-f.gate
			}
			if st != nil && i == st.at {
				if st.ignoresContext {
					<-st.release
				} else {
					select {
					case <-st.release:
					case <-ctx.Done():
						yield(kittentts.Chunk{}, ctx.Err())
						return
					}
				}
			}
			f.mu.Lock()
			f.produced++
			f.mu.Unlock()
			if !yield(c, nil) {
				return
			}
		}
		if err != nil {
			yield(kittentts.Chunk{}, err)
		}
	}
}

func (f *fakeEngine) producedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.produced
}

func (f *fakeEngine) last(t *testing.T) call {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("the engine was not called")
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeEngine) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

const testConfig = `
default_model: kitten-tts-mini-0.8
models:
  kitten-tts-mini-0.8: {}
  kitten-tts-nano-0.8-fp32: {}
model_aliases:
  tts-1: default
  gpt-4o-mini-tts: default
  tts-1-hd: kitten-tts-nano-0.8-fp32
limits:
  max_input_chars: 20
ffmpeg: ""
`

func newServer(t *testing.T) (http.Handler, *fakeEngine) {
	t.Helper()
	return newServerWithConfig(t, testConfig)
}

func newServerWithConfig(t *testing.T, yaml string) (http.Handler, *fakeEngine) {
	t.Helper()
	return newServerWithEnv(t, yaml, nil)
}

func newServerWithEnv(t *testing.T, yaml string, env map[string]string) (http.Handler, *fakeEngine) {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml), func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	// 0, 0.5, -0.5 as 16-bit little-endian PCM: 0000 0040 00c0.
	eng := &fakeEngine{pcm: []float32{0, 0.5, -0.5}}
	return server.New(cfg, eng), eng
}

func speech(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(body)))
	return rec
}

// openAIError checks the status and that the body is {"error": {message,
// type, param, code}}, and returns the error object.
func openAIError(t *testing.T, rec *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d; body %s", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q, want application/json", ct)
	}
	var body map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body %s: %v", rec.Body, err)
	}
	e, ok := body["error"]
	if !ok {
		t.Fatalf("no error object in %s", rec.Body)
	}
	for _, k := range []string{"message", "type", "param", "code"} {
		if _, ok := e[k]; !ok {
			t.Errorf("error object has no %q: %s", k, rec.Body)
		}
	}
	if msg, _ := e["message"].(string); msg == "" {
		t.Errorf("empty message: %s", rec.Body)
	}
	return e
}

func TestSpeechPCM(t *testing.T) {
	h, _ := newServer(t)
	rec := speech(t, h, `{"model":"tts-1","input":"Hello.","voice":"alloy","response_format":"pcm"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/pcm" {
		t.Errorf("Content-Type %q, want audio/pcm", ct)
	}
	if got := hex.EncodeToString(rec.Body.Bytes()); got != "0000004000c0" {
		t.Errorf("body %s, want 0000004000c0 (16-bit little-endian)", got)
	}
}

func TestSpeechWAV(t *testing.T) {
	h, _ := newServer(t)
	rec := speech(t, h, `{"model":"tts-1","input":"Hello.","voice":"alloy","response_format":"wav"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/wav" {
		t.Errorf("Content-Type %q, want audio/wav", ct)
	}
	b := rec.Body.Bytes()
	if len(b) != 44+6 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatalf("not a 3-sample WAV: %x", b)
	}
	if got := hex.EncodeToString(b[4:8]) + hex.EncodeToString(b[40:44]); got != "ffffffff"+"ffffffff" {
		t.Errorf("RIFF and data sizes %s, want ffffffff ffffffff (streamed)", got)
	}
	if got := hex.EncodeToString(b[22:24]) + hex.EncodeToString(b[24:28]) + hex.EncodeToString(b[34:36]); got != "0100"+"c05d0000"+"1000" {
		t.Errorf("channels, rate, bits = %s, want mono, 24000 Hz, 16-bit", got)
	}
	if got := hex.EncodeToString(b[44:]); got != "0000004000c0" {
		t.Errorf("samples %s, want 0000004000c0", got)
	}
}

func TestModelResolution(t *testing.T) {
	cases := map[string]string{
		"kitten-tts-mini-0.8":      "kitten-tts-mini-0.8",
		"kitten-tts-nano-0.8-fp32": "kitten-tts-nano-0.8-fp32",
		"tts-1":                    "kitten-tts-mini-0.8",
		"gpt-4o-mini-tts":          "kitten-tts-mini-0.8",
		"tts-1-hd":                 "kitten-tts-nano-0.8-fp32",
	}
	h, eng := newServer(t)
	for model, want := range cases {
		rec := speech(t, h, `{"model":"`+model+`","input":"Hi.","voice":"Leo"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", model, rec.Code, rec.Body)
		}
		if got := eng.last(t).model; got != want {
			t.Errorf("model %s reached the engine as %s, want %s", model, got, want)
		}
	}
}

func TestUnknownModel(t *testing.T) {
	h, _ := newServer(t)
	e := openAIError(t, speech(t, h, `{"model":"tts-2","input":"Hi.","voice":"Leo"}`), http.StatusBadRequest)
	if e["param"] != "model" || !strings.Contains(e["message"].(string), "tts-2") {
		t.Errorf("error %v", e)
	}
	openAIError(t, speech(t, h, `{"input":"Hi.","voice":"Leo"}`), http.StatusBadRequest)
}

func TestVoiceResolution(t *testing.T) {
	cases := map[string]string{
		`"Bruno"`:          "Bruno",
		`"expr-voice-3-m"`: "expr-voice-3-m",
		`"alloy"`:          "Bella",
		`{"id":"onyx"}`:    "Hugo",
		`{"id":"Leo"}`:     "Leo",
	}
	h, eng := newServer(t)
	for voice, want := range cases {
		rec := speech(t, h, `{"model":"tts-1","input":"Hi.","voice":`+voice+`}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", voice, rec.Code, rec.Body)
		}
		if got := eng.last(t).req.Voice; got != want {
			t.Errorf("voice %s reached the engine as %s, want %s", voice, got, want)
		}
	}
}

func TestUnknownVoice(t *testing.T) {
	h, eng := newServer(t)
	for _, voice := range []string{`"zzz"`, `{"id":"zzz"}`, `{"name":"Leo"}`, `42`} {
		e := openAIError(t, speech(t, h, `{"model":"tts-1","input":"Hi.","voice":`+voice+`}`), http.StatusBadRequest)
		msg := e["message"].(string)
		if e["param"] != "voice" || !strings.Contains(msg, "Bruno") || !strings.Contains(msg, "alloy") {
			t.Errorf("voice %s: error %v, want param voice and a message naming the valid voices", voice, e)
		}
	}
	openAIError(t, speech(t, h, `{"model":"tts-1","input":"Hi."}`), http.StatusBadRequest)
	if n := eng.count(); n != 0 {
		t.Errorf("the engine was called %d times", n)
	}
}

func TestSpeed(t *testing.T) {
	accepted := map[string]float32{
		``:              1,
		`,"speed":1.25`: 1.25,
		`,"speed":0.25`: 0.5, // clamped to the configured 0.5–2.0
		`,"speed":0.4`:  0.5,
		`,"speed":4`:    2,
		`,"speed":3.5`:  2,
	}
	h, eng := newServer(t)
	for speed, want := range accepted {
		rec := speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo"`+speed+`}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: status %d: %s", speed, rec.Code, rec.Body)
		}
		if got := eng.last(t).req.Speed; got != want {
			t.Errorf("%q reached the engine as %v, want %v", speed, got, want)
		}
	}
	for _, speed := range []string{`0.2`, `4.01`, `0`, `-1`, `"fast"`} {
		e := openAIError(t, speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo","speed":`+speed+`}`), http.StatusBadRequest)
		if e["param"] != "speed" && speed != `"fast"` {
			t.Errorf("speed %s: error %v, want param speed", speed, e)
		}
	}
}

func TestInputLength(t *testing.T) {
	h, _ := newServer(t)
	// max_input_chars is 20 in the test config, counted in characters.
	if rec := speech(t, h, `{"model":"tts-1","voice":"Leo","input":"`+strings.Repeat("é", 20)+`"}`); rec.Code != http.StatusOK {
		t.Errorf("20 characters: status %d: %s", rec.Code, rec.Body)
	}
	e := openAIError(t, speech(t, h, `{"model":"tts-1","voice":"Leo","input":"`+strings.Repeat("a", 21)+`"}`), http.StatusBadRequest)
	if e["param"] != "input" {
		t.Errorf("21 characters: error %v, want param input", e)
	}
	openAIError(t, speech(t, h, `{"model":"tts-1","voice":"Leo","input":""}`), http.StatusBadRequest)
	openAIError(t, speech(t, h, `{"model":"tts-1","voice":"Leo"}`), http.StatusBadRequest)
	// The spec allows 1 to max_input_chars characters; whitespace counts.
	if rec := speech(t, h, `{"model":"tts-1","voice":"Leo","input":"   "}`); rec.Code != http.StatusOK {
		t.Errorf("whitespace input: status %d: %s", rec.Code, rec.Body)
	}
}

func TestBodySizeIsCapped(t *testing.T) {
	h, eng := newServer(t)
	// Valid JSON whose input is within max_input_chars, padded with 1 MiB of
	// whitespace: the body must be refused before it is all read.
	body := `{"model":"tts-1","voice":"Leo","input":"Hi."` + strings.Repeat(" ", 1<<20) + `}`
	openAIError(t, speech(t, h, body), http.StatusRequestEntityTooLarge)
	if eng.count() != 0 {
		t.Error("the engine was called")
	}
}

func TestNormalizeAndMarkdownReachTheEngine(t *testing.T) {
	cases := []struct {
		fields              string
		normalize, markdown bool
	}{
		{``, true, true},
		{`,"normalize":false`, false, true},
		{`,"markdown":false`, true, false},
		{`,"normalize":false,"markdown":false`, false, false},
		{`,"normalize":true,"markdown":true`, true, true},
	}
	h, eng := newServer(t)
	for _, c := range cases {
		rec := speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo"`+c.fields+`}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: status %d: %s", c.fields, rec.Code, rec.Body)
		}
		if r := eng.last(t).req; r.Normalize != c.normalize || r.Markdown != c.markdown {
			t.Errorf("%q: normalize %v, markdown %v; want %v, %v", c.fields, r.Normalize, r.Markdown, c.normalize, c.markdown)
		}
	}
}

func TestInputReachesTheEngine(t *testing.T) {
	h, eng := newServer(t)
	if rec := speech(t, h, `{"model":"tts-1","input":"Hi there.","voice":"Leo","instructions":"Speak like a pirate."}`); rec.Code != http.StatusOK {
		t.Fatalf("instructions should be accepted and ignored: status %d: %s", rec.Code, rec.Body)
	}
	if got := eng.last(t).req.Text; got != "Hi there." {
		t.Errorf("text %q", got)
	}
}

func TestSpeechMP3IsTheDefault(t *testing.T) {
	h, eng := newServer(t)
	eng.pcm = make([]float32, kittentts.SampleRate/2)
	for _, format := range []string{``, `,"response_format":"mp3"`} {
		rec := speech(t, h, `{"model":"tts-1","input":"Hello.","voice":"alloy"`+format+`}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
			t.Errorf("Content-Type %q, want audio/mpeg", ct)
		}
		// The encoder pads to whole frames and flushes its delay.
		if n := decodeMP3(t, rec.Body.Bytes()); n < len(eng.pcm) || n > len(eng.pcm)+4*576 {
			t.Errorf("mp3 decodes to %d samples, want about %d", n, len(eng.pcm))
		}
	}
}

func TestFFmpegFormatsWithoutFFmpeg(t *testing.T) {
	disabled := testConfig
	missing := strings.Replace(testConfig, `ffmpeg: ""`, `ffmpeg: /nonexistent/ffmpeg`, 1)
	for name, cfg := range map[string]string{"disabled": disabled, "not found": missing} {
		h, eng := newServerWithConfig(t, cfg)
		for _, format := range []string{"opus", "aac", "flac"} {
			e := openAIError(t, speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo","response_format":"`+format+`"}`), http.StatusBadRequest)
			msg := e["message"].(string)
			if e["param"] != "response_format" || !strings.Contains(msg, "ffmpeg") || !strings.Contains(msg, "mp3, pcm, wav") {
				t.Errorf("%s, %s: error %v, want param response_format naming ffmpeg and mp3, pcm, wav", name, format, e)
			}
		}
		if n := eng.count(); n != 0 {
			t.Errorf("%s: the engine ran %d times for rejected formats", name, n)
		}
	}
}

func TestFFmpegFormats(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	h, eng := newServerWithConfig(t, strings.Replace(testConfig, `ffmpeg: ""`, `ffmpeg: ffmpeg`, 1))
	eng.pcm = make([]float32, kittentts.SampleRate/2)
	for i := range eng.pcm {
		eng.pcm[i] = 0.5 * float32(math.Sin(2*math.Pi*440*float64(i)/kittentts.SampleRate))
	}
	for _, tc := range []struct{ format, contentType, magic string }{
		{"opus", "audio/ogg", "OggS"},
		{"aac", "audio/aac", "\xff"},
		{"flac", "audio/flac", "fLaC"},
	} {
		rec := speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo","response_format":"`+tc.format+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", tc.format, rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != tc.contentType {
			t.Errorf("%s: Content-Type %q, want %s", tc.format, ct, tc.contentType)
		}
		if !bytes.HasPrefix(rec.Body.Bytes(), []byte(tc.magic)) {
			t.Errorf("%s: body starts % x, want %q", tc.format, rec.Body.Bytes()[:min(4, rec.Body.Len())], tc.magic)
		}
		// Playable: ffmpeg decodes it back to about half a second, give or
		// take encoder priming and padding (aac adds up to two 1024-sample frames).
		cmd := exec.Command(ffmpeg, "-v", "error", "-i", "-", "-f", "s16le", "-ac", "1", "-ar", "24000", "-")
		cmd.Stdin = bytes.NewReader(rec.Body.Bytes())
		pcm, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: decoding: %v", tc.format, err)
		}
		if n := len(pcm) / 2; n < len(eng.pcm)*9/10 || n > len(eng.pcm)+2*1024 {
			t.Errorf("%s: decodes to %d samples, want about %d", tc.format, n, len(eng.pcm))
		}
	}
}

func TestUnsupportedFormats(t *testing.T) {
	h, _ := newServer(t)
	e := openAIError(t, speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo","response_format":"ogg"}`), http.StatusBadRequest)
	if msg := e["message"].(string); e["param"] != "response_format" || !strings.Contains(msg, "mp3, pcm, wav") {
		t.Errorf("ogg: error %v, want param response_format naming mp3, pcm, wav", e)
	}
	e = openAIError(t, speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo","stream_format":"chunks"}`), http.StatusBadRequest)
	if msg := e["message"].(string); e["param"] != "stream_format" || !strings.Contains(msg, "audio") || !strings.Contains(msg, "sse") {
		t.Errorf("chunks: error %v, want param stream_format naming audio and sse", e)
	}
}

// flushRecorder records the body sent at each Flush.
type flushRecorder struct {
	*httptest.ResponseRecorder
	pending bytes.Buffer
	flushes [][]byte
}

func (r *flushRecorder) Write(p []byte) (int, error) {
	r.pending.Write(p)
	return r.ResponseRecorder.Write(p)
}

func (r *flushRecorder) Flush() {
	if r.pending.Len() > 0 {
		r.flushes = append(r.flushes, bytes.Clone(r.pending.Bytes()))
		r.pending.Reset()
	}
	r.ResponseRecorder.Flush()
}

func streamSpeech(t *testing.T, h http.Handler, body string) *flushRecorder {
	t.Helper()
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if rec.pending.Len() > 0 {
		t.Errorf("%d bytes were written but never flushed", rec.pending.Len())
	}
	return rec
}

// threeChunks are 0, 0.5 | -0.5 | 1 as 16-bit PCM: 0000 0040 | 00c0 | ff7f.
var threeChunks = []kittentts.Chunk{
	{PCM: []float32{0, 0.5}, Tokens: 5, Frames: 20},
	{PCM: []float32{-0.5}, Tokens: 7, Frames: 30},
	{PCM: []float32{1}, Tokens: 3, Frames: 12},
}

func TestRawStreamingFlushesEachChunk(t *testing.T) {
	for _, streamFormat := range []string{``, `,"stream_format":"audio"`} {
		h, eng := newServer(t)
		eng.chunks = threeChunks
		rec := streamSpeech(t, h, `{"model":"tts-1","input":"A. B. C.","voice":"Leo","response_format":"pcm"`+streamFormat+`}`)
		if got := fmt.Sprintf("%x", rec.flushes); got != "[00000040 00c0 ff7f]" {
			t.Errorf("flushes %s, want [00000040 00c0 ff7f], one per chunk", got)
		}
	}
}

func TestStreamedWAV(t *testing.T) {
	h, eng := newServer(t)
	eng.chunks = threeChunks
	rec := streamSpeech(t, h, `{"model":"tts-1","input":"A. B. C.","voice":"Leo","response_format":"wav"}`)
	if len(rec.flushes) != 3 {
		t.Fatalf("%d flushes, want 3", len(rec.flushes))
	}
	first := rec.flushes[0]
	if len(first) != 44+4 || string(first[:4]) != "RIFF" || string(first[36:40]) != "data" {
		t.Fatalf("first flush %x, want the 44-byte header and the first chunk", first)
	}
	if got := hex.EncodeToString(first[4:8]) + " " + hex.EncodeToString(first[40:44]); got != "ffffffff ffffffff" {
		t.Errorf("RIFF and data sizes %s, want ffffffff ffffffff", got)
	}
	if got := fmt.Sprintf("%x", [][]byte{first[44:], rec.flushes[1], rec.flushes[2]}); got != "[00000040 00c0 ff7f]" {
		t.Errorf("samples %s, want [00000040 00c0 ff7f]", got)
	}
}

func TestStreamedMP3(t *testing.T) {
	h, eng := newServer(t)
	second := make([]float32, kittentts.SampleRate/4)
	eng.chunks = []kittentts.Chunk{{PCM: make([]float32, kittentts.SampleRate/4)}, {PCM: second}}
	rec := streamSpeech(t, h, `{"model":"tts-1","input":"A. B.","voice":"Leo"}`)
	// Each chunk fills at least one frame, so each sends audio as it
	// arrives; the encoder's flush follows.
	if len(rec.flushes) != 3 {
		t.Errorf("%d flushes, want 3 (two chunks and the encoder flush)", len(rec.flushes))
	}
	if n := decodeMP3(t, rec.Body.Bytes()); n < kittentts.SampleRate/2 || n > kittentts.SampleRate/2+4*576 {
		t.Errorf("mp3 decodes to %d samples, want about %d", n, kittentts.SampleRate/2)
	}
}

// decodeMP3 returns the number of samples in an mp3.
func decodeMP3(t *testing.T, b []byte) int {
	t.Helper()
	dec, err := mp3.NewDecoder(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	return len(pcm) / 4 // the decoder always writes 16-bit stereo
}

func TestSSE(t *testing.T) {
	h, eng := newServer(t)
	eng.chunks = threeChunks
	rec := streamSpeech(t, h, `{"model":"tts-1","input":"A. B. C.","voice":"Leo","response_format":"pcm","stream_format":"sse"}`)
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type %q, want text/event-stream", ct)
	}
	if len(rec.flushes) != 4 {
		t.Errorf("%d flushes, want 4 (three deltas and done)", len(rec.flushes))
	}
	events := strings.SplitAfter(rec.Body.String(), "\n\n")
	if events[len(events)-1] == "" {
		events = events[:len(events)-1]
	}
	if len(events) != 4 {
		t.Fatalf("%d events, want 4: %q", len(events), rec.Body)
	}
	for i, want := range []string{"00000040", "00c0", "ff7f"} {
		var delta struct{ Type, Audio string }
		decodeEvent(t, events[i], &delta)
		audio, err := base64.StdEncoding.DecodeString(delta.Audio)
		if delta.Type != "speech.audio.delta" || err != nil || hex.EncodeToString(audio) != want {
			t.Errorf("event %d %q: want a speech.audio.delta with base64 %s", i, events[i], want)
		}
	}
	var done struct {
		Type  string
		Usage map[string]int
	}
	decodeEvent(t, events[3], &done)
	want := map[string]int{"input_tokens": 15, "output_tokens": 62, "total_tokens": 77}
	if done.Type != "speech.audio.done" || !maps.Equal(done.Usage, want) {
		t.Errorf("done event %q: want speech.audio.done with usage %v", events[3], want)
	}
}

func TestSSEWithMP3(t *testing.T) {
	h, eng := newServer(t)
	eng.chunks = []kittentts.Chunk{{PCM: make([]float32, kittentts.SampleRate/2)}}
	rec := streamSpeech(t, h, `{"model":"tts-1","input":"A.","voice":"Leo","stream_format":"sse"}`)
	var mp3Bytes []byte
	for _, ev := range strings.SplitAfter(strings.TrimSuffix(rec.Body.String(), "\n\n"), "\n\n") {
		if !strings.HasSuffix(ev, "\n\n") {
			ev += "\n\n" // the last event, trimmed above
		}
		var e struct{ Type, Audio string }
		decodeEvent(t, ev, &e)
		if e.Type == "speech.audio.delta" {
			b, err := base64.StdEncoding.DecodeString(e.Audio)
			if err != nil {
				t.Fatal(err)
			}
			mp3Bytes = append(mp3Bytes, b...)
		}
	}
	if n := decodeMP3(t, mp3Bytes); n < kittentts.SampleRate/2 {
		t.Errorf("the deltas decode to %d samples, want at least %d", n, kittentts.SampleRate/2)
	}
}

func decodeEvent(t *testing.T, event string, v any) {
	t.Helper()
	data, ok := strings.CutPrefix(event, "data: ")
	if !ok || !strings.HasSuffix(data, "\n\n") {
		t.Fatalf("event %q is not one data line and a blank line", event)
	}
	if err := json.Unmarshal([]byte(data), v); err != nil {
		t.Fatalf("event %q: %v", event, err)
	}
}

func TestDisconnectStopsTheEngine(t *testing.T) {
	h, eng := newServer(t)
	eng.chunks = slices.Repeat([]kittentts.Chunk{{PCM: []float32{0.5}}}, 10)
	eng.gate = make(chan struct{})
	eng.finished = make(chan struct{})
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer close(eng.gate) // runs first, so a failing test can't leave the handler blocked

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/audio/speech",
		strings.NewReader(`{"model":"tts-1","input":"Hi.","voice":"Leo","response_format":"pcm"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	first := make([]byte, 2)
	if _, err := io.ReadFull(resp.Body, first); err != nil {
		t.Fatalf("reading the first chunk: %v", err)
	}
	cancel()
	resp.Body.Close()

	// Once the server sees the disconnect, let the in-flight chunk finish.
	select {
	case <-eng.last(t).ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the engine's context was not cancelled after the client disconnected")
	}
	eng.gate <- struct{}{}
	select {
	case <-eng.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream kept going after the client disconnected")
	}
	if n := eng.producedCount(); n != 2 {
		t.Errorf("the engine produced %d chunks, want 2 (the first and the in-flight one)", n)
	}
}

func TestEngineFailureMidStream(t *testing.T) {
	h, eng := newServer(t)
	eng.chunks = threeChunks[:1]
	eng.err = errors.New("onnxruntime exploded")
	assertCutOff(t, h, `{"model":"tts-1","input":"A. B.","voice":"Leo","response_format":"pcm"}`)
}

func TestEncoderFailureMidStream(t *testing.T) {
	// "false" stands in for an ffmpeg that dies: it exits without reading.
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("no false binary")
	}
	h, eng := newServerWithConfig(t, strings.Replace(testConfig, `ffmpeg: ""`, `ffmpeg: "false"`, 1))
	eng.chunks = threeChunks
	assertCutOff(t, h, `{"model":"tts-1","input":"A. B. C.","voice":"Leo","response_format":"opus"}`)
}

// assertCutOff checks that a request starts a 200 stream that is then cut
// off rather than ending cleanly, so the client can't mistake it for
// complete audio.
func assertCutOff(t *testing.T, h http.Handler, body string) {
	t.Helper()
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v1/audio/speech", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200 (the failure comes after the stream starts)", resp.StatusCode)
	}
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Error("the body ended cleanly, want an error")
	}
}

func TestMalformedRequest(t *testing.T) {
	h, _ := newServer(t)
	for _, body := range []string{``, `{`, `[]`, `{"model":1}`, `{"model":"tts-1","input":"Hi.","voice":"Leo"} trailing`, `{"model":"tts-1","input":"Hi.","voice":"Leo"}{}`} {
		openAIError(t, speech(t, h, body), http.StatusBadRequest)
	}
}

func TestEngineFailure(t *testing.T) {
	h, eng := newServer(t)
	eng.err = errors.New("onnxruntime exploded")
	e := openAIError(t, speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo"}`), http.StatusInternalServerError)
	if e["type"] != "server_error" {
		t.Errorf("error %v, want type server_error", e)
	}
}

func TestVoices(t *testing.T) {
	h, _ := newServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/voices", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Voices []struct{ Name, Key string } `json:"voices"`
		OpenAI map[string]string            `json:"openai"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: %v", rec.Body, err)
	}
	if len(body.Voices) != 8 {
		t.Fatalf("got %d voices, want 8: %s", len(body.Voices), rec.Body)
	}
	found := false
	for _, v := range body.Voices {
		found = found || v.Name == "Bruno" && v.Key == "expr-voice-3-m"
	}
	if !found {
		t.Errorf("Bruno / expr-voice-3-m missing: %s", rec.Body)
	}
	if body.OpenAI["alloy"] != "Bella" || len(body.OpenAI) != 13 {
		t.Errorf("OpenAI map %v", body.OpenAI)
	}
}

// queueConfig gives mini a queue of one and the given request timeout.
func queueConfig(timeout string) string {
	return strings.NewReplacer(
		"kitten-tts-mini-0.8: {}", "kitten-tts-mini-0.8: { max_queue: 1 }",
		"max_input_chars: 20", "max_input_chars: 20\n  request_timeout: "+timeout,
	).Replace(testConfig)
}

// serveInBackground serves a speech request on another goroutine.
func serveInBackground(h http.Handler, ctx context.Context, body string) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/audio/speech", strings.NewReader(body)))
		done <- rec
	}()
	return done
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func receiveResponse(t *testing.T, what string, c <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case rec := <-c:
		return rec
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for the %s", what)
		return nil
	}
}

const pcmRequest = `{"model":"tts-1","input":"Hi.","voice":"Leo","response_format":"pcm"}`

// assertQueueOfOne checks through HTTP alone that mini, idle and configured
// by queueConfig with a short timeout, runs one request, queues exactly one
// more and turns the rest away with 429. The running request is stuck in a
// run that ignores its timeout, so of two more requests, whichever order
// they arrive in, one takes the queue place and times out (503) and the
// other is rejected (429). Afterwards the model serves requests again.
func assertQueueOfOne(t *testing.T, h http.Handler, eng *fakeEngine) {
	t.Helper()
	stuck := &stall{release: make(chan struct{}), ignoresContext: true}
	eng.setStall(stuck)
	calls := eng.count()
	running := serveInBackground(h, context.Background(), pcmRequest)
	waitFor(t, "a request to reach the engine", func() bool { return eng.count() == calls+1 })

	b := serveInBackground(h, context.Background(), pcmRequest)
	c := serveInBackground(h, context.Background(), pcmRequest)
	codes := map[int]int{}
	for _, rec := range []*httptest.ResponseRecorder{receiveResponse(t, "response", b), receiveResponse(t, "response", c)} {
		codes[rec.Code]++
		switch rec.Code {
		case http.StatusTooManyRequests:
			check429(t, rec)
		case http.StatusServiceUnavailable:
			checkTimeout(t, "queued", rec)
		}
	}
	if codes[http.StatusTooManyRequests] != 1 || codes[http.StatusServiceUnavailable] != 1 {
		t.Errorf("statuses %v, want one 429 (rejected) and one 503 (queued, then timed out)", codes)
	}
	if n := eng.count(); n != calls+1 {
		t.Errorf("the engine was called %d times while the model was busy, want %d", n, calls+1)
	}

	close(stuck.release)
	checkTimeout(t, "stuck", receiveResponse(t, "stuck response", running))
	eng.setStall(nil)
	if rec := speech(t, h, pcmRequest); rec.Code != http.StatusOK {
		t.Errorf("after the queue drained: status %d: %s", rec.Code, rec.Body)
	}
}

func check429(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	openAIError(t, rec, http.StatusTooManyRequests)
	if ra := rec.Header().Get("Retry-After"); ra != "1" {
		t.Errorf("Retry-After %q, want 1", ra)
	}
}

func checkTimeout(t *testing.T, name string, rec *httptest.ResponseRecorder) {
	t.Helper()
	e := openAIError(t, rec, http.StatusServiceUnavailable)
	if msg := e["message"].(string); !strings.Contains(msg, "timed out") {
		t.Errorf("%s request: error %v, want a timeout message", name, e)
	}
}

func TestFullQueueReturns429(t *testing.T) {
	h, eng := newServerWithConfig(t, queueConfig("100ms"))
	assertQueueOfOne(t, h, eng)
}

func TestQueuesArePerModel(t *testing.T) {
	h, eng := newServerWithConfig(t, strings.Replace(queueConfig("1m"), "max_queue: 1", "max_queue: 0", 1))
	stuck := &stall{release: make(chan struct{}), ignoresContext: true}
	eng.setStall(stuck)
	running := serveInBackground(h, context.Background(), pcmRequest)
	waitFor(t, "the first request to reach the engine", func() bool { return eng.count() == 1 })
	check429(t, speech(t, h, pcmRequest))

	// tts-1-hd is nano, which is idle.
	eng.setStall(nil)
	if rec := speech(t, h, strings.Replace(pcmRequest, "tts-1", "tts-1-hd", 1)); rec.Code != http.StatusOK {
		t.Errorf("another model: status %d: %s", rec.Code, rec.Body)
	}
	close(stuck.release)
	receiveResponse(t, "running response", running)
}

func TestRequestTimeout(t *testing.T) {
	h, eng := newServerWithConfig(t, queueConfig("100ms"))

	// A stuck run and a waiting request both time out (assertQueueOfOne),
	// and so does an engine that honours the timeout.
	assertQueueOfOne(t, h, eng)
	eng.setStall(&stall{release: make(chan struct{})})
	checkTimeout(t, "cancelled", speech(t, h, pcmRequest))

	// None of them kept a place.
	eng.setStall(nil)
	assertQueueOfOne(t, h, eng)
}

func TestRequestTimeoutMidStream(t *testing.T) {
	h, eng := newServerWithConfig(t, queueConfig("100ms"))
	eng.chunks = threeChunks
	eng.setStall(&stall{release: make(chan struct{}), at: 1})
	assertCutOff(t, h, `{"model":"tts-1","input":"A. B. C.","voice":"Leo","response_format":"pcm"}`)
}

func TestDisconnectWhileQueuedFreesThePlace(t *testing.T) {
	h, eng := newServerWithConfig(t, queueConfig("100ms"))
	stuck := &stall{release: make(chan struct{}), ignoresContext: true}
	eng.setStall(stuck)
	running := serveInBackground(h, context.Background(), pcmRequest)
	waitFor(t, "the first request to reach the engine", func() bool { return eng.count() == 1 })

	// This client leaves while the model is busy, so it never runs.
	ctx, cancel := context.WithCancel(context.Background())
	left := serveInBackground(h, ctx, pcmRequest)
	cancel()
	receiveResponse(t, "disconnected response", left)
	close(stuck.release)
	receiveResponse(t, "running response", running)
	if n := eng.count(); n != 1 {
		t.Errorf("the engine was called %d times, want 1 (the client that left never ran)", n)
	}

	// It kept no place.
	eng.setStall(nil)
	assertQueueOfOne(t, h, eng)
}

func TestAuth(t *testing.T) {
	h, _ := newServerWithEnv(t, testConfig, map[string]string{"KITTEN_API_KEY": "s3cret"})
	for _, c := range []struct {
		name, method, path, auth string
		want                     int
	}{
		{"speech without a key", http.MethodPost, "/v1/audio/speech", "", http.StatusUnauthorized},
		{"speech with a wrong key", http.MethodPost, "/v1/audio/speech", "Bearer nope", http.StatusUnauthorized},
		{"speech with a longer key", http.MethodPost, "/v1/audio/speech", "Bearer s3cret2", http.StatusUnauthorized},
		{"speech with the key as Basic", http.MethodPost, "/v1/audio/speech", "Basic s3cret", http.StatusUnauthorized},
		{"speech with the key", http.MethodPost, "/v1/audio/speech", "Bearer s3cret", http.StatusOK},
		{"the scheme is case-insensitive", http.MethodPost, "/v1/audio/speech", "bearer s3cret", http.StatusOK},
		{"voices without a key", http.MethodGet, "/v1/voices", "", http.StatusUnauthorized},
		{"voices with the key", http.MethodGet, "/v1/voices", "Bearer s3cret", http.StatusOK},
		{"unknown /v1 path without a key", http.MethodGet, "/v1/nothing", "", http.StatusUnauthorized},
		{"healthz without a key", http.MethodGet, "/healthz", "", http.StatusOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			var body io.Reader
			if c.method == http.MethodPost {
				body = strings.NewReader(pcmRequest)
			}
			req := httptest.NewRequest(c.method, c.path, body)
			if c.auth != "" {
				req.Header.Set("Authorization", c.auth)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if c.want == http.StatusUnauthorized {
				openAIError(t, rec, http.StatusUnauthorized)
				if rec.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Errorf("WWW-Authenticate %q, want Bearer", rec.Header().Get("WWW-Authenticate"))
				}
			} else if rec.Code != c.want {
				t.Errorf("status %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	h, _ := newServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestOpenServerWarning(t *testing.T) {
	for _, c := range []struct {
		listen, key string
		warn        bool
	}{
		{":8880", "", true},
		{"0.0.0.0:8880", "", true},
		{"[::]:8880", "", true},
		{"192.168.1.10:8880", "", true},
		{"tts.example.com:8880", "", true},
		{"127.0.0.1:8880", "", false},
		{"127.1.2.3:8880", "", false},
		{"[::1]:8880", "", false},
		{"localhost:8880", "", false},
		{":8880", "s3cret", false},
	} {
		var logs bytes.Buffer
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
		newServerWithEnv(t, testConfig, map[string]string{"KITTEN_LISTEN": c.listen, "KITTEN_API_KEY": c.key})
		slog.SetDefault(old)
		if got := strings.Contains(logs.String(), "KITTEN_API_KEY"); got != c.warn {
			t.Errorf("listen %q, key %q: warned %v, want %v; logs: %s", c.listen, c.key, got, c.warn, logs.String())
		}
	}
}
