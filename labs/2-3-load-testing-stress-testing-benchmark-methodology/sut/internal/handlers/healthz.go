package handlers

import (
	"encoding/json"
	"net/http"
)

// Healthz is intentionally trivial: it must not touch postgres or the
// downstream stub, so the readiness check is independent of dependency
// state. The compose healthcheck calls this and the stack only flips
// to "healthy" once the SUT itself is up.
func Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "sut",
	})
}
