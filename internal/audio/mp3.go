package audio

import (
	"io"

	"github.com/braheezy/shine-mp3/pkg/mp3"
)

// mp3Bitrate is the mp3 bitrate in kbps.
const mp3Bitrate = 64

// mp3Flush is the silence appended so the encoder's analysis delay doesn't
// cut off the end of the audio.
const mp3Flush = 2 * 576

// WriteMP3 writes mono samples as 64 kbps MPEG layer III, with samples
// converted as by PCM16. Empty input still writes a valid, silent mp3.
func WriteMP3(w io.Writer, samples []float32, sampleRate int) error {
	version, err := mp3.CheckConfig(sampleRate, mp3Bitrate)
	if err != nil {
		return err
	}
	enc := mp3.NewEncoder(sampleRate, 1)
	// NewEncoder always picks 128 kbps; redo its bitrate-derived fields.
	enc.Mpeg.Bitrate = mp3Bitrate
	enc.Mpeg.BitrateIndex = 8 // 64 kbps in the MPEG-2 and 2.5 tables
	if version == mp3.MPEG_I {
		enc.Mpeg.BitrateIndex = 5 // 64 kbps in the MPEG-1 table
	}
	slots := float64(enc.Mpeg.GranulesPerFrame*mp3.GRANULE_SIZE) / float64(sampleRate) * mp3Bitrate * 1000 / float64(enc.Mpeg.BitsPerSlot)
	enc.Mpeg.WholeSlotsPerFrame = int64(slots)
	enc.Mpeg.FracSlotsPerFrame = slots - float64(enc.Mpeg.WholeSlotsPerFrame)
	enc.Mpeg.SlotLag = -enc.Mpeg.FracSlotsPerFrame

	pcm := make([]int16, len(samples), len(samples)+mp3Flush)
	for i, s := range samples {
		pcm[i] = int16Sample(s)
	}
	return enc.Write(w, append(pcm, make([]int16, mp3Flush)...))
}
