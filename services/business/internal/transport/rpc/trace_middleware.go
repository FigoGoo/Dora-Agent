package rpc

import (
	"context"

	"github.com/FigoGoo/Dora-Agent/internal/tracectx"
	"github.com/cloudwego/kitex/pkg/endpoint"
)

func TraceContextMiddleware(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, req, resp interface{}) error {
		ctx = tracectx.FromMetainfo(ctx)
		return next(ctx, req, resp)
	}
}
