package server_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/internal/server"
	"github.com/androiddrew/gokittentts/kittentts"
)

// fakeEngine records what reaches it and returns fixed PCM.
type fakeEngine struct {
	mu    sync.Mutex
	calls []call
	pcm   []float32
	err   error
}

type call struct {
	model string
	req   kittentts.Request
}

func (f *fakeEngine) Synthesize(_ context.Context, model string, r kittentts.Request) ([]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{model, r})
	return f.pcm, f.err
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
`

func newServer(t *testing.T) (http.Handler, *fakeEngine) {
	t.Helper()
	cfg, err := config.Parse([]byte(testConfig), func(string) string { return "" })
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
	for _, format := range []string{`,"response_format":"wav"`, ``} { // wav is the default until mp3 lands
		rec := speech(t, h, `{"model":"tts-1","input":"Hello.","voice":"alloy"`+format+`}`)
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
		if got := hex.EncodeToString(b[22:24]) + hex.EncodeToString(b[24:28]) + hex.EncodeToString(b[34:36]); got != "0100"+"c05d0000"+"1000" {
			t.Errorf("channels, rate, bits = %s, want mono, 24000 Hz, 16-bit", got)
		}
		if got := hex.EncodeToString(b[44:]); got != "0000004000c0" {
			t.Errorf("samples %s, want 0000004000c0", got)
		}
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

func TestUnsupportedFormats(t *testing.T) {
	h, _ := newServer(t)
	for _, format := range []string{"mp3", "opus", "aac", "flac", "ogg"} {
		e := openAIError(t, speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo","response_format":"`+format+`"}`), http.StatusBadRequest)
		if msg := e["message"].(string); e["param"] != "response_format" || !strings.Contains(msg, "wav") || !strings.Contains(msg, "pcm") {
			t.Errorf("%s: error %v, want param response_format naming wav and pcm", format, e)
		}
	}
	e := openAIError(t, speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo","stream_format":"sse"}`), http.StatusBadRequest)
	if e["param"] != "stream_format" {
		t.Errorf("sse: error %v", e)
	}
	if rec := speech(t, h, `{"model":"tts-1","input":"Hi.","voice":"Leo","stream_format":"audio"}`); rec.Code != http.StatusOK {
		t.Errorf("stream_format audio: status %d: %s", rec.Code, rec.Body)
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
