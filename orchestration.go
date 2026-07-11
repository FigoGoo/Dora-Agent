package DoraAgent

import (
	"context"

	"github.com/cloudwego/eino/compose"
)

func BuildImageTool(ctx context.Context) (r compose.Runnable[*string, *string], err error) {
	const ToolsNode1 = "ToolsNode1"
	g := compose.NewGraph[*string, *string]()
	toolsNode1KeyOfToolsNode, err := newToolsNode(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddToolsNode(ToolsNode1, toolsNode1KeyOfToolsNode)
	_ = g.AddEdge(compose.START, ToolsNode1)
	_ = g.AddEdge(ToolsNode1, compose.END)
	r, err = g.Compile(ctx, compose.WithGraphName("ImageTool"), compose.WithNodeTriggerMode(compose.AnyPredecessor))
	if err != nil {
		return nil, err
	}
	return r, err
}
