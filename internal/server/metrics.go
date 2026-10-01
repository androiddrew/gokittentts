package server

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/androiddrew/gokittentts/internal/config"
	"github.com/androiddrew/gokittentts/kittentts"
)

// metrics are the Prometheus metrics of one server. Each server has its own
// registry, so tests can run several servers in one process.
type metrics struct {
	requests   *prometheus.CounterVec   // model, format, status
	synthesis  *prometheus.HistogramVec // model
	rtf        *prometheus.HistogramVec // model
	firstAudio *prometheus.HistogramVec // model
	loaded     *prometheus.GaugeVec     // model, device
	downloads  *prometheus.CounterVec   // model, status; counted once the model store lands
	devices    map[string]string        // by model name
	handler    http.Handler
}

// newMetrics registers every metric, with a zero series for each configured
// model and supported format so dashboards and alerts see them before the
// first request.
func newMetrics(cfg *config.Config, queues map[string]*queue, formats []string) *metrics {
	reg := prometheus.NewRegistry()
	f := promauto.With(reg)
	histogram := func(name, help string, buckets []float64) *prometheus.HistogramVec {
		return f.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets}, []string{"model"})
	}
	m := &metrics{
		requests: f.NewCounterVec(prometheus.CounterOpts{
			Name: "kitten_requests_total",
			Help: "Speech requests by model, response format and status. Unknown models and formats have empty labels; 499 is a client that disconnected.",
		}, []string{"model", "format", "status"}),
		synthesis: histogram("kitten_synthesis_seconds",
			"Time a successful request spent in the model, excluding queue wait and encoding.",
			[]float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120}),
		rtf: histogram("kitten_rtf",
			"Real-time factor of successful requests: synthesis seconds per second of audio.",
			[]float64{.05, .1, .2, .3, .4, .5, .6, .8, 1, 1.5, 2, 5}),
		firstAudio: histogram("kitten_time_to_first_audio_seconds",
			"Time from a successful request's arrival to its first audio, including queue wait.",
			[]float64{.05, .1, .25, .5, .75, 1, 1.5, 2, 5, 10, 30}),
		loaded: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kitten_model_loaded",
			Help: "1 once a model has synthesized, so it is loaded; 0 before.",
		}, []string{"model", "device"}),
		downloads: f.NewCounterVec(prometheus.CounterOpts{
			Name: "kitten_model_downloads_total",
			Help: "Model downloads by status.",
		}, []string{"model", "status"}),
		devices: make(map[string]string, len(cfg.Models)),
	}
	for name, model := range cfg.Models {
		q := queues[name]
		f.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "kitten_queue_depth",
			Help:        "Requests waiting for a model behind the running one.",
			ConstLabels: prometheus.Labels{"model": name},
		}, func() float64 { return float64(q.waiting.Load()) })

		device := string(model.Device)
		if device == "" {
			device = string(kittentts.CPU)
		}
		m.devices[name] = device
		m.loaded.WithLabelValues(name, device)
		m.synthesis.WithLabelValues(name)
		m.rtf.WithLabelValues(name)
		m.firstAudio.WithLabelValues(name)
		for _, format := range formats {
			m.requests.WithLabelValues(name, format, strconv.Itoa(http.StatusOK))
		}
		for _, status := range []string{"success", "failure"} {
			m.downloads.WithLabelValues(name, status)
		}
	}
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m.handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	return m
}

// observe counts a finished speech request.
func (m *metrics) observe(rec *requestRecord) {
	m.requests.WithLabelValues(rec.modelLabel, rec.formatLabel, strconv.Itoa(rec.status)).Inc()
	if rec.chunks > 0 {
		m.loaded.WithLabelValues(rec.model, m.devices[rec.model]).Set(1)
	}
	if rec.status != http.StatusOK {
		return
	}
	m.synthesis.WithLabelValues(rec.model).Observe(rec.synthesis.Seconds())
	m.rtf.WithLabelValues(rec.model).Observe(rec.rtf())
	m.firstAudio.WithLabelValues(rec.model).Observe(rec.firstAudio.Seconds())
}
