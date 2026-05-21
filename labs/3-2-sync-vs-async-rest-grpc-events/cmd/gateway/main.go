// gateway is the synchronous call chain driver. One request from the
// load gen flows through:
//
//	gateway -> auth -> pricing -> inventory
//
// Each hop is an HTTP call. The gateway then optionally:
//
//   - hits lookup-svc on the hot internal path (REST by default, gRPC
//     when GATEWAY_LOOKUP_TRANSPORT=grpc), to model the topic's "hot
//     chatty internal path" that step 7's CANDIDATE=grpc-hot-path
//     converts;
//   - performs an "audit write" side-effect, either synchronously into
//     a Postgres connection (GATEWAY_AUDIT_MODE=sync) or by emitting an
//     event to Kafka (GATEWAY_AUDIT_MODE=event - the
//     async-side-effect candidate).
//
// Endpoints exposed to k6:
//
//	GET  /healthz             - liveness
//	GET  /metrics             - Prometheus
//	GET  /sync-chain?key=...  - the chained call. Returns 200 only if
//	                            every hop returned 2xx.
//	GET  /admin/state         - dump current config knobs
//
// The CIRCUIT_BREAKER=on knob wraps each hop in a per-hop breaker so
// the gateway sees fast failures during the faulted run. Critically,
// the breaker does NOT remove the temporal coupling - requests that
// genuinely need the open hop still fail. That is the topic's point.
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
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/hlsa2-labs/lab3-2/internal/breaker"
	"github.com/hlsa2-labs/lab3-2/internal/kafka"
	"github.com/hlsa2-labs/lab3-2/internal/metrics"
	lab32pb "github.com/hlsa2-labs/lab3-2/proto"
)

// Per-hop metrics: success, failure, fast-fail (breaker open),
// per-hop duration. The gateway also exports an end-to-end histogram.
var (
	hopRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lab32_gateway_hop_requests_total",
		Help: "Per-hop call outcomes from the gateway's perspective.",
	}, []string{"hop", "outcome"})
	hopDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "lab32_gateway_hop_duration_seconds",
		Help:    "Per-hop request duration from the gateway.",
		Buckets: []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"hop"})
	endToEndDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "lab32_gateway_end_to_end_duration_seconds",
		Help:    "End-to-end /sync-chain duration.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})
	breakerOpenGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "lab32_gateway_breaker_open",
		Help: "1 when the per-hop breaker is open.",
	}, []string{"hop"})
)

type hop struct {
	name string
	url  string
	br   *breaker.Breaker
}

type gatewayConfig struct {
	listenAddr      string
	authURL         string
	pricingURL      string
	inventoryURL    string
	lookupRestURL   string
	lookupGRPCAddr  string
	breakerEnabled  bool
	lookupTransport string
	auditMode       string
	kafkaBrokers    string
}

func envDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func loadConfig() gatewayConfig {
	return gatewayConfig{
		listenAddr:      envDefault("GATEWAY_LISTEN_ADDR", ":8090"),
		authURL:         envDefault("GATEWAY_AUTH_URL", "http://auth:8091"),
		pricingURL:      envDefault("GATEWAY_PRICING_URL", "http://pricing:8092"),
		inventoryURL:    envDefault("GATEWAY_INVENTORY_URL", "http://inventory:8093"),
		lookupRestURL:   envDefault("GATEWAY_LOOKUP_REST_URL", "http://lookup-svc:8080"),
		lookupGRPCAddr:  envDefault("GATEWAY_LOOKUP_GRPC_ADDR", "lookup-svc:9000"),
		breakerEnabled:  envDefault("CIRCUIT_BREAKER", "off") == "on",
		lookupTransport: envDefault("GATEWAY_LOOKUP_TRANSPORT", "rest"),
		auditMode:       envDefault("GATEWAY_AUDIT_MODE", "sync"),
		kafkaBrokers:    envDefault("GATEWAY_KAFKA_BROKERS", "redpanda:29092"),
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := loadConfig()
	httpMetrics := metrics.NewHTTPMetrics("gateway")

	hopAuth := &hop{name: "auth", url: cfg.authURL, br: breaker.New(5, 2*time.Second)}
	hopPricing := &hop{name: "pricing", url: cfg.pricingURL, br: breaker.New(5, 2*time.Second)}
	hopInventory := &hop{name: "inventory", url: cfg.inventoryURL, br: breaker.New(5, 2*time.Second)}

	// HTTP client tuned for the chain. KeepAlives are essential -
	// re-establishing a TCP connection per hop would dominate p99
	// even when the chain is healthy.
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        128,
			MaxIdleConnsPerHost: 32,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	var grpcConn *grpc.ClientConn
	var grpcCli lab32pb.LookupClient
	if cfg.lookupTransport == "grpc" {
		c, err := grpc.NewClient(cfg.lookupGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			logger.Error("grpc dial", "err", err)
			os.Exit(1)
		}
		grpcConn = c
		grpcCli = lab32pb.NewLookupClient(c)
	}

	var kProd *kgo.Client
	if cfg.auditMode == "event" {
		c, err := kafka.NewProducer(cfg.kafkaBrokers, "gateway-audit")
		if err != nil {
			logger.Error("kafka producer init", "err", err)
			os.Exit(1)
		}
		kProd = c
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	r := chi.NewRouter()
	skip := map[string]bool{"/metrics": true, "/healthz": true}
	r.Use(httpMetrics.Middleware(skip))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"gateway"}`))
	})
	r.Handle("/metrics", metrics.Handler())
	r.Get("/admin/state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"circuit_breaker":   cfg.breakerEnabled,
			"lookup_transport":  cfg.lookupTransport,
			"audit_mode":        cfg.auditMode,
			"hop_breakers_open": map[string]bool{
				"auth":      isBreakerOpen(hopAuth.br),
				"pricing":   isBreakerOpen(hopPricing.br),
				"inventory": isBreakerOpen(hopInventory.br),
			},
		})
	})
	r.Get("/sync-chain", syncChainHandler(syncChainDeps{
		httpClient: httpClient,
		grpcCli:    grpcCli,
		kProd:      kProd,
		cfg:        cfg,
		hops:       []*hop{hopAuth, hopPricing, hopInventory},
	}))

	srv := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		logger.Info("gateway listening",
			"addr", cfg.listenAddr,
			"circuit_breaker", cfg.breakerEnabled,
			"lookup_transport", cfg.lookupTransport,
			"audit_mode", cfg.auditMode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "err", err)
			cancel()
		}
	}()

	// Background breaker-state exporter (cheap; no busy loop).
	go exportBreakerState(ctx, map[string]*breaker.Breaker{
		"auth": hopAuth.br, "pricing": hopPricing.br, "inventory": hopInventory.br,
	})

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
	defer sc()
	_ = srv.Shutdown(shutdownCtx)
	if grpcConn != nil {
		_ = grpcConn.Close()
	}
	if kProd != nil {
		kProd.Close()
	}
}

type syncChainDeps struct {
	httpClient *http.Client
	grpcCli    lab32pb.LookupClient
	kProd      *kgo.Client
	cfg        gatewayConfig
	hops       []*hop
}

type chainResponse struct {
	OK      bool              `json:"ok"`
	Failed  string            `json:"failed,omitempty"`
	Hops    map[string]string `json:"hops"`
	Lookup  string            `json:"lookup_transport"`
	Audit   string            `json:"audit_mode"`
	TotalMs int64             `json:"total_ms"`
}

func syncChainHandler(d syncChainDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		key := r.URL.Query().Get("key")
		if key == "" {
			key = "default"
		}

		hops := map[string]string{}

		// 1) Synchronous chain. Each hop must succeed for the request
		//    to succeed. This is the "0.999 ^ N" path.
		for _, h := range d.hops {
			outcome, err := d.callHop(r.Context(), h, key)
			hops[h.name] = outcome
			if err != nil {
				endToEndDuration.Observe(time.Since(start).Seconds())
				w.Header().Set("content-type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(&chainResponse{
					OK:      false,
					Failed:  h.name,
					Hops:    hops,
					Lookup:  d.cfg.lookupTransport,
					Audit:   d.cfg.auditMode,
					TotalMs: time.Since(start).Milliseconds(),
				})
				return
			}
		}

		// 2) Hot-path lookup. REST by default, gRPC under the
		//    grpc-hot-path candidate. This is the chatty internal
		//    call the topic guide flags as a real gRPC win.
		if err := d.lookup(r.Context(), key); err != nil {
			hopRequests.WithLabelValues("lookup", "failure").Inc()
			endToEndDuration.Observe(time.Since(start).Seconds())
			http.Error(w, "lookup failed: "+err.Error(), http.StatusBadGateway)
			return
		}

		// 3) Audit side-effect. Inline DB-write by default; event-
		//    emit under async-side-effect candidate.
		switch d.cfg.auditMode {
		case "event":
			// Async; do NOT block the response on it.
			go d.emitAudit(context.Background(), key)
		default:
			// Inline. We simulate the DB write with a tiny sleep
			// (the real lab would actually write); the point is to
			// show this work is in the critical path.
			time.Sleep(8 * time.Millisecond)
		}

		endToEndDuration.Observe(time.Since(start).Seconds())
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(&chainResponse{
			OK:      true,
			Hops:    hops,
			Lookup:  d.cfg.lookupTransport,
			Audit:   d.cfg.auditMode,
			TotalMs: time.Since(start).Milliseconds(),
		})
	}
}

func (d *syncChainDeps) callHop(ctx context.Context, h *hop, key string) (string, error) {
	start := time.Now()
	defer func() { hopDuration.WithLabelValues(h.name).Observe(time.Since(start).Seconds()) }()

	fn := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url+"/lookup?key="+key, nil)
		if err != nil {
			return err
		}
		resp, err := d.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			return fmt.Errorf("%s: status %d", h.name, resp.StatusCode)
		}
		return nil
	}

	var err error
	if d.cfg.breakerEnabled {
		err = h.br.Do(fn)
		if errors.Is(err, breaker.ErrOpen) {
			hopRequests.WithLabelValues(h.name, "shortcircuit").Inc()
			return "shortcircuit", err
		}
	} else {
		err = fn()
	}
	if err != nil {
		hopRequests.WithLabelValues(h.name, "failure").Inc()
		return "failure", err
	}
	hopRequests.WithLabelValues(h.name, "success").Inc()
	return "ok", nil
}

func (d *syncChainDeps) lookup(ctx context.Context, key string) error {
	start := time.Now()
	defer func() { hopDuration.WithLabelValues("lookup").Observe(time.Since(start).Seconds()) }()

	if d.cfg.lookupTransport == "grpc" && d.grpcCli != nil {
		_, err := d.grpcCli.Lookup(ctx, &lab32pb.LookupRequest{Key: key, PayloadSize: "small"})
		if err != nil {
			hopRequests.WithLabelValues("lookup", "failure").Inc()
			return err
		}
		hopRequests.WithLabelValues("lookup", "success").Inc()
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.cfg.lookupRestURL+"/lookup?key="+key+"&size=small", nil)
	if err != nil {
		return err
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		hopRequests.WithLabelValues("lookup", "failure").Inc()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		hopRequests.WithLabelValues("lookup", "failure").Inc()
		return fmt.Errorf("lookup: status %d", resp.StatusCode)
	}
	hopRequests.WithLabelValues("lookup", "success").Inc()
	return nil
}

func (d *syncChainDeps) emitAudit(ctx context.Context, key string) {
	if d.kProd == nil {
		return
	}
	ev := &kafka.Event{
		EventID:   fmt.Sprintf("gw-%d-%s", time.Now().UnixNano(), key),
		OrderID:   key,
		Amount:    1,
		EmittedAt: time.Now().UnixNano(),
		Source:    "gateway-audit",
	}
	if err := kafka.Produce(ctx, d.kProd, "events", ev); err != nil {
		slog.Default().Warn("audit emit failed", "err", err)
	}
}

func isBreakerOpen(b *breaker.Breaker) bool {
	state, _ := b.Snapshot()
	return state == breaker.Open
}

func exportBreakerState(ctx context.Context, brs map[string]*breaker.Breaker) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for name, b := range brs {
				if isBreakerOpen(b) {
					breakerOpenGauge.WithLabelValues(name).Set(1)
				} else {
					breakerOpenGauge.WithLabelValues(name).Set(0)
				}
			}
		}
	}
}
