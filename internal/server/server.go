// Package server is the OpenAI-compatible HTTP API.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/androiddrew/gokittentts/internal/audio"
	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/kittentts"
)

// Engine synthesizes a request with the named model.
type Engine interface {
	Synthesize(ctx context.Context, model string, r kittentts.Request) ([]float32, error)
}

// KittenEngine adapts *kittentts.Engine to Engine.
type KittenEngine struct{ *kittentts.Engine }

// Synthesize loads the model if needed and synthesizes r.
func (e KittenEngine) Synthesize(ctx context.Context, model string, r kittentts.Request) ([]float32, error) {
	m, err := e.Model(model)
	if err != nil {
		return nil, err
	}
	return m.Synthesize(ctx, r)
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
	ffmpeg string // resolved path, or empty when ffmpeg is disabled or missing
}

// New returns the HTTP handler for cfg, synthesizing with engine. If cfg's
// ffmpeg can't be found, opus, aac and flac are rejected.
func New(cfg *config.Config, engine Engine) http.Handler {
	s := &server{cfg: cfg, engine: engine}
	if cfg.FFmpeg != "" {
		// LookPath can return a path alongside exec.ErrDot; that path is not used.
		if path, err := exec.LookPath(cfg.FFmpeg); err != nil {
			slog.Warn("ffmpeg not found; opus, aac and flac are disabled", "ffmpeg", cfg.FFmpeg, "err", err)
		} else {
			s.ffmpeg = path
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/audio/speech", s.speech)
	mux.HandleFunc("GET /v1/voices", s.voices)
	return mux
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
	case "", "audio":
	default:
		writeError(w, http.StatusBadRequest, invalidRequest, "stream_format",
			fmt.Sprintf("stream_format %q is not supported; use audio", req.StreamFormat))
		return
	}

	pcm, err := s.engine.Synthesize(r.Context(), model, kittentts.Request{
		Text:      *req.Input,
		Voice:     voice,
		Speed:     max(s.cfg.Speed.Min, min(s.cfg.Speed.Max, float32(speed))),
		Normalize: req.Normalize == nil || *req.Normalize,
		Markdown:  req.Markdown == nil || *req.Markdown,
	})
	if err != nil {
		slog.Error("synthesis failed", "model", model, "err", err)
		writeError(w, http.StatusInternalServerError, serverError, "", "synthesis failed: "+err.Error())
		return
	}

	var buf bytes.Buffer
	switch {
	case format == "mp3":
		err = audio.WriteMP3(&buf, pcm, kittentts.SampleRate)
	case format == "wav":
		err = audio.WriteWAV(&buf, pcm, kittentts.SampleRate)
	case format == "pcm":
		buf.Write(audio.PCM16(pcm))
	case f.ffmpeg:
		err = audio.EncodeFFmpeg(r.Context(), &buf, s.ffmpeg, pcm, kittentts.SampleRate, format)
	}
	if err != nil {
		slog.Error("encoding failed", "format", format, "err", err)
		writeError(w, http.StatusInternalServerError, serverError, "", "encoding "+format+" failed: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", f.contentType)
	w.Write(buf.Bytes())
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
