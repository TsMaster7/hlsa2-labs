// Package middleware implements the RED instrumentation for the SUT.
//
// Cardinality is bounded to three labels:
//
//	endpoint     -> the route template (never the raw URL path)
//	method       -> HTTP verb
//	status_class -> 2xx | 3xx | 4xx | 5xx | other
//
// Latency buckets are calibrated to the per-endpoint thresholds the
// k6 baseline.js asserts on: 50ms / 150ms / 500ms.
package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var latencyBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests handled by the SUT.",
		},
		[]string{"endpoint", "method", "status_class"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds, observed server-side.",
			Buckets: latencyBuckets,
		},
		[]string{"endpoint", "method"},
	)

	httpInflight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_inflight_requests",
			Help: "Currently in-flight HTTP requests on the SUT (instrumented routes only).",
		},
	)
)

func statusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

// instrumented returns false for routes we deliberately exclude from
// the SLI (health, metrics scrape).
func instrumented(routeTemplate string) bool {
	switch routeTemplate {
	case "/healthz", "/metrics", "":
		return false
	default:
		return true
	}
}

// RED is a chi middleware that observes per-endpoint latency, count,
// and inflight gauge. The endpoint label is the chi route template
// (e.g. "/medium"), not the instantiated URL path.
//
// Inflight is incremented before the handler runs and decremented
// after; latency and count are recorded once on completion. The route
// template is read after the chain runs because chi only populates
// RouteContext during routing.
func RED(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		httpInflight.Inc()
		defer httpInflight.Dec()

		next.ServeHTTP(rec, r)

		routeTemplate := chi.RouteContext(r.Context()).RoutePattern()
		if !instrumented(routeTemplate) {
			return
		}

		dur := time.Since(start).Seconds()
		httpRequestDuration.WithLabelValues(routeTemplate, r.Method).Observe(dur)
		httpRequestsTotal.WithLabelValues(routeTemplate, r.Method, statusClass(rec.status)).Inc()
	})
}
