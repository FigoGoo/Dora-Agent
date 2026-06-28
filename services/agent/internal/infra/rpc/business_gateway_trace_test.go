package rpc

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/tracectx"
	"github.com/bytedance/gopkg/cloud/metainfo"
)

func TestBusinessGatewayCallContextInjectsTraceparentMetainfo(t *testing.T) {
	gateway := &BusinessGateway{timeout: time.Second}
	ctx := tracectx.WithTraceID(t.Context(), "4bf92f3577b34da6a3ce929d0e0e4736")

	callCtx, cancel := gateway.callContext(ctx, "ignored")
	defer cancel()

	value, ok := metainfo.GetValue(callCtx, tracectx.HeaderTraceparent)
	if !ok || value == "" {
		t.Fatalf("traceparent not injected")
	}
	if got := tracectx.TraceID(callCtx); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q", got)
	}
}
