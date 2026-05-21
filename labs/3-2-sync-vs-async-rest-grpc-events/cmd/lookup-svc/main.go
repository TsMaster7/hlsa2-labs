// lookup-svc serves the same logical `/lookup` operation over two
// transports:
//
//	REST/JSON  - GET /lookup?key=<k>&size=small|large  on :8080
//	gRPC/PB    - hlsa2.lab32.Lookup/Lookup             on :9000
//
// Step 3 of the topic-245 guide compares the two at identical arrival
// rate + identical payload. Anything that would make one of them
// faster for unrelated reasons (different code paths, different
// dataset, different CPU contention) lives here, in one binary, on
// purpose - so the bench is honest.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/hlsa2-labs/lab3-2/internal/metrics"
	"github.com/hlsa2-labs/lab3-2/internal/payload"
	lab32pb "github.com/hlsa2-labs/lab3-2/proto"
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

	restAddr := envDefault("LOOKUP_REST_ADDR", ":8080")
	grpcAddr := envDefault("LOOKUP_GRPC_ADDR", ":9000")

	httpMetrics := metrics.NewHTTPMetrics("lookup")
	grpcMetrics := metrics.NewGRPCMetrics("lookup")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// REST server -------------------------------------------------------
	r := chi.NewRouter()
	skip := map[string]bool{"/metrics": true, "/healthz": true}
	r.Use(httpMetrics.Middleware(skip))
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"lookup"}`))
	})
	r.Handle("/metrics", metrics.Handler())
	r.Get("/lookup", restLookupHandler())

	restSrv := &http.Server{
		Addr:              restAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	// gRPC server -------------------------------------------------------
	gsrv := grpc.NewServer()
	lab32pb.RegisterLookupServer(gsrv, &grpcServer{metrics: grpcMetrics})
	reflection.Register(gsrv)

	gln, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		logger.Error("grpc listen", "err", err)
		os.Exit(1)
	}

	go func() {
		logger.Info("lookup-svc REST listening", "addr", restAddr)
		if err := restSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("rest serve", "err", err)
			cancel()
		}
	}()

	go func() {
		logger.Info("lookup-svc gRPC listening", "addr", grpcAddr)
		if err := gsrv.Serve(gln); err != nil {
			logger.Error("grpc serve", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	_ = restSrv.Shutdown(shutdownCtx)
	gsrv.GracefulStop()
}

// REST handler ----------------------------------------------------------

type restResponse struct {
	Key      string `json:"key"`
	Payload  string `json:"payload"` // base64-encoded so the wire-comparable size matches gRPC's raw bytes
	ServerUs int64  `json:"server_us"`
}

func restLookupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		key := r.URL.Query().Get("key")
		if key == "" {
			key = "default"
		}
		size := payload.SizeForRequest(r.URL.Query().Get("size"))

		body := payload.JSONString(key, size)

		resp := restResponse{
			Key:      key,
			Payload:  body,
			ServerUs: time.Since(start).Microseconds(),
		}
		w.Header().Set("content-type", "application/json")
		if err := json.NewEncoder(w).Encode(&resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// gRPC server ----------------------------------------------------------

type grpcServer struct {
	lab32pb.UnimplementedLookupServer
	metrics *metrics.GRPCMetrics
}

func (g *grpcServer) Lookup(ctx context.Context, in *lab32pb.LookupRequest) (*lab32pb.LookupResponse, error) {
	start := time.Now()

	key := in.GetKey()
	if key == "" {
		key = "default"
	}
	size := payload.SizeForRequest(in.GetPayloadSize())

	out := &lab32pb.LookupResponse{
		Key:      key,
		Payload:  payload.Bytes(key, size),
		ServerUs: time.Since(start).Microseconds(),
	}

	g.metrics.Observe("lookup", "Lookup", "OK", time.Since(start),
		len(in.GetKey())+len(in.GetPayloadSize()), len(out.Payload)+len(out.Key))
	return out, nil
}
