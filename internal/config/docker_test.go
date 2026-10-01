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

// The images' configs must load, point at the paths the Dockerfiles lay out
// and offer every pinned model.
func TestDockerConfigs(t *testing.T) {
	for _, tc := range []struct {
		file         string
		defaultModel string
		device       kittentts.Device
		preload      []string
	}{
		{"config.yaml", "kitten-tts-mini-0.8", kittentts.CPU, nil},
		// The CUDA images default to mini's GPU conversion, and without a usable
		// GPU they exit instead of starting.
		{"config.cuda.yaml", "kitten-tts-mini-0.8-fp32", kittentts.CUDA, []string{"kitten-tts-mini-0.8-fp32"}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			b, err := os.ReadFile("../../docker/" + tc.file)
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
			if c.DefaultModel != tc.defaultModel {
				t.Errorf("default_model %q, want %q", c.DefaultModel, tc.defaultModel)
			}
			if got, want := slices.Sorted(maps.Keys(c.Models)), slices.Sorted(maps.Keys(modelstore.Manifest)); !slices.Equal(got, want) {
				t.Errorf("models %v, want %v", got, want)
			}
			var preload []string
			for name, m := range c.Models {
				if m.Device != tc.device {
					t.Errorf("model %s: device %q, want %q", name, m.Device, tc.device)
				}
				if m.Preload {
					preload = append(preload, name)
				}
			}
			if slices.Sort(preload); !slices.Equal(preload, tc.preload) {
				t.Errorf("preloaded %v, want %v", preload, tc.preload)
			}
		})
	}
}

// The Pi 5 build picks nano-fp32 at run time.
func TestDockerConfigDefaultModelOverride(t *testing.T) {
	b, err := os.ReadFile("../../docker/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse(b, func(k string) string {
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
