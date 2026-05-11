// Service under test for HLSA2 lab 2-3.
//
// Three deliberately different endpoints:
//
//	GET /fast    - pure-CPU response, sub-5ms p99 in isolation.
//	GET /medium  - single SELECT against postgres, 10-30ms target.
//	GET /slow    - calls the downstream stub; tail latency is dominated
//	               by downstream variance and (when enabled) its stall switch.
//
// /healthz and /metrics are kept out of the RED counters by the middleware.
//
// The four measurement layers expected by the homework are sourced as:
//
//  1. client       - k6 emits its own histogram (collected separately).
//  2. network      - node-exporter on the host (TCP retransmits, conn states).
//  3. service      - this binary: per-endpoint RED, pool-wait USE, goroutine USE.
//  4. dependencies - postgres-exporter and the downstream stub's /metrics.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hlsa2-labs/lab2-3/sut/internal/db"
	"github.com/hlsa2-labs/lab2-3/sut/internal/downstream"
	"github.com/hlsa2-labs/lab2-3/sut/internal/handlers"
	"github.com/hlsa2-labs/lab2-3/sut/internal/middleware"
	runtimemetrics "github.com/hlsa2-labs/lab2-3/sut/internal/runtimemetrics"
)

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := envDefault("SUT_LISTEN_ADDR", ":8080")
	dsn := envDefault(
		"SUT_DB_DSN",
		"postgres://sut:sut@postgres:5432/sut?sslmode=disable",
	)
	downstreamURL := envDefault("SUT_DOWNSTREAM_URL", "http://downstream:8081/api/widget")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		logger.Error("failed to connect to postgres", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	dsClient := downstream.NewClient(downstreamURL)

	runtimemetrics.Register()

	r := chi.NewRouter()
	r.Use(middleware.RED)

	r.Get("/healthz", handlers.Healthz)
	r.Handle("/metrics", promhttp.Handler())
	r.Get("/fast", handlers.Fast)
	r.Get("/medium", handlers.NewMedium(pool))
	r.Get("/slow", handlers.NewSlow(dsClient))

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		logger.Info("sut listening", "addr", addr, "downstream", downstreamURL, "pool_size", pool.Stat().MaxConns())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}
}
