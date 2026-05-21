// Package inject is the runtime-flippable fault state used by chain-svc.
// It mirrors the pattern in lab 2-3's downstream stub: env-default at
// boot, runtime-flippable via POST /admin/inject, snapshot-read on
// every request.
package inject

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// State holds the latency + error knobs.
type State struct {
	mu         sync.RWMutex
	p99Ms      int
	errorRate  float64
	baseLatMs  int
	jitterMs   int
}

// New returns a state pre-populated with the service's baseline latency
// and jitter. Faults default to zero (healthy).
func New(baseLatencyMs, jitterMs int) *State {
	return &State{
		baseLatMs: baseLatencyMs,
		jitterMs:  jitterMs,
	}
}

// Snapshot returns the current knobs.
func (s *State) Snapshot() (p99Ms int, errorRate float64, baseLatMs int, jitterMs int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.p99Ms, s.errorRate, s.baseLatMs, s.jitterMs
}

// Sleep blocks for the baseline latency (uniform jitter around mean)
// plus an injected p99 tail for ~1% of calls. Returns true when the
// call should be failed by the caller for the injected error rate.
func (s *State) ApplyAndShouldFail() bool {
	p99, errRate, base, jitter := s.Snapshot()

	delay := time.Duration(base)*time.Millisecond +
		time.Duration(rand.IntN(2*jitter+1)-jitter)*time.Millisecond
	if delay < 0 {
		delay = 0
	}
	if p99 > 0 && rand.Float64() < 0.01 {
		// 1% of requests get the full injected tail. This is enough
		// to dominate p99 without inflating the median, which is the
		// regime the topic guide cares about for the availability tax.
		delay += time.Duration(p99) * time.Millisecond
	}
	time.Sleep(delay)

	if errRate > 0 && rand.Float64() < errRate {
		return true
	}
	return false
}

type injectRequest struct {
	P99Ms     *int     `json:"p99_ms,omitempty"`
	ErrorRate *float64 `json:"error_rate,omitempty"`
}

// Handler returns the /admin/inject HTTP handler. GET returns the
// current state; POST sets it.
func (s *State) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			p99, errRate, base, jitter := s.Snapshot()
			w.Header().Set("content-type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"p99_ms":         p99,
				"error_rate":     errRate,
				"base_latency":   base,
				"latency_jitter": jitter,
			})
		case http.MethodPost:
			var req injectRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
				return
			}
			s.mu.Lock()
			if req.P99Ms != nil && *req.P99Ms >= 0 {
				s.p99Ms = *req.P99Ms
			}
			if req.ErrorRate != nil && *req.ErrorRate >= 0 && *req.ErrorRate <= 1 {
				s.errorRate = *req.ErrorRate
			}
			p99, errRate := s.p99Ms, s.errorRate
			s.mu.Unlock()

			w.Header().Set("content-type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"p99_ms":     p99,
				"error_rate": errRate,
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
