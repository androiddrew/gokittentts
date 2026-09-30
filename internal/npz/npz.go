// Package npz reads a KittenTTS voices.npz: a zip of .npy files, one
// (400, 256) float32 style array per voice key.
package npz

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
)

// Rows and Cols are the shape of every voice's style array.
const (
	Rows = 400
	Cols = 256
)

var (
	descrRe   = regexp.MustCompile(`'descr':\s*'([^']*)'`)
	fortranRe = regexp.MustCompile(`'fortran_order':\s*(True|False)`)
	shapeRe   = regexp.MustCompile(`'shape':\s*\(([^)]*)\)`)
)

// ReadVoices returns each voice's style array, keyed by the .npy name without
// its extension (for example "expr-voice-3-m"), as Rows*Cols floats in row
// order. Every array must be little-endian float32, C order, (400, 256).
func ReadVoices(path string) (map[string][]float32, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	voices := make(map[string][]float32, len(zr.File))
	for _, f := range zr.File {
		v, err := readNPY(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", path, f.Name, err)
		}
		voices[strings.TrimSuffix(f.Name, ".npy")] = v
	}
	return voices, nil
}

func readNPY(f *zip.File) ([]float32, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	if len(b) < 10 || !bytes.HasPrefix(b, []byte("\x93NUMPY")) {
		return nil, fmt.Errorf("not an npy file")
	}
	var headerLen, offset int
	switch b[6] {
	case 1:
		headerLen, offset = int(binary.LittleEndian.Uint16(b[8:10])), 10
	case 2, 3:
		if len(b) < 12 {
			return nil, fmt.Errorf("truncated npy header")
		}
		headerLen, offset = int(binary.LittleEndian.Uint32(b[8:12])), 12
	default:
		return nil, fmt.Errorf("unsupported npy version %d", b[6])
	}
	if offset+headerLen > len(b) {
		return nil, fmt.Errorf("truncated npy header")
	}
	header := string(b[offset : offset+headerLen])

	if m := descrRe.FindStringSubmatch(header); m == nil || m[1] != "<f4" {
		return nil, fmt.Errorf("dtype must be '<f4', header %q", header)
	}
	if m := fortranRe.FindStringSubmatch(header); m == nil || m[1] != "False" {
		return nil, fmt.Errorf("must not be fortran_order, header %q", header)
	}
	if m := shapeRe.FindStringSubmatch(header); m == nil || strings.ReplaceAll(m[1], " ", "") != fmt.Sprintf("%d,%d", Rows, Cols) {
		return nil, fmt.Errorf("shape must be (%d, %d), header %q", Rows, Cols, header)
	}

	data := b[offset+headerLen:]
	if len(data) != Rows*Cols*4 {
		return nil, fmt.Errorf("data is %d bytes, want %d", len(data), Rows*Cols*4)
	}
	v := make([]float32, Rows*Cols)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return v, nil
}
