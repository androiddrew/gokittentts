//go:build !espeak && !native

package espeak

import "errors"

var errNoEspeak = errors.New("espeak: built without espeak-ng; build with -tags espeak")

// Espeak is unavailable in builds without the espeak tag.
type Espeak struct{}

// New always fails in builds without the espeak tag.
func New() (*Espeak, error) { return nil, errNoEspeak }

// Version is empty in builds without the espeak tag.
func Version() string { return "" }

// Phonemize always fails in builds without the espeak tag.
func (*Espeak) Phonemize(string) (string, error) { return "", errNoEspeak }
