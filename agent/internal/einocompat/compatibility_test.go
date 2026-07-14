// Package einocompat 验证 Dora 已审核的 Eino 依赖面，不注册任何生产 Agent 或 Graph Tool。
package einocompat

import (
	"context"
	"testing"

	"github.com/cloudwego/eino-ext/components/model/deepseek"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

var _ model.BaseChatModel = (*deepseek.ChatModel)(nil)
var _ model.ToolCallingChatModel = (*deepseek.ChatModel)(nil)
var _ model.BaseModel[*schema.Message] = (*deepseek.ChatModel)(nil)
var _ func(context.Context, *adk.ChatModelAgentConfig) (*adk.ChatModelAgent, error) = adk.NewChatModelAgent
var _ = classicAgentConfig((*deepseek.ChatModel)(nil))

// classicAgentConfig 用类型关系固定经典 ChatModel 可以直接装配到经典 Message Agent 配置。
func classicAgentConfig(chatModel model.BaseChatModel) *adk.ChatModelAgentConfig {
	return &adk.ChatModelAgentConfig{Model: chatModel}
}

// TestPinnedEinoClassicGraphCompatibility 固定 Dora 使用的经典 Message、DAG Compile 和 Invoke API。
func TestPinnedEinoClassicGraphCompatibility(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	graph := compose.NewGraph[string, string]()
	if err := graph.AddLambdaNode("identity", compose.InvokableLambda(
		func(_ context.Context, input string) (string, error) {
			return input, nil
		},
	)); err != nil {
		t.Fatalf("add compatibility lambda node: %v", err)
	}
	if err := graph.AddEdge(compose.START, "identity"); err != nil {
		t.Fatalf("add compatibility start edge: %v", err)
	}
	if err := graph.AddEdge("identity", compose.END); err != nil {
		t.Fatalf("add compatibility end edge: %v", err)
	}

	runnable, err := graph.Compile(
		ctx,
		compose.WithGraphName("dora_eino_dependency_compatibility"),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
	if err != nil {
		t.Fatalf("compile compatibility graph: %v", err)
	}

	const input = "classic-schema-message-compatible"
	output, err := runnable.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("invoke compatibility graph: %v", err)
	}
	if output != input {
		t.Fatalf("compatibility graph output = %q, want %q", output, input)
	}
}
