// Command gokittentts is text-to-speech with the KittenTTS 0.8 models.
//
//	gokittentts say --onnxruntime-lib <libonnxruntime.so.1.29.1> [--voice Bruno] [--out out.wav] "Hello from Go."
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/androiddrew/gokittentts/internal/audio"
	"github.com/androiddrew/gokittentts/kittentts"
)

const usage = `usage: gokittentts <command> [flags]

commands:
  say   synthesize text to a WAV file
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "say":
		err = say(os.Args[2:])
	case "-h", "-help", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "gokittentts: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gokittentts:", err)
		os.Exit(1)
	}
}

func say(args []string) error {
	fs := flag.NewFlagSet("say", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: gokittentts say [flags] <text>")
		fs.PrintDefaults()
	}
	ortLib := fs.String("onnxruntime-lib", "", "path to the versioned ONNX Runtime library, libonnxruntime.so.1.29.1 (required)")
	modelsDir := fs.String("models-dir", "models", "directory holding one directory per model")
	model := fs.String("model", "kitten-tts-mini-0.8", "model name, a directory under --models-dir")
	voice := fs.String("voice", "Leo", "Kitten voice name (Bella, Bruno, …) or expr-voice-* key")
	speed := fs.Float64("speed", 1, "speaking speed, 0.5 to 2.0")
	out := fs.String("out", "out.wav", "WAV file to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text := strings.Join(fs.Args(), " ")
	switch {
	case *ortLib == "":
		return errors.New("say: --onnxruntime-lib is required")
	case strings.TrimSpace(text) == "":
		return errors.New("say: no text given")
	case *speed < 0.5 || *speed > 2:
		return fmt.Errorf("say: --speed %v is outside 0.5 to 2.0", *speed)
	}

	engine, err := kittentts.NewEngine(*ortLib, []kittentts.ModelConfig{
		{Name: *model, Dir: filepath.Join(*modelsDir, *model), Device: kittentts.CPU},
	})
	if err != nil {
		return err
	}
	defer engine.Close()
	m, err := engine.Model(*model)
	if err != nil {
		return err
	}
	pcm, err := m.Synthesize(context.Background(), kittentts.Request{
		Text:  text,
		Voice: *voice,
		Speed: float32(*speed),
	})
	if err != nil {
		return err
	}

	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	if err := audio.WriteWAV(f, pcm, kittentts.SampleRate); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %.2f s of audio\n", *out, float64(len(pcm))/kittentts.SampleRate)
	return nil
}
