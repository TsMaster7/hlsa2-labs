// Downstream stub for HLSA2 lab 2-3.
//
// Mirrors a small upstream API the SUT depends on. Two responsibilities:
//
//  1. Produce the baseline 1-2% latency variance that makes /slow's
//     tail latency interesting (LATENCY_MS / LATENCY_JITTER_MS).
//  2. Expose a runtime-flippable "stall switch" that injects an
//     8-10s sleep on every Nth request. This is the trigger for the
//     coordinated omission demonstration in homework Step 4.
//
// The stall switch is controlled by either env (at boot) or by POSTing
// JSON to /admin/inject at runtime, so students don't have to restart
// the container between the closed-loop and open-loop runs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

// state holds the runtime-flippable knobs. Reads happen on every
// request; writes happen rarely (admin POSTs and boot). A RWMutex is
// fine here. A counter is bumped per-call to decide stall-vs-no-stall.
type state struct {
	mu              sync.RWMutex
	latencyMs       int
	latencyJitterMs int
	stallEveryN     int
	stallDurationMs int
	counter         atomic.Uint64
}

func newState() *state {
	return &state{
		latencyMs:       envInt("LATENCY_MS", 30),
		latencyJitterMs: envInt("LATENCY_JITTER_MS", 5),
		stallEveryN:     envInt("STALL_EVERY_N", 0),
		stallDurationMs: envInt("STALL_DURATION_MS", 8000),
	}
}

func (s *state) snapshot() (latency, jitter, every, dur int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latencyMs, s.latencyJitterMs, s.stallEveryN, s.stallDurationMs
}

// Metrics
var (
	requestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "downstream_requests_total",
			Help: "HTTP requests handled by the downstream stub.",
		},
		[]string{"endpoint", "outcome"},
	)
	requestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "downstream_request_duration_seconds",
			Help:    "Server-side request duration on the downstream stub.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		},
		[]string{"endpoint"},
	)
	stallsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "downstream_stalls_total",
			Help: "Number of stall-injection events emitted by the downstream stub.",
		},
	)
	stallEveryNGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "downstream_stall_every_n",
			Help: "Currently configured stall frequency (0 = disabled).",
		},
	)
	stallDurationMsGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "downstream_stall_duration_ms",
			Help: "Currently configured stall duration in milliseconds.",
		},
	)
)

type widgetResponse struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Tags  []string `json:"tags"`
	Bytes string   `json:"bytes"`
}

// payloadFiller is ~1.5 KB of base64-ish padding so /api/widget
// returns a payload comparable in size to a realistic JSON response.
const payloadFiller = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" +
	"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB" +
	"CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC" +
	"DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD" +
	"EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE" +
	"FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF" +
	"GGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG" +
	"HHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHH" +
	"IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII" +
	"JJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJ" +
	"KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK" +
	"LLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLL" +
	"MMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMM" +
	"NNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNN" +
	"OOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO" +
	"PPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPP" +
	"QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ" +
	"RRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRR" +
	"SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS" +
	"TTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTT" +
	"UUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUU" +
	"VVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVV" +
	"WWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWW" +
	"XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"

func widgetHandler(s *state) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		latency, jitter, every, dur := s.snapshot()

		// baseline latency: uniform jitter around the mean.
		baseDelay := time.Duration(latency)*time.Millisecond +
			time.Duration(rand.IntN(2*jitter+1)-jitter)*time.Millisecond
		if baseDelay < 0 {
			baseDelay = 0
		}
		time.Sleep(baseDelay)

		// stall-switch: every Nth request gets an extra dur sleep.
		n := s.counter.Add(1)
		stalled := false
		if every > 0 && n%uint64(every) == 0 {
			stalled = true
			stallsTotal.Inc()
			time.Sleep(time.Duration(dur) * time.Millisecond)
		}

		resp := widgetResponse{
			ID:    int(n & 0xFFFFFF),
			Name:  "widget",
			Tags:  []string{"hlsa2", "lab2-3"},
			Bytes: payloadFiller,
		}

		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)

		outcome := "ok"
		if stalled {
			outcome = "stalled"
		}
		requestsTotal.WithLabelValues("/api/widget", outcome).Inc()
		requestDuration.WithLabelValues("/api/widget").Observe(time.Since(start).Seconds())
	}
}

type injectRequest struct {
	LatencyMs       *int `json:"latency_ms,omitempty"`
	LatencyJitterMs *int `json:"latency_jitter_ms,omitempty"`
	StallEveryN     *int `json:"stall_every_n,omitempty"`
	StallDurationMs *int `json:"stall_duration_ms,omitempty"`
}

func adminInjectHandler(s *state) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			latency, jitter, every, dur := s.snapshot()
			w.Header().Set("content-type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int{
				"latency_ms":        latency,
				"latency_jitter_ms": jitter,
				"stall_every_n":     every,
				"stall_duration_ms": dur,
			})
			return
		case http.MethodPost:
			var req injectRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
				return
			}
			s.mu.Lock()
			if req.LatencyMs != nil && *req.LatencyMs >= 0 {
				s.latencyMs = *req.LatencyMs
			}
			if req.LatencyJitterMs != nil && *req.LatencyJitterMs >= 0 {
				s.latencyJitterMs = *req.LatencyJitterMs
			}
			if req.StallEveryN != nil && *req.StallEveryN >= 0 {
				s.stallEveryN = *req.StallEveryN
			}
			if req.StallDurationMs != nil && *req.StallDurationMs >= 0 {
				s.stallDurationMs = *req.StallDurationMs
			}
			latency, jitter, every, dur := s.latencyMs, s.latencyJitterMs, s.stallEveryN, s.stallDurationMs
			s.mu.Unlock()

			stallEveryNGauge.Set(float64(every))
			stallDurationMsGauge.Set(float64(dur))

			w.Header().Set("content-type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int{
				"latency_ms":        latency,
				"latency_jitter_ms": jitter,
				"stall_every_n":     every,
				"stall_duration_ms": dur,
			})
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "downstream",
	})
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := ":8081"
	if v := os.Getenv("DOWNSTREAM_LISTEN_ADDR"); v != "" {
		addr = v
	}

	st := newState()
	stallEveryNGauge.Set(float64(st.stallEveryN))
	stallDurationMsGauge.Set(float64(st.stallDurationMs))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/api/widget", widgetHandler(st))
	mux.HandleFunc("/admin/inject", adminInjectHandler(st))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		logger.Info("downstream listening",
			"addr", addr,
			"latency_ms", st.latencyMs,
			"latency_jitter_ms", st.latencyJitterMs,
			"stall_every_n", st.stallEveryN,
			"stall_duration_ms", st.stallDurationMs,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
