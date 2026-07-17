package analyzematerialsruntime

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

// ModelCallKind 区分外层 Router 和 Graph 内部素材候选模型的独立回执命名空间。
type ModelCallKind string

const (
	// ModelCallRouter 是唯一主 Agent 的一次 ToolCall 生成。
	ModelCallRouter ModelCallKind = "router"
	// ModelCallGraphAnalysis 是 Graph call_model_primary 的一次候选生成。
	ModelCallGraphAnalysis ModelCallKind = "graph_analysis"
)

// ModelReceiptStage 是本地 Fake Model 的完整状态集合，不声明真实 Provider unknown outcome。
type ModelReceiptStage string

const (
	ModelReceiptReserved  ModelReceiptStage = "reserved"
	ModelReceiptCompleted ModelReceiptStage = "completed"
	ModelReceiptFailed    ModelReceiptStage = "failed"
)

// ModelReceiptIdentity 把模型回执写入绑定到稳定调用和当前 owner/fence。
type ModelReceiptIdentity struct {
	Owner       string
	FenceToken  int64
	SessionID   string
	InputID     string
	TurnID      string
	RunID       string
	ModelCallID string
	CallKind    ModelCallKind
}

// ModelReceiptSnapshot 是 first-write-wins 模型回执读取结果。
type ModelReceiptSnapshot struct {
	Stage     ModelReceiptStage
	Response  *schema.Message
	ErrorCode string
}

// ModelReceiptStore 由 PostgreSQL Adapter 实现稳定键、摘要冲突与 stale Fence CAS。
type ModelReceiptStore interface {
	ReplayOrReserveModel(context.Context, ModelReceiptIdentity, string) (ModelReceiptSnapshot, bool, error)
	FreezeModelCompleted(context.Context, ModelReceiptIdentity, string, *schema.Message) error
	FreezeModelFailed(context.Context, ModelReceiptIdentity, string, string) error
}

// ReceiptModel 在本地模型外增加分层、可重放的 first-write-wins 回执。
type ReceiptModel struct {
	base  model.BaseChatModel
	store ModelReceiptStore
	kind  ModelCallKind
}

// NewReceiptModel 创建 Router 或 Graph Analysis 的独立回执包装器。
func NewReceiptModel(base model.BaseChatModel, store ModelReceiptStore, kind ModelCallKind) (*ReceiptModel, error) {
	if base == nil || store == nil || (kind != ModelCallRouter && kind != ModelCallGraphAnalysis) {
		return nil, fmt.Errorf("create analyze materials receipt model: invalid dependency or call kind")
	}
	return &ReceiptModel{base: base, store: store, kind: kind}, nil
}

// Generate 先重放/占位，再至多执行一次本地模型并重读数据库首写结果。
func (m *ReceiptModel) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	trusted, ok := turncontext.MaterialAnalysisRuntimeFrom(ctx)
	if !ok || trusted.Owner == "" || trusted.FenceToken < 1 || trusted.Context.Profile != Profile {
		return nil, fmt.Errorf("run analyze materials receipt model: trusted runtime context is invalid")
	}
	identity, routeRef, routeDigest, err := m.identity(trusted)
	if err != nil {
		return nil, err
	}
	requestDigest, err := digestModelRequest(m.kind, identity.ModelCallID, trusted.Context.ContextDigest, routeRef, routeDigest, messages)
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
	if snapshot.Stage != ModelReceiptReserved || !execute {
		return nil, ErrModelReceiptReserved
	}
	response, runErr := m.base.Generate(ctx, messages, options...)
	if runErr != nil || response == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := m.store.FreezeModelFailed(ctx, identity, requestDigest, "LOCAL_FAKE_MODEL_EXECUTION_FAILED"); err != nil {
			return nil, err
		}
	} else if err := m.store.FreezeModelCompleted(ctx, identity, requestDigest, response); err != nil {
		return nil, err
	}
	snapshot, execute, err = m.store.ReplayOrReserveModel(ctx, identity, requestDigest)
	if err != nil {
		return nil, err
	}
	response, terminalErr, terminal := terminalModelReceipt(snapshot)
	if !terminal || execute {
		return nil, fmt.Errorf("run analyze materials receipt model: frozen response is unavailable")
	}
	return response, terminalErr
}

// Stream 返回一个冻结响应块，保持 classic BaseChatModel 完整接口。
func (m *ReceiptModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

// WithTools 只在 Router 回执层保留 ToolCallingChatModel 的不可变绑定能力。
func (m *ReceiptModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if m.kind != ModelCallRouter {
		return nil, fmt.Errorf("bind analyze materials receipt model: graph analysis model cannot bind tools")
	}
	base, ok := m.base.(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("bind analyze materials receipt model: router does not support tools")
	}
	bound, err := base.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &ReceiptModel{base: bound, store: m.store, kind: m.kind}, nil
}

func (m *ReceiptModel) identity(value turncontext.MaterialAnalysisRuntime) (ModelReceiptIdentity, string, string, error) {
	ctx := value.Context
	identity := ModelReceiptIdentity{Owner: value.Owner, FenceToken: value.FenceToken, SessionID: ctx.SessionID, InputID: ctx.InputID, TurnID: ctx.TurnID, RunID: ctx.RunID, CallKind: m.kind}
	switch m.kind {
	case ModelCallRouter:
		identity.ModelCallID = value.RouterModelCallID
		return identity, ctx.RouterModelRouteRef, ctx.RouterModelRouteDigest, nil
	case ModelCallGraphAnalysis:
		identity.ModelCallID = value.GraphModelCallID
		return identity, ctx.AnalysisModelRouteRef, ctx.AnalysisModelRouteDigest, nil
	default:
		return ModelReceiptIdentity{}, "", "", fmt.Errorf("run analyze materials receipt model: unknown call kind")
	}
}

func terminalModelReceipt(snapshot ModelReceiptSnapshot) (*schema.Message, error, bool) {
	switch snapshot.Stage {
	case ModelReceiptCompleted:
		if snapshot.Response == nil || snapshot.ErrorCode != "" {
			return nil, ErrReceiptConflict, true
		}
		return cloneMessage(snapshot.Response), nil, true
	case ModelReceiptFailed:
		if snapshot.Response != nil || snapshot.ErrorCode == "" {
			return nil, ErrReceiptConflict, true
		}
		return nil, fmt.Errorf("analyze materials local model failed: %s", snapshot.ErrorCode), true
	case ModelReceiptReserved:
		if snapshot.Response != nil || snapshot.ErrorCode != "" {
			return nil, ErrReceiptConflict, true
		}
		return nil, nil, false
	default:
		return nil, ErrReceiptConflict, true
	}
}

func digestModelRequest(kind ModelCallKind, callID, contextDigest, routeRef, routeDigest string, messages []*schema.Message) (string, error) {
	if len(messages) == 0 || !canonicalUUIDv7.MatchString(callID) || len(contextDigest) != 64 || routeRef == "" || len(routeDigest) != 64 {
		return "", fmt.Errorf("digest analyze materials model request: invalid identity or messages")
	}
	wire := struct {
		SchemaVersion string            `json:"schema_version"`
		CallKind      ModelCallKind     `json:"call_kind"`
		ModelCallID   string            `json:"model_call_id"`
		ContextDigest string            `json:"context_digest"`
		RouteRef      string            `json:"route_ref"`
		RouteDigest   string            `json:"route_digest"`
		Messages      []*schema.Message `json:"messages"`
	}{"analyze_materials.model_request.v2preview1", kind, callID, contextDigest, routeRef, routeDigest, messages}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("digest analyze materials model request: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func cloneMessage(message *schema.Message) *schema.Message {
	if message == nil {
		return nil
	}
	clone := *message
	clone.ToolCalls = append([]schema.ToolCall(nil), message.ToolCalls...)
	if message.Extra != nil {
		clone.Extra = make(map[string]any, len(message.Extra))
		for key, value := range message.Extra {
			clone.Extra[key] = value
		}
	}
	return &clone
}

var _ model.ToolCallingChatModel = (*ReceiptModel)(nil)
