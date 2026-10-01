package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/kittentts"
)

func noEnv(string) string { return "" }

const full = `
listen: ":9000"
onnxruntime_lib: /opt/onnxruntime/lib/libonnxruntime.so.1.29.1
models_dir: /var/lib/gokittentts/models
default_model: kitten-tts-micro-0.8
models:
  kitten-tts-mini-0.8:  { device: cpu, intra_op_threads: 4, max_queue: 3 }
  kitten-tts-micro-0.8: { device: cpu }
  kitten-tts-nano-0.8-fp32: { device: cuda, cuda_device_id: 1, preload: true }
  my-kitten: { repo: someone/my-kitten, revision: abc123 }
model_aliases:
  tts-1: default
  tts-1-hd: kitten-tts-mini-0.8
voices:
  alloy: Bella
  onyx: Hugo
speed: { min: 0.75, max: 1.5 }
limits:
  max_input_chars: 100
  request_timeout: 45s
ffmpeg: /usr/local/bin/ffmpeg
metrics: false
log_format: text
download: false
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
	if m := c.Models["kitten-tts-mini-0.8"]; m.Device != "cpu" || m.IntraOpThreads != 4 || m.QueueSize() != 3 || m.Preload {
		t.Errorf("mini: %+v, queue %d", m, m.QueueSize())
	}
	if m := c.Models["kitten-tts-nano-0.8-fp32"]; m.Device != kittentts.CUDA || m.CUDADeviceID != 1 || !m.Preload {
		t.Errorf("nano-fp32: %+v", m)
	}
	if c.Limits.RequestTimeout != 45*time.Second {
		t.Errorf("request timeout %v", c.Limits.RequestTimeout)
	}
	if c.APIKey != "" {
		t.Errorf("api key %q without KITTEN_API_KEY", c.APIKey)
	}
	if c.ModelAliases["tts-1-hd"] != "kitten-tts-mini-0.8" || c.Voices["onyx"] != "Hugo" {
		t.Errorf("aliases %v, voices %v", c.ModelAliases, c.Voices)
	}
	if c.Speed.Min != 0.75 || c.Speed.Max != 1.5 || c.Limits.MaxInputChars != 100 {
		t.Errorf("speed %+v, limits %+v", c.Speed, c.Limits)
	}
	if c.FFmpeg != "/usr/local/bin/ffmpeg" {
		t.Errorf("ffmpeg %q", c.FFmpeg)
	}
	if c.Metrics || c.LogFormat != "text" || c.Download {
		t.Errorf("metrics %v, log format %q, download %v", c.Metrics, c.LogFormat, c.Download)
	}
	if m := c.Models["my-kitten"]; m.Repo != "someone/my-kitten" || m.Revision != "abc123" {
		t.Errorf("custom model %+v", m)
	}
}

func TestFFmpegCanBeDisabled(t *testing.T) {
	c, err := config.Parse([]byte("models:\n  kitten-tts-mini-0.8: {}\nffmpeg: \"\"\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.FFmpeg != "" {
		t.Errorf("ffmpeg %q, want empty", c.FFmpeg)
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
	if c.FFmpeg != "ffmpeg" {
		t.Errorf("ffmpeg %q, want ffmpeg", c.FFmpeg)
	}
	if q := c.Models["kitten-tts-mini-0.8"].QueueSize(); q != 8 {
		t.Errorf("queue size %d, want 8", q)
	}
	if c.Limits.RequestTimeout != 120*time.Second {
		t.Errorf("request timeout %v, want 120s", c.Limits.RequestTimeout)
	}
	if !c.Metrics || c.LogFormat != "json" || !c.Download {
		t.Errorf("metrics %v, log format %q, download %v; want true, json and true", c.Metrics, c.LogFormat, c.Download)
	}
}

func TestQueueSizeCanBeZero(t *testing.T) {
	c, err := config.Parse([]byte("models:\n  kitten-tts-mini-0.8: { max_queue: 0 }\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if q := c.Models["kitten-tts-mini-0.8"].QueueSize(); q != 0 {
		t.Errorf("queue size %d, want 0", q)
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	env := map[string]string{"KITTEN_DEFAULT_MODEL": "kitten-tts-mini-0.8", "KITTEN_LISTEN": "127.0.0.1:1234", "KITTEN_API_KEY": "s3cret"}
	c, err := config.Parse([]byte(full), func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultModel != "kitten-tts-mini-0.8" || c.Listen != "127.0.0.1:1234" || c.APIKey != "s3cret" {
		t.Errorf("default model %q, listen %q, api key %q", c.DefaultModel, c.Listen, c.APIKey)
	}
	if _, err := config.Parse([]byte(full), func(k string) string { return map[string]string{"KITTEN_API_KEY": " \t"}[k] }); err == nil || !strings.Contains(err.Error(), "KITTEN_API_KEY") {
		t.Errorf("a whitespace-only KITTEN_API_KEY: err = %v, want an error naming it", err)
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
		{"negative CUDA device id", "models:\n  kitten-tts-mini-0.8: { device: cuda, cuda_device_id: -1 }\n", "cuda_device_id"},
		{"negative intra-op threads", "models:\n  kitten-tts-mini-0.8: { intra_op_threads: -2 }\n", "intra_op_threads"},
		{"unconfigured default model", "default_model: kitten-tts-nano-0.8-fp32\nmodels:\n  kitten-tts-mini-0.8: {}\n", "kitten-tts-nano-0.8-fp32"},
		{"no models", "listen: \":1\"\n", "models"},
		{"voice map names a non-Kitten voice", "models:\n  kitten-tts-mini-0.8: {}\nvoices:\n  alloy: Nobody\n", "Nobody"},
		{"inverted speed range", "models:\n  kitten-tts-mini-0.8: {}\nspeed: { min: 2, max: 1 }\n", "speed"},
		{"alias to an unconfigured model", "models:\n  kitten-tts-mini-0.8: {}\nmodel_aliases:\n  tts-1: kitten-tts-micro-0.8\n", "kitten-tts-micro-0.8"},
		{"non-positive input limit", "models:\n  kitten-tts-mini-0.8: {}\nlimits: { max_input_chars: -1 }\n", "max_input_chars"},
		{"negative queue", "models:\n  kitten-tts-mini-0.8: { max_queue: -1 }\n", "max_queue"},
		{"non-positive request timeout", "models:\n  kitten-tts-mini-0.8: {}\nlimits: { request_timeout: 0s }\n", "request_timeout"},
		{"unknown log format", "models:\n  kitten-tts-mini-0.8: {}\nlog_format: xml\n", "log_format"},
		{"repo without a revision", "models:\n  kitten-tts-mini-0.8: {}\n  mine: { repo: a/b }\n", "revision"},
		{"revision without a repo", "models:\n  kitten-tts-mini-0.8: {}\n  mine: { revision: abc }\n", "repo"},
		{"repo on a pinned model", "models:\n  kitten-tts-mini-0.8: { repo: a/b, revision: abc }\n", "pinned"},
		{"revision that is a path", "models:\n  kitten-tts-mini-0.8: {}\n  mine: { repo: a/b, revision: ../x }\n", "revision"},
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
