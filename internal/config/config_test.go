package config_test

import (
	"strings"
	"testing"

	"github.com/androiddrew/gokittentts/internal/config"
)

func noEnv(string) string { return "" }

const full = `
listen: ":9000"
onnxruntime_lib: /opt/onnxruntime/lib/libonnxruntime.so.1.29.1
models_dir: /var/lib/gokittentts/models
default_model: kitten-tts-micro-0.8
models:
  kitten-tts-mini-0.8:  { device: cpu, intra_op_threads: 4 }
  kitten-tts-micro-0.8: { device: cpu }
model_aliases:
  tts-1: default
  tts-1-hd: kitten-tts-mini-0.8
voices:
  alloy: Bella
  onyx: Hugo
speed: { min: 0.75, max: 1.5 }
limits:
  max_input_chars: 100
`

func TestParse(t *testing.T) {
	c, err := config.Parse([]byte(full), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":9000" || c.ONNXRuntimeLib != "/opt/onnxruntime/lib/libonnxruntime.so.1.29.1" ||
		c.ModelsDir != "/var/lib/gokittentts/models" || c.DefaultModel != "kitten-tts-micro-0.8" {
		t.Errorf("top-level fields: %+v", c)
	}
	if m := c.Models["kitten-tts-mini-0.8"]; m.Device != "cpu" || m.IntraOpThreads != 4 {
		t.Errorf("mini: %+v", m)
	}
	if c.ModelAliases["tts-1-hd"] != "kitten-tts-mini-0.8" || c.Voices["onyx"] != "Hugo" {
		t.Errorf("aliases %v, voices %v", c.ModelAliases, c.Voices)
	}
	if c.Speed.Min != 0.75 || c.Speed.Max != 1.5 || c.Limits.MaxInputChars != 100 {
		t.Errorf("speed %+v, limits %+v", c.Speed, c.Limits)
	}
}

func TestParseDefaults(t *testing.T) {
	c, err := config.Parse([]byte("models:\n  kitten-tts-mini-0.8: {}\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8880" || c.DefaultModel != "kitten-tts-mini-0.8" || c.ModelsDir != "/var/lib/gokittentts/models" {
		t.Errorf("listen %q, default model %q, models dir %q", c.Listen, c.DefaultModel, c.ModelsDir)
	}
	if c.Speed.Min != 0.5 || c.Speed.Max != 2.0 || c.Limits.MaxInputChars != 4096 {
		t.Errorf("speed %+v, limits %+v", c.Speed, c.Limits)
	}
	for _, alias := range []string{"tts-1", "tts-1-hd", "gpt-4o-mini-tts"} {
		if c.ModelAliases[alias] != "default" {
			t.Errorf("alias %s = %q, want default", alias, c.ModelAliases[alias])
		}
	}
	if c.Voices["alloy"] != "Bella" || c.Voices["onyx"] != "Hugo" || len(c.Voices) != 13 {
		t.Errorf("default voice map %v", c.Voices)
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	env := map[string]string{"KITTEN_DEFAULT_MODEL": "kitten-tts-mini-0.8", "KITTEN_LISTEN": "127.0.0.1:1234"}
	c, err := config.Parse([]byte(full), func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultModel != "kitten-tts-mini-0.8" || c.Listen != "127.0.0.1:1234" {
		t.Errorf("default model %q, listen %q", c.DefaultModel, c.Listen)
	}
	// The override is validated like the file.
	env["KITTEN_DEFAULT_MODEL"] = "nope"
	if _, err := config.Parse([]byte(full), func(k string) string { return env[k] }); err == nil {
		t.Error("an unconfigured KITTEN_DEFAULT_MODEL should fail")
	}
}

func TestValidation(t *testing.T) {
	cases := []struct {
		name, yaml, want string
	}{
		{"unknown device", "models:\n  kitten-tts-mini-0.8: { device: rocm }\n", "device"},
		{"unconfigured default model", "default_model: kitten-tts-nano-0.8-fp32\nmodels:\n  kitten-tts-mini-0.8: {}\n", "kitten-tts-nano-0.8-fp32"},
		{"no models", "listen: \":1\"\n", "models"},
		{"voice map names a non-Kitten voice", "models:\n  kitten-tts-mini-0.8: {}\nvoices:\n  alloy: Nobody\n", "Nobody"},
		{"inverted speed range", "models:\n  kitten-tts-mini-0.8: {}\nspeed: { min: 2, max: 1 }\n", "speed"},
		{"alias to an unconfigured model", "models:\n  kitten-tts-mini-0.8: {}\nmodel_aliases:\n  tts-1: kitten-tts-micro-0.8\n", "kitten-tts-micro-0.8"},
		{"non-positive input limit", "models:\n  kitten-tts-mini-0.8: {}\nlimits: { max_input_chars: -1 }\n", "max_input_chars"},
		{"bad yaml", "models: [", "yaml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := config.Parse([]byte(c.yaml), noEnv)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}
