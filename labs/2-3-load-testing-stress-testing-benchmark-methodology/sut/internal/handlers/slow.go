package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/hlsa2-labs/lab2-3/sut/internal/downstream"
)

// NewSlow returns the /slow handler. It calls the downstream stub once
// per request; when STALL_EVERY_N is enabled on the downstream, this
// is the endpoint whose tail latency reveals coordinated omission.
func NewSlow(client *downstream.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, status, err := client.Get(r.Context())
		if err != nil {
			http.Error(w, "downstream error: "+err.Error(), http.StatusBadGateway)
			return
		}
		if status >= 500 {
			http.Error(w, "downstream 5xx", http.StatusBadGateway)
			return
		}

		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoint":      "slow",
			"downstream_kb": len(body) / 1024,
			"status":        status,
		})
	}
}
