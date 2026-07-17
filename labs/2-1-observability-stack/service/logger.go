// Structured JSON logging via log/slog.
//
// Output is one JSON object per line, written to stdout AND the shared log
// file (/var/log/app/service.log) that Promtail tails and ships to Loki.
// Every request log line carries service, method, path, trace_id, span_id,
// route, status, duration_ms, and level.
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Logger is the process-wide structured logger. Defaults to stdout; InitLogger
// swaps in a multi-writer (stdout + log file) once config is known.
var Logger = newLogger(os.Stdout)

func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.LevelKey:
				// Lowercase so Loki's `level` label follows convention (info/warn/error).
				a.Value = slog.StringValue(strings.ToLower(a.Value.String()))
			case slog.TimeKey:
				// Force UTC for deterministic RFC3339Nano timestamps.
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	}))
}

// InitLogger points the global logger at stdout plus the shared log file
// (which Promtail tails). Called once from main after the file is opened.
func InitLogger(fileW io.Writer) {
	Logger = newLogger(io.MultiWriter(os.Stdout, fileW))
}

type loggerKey struct{}

// LoggerFromCtx returns the request-scoped logger (with trace_id etc. already
// attached), or the global logger if none is present.
func LoggerFromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return Logger
}

func ctxWithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}
