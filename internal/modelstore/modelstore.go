// Package modelstore keeps models on disk and downloads missing ones from
// Hugging Face at a pinned revision.
//
// A model's files live in <dir>/<name>/<revision>/, and <dir>/<name>/current
// is a symlink to the revision in use. A download goes into a staging
// directory beside them, every file is checked against its SHA-256 as it
// arrives, and only a complete, verified directory is renamed into place.
// A failed download leaves nothing behind.
package modelstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// File is a model file and its SHA-256, in hex.
type File struct {
	Name   string
	SHA256 string
}

// Source is where a model is downloaded from. A pinned source lists its
// files and their hashes. A custom source lists none: its file names are
// read from its config.json, and nothing is hash-checked.
type Source struct {
	Repo     string // Hugging Face repo, e.g. KittenML/kitten-tts-mini-0.8
	Revision string
	Files    []File
}

// Manifest pins the KittenTTS 0.8 models by revision and SHA-256
// (docs/ORIGINAL_SPEC.md section 3.1).
var Manifest = map[string]Source{
	"kitten-tts-mini-0.8": {"KittenML/kitten-tts-mini-0.8", "c02725660cea441db4c383af69f1f26f5cd00947", []File{
		{"config.json", "6b160bc9b19e24ecb21e84bc14f8a7da21fdf47ec72d42450bc5cf514b61804a"},
		{"kitten_tts_mini_v0_8.onnx", "0f5bbae4fc4800c98dbc544a87ecfa79510de2fb8222db30d12e5bfe9177df91"},
		{"voices.npz", "40ad2638952b77b7b2f30127e2608e169fc69dd256b53bd8aaa3409a33193c42"},
	}},
	"kitten-tts-micro-0.8": {"KittenML/kitten-tts-micro-0.8", "1ccf72b2c2048fd17efac7de2fab32d10e225084", []File{
		{"config.json", "1f0bd2208348f9211cb0da64fcd1536eb28228571cc6b09e767eb6e203a0a532"},
		{"kitten_tts_micro_v0_8.onnx", "95481626fee1ba70ce683e69c534fc7cb38433c46ce42d3abbeafb4b9f1a4123"},
		{"voices.npz", "112710c1be8ad0e967c190fb0fd95cbe5848ec4791b93209f20b28b7da20dac1"},
	}},
	"kitten-tts-nano-0.8-int8": {"KittenML/kitten-tts-nano-0.8-int8", "84781d74e29ee25217551556398b42f80593a813", []File{
		{"config.json", "b66006ccbeccd4de5fc3c9272059c47f5725df7215fd889785c03602652fab64"},
		{"kitten_tts_nano_v0_8.onnx", "f7b0afcbee92870b32b8e0276d855b954dc25470c9f051b376ac7eee537c76fc"},
		{"voices.npz", "8aa7cee235abb0739cb51e6559685f65a4dacd95568833d05699b1633f519b3f"},
	}},
	"kitten-tts-nano-0.8-fp32": {"KittenML/kitten-tts-nano-0.8-fp32", "7a1db645b1f3ab9420761d87428e042b9cec3f26", []File{
		{"config.json", "b66006ccbeccd4de5fc3c9272059c47f5725df7215fd889785c03602652fab64"},
		{"kitten_tts_nano_v0_8.onnx", "320564d2615f235de972ca27a7f39551c94185cfa24ca85b07a29084135f1e5e"},
		{"voices.npz", "8aa7cee235abb0739cb51e6559685f65a4dacd95568833d05699b1633f519b3f"},
	}},
}

// Store finds models on disk and downloads missing ones. It is safe for
// concurrent use.
type Store struct {
	BaseURL string       // Hugging Face; set by New, changed by tests
	Client  *http.Client // set by New

	dir      string
	sources  map[string]Source
	download bool

	mu      sync.Mutex
	flights map[string]*flight // downloads in progress, by model name
	counts  map[string]*counts // fixed by New
}

type flight struct {
	done chan struct{}
	err  error
}

type counts struct{ succeeded, failed atomic.Uint64 }

// New returns a Store over dir that can download the models in sources.
// With download false it only finds models already on disk.
func New(dir string, sources map[string]Source, download bool) *Store {
	s := &Store{
		BaseURL:  "https://huggingface.co",
		Client:   &http.Client{Timeout: 30 * time.Minute},
		dir:      dir,
		sources:  sources,
		download: download,
		flights:  map[string]*flight{},
		counts:   map[string]*counts{},
	}
	for name := range sources {
		s.counts[name] = &counts{}
	}
	return s
}

// Path is the directory holding the named model's files once it is on disk.
func (s *Store) Path(name string) string {
	return filepath.Join(s.dir, name, "current")
}

// Ensure returns Path(name), downloading the model's revision first if it
// isn't the one on disk. Concurrent calls for one model share a single
// download. A caller whose ctx ends stops waiting, but the download carries
// on for the others. With downloads off, whatever revision is on disk is
// used.
func (s *Store) Ensure(ctx context.Context, name string) (string, error) {
	path := s.Path(name)
	src, known := s.sources[name]
	onDisk := hasConfig(path)
	if onDisk && (!known || !s.download) {
		return path, nil
	}
	if onDisk {
		if target, err := os.Readlink(path); err == nil && target == src.Revision {
			return path, nil
		}
	}
	if !s.download {
		return "", fmt.Errorf("model %s is not in %s and downloads are off; run gokittentts pull %s", name, s.dir, name)
	}
	if !known {
		return "", fmt.Errorf("model %s is not in %s, and it is not a pinned model or one with a repo and revision to download from", name, s.dir)
	}

	s.mu.Lock()
	f, ok := s.flights[name]
	if !ok {
		f = &flight{done: make(chan struct{})}
		s.flights[name] = f
		go func() {
			var downloaded bool
			downloaded, f.err = s.fetchModel(name, src)
			switch {
			case f.err != nil:
				s.counts[name].failed.Add(1)
			case downloaded:
				s.counts[name].succeeded.Add(1)
			}
			s.mu.Lock()
			delete(s.flights, name)
			s.mu.Unlock()
			close(f.done)
		}()
	}
	s.mu.Unlock()

	select {
	case <-f.done:
		if f.err != nil {
			return "", fmt.Errorf("downloading model %s: %w", name, f.err)
		}
		return path, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Downloads reports how many downloads of the named model have succeeded
// and failed.
func (s *Store) Downloads(name string) (succeeded, failed uint64) {
	c, ok := s.counts[name]
	if !ok {
		return 0, 0
	}
	return c.succeeded.Load(), c.failed.Load()
}

func hasConfig(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "config.json"))
	return err == nil
}

// fetchModel downloads the model's revision unless it is already on disk,
// then points current at it. It reports whether it downloaded anything.
func (s *Store) fetchModel(name string, src Source) (bool, error) {
	modelDir := filepath.Join(s.dir, name)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return false, err
	}
	sweepStale(modelDir)
	revDir := filepath.Join(modelDir, src.Revision)
	downloaded := false
	if !hasConfig(revDir) {
		if err := s.fetchRevision(modelDir, revDir, src); err != nil {
			// Leave no empty model directory behind either.
			os.Remove(modelDir)
			return false, err
		}
		downloaded = true
	}
	return downloaded, pointCurrent(modelDir, src.Revision)
}

// staleAfter is how old a staging directory or temporary link must be
// before it is taken for the leftover of a process that died, rather than
// another process's download in progress.
const staleAfter = 24 * time.Hour

// sweepStale removes staging directories and temporary links in modelDir
// left by processes that died mid-download.
func sweepStale(modelDir string) {
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".") || !strings.Contains(name, ".partial-") && !strings.HasPrefix(name, ".current-") {
			continue
		}
		if info, err := os.Lstat(filepath.Join(modelDir, name)); err == nil && time.Since(info.ModTime()) > staleAfter {
			os.RemoveAll(filepath.Join(modelDir, name))
		}
	}
}

// fetchRevision downloads src's files into a staging directory and renames
// it to revDir.
func (s *Store) fetchRevision(modelDir, revDir string, src Source) error {
	staging, err := os.MkdirTemp(modelDir, "."+src.Revision+".partial-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging) // nothing left to remove after the rename

	files := src.Files
	if files == nil {
		slog.Warn("downloading a custom model without hash checks", "repo", src.Repo, "revision", src.Revision)
		if err := s.fetchFile(staging, src, File{Name: "config.json"}); err != nil {
			return err
		}
		if files, err = filesNamedBy(filepath.Join(staging, "config.json")); err != nil {
			return err
		}
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(staging, f.Name)); err == nil {
			continue // config.json of a custom model
		}
		if err := s.fetchFile(staging, src, f); err != nil {
			return err
		}
	}
	// MkdirTemp makes the directory private; the model is for every user.
	if err := os.Chmod(staging, 0o755); err != nil {
		return err
	}
	if err := os.Rename(staging, revDir); err != nil {
		// Another process may have finished the same revision first.
		if hasConfig(revDir) {
			return nil
		}
		return err
	}
	return nil
}

// filesNamedBy lists the files a custom model's config.json names, with no
// hashes.
func filesNamedBy(configPath string) ([]File, error) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		ModelFile string `json:"model_file"`
		Voices    string `json:"voices"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("config.json: %w", err)
	}
	files := []File{{Name: "config.json"}}
	for _, name := range []string{cfg.ModelFile, cfg.Voices} {
		// The names become paths, so they must stay in the model's directory.
		if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
			return nil, fmt.Errorf("config.json: bad file name %q", name)
		}
		files = append(files, File{Name: name})
	}
	return files, nil
}

// fetchFile downloads f into dir, checking its SHA-256 if it has one.
func (s *Store) fetchFile(dir string, src Source, f File) error {
	addr := fmt.Sprintf("%s/%s/resolve/%s/%s", s.BaseURL, src.Repo, url.PathEscape(src.Revision), url.PathEscape(f.Name))
	resp, err := s.Client.Get(addr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", addr, resp.Status)
	}
	out, err := os.Create(filepath.Join(dir, f.Name))
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), resp.Body)
	if err == nil {
		err = out.Sync() // on disk before the directory is renamed into place
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); f.SHA256 != "" && got != f.SHA256 {
		return fmt.Errorf("%s: SHA-256 is %s, want %s", f.Name, got, f.SHA256)
	}
	return nil
}

// pointCurrent atomically points modelDir/current at revision.
func pointCurrent(modelDir, revision string) error {
	current := filepath.Join(modelDir, "current")
	if target, err := os.Readlink(current); err == nil && target == revision {
		return nil
	}
	// Unique, so another process pointing current at the same time can't
	// remove this one's link.
	tmp := filepath.Join(modelDir, fmt.Sprintf(".current-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err := os.Symlink(revision, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, current); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
