package rpc

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/tracectx"
	"github.com/bytedance/gopkg/cloud/metainfo"
	"github.com/cloudwego/kitex/pkg/endpoint"
)

func TestTraceContextMiddlewareExtractsTraceparentFromMetainfo(t *testing.T) {
	ctx := metainfo.WithValue(context.Background(), tracectx.HeaderTraceparent, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	var gotTraceID string
	next := endpoint.Endpoint(func(ctx context.Context, req, resp interface{}) error {
		gotTraceID = tracectx.TraceID(ctx)
		return nil
	})

	if err := TraceContextMiddleware(next)(ctx, nil, nil); err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if gotTraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q", gotTraceID)
	}
}
