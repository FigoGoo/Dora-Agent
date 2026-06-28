package logger

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/tracectx"
)

type contextKey string

const (
	FieldService   = "service"
	FieldEnv       = "env"
	FieldTraceID   = "trace_id"
	FieldRequestID = "request_id"
	FieldMethod    = "method"
	FieldPath      = "path"
	FieldStatus    = "status"
	FieldLatencyMS = "latency_ms"
)

var BaseFields = []string{FieldService, FieldEnv}
var HTTPRequestFields = []string{FieldTraceID, FieldRequestID, FieldMethod, FieldPath, FieldStatus, FieldLatencyMS}

const (
	TraceIDKey   contextKey = FieldTraceID
	RequestIDKey contextKey = FieldRequestID
)

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return tracectx.WithTraceID(ctx, traceID)
}

func TraceID(ctx context.Context) string {
	return tracectx.TraceID(ctx)
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(RequestIDKey).(string)
	return value
}

func New(w io.Writer, service, env, level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redactAttr,
	})).With(FieldService, service, FieldEnv, env)
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
