package chatmodel

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

const modelRequestSchemaVersion = "creation_spec.preview.model-request.v1"

// ReceiptStore 冻结单个 Tool Call 内每次模型调用的请求摘要与首个完成响应。
type ReceiptStore interface {
	ReplayOrReserveModel(
		ctx context.Context,
		identity ReceiptIdentity,
		callIndex int,
		requestDigest string,
	) (response *schema.Message, execute bool, err error)
	FreezeModel(
		ctx context.Context,
		identity ReceiptIdentity,
		callIndex int,
		requestDigest string,
		response *schema.Message,
	) error
}

// ReceiptIdentity 是模型回执写点显式携带的当前 Session Lane owner/fence 绑定。
type ReceiptIdentity struct {
	Owner      string
	SessionID  string
	InputID    string
	ToolCallID string
	FenceToken int64
}

// ReceiptCallKind 为 V1 唯一主 Agent 的稳定调用位置分配固定 call_index。
type ReceiptCallKind string

const (
	ReceiptCallRouter   ReceiptCallKind = "router"
	ReceiptCallProposal ReceiptCallKind = "proposal"
)

// ReceiptModel 在真实 Eino ChatModel 组件外增加 first-write-wins 密文回执，不改变 Graph/ADK 编排。
type ReceiptModel struct {
	base  model.BaseChatModel
	store ReceiptStore
	kind  ReceiptCallKind
}

// NewReceiptModel 创建 Router 或 Proposal 模型回执包装器。
func NewReceiptModel(base model.BaseChatModel, store ReceiptStore, kind ReceiptCallKind) (*ReceiptModel, error) {
	if base == nil || store == nil || (kind != ReceiptCallRouter && kind != ReceiptCallProposal) {
		return nil, fmt.Errorf("create preview receipt model: invalid dependency or call kind")
	}
	return &ReceiptModel{base: base, store: store, kind: kind}, nil
}

// Generate 先重放/占位，再调用底层模型并冻结首个完成响应。
func (m *ReceiptModel) Generate(
	ctx context.Context,
	messages []*schema.Message,
	options ...model.Option,
) (*schema.Message, error) {
	trusted, ok := turncontext.PreviewFrom(ctx)
	if !ok || trusted.ToolCallID == "" {
		return nil, fmt.Errorf("run preview receipt model: trusted turn context is missing")
	}
	callIndex, err := m.callIndex(messages)
	if err != nil {
		return nil, err
	}
	requestDigest, err := modelRequestDigest(trusted.ToolCallID, callIndex, messages)
	if err != nil {
		return nil, err
	}
	identity := ReceiptIdentity{
		Owner: trusted.Owner, SessionID: trusted.SessionID, InputID: trusted.InputID,
		ToolCallID: trusted.ToolCallID, FenceToken: trusted.FenceToken,
	}
	replayed, execute, err := m.store.ReplayOrReserveModel(ctx, identity, callIndex, requestDigest)
	if err != nil {
		return nil, err
	}
	if !execute {
		if replayed == nil {
			return nil, fmt.Errorf("run preview receipt model: completed response is missing")
		}
		return replayed, nil
	}
	response, err := m.base.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("run preview receipt model: model returned nil response")
	}
	if err := m.store.FreezeModel(ctx, identity, callIndex, requestDigest, response); err != nil {
		return nil, err
	}
	// 再读一次确保并发竞争时只向下游返回数据库 first-write-wins 的响应。
	frozen, executeAgain, err := m.store.ReplayOrReserveModel(ctx, identity, callIndex, requestDigest)
	if err != nil || executeAgain || frozen == nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("run preview receipt model: frozen response is unavailable")
	}
	return frozen, nil
}

// Stream 返回单块冻结响应，仍要求上层完整消费 StreamReader。
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

// WithTools 保留 ToolCallingChatModel 能力，并让 ADK 绑定后的不可变模型继续经过同一回执层。
func (m *ReceiptModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	toolCalling, ok := m.base.(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("bind preview receipt model: base model does not support tools")
	}
	bound, err := toolCalling.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &ReceiptModel{base: bound, store: m.store, kind: m.kind}, nil
}

func (m *ReceiptModel) callIndex(messages []*schema.Message) (int, error) {
	if len(messages) == 0 {
		return 0, fmt.Errorf("run preview receipt model: messages are empty")
	}
	if m.kind == ReceiptCallProposal {
		return 2, nil
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index] == nil {
			continue
		}
		if messages[index].Role == schema.Tool {
			return 3, nil
		}
		return 1, nil
	}
	return 0, fmt.Errorf("run preview receipt model: messages contain only nil values")
}

type modelRequestWire struct {
	SchemaVersion string            `json:"schema_version"`
	ToolCallID    string            `json:"tool_call_id"`
	CallIndex     int               `json:"call_index"`
	Messages      []*schema.Message `json:"messages"`
}

func modelRequestDigest(toolCallID string, callIndex int, messages []*schema.Message) (string, error) {
	encoded, err := json.Marshal(modelRequestWire{
		SchemaVersion: modelRequestSchemaVersion, ToolCallID: toolCallID, CallIndex: callIndex, Messages: messages,
	})
	if err != nil {
		return "", fmt.Errorf("compute preview model request digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

var _ model.ToolCallingChatModel = (*ReceiptModel)(nil)
