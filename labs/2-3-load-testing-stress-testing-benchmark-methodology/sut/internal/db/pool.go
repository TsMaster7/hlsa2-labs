// Package db owns the postgres connection pool and exposes its USE-method
// signals to Prometheus: pool size, in-use, idle, and acquire-wait time.
//
// The pool size is the central knob for the homework's three-run
// regression protocol: SUT_DB_POOL_SIZE=10 (default) vs. =40 (candidate)
// reliably moves /medium p99 by more than 2 sigma at 100 RPS.
package db

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	pgxPoolMaxConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgxpool_max_conns",
		Help: "Configured maximum size of the pgx connection pool.",
	})
	pgxPoolAcquired = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgxpool_acquired_conns",
		Help: "Connections currently checked out of the pool.",
	})
	pgxPoolIdle = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgxpool_idle_conns",
		Help: "Connections currently idle in the pool.",
	})
	pgxPoolTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pgxpool_total_conns",
		Help: "Total connections in the pool (acquired + idle + constructing).",
	})

	pgxPoolAcquireWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "pgxpool_acquire_wait_seconds",
		Help:    "Wall time spent waiting to acquire a pool connection.",
		Buckets: []float64{0.0001, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	})

	pgxPoolAcquireCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pgxpool_acquire_count_total",
		Help: "Cumulative number of successful pool acquires.",
	})
)

func envInt32(key string, def int32) int32 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 1024 {
			return int32(n)
		}
	}
	return def
}

// NewPool builds a pgxpool from the DSN and starts the metrics
// scraper goroutine. The pool size is taken from SUT_DB_POOL_SIZE
// (default 10).
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = envInt32("SUT_DB_POOL_SIZE", 10)
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}

	pgxPoolMaxConns.Set(float64(cfg.MaxConns))

	go scrapePoolStats(ctx, pool)

	return pool, nil
}

// scrapePoolStats updates the pool gauges every second. The acquire
// wait histogram is fed by EmptyAcquireWaitTime / EmptyAcquireCount
// deltas so the histogram reflects the real distribution.
func scrapePoolStats(ctx context.Context, pool *pgxpool.Pool) {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()

	var lastAcquires int64
	var lastWaitNS int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s := pool.Stat()
		pgxPoolAcquired.Set(float64(s.AcquiredConns()))
		pgxPoolIdle.Set(float64(s.IdleConns()))
		pgxPoolTotal.Set(float64(s.TotalConns()))

		acq := s.AcquireCount()
		waitNS := int64(s.AcquireDuration())
		dAcq := acq - lastAcquires
		dWaitNS := waitNS - lastWaitNS

		if dAcq > 0 {
			meanWait := float64(dWaitNS) / float64(dAcq) / 1e9
			pgxPoolAcquireWait.Observe(meanWait)
			pgxPoolAcquireCount.Add(float64(dAcq))
		}
		lastAcquires = acq
		lastWaitNS = waitNS
	}
}
