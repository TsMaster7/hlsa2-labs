// Package metrics centralises the Prometheus metrics used by all the
// lab's services. Every binary that exposes /metrics shares this
// vocabulary so the recording rules and the Sync/Async Overview
// dashboard can stay simple.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Standard latency histogram buckets - cover sub-ms to 30s so step 5's
// async event-to-effect latency stays on-scale even when backpressure
// is disabled and the queue diverges.
var latencyBuckets = []float64{
	0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1,
	0.25, 0.5, 1, 2.5, 5, 10, 30,
}

// HTTPMetrics holds the standard RED counters for an HTTP server.
type HTTPMetrics struct {
	Service        string
	RequestsTotal  *prometheus.CounterVec
	RequestSeconds *prometheus.HistogramVec
	BytesSent      *prometheus.CounterVec
	BytesReceived  *prometheus.CounterVec
}

func NewHTTPMetrics(service string) *HTTPMetrics {
	labels := []string{"service", "endpoint", "method", "code"}
	return &HTTPMetrics{
		Service: service,
		RequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "lab32_http_requests_total",
			Help: "HTTP requests handled, broken out by service/endpoint/method/code.",
		}, labels),
		RequestSeconds: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lab32_http_request_duration_seconds",
			Help:    "Server-side HTTP request duration.",
			Buckets: latencyBuckets,
		}, labels),
		BytesSent: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "lab32_http_response_bytes_total",
			Help: "Total bytes written to HTTP responses.",
		}, []string{"service", "endpoint"}),
		BytesReceived: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "lab32_http_request_bytes_total",
			Help: "Total bytes read from HTTP request bodies.",
		}, []string{"service", "endpoint"}),
	}
}

// statusRecorder wraps http.ResponseWriter so the middleware can read
// the response status + bytes written without callers having to set
// them explicitly.
type statusRecorder struct {
	http.ResponseWriter
	code  int
	bytes int
}

func (s *statusRecorder) WriteHeader(c int) {
	s.code = c
	s.ResponseWriter.WriteHeader(c)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Middleware returns a net/http middleware that records RED metrics
// for every request. Skip endpoints can be passed as a set; /metrics
// and /healthz typically belong there.
func (m *HTTPMetrics) Middleware(skip map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip != nil && skip[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
			next.ServeHTTP(rec, r)
			elapsed := time.Since(start).Seconds()

			labels := prometheus.Labels{
				"service":  m.Service,
				"endpoint": r.URL.Path,
				"method":   r.Method,
				"code":     strconv.Itoa(rec.code),
			}
			m.RequestsTotal.With(labels).Inc()
			m.RequestSeconds.With(labels).Observe(elapsed)
			m.BytesSent.With(prometheus.Labels{
				"service":  m.Service,
				"endpoint": r.URL.Path,
			}).Add(float64(rec.bytes))
			if r.ContentLength > 0 {
				m.BytesReceived.With(prometheus.Labels{
					"service":  m.Service,
					"endpoint": r.URL.Path,
				}).Add(float64(r.ContentLength))
			}
		})
	}
}

// Handler returns a ready-to-mount Prometheus /metrics handler.
func Handler() http.Handler {
	return promhttp.Handler()
}

// GRPCRequest is a small helper for the lookup-svc gRPC server to
// record the same RED-shaped counter family with method="gRPC".
type GRPCMetrics struct {
	RequestsTotal  *prometheus.CounterVec
	RequestSeconds *prometheus.HistogramVec
	BytesSent      *prometheus.CounterVec
	BytesReceived  *prometheus.CounterVec
}

func NewGRPCMetrics(service string) *GRPCMetrics {
	labels := []string{"service", "endpoint", "method", "code"}
	g := &GRPCMetrics{
		RequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "lab32_grpc_requests_total",
			Help: "gRPC unary calls handled.",
		}, labels),
		RequestSeconds: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lab32_grpc_request_duration_seconds",
			Help:    "Server-side gRPC request duration.",
			Buckets: latencyBuckets,
		}, labels),
		BytesSent: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "lab32_grpc_response_bytes_total",
			Help: "Total bytes serialized into gRPC response messages.",
		}, []string{"service", "endpoint"}),
		BytesReceived: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "lab32_grpc_request_bytes_total",
			Help: "Total bytes deserialized from gRPC request messages.",
		}, []string{"service", "endpoint"}),
	}
	// Seed labels we always emit so empty PromQL queries don't 404.
	_ = service
	return g
}

// Observe records one unary call.
func (g *GRPCMetrics) Observe(service, endpoint, code string, elapsed time.Duration, reqBytes, respBytes int) {
	labels := prometheus.Labels{
		"service":  service,
		"endpoint": endpoint,
		"method":   "gRPC",
		"code":     code,
	}
	g.RequestsTotal.With(labels).Inc()
	g.RequestSeconds.With(labels).Observe(elapsed.Seconds())
	g.BytesSent.With(prometheus.Labels{"service": service, "endpoint": endpoint}).Add(float64(respBytes))
	g.BytesReceived.With(prometheus.Labels{"service": service, "endpoint": endpoint}).Add(float64(reqBytes))
}
