package writepromptsruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// TestReceiptModelHigherFenceNeverReexecutesReservedCall 模拟进程在模型调用后、冻结回执前崩溃。
// 更高 Fence takeover 看到 reserved 只能失败关闭，不能再次调用底层模型。
func TestReceiptModelHigherFenceNeverReexecutesReservedCall(t *testing.T) {
	base := &countingWritePromptsModel{}
	store := &reservedWritePromptsModelStore{reservedFence: 1}
	receipt, err := NewReceiptModel(base, store, ModelCallGraphPrompt)
	if err != nil {
		t.Fatal(err)
	}
	pins := ApprovedPins()
	runtimeContext := turncontext.WritePromptsRuntime{
		Owner: "takeover-owner", FenceToken: 2,
		Context: turncontext.WritePromptsTurnContext{
			Profile: Profile, SessionID: "019f0000-0000-7000-8000-000000000001",
			InputID: "019f0000-0000-7000-8000-000000000002", TurnID: "019f0000-0000-7000-8000-000000000003",
			RunID: "019f0000-0000-7000-8000-000000000004", GraphModelCallID: "019f0000-0000-7000-8000-000000000005",
			ContextDigest: stringsOfHex('a'), PromptModelRouteRef: pins.PromptModelRouteRef,
			PromptModelRouteDigest: pins.PromptModelRouteDigest,
		},
	}
	ctx := turncontext.WithWritePromptsRuntime(context.Background(), runtimeContext)
	_, err = receipt.Generate(ctx, []*schema.Message{schema.UserMessage("frozen prompt")})
	if !errors.Is(err, ErrModelReceiptReserved) {
		t.Fatalf("higher Fence reserved 调用错误=%v want=%v", err, ErrModelReceiptReserved)
	}
	if base.calls != 0 || store.freezeCalls != 0 || store.seenFence != 2 {
		t.Fatalf("reserved takeover 发生重复执行: base_calls=%d freeze_calls=%d fence=%d", base.calls, store.freezeCalls, store.seenFence)
	}
}

type countingWritePromptsModel struct{ calls int }

func (base *countingWritePromptsModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	base.calls++
	return schema.AssistantMessage("must-not-run", nil), nil
}

func (base *countingWritePromptsModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := base.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

type reservedWritePromptsModelStore struct {
	reservedFence int64
	seenFence     int64
	freezeCalls   int
}

func (store *reservedWritePromptsModelStore) ReplayOrReserveModel(
	_ context.Context,
	identity ModelReceiptIdentity,
	_ string,
) (ModelReceiptSnapshot, bool, error) {
	store.seenFence = identity.FenceToken
	if identity.FenceToken <= store.reservedFence {
		return ModelReceiptSnapshot{}, false, ErrFenceLost
	}
	return ModelReceiptSnapshot{Stage: ModelReceiptReserved}, false, nil
}

func (store *reservedWritePromptsModelStore) FreezeModelCompleted(context.Context, ModelReceiptIdentity, string, *schema.Message) error {
	store.freezeCalls++
	return nil
}

func (store *reservedWritePromptsModelStore) FreezeModelFailed(context.Context, ModelReceiptIdentity, string, string) error {
	store.freezeCalls++
	return nil
}

func stringsOfHex(value byte) string {
	encoded := make([]byte, 64)
	for index := range encoded {
		encoded[index] = value
	}
	return string(encoded)
}

var _ model.BaseChatModel = (*countingWritePromptsModel)(nil)
