package handlers

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewMedium returns the /medium handler. It picks a row id with a
// roughly Zipfian distribution (sqrt-skew over the seeded 50k rows)
// so the working set is realistic but bounded, then runs a single
// SELECT through the connection pool. Pool wait time is the most
// useful saturation signal here: when the pool is too small for the
// arrival rate, p99 climbs even though the per-query cost is flat.
func NewMedium(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// sqrt-skew over [1, 50000] - small ids hit hot, large ids are rare.
		u := rand.Float64()
		idFloat := u * u * 50000.0
		id := int64(idFloat) + 1

		var (
			rowID  int64
			sku    string
			amount int64
		)
		err := pool.QueryRow(
			ctx,
			"SELECT id, sku, amount_cents FROM items WHERE id = $1",
			id,
		).Scan(&rowID, &sku, &amount)
		if err != nil {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoint":     "medium",
			"id":           rowID,
			"sku":          sku,
			"amount_cents": amount,
		})
	}
}
