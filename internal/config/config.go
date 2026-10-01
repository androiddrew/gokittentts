// Package config loads and validates the gokittentts YAML config.
package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/androiddrew/gokittentts/kittentts"
)

// DefaultAlias is the model_aliases target that means default_model.
const DefaultAlias = "default"

// Config is the whole config file. Fields that later features read (download
// and so on) are ignored until those features land.
type Config struct {
	Listen         string            `yaml:"listen"`
	ONNXRuntimeLib string            `yaml:"onnxruntime_lib"`
	ModelsDir      string            `yaml:"models_dir"`
	DefaultModel   string            `yaml:"default_model"`
	Models         map[string]Model  `yaml:"models"`
	ModelAliases   map[string]string `yaml:"model_aliases"` // OpenAI model name -> model name or "default"
	Voices         map[string]string `yaml:"voices"`        // OpenAI voice name -> Kitten voice name
	Speed          SpeedRange        `yaml:"speed"`
	Limits         Limits            `yaml:"limits"`
	FFmpeg         string            `yaml:"ffmpeg"`     // path or name on PATH; empty disables opus, aac and flac
	Metrics        bool              `yaml:"metrics"`    // serve /metrics
	LogFormat      string            `yaml:"log_format"` // json or text
	APIKey         string            `yaml:"-"`          // from KITTEN_API_KEY only; empty turns auth off
}

// Model is one entry under models.
type Model struct {
	Device         kittentts.Device `yaml:"device"`
	CUDADeviceID   int              `yaml:"cuda_device_id"`
	IntraOpThreads int              `yaml:"intra_op_threads"`
	MaxQueue       *int             `yaml:"max_queue"` // requests that may wait behind the running one; nil means DefaultMaxQueue
}

// DefaultMaxQueue is a model's queue size when max_queue is not set.
const DefaultMaxQueue = 8

// QueueSize is how many requests may wait for the model behind the one
// that is running.
func (m Model) QueueSize() int {
	if m.MaxQueue == nil {
		return DefaultMaxQueue
	}
	return *m.MaxQueue
}

// SpeedRange is the range effective speeds are clamped to.
type SpeedRange struct {
	Min float32 `yaml:"min"`
	Max float32 `yaml:"max"`
}

// Limits are per-request limits.
type Limits struct {
	MaxInputChars  int           `yaml:"max_input_chars"`
	RequestTimeout time.Duration `yaml:"request_timeout"` // covers queue wait and synthesis
}

// Load reads the config file at path, applies environment overrides and
// validates the result.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(b, os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse decodes a config, fills in defaults, applies the KITTEN_DEFAULT_MODEL
// and KITTEN_LISTEN overrides and the KITTEN_API_KEY setting from getenv, and
// validates it.
func Parse(data []byte, getenv func(string) string) (*Config, error) {
	c := Config{
		Listen:       ":8880",
		ModelsDir:    "/var/lib/gokittentts/models",
		DefaultModel: "kitten-tts-mini-0.8",
		Speed:        SpeedRange{Min: 0.5, Max: 2.0},
		Limits:       Limits{MaxInputChars: 4096, RequestTimeout: 120 * time.Second},
		FFmpeg:       "ffmpeg",
		Metrics:      true,
		LogFormat:    "json",
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if c.ModelAliases == nil {
		c.ModelAliases = map[string]string{"tts-1": DefaultAlias, "tts-1-hd": DefaultAlias, "gpt-4o-mini-tts": DefaultAlias}
	}
	if c.Voices == nil {
		c.Voices = defaultVoices()
	}
	if v := getenv("KITTEN_DEFAULT_MODEL"); v != "" {
		c.DefaultModel = v
	}
	if v := getenv("KITTEN_LISTEN"); v != "" {
		c.Listen = v
	}
	c.APIKey = getenv("KITTEN_API_KEY")
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}

// defaultVoices maps OpenAI's voices to Kitten voices by rough gender match.
func defaultVoices() map[string]string {
	return map[string]string{
		"alloy": "Bella", "ash": "Jasper", "ballad": "Leo", "cedar": "Bruno",
		"coral": "Rosie", "echo": "Bruno", "fable": "Jasper", "marin": "Luna",
		"nova": "Luna", "onyx": "Hugo", "sage": "Kiki", "shimmer": "Kiki", "verse": "Leo",
	}
}

func (c *Config) validate() error {
	var errs []error
	if len(c.Models) == 0 {
		errs = append(errs, errors.New("no models configured"))
	}
	for name, m := range c.Models {
		if m.Device != "" && m.Device != kittentts.CPU && m.Device != "cuda" {
			errs = append(errs, fmt.Errorf("model %s: unknown device %q (want cpu or cuda)", name, m.Device))
		}
		if m.QueueSize() < 0 {
			errs = append(errs, fmt.Errorf("model %s: max_queue must not be negative, got %d", name, m.QueueSize()))
		}
	}
	if _, ok := c.Models[c.DefaultModel]; !ok && len(c.Models) > 0 {
		errs = append(errs, fmt.Errorf("default_model %s is not under models", c.DefaultModel))
	}
	for alias, target := range c.ModelAliases {
		if _, ok := c.Models[target]; !ok && target != DefaultAlias {
			errs = append(errs, fmt.Errorf("model_aliases: %s maps to %s, which is not under models", alias, target))
		}
	}
	for openai, kitten := range c.Voices {
		if !slices.ContainsFunc(kittentts.Voices, func(v kittentts.Voice) bool { return v.Name == kitten }) {
			errs = append(errs, fmt.Errorf("voices: %s maps to %s, which is not a Kitten voice", openai, kitten))
		}
	}
	if c.Speed.Min > c.Speed.Max {
		errs = append(errs, fmt.Errorf("speed: min %v is greater than max %v", c.Speed.Min, c.Speed.Max))
	}
	if c.Limits.MaxInputChars <= 0 {
		errs = append(errs, fmt.Errorf("limits: max_input_chars must be positive, got %d", c.Limits.MaxInputChars))
	}
	if c.APIKey != "" && strings.TrimSpace(c.APIKey) == "" {
		errs = append(errs, errors.New("KITTEN_API_KEY is only whitespace; unset it to turn auth off"))
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("log_format must be json or text, got %q", c.LogFormat))
	}
	if c.Limits.RequestTimeout <= 0 {
		errs = append(errs, fmt.Errorf("limits: request_timeout must be positive, got %v", c.Limits.RequestTimeout))
	}
	return errors.Join(errs...)
}
