package usermessageruntime_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodel"
	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	usermessageruntime "github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// TestEinoRunnerExecutesOneNoToolModelCallAndReplaysReceipt 验证真实 ADK 无 Tool 路径只调用一次模型，重跑由模型回执重放。
func TestEinoRunnerExecutesOneNoToolModelCallAndReplaysReceipt(t *testing.T) {
	claim := externalRuntimeTestClaim(t)
	base := &runnerCountingModel{base: chatmodel.NewUserMessageFake()}
	store := &runnerModelReceiptStore{}
	receipted, err := usermessageruntime.NewReceiptModel(base, store)
	if err != nil {
		t.Fatalf("创建模型回执包装器失败: %v", err)
	}
	agent, err := chatmodelagent.NewDirectResponse(context.Background(), receipted)
	if err != nil {
		t.Fatalf("创建无 Tool Agent 失败: %v", err)
	}
	runner, err := usermessageruntime.NewEinoRunner(context.Background(), agent)
	if err != nil {
		t.Fatalf("创建 Eino Runner 失败: %v", err)
	}
	for index := 0; index < 2; index++ {
		output, runErr := runner.Run(context.Background(), claim)
		if runErr != nil || output.DirectResponse == nil || output.Failure != nil ||
			output.DirectResponse.Summary != usermessageruntime.DirectResponseSummary {
			t.Fatalf("Runner[%d] 输出异常: output=%+v err=%v", index, output, runErr)
		}
	}
	if base.calls != 1 || store.freezeCalls != 1 || base.lastToolCount != 0 {
		t.Fatalf("无 Tool 单调用预算漂移: model_calls=%d freezes=%d tools=%d", base.calls, store.freezeCalls, base.lastToolCount)
	}
}

// TestEinoRunnerRejectsToolCallUnknownFieldsNonPureAndOversize 验证所有非 exact-set Assistant 输出失败关闭。
func TestEinoRunnerRejectsToolCallUnknownFieldsNonPureAndOversize(t *testing.T) {
	claim := externalRuntimeTestClaim(t)
	valid := usermessageruntime.NewDirectResponse(claim)
	validJSON, _ := validOutputJSON(valid)
	tests := []struct {
		name    string
		message *schema.Message
	}{
		{
			name: "tool call",
			message: schema.AssistantMessage(validJSON, []schema.ToolCall{{
				ID: "019f68e8-7501-7000-8000-000000000001", Type: "function",
				Function: schema.FunctionCall{Name: "plan_creation_spec", Arguments: `{}`},
			}}),
		},
		{name: "unknown json field", message: schema.AssistantMessage(strings.Replace(validJSON, `"summary":`, `"unknown":true,"summary":`, 1), nil)},
		{name: "oversize", message: schema.AssistantMessage(strings.Repeat("x", usermessageruntime.MaxModelOutputBytes+1), nil)},
		{name: "non-pure reasoning", message: &schema.Message{Role: schema.Assistant, Content: validJSON, ReasoningContent: "hidden"}},
		{name: "non-ADK extra", message: &schema.Message{Role: schema.Assistant, Content: validJSON, Extra: map[string]any{"provider": "hidden"}}},
		{name: "non-assistant", message: &schema.Message{Role: schema.Tool, Content: validJSON, ToolCallID: "call"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agent, err := chatmodelagent.NewDirectResponse(context.Background(), &runnerFixedModel{message: test.message})
			if err != nil {
				t.Fatalf("创建测试 Agent 失败: %v", err)
			}
			runner, err := usermessageruntime.NewEinoRunner(context.Background(), agent)
			if err != nil {
				t.Fatalf("创建测试 Runner 失败: %v", err)
			}
			if _, err := runner.Run(context.Background(), claim); !errors.Is(err, usermessageruntime.ErrOutputContract) {
				t.Fatalf("非法输出 err=%v want output contract", err)
			}
		})
	}
}

type runnerCountingModel struct {
	base          model.BaseChatModel
	calls         int
	lastToolCount int
}

func (m *runnerCountingModel) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	m.calls++
	m.lastToolCount = len(model.GetCommonOptions(&model.Options{}, options...).Tools)
	return m.base.Generate(ctx, messages, options...)
}

func (m *runnerCountingModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

type runnerFixedModel struct{ message *schema.Message }

func (m *runnerFixedModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	copy := *m.message
	copy.ToolCalls = append([]schema.ToolCall(nil), m.message.ToolCalls...)
	return &copy, nil
}

func (m *runnerFixedModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

type runnerModelReceiptStore struct {
	mu          sync.Mutex
	digest      string
	response    *schema.Message
	freezeCalls int
}

func (s *runnerModelReceiptStore) ReplayOrReserveModel(
	_ context.Context,
	_ usermessageruntime.ModelReceiptIdentity,
	digest string,
) (usermessageruntime.ModelReceiptSnapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.digest == "" {
		s.digest = digest
		return usermessageruntime.ModelReceiptSnapshot{Stage: usermessageruntime.ModelReceiptReserved}, true, nil
	}
	if s.digest != digest {
		return usermessageruntime.ModelReceiptSnapshot{}, false, errors.New("digest conflict")
	}
	if s.response == nil {
		return usermessageruntime.ModelReceiptSnapshot{Stage: usermessageruntime.ModelReceiptReserved}, true, nil
	}
	copy := *s.response
	copy.ToolCalls = append([]schema.ToolCall(nil), s.response.ToolCalls...)
	return usermessageruntime.ModelReceiptSnapshot{Stage: usermessageruntime.ModelReceiptCompleted, Response: &copy}, false, nil
}

func (s *runnerModelReceiptStore) FreezeModelCompleted(
	_ context.Context,
	_ usermessageruntime.ModelReceiptIdentity,
	digest string,
	response *schema.Message,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.digest != digest {
		return errors.New("digest conflict")
	}
	if s.response == nil {
		copy := *response
		copy.ToolCalls = append([]schema.ToolCall(nil), response.ToolCalls...)
		s.response = &copy
		s.freezeCalls++
	}
	return nil
}

func (*runnerModelReceiptStore) FreezeModelFailed(
	context.Context,
	usermessageruntime.ModelReceiptIdentity,
	string,
	string,
) error {
	return errors.New("unexpected model failure")
}

func externalRuntimeTestClaim(t *testing.T) usermessageruntime.Claim {
	t.Helper()
	message := "帮我制作一张产品海报"
	messageDigest := sha256.Sum256([]byte(message))
	contextValue := turncontext.UserMessageTurnContext{
		SchemaVersion:    turncontext.UserMessageTurnContextSchemaVersion,
		TurnID:           "019f68e8-7401-7000-8000-000000000001",
		SessionID:        "019f68e8-7402-7000-8000-000000000002",
		InputID:          "019f68e8-7403-7000-8000-000000000003",
		MessageID:        "019f68e8-7404-7000-8000-000000000004",
		UserID:           "019f68e8-7405-7000-8000-000000000005",
		ProjectID:        "019f68e8-7406-7000-8000-000000000006",
		MessageCutoffSeq: 1, MessageContentDigest: hex.EncodeToString(messageDigest[:]),
		SkillSnapshotRef:    "session_skill_snapshot:019f68e8-7402-7000-8000-000000000002",
		SkillSnapshotDigest: strings.Repeat("a", 64),
		PromptRef:           usermessageruntime.PromptRef, PromptDigest: usermessageruntime.PromptDigest,
		ToolRegistryRef:     usermessageruntime.EmptyToolRegistryRef,
		ToolRegistryDigest:  usermessageruntime.EmptyToolRegistryDigest,
		RuntimePolicyRef:    usermessageruntime.RuntimePolicyRef,
		RuntimePolicyDigest: usermessageruntime.RuntimePolicyDigest,
		ModelRouteRef:       usermessageruntime.LocalFakeModelRouteRef,
		ModelRouteDigest:    usermessageruntime.LocalFakeModelRouteDigest,
		BudgetRef:           usermessageruntime.BudgetRef, BudgetDigest: usermessageruntime.BudgetDigest,
		AccessScopeRef:    "ensure_command:019f68e8-7410-7000-8000-000000000010",
		AccessScopeDigest: strings.Repeat("b", 64),
	}
	digest, err := session.DigestUserMessageContext(session.UserMessageContext{
		TurnID: contextValue.TurnID, SchemaVersion: contextValue.SchemaVersion,
		SessionID: contextValue.SessionID, InputID: contextValue.InputID, MessageID: contextValue.MessageID,
		UserID: contextValue.UserID, ProjectID: contextValue.ProjectID,
		MessageCutoffSeq: contextValue.MessageCutoffSeq, MessageContentDigest: contextValue.MessageContentDigest,
		SkillSnapshotRef: contextValue.SkillSnapshotRef, SkillSnapshotDigest: contextValue.SkillSnapshotDigest,
		PromptRef: contextValue.PromptRef, PromptDigest: contextValue.PromptDigest,
		ToolRegistryRef: contextValue.ToolRegistryRef, ToolRegistryDigest: contextValue.ToolRegistryDigest,
		RuntimePolicyRef: contextValue.RuntimePolicyRef, RuntimePolicyDigest: contextValue.RuntimePolicyDigest,
		ModelRouteRef: contextValue.ModelRouteRef, ModelRouteDigest: contextValue.ModelRouteDigest,
		BudgetRef: contextValue.BudgetRef, BudgetDigest: contextValue.BudgetDigest,
		AccessScopeRef: contextValue.AccessScopeRef, AccessScopeDigest: contextValue.AccessScopeDigest,
	})
	if err != nil {
		t.Fatalf("计算 Context digest 失败: %v", err)
	}
	contextValue.ContextDigest = digest
	return usermessageruntime.Claim{
		Profile: usermessageruntime.Profile, Owner: "runner-test",
		RunID:           "019f68e8-7407-7000-8000-000000000007",
		ModelCallID:     "019f68e8-7408-7000-8000-000000000008",
		OutputID:        "019f68e8-7409-7000-8000-000000000009",
		RecoveryEventID: "019f68e8-7411-7000-8000-000000000011",
		TerminalEventID: "019f68e8-7412-7000-8000-000000000012",
		FenceToken:      3, Attempts: 1, EnqueueSeq: 1, Context: contextValue, MessagePlaintext: message,
	}
}

func validOutputJSON(card usermessageruntime.DirectResponseCard) (string, error) {
	output := usermessageruntime.Output{DirectResponse: &card}
	encoded, err := output.CanonicalJSON()
	return string(encoded), err
}

var _ model.BaseChatModel = (*runnerCountingModel)(nil)
var _ model.BaseChatModel = (*runnerFixedModel)(nil)
var _ usermessageruntime.ModelReceiptStore = (*runnerModelReceiptStore)(nil)
