// Command gokittentts is text-to-speech with the KittenTTS 0.8 models.
//
//	gokittentts serve [--config /etc/gokittentts/config.yaml]
//	gokittentts pull [--config config.yaml] [--dir models-dir] [model[,model]...]
//	gokittentts say --onnxruntime-lib <libonnxruntime.so.1.29.1> [--voice Bruno] [--out out.wav] "Hello from Go."
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/androiddrew/gokittentts/internal/audio"
	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/internal/modelstore"
	"github.com/androiddrew/gokittentts/internal/server"
	"github.com/androiddrew/gokittentts/kittentts"
)

const usage = `usage: gokittentts <command> [flags]

commands:
  serve  serve the OpenAI-compatible HTTP API
  pull   download models into the models directory
  say    synthesize text to a WAV file
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "pull":
		err = pull(os.Args[2:])
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

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "/etc/gokittentts/config.yaml", "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if cfg.LogFormat == "text" {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	} else {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	}
	if cfg.ONNXRuntimeLib == "" {
		return fmt.Errorf("%s: onnxruntime_lib is required", *configPath)
	}

	store := modelstore.New(cfg.ModelsDir, modelSources(cfg), cfg.Download)
	var models []kittentts.ModelConfig
	for name, m := range cfg.Models {
		models = append(models, kittentts.ModelConfig{
			Name:           name,
			Dir:            store.Path(name),
			Device:         m.Device,
			CUDADeviceID:   m.CUDADeviceID,
			IntraOpThreads: m.IntraOpThreads,
		})
	}
	engine, err := kittentts.NewEngine(cfg.ONNXRuntimeLib, models)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		engine.Close()
		return err
	}
	srv := &http.Server{
		Handler:           server.New(cfg, server.KittenEngine{Engine: engine, Store: store}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	slog.Info("listening", "addr", ln.Addr().String(), "default_model", cfg.DefaultModel)

	select {
	case err := <-errc:
		engine.Close()
		return err
	case <-ctx.Done():
	}
	stop() // a second signal now kills the process
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Requests may still be synthesizing, so the engine stays open; the
		// process is exiting anyway.
		return err
	}
	return engine.Close()
}

// modelSources is the pinned manifest plus the config's custom models.
func modelSources(cfg *config.Config) map[string]modelstore.Source {
	sources := maps.Clone(modelstore.Manifest)
	for name, m := range cfg.Models {
		if m.Repo != "" {
			sources[name] = modelstore.Source{Repo: m.Repo, Revision: m.Revision}
		}
	}
	return sources
}

func pull(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: gokittentts pull [flags] [model[,model]...]")
		fmt.Fprintln(fs.Output(), "\nDownloads models, checked against their pinned SHA-256s. With --config and no\nmodels named, pulls every model the config lists.")
		fs.PrintDefaults()
	}
	configPath := fs.String("config", "", "config file, for models_dir, the models to pull and custom models' repos")
	modelsDir := fs.String("dir", "", "models directory (default: the config's models_dir, or "+config.DefaultModelsDir+")")
	// Flags may follow the names, as in `pull $BAKE_MODELS --dir …`.
	var names []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		for name := range strings.SplitSeq(fs.Arg(0), ",") {
			if name != "" {
				names = append(names, name)
			}
		}
		args = fs.Args()[1:]
	}
	sources, dir := modelstore.Manifest, config.DefaultModelsDir
	if *configPath != "" {
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		sources, dir = modelSources(cfg), cfg.ModelsDir
		if len(names) == 0 {
			names = slices.Sorted(maps.Keys(cfg.Models))
		}
	}
	if *modelsDir != "" {
		dir = *modelsDir
	}
	if len(names) == 0 {
		return fmt.Errorf("pull: name the models to pull (%s), or pass --config", strings.Join(slices.Sorted(maps.Keys(modelstore.Manifest)), ", "))
	}

	store := modelstore.New(dir, sources, true)
	for _, name := range names {
		path, err := store.Ensure(context.Background(), name)
		if err != nil {
			return err
		}
		status := "already present"
		if succeeded, _ := store.Downloads(name); succeeded > 0 {
			status = "downloaded"
		}
		fmt.Fprintf(os.Stderr, "%s: %s in %s\n", name, status, path)
	}
	return nil
}

func say(args []string) error {
	fs := flag.NewFlagSet("say", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: gokittentts say [flags] <text>")
		fs.PrintDefaults()
	}
	ortLib := fs.String("onnxruntime-lib", "", "path to the versioned ONNX Runtime library, libonnxruntime.so.1.29.1 (required)")
	modelsDir := fs.String("models-dir", "models", "models directory; a missing pinned model is downloaded into it")
	model := fs.String("model", "kitten-tts-mini-0.8", "model name")
	voice := fs.String("voice", "Bruno", "Kitten voice name (Bella, Bruno, …) or expr-voice-* key")
	speed := fs.Float64("speed", 1, "speaking speed, 0.5 to 2.0")
	out := fs.String("out", "out.wav", "WAV file to write")
	md := fs.Bool("markdown", true, "read markdown as prose; --markdown=false reads the text as written")
	normalizeText := fs.Bool("normalize", true, "read numbers, dates and times as words; --normalize=false leaves them to espeak-ng")
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

	dir, err := modelstore.New(*modelsDir, modelstore.Manifest, true).Ensure(context.Background(), *model)
	if err != nil {
		return err
	}
	engine, err := kittentts.NewEngine(*ortLib, []kittentts.ModelConfig{
		{Name: *model, Dir: dir, Device: kittentts.CPU},
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
		Text:      text,
		Voice:     *voice,
		Speed:     float32(*speed),
		Markdown:  *md,
		Normalize: *normalizeText,
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
