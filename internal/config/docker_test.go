package config_test

import (
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/internal/modelstore"
	"github.com/androiddrew/gokittentts/kittentts"
)

// The images' config must load, point at the paths the Dockerfiles lay out
// and offer every pinned model on the CPU.
func TestDockerConfig(t *testing.T) {
	b, err := os.ReadFile("../../docker/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse(b, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8880" {
		t.Errorf("listen %q", c.Listen)
	}
	if c.ONNXRuntimeLib != "/opt/onnxruntime/lib/libonnxruntime.so.1.29.1" {
		t.Errorf("onnxruntime_lib %q", c.ONNXRuntimeLib)
	}
	if c.ModelsDir != config.DefaultModelsDir || !c.Download {
		t.Errorf("models_dir %q, download %v", c.ModelsDir, c.Download)
	}
	if c.DefaultModel != "kitten-tts-mini-0.8" {
		t.Errorf("default_model %q", c.DefaultModel)
	}
	if got, want := slices.Sorted(maps.Keys(c.Models)), slices.Sorted(maps.Keys(modelstore.Manifest)); !slices.Equal(got, want) {
		t.Errorf("models %v, want %v", got, want)
	}
	for name, m := range c.Models {
		if m.Device != kittentts.CPU {
			t.Errorf("model %s: device %q", name, m.Device)
		}
	}

	// The Pi 5 build picks nano-fp32 at run time.
	c, err = config.Parse(b, func(k string) string {
		if k == "KITTEN_DEFAULT_MODEL" {
			return "kitten-tts-nano-0.8-fp32"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultModel != "kitten-tts-nano-0.8-fp32" {
		t.Errorf("default_model %q with KITTEN_DEFAULT_MODEL set", c.DefaultModel)
	}
}
