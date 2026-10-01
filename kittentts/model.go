package kittentts

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/androiddrew/gokittentts/internal/chunk"
	"github.com/androiddrew/gokittentts/internal/markdown"
	"github.com/androiddrew/gokittentts/internal/normalize"
	"github.com/androiddrew/gokittentts/internal/npz"
	"github.com/androiddrew/gokittentts/internal/phonemize"
	"github.com/androiddrew/gokittentts/internal/tokenize"
)

// trimSamples is cut from the end of every chunk's waveform, as Python does.
const trimSamples = 5000

// Model is a loaded model. It is safe for concurrent use.
type Model struct {
	session     *ort.DynamicAdvancedSession
	phonemizer  phonemize.Phonemizer
	voices      map[string][]float32 // voices.npz key -> Rows*Cols styles
	aliases     map[string]string    // Kitten name -> voices.npz key
	speedPriors map[string]float32   // voices.npz key -> speed multiplier
}

// configJSON is the part of a model's config.json that gokittentts reads.
type configJSON struct {
	ModelFile    string             `json:"model_file"`
	Voices       string             `json:"voices"`
	SpeedPriors  map[string]float32 `json:"speed_priors"`
	VoiceAliases map[string]string  `json:"voice_aliases"`
}

func loadModel(cfg ModelConfig, p phonemize.Phonemizer) (*Model, error) {
	b, err := os.ReadFile(filepath.Join(cfg.Dir, "config.json"))
	if err != nil {
		return nil, err
	}
	var mf configJSON
	if err := json.Unmarshal(b, &mf); err != nil {
		return nil, fmt.Errorf("config.json: %w", err)
	}
	onnxPath := filepath.Join(cfg.Dir, mf.ModelFile)

	if err := checkContract(onnxPath); err != nil {
		return nil, err
	}

	voices, err := npz.ReadVoices(filepath.Join(cfg.Dir, mf.Voices))
	if err != nil {
		return nil, err
	}
	for name, key := range mf.VoiceAliases {
		if _, ok := voices[key]; !ok {
			return nil, fmt.Errorf("voice %s maps to %s, which is not in %s", name, key, mf.Voices)
		}
	}

	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer opts.Destroy()
	if cfg.IntraOpThreads > 0 {
		if err := opts.SetIntraOpNumThreads(cfg.IntraOpThreads); err != nil {
			return nil, err
		}
	}
	session, err := ort.NewDynamicAdvancedSession(onnxPath,
		[]string{"input_ids", "style", "speed"}, []string{"waveform", "duration"}, opts)
	if err != nil {
		return nil, err
	}
	return &Model{
		session:     session,
		phonemizer:  p,
		voices:      voices,
		aliases:     mf.VoiceAliases,
		speedPriors: mf.SpeedPriors,
	}, nil
}

// contract is every model's inputs and outputs. -1 is a dynamic dimension.
var contract = struct{ inputs, outputs []ort.InputOutputInfo }{
	inputs: []ort.InputOutputInfo{
		{Name: "input_ids", DataType: ort.TensorElementDataTypeInt64, Dimensions: ort.NewShape(1, -1)},
		{Name: "style", DataType: ort.TensorElementDataTypeFloat, Dimensions: ort.NewShape(1, npz.Cols)},
		{Name: "speed", DataType: ort.TensorElementDataTypeFloat, Dimensions: ort.NewShape(1)},
	},
	outputs: []ort.InputOutputInfo{
		{Name: "waveform", DataType: ort.TensorElementDataTypeFloat, Dimensions: ort.NewShape(-1)},
		{Name: "duration", DataType: ort.TensorElementDataTypeInt64, Dimensions: ort.NewShape(-1)},
	},
}

func checkContract(onnxPath string) error {
	inputs, outputs, err := ort.GetInputOutputInfo(onnxPath)
	if err != nil {
		return err
	}
	if err := matchIO("input", inputs, contract.inputs); err != nil {
		return fmt.Errorf("model contract: %w", err)
	}
	if err := matchIO("output", outputs, contract.outputs); err != nil {
		return fmt.Errorf("model contract: %w", err)
	}
	return nil
}

func matchIO(kind string, got, want []ort.InputOutputInfo) error {
	if len(got) != len(want) {
		return fmt.Errorf("%d %ss, want %d", len(got), kind, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.Name {
			return fmt.Errorf("%s %d is %q, want %q", kind, i, g.Name, w.Name)
		}
		if g.OrtValueType != ort.ONNXTypeTensor || g.DataType != w.DataType || !dimsMatch(g.Dimensions, w.Dimensions) {
			return fmt.Errorf("%s %s is %s %s %v, want tensor %s %v",
				kind, g.Name, g.OrtValueType, g.DataType, g.Dimensions, w.DataType, w.Dimensions)
		}
	}
	return nil
}

// dimsMatch compares shapes, where -1 in want accepts any size.
func dimsMatch(got, want ort.Shape) bool {
	if len(got) != len(want) {
		return false
	}
	for i, d := range want {
		if d != -1 && got[i] != d {
			return false
		}
	}
	return true
}

// Voices returns the Kitten voice names, sorted.
func (m *Model) Voices() []string {
	names := make([]string, 0, len(m.aliases))
	for name := range m.aliases {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// voiceKey resolves a Kitten name or voices.npz key to a voices.npz key.
func (m *Model) voiceKey(voice string) (string, error) {
	if key, ok := m.aliases[voice]; ok {
		return key, nil
	}
	if _, ok := m.voices[voice]; ok {
		return voice, nil
	}
	return "", fmt.Errorf("kittentts: unknown voice %q; valid voices are %s", voice, strings.Join(m.Voices(), ", "))
}

// Synthesize returns the PCM for the whole request.
func (m *Model) Synthesize(ctx context.Context, r Request) ([]float32, error) {
	var pcm []float32
	for c, err := range m.Stream(ctx, r) {
		if err != nil {
			return nil, err
		}
		pcm = append(pcm, c.PCM...)
	}
	return pcm, nil
}

// Chunk is the audio for one chunk of text (about a sentence).
type Chunk struct {
	PCM    []float32 // trimmed by 5,000 samples
	Tokens int       // token ids the model was run on
	Frames int       // the sum of the model's per-token durations, 600 samples each before trimming
}

// Stream yields the audio one chunk at a time. It checks ctx between chunks;
// a chunk's run can't be interrupted. After an error it yields nothing more.
func (m *Model) Stream(ctx context.Context, r Request) iter.Seq2[Chunk, error] {
	return func(yield func(Chunk, error) bool) {
		key, err := m.voiceKey(r.Voice)
		if err != nil {
			yield(Chunk{}, err)
			return
		}
		speed := r.Speed
		if speed == 0 {
			speed = 1
		}
		// Python looks the prior up by key, after resolving the alias.
		if prior, ok := m.speedPriors[key]; ok {
			speed *= prior
		}
		prose := r.Text
		if r.Markdown {
			prose = markdown.ToSpeech(prose)
		}
		if r.Normalize {
			prose = normalize.Text(prose)
		}
		for _, text := range chunk.Split(prose) {
			if err := ctx.Err(); err != nil {
				yield(Chunk{}, err)
				return
			}
			wave, durations, err := m.run(text, key, speed)
			if err != nil {
				yield(Chunk{}, fmt.Errorf("kittentts: %q: %w", text, err))
				return
			}
			c := Chunk{PCM: wave[:max(0, len(wave)-trimSamples)], Tokens: len(durations)}
			for _, d := range durations {
				c.Frames += int(d)
			}
			if !yield(c, nil) {
				return
			}
		}
	}
}

// run synthesizes one chunk, untrimmed, at exactly speed, returning the
// waveform and the per-token durations.
func (m *Model) run(text, voiceKey string, speed float32) ([]float32, []int64, error) {
	phonemes, err := m.phonemizer.Phonemize(text)
	if err != nil {
		return nil, nil, err
	}
	ids := tokenize.IDs(phonemes)

	// The style row is the chunk's character count (not its phonemes'), capped.
	row := min(utf8.RuneCountInString(text), npz.Rows-1)
	style := m.voices[voiceKey][row*npz.Cols : (row+1)*npz.Cols]

	idsT, err := ort.NewTensor(ort.NewShape(1, int64(len(ids))), ids)
	if err != nil {
		return nil, nil, err
	}
	defer idsT.Destroy()
	styleT, err := ort.NewTensor(ort.NewShape(1, npz.Cols), style)
	if err != nil {
		return nil, nil, err
	}
	defer styleT.Destroy()
	speedT, err := ort.NewTensor(ort.NewShape(1), []float32{speed})
	if err != nil {
		return nil, nil, err
	}
	defer speedT.Destroy()

	outputs := []ort.Value{nil, nil}
	if err := m.session.Run([]ort.Value{idsT, styleT, speedT}, outputs); err != nil {
		return nil, nil, err
	}
	defer outputs[0].Destroy()
	defer outputs[1].Destroy()
	wave, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, nil, fmt.Errorf("waveform is %T, want float32 tensor", outputs[0])
	}
	durations, ok := outputs[1].(*ort.Tensor[int64])
	if !ok {
		return nil, nil, fmt.Errorf("duration is %T, want int64 tensor", outputs[1])
	}
	// onnxruntime_go copies outputs it allocated into Go memory, so these
	// slices outlive Destroy.
	return wave.GetData(), durations.GetData(), nil
}
