package logger

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

type contextKey string

const TraceIDKey contextKey = "trace_id"

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceIDKey, traceID)
}

func TraceID(ctx context.Context) string {
	value, _ := ctx.Value(TraceIDKey).(string)
	return value
}

func New(w io.Writer, service, env, level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redactAttr,
	})).With("service", service, "env", env)
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
