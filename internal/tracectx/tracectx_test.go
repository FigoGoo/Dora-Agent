package tracectx

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/bytedance/gopkg/cloud/metainfo"
)

func TestFromHTTPHeadersPrefersW3CTraceparent(t *testing.T) {
	header := http.Header{}
	header.Set(HeaderTraceparent, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	header.Set(HeaderTraceID, "legacy-trace")

	ctx := FromHTTPHeaders(t.Context(), header)
	if got := TraceID(ctx); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q", got)
	}
	if got := Traceparent(ctx); got != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("traceparent = %q", got)
	}
}

func TestFromHTTPHeadersFallsBackToLegacyTraceID(t *testing.T) {
	header := http.Header{}
	header.Set(HeaderTraceID, "trace-legacy")

	ctx := FromHTTPHeaders(t.Context(), header)
	if got := TraceID(ctx); got != "trace-legacy" {
		t.Fatalf("trace id = %q", got)
	}
	if got := Traceparent(ctx); !strings.HasPrefix(got, "00-") || !strings.Contains(got, deriveTraceID("trace-legacy")) {
		t.Fatalf("legacy trace id should synthesize traceparent, got %q", got)
	}
}

func TestInjectHTTPHeadersWritesTraceparentAndLegacyTraceID(t *testing.T) {
	header := http.Header{}
	incoming := http.Header{}
	incoming.Set(HeaderTraceparent, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	ctx := FromHTTPHeaders(t.Context(), incoming)

	InjectHTTPHeaders(ctx, header)
	if got := header.Get(HeaderTraceID); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("x-trace-id = %q", got)
	}
	if got := header.Get(HeaderTraceparent); got != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("traceparent = %q", got)
	}
}

func TestInjectAndExtractMetainfo(t *testing.T) {
	incoming := http.Header{}
	incoming.Set(HeaderTraceparent, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	ctx := FromHTTPHeaders(t.Context(), incoming)

	outgoing := InjectMetainfo(ctx)
	value, ok := metainfo.GetValue(outgoing, HeaderTraceparent)
	if !ok || value == "" {
		t.Fatalf("traceparent not injected into metainfo")
	}

	incomingCtx := metainfo.TransferForward(outgoing)
	extracted := FromMetainfo(incomingCtx)
	if got := TraceID(extracted); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("extracted trace id = %q", got)
	}
	if got := Traceparent(extracted); !strings.HasPrefix(got, "00-4bf92f3577b34da6a3ce929d0e0e4736-") {
		t.Fatalf("extracted traceparent = %q", got)
	}
}

func TestWithTraceIDCreatesTraceparentForW3CTraceID(t *testing.T) {
	ctx := WithTraceID(context.Background(), "4bf92f3577b34da6a3ce929d0e0e4736")
	if got := TraceID(ctx); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q", got)
	}
	if got := Traceparent(ctx); !strings.HasPrefix(got, "00-4bf92f3577b34da6a3ce929d0e0e4736-") {
		t.Fatalf("traceparent = %q", got)
	}
}
