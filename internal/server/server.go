// Package server is the OpenAI-compatible HTTP API.
package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/androiddrew/gokittentts/internal/audio"
	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/kittentts"
)

// Engine streams a request's audio with the named model, one chunk at a time.
type Engine interface {
	Stream(ctx context.Context, model string, r kittentts.Request) iter.Seq2[kittentts.Chunk, error]
}

// KittenEngine adapts *kittentts.Engine to Engine.
type KittenEngine struct{ *kittentts.Engine }

// Stream loads the model if needed and streams r.
func (e KittenEngine) Stream(ctx context.Context, model string, r kittentts.Request) iter.Seq2[kittentts.Chunk, error] {
	m, err := e.Model(model)
	if err != nil {
		return func(yield func(kittentts.Chunk, error) bool) { yield(kittentts.Chunk{}, err) }
	}
	return m.Stream(ctx, r)
}

// OpenAI accepts speeds in this range; anything else is a 400.
const (
	minSpeed = 0.25
	maxSpeed = 4.0
)

// format is a response format.
type format struct {
	contentType string
	ffmpeg      bool // encoded by piping through ffmpeg
}

var formats = map[string]format{
	"mp3":  {"audio/mpeg", false},
	"wav":  {"audio/wav", false},
	"pcm":  {"audio/pcm", false},
	"opus": {"audio/ogg", true},
	"aac":  {"audio/aac", true},
	"flac": {"audio/flac", true},
}

// defaultFormat is OpenAI's default.
const defaultFormat = "mp3"

type server struct {
	cfg    *config.Config
	engine Engine
	ffmpeg string            // resolved path, or empty when ffmpeg is disabled or missing
	queues map[string]*queue // by model name
}

// New returns the HTTP handler for cfg, synthesizing with engine. If cfg's
// ffmpeg can't be found, opus, aac and flac are rejected. With an API key
// set, /v1/* requires it; /healthz is always open.
func New(cfg *config.Config, engine Engine) http.Handler {
	s := &server{cfg: cfg, engine: engine, queues: make(map[string]*queue, len(cfg.Models))}
	for name, m := range cfg.Models {
		s.queues[name] = newQueue(m.QueueSize())
	}
	if cfg.APIKey == "" && !isLoopback(cfg.Listen) {
		slog.Warn("auth is off and the server listens beyond loopback; set KITTEN_API_KEY to require a key", "listen", cfg.Listen)
	}
	if cfg.FFmpeg != "" {
		// LookPath can return a path alongside exec.ErrDot; that path is not used.
		if path, err := exec.LookPath(cfg.FFmpeg); err != nil {
			slog.Warn("ffmpeg not found; opus, aac and flac are disabled", "ffmpeg", cfg.FFmpeg, "err", err)
		} else {
			s.ffmpeg = path
		}
	}
	api := http.NewServeMux()
	api.HandleFunc("POST /v1/audio/speech", s.speech)
	api.HandleFunc("GET /v1/voices", s.voices)
	mux := http.NewServeMux()
	mux.Handle("/v1/", s.requireKey(api))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

// requireKey wraps next to require "Authorization: Bearer <key>" when an
// API key is configured. Keys are compared as SHA-256 digests in constant
// time, so neither their contents nor their length leaks.
func (s *server) requireKey(next http.Handler) http.Handler {
	if s.cfg.APIKey == "" {
		return next
	}
	want := sha256.Sum256([]byte(s.cfg.APIKey))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, key, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		got := sha256.Sum256([]byte(key))
		if !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, invalidRequest, "", "missing or invalid API key; send Authorization: Bearer <key>")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopback reports whether a listen address only accepts local
// connections. An empty host listens on every interface.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// queue lets one request at a time run on a model, with up to a fixed
// number waiting behind it.
type queue struct {
	admitted chan struct{} // a token per running or waiting request
	running  chan struct{} // a token for the running request
}

func newQueue(size int) *queue {
	return &queue{admitted: make(chan struct{}, 1+size), running: make(chan struct{}, 1)}
}

var errQueueFull = errors.New("queue full")

// enter admits a request, or returns errQueueFull, then waits for its turn
// until ctx ends. On success the caller must call release when done.
func (q *queue) enter(ctx context.Context) (release func(), err error) {
	select {
	case q.admitted <- struct{}{}:
	default:
		return nil, errQueueFull
	}
	select {
	case q.running <- struct{}{}:
		return func() { <-q.running; <-q.admitted }, nil
	case <-ctx.Done():
		<-q.admitted
		return nil, ctx.Err()
	}
}

// speechRequest is OpenAI's CreateSpeechRequest plus normalize and markdown.
type speechRequest struct {
	Model          string          `json:"model"`
	Input          *string         `json:"input"`
	Voice          json.RawMessage `json:"voice"` // a string, or an object with an id
	ResponseFormat string          `json:"response_format"`
	Speed          *float64        `json:"speed"`
	StreamFormat   string          `json:"stream_format"`
	Instructions   string          `json:"instructions"` // accepted and ignored
	Normalize      *bool           `json:"normalize"`
	Markdown       *bool           `json:"markdown"`
}

func (s *server) speech(w http.ResponseWriter, r *http.Request) {
	var req speechRequest
	if status, msg := s.decode(w, r, &req); status != 0 {
		writeError(w, status, invalidRequest, "", msg)
		return
	}
	if req.Instructions != "" {
		slog.Debug("ignoring instructions", "instructions", req.Instructions)
	}

	model, ok := s.resolveModel(req.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, invalidRequest, "model",
			fmt.Sprintf("unknown model %q; configured models are %s and aliases %s",
				req.Model, strings.Join(sortedKeys(s.cfg.Models), ", "), strings.Join(sortedKeys(s.cfg.ModelAliases), ", ")))
		return
	}

	if req.Input == nil || *req.Input == "" {
		writeError(w, http.StatusBadRequest, invalidRequest, "input", "input is required")
		return
	}
	if n := utf8.RuneCountInString(*req.Input); n > s.cfg.Limits.MaxInputChars {
		writeError(w, http.StatusBadRequest, invalidRequest, "input",
			fmt.Sprintf("input is %d characters; the limit is %d", n, s.cfg.Limits.MaxInputChars))
		return
	}

	voice, ok := s.resolveVoice(req.Voice)
	if !ok {
		writeError(w, http.StatusBadRequest, invalidRequest, "voice", s.unknownVoiceMessage(req.Voice))
		return
	}

	format := req.ResponseFormat
	if format == "" {
		format = defaultFormat
	}
	f, ok := formats[format]
	if !ok || f.ffmpeg && s.ffmpeg == "" {
		reason := "is not supported"
		if ok {
			reason = "needs ffmpeg, which is not available"
		}
		writeError(w, http.StatusBadRequest, invalidRequest, "response_format",
			fmt.Sprintf("response_format %q %s; supported formats are %s", format, reason, strings.Join(s.supportedFormats(), ", ")))
		return
	}

	speed := 1.0
	if req.Speed != nil {
		speed = *req.Speed
	}
	if speed < minSpeed || speed > maxSpeed {
		writeError(w, http.StatusBadRequest, invalidRequest, "speed",
			fmt.Sprintf("speed %v is outside %v to %v", speed, minSpeed, maxSpeed))
		return
	}

	switch req.StreamFormat {
	case "", "audio", "sse":
	default:
		writeError(w, http.StatusBadRequest, invalidRequest, "stream_format",
			fmt.Sprintf("stream_format %q is not supported; use audio or sse", req.StreamFormat))
		return
	}

	// The timeout covers the queue wait and synthesis.
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Limits.RequestTimeout)
	defer cancel()
	release, err := s.queues[model].enter(ctx)
	if errors.Is(err, errQueueFull) {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, rateLimit, "",
			fmt.Sprintf("model %s is busy and its queue is full; retry shortly", model))
		return
	}
	if err != nil {
		s.writeContextError(w, r, model, err)
		return
	}
	defer release()

	// Pull the first chunk before sending headers, so a failure that comes
	// before any audio is still an error response.
	next, stop := iter.Pull2(s.engine.Stream(ctx, model, kittentts.Request{
		Text:      *req.Input,
		Voice:     voice,
		Speed:     max(s.cfg.Speed.Min, min(s.cfg.Speed.Max, float32(speed))),
		Normalize: req.Normalize == nil || *req.Normalize,
		Markdown:  req.Markdown == nil || *req.Markdown,
	}))
	defer stop()
	chunk, err, more := next()
	if ctx.Err() != nil {
		s.writeContextError(w, r, model, ctx.Err())
		return
	}
	if err != nil {
		slog.Error("synthesis failed", "model", model, "err", err)
		writeError(w, http.StatusInternalServerError, serverError, "", "synthesis failed: "+err.Error())
		return
	}

	sink := &flushWriter{w: w, rc: http.NewResponseController(w)}
	var out io.Writer = sink
	var sse *sseWriter
	if req.StreamFormat == "sse" {
		sse = &sseWriter{sink}
		out = sse
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Content-Type", f.contentType)
	}
	// The headers go first: ffmpeg's output is copied to the response from
	// another goroutine as soon as it starts.
	w.WriteHeader(http.StatusOK)
	sink.rc.Flush()
	enc, err := s.encoder(ctx, out, format)
	if err != nil {
		slog.Error("starting encoder", "format", format, "err", err)
		panic(http.ErrAbortHandler)
	}

	var total usage
	for ; more; chunk, err, more = next() {
		if err == nil {
			total.InputTokens += chunk.Tokens
			total.OutputTokens += chunk.Frames
			err = enc.Write(chunk.PCM)
		}
		if err == nil {
			// A disconnect or the timeout ends the context; don't start another run.
			err = ctx.Err()
		}
		if err != nil {
			// Drop the encoder's tail, but still reap it.
			sink.discard.Store(true)
			enc.Close()
			if r.Context().Err() != nil {
				slog.Info("client disconnected", "model", model, "err", err)
				return
			}
			slog.Error("stream failed", "model", model, "format", format, "err", err)
			panic(http.ErrAbortHandler) // cut the response off so it can't pass for complete audio
		}
	}
	if err := enc.Close(); err != nil {
		slog.Error("encoding failed", "format", format, "err", err)
		panic(http.ErrAbortHandler)
	}
	if sse != nil {
		total.TotalTokens = total.InputTokens + total.OutputTokens
		sse.event(map[string]any{"type": "speech.audio.done", "usage": total})
	}
}

// writeContextError answers a request whose context ended before any audio
// was sent: nothing for a client that has gone, 503 for the timeout.
func (s *server) writeContextError(w http.ResponseWriter, r *http.Request, model string, err error) {
	if r.Context().Err() != nil {
		slog.Info("client disconnected", "model", model, "err", err)
		return
	}
	slog.Warn("request timed out", "model", model, "timeout", s.cfg.Limits.RequestTimeout)
	writeError(w, http.StatusServiceUnavailable, serverError, "",
		fmt.Sprintf("request timed out after %v waiting for or running model %s", s.cfg.Limits.RequestTimeout, model))
}

// encoder returns the Encoder for format, writing to w.
func (s *server) encoder(ctx context.Context, w io.Writer, format string) (audio.Encoder, error) {
	switch format {
	case "mp3":
		return audio.NewMP3Encoder(w, kittentts.SampleRate)
	case "wav":
		return audio.NewWAVStreamEncoder(w, kittentts.SampleRate), nil
	case "pcm":
		return audio.NewPCMEncoder(w), nil
	}
	if formats[format].ffmpeg {
		return audio.NewFFmpegEncoder(ctx, w, s.ffmpeg, kittentts.SampleRate, format)
	}
	return nil, fmt.Errorf("no encoder for %q", format)
}

// usage is the speech.audio.done usage: input tokens are the token ids the
// model ran on, and output tokens are its summed durations.
type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// flushWriter flushes the response after every Write. Once discard is set,
// it drops writes but reports success, so an encoder can still drain.
type flushWriter struct {
	w       io.Writer
	rc      *http.ResponseController
	discard atomic.Bool
}

func (f *flushWriter) Write(p []byte) (int, error) {
	if f.discard.Load() {
		return len(p), nil
	}
	n, err := f.w.Write(p)
	if err != nil {
		return n, err
	}
	return n, f.rc.Flush()
}

// sseWriter sends each Write as a speech.audio.delta event.
type sseWriter struct{ w *flushWriter }

func (s *sseWriter) Write(p []byte) (int, error) {
	if err := s.event(map[string]string{"type": "speech.audio.delta", "audio": base64.StdEncoding.EncodeToString(p)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *sseWriter) event(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.w.Write(fmt.Appendf(nil, "data: %s\n\n", b))
	return err
}

// supportedFormats lists the response formats this server can produce.
func (s *server) supportedFormats() []string {
	var names []string
	for _, name := range sortedKeys(formats) {
		if !formats[name].ffmpeg || s.ffmpeg != "" {
			names = append(names, name)
		}
	}
	return names
}

// decode reads exactly one JSON object from a body capped at a size that
// always fits max_input_chars characters, even when every one is escaped.
// It returns a non-zero status and a message on failure.
func (s *server) decode(w http.ResponseWriter, r *http.Request, req *speechRequest) (int, string) {
	limit := int64(64<<10 + 12*s.cfg.Limits.MaxInputChars) // \uXXXX\uXXXX is 12 bytes
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	err := dec.Decode(req)
	if err == nil {
		if err = dec.Decode(&struct{}{}); err == io.EOF {
			return 0, ""
		} else if err == nil {
			err = errors.New("more than one JSON value")
		}
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return http.StatusRequestEntityTooLarge, fmt.Sprintf("request body is larger than %d bytes", limit)
	}
	return http.StatusBadRequest, "invalid JSON body: " + err.Error()
}

// resolveModel maps a configured model name, or an alias, to a model name.
func (s *server) resolveModel(name string) (string, bool) {
	if _, ok := s.cfg.Models[name]; ok {
		return name, true
	}
	target, ok := s.cfg.ModelAliases[name]
	if !ok {
		return "", false
	}
	if target == config.DefaultAlias {
		return s.cfg.DefaultModel, true
	}
	return target, true
}

// resolveVoice resolves a voice, given as a string or an object with an id,
// as a Kitten name, then a voices.npz key, then an OpenAI voice from the
// config's map.
func (s *server) resolveVoice(raw json.RawMessage) (string, bool) {
	name, ok := voiceName(raw)
	if !ok {
		return "", false
	}
	for _, v := range kittentts.Voices {
		if name == v.Name || name == v.Key {
			return name, true
		}
	}
	kitten, ok := s.cfg.Voices[name]
	return kitten, ok
}

func voiceName(raw json.RawMessage) (string, bool) {
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		return name, true
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.ID != "" {
		return obj.ID, true
	}
	return "", false
}

func (s *server) unknownVoiceMessage(raw json.RawMessage) string {
	var names []string
	for _, v := range kittentts.Voices {
		names = append(names, v.Name)
	}
	slices.Sort(names)
	given := "missing voice"
	if len(raw) > 0 {
		given = "unknown voice " + string(raw)
	}
	return fmt.Sprintf("%s; valid voices are %s, their expr-voice-* keys, or %s",
		given, strings.Join(names, ", "), strings.Join(sortedKeys(s.cfg.Voices), ", "))
}

type voiceInfo struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func (s *server) voices(w http.ResponseWriter, _ *http.Request) {
	list := make([]voiceInfo, 0, len(kittentts.Voices))
	for _, v := range kittentts.Voices {
		list = append(list, voiceInfo{v.Name, v.Key})
	}
	writeJSON(w, http.StatusOK, struct {
		Voices []voiceInfo       `json:"voices"`
		OpenAI map[string]string `json:"openai"`
	}{list, s.cfg.Voices})
}

// OpenAI error types.
const (
	invalidRequest = "invalid_request_error"
	rateLimit      = "rate_limit_exceeded"
	serverError    = "server_error"
)

// writeError writes OpenAI's {"error": {message, type, param, code}}. An
// empty param is written as null; code is always null.
func writeError(w http.ResponseWriter, status int, typ, param, message string) {
	var p *string
	if param != "" {
		p = &param
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"message": message,
		"type":    typ,
		"param":   p,
		"code":    nil,
	}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writing response", "err", err)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
