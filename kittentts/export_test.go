//go:build native

package kittentts

// RunChunk runs one chunk untrimmed, for tests of the model's own invariants.
func RunChunk(m *Model, text, voice string) ([]float32, []int64, error) {
	key, err := m.voiceKey(voice)
	if err != nil {
		return nil, nil, err
	}
	return m.run(text, key, 1)
}

// VoiceKey resolves a Kitten name or voices.npz key the way Stream does.
func VoiceKey(m *Model, voice string) (string, error) { return m.voiceKey(voice) }
