// Package downstream owns the HTTP client to the downstream stub.
//
// The client uses a single shared http.Transport with bounded
// MaxIdleConnsPerHost (configurable via SUT_DOWNSTREAM_POOL_SIZE);
// per-call latency is recorded as a Prometheus histogram so the
// "dependencies" layer of the four-layer instrumentation is populated
// even without any tracing backend.
package downstream

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	downstreamLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "downstream_call_duration_seconds",
			Help:    "Latency of HTTP calls from the SUT to the downstream stub.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		},
		[]string{"target", "outcome"},
	)
	downstreamCalls = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "downstream_calls_total",
			Help: "Number of HTTP calls from the SUT to the downstream stub.",
		},
		[]string{"target", "outcome"},
	)
)

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// Client is a thin wrapper around http.Client that issues GETs to a
// fixed downstream URL and emits Prometheus metrics for every call.
type Client struct {
	url    string
	hc     *http.Client
	target string
}

// NewClient builds a Client with a shared transport sized for high RPS.
func NewClient(url string) *Client {
	pool := envInt("SUT_DOWNSTREAM_POOL_SIZE", 256)
	timeout := time.Duration(envInt("SUT_DOWNSTREAM_TIMEOUT_MS", 30000)) * time.Millisecond

	tr := &http.Transport{
		MaxIdleConns:        pool * 2,
		MaxIdleConnsPerHost: pool,
		MaxConnsPerHost:     pool,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &Client{
		url:    url,
		target: "downstream",
		hc: &http.Client{
			Transport: tr,
			Timeout:   timeout,
		},
	}
}

// Get issues GET against the configured URL. It returns the body
// bytes, HTTP status and any transport error.
func (c *Client) Get(ctx context.Context) ([]byte, int, error) {
	start := time.Now()
	outcome := "ok"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		downstreamCalls.WithLabelValues(c.target, "error").Inc()
		downstreamLatency.WithLabelValues(c.target, "error").Observe(time.Since(start).Seconds())
		return nil, 0, fmt.Errorf("new request: %w", err)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		outcome = "error"
		downstreamCalls.WithLabelValues(c.target, outcome).Inc()
		downstreamLatency.WithLabelValues(c.target, outcome).Observe(time.Since(start).Seconds())
		return nil, 0, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	dur := time.Since(start).Seconds()

	if err != nil {
		outcome = "error"
		downstreamCalls.WithLabelValues(c.target, outcome).Inc()
		downstreamLatency.WithLabelValues(c.target, outcome).Observe(dur)
		return nil, resp.StatusCode, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 500 {
		outcome = "5xx"
	} else if resp.StatusCode >= 400 {
		outcome = "4xx"
	}

	downstreamCalls.WithLabelValues(c.target, outcome).Inc()
	downstreamLatency.WithLabelValues(c.target, outcome).Observe(dur)

	return body, resp.StatusCode, nil
}
