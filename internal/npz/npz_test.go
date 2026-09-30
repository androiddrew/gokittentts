package npz_test

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/androiddrew/gokittentts/internal/npz"
)

// npy builds a .npy file the way numpy writes one: magic, version, header
// length, then a dict literal padded with spaces to a 64-byte boundary.
func npy(version byte, dict string, data []float32) []byte {
	lenField := 2
	if version >= 2 {
		lenField = 4
	}
	header := dict
	for (6+2+lenField+len(header)+1)%64 != 0 {
		header += " "
	}
	header += "\n"
	var b bytes.Buffer
	b.WriteString("\x93NUMPY")
	b.Write([]byte{version, 0})
	if lenField == 2 {
		binary.Write(&b, binary.LittleEndian, uint16(len(header)))
	} else {
		binary.Write(&b, binary.LittleEndian, uint32(len(header)))
	}
	b.WriteString(header)
	for _, v := range data {
		binary.Write(&b, binary.LittleEndian, math.Float32bits(v))
	}
	return b.Bytes()
}

func writeNPZ(t *testing.T, files map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "voices.npz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, b := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func styles() []float32 {
	v := make([]float32, 400*256)
	for i := range v {
		v[i] = float32(i) / 1000
	}
	return v
}

const goodDict = "{'descr': '<f4', 'fortran_order': False, 'shape': (400, 256), }"

func TestReadVoices(t *testing.T) {
	for _, version := range []byte{1, 2} {
		t.Run(fmt.Sprintf("npy v%d", version), func(t *testing.T) {
			path := writeNPZ(t, map[string][]byte{
				"expr-voice-2-f.npy": npy(version, goodDict, styles()),
				"expr-voice-3-m.npy": npy(version, goodDict, styles()),
			})
			voices, err := npz.ReadVoices(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(voices) != 2 {
				t.Fatalf("got %d voices, want 2", len(voices))
			}
			v := voices["expr-voice-3-m"]
			if len(v) != 400*256 || v[0] != 0 || v[256*3+7] != float32(256*3+7)/1000 {
				t.Fatalf("expr-voice-3-m: len %d, v[0]=%v, v[775]=%v", len(v), v[0], v[256*3+7])
			}
		})
	}
}

func TestReadVoicesRejects(t *testing.T) {
	cases := []struct {
		name string
		file []byte
		want string
	}{
		{"big-endian", npy(1, "{'descr': '>f4', 'fortran_order': False, 'shape': (400, 256), }", styles()), "<f4"},
		{"float64", npy(1, "{'descr': '<f8', 'fortran_order': False, 'shape': (400, 256), }", styles()), "<f4"},
		{"fortran order", npy(1, "{'descr': '<f4', 'fortran_order': True, 'shape': (400, 256), }", styles()), "fortran"},
		{"wrong shape", npy(1, "{'descr': '<f4', 'fortran_order': False, 'shape': (400, 255), }", styles()[:400*255]), "(400, 256)"},
		{"one-dimensional", npy(1, "{'descr': '<f4', 'fortran_order': False, 'shape': (102400,), }", styles()), "(400, 256)"},
		{"short data", npy(1, goodDict, styles()[:1000]), "data"},
		{"not npy", []byte("hello"), "npy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeNPZ(t, map[string][]byte{"expr-voice-2-f.npy": c.file})
			_, err := npz.ReadVoices(path)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}
