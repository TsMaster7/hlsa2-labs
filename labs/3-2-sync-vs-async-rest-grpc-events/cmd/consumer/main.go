// consumer drains the `events` topic into Postgres. It exposes a
// handful of HTTP knobs the bench harness flips between runs:
//
//	GET  /healthz                 liveness
//	GET  /metrics                 Prometheus
//	GET  /state                   dump current knobs + queue depth +
//	                              whether we are asking the producer
//	                              to shed (`shed:true`).
//	POST /mode { mode }           naive | idempotent
//	POST /flow { on }             turn FLOW_CONTROL on/off at runtime
//	POST /reset                   truncate audit tables (only for
//	                              assert-idempotent test loops)
//	POST /replay { window_s }     spin a fresh consumer group from
//	                              offset 0 over the window, then exit
//	                              the goroutine. Used by `make replay`.
//
// Two operating modes:
//
//	naive       INSERT INTO events_audit_naive ...
//	idempotent  INSERT INTO events_audit ... ON CONFLICT (event_id) DO NOTHING
//
// Two flow-control behaviours:
//
//	off  unbounded in-memory queue. Watch for the
//	     ASYNC_MAX_QUEUE_GUARD ceiling - the no-bp run aborts cleanly
//	     before it OOMs the host.
//	on   bounded queue with drop-oldest. /state exposes shed:true while
//	     the queue is above the high-water mark so the producer can
//	     return 429.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/hlsa2-labs/lab3-2/internal/kafka"
	"github.com/hlsa2-labs/lab3-2/internal/metrics"
)

var (
	consumedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lab32_consumer_consumed_total",
		Help: "Events drained from the topic into the worker queue.",
	})
	handledTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lab32_consumer_handled_total",
		Help: "Events flushed to Postgres by handler outcome.",
	}, []string{"outcome", "mode"})
	droppedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lab32_consumer_dropped_total",
		Help: "Events dropped (drop-oldest) due to flow control.",
	})
	queueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lab32_consumer_queue_depth",
		Help: "In-memory worker queue depth.",
	})
	eventToEffect = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "lab32_consumer_event_to_effect_seconds",
		Help:    "End-to-end latency from event emission to row visible in Postgres.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 30, 120, 600, 3600},
	})
	handlerDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "lab32_consumer_handler_duration_seconds",
		Help:    "Per-message handler latency (DB write only).",
		Buckets: []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	})
	dlqTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lab32_consumer_dlq_total",
		Help: "Events routed to the dead-letter queue under CANDIDATE=dlq-retry-budget.",
	})
	shedSignal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lab32_consumer_shed_signal",
		Help: "1 when the consumer is asking the producer to shed.",
	})
)

type config struct {
	addr            string
	brokers         string
	topic           string
	dlqTopic        string
	group           string
	dsn             string
	mode            string
	flowControl     bool
	queueMax        int
	handlerRate     int
	queueGuard      int
	replayRateLimit int
}

func envDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return d
}

func loadConfig() config {
	return config{
		addr:            envDefault("CONSUMER_LISTEN_ADDR", ":8095"),
		brokers:         envDefault("CONSUMER_KAFKA_BROKERS", "redpanda:29092"),
		topic:           envDefault("CONSUMER_TOPIC", "events"),
		dlqTopic:        envDefault("CONSUMER_DLQ_TOPIC", "events.dlq"),
		group:           envDefault("CONSUMER_GROUP", "events-consumer"),
		dsn:             envDefault("CONSUMER_DB_DSN", "postgres://lab:lab@postgres:5432/lab?sslmode=disable"),
		mode:            envDefault("CONSUMER_MODE", "idempotent"),
		flowControl:     envDefault("FLOW_CONTROL", "off") == "on",
		queueMax:        envInt("CONSUMER_QUEUE_MAX", 10000),
		handlerRate:     envInt("CONSUMER_HANDLER_RATE", 1500),
		queueGuard:      envInt("ASYNC_MAX_QUEUE_GUARD", 2_000_000),
		replayRateLimit: envInt("REPLAY_RATE_LIMIT", 0),
	}
}

type consumerState struct {
	cfg       config
	cfgMu     sync.RWMutex
	pool      *pgxpool.Pool
	queue     chan *kafka.Event
	queueLen  atomic.Int64
	aborted   atomic.Bool
	shed      atomic.Bool
	dlqClient *kgo.Client

	// replay coordination
	replayCancel context.CancelFunc
	replayMu     sync.Mutex
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := loadConfig()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.dsn)
	if err != nil {
		logger.Error("postgres connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	dlq, err := kafka.NewProducer(cfg.brokers, "lab32-dlq")
	if err != nil {
		logger.Error("dlq producer init", "err", err)
		os.Exit(1)
	}
	defer dlq.Close()

	st := &consumerState{
		cfg:       cfg,
		pool:      pool,
		dlqClient: dlq,
	}
	st.allocQueue(cfg.flowControl, cfg.queueMax)

	go st.runMainConsumer(ctx, cfg)
	go st.runHandlers(ctx)

	httpMetrics := metrics.NewHTTPMetrics("consumer")
	r := chi.NewRouter()
	skip := map[string]bool{"/metrics": true, "/healthz": true, "/state": true}
	r.Use(httpMetrics.Middleware(skip))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"consumer"}`))
	})
	r.Handle("/metrics", metrics.Handler())
	r.Get("/state", st.handleState)
	r.Post("/mode", st.handleMode)
	r.Post("/flow", st.handleFlow)
	r.Post("/reset", st.handleReset)
	r.Post("/replay", st.handleReplay)

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		logger.Info("consumer listening",
			"addr", cfg.addr, "mode", cfg.mode, "flow_control", cfg.flowControl,
			"queue_max", cfg.queueMax, "handler_rate", cfg.handlerRate)
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
}

// --- queue management ----------------------------------------------

func (s *consumerState) allocQueue(flowOn bool, qmax int) {
	if flowOn {
		s.queue = make(chan *kafka.Event, qmax)
	} else {
		// "Unbounded" - we still set a hard ceiling to avoid OOMing
		// the host, but it is large enough that the no-bp run can
		// clearly diverge before tripping it.
		s.queue = make(chan *kafka.Event, s.cfg.queueGuard)
	}
}

func (s *consumerState) enqueue(ev *kafka.Event) {
	s.cfgMu.RLock()
	flowOn := s.cfg.flowControl
	highWater := s.cfg.queueMax * 8 / 10
	hardCeil := s.cfg.queueGuard
	s.cfgMu.RUnlock()

	depth := int(s.queueLen.Load())
	if depth >= hardCeil {
		s.aborted.Store(true)
		droppedTotal.Inc()
		return
	}

	if flowOn {
		if depth >= highWater {
			s.shed.Store(true)
			shedSignal.Set(1)
		} else {
			s.shed.Store(false)
			shedSignal.Set(0)
		}
		// drop-oldest if full
		if depth >= cap(s.queue) {
			select {
			case <-s.queue:
				s.queueLen.Add(-1)
				droppedTotal.Inc()
			default:
			}
		}
	}

	select {
	case s.queue <- ev:
		s.queueLen.Add(1)
		queueDepth.Set(float64(s.queueLen.Load()))
	default:
		droppedTotal.Inc()
	}
}

// --- main consumer loop --------------------------------------------

func (s *consumerState) runMainConsumer(ctx context.Context, cfg config) {
	cl, err := kafka.NewConsumer(cfg.brokers, cfg.group, cfg.topic, false)
	if err != nil {
		slog.Default().Error("consumer client init", "err", err)
		return
	}
	defer cl.Close()

	for {
		if ctx.Err() != nil {
			return
		}
		fetches := cl.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return
		}
		fetches.EachError(func(t string, p int32, err error) {
			slog.Default().Warn("fetch error", "topic", t, "partition", p, "err", err)
		})
		fetches.EachRecord(func(rec *kgo.Record) {
			ev := &kafka.Event{}
			if err := json.Unmarshal(rec.Value, ev); err != nil {
				slog.Default().Warn("bad event", "err", err)
				return
			}
			consumedTotal.Inc()
			s.enqueue(ev)
		})
		if err := cl.CommitUncommittedOffsets(ctx); err != nil {
			slog.Default().Warn("commit", "err", err)
		}
	}
}

// --- handlers ------------------------------------------------------

func (s *consumerState) runHandlers(ctx context.Context) {
	s.cfgMu.RLock()
	rate := s.cfg.handlerRate
	s.cfgMu.RUnlock()
	if rate <= 0 {
		rate = 1500
	}
	tick := time.NewTicker(time.Second / time.Duration(rate))
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			select {
			case ev := <-s.queue:
				s.queueLen.Add(-1)
				queueDepth.Set(float64(s.queueLen.Load()))
				s.handleOne(ctx, ev)
			default:
			}
		}
	}
}

func (s *consumerState) handleOne(ctx context.Context, ev *kafka.Event) {
	start := time.Now()
	s.cfgMu.RLock()
	mode := s.cfg.mode
	s.cfgMu.RUnlock()

	var err error
	switch mode {
	case "naive":
		_, err = s.pool.Exec(ctx,
			`INSERT INTO events_audit_naive (event_id, order_id, amount, emitted_at, source)
			 VALUES ($1, $2, $3, $4, $5)`,
			ev.EventID, ev.OrderID, ev.Amount, ev.EmittedAt, ev.Source)
	default:
		_, err = s.pool.Exec(ctx,
			`INSERT INTO events_audit (event_id, order_id, amount, emitted_at, source)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (event_id) DO NOTHING`,
			ev.EventID, ev.OrderID, ev.Amount, ev.EmittedAt, ev.Source)
	}
	handlerDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		// DLQ candidate behaviour: route poison messages.
		if s.dlqClient != nil {
			dlqTotal.Inc()
			_ = kafka.Produce(ctx, s.dlqClient, s.cfg.dlqTopic, ev)
		}
		handledTotal.WithLabelValues("error", mode).Inc()
		return
	}
	handledTotal.WithLabelValues("success", mode).Inc()
	if ev.EmittedAt > 0 {
		eventToEffect.Observe(float64(time.Now().UnixNano()-ev.EmittedAt) / 1e9)
	}
}

// --- HTTP handlers -------------------------------------------------

func (s *consumerState) handleState(w http.ResponseWriter, _ *http.Request) {
	s.cfgMu.RLock()
	mode := s.cfg.mode
	flowOn := s.cfg.flowControl
	s.cfgMu.RUnlock()

	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"mode":         mode,
		"flow_control": flowOn,
		"queue_depth":  s.queueLen.Load(),
		"queue_cap":    cap(s.queue),
		"shed":         s.shed.Load(),
		"aborted":      s.aborted.Load(),
	})
}

func (s *consumerState) handleMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.Mode != "naive" && body.Mode != "idempotent" {
		http.Error(w, "mode must be naive|idempotent", http.StatusBadRequest)
		return
	}
	s.cfgMu.Lock()
	s.cfg.mode = body.Mode
	s.cfgMu.Unlock()
	fmt.Fprintf(w, `{"ok":true,"mode":%q}`, body.Mode)
}

func (s *consumerState) handleFlow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.cfgMu.Lock()
	s.cfg.flowControl = body.On
	if body.On {
		s.shed.Store(false)
		shedSignal.Set(0)
	}
	s.cfgMu.Unlock()
	fmt.Fprintf(w, `{"ok":true,"flow_control":%v}`, body.On)
}

func (s *consumerState) handleReset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, err := s.pool.Exec(ctx, `TRUNCATE events_audit, events_audit_naive`); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *consumerState) handleReplay(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WindowS int    `json:"window_s"`
		Mode    string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.Mode == "" {
		body.Mode = "idempotent"
	}

	s.replayMu.Lock()
	if s.replayCancel != nil {
		s.replayCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.replayCancel = cancel
	s.replayMu.Unlock()

	s.cfgMu.Lock()
	s.cfg.mode = body.Mode
	s.cfgMu.Unlock()

	go func() {
		defer cancel()
		group := fmt.Sprintf("events-replay-%d", time.Now().UnixNano())
		cl, err := kafka.NewConsumer(s.cfg.brokers, group, s.cfg.topic, true)
		if err != nil {
			slog.Default().Error("replay client", "err", err)
			return
		}
		defer cl.Close()

		until := time.Now().Add(10 * time.Minute)
		if body.WindowS > 0 {
			until = time.Now().Add(time.Duration(body.WindowS) * 2 * time.Second)
		}

		s.cfgMu.RLock()
		rate := s.cfg.replayRateLimit
		s.cfgMu.RUnlock()
		var tick <-chan time.Time
		if rate > 0 {
			t := time.NewTicker(time.Second / time.Duration(rate))
			defer t.Stop()
			tick = t.C
		}

		for time.Now().Before(until) {
			fetches := cl.PollFetches(ctx)
			if fetches.IsClientClosed() {
				return
			}
			fetches.EachRecord(func(rec *kgo.Record) {
				if rate > 0 {
					<-tick
				}
				ev := &kafka.Event{}
				if err := json.Unmarshal(rec.Value, ev); err != nil {
					return
				}
				s.handleOne(ctx, ev)
			})
			if ctx.Err() != nil {
				return
			}
		}
	}()

	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"window_s":  body.WindowS,
		"mode":      body.Mode,
	})
}
