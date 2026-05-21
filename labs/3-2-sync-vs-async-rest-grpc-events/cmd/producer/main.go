// producer is an HTTP-triggered event generator. Bench scripts hit it
// to start/stop a stream of events at a configurable rate, optionally
// honouring the consumer's shed signal (the "load shedding via 429"
// half of the FLOW_CONTROL story in step 5).
//
// Endpoints:
//
//	GET  /healthz                       liveness, no RED counter
//	GET  /metrics                       Prometheus
//	POST /start { rate, duration_s, window_s, source }
//	                                    start a stream at `rate` msg/s
//	                                    for `duration_s` seconds. `window_s`
//	                                    spreads events evenly across that
//	                                    many simulated seconds of history -
//	                                    used by `make seed-events WINDOW=24h`
//	                                    to backfill a day of events quickly.
//	POST /stop                          stop the current stream
//	GET  /state                         introspect current state
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/hlsa2-labs/lab3-2/internal/kafka"
	"github.com/hlsa2-labs/lab3-2/internal/metrics"
)

var (
	producedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lab32_producer_emitted_total",
		Help: "Events successfully emitted to Kafka.",
	})
	failedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lab32_producer_failed_total",
		Help: "Events that failed to emit.",
	})
	shedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lab32_producer_shed_total",
		Help: "Events shed by FLOW_CONTROL=on after the consumer asked for backpressure.",
	})
	targetRateGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lab32_producer_target_rate",
		Help: "Currently configured production rate (msg/s).",
	})
)

func envDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type producerState struct {
	mu        sync.Mutex
	cancelFn  context.CancelFunc
	running   atomic.Bool
	cl        *kgo.Client
	topic     string
	flowOn    bool
	consumer  string
}

type startRequest struct {
	Rate       int    `json:"rate"`
	DurationS  int    `json:"duration_s"`
	WindowS    int    `json:"window_s"`
	Source     string `json:"source"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := envDefault("PRODUCER_LISTEN_ADDR", ":8094")
	brokers := envDefault("PRODUCER_KAFKA_BROKERS", "redpanda:29092")
	topic := envDefault("PRODUCER_TOPIC", "events")
	consumerURL := envDefault("PRODUCER_CONSUMER_URL", "http://consumer:8095")
	flowOn := envDefault("FLOW_CONTROL", "off") == "on"

	cl, err := kafka.NewProducer(brokers, "lab32-producer")
	if err != nil {
		logger.Error("kafka producer init", "err", err)
		os.Exit(1)
	}
	defer cl.Close()

	st := &producerState{
		cl:       cl,
		topic:    topic,
		flowOn:   flowOn,
		consumer: consumerURL,
	}

	httpMetrics := metrics.NewHTTPMetrics("producer")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	r := chi.NewRouter()
	skip := map[string]bool{"/metrics": true, "/healthz": true}
	r.Use(httpMetrics.Middleware(skip))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"producer"}`))
	})
	r.Handle("/metrics", metrics.Handler())
	r.Get("/state", st.handleState)
	r.Post("/start", st.handleStart)
	r.Post("/stop", st.handleStop)

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		logger.Info("producer listening", "addr", addr, "topic", topic, "flow_control", flowOn)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
	defer sc()
	_ = srv.Shutdown(shutdownCtx)
	st.stopStream()
}

func (p *producerState) handleState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"running":      p.running.Load(),
		"topic":        p.topic,
		"flow_control": p.flowOn,
	})
}

func (p *producerState) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Rate <= 0 {
		req.Rate = 1000
	}
	if req.DurationS <= 0 {
		req.DurationS = 60
	}
	if req.Source == "" {
		req.Source = "load-gen"
	}
	p.stopStream()
	p.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelFn = cancel
	p.running.Store(true)
	p.mu.Unlock()

	targetRateGauge.Set(float64(req.Rate))

	go p.runStream(ctx, req)

	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":         true,
		"rate":       req.Rate,
		"duration_s": req.DurationS,
		"window_s":   req.WindowS,
		"source":     req.Source,
	})
}

func (p *producerState) handleStop(w http.ResponseWriter, _ *http.Request) {
	p.stopStream()
	w.Header().Set("content-type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (p *producerState) stopStream() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelFn != nil {
		p.cancelFn()
		p.cancelFn = nil
	}
	p.running.Store(false)
	targetRateGauge.Set(0)
}

func (p *producerState) runStream(ctx context.Context, req startRequest) {
	defer p.running.Store(false)
	defer targetRateGauge.Set(0)

	// Spread events over the simulated history window (window_s) so a
	// 24h backfill takes ~window_s/rate wall-clock seconds, not 24h.
	// emitted_at_ns is offset accordingly.
	startEmitNs := time.Now().UnixNano()
	if req.WindowS > 0 {
		startEmitNs -= int64(req.WindowS) * int64(time.Second)
	}
	totalToEmit := int64(req.Rate) * int64(req.DurationS)
	emitted := int64(0)

	tick := time.NewTicker(time.Second / time.Duration(req.Rate))
	defer tick.Stop()

	end := time.Now().Add(time.Duration(req.DurationS) * time.Second)
	for time.Now().Before(end) {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			// Respect consumer's shed signal under FLOW_CONTROL=on.
			if p.flowOn && p.shouldShed(ctx) {
				shedTotal.Inc()
				continue
			}
			emitted++
			emitNs := startEmitNs
			if totalToEmit > 0 && req.WindowS > 0 {
				emitNs += (emitted * int64(req.WindowS) * int64(time.Second)) / totalToEmit
			} else {
				emitNs = time.Now().UnixNano()
			}
			ev := &kafka.Event{
				EventID:   fmt.Sprintf("%s-%d-%d", req.Source, time.Now().UnixNano(), rand.Int64()),
				OrderID:   fmt.Sprintf("order-%d", rand.IntN(50_000)),
				Amount:    int64(rand.IntN(10_000) + 1),
				EmittedAt: emitNs,
				Source:    req.Source,
			}
			if err := kafka.Produce(ctx, p.cl, p.topic, ev); err != nil {
				failedTotal.Inc()
				continue
			}
			producedTotal.Inc()
		}
	}
}

// shouldShed asks the consumer whether it is currently in overload.
// We make this call cheaply (cached for 200ms) so the producer's hot
// loop is not dominated by HTTP.
var (
	shedCacheMu   sync.RWMutex
	shedCacheAt   time.Time
	shedCacheVal  bool
)

func (p *producerState) shouldShed(ctx context.Context) bool {
	shedCacheMu.RLock()
	if time.Since(shedCacheAt) < 200*time.Millisecond {
		v := shedCacheVal
		shedCacheMu.RUnlock()
		return v
	}
	shedCacheMu.RUnlock()

	shedCacheMu.Lock()
	defer shedCacheMu.Unlock()
	if time.Since(shedCacheAt) < 200*time.Millisecond {
		return shedCacheVal
	}
	shedCacheAt = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.consumer+"/state", nil)
	if err != nil {
		shedCacheVal = false
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		shedCacheVal = false
		return false
	}
	defer resp.Body.Close()
	var s struct {
		Shed bool `json:"shed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		shedCacheVal = false
		return false
	}
	shedCacheVal = s.Shed
	return shedCacheVal
}
