package analyzematerials

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type classifiedChatModel struct {
	model.BaseChatModel
}

func classifyModelErrors(base model.BaseChatModel) model.BaseChatModel {
	return &classifiedChatModel{BaseChatModel: base}
}

func (m *classifiedChatModel) Generate(
	ctx context.Context,
	input []*schema.Message,
	opts ...model.Option,
) (*schema.Message, error) {
	message, err := m.BaseChatModel.Generate(ctx, input, opts...)
	return message, classifyModelError(err)
}

func (m *classifiedChatModel) Stream(
	ctx context.Context,
	input []*schema.Message,
	opts ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	reader, err := m.BaseChatModel.Stream(ctx, input, opts...)
	return reader, classifyModelError(err)
}

func classifyModelError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return newContractError(ResultCodeModelFailed, err)
}

// captureModelMessage 是真实 AddChatModelNode 的 State post-handler；这里只冻结模型响应，不解析 Candidate。
func (*graphBuilder) captureModelMessage(_ context.Context, message *schema.Message, state *State) (*schema.Message, error) {
	if !plainModelCandidateMessage(message) {
		return nil, contractErrorf(ResultCodeModelFailed, "capture primary model message: invalid assistant response")
	}
	// 只把 Validator 所需的 Role + Content 带过模型边界，避免 Provider metadata、
	// reasoning 或新版多模态字段共享到调用 State。
	cloned := &schema.Message{Role: schema.Assistant, Content: message.Content}
	state.ModelMessage = cloned
	return &schema.Message{Role: cloned.Role, Content: cloned.Content}, nil
}

func plainModelCandidateMessage(message *schema.Message) bool {
	return message != nil && message.Role == schema.Assistant && strings.TrimSpace(message.Content) != "" &&
		len(message.ToolCalls) == 0 && len(message.MultiContent) == 0 &&
		len(message.UserInputMultiContent) == 0 && len(message.AssistantGenMultiContent) == 0 &&
		message.Name == "" && message.ToolCallID == "" && message.ToolName == "" &&
		message.ResponseMeta == nil && message.ReasoningContent == "" && len(message.Extra) == 0
}
