package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"github.com/androiddrew/gokittentts/kittentts"
)

// syncBuffer is a bytes.Buffer that handlers on other goroutines can log to.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// lines returns each JSON log line written so far, decoded.
func (b *syncBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for line := range strings.Lines(b.buf.String()) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// captureLogs sends slog's default logger to a JSON buffer for the test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	var logs syncBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &logs
}

// requestFields are the fields every request's log line carries.
var requestFields = []string{
	"model", "voice", "format", "input_chars", "chunks", "audio_seconds",
	"synthesis_seconds", "rtf", "time_to_first_audio_seconds", "queue_wait_seconds", "status",
}

// onlyRequestLine checks that logs hold exactly one line, carrying every
// request field and the given status, and returns it.
func onlyRequestLine(t *testing.T, logs *syncBuffer, status int) map[string]any {
	t.Helper()
	lines := logs.lines(t)
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %v", len(lines), lines)
	}
	line := lines[0]
	for _, k := range requestFields {
		if _, ok := line[k]; !ok {
			t.Errorf("log line has no %q: %v", k, line)
		}
	}
	if line["status"] != float64(status) {
		t.Errorf("log status %v, want %d: %v", line["status"], status, line)
	}
	return line
}

func TestOneLogLinePerRequest(t *testing.T) {
	for _, c := range []struct {
		name   string
		body   string
		setup  func(*fakeEngine)
		status int
	}{
		{"success", `{"model":"tts-1","input":"Hi.","voice":"alloy","response_format":"pcm"}`, nil, http.StatusOK},
		{"unknown model", `{"model":"tts-9","input":"Hi.","voice":"Leo"}`, nil, http.StatusBadRequest},
		{"unknown voice", `{"model":"tts-1","input":"Hi.","voice":"Nobody"}`, nil, http.StatusBadRequest},
		{"malformed body", `{`, nil, http.StatusBadRequest},
		{"engine failure", pcmRequest, func(e *fakeEngine) { e.err = errors.New("onnxruntime exploded") }, http.StatusInternalServerError},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, eng := newServer(t)
			if c.setup != nil {
				c.setup(eng)
			}
			logs := captureLogs(t)
			rec := speech(t, h, c.body)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			line := onlyRequestLine(t, logs, c.status)
			if c.status != http.StatusOK && line["error"] == nil {
				t.Errorf("an error response's log line has no error: %v", line)
			}
		})
	}
}

func TestRequestLogLineValues(t *testing.T) {
	h, eng := newServer(t)
	eng.chunks = threeChunks
	logs := captureLogs(t)
	if rec := speech(t, h, `{"model":"tts-1","input":"A. B. C.","voice":"alloy","response_format":"pcm"}`); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	line := onlyRequestLine(t, logs, http.StatusOK)
	want := map[string]any{
		"model":         "kitten-tts-mini-0.8",
		"voice":         "Bella",
		"format":        "pcm",
		"input_chars":   float64(8),
		"chunks":        float64(3),
		"audio_seconds": 4.0 / kittentts.SampleRate,
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
	for _, k := range []string{"synthesis_seconds", "rtf", "time_to_first_audio_seconds"} {
		if v, _ := line[k].(float64); v <= 0 {
			t.Errorf("%s = %v, want a positive number", k, line[k])
		}
	}
	if _, ok := line["error"]; ok {
		t.Errorf("a successful request's log line has an error: %v", line)
	}
}

func TestLogLineForFailureMidStream(t *testing.T) {
	h, eng := newServer(t)
	eng.chunks = threeChunks[:1]
	eng.err = errors.New("onnxruntime exploded")
	logs := captureLogs(t)
	assertCutOff(t, h, pcmRequest)
	waitFor(t, "the request's log line", func() bool { return len(logs.lines(t)) > 0 })
	line := onlyRequestLine(t, logs, http.StatusInternalServerError)
	if line["chunks"] != float64(1) {
		t.Errorf("chunks = %v, want 1 (the one sent before the failure)", line["chunks"])
	}
}

// statusClientClosed is the status logged for a client that disconnected.
const statusClientClosed = 499

func TestLogLineForClientThatLeft(t *testing.T) {
	h, _ := newServer(t)
	logs := captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	receiveResponse(t, "response", serveInBackground(h, ctx, pcmRequest))
	onlyRequestLine(t, logs, statusClientClosed)
}

// scrape fetches /metrics and parses it.
func scrape(t *testing.T, h http.Handler) map[string]*dto.MetricFamily {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics: status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("/metrics Content-Type %q, want Prometheus text", ct)
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(rec.Body)
	if err != nil {
		t.Fatalf("/metrics is not Prometheus text: %v", err)
	}
	return families
}

// sample returns the value of the series in family name with exactly the
// given labels: a counter or gauge's value, or a histogram's sample count.
func sample(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	f, ok := families[name]
	if !ok {
		t.Fatalf("no %s in /metrics", name)
	}
	for _, m := range f.GetMetric() {
		got := map[string]string{}
		for _, l := range m.GetLabel() {
			got[l.GetName()] = l.GetValue()
		}
		if !maps.Equal(got, labels) {
			continue
		}
		switch {
		case m.Counter != nil:
			return m.Counter.GetValue()
		case m.Gauge != nil:
			return m.Gauge.GetValue()
		case m.Histogram != nil:
			return float64(m.Histogram.GetSampleCount())
		}
	}
	t.Fatalf("no %s%v in /metrics", name, labels)
	return 0
}

const mini, nano = "kitten-tts-mini-0.8", "kitten-tts-nano-0.8-fp32"

func TestMetricFamilies(t *testing.T) {
	h, _ := newServer(t)
	families := scrape(t, h)
	for name, typ := range map[string]dto.MetricType{
		"kitten_requests_total":              dto.MetricType_COUNTER,
		"kitten_synthesis_seconds":           dto.MetricType_HISTOGRAM,
		"kitten_rtf":                         dto.MetricType_HISTOGRAM,
		"kitten_time_to_first_audio_seconds": dto.MetricType_HISTOGRAM,
		"kitten_queue_depth":                 dto.MetricType_GAUGE,
		"kitten_model_loaded":                dto.MetricType_GAUGE,
		"kitten_model_downloads_total":       dto.MetricType_COUNTER,
	} {
		f, ok := families[name]
		if !ok {
			t.Errorf("no %s in /metrics; got %v", name, slices.Sorted(maps.Keys(families)))
			continue
		}
		if f.GetType() != typ {
			t.Errorf("%s is a %v, want a %v", name, f.GetType(), typ)
		}
	}
	for _, m := range []string{mini, nano} {
		if v := sample(t, families, "kitten_model_loaded", map[string]string{"model": m, "device": "cpu"}); v != 0 {
			t.Errorf("%s loaded = %v before any request, want 0", m, v)
		}
		if v := sample(t, families, "kitten_queue_depth", map[string]string{"model": m}); v != 0 {
			t.Errorf("%s queue depth = %v when idle, want 0", m, v)
		}
	}
}

func TestMetricsCountRequests(t *testing.T) {
	h, _ := newServer(t)
	for _, body := range []string{pcmRequest, pcmRequest, `{"model":"tts-1","input":"Hi.","voice":"Nobody","response_format":"pcm"}`, `{"model":"tts-9","input":"Hi.","voice":"Leo","response_format":"xyz"}`} {
		speech(t, h, body)
	}
	families := scrape(t, h)
	for _, c := range []struct {
		labels map[string]string
		want   float64
	}{
		{map[string]string{"model": mini, "format": "pcm", "status": "200"}, 2},
		{map[string]string{"model": mini, "format": "pcm", "status": "400"}, 1},
		// Unknown names are not labels, so clients can't add series.
		{map[string]string{"model": "", "format": "", "status": "400"}, 1},
	} {
		if v := sample(t, families, "kitten_requests_total", c.labels); v != c.want {
			t.Errorf("kitten_requests_total%v = %v, want %v", c.labels, v, c.want)
		}
	}
	for _, name := range []string{"kitten_synthesis_seconds", "kitten_rtf", "kitten_time_to_first_audio_seconds"} {
		if n := sample(t, families, name, map[string]string{"model": mini}); n != 2 {
			t.Errorf("%s{model=%s} has %v samples, want 2 (the successes)", name, mini, n)
		}
	}
	if v := sample(t, families, "kitten_model_loaded", map[string]string{"model": mini, "device": "cpu"}); v != 1 {
		t.Errorf("mini loaded = %v after it served a request, want 1", v)
	}
	if v := sample(t, families, "kitten_model_loaded", map[string]string{"model": nano, "device": "cpu"}); v != 0 {
		t.Errorf("nano loaded = %v, want 0 (it never ran)", v)
	}
}

func TestMetricsAreOpenWithAuth(t *testing.T) {
	h, _ := newServerWithEnv(t, testConfig, map[string]string{"KITTEN_API_KEY": "s3cret"})
	scrape(t, h)
}

func TestMetricsCanBeTurnedOff(t *testing.T) {
	h, _ := newServerWithConfig(t, testConfig+"metrics: false\n")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/metrics with metrics off: status %d, want 404", rec.Code)
	}
}

func TestQueueDepthAndWait(t *testing.T) {
	h, eng := newServerWithConfig(t, queueConfig("1m"))
	logs := captureLogs(t)
	held := &stall{release: make(chan struct{})}
	eng.setStall(held)
	running := serveInBackground(h, context.Background(), pcmRequest)
	waitFor(t, "the first request to reach the engine", func() bool { return eng.count() == 1 })
	queued := serveInBackground(h, context.Background(), pcmRequest)
	depth := func() float64 { return sample(t, scrape(t, h), "kitten_queue_depth", map[string]string{"model": mini}) }
	waitFor(t, "a queue depth of 1", func() bool { return depth() == 1 })
	check429(t, speech(t, h, pcmRequest))

	const wait = 50 * time.Millisecond
	time.Sleep(wait)
	close(held.release)
	receiveResponse(t, "running response", running)
	if rec := receiveResponse(t, "queued response", queued); rec.Code != http.StatusOK {
		t.Fatalf("queued request: status %d: %s", rec.Code, rec.Body)
	}
	if d := depth(); d != 0 {
		t.Errorf("queue depth %v after the queue drained, want 0", d)
	}

	var waits []float64
	for _, line := range logs.lines(t) {
		if line["status"] == float64(http.StatusOK) {
			waits = append(waits, line["queue_wait_seconds"].(float64))
		}
	}
	slices.Sort(waits)
	if len(waits) != 2 || waits[0] >= wait.Seconds() || waits[1] < wait.Seconds() {
		t.Errorf("queue waits %v, want one under %v (it ran at once) and one at least %v (it queued)", waits, wait, wait)
	}
}
