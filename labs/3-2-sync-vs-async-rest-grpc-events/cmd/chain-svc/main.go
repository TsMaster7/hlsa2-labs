// chain-svc is a small HTTP service that plays the role of auth,
// pricing, or inventory in the synchronous chain. One binary, env-
// driven label.
//
// Endpoints:
//
//	GET  /healthz            - liveness, excluded from RED counters
//	GET  /metrics            - Prometheus
//	GET  /lookup?key=...     - the only "business" endpoint; emits an
//	                           identifiable response so the gateway can
//	                           tell which hop returned what
//	POST /admin/inject       - flip latency + error knobs at runtime
//	GET  /admin/inject       - read current state
//
// Baseline latency + jitter come from CHAIN_BASE_LATENCY_MS and
// CHAIN_BASE_JITTER_MS so each hop can be slightly different by
// default (auth fast, pricing medium, inventory medium). Step 4
// reproduces the "0.999 * 0.999 * 0.999" multiplication empirically
// by POSTing /admin/inject on the pricing hop only.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hlsa2-labs/lab3-2/internal/inject"
	"github.com/hlsa2-labs/lab3-2/internal/metrics"
)

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	name := envDefault("CHAIN_NAME", "chain")
	addr := envDefault("CHAIN_LISTEN_ADDR", ":8091")
	baseLat := envInt("CHAIN_BASE_LATENCY_MS", 10)
	jitter := envInt("CHAIN_BASE_JITTER_MS", 3)

	state := inject.New(baseLat, jitter)
	httpMetrics := metrics.NewHTTPMetrics(name)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	r := chi.NewRouter()
	skip := map[string]bool{"/metrics": true, "/healthz": true}
	r.Use(httpMetrics.Middleware(skip))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"` + name + `"}`))
	})
	r.Handle("/metrics", metrics.Handler())
	r.Get("/lookup", lookupHandler(name, state))
	r.Method(http.MethodGet, "/admin/inject", state.Handler())
	r.Method(http.MethodPost, "/admin/inject", state.Handler())

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		logger.Info("chain-svc listening", "name", name, "addr", addr,
			"base_latency_ms", baseLat, "jitter_ms", jitter)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down", "name", name)
	shutdownCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
	defer sc()
	_ = srv.Shutdown(shutdownCtx)
}

type lookupResponse struct {
	Service string `json:"service"`
	Key     string `json:"key"`
	OK      bool   `json:"ok"`
}

func lookupHandler(name string, state *inject.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			key = "default"
		}
		fail := state.ApplyAndShouldFail()
		if fail {
			http.Error(w, name+": injected failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(&lookupResponse{
			Service: name,
			Key:     key,
			OK:      true,
		})
	}
}
