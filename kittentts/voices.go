package kittentts

// Voice is one of the eight KittenTTS 0.8 voices: its display name and its
// voices.npz key. Every model has the same eight, though each model ships
// its own style vectors.
type Voice struct {
	Name string // e.g. "Bruno"
	Key  string // e.g. "expr-voice-3-m"
}

// Voices lists the KittenTTS 0.8 voices, in voices.npz key order.
var Voices = []Voice{
	{"Jasper", "expr-voice-2-m"},
	{"Bella", "expr-voice-2-f"},
	{"Bruno", "expr-voice-3-m"},
	{"Luna", "expr-voice-3-f"},
	{"Hugo", "expr-voice-4-m"},
	{"Rosie", "expr-voice-4-f"},
	{"Leo", "expr-voice-5-m"},
	{"Kiki", "expr-voice-5-f"},
}
