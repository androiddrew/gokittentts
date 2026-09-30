//go:build espeak || native

// Package espeak is the libespeak-ng backend for phonemize, linked through
// cgo. It phonemizes text the way phonemizer's EspeakBackend does before
// punctuation is restored, so it is meant to be wrapped with
// phonemize.PreservePunctuation.
//
// The cgo backend needs libespeak-ng-dev and is built only with the espeak
// (or native) build tag, so that go test ./... builds without it. Without the
// tag, New returns an error.
package espeak

/*
#cgo LDFLAGS: -lespeak-ng
#include <stdlib.h>
#include <espeak-ng/speak_lib.h>
*/
import "C"

import (
	"errors"
	"strings"
	"sync"
	"unsafe"
)

// espeak-ng keeps global state, so every call goes through this mutex.
var (
	mu      sync.Mutex
	once    sync.Once
	initErr error
)

// Espeak phonemizes en-us text into IPA with stress marks.
type Espeak struct{}

// New initializes libespeak-ng once per process with the en-us voice.
func New() (*Espeak, error) {
	once.Do(func() {
		mu.Lock()
		defer mu.Unlock()
		if C.espeak_Initialize(C.AUDIO_OUTPUT_SYNCHRONOUS, 0, nil, C.espeakINITIALIZE_DONT_EXIT) < 0 {
			initErr = errors.New("espeak: espeak_Initialize failed")
			return
		}
		voice := C.CString("en-us")
		defer C.free(unsafe.Pointer(voice))
		if C.espeak_SetVoiceByName(voice) != C.EE_OK {
			initErr = errors.New("espeak: cannot select voice en-us")
		}
	})
	if initErr != nil {
		return nil, initErr
	}
	return &Espeak{}, nil
}

// Version is the libespeak-ng version string, such as "1.51".
func Version() string {
	mu.Lock()
	defer mu.Unlock()
	return C.GoString(C.espeak_Info(nil))
}

// Phonemize returns the IPA for text, one clause at a time joined by spaces,
// with every word followed by a space, as phonemizer's EspeakBackend
// (text_to_phonemes and _postprocess_line) produces it.
func (*Espeak) Phonemize(text string) (string, error) {
	line := textToPhonemes(text)

	line = strings.TrimSpace(line)
	line = strings.ReplaceAll(line, "\n", " ")
	line = strings.ReplaceAll(line, "  ", " ")
	if line == "" {
		return "", nil
	}
	var sb strings.Builder
	for word := range strings.SplitSeq(line, " ") {
		sb.WriteString(strings.TrimSpace(word))
		sb.WriteByte(' ')
	}
	return sb.String(), nil
}

func textToPhonemes(text string) string {
	mu.Lock()
	defer mu.Unlock()
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	ptr := unsafe.Pointer(cs)
	var clauses []string
	for ptr != nil {
		if ph := C.GoString(C.espeak_TextToPhonemes(&ptr, C.espeakCHARS_UTF8, C.espeakPHONEMES_IPA)); ph != "" {
			clauses = append(clauses, ph)
		}
	}
	return strings.Join(clauses, " ")
}
