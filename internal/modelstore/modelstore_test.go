package modelstore_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/androiddrew/gokittentts/internal/modelstore"
)

// fixture is a fake Hugging Face serving files at
// /<repo>/resolve/<revision>/<file>.
type fixture struct {
	*httptest.Server
	mu       sync.Mutex
	files    map[string][]byte // by URL path
	requests map[string]int    // by URL path
	broken   map[string]bool   // paths whose body is cut off
	hold     chan struct{}     // if set, every response waits for it
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{files: map[string][]byte{}, requests: map[string]int{}, broken: map[string]bool{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hugging Face redirects resolve URLs to its CDN.
		if p, ok := strings.CutPrefix(r.URL.Path, "/cdn"); ok {
			f.serve(w, p)
			return
		}
		http.Redirect(w, r, "/cdn"+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fixture) serve(w http.ResponseWriter, path string) {
	f.mu.Lock()
	f.requests[path]++
	body, ok := f.files[path]
	broken, hold := f.broken[path], f.hold
	f.mu.Unlock()
	if hold != nil {
		<-hold
	}
	if !ok {
		http.NotFound(w, nil)
		return
	}
	if broken {
		// Promise the whole body, send half, and hang up.
		w.Header().Set("Content-Length", "1000000")
		w.Write(body[:len(body)/2])
		panic(http.ErrAbortHandler)
	}
	w.Write(body)
}

func (f *fixture) put(repo, revision, name string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files["/"+repo+"/resolve/"+revision+"/"+name] = body
}

func (f *fixture) remove(repo, revision, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, "/"+repo+"/resolve/"+revision+"/"+name)
}

func (f *fixture) cutOff(repo, revision, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broken["/"+repo+"/resolve/"+revision+"/"+name] = true
}

func (f *fixture) count(repo, revision, name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests["/"+repo+"/resolve/"+revision+"/"+name]
}

func (f *fixture) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.requests {
		n += c
	}
	return n
}

const (
	model    = "kitten-test"
	repo     = "KittenML/kitten-test"
	revision = "0123456789abcdef"
)

// files are a small model: its config.json names its .onnx and voices files.
var files = map[string][]byte{
	"config.json":     []byte(`{"model_file": "test.onnx", "voices": "voices.npz"}`),
	"test.onnx":       bytes.Repeat([]byte("onnx"), 1000),
	"voices.npz":      bytes.Repeat([]byte("npz"), 1000),
	"not-listed.json": []byte("{}"),
}

func pinned() modelstore.Source {
	src := modelstore.Source{Repo: repo, Revision: revision}
	for _, name := range []string{"config.json", "test.onnx", "voices.npz"} {
		sum := sha256.Sum256(files[name])
		src.Files = append(src.Files, modelstore.File{Name: name, SHA256: hex.EncodeToString(sum[:])})
	}
	return src
}

func newStore(t *testing.T, f *fixture, src modelstore.Source, download bool) (*modelstore.Store, string) {
	t.Helper()
	for name, body := range files {
		f.put(src.Repo, src.Revision, name, body)
	}
	dir := t.TempDir()
	s := modelstore.New(dir, map[string]modelstore.Source{model: src}, download)
	s.BaseURL = f.URL
	return s, dir
}

// checkModel checks that the model's files are under <dir>/<name>/<revision>
// with current pointing at them, and that nothing else is under <name>.
func checkModel(t *testing.T, dir, path string) {
	t.Helper()
	if want := filepath.Join(dir, model, "current"); path != want {
		t.Errorf("path %s, want %s", path, want)
	}
	for _, name := range []string{"config.json", "test.onnx", "voices.npz"} {
		got, err := os.ReadFile(filepath.Join(dir, model, revision, name))
		if err != nil || !bytes.Equal(got, files[name]) {
			t.Errorf("%s/%s: %d bytes, %v", revision, name, len(got), err)
		}
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			t.Errorf("current/%s: %v", name, err)
		}
	}
	// Readable by other users, such as a server that didn't pull it.
	if info, err := os.Stat(filepath.Join(dir, model, revision)); err != nil || info.Mode().Perm()&0o055 != 0o055 {
		t.Errorf("%s/%s: %v, %v; want it readable by everyone", model, revision, info.Mode(), err)
	}
	if target, err := os.Readlink(filepath.Join(dir, model, "current")); err != nil || target != revision {
		t.Errorf("current points at %q (%v), want %s", target, err, revision)
	}
	if got := entries(t, filepath.Join(dir, model)); !slices.Equal(got, []string{revision, "current"}) {
		t.Errorf("%s holds %v, want [%s current]", model, got, revision)
	}
}

// entries lists the names in dir, sorted.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

func TestDownloadsAMissingModel(t *testing.T) {
	f := newFixture(t)
	s, dir := newStore(t, f, pinned(), true)
	path, err := s.Ensure(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	checkModel(t, dir, path)
	if ok, failed := s.Downloads(model); ok != 1 || failed != 0 {
		t.Errorf("downloads: %d succeeded, %d failed; want 1 and 0", ok, failed)
	}

	// Once it's on disk, nothing is fetched again.
	before := f.total()
	if _, err := s.Ensure(context.Background(), model); err != nil {
		t.Fatal(err)
	}
	if f.total() != before {
		t.Errorf("a model on disk was fetched again")
	}
	if ok, _ := s.Downloads(model); ok != 1 {
		t.Errorf("%d downloads, want 1", ok)
	}
}

func TestAModelOnDiskIsNotDownloaded(t *testing.T) {
	f := newFixture(t)
	s, dir := newStore(t, f, pinned(), false)
	rev := filepath.Join(dir, model, "hand-placed")
	if err := os.MkdirAll(rev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rev, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("hand-placed", filepath.Join(dir, model, "current")); err != nil {
		t.Fatal(err)
	}
	path, err := s.Ensure(context.Background(), model)
	if err != nil || path != filepath.Join(dir, model, "current") {
		t.Fatalf("Ensure = %q, %v", path, err)
	}
	if f.total() != 0 {
		t.Errorf("%d requests for a model on disk", f.total())
	}
}

// placeRevision puts a model's files in <dir>/<model>/<rev>, as a download
// or an admin would, and points current at it if current is set.
func placeRevision(t *testing.T, dir, rev string, current bool) {
	t.Helper()
	revDir := filepath.Join(dir, model, rev)
	if err := os.MkdirAll(revDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "test.onnx", "voices.npz"} {
		if err := os.WriteFile(filepath.Join(revDir, name), files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if current {
		if err := os.Symlink(rev, filepath.Join(dir, model, "current")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnotherRevisionOnDiskIsReplaced(t *testing.T) {
	f := newFixture(t)
	s, dir := newStore(t, f, pinned(), true)
	placeRevision(t, dir, "older", true)
	path, err := s.Ensure(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(path); target != revision {
		t.Errorf("current points at %q, want the pinned %s", target, revision)
	}
	if ok, _ := s.Downloads(model); ok != 1 {
		t.Errorf("%d downloads, want 1", ok)
	}
}

func TestOfflineUsesWhateverRevisionIsOnDisk(t *testing.T) {
	f := newFixture(t)
	s, dir := newStore(t, f, pinned(), false)
	placeRevision(t, dir, "older", true)
	path, err := s.Ensure(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(path); target != "older" || f.total() != 0 {
		t.Errorf("current points at %q after %d requests; want older and none", target, f.total())
	}
}

func TestARevisionOnDiskIsNotDownloadedAgain(t *testing.T) {
	f := newFixture(t)
	s, dir := newStore(t, f, pinned(), true)
	placeRevision(t, dir, revision, false) // e.g. current was deleted
	path, err := s.Ensure(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	checkModel(t, dir, path)
	if f.total() != 0 {
		t.Errorf("%d requests for a revision on disk", f.total())
	}
	if ok, _ := s.Downloads(model); ok != 0 {
		t.Errorf("%d downloads counted, want 0 (only current was set)", ok)
	}
}

func TestStaleStagingIsSweptAway(t *testing.T) {
	f := newFixture(t)
	s, dir := newStore(t, f, pinned(), true)
	// A download from a process that crashed a while ago.
	stale := filepath.Join(dir, model, "."+revision+".partial-123")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	path, err := s.Ensure(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	checkModel(t, dir, path)
}

func TestFailedDownloadsLeaveNothingBehind(t *testing.T) {
	for _, c := range []struct {
		name    string
		breakIt func(*fixture)
		want    string
	}{
		{"hash mismatch", func(f *fixture) { f.put(repo, revision, "test.onnx", []byte("tampered")) }, "SHA-256"},
		{"cut off", func(f *fixture) { f.cutOff(repo, revision, "test.onnx") }, "test.onnx"},
		{"not found", func(f *fixture) { f.remove(repo, revision, "voices.npz") }, "404"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			s, dir := newStore(t, f, pinned(), true)
			c.breakIt(f)
			if _, err := s.Ensure(context.Background(), model); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, c.want)
			}
			if got := entries(t, filepath.Join(dir, model)); len(got) != 0 {
				t.Errorf("a failed download left %v", got)
			}
			if ok, failed := s.Downloads(model); ok != 0 || failed != 1 {
				t.Errorf("downloads: %d succeeded, %d failed; want 0 and 1", ok, failed)
			}

			// A retry starts clean and succeeds once the files are right.
			f.mu.Lock()
			f.broken = map[string]bool{}
			f.mu.Unlock()
			for name, body := range files {
				f.put(repo, revision, name, body)
			}
			path, err := s.Ensure(context.Background(), model)
			if err != nil {
				t.Fatal(err)
			}
			checkModel(t, dir, path)
		})
	}
}

func TestConcurrentRequestsShareOneDownload(t *testing.T) {
	f := newFixture(t)
	s, dir := newStore(t, f, pinned(), true)
	f.hold = make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() { _, errs[i] = s.Ensure(context.Background(), model) })
	}
	// Let every caller reach the store before the first file arrives.
	time.Sleep(50 * time.Millisecond)
	close(f.hold)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"config.json", "test.onnx", "voices.npz"} {
		if n := f.count(repo, revision, name); n != 1 {
			t.Errorf("%s fetched %d times, want 1", name, n)
		}
	}
	checkModel(t, dir, filepath.Join(dir, model, "current"))
	if ok, _ := s.Downloads(model); ok != 1 {
		t.Errorf("%d downloads, want 1", ok)
	}
}

func TestACallerThatGivesUpDoesNotStopTheDownload(t *testing.T) {
	f := newFixture(t)
	s, dir := newStore(t, f, pinned(), true)
	f.hold = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.Ensure(ctx, model); done <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	close(f.hold)
	// The next caller joins the download still running, or finds it done.
	path, err := s.Ensure(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	checkModel(t, dir, path)
	if n := f.count(repo, revision, "test.onnx"); n != 1 {
		t.Errorf("test.onnx fetched %d times, want 1", n)
	}
}

func TestDownloadsOff(t *testing.T) {
	f := newFixture(t)
	s, _ := newStore(t, f, pinned(), false)
	_, err := s.Ensure(context.Background(), model)
	if err == nil || !strings.Contains(err.Error(), "gokittentts pull "+model) {
		t.Fatalf("err = %v, want one suggesting gokittentts pull %s", err, model)
	}
	if f.total() != 0 {
		t.Errorf("%d requests with downloads off", f.total())
	}
}

func TestUnknownModel(t *testing.T) {
	f := newFixture(t)
	s, _ := newStore(t, f, pinned(), true)
	if _, err := s.Ensure(context.Background(), "no-such-model"); err == nil || !strings.Contains(err.Error(), "repo") {
		t.Fatalf("err = %v, want one saying the model has no repo to download from", err)
	}
}

func TestCustomModelIsDownloadedWithAWarningAndNoHashCheck(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	f := newFixture(t)
	// No hashes: file names come from the downloaded config.json.
	s, dir := newStore(t, f, modelstore.Source{Repo: repo, Revision: revision}, true)
	path, err := s.Ensure(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	checkModel(t, dir, path)
	if f.count(repo, revision, "not-listed.json") != 0 {
		t.Error("fetched a file config.json doesn't name")
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "hash") {
		t.Errorf("want a warning about the missing hash checks; logs: %s", logs.String())
	}
}

func TestManifestPinsTheFourModels(t *testing.T) {
	for _, name := range []string{"kitten-tts-mini-0.8", "kitten-tts-micro-0.8", "kitten-tts-nano-0.8-int8", "kitten-tts-nano-0.8-fp32"} {
		src, ok := modelstore.Manifest[name]
		if !ok {
			t.Errorf("%s is not in the manifest", name)
			continue
		}
		if src.Repo != "KittenML/"+name || len(src.Revision) != 40 || len(src.Files) != 3 {
			t.Errorf("%s: %+v", name, src)
		}
		for _, f := range src.Files {
			if len(f.SHA256) != 64 {
				t.Errorf("%s/%s: SHA-256 %q", name, f.Name, f.SHA256)
			}
		}
	}
}
