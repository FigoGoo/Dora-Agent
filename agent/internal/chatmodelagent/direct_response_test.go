package chatmodelagent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// TestNewDirectResponseRequiresModel 验证无 Tool 构造器不允许缺失模型，且不会改变既有 New 的签名或行为。
func TestNewDirectResponseRequiresModel(t *testing.T) {
	if _, err := NewDirectResponse(context.Background(), nil); err == nil {
		t.Fatal("缺失模型仍创建 Direct Response Agent")
	}
	agent, err := NewDirectResponse(context.Background(), directResponseTestModel{})
	if err != nil {
		t.Fatalf("创建 Direct Response Agent 失败: %v", err)
	}
	if got := agent.Name(context.Background()); got != DirectResponseName {
		t.Fatalf("Direct Response Agent 名称=%q want=%q", got, DirectResponseName)
	}
}

type directResponseTestModel struct{}

func (directResponseTestModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage(`{"schema_version":"test"}`, nil), nil
}

func (m directResponseTestModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

var _ model.BaseChatModel = directResponseTestModel{}
