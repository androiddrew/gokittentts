// Command gokittentts is text-to-speech with the KittenTTS 0.8 models.
//
//	gokittentts serve [--config /etc/gokittentts/config.yaml]
//	gokittentts pull [--config config.yaml] [--dir models-dir] [model[,model]...]
//	gokittentts say --onnxruntime-lib <libonnxruntime.so.1.29.1> [--device cuda] [--voice Bruno] [--out out.wav] "Hello from Go."
//	gokittentts bench --onnxruntime-lib <libonnxruntime.so.1.29.1> [--model kitten-tts-nano-0.8-fp32] [--device cuda] [--out results.json]
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
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/androiddrew/gokittentts/internal/audio"
	benchpkg "github.com/androiddrew/gokittentts/internal/bench"
	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/internal/modelstore"
	"github.com/androiddrew/gokittentts/internal/server"
	"github.com/androiddrew/gokittentts/kittentts"
)

// revision is the gokittentts commit, stamped by the image builds with
// -ldflags "-X main.revision=…"; otherwise it comes from the build info.
var revision string

const usage = `usage: gokittentts <command> [flags]

commands:
  serve  serve the OpenAI-compatible HTTP API
  pull   download models into the models directory
  say    synthesize text to a WAV file
  bench  measure real-time factor and time to first audio against the release targets
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
	case "bench":
		err = bench(os.Args[2:])
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	kitten := server.KittenEngine{Engine: engine, Store: store}
	for _, name := range slices.Sorted(maps.Keys(cfg.Models)) {
		if m := cfg.Models[name]; m.Preload {
			if err := kitten.Load(ctx, name); err != nil {
				engine.Close()
				return fmt.Errorf("preloading %s: %w", name, err)
			}
			slog.Info("preloaded", "model", name, "device", m.Device)
		}
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		engine.Close()
		return err
	}
	srv := &http.Server{
		Handler:           server.New(cfg, kitten),
		ReadHeaderTimeout: 10 * time.Second,
	}
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
	device := fs.String("device", "cpu", "cpu, or cuda with a CUDA build of ONNX Runtime")
	cudaDeviceID := fs.Int("cuda-device-id", 0, "the GPU to use with --device cuda")
	voice := fs.String("voice", "Bruno", "Kitten voice name (Bella, Bruno, …) or expr-voice-* key")
	speed := fs.Float64("speed", 1, "speaking speed, 0.5 to 2.0")
	out := fs.String("out", "out.wav", "WAV file to write")
	md := fs.Bool("markdown", true, "read markdown as prose; --markdown=false reads the text as written")
	normalizeText := fs.Bool("normalize", true, "read numbers, dates, money, units and URLs as words; --normalize=false leaves them to espeak-ng")
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
	case kittentts.Device(*device) != kittentts.CPU && kittentts.Device(*device) != kittentts.CUDA:
		return fmt.Errorf("say: --device %q is not cpu or cuda", *device)
	case *cudaDeviceID < 0:
		return fmt.Errorf("say: --cuda-device-id %d is negative", *cudaDeviceID)
	}

	dir, err := modelstore.New(*modelsDir, modelstore.Manifest, true).Ensure(context.Background(), *model)
	if err != nil {
		return err
	}
	engine, err := kittentts.NewEngine(*ortLib, []kittentts.ModelConfig{
		{Name: *model, Dir: dir, Device: kittentts.Device(*device), CUDADeviceID: *cudaDeviceID},
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

func bench(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: gokittentts bench [flags]")
		fmt.Fprintln(fs.Output(), "\nSynthesizes the built-in corpus and reports p50 and p95 real-time factor and\ntime to first audio. Exits 1 when either p95 is at or above its target. With\n--config, the runtime, models directory, default model and its device come\nfrom the config; flags override them.")
		fs.PrintDefaults()
	}
	configPath := fs.String("config", "", "config file to take the runtime, models and devices from")
	ortLib := fs.String("onnxruntime-lib", "", "path to the versioned ONNX Runtime library, libonnxruntime.so.1.29.1 (required without --config)")
	modelsDir := fs.String("models-dir", "models", "models directory; a missing pinned model is downloaded into it")
	model := fs.String("model", "kitten-tts-mini-0.8", "model name")
	device := fs.String("device", "cpu", "cpu, or cuda with a CUDA build of ONNX Runtime")
	cudaDeviceID := fs.Int("cuda-device-id", 0, "the GPU to use with --device cuda")
	threads := fs.Int("intra-op-threads", 0, "ONNX Runtime intra-op threads; 0 is its default")
	voice := fs.String("voice", "Bruno", "Kitten voice name (Bella, Bruno, …) or expr-voice-* key")
	runs := fs.Int("runs", 3, "passes over the corpus")
	maxRTF := fs.Float64("max-rtf", benchpkg.DefaultTargets.RTF, "fail unless the p95 real-time factor is under this")
	maxFirstAudio := fs.Duration("max-first-audio", time.Duration(benchpkg.DefaultTargets.FirstAudioSeconds*float64(time.Second)), "fail unless the p95 time to first audio is under this")
	out := fs.String("out", "", "also write the results record as JSON to this file; - writes it to stdout and the summary to stderr")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("bench: unexpected argument %q", fs.Arg(0))
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	sources, download := modelstore.Manifest, true
	if *configPath != "" {
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		sources, download = modelSources(cfg), cfg.Download
		if !set["onnxruntime-lib"] {
			*ortLib = cfg.ONNXRuntimeLib
		}
		if !set["models-dir"] {
			*modelsDir = cfg.ModelsDir
		}
		if !set["model"] {
			*model = cfg.DefaultModel
		}
		m, ok := cfg.Models[*model]
		if !ok {
			return fmt.Errorf("bench: model %q is not in %s", *model, *configPath)
		}
		if !set["device"] && m.Device != "" {
			*device = string(m.Device)
		}
		if !set["cuda-device-id"] {
			*cudaDeviceID = m.CUDADeviceID
		}
		if !set["intra-op-threads"] {
			*threads = m.IntraOpThreads
		}
	}
	switch {
	case *ortLib == "":
		return errors.New("bench: --onnxruntime-lib is required (or onnxruntime_lib in --config)")
	case kittentts.Device(*device) != kittentts.CPU && kittentts.Device(*device) != kittentts.CUDA:
		return fmt.Errorf("bench: --device %q is not cpu or cuda", *device)
	case *cudaDeviceID < 0:
		return fmt.Errorf("bench: --cuda-device-id %d is negative", *cudaDeviceID)
	case *threads < 0:
		return fmt.Errorf("bench: --intra-op-threads %d is negative", *threads)
	case *runs < 1:
		return fmt.Errorf("bench: --runs %d is less than 1", *runs)
	case *maxRTF <= 0 || *maxFirstAudio <= 0:
		return errors.New("bench: --max-rtf and --max-first-audio must be positive")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dir, err := modelstore.New(*modelsDir, sources, download).Ensure(ctx, *model)
	if err != nil {
		return err
	}
	engine, err := kittentts.NewEngine(*ortLib, []kittentts.ModelConfig{
		{Name: *model, Dir: dir, Device: kittentts.Device(*device), CUDADeviceID: *cudaDeviceID, IntraOpThreads: *threads},
	})
	if err != nil {
		return err
	}
	defer engine.Close()
	m, err := engine.Model(*model)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "benchmarking %s on %s…\n", *model, *device)
	res, err := benchpkg.Run(ctx, m, benchpkg.Corpus(), benchpkg.Options{
		Voice:   *voice,
		Runs:    *runs,
		Targets: benchpkg.Targets{RTF: *maxRTF, FirstAudioSeconds: maxFirstAudio.Seconds()},
	})
	if err != nil {
		return err
	}

	host, _ := os.Hostname()
	rec := benchpkg.Record{
		Date:       time.Now().UTC(),
		Revision:   buildRevision(),
		Host:       host,
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		ORTVersion: kittentts.ORTVersion,
		Model:      *model,
		Device:     *device,
		Voice:      *voice,
		Result:     res,
	}
	switch *out {
	case "":
		rec.WriteSummary(os.Stdout)
	case "-":
		rec.WriteSummary(os.Stderr)
		if err := rec.WriteJSON(os.Stdout); err != nil {
			return err
		}
	default:
		rec.WriteSummary(os.Stdout)
		if err := writeRecord(*out, rec); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "wrote", *out)
	}
	if !res.Pass() {
		return errors.New("bench: missed the targets")
	}
	return nil
}

func writeRecord(path string, rec benchpkg.Record) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := rec.WriteJSON(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// buildRevision is the stamped revision, or the VCS revision Go recorded at
// build time, marked -dirty for a modified tree.
func buildRevision() string {
	if revision != "" {
		return revision
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "unknown", ""
	for _, s := range info.Settings {
		switch {
		case s.Key == "vcs.revision":
			rev = s.Value
		case s.Key == "vcs.modified" && s.Value == "true":
			dirty = "-dirty"
		}
	}
	return rev + dirty
}
