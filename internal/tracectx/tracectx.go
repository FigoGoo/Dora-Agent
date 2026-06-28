package tracectx

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/gopkg/cloud/metainfo"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	HeaderTraceparent = "traceparent"
	HeaderTracestate  = "tracestate"
	HeaderTraceID     = "X-Trace-Id"
	HeaderRequestID   = "X-Request-Id"
)

type contextKey string

const (
	traceIDKey     contextKey = "trace_id"
	traceparentKey contextKey = "traceparent"
)

var traceContext = propagation.TraceContext{}

func FromHTTPHeaders(ctx context.Context, header http.Header) context.Context {
	ctx = traceContext.Extract(ctx, propagation.HeaderCarrier(header))
	traceID := TraceID(ctx)
	if traceID == "" {
		traceID = firstNonEmpty(header.Get(HeaderTraceID), header.Get(HeaderRequestID), newLocalTraceID())
		ctx = context.WithValue(ctx, traceIDKey, traceID)
		return context.WithValue(ctx, traceparentKey, MakeTraceparent(traceID))
	}
	return withTraceparent(ctx, traceID)
}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		traceID = newLocalTraceID()
	}
	ctx = context.WithValue(ctx, traceIDKey, traceID)
	return context.WithValue(ctx, traceparentKey, MakeTraceparent(traceID))
}

func FromMetainfo(ctx context.Context) context.Context {
	value, ok := metainfo.GetValue(ctx, HeaderTraceparent)
	if !ok {
		value, ok = metainfo.GetPersistentValue(ctx, HeaderTraceparent)
	}
	if !ok || strings.TrimSpace(value) == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{HeaderTraceparent: value}
	if state, ok := metainfo.GetValue(ctx, HeaderTracestate); ok {
		carrier[HeaderTracestate] = state
	} else if state, ok := metainfo.GetPersistentValue(ctx, HeaderTracestate); ok {
		carrier[HeaderTracestate] = state
	}
	ctx = traceContext.Extract(ctx, carrier)
	traceID := TraceID(ctx)
	if traceID == "" {
		return ctx
	}
	return withTraceparent(ctx, traceID)
}

func InjectHTTPHeaders(ctx context.Context, header http.Header) {
	traceparent := Traceparent(ctx)
	if traceparent != "" {
		header.Set(HeaderTraceparent, traceparent)
	}
	if traceID := TraceID(ctx); traceID != "" {
		header.Set(HeaderTraceID, traceID)
	}
}

func InjectMetainfo(ctx context.Context) context.Context {
	if traceparent := Traceparent(ctx); traceparent != "" {
		ctx = metainfo.WithValue(ctx, HeaderTraceparent, traceparent)
	}
	if state := tracestate(ctx); state != "" {
		ctx = metainfo.WithValue(ctx, HeaderTracestate, state)
	}
	return ctx
}

func TraceID(ctx context.Context) string {
	if value, ok := ctx.Value(traceIDKey).(string); ok && value != "" {
		return value
	}
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

func Traceparent(ctx context.Context) string {
	if value, ok := ctx.Value(traceparentKey).(string); ok && value != "" {
		return value
	}
	if traceID := TraceID(ctx); traceID != "" {
		return MakeTraceparent(traceID)
	}
	return ""
}

func MakeTraceparent(traceID string) string {
	normalized := strings.ToLower(strings.TrimSpace(traceID))
	if normalized == "" {
		return ""
	}
	if !isW3CTraceID(normalized) {
		normalized = deriveTraceID(normalized)
	}
	return "00-" + normalized + "-" + newSpanID() + "-01"
}

func withTraceparent(ctx context.Context, fallbackTraceID string) context.Context {
	sc := trace.SpanContextFromContext(ctx)
	traceID := fallbackTraceID
	if sc.HasTraceID() {
		traceID = sc.TraceID().String()
	}
	ctx = context.WithValue(ctx, traceIDKey, traceID)
	if sc.IsValid() {
		carrier := propagation.MapCarrier{}
		traceContext.Inject(ctx, carrier)
		if traceparent := carrier.Get(HeaderTraceparent); traceparent != "" {
			ctx = context.WithValue(ctx, traceparentKey, traceparent)
		}
	}
	if Traceparent(ctx) == "" {
		ctx = context.WithValue(ctx, traceparentKey, MakeTraceparent(traceID))
	}
	return ctx
}

func tracestate(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	traceContext.Inject(ctx, carrier)
	return carrier.Get(HeaderTracestate)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func newLocalTraceID() string {
	return "local-" + time.Now().UTC().Format("20060102150405.000000000")
}

func newSpanID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000000000000001"
	}
	if b == [8]byte{} {
		b[7] = 1
	}
	return hex.EncodeToString(b[:])
}

func isW3CTraceID(value string) bool {
	if len(value) != 32 || value == "00000000000000000000000000000000" {
		return false
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func deriveTraceID(value string) string {
	sum := sha256.Sum256([]byte(value))
	out := hex.EncodeToString(sum[:16])
	if out == "00000000000000000000000000000000" {
		return "00000000000000000000000000000001"
	}
	return out
}
