//go:build native

package server_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/internal/modelstore"
	"github.com/androiddrew/gokittentts/internal/server"
	"github.com/androiddrew/gokittentts/kittentts"
)

// The native tests serve real models from KITTEN_MODELS_DIR, which `make
// test-native` fills with `gokittentts pull`. ONNX Runtime's environment is
// process-wide, so they share one handler.
var native http.Handler

// pinnedModels are the four models make test-native pulls, and unobtainable
// is configured but on no disk and in no manifest.
var pinnedModels = []string{"kitten-tts-mini-0.8", "kitten-tts-micro-0.8", "kitten-tts-nano-0.8-int8", "kitten-tts-nano-0.8-fp32"}

const unobtainable = "kitten-unobtainable"

func TestMain(m *testing.M) {
	lib, dir := os.Getenv("KITTEN_ORT_LIB"), os.Getenv("KITTEN_MODELS_DIR")
	if lib == "" || dir == "" {
		fmt.Fprintln(os.Stderr, "native tests need KITTEN_ORT_LIB and KITTEN_MODELS_DIR; run make test-native")
		os.Exit(1)
	}
	yaml := fmt.Sprintf("models_dir: %s\ndownload: false\nffmpeg: \"\"\nmodels:\n  %s: {}\n", dir, unobtainable)
	for _, name := range pinnedModels {
		yaml += fmt.Sprintf("  %s: {}\n", name)
	}
	cfg, err := config.Parse([]byte(yaml), func(string) string { return "" })
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	store := modelstore.New(cfg.ModelsDir, modelstore.Manifest, cfg.Download)
	var models []kittentts.ModelConfig
	for name := range cfg.Models {
		models = append(models, kittentts.ModelConfig{Name: name, Dir: store.Path(name)})
	}
	engine, err := kittentts.NewEngine(lib, models)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	native = server.New(cfg, server.KittenEngine{Engine: engine, Store: store})
	code := m.Run()
	engine.Close()
	os.Exit(code)
}

func TestEveryModelSpeaksOverHTTP(t *testing.T) {
	for _, model := range pinnedModels {
		rec := speech(t, native, `{"model":"`+model+`","input":"Hello from Go.","voice":"Bella","response_format":"wav"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", model, rec.Code, rec.Body)
		}
		pcm := rec.Body.Bytes()[44:] // after the streamed WAV header
		var sum float64
		for i := 0; i+1 < len(pcm); i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768
			sum += v * v
		}
		samples := len(pcm) / 2
		if r := math.Sqrt(sum / float64(max(1, samples))); samples < kittentts.SampleRate/2 || r <= 0.01 {
			t.Errorf("%s: %d samples, RMS %.4f; want at least half a second of speech", model, samples, r)
		}
	}
}

func TestAModelThatCannotBeGotIs503(t *testing.T) {
	e := openAIError(t, speech(t, native, `{"model":"`+unobtainable+`","input":"Hi.","voice":"Leo"}`), http.StatusServiceUnavailable)
	if msg, _ := e["message"].(string); !strings.Contains(msg, "gokittentts pull "+unobtainable) {
		t.Errorf("error %v, want the store's advice to pull the model", e)
	}
}
