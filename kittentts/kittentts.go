// Package kittentts runs the KittenTTS 0.8 models: text in, 24 kHz mono
// float32 PCM out. It owns the whole pipeline (chunking, espeak-ng
// phonemization, tokenization, voice styles and the ONNX Runtime run) and has
// no queue, auth or audio encoding.
package kittentts

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

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
	slots      map[string]*slot // by model name, fixed by NewEngine

	mu     sync.Mutex // guards closed
	closed bool
}

// slot is a configured model, loaded on first use. Each slot loads on its
// own, so loading one model doesn't hold up requests for others.
type slot struct {
	cfg   ModelConfig
	mu    sync.Mutex // held while loading
	model atomic.Pointer[Model]
}

// NewEngine loads ONNX Runtime from ortLibPath, which must be the versioned
// library file (libonnxruntime.so.1.29.1, not the symlink), and fails if it
// is not ONNX Runtime 1.29.1. Models load on first use.
func NewEngine(ortLibPath string, models []ModelConfig) (*Engine, error) {
	slots := make(map[string]*slot, len(models))
	for _, c := range models {
		if _, dup := slots[c.Name]; dup {
			return nil, fmt.Errorf("kittentts: model %q configured twice", c.Name)
		}
		if c.Device != "" && c.Device != CPU {
			return nil, fmt.Errorf("kittentts: model %q: device %q is not supported", c.Name, c.Device)
		}
		slots[c.Name] = &slot{cfg: c}
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
		slots:      slots,
	}, nil
}

var errClosed = errors.New("kittentts: engine is closed")

// Model returns the named model, loading it on first use. Loading checks the
// model contract, reads the model's config.json and voices from its own
// directory, and creates the ONNX Runtime session. Concurrent first calls
// load the model once. A failed load is not remembered, so a later call
// tries again.
func (e *Engine) Model(name string) (*Model, error) {
	sl, ok := e.slots[name]
	if !ok {
		return nil, fmt.Errorf("kittentts: unknown model %q", name)
	}
	if e.isClosed() {
		return nil, errClosed
	}
	if m := sl.model.Load(); m != nil {
		return m, nil
	}
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if m := sl.model.Load(); m != nil {
		return m, nil
	}
	// Close takes every slot's lock after closing, so a load that holds this
	// lock either finishes before Close destroys anything or sees it closed.
	if e.isClosed() {
		return nil, errClosed
	}
	m, err := loadModel(sl.cfg, e.phonemizer)
	if err != nil {
		return nil, fmt.Errorf("kittentts: model %s: %w", name, err)
	}
	sl.model.Store(m)
	return m, nil
}

// Loaded reports whether the named model has loaded. It doesn't wait for a
// load in progress.
func (e *Engine) Loaded(name string) bool {
	sl, ok := e.slots[name]
	return ok && sl.model.Load() != nil
}

func (e *Engine) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

// Close destroys every loaded model's session and the ONNX Runtime
// environment, after waiting for any load in progress. Close must not be
// called while a model is synthesizing, and Models from this Engine must not
// be used after it.
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()
	var errs []error
	for _, sl := range e.slots {
		sl.mu.Lock()
		if m := sl.model.Load(); m != nil {
			errs = append(errs, m.session.Destroy())
		}
		sl.mu.Unlock()
	}
	errs = append(errs, ort.DestroyEnvironment())
	return errors.Join(errs...)
}
