package observability

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/tracectx"
)

type contextKey string

const TraceIDKey contextKey = "trace_id"

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return tracectx.WithTraceID(ctx, traceID)
}

func TraceID(ctx context.Context) string {
	return tracectx.TraceID(ctx)
}

func NewLogger(w io.Writer, service, env, level string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redactAttr,
	}
	return slog.New(slog.NewJSONHandler(w, opts)).With("service", service, "env", env)
}

func AttrsFromContext(ctx context.Context) []slog.Attr {
	traceID := TraceID(ctx)
	if traceID == "" {
		return nil
	}
	return []slog.Attr{slog.String("trace_id", traceID)}
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func redactAttr(_ []string, attr slog.Attr) slog.Attr {
	key := strings.ToLower(attr.Key)
	if strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "token") || strings.Contains(key, "key") {
		if attr.Value.Kind() == slog.KindString && attr.Value.String() != "" {
			attr.Value = slog.StringValue("[REDACTED]")
		}
	}
	return attr
}
