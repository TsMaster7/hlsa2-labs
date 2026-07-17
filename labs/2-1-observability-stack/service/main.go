// HLSA2 Lab 2-1 — instrumented API service (system under observation).
//
// Exposes:
//
//	:8080  application API   (/api/orders...)  + /healthz + /admin/inject
//	:9090  /metrics          Prometheus text format, scraped by Telegraf
//
// Emits RED metrics (rate/errors/duration) and USE metrics (utilisation +
// saturation of a bounded worker pool and a bounded DB connection pool), plus
// structured JSON logs (stdout + /var/log/app/service.log) carrying trace_id
// and level for Promtail -> Loki.
//
// Saturation model: the DB connection pool is small (DB_POOL_SIZE). When
// offered load exceeds it, requests block waiting for a connection
// (db_connections_waiting rises), latency climbs past the 200ms SLO, and once
// the acquire timeout is hit the request 5xxes — driving the alert rules.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---------------------------------------------------------------------------
// RED metrics
// ---------------------------------------------------------------------------
var (
	// R — Request rate.
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests by route, method, and status class",
		},
		[]string{"route", "method", "status_class"}, // status_class: 2xx/4xx/5xx
	)

	// D — Duration (histogram; buckets calibrated to a 200ms p99 SLO).
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_ms",
			Help:    "HTTP request latency in milliseconds",
			Buckets: []float64{5, 10, 25, 50, 75, 100, 150, 200, 300, 500, 1000},
		},
		[]string{"route", "method"},
	)

	// E — Errors (5xx specifically).
	httpErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_errors_total",
			Help: "Total HTTP 5xx errors by route",
		},
		[]string{"route", "error_type"},
	)
)

// ---------------------------------------------------------------------------
// USE metrics
// ---------------------------------------------------------------------------
var (
	// U — Utilisation: handler goroutines currently in flight.
	goroutinesActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "goroutines_active",
		Help: "Number of goroutines currently processing requests",
	})

	// S — Saturation: requests waiting for a worker slot.
	requestsQueued = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "requests_queued",
		Help: "Number of requests waiting in the accept queue",
	})

	// Connection pool USE metrics.
	dbConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_connections_active",
		Help: "Number of active database connections",
	})
	dbConnectionsWaiting = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_connections_waiting",
		Help: "Number of requests waiting for a DB connection (saturation signal)",
	})

	// E — Errors at the resource level.
	dbQueryErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "db_query_errors_total",
			Help: "Total failed DB queries by operation type",
		},
		[]string{"operation"},
	)
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------
type config struct {
	httpAddr       string
	metricsAddr    string
	dbPoolSize     int
	maxWorkers     int
	dbQueryMS      int
	dbJitterMS     int
	acquireTimeout time.Duration
	baseFailRatio  float64
	logPath        string
}

func loadConfig() config {
	return config{
		httpAddr:       envStr("HTTP_ADDR", ":8080"),
		metricsAddr:    envStr("METRICS_ADDR", ":9090"),
		dbPoolSize:     envInt("DB_POOL_SIZE", 8),
		maxWorkers:     envInt("MAX_WORKERS", 64),
		dbQueryMS:      envInt("DB_QUERY_MS", 20),
		dbJitterMS:     envInt("DB_JITTER_MS", 30),
		acquireTimeout: time.Duration(envInt("DB_ACQUIRE_TIMEOUT_MS", 500)) * time.Millisecond,
		baseFailRatio:  envFloat("BASE_FAIL_RATIO", 0.0),
		logPath:        envStr("LOG_PATH", "/var/log/app/service.log"),
	}
}

// ---------------------------------------------------------------------------
// Bounded resources
// ---------------------------------------------------------------------------

// dbPool is a semaphore modelling a fixed-size DB connection pool.
type dbPool struct{ slots chan struct{} }

func newDBPool(n int) *dbPool { return &dbPool{slots: make(chan struct{}, n)} }

// acquire blocks up to timeout for a connection. Returns false on timeout.
func (p *dbPool) acquire(timeout time.Duration) bool {
	dbConnectionsWaiting.Inc()
	defer dbConnectionsWaiting.Dec()
	select {
	case p.slots <- struct{}{}:
		dbConnectionsActive.Inc()
		return true
	case <-time.After(timeout):
		return false
	}
}

func (p *dbPool) release() {
	<-p.slots
	dbConnectionsActive.Dec()
}

// workerPool is a semaphore modelling a fixed application concurrency limit.
type workerPool struct{ slots chan struct{} }

func newWorkerPool(n int) *workerPool { return &workerPool{slots: make(chan struct{}, n)} }

func (w *workerPool) acquire() {
	requestsQueued.Inc()
	w.slots <- struct{}{}
	requestsQueued.Dec()
}

func (w *workerPool) release() { <-w.slots }

// injState holds runtime-tunable fault injection (flipped via /admin/inject).
type injState struct {
	mu         sync.RWMutex
	failRatio  float64
	extraLatMS int
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------
type server struct {
	cfg     config
	db      *dbPool
	workers *workerPool
	inj     *injState
}

type ctxKey int

const traceIDKey ctxKey = 0

func traceIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(traceIDKey).(string)
	return v
}

// statusRecorder captures the response status for RED metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// observability instruments every request: trace_id, RED metrics, in-flight
// gauge, and a structured JSON access log line.
func (s *server) observability(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		traceID := r.Header.Get("X-Trace-Id")
		if traceID == "" {
			traceID = genTraceID()
		}
		spanID := genSpanID()
		w.Header().Set("X-Trace-Id", traceID)

		// Request-scoped logger: handler logs inherit these fields, so a
		// db-error line and its request line share the same trace_id.
		reqLog := Logger.With(
			slog.String("service", "api-service"),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("trace_id", traceID),
			slog.String("span_id", spanID),
			slog.String("request_id", r.Header.Get("X-Request-ID")),
		)
		ctx := ctxWithLogger(context.WithValue(r.Context(), traceIDKey, traceID), reqLog)
		r = r.WithContext(ctx)

		goroutinesActive.Inc()
		defer goroutinesActive.Dec()

		ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)

		durationMs := float64(time.Since(start).Milliseconds())
		route := chi.RouteContext(r.Context()).RoutePattern() // router template, never the raw path
		if route == "" {
			route = "unknown"
		}
		statusClass := fmt.Sprintf("%dxx", ww.status/100)

		httpRequestsTotal.WithLabelValues(route, r.Method, statusClass).Inc()
		httpRequestDuration.WithLabelValues(route, r.Method).Observe(durationMs)

		logReq := reqLog.InfoContext
		if ww.status >= 500 {
			httpErrorsTotal.WithLabelValues(route, "server_error").Inc()
			logReq = reqLog.ErrorContext
		}
		logReq(ctx, "request completed",
			slog.String("route", route),
			slog.String("status_class", statusClass),
			slog.Int("status", ww.status),
			slog.Float64("duration_ms", durationMs),
		)
	})
}

// handleOrder simulates a request that must borrow a worker and a DB
// connection to run a query. op is a bounded label (read/list/write).
func (s *server) handleOrder(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := LoggerFromCtx(ctx)
		traceID := traceIDFrom(ctx)

		// Accept queue -> worker pool (drives requests_queued).
		s.workers.acquire()
		defer s.workers.release()

		// DB connection pool (drives db_connections_waiting / _active).
		if !s.db.acquire(s.cfg.acquireTimeout) {
			dbQueryErrorsTotal.WithLabelValues(op).Inc()
			log.WarnContext(ctx, "db pool acquire timeout", slog.String("operation", op))
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db pool timeout"})
			return
		}
		defer s.db.release()

		s.inj.mu.RLock()
		failRatio, extra := s.inj.failRatio, s.inj.extraLatMS
		s.inj.mu.RUnlock()

		// Simulate query latency.
		latency := time.Duration(s.cfg.dbQueryMS+rand.Intn(s.cfg.dbJitterMS+1)+extra) * time.Millisecond
		time.Sleep(latency)

		// Simulate query failure.
		if failRatio > 0 && rand.Float64() < failRatio {
			dbQueryErrorsTotal.WithLabelValues(op).Inc()
			log.ErrorContext(ctx, "db query failed", slog.String("operation", op))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"trace_id": traceID, "operation": op, "status": "ok",
		})
	}
}

// handleInject flips fault injection at runtime (used by the load test's
// saturation phase). GET returns current state.
func (s *server) handleInject(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.inj.mu.RLock()
		defer s.inj.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"fail_ratio": s.inj.failRatio, "extra_latency_ms": s.inj.extraLatMS,
		})
		return
	}
	var body struct {
		FailRatio      *float64 `json:"fail_ratio"`
		ExtraLatencyMS *int     `json:"extra_latency_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	s.inj.mu.Lock()
	if body.FailRatio != nil {
		s.inj.failRatio = *body.FailRatio
	}
	if body.ExtraLatencyMS != nil {
		s.inj.extraLatMS = *body.ExtraLatencyMS
	}
	fr, el := s.inj.failRatio, s.inj.extraLatMS
	s.inj.mu.Unlock()

	Logger.Info("fault injection updated",
		slog.Float64("fail_ratio", fr),
		slog.Int("extra_latency_ms", el),
	)
	writeJSON(w, http.StatusOK, map[string]any{"fail_ratio": fr, "extra_latency_ms": el})
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------
func main() {
	cfg := loadConfig()

	if err := os.MkdirAll(filepath.Dir(cfg.logPath), 0o755); err != nil {
		log.Fatalf("create log dir: %v", err)
	}
	f, err := os.OpenFile(cfg.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("open log file: %v", err)
	}
	defer f.Close()

	InitLogger(f)
	s := &server{
		cfg:     cfg,
		db:      newDBPool(cfg.dbPoolSize),
		workers: newWorkerPool(cfg.maxWorkers),
		inj:     &injState{failRatio: cfg.baseFailRatio},
	}

	// Metrics server on a separate port so /metrics never pollutes the API port.
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		Logger.Info("metrics server listening", slog.String("addr", cfg.metricsAddr))
		if err := http.ListenAndServe(cfg.metricsAddr, mux); err != nil {
			log.Fatalf("metrics server: %v", err)
		}
	}()

	// API router. Business routes go through the observability middleware;
	// /healthz and /admin/* are mounted outside it so they don't pollute RED.
	r := chi.NewRouter()
	r.Use(s.observability)
	r.Get("/api/orders/{id}", s.handleOrder("read"))
	r.Get("/api/orders", s.handleOrder("list"))
	r.Post("/api/orders", s.handleOrder("write"))

	root := http.NewServeMux()
	root.HandleFunc("/healthz", healthHandler)
	root.HandleFunc("/admin/inject", s.handleInject)
	root.Handle("/", r)

	Logger.Info("api server listening",
		slog.String("addr", cfg.httpAddr),
		slog.Int("db_pool_size", cfg.dbPoolSize),
		slog.Int("max_workers", cfg.maxWorkers),
	)
	if err := http.ListenAndServe(cfg.httpAddr, root); err != nil {
		log.Fatalf("api server: %v", err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func genTraceID() string { return randHex(32) } // 16-byte trace id
func genSpanID() string  { return randHex(16) } // 8-byte span id

func randHex(n int) string {
	const hexdigits = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexdigits[rand.Intn(len(hexdigits))]
	}
	return string(b)
}

func envStr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func envFloat(name string, def float64) float64 {
	if v := os.Getenv(name); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			return f
		}
	}
	return def
}
