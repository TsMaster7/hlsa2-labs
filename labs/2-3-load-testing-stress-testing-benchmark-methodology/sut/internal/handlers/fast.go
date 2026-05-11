package handlers

import (
	"encoding/json"
	"hash/fnv"
	"net/http"
	"strconv"
)

// Fast is the fast-path endpoint. It is deliberately CPU-bound and
// allocation-light so the p99 in isolation is below 5ms even on a
// modest laptop. We hash a small constant string to keep the compiler
// from optimising the work away and to give the CPU profile a non-zero
// signal during the soak test.
func Fast(w http.ResponseWriter, r *http.Request) {
	h := fnv.New64a()
	for i := 0; i < 64; i++ {
		_, _ = h.Write([]byte("hlsa2-fast-path-" + strconv.Itoa(i)))
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"endpoint": "fast",
		"hash":     h.Sum64(),
	})
}
