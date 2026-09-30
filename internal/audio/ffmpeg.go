package audio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// ffmpegOutputs are the ffmpeg output arguments for each format EncodeFFmpeg
// produces.
var ffmpegOutputs = map[string][]string{
	"opus": {"-c:a", "libopus", "-f", "ogg"},
	"aac":  {"-c:a", "aac", "-f", "adts"},
	"flac": {"-c:a", "flac", "-f", "flac"},
}

// EncodeFFmpeg encodes mono samples as format (opus in Ogg, aac in ADTS, or
// flac) by piping them, converted as by PCM16, through the ffmpeg binary at
// path.
func EncodeFFmpeg(ctx context.Context, w io.Writer, path string, samples []float32, sampleRate int, format string) error {
	out, ok := ffmpegOutputs[format]
	if !ok {
		return fmt.Errorf("ffmpeg: unsupported format %q", format)
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "s16le", "-ar", strconv.Itoa(sampleRate), "-ac", "1", "-i", "-"}
	cmd := exec.CommandContext(ctx, path, append(append(args, out...), "-")...)
	cmd.Stdin = bytes.NewReader(PCM16(samples))
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg %s: %w: %s", format, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
