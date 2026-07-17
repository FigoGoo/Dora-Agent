package usermessageruntime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var errModelReceiptTestConflict = errors.New("model receipt digest conflict")

// TestReceiptModelFirstWriteWinsAndReplays 验证同一稳定 model_call 只执行一次底层模型并重放首写响应。
func TestReceiptModelFirstWriteWinsAndReplays(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	card := NewDirectResponse(claim)
	encoded, _ := json.Marshal(card)
	base := &modelReceiptTestBase{response: schema.AssistantMessage(string(encoded), nil)}
	store := newModelReceiptTestStore()
	wrapped, err := NewReceiptModel(base, store)
	if err != nil {
		t.Fatalf("创建 User Message ReceiptModel 失败: %v", err)
	}
	ctx := receiptModelTestContext(claim)
	messages := []*schema.Message{schema.SystemMessage("fixed"), schema.UserMessage(claim.MessagePlaintext)}
	first, err := wrapped.Generate(ctx, messages)
	if err != nil {
		t.Fatalf("首次模型调用失败: %v", err)
	}
	base.response = schema.AssistantMessage("must not escape", nil)
	second, err := wrapped.Generate(ctx, messages)
	if err != nil {
		t.Fatalf("模型回执重放失败: %v", err)
	}
	if first.Content != second.Content || base.calls != 1 || store.completeCalls != 1 {
		t.Fatalf("模型首写重放漂移: first=%q second=%q calls=%d freezes=%d", first.Content, second.Content, base.calls, store.completeCalls)
	}
}

// TestReceiptModelFreezesStableFailure 验证底层错误只冻结稳定 error_code，不泄漏原始错误且不重复执行。
func TestReceiptModelFreezesStableFailure(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	base := &modelReceiptTestBase{err: errors.New("secret provider detail")}
	store := newModelReceiptTestStore()
	wrapped, _ := NewReceiptModel(base, store)
	ctx := receiptModelTestContext(claim)
	messages := []*schema.Message{schema.SystemMessage("fixed"), schema.UserMessage(claim.MessagePlaintext)}

	for index := 0; index < 2; index++ {
		_, err := wrapped.Generate(ctx, messages)
		if !errors.Is(err, ErrModelFailed) || err.Error() == "secret provider detail" {
			t.Fatalf("冻结失败[%d] err=%v want stable model failure", index, err)
		}
	}
	if base.calls != 1 || store.failedCalls != 1 || store.entry.snapshot.ErrorCode != ModelFailureCodeExecutionFailed {
		t.Fatalf("模型失败回执不稳定: calls=%d failed=%d snapshot=%+v", base.calls, store.failedCalls, store.entry.snapshot)
	}
}

// TestReceiptModelRejectsDigestConflictAndReservedWithoutAuthorization 验证异义请求或未授权 reserved 不进入底层模型。
func TestReceiptModelRejectsDigestConflictAndReservedWithoutAuthorization(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	card := NewDirectResponse(claim)
	encoded, _ := json.Marshal(card)
	base := &modelReceiptTestBase{response: schema.AssistantMessage(string(encoded), nil)}
	store := newModelReceiptTestStore()
	wrapped, _ := NewReceiptModel(base, store)
	ctx := receiptModelTestContext(claim)
	firstMessages := []*schema.Message{schema.SystemMessage("fixed"), schema.UserMessage(claim.MessagePlaintext)}
	if _, err := wrapped.Generate(ctx, firstMessages); err != nil {
		t.Fatalf("建立首个模型回执失败: %v", err)
	}
	if _, err := wrapped.Generate(ctx, []*schema.Message{schema.SystemMessage("fixed"), schema.UserMessage("other")}); !errors.Is(err, errModelReceiptTestConflict) {
		t.Fatalf("异义模型请求 err=%v want conflict", err)
	}

	store = newModelReceiptTestStore()
	store.reserveWithoutExecute = true
	wrapped, _ = NewReceiptModel(base, store)
	if _, err := wrapped.Generate(ctx, firstMessages); !errors.Is(err, ErrModelReceiptReserved) {
		t.Fatalf("未授权 reserved err=%v want reserved", err)
	}
	if base.calls != 1 {
		t.Fatalf("未授权 reserved 意外调用模型: calls=%d", base.calls)
	}
}

// TestValidateCompletedModelResponseBindsSuccessOutput 验证终态成功不能脱离冻结模型响应独立提交。
func TestValidateCompletedModelResponseBindsSuccessOutput(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	card := NewDirectResponse(claim)
	encoded, _ := json.Marshal(card)
	output := Output{DirectResponse: &card}
	if err := ValidateCompletedModelResponse(schema.AssistantMessage(string(encoded), nil), claim, output); err != nil {
		t.Fatalf("合法冻结模型响应未通过绑定校验: %v", err)
	}
	if err := ValidateCompletedModelResponse(nil, claim, output); !errors.Is(err, ErrOutputContract) {
		t.Fatalf("缺失模型响应 err=%v want output contract", err)
	}
	toolResponse := schema.AssistantMessage(string(encoded), []schema.ToolCall{{ID: "forbidden"}})
	if err := ValidateCompletedModelResponse(toolResponse, claim, output); !errors.Is(err, ErrOutputContract) {
		t.Fatalf("ToolCall 模型响应 err=%v want output contract", err)
	}
	failure := NewFailure(claim, false)
	if err := ValidateCompletedModelResponse(schema.AssistantMessage(string(encoded), nil), claim, Output{Failure: &failure}); !errors.Is(err, ErrOutputContract) {
		t.Fatalf("失败 Output 错绑 completed 模型响应 err=%v want output contract", err)
	}
}

// TestModelReceiptAuthorizationAdvancesOnlyOnTakeoverFence 冻结 reserved 执行权：同 Fence 不得二次调用，只有更高 takeover Fence 可恢复本地纯 Fake。
func TestModelReceiptAuthorizationAdvancesOnlyOnTakeoverFence(t *testing.T) {
	store := newModelReceiptTestStore()
	identity := ModelReceiptIdentity{FenceToken: 7}
	if _, execute, err := store.ReplayOrReserveModel(context.Background(), identity, "digest"); err != nil || !execute {
		t.Fatalf("首次 reserve 未取得执行权: execute=%v err=%v", execute, err)
	}
	if _, execute, err := store.ReplayOrReserveModel(context.Background(), identity, "digest"); err != nil || execute {
		t.Fatalf("同 Fence 重放重复取得执行权: execute=%v err=%v", execute, err)
	}
	identity.FenceToken++
	if _, execute, err := store.ReplayOrReserveModel(context.Background(), identity, "digest"); err != nil || !execute {
		t.Fatalf("更高 takeover Fence 未取得恢复权: execute=%v err=%v", execute, err)
	}
}

type modelReceiptTestBase struct {
	response *schema.Message
	err      error
	calls    int
}

func (m *modelReceiptTestBase) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	copy := *m.response
	copy.ToolCalls = append([]schema.ToolCall(nil), m.response.ToolCalls...)
	return &copy, nil
}

func (m *modelReceiptTestBase) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

type modelReceiptTestEntry struct {
	digest         string
	executionFence int64
	snapshot       ModelReceiptSnapshot
}

type modelReceiptTestStore struct {
	mu                    sync.Mutex
	entry                 modelReceiptTestEntry
	exists                bool
	reserveWithoutExecute bool
	completeCalls         int
	failedCalls           int
}

func newModelReceiptTestStore() *modelReceiptTestStore { return &modelReceiptTestStore{} }

func (s *modelReceiptTestStore) ReplayOrReserveModel(
	_ context.Context,
	identity ModelReceiptIdentity,
	requestDigest string,
) (ModelReceiptSnapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exists {
		if s.entry.digest != requestDigest {
			return ModelReceiptSnapshot{}, false, errModelReceiptTestConflict
		}
		execute := false
		if s.entry.snapshot.Stage == ModelReceiptReserved && !s.reserveWithoutExecute && identity.FenceToken > s.entry.executionFence {
			s.entry.executionFence = identity.FenceToken
			execute = true
		}
		return cloneModelReceiptSnapshot(s.entry.snapshot), execute, nil
	}
	s.exists = true
	s.entry = modelReceiptTestEntry{
		digest: requestDigest, executionFence: identity.FenceToken,
		snapshot: ModelReceiptSnapshot{Stage: ModelReceiptReserved},
	}
	return s.entry.snapshot, !s.reserveWithoutExecute, nil
}

func (s *modelReceiptTestStore) FreezeModelCompleted(
	_ context.Context,
	_ ModelReceiptIdentity,
	requestDigest string,
	response *schema.Message,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exists || s.entry.digest != requestDigest {
		return errModelReceiptTestConflict
	}
	if s.entry.snapshot.Stage == ModelReceiptReserved {
		copy := *response
		copy.ToolCalls = append([]schema.ToolCall(nil), response.ToolCalls...)
		s.entry.snapshot = ModelReceiptSnapshot{Stage: ModelReceiptCompleted, Response: &copy}
		s.completeCalls++
	}
	return nil
}

func (s *modelReceiptTestStore) FreezeModelFailed(
	_ context.Context,
	_ ModelReceiptIdentity,
	requestDigest string,
	errorCode string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exists || s.entry.digest != requestDigest {
		return errModelReceiptTestConflict
	}
	if s.entry.snapshot.Stage == ModelReceiptReserved {
		s.entry.snapshot = ModelReceiptSnapshot{Stage: ModelReceiptFailed, ErrorCode: errorCode}
		s.failedCalls++
	}
	return nil
}

func cloneModelReceiptSnapshot(snapshot ModelReceiptSnapshot) ModelReceiptSnapshot {
	if snapshot.Response != nil {
		copy := *snapshot.Response
		copy.ToolCalls = append([]schema.ToolCall(nil), snapshot.Response.ToolCalls...)
		snapshot.Response = &copy
	}
	return snapshot
}

func receiptModelTestContext(claim Claim) context.Context {
	return turncontext.WithUserMessageRuntime(context.Background(), turncontext.UserMessageRuntime{
		Profile: claim.Profile, Owner: claim.Owner, RunID: claim.RunID,
		ModelCallID: claim.ModelCallID, OutputID: claim.OutputID, FenceToken: claim.FenceToken,
		Context: claim.Context,
	})
}

var _ model.BaseChatModel = (*modelReceiptTestBase)(nil)
var _ ModelReceiptStore = (*modelReceiptTestStore)(nil)
