package usermessageruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	modelRequestSchemaVersion = "user_message.model_request.v2preview1"
	// ModelFailureCodeExecutionFailed 是本地 Fake Model 唯一持久化失败码，不包含底层错误文本。
	ModelFailureCodeExecutionFailed = "LOCAL_FAKE_MODEL_EXECUTION_FAILED"
)

// ModelReceiptStage 是 B3 本地 Fake Model 的精确状态集合；不包含真实 Provider dispatched/model_unknown。
type ModelReceiptStage string

const (
	ModelReceiptReserved  ModelReceiptStage = "reserved"
	ModelReceiptCompleted ModelReceiptStage = "completed"
	ModelReceiptFailed    ModelReceiptStage = "failed"
)

// ModelReceiptIdentity 把模型回执写点绑定到当前 owner/fence 与全部稳定调用身份。
type ModelReceiptIdentity struct {
	Owner       string
	SessionID   string
	InputID     string
	TurnID      string
	RunID       string
	ModelCallID string
	FenceToken  int64
}

// ModelReceiptSnapshot 是一次真源读取；completed 必须有完整响应，failed 必须有稳定 error_code。
type ModelReceiptSnapshot struct {
	Stage     ModelReceiptStage
	Response  *schema.Message
	ErrorCode string
}

// ModelReceiptStore 由后续 PostgreSQL Adapter 实现 first-write-wins/CAS。
// 对本地 Fake，reserved 可由当前合法 owner/fence 安全执行；本接口不支持真实 Provider dispatch。
type ModelReceiptStore interface {
	// ReplayOrReserveModel 只在首次 reserve 或更高 Fence takeover 时返回 execute=true；同一 Fence 重放必须为 false。
	ReplayOrReserveModel(
		ctx context.Context,
		identity ModelReceiptIdentity,
		requestDigest string,
	) (snapshot ModelReceiptSnapshot, execute bool, err error)
	FreezeModelCompleted(
		ctx context.Context,
		identity ModelReceiptIdentity,
		requestDigest string,
		response *schema.Message,
	) error
	FreezeModelFailed(
		ctx context.Context,
		identity ModelReceiptIdentity,
		requestDigest string,
		errorCode string,
	) error
}

// ReceiptModel 在本地 Fake BaseChatModel 外增加单调用 first-write-wins 回执。
type ReceiptModel struct {
	base  model.BaseChatModel
	store ModelReceiptStore
}

// NewReceiptModel 创建 user_message.runtime.v2preview1 专用模型回执包装器。
func NewReceiptModel(base model.BaseChatModel, store ModelReceiptStore) (*ReceiptModel, error) {
	if base == nil || store == nil {
		return nil, fmt.Errorf("create user message receipt model: model and store are required")
	}
	return &ReceiptModel{base: base, store: store}, nil
}

// Generate 先重放/占位，再执行至多一次本地模型并冻结完成或失败；返回前总是重读首写结果。
func (m *ReceiptModel) Generate(
	ctx context.Context,
	messages []*schema.Message,
	options ...model.Option,
) (*schema.Message, error) {
	if len(model.GetCommonOptions(&model.Options{}, options...).Tools) != 0 {
		return nil, fmt.Errorf("run user message receipt model: executable tool registry must be empty")
	}
	trusted, ok := turncontext.UserMessageRuntimeFrom(ctx)
	if !ok || trusted.Profile != Profile || trusted.Owner == "" || trusted.FenceToken < 1 ||
		trusted.Context.SessionID == "" || trusted.Context.InputID == "" || trusted.Context.TurnID == "" ||
		trusted.RunID == "" || trusted.ModelCallID == "" {
		return nil, fmt.Errorf("run user message receipt model: trusted turn context is invalid")
	}
	identity := ModelReceiptIdentity{
		Owner: trusted.Owner, SessionID: trusted.Context.SessionID, InputID: trusted.Context.InputID,
		TurnID: trusted.Context.TurnID, RunID: trusted.RunID, ModelCallID: trusted.ModelCallID,
		FenceToken: trusted.FenceToken,
	}
	requestDigest, err := digestModelRequest(trusted, messages)
	if err != nil {
		return nil, err
	}
	snapshot, execute, err := m.store.ReplayOrReserveModel(ctx, identity, requestDigest)
	if err != nil {
		return nil, err
	}
	if response, terminalErr, terminal := terminalModelReceipt(snapshot); terminal {
		return response, terminalErr
	}
	if snapshot.Stage != ModelReceiptReserved {
		return nil, fmt.Errorf("run user message receipt model: unknown receipt stage")
	}
	if !execute {
		return nil, ErrModelReceiptReserved
	}
	response, runErr := m.base.Generate(ctx, messages, options...)
	if runErr != nil || response == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := m.store.FreezeModelFailed(ctx, identity, requestDigest, ModelFailureCodeExecutionFailed); err != nil {
			return nil, err
		}
		return m.reloadFrozenModel(ctx, identity, requestDigest)
	}
	if err := m.store.FreezeModelCompleted(ctx, identity, requestDigest, response); err != nil {
		return nil, err
	}
	return m.reloadFrozenModel(ctx, identity, requestDigest)
}

// Stream 返回单块冻结响应，仍保持 Eino BaseChatModel 接口完整。
func (m *ReceiptModel) Stream(
	ctx context.Context,
	messages []*schema.Message,
	options ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

func (m *ReceiptModel) reloadFrozenModel(
	ctx context.Context,
	identity ModelReceiptIdentity,
	requestDigest string,
) (*schema.Message, error) {
	snapshot, execute, err := m.store.ReplayOrReserveModel(ctx, identity, requestDigest)
	if err != nil {
		return nil, err
	}
	response, terminalErr, terminal := terminalModelReceipt(snapshot)
	if !terminal || execute {
		return nil, fmt.Errorf("run user message receipt model: frozen result is unavailable")
	}
	return response, terminalErr
}

func terminalModelReceipt(snapshot ModelReceiptSnapshot) (*schema.Message, error, bool) {
	switch snapshot.Stage {
	case ModelReceiptCompleted:
		if snapshot.Response == nil || snapshot.ErrorCode != "" {
			return nil, ErrOutputContract, true
		}
		return snapshot.Response, nil, true
	case ModelReceiptFailed:
		if snapshot.Response != nil || snapshot.ErrorCode == "" {
			return nil, ErrOutputContract, true
		}
		return nil, &ModelFailureError{Code: snapshot.ErrorCode}, true
	case ModelReceiptReserved:
		if snapshot.Response != nil || snapshot.ErrorCode != "" {
			return nil, ErrOutputContract, true
		}
		return nil, nil, false
	default:
		return nil, ErrOutputContract, true
	}
}

// ValidateCompletedModelResponse 把成功 Output Receipt 重新绑定到冻结的纯 Assistant 模型响应。
// PostgreSQL 终态事务必须在写 Projection/Event 前调用它，禁止绕过 Model Receipt 直接提交成功 Card。
func ValidateCompletedModelResponse(response *schema.Message, claim Claim, output Output) error {
	if response == nil || output.DirectResponse == nil || output.Failure != nil {
		return ErrOutputContract
	}
	if violations := pureAssistantViolations(response); len(violations) != 0 {
		return ErrOutputContract
	}
	card, err := DecodeDirectResponseCard(response.Content)
	if err != nil || ValidateDirectResponse(card, claim) != nil || ValidateOutput(output, claim) != nil {
		return ErrOutputContract
	}
	return nil
}

// ModelFailureError 只暴露持久稳定 error_code，不包含模型实现的原始错误文本。
type ModelFailureError struct{ Code string }

func (err *ModelFailureError) Error() string {
	return fmt.Sprintf("user message model failed: %s", err.Code)
}
func (err *ModelFailureError) Unwrap() error { return ErrModelFailed }

type modelRequestWire struct {
	SchemaVersion    string            `json:"schema_version"`
	Profile          string            `json:"profile"`
	ModelCallID      string            `json:"model_call_id"`
	ContextDigest    string            `json:"context_digest"`
	ModelRouteRef    string            `json:"model_route_ref"`
	ModelRouteDigest string            `json:"model_route_digest"`
	Messages         []*schema.Message `json:"messages"`
}

func digestModelRequest(trusted turncontext.UserMessageRuntime, messages []*schema.Message) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("compute user message model request digest: messages are empty")
	}
	encoded, err := json.Marshal(modelRequestWire{
		SchemaVersion: modelRequestSchemaVersion, Profile: trusted.Profile, ModelCallID: trusted.ModelCallID,
		ContextDigest: trusted.Context.ContextDigest, ModelRouteRef: trusted.Context.ModelRouteRef,
		ModelRouteDigest: trusted.Context.ModelRouteDigest, Messages: messages,
	})
	if err != nil {
		return "", fmt.Errorf("compute user message model request digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

var _ model.BaseChatModel = (*ReceiptModel)(nil)
