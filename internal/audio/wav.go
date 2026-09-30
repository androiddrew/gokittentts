// Package audio encodes float32 PCM for output.
package audio

import (
	"encoding/binary"
	"io"
	"math"
)

// WriteWAV writes mono 16-bit PCM WAV, with samples encoded as by PCM16.
func WriteWAV(w io.Writer, samples []float32, sampleRate int) error {
	dataLen := uint32(len(samples) * 2)
	le := binary.LittleEndian
	b := make([]byte, 0, 44+dataLen)
	b = append(b, "RIFF"...)
	b = le.AppendUint32(b, 36+dataLen)
	b = append(b, "WAVE"...)
	b = append(b, "fmt "...)
	b = le.AppendUint32(b, 16) // fmt chunk size
	b = le.AppendUint16(b, 1)  // PCM
	b = le.AppendUint16(b, 1)  // mono
	b = le.AppendUint32(b, uint32(sampleRate))
	b = le.AppendUint32(b, uint32(sampleRate*2)) // byte rate
	b = le.AppendUint16(b, 2)                    // block align
	b = le.AppendUint16(b, 16)                   // bits per sample
	b = append(b, "data"...)
	b = le.AppendUint32(b, dataLen)
	b = appendPCM16(b, samples)
	_, err := w.Write(b)
	return err
}

// PCM16 encodes samples as raw 16-bit little-endian PCM, OpenAI's pcm format.
// Samples are clipped to [-1, 1] and scaled by 32767.
func PCM16(samples []float32) []byte {
	return appendPCM16(make([]byte, 0, len(samples)*2), samples)
}

func appendPCM16(b []byte, samples []float32) []byte {
	for _, s := range samples {
		b = binary.LittleEndian.AppendUint16(b, uint16(int16Sample(s)))
	}
	return b
}

// int16Sample clips s to [-1, 1] and scales it by 32767.
func int16Sample(s float32) int16 {
	s = max(-1, min(1, s))
	return int16(math.Round(float64(s) * 32767))
}
