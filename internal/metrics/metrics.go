// Package metrics exposes operational metrics in the Prometheus format
// (ADR-0016) on a registry of its own, so nothing a library registers
// globally ends up in them.
//
// Labels are bounded: a request is labelled with its route pattern from the
// route table, never with its path, so item IDs cannot multiply the series.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Sources are the parts of the running server the metrics read when they
// are scraped. Each may be nil.
type Sources struct {
	// Items counts the items per presence state (live, trashed, purged).
	Items func() map[string]int
	// Live reports the open live editing sessions and their clients.
	Live func() (sessions, clients int)
	// Storage reports the bytes per data file.
	Storage func() map[string]int64
	// Disk reports the data directory's file system as last measured
	// (ADR-0150): free and total bytes, the level (0 ok, 1 warning, 2
	// critical, 3 full) and the bytes of the attachments kept.
	Disk func() (free, total uint64, level int, attachments int64)
	// Engines reports the task links of each process engine (ADR-0151):
	// when its tasks were last read in full, its failed rounds since the
	// start, and how many people are connected to it.
	Engines func() []EngineRounds
	// Build names the running version.
	Build string
}

// EngineRounds are the task link rounds of one engine.
type EngineRounds struct {
	Engine    string
	Read      time.Time
	Failed    int
	Connected int
}

// Metrics is the server's metric registry.
type Metrics struct {
	reg      *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	search   prometheus.Histogram
}

// New registers the server's metrics, the Go runtime's and the process's.
func New(src Sources) *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wwbb_http_requests_total", Help: "HTTP requests by route pattern, method and status code.",
		}, []string{"route", "method", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "wwbb_http_request_duration_seconds", Help: "Time to answer an HTTP request, by route pattern; event streams are not included.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		search: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "wwbb_search_duration_seconds", Help: "Time to scan the state for one search (ADR-0033).",
			Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		}),
	}
	reg.MustRegister(m.requests, m.duration, m.search,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "wwbb_build_info", Help: "Always 1; the label names the running version.",
			ConstLabels: prometheus.Labels{"version": src.Build},
		}, func() float64 { return 1 }),
	)
	if src.Items != nil {
		reg.MustRegister(&mapGauge{
			desc: prometheus.NewDesc("wwbb_items", "Items by presence state.", []string{"presence"}, nil),
			read: func() map[string]float64 {
				out := map[string]float64{}
				for k, v := range src.Items() {
					out[k] = float64(v)
				}
				return out
			},
		})
	}
	if src.Storage != nil {
		reg.MustRegister(&mapGauge{
			desc: prometheus.NewDesc("wwbb_storage_bytes", "Bytes of each data file.", []string{"file"}, nil),
			read: func() map[string]float64 {
				out := map[string]float64{}
				for k, v := range src.Storage() {
					out[k] = float64(v)
				}
				return out
			},
		})
	}
	if src.Disk != nil {
		reg.MustRegister(
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "wwbb_disk_free_bytes", Help: "Free bytes of the data directory's file system (ADR-0150)."},
				func() float64 { f, _, _, _ := src.Disk(); return float64(f) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "wwbb_disk_total_bytes", Help: "Size in bytes of the data directory's file system."},
				func() float64 { _, t, _, _ := src.Disk(); return float64(t) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "wwbb_disk_level", Help: "How close the file system is to full: 0 ok, 1 warning, 2 critical (uploads refused), 3 full (read-only)."},
				func() float64 { _, _, l, _ := src.Disk(); return float64(l) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "wwbb_attachments_bytes", Help: "Bytes of the attachment files kept, album versions included."},
				func() float64 { _, _, _, a := src.Disk(); return float64(a) }),
		)
	}
	if src.Engines != nil {
		by := func(value func(EngineRounds) float64) func() map[string]float64 {
			return func() map[string]float64 {
				out := map[string]float64{}
				for _, e := range src.Engines() {
					out[e.Engine] = value(e)
				}
				return out
			}
		}
		reg.MustRegister(
			&mapGauge{desc: prometheus.NewDesc("wwbb_tasklinks_last_read_timestamp_seconds", "When each engine's tasks were last read in full, as Unix time (ADR-0151); 0 before the first round.", []string{"engine"}, nil),
				read: by(func(e EngineRounds) float64 {
					if e.Read.IsZero() {
						return 0
					}
					return float64(e.Read.Unix())
				})},
			&mapGauge{desc: prometheus.NewDesc("wwbb_tasklinks_failed_rounds", "Failed task link rounds of each engine since the start.", []string{"engine"}, nil),
				read: by(func(e EngineRounds) float64 { return float64(e.Failed) })},
			&mapGauge{desc: prometheus.NewDesc("wwbb_engine_connections", "People whose accounts are connected to each engine.", []string{"engine"}, nil),
				read: by(func(e EngineRounds) float64 { return float64(e.Connected) })},
		)
	}
	if src.Live != nil {
		reg.MustRegister(
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "wwbb_live_sessions", Help: "Open live editing sessions (ADR-0010)."},
				func() float64 { s, _ := src.Live(); return float64(s) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "wwbb_live_clients", Help: "Connections in live editing sessions."},
				func() float64 { _, c := src.Live(); return float64(c) }),
		)
	}
	return m
}

// Handler serves the metrics in the Prometheus exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{Registry: m.reg})
}

// ObserveSearch records the duration of one search.
func (m *Metrics) ObserveSearch(d time.Duration) { m.search.Observe(d.Seconds()) }

// Instrument counts the requests of one route and times them, unless
// stream is set: a long-lived event stream's duration says nothing about
// latency.
func (m *Metrics) Instrument(route string, stream bool, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		defer func() {
			m.requests.WithLabelValues(route, r.Method, strconv.Itoa(sw.code)).Inc()
			if !stream {
				m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
			}
		}()
		h.ServeHTTP(sw, r)
	})
}

// statusWriter remembers the status code. Unwrap lets
// http.ResponseController reach the connection, which event streams and
// long downloads need to flush and extend their deadlines.
type statusWriter struct {
	http.ResponseWriter
	code    int
	written bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.written {
		w.code, w.written = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// mapGauge is a gauge with one label whose values are read at scrape time.
type mapGauge struct {
	desc *prometheus.Desc
	read func() map[string]float64
}

func (g *mapGauge) Describe(ch chan<- *prometheus.Desc) { ch <- g.desc }

func (g *mapGauge) Collect(ch chan<- prometheus.Metric) {
	for label, v := range g.read() {
		ch <- prometheus.MustNewConstMetric(g.desc, prometheus.GaugeValue, v, label)
	}
}
