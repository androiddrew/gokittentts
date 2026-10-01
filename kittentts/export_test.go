//go:build native

package kittentts

// RunChunk runs one chunk untrimmed at exactly speed, with no speed prior,
// for tests of the model's own invariants.
func RunChunk(m *Model, text, voice string, speed float32) ([]float32, []int64, error) {
	key, err := m.voiceKey(voice)
	if err != nil {
		return nil, nil, err
	}
	return m.run(text, key, speed)
}

// VoiceKey resolves a Kitten name or voices.npz key the way Stream does.
func VoiceKey(m *Model, voice string) (string, error) { return m.voiceKey(voice) }
