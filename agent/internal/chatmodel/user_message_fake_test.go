package chatmodel

import (
	"context"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// TestUserMessageFakeReturnsFixedCardWithoutPrompt 验证本地模型只返回固定 Card，不复制用户原文。
func TestUserMessageFakeReturnsFixedCardWithoutPrompt(t *testing.T) {
	ctx := turncontext.WithUserMessageRuntime(context.Background(), fakeUserMessageRuntimeContext())
	secretPrompt := "不要把这段用户原文复制到输出"
	response, err := NewUserMessageFake().Generate(ctx, []*schema.Message{
		schema.SystemMessage("fixed instruction"), schema.UserMessage(secretPrompt),
	})
	if err != nil {
		t.Fatalf("执行 User Message Fake 失败: %v", err)
	}
	if response.Role != schema.Assistant || len(response.ToolCalls) != 0 || strings.Contains(response.Content, secretPrompt) {
		t.Fatalf("Fake 输出不是纯安全 Assistant: %+v", response)
	}
	card, err := usermessageruntime.DecodeDirectResponseCard(response.Content)
	if err != nil || card.Summary != usermessageruntime.DirectResponseSummary {
		t.Fatalf("Fake Card 不符合固定契约: card=%+v err=%v", card, err)
	}
}

// TestUserMessageFakeRejectsToolsAndMissingContext 验证模型自身也对动态 Tool 注入和缺失可信上下文失败关闭。
func TestUserMessageFakeRejectsToolsAndMissingContext(t *testing.T) {
	messages := []*schema.Message{schema.SystemMessage("fixed"), schema.UserMessage("request")}
	if _, err := NewUserMessageFake().Generate(context.Background(), messages); err == nil {
		t.Fatal("缺失可信 Context 仍执行 Fake")
	}
	ctx := turncontext.WithUserMessageRuntime(context.Background(), fakeUserMessageRuntimeContext())
	_, err := NewUserMessageFake().Generate(ctx, messages, model.WithTools([]*schema.ToolInfo{{Name: "forbidden"}}))
	if err == nil {
		t.Fatal("动态 Tool 注入未失败关闭")
	}
}

func fakeUserMessageRuntimeContext() turncontext.UserMessageRuntime {
	return turncontext.UserMessageRuntime{
		Profile: usermessageruntime.Profile, Owner: "processor-test",
		RunID:       "019f68e8-7101-7000-8000-000000000001",
		ModelCallID: "019f68e8-7102-7000-8000-000000000002",
		OutputID:    "019f68e8-7103-7000-8000-000000000003", FenceToken: 3,
		Context: turncontext.UserMessageTurnContext{
			SchemaVersion:   turncontext.UserMessageTurnContextSchemaVersion,
			TurnID:          "019f68e8-7104-7000-8000-000000000004",
			SessionID:       "019f68e8-7105-7000-8000-000000000005",
			InputID:         "019f68e8-7106-7000-8000-000000000006",
			ToolRegistryRef: usermessageruntime.EmptyToolRegistryRef,
			ModelRouteRef:   usermessageruntime.LocalFakeModelRouteRef,
		},
	}
}
