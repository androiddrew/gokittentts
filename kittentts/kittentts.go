// Package kittentts runs the KittenTTS 0.8 models: text in, 24 kHz mono
// float32 PCM out. It owns the whole pipeline (chunking, espeak-ng
// phonemization, tokenization, voice styles and the ONNX Runtime run) and has
// no queue, auth or audio encoding.
package kittentts

import (
	"errors"
	"fmt"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/androiddrew/gokittentts/internal/phonemize"
	"github.com/androiddrew/gokittentts/internal/phonemize/espeak"
)

// SampleRate is the sample rate of all PCM the models produce.
const SampleRate = 24000

// ORTVersion is the ONNX Runtime release onnxruntime_go v1.36.0 is built for.
const ORTVersion = "1.29.1"

// Device is where a model runs.
type Device string

// CPU is the only device until CUDA support lands.
const CPU Device = "cpu"

// ModelConfig names a model and the directory holding its config.json,
// .onnx file and voices.npz.
type ModelConfig struct {
	Name           string // e.g. "kitten-tts-mini-0.8"
	Dir            string
	Device         Device // "" means CPU
	CUDADeviceID   int
	IntraOpThreads int // 0 = ONNX Runtime default
}

// Request is one text to synthesize.
type Request struct {
	Text  string
	Voice string  // Kitten name (e.g. "Bruno") or voices.npz key (e.g. "expr-voice-3-m")
	Speed float32 // already clamped by the caller; 0 means 1.0
	// Normalize and Markdown turn on the normalizer and the markdown pass.
	// Neither is implemented yet, so both are ignored.
	Normalize bool
	Markdown  bool
}

// Engine is a registry of models sharing one ONNX Runtime environment and one
// phonemizer. ONNX Runtime's environment is process-wide, so a process has at
// most one open Engine.
type Engine struct {
	phonemizer phonemize.Phonemizer

	mu      sync.Mutex
	configs map[string]ModelConfig
	models  map[string]*Model
	closed  bool
}

// NewEngine loads ONNX Runtime from ortLibPath, which must be the versioned
// library file (libonnxruntime.so.1.29.1, not the symlink), and fails if it
// is not ONNX Runtime 1.29.1. Models load on first use.
func NewEngine(ortLibPath string, models []ModelConfig) (*Engine, error) {
	configs := make(map[string]ModelConfig, len(models))
	for _, c := range models {
		if _, dup := configs[c.Name]; dup {
			return nil, fmt.Errorf("kittentts: model %q configured twice", c.Name)
		}
		if c.Device != "" && c.Device != CPU {
			return nil, fmt.Errorf("kittentts: model %q: device %q is not supported", c.Name, c.Device)
		}
		configs[c.Name] = c
	}

	backend, err := espeak.New()
	if err != nil {
		return nil, fmt.Errorf("kittentts: %w", err)
	}

	if ort.IsInitialized() {
		return nil, errors.New("kittentts: ONNX Runtime is already initialized; a process can have one Engine")
	}
	ort.SetSharedLibraryPath(ortLibPath)
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("kittentts: loading ONNX Runtime from %s: %w", ortLibPath, err)
	}
	if v := ort.GetVersion(); v != ORTVersion {
		ort.DestroyEnvironment()
		return nil, fmt.Errorf("kittentts: %s is ONNX Runtime %s, want %s", ortLibPath, v, ORTVersion)
	}

	return &Engine{
		phonemizer: phonemize.PreservePunctuation(backend),
		configs:    configs,
		models:     map[string]*Model{},
	}, nil
}

// Model returns the named model, loading it on first use. Loading checks the
// model contract, reads the voices and creates the ONNX Runtime session.
func (e *Engine) Model(name string) (*Model, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, errors.New("kittentts: engine is closed")
	}
	if m, ok := e.models[name]; ok {
		return m, nil
	}
	cfg, ok := e.configs[name]
	if !ok {
		return nil, fmt.Errorf("kittentts: unknown model %q", name)
	}
	m, err := loadModel(cfg, e.phonemizer)
	if err != nil {
		return nil, fmt.Errorf("kittentts: model %s: %w", name, err)
	}
	e.models[name] = m
	return m, nil
}

// Close destroys every loaded model's session and the ONNX Runtime
// environment. Close must not be called while a synthesis is running, and
// Models from this Engine must not be used after it.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	var errs []error
	for _, m := range e.models {
		errs = append(errs, m.session.Destroy())
	}
	errs = append(errs, ort.DestroyEnvironment())
	return errors.Join(errs...)
}
