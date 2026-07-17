package writepromptsruntime

import (
	"context"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
)

const (
	// EnqueueResponseSchemaVersion 是 HTTP 202 响应的稳定 Schema。
	EnqueueResponseSchemaVersion = "write_prompts.preview.enqueue.v1"
	// EnqueuePendingStatus 表示 typed Input 已持久化，后台尚未形成 Tool Result。
	EnqueuePendingStatus = "pending"
)

// EnqueueRequest 是 Handler 完成身份与 PromptPreview 卡片绑定校验后交给 Service 的 DTO。
type EnqueueRequest struct {
	// RequestID 是当前请求 UUIDv7。
	RequestID string
	// SessionID 是目标 Session UUIDv7。
	SessionID string
	// UserID 是认证用户 UUIDv7。
	UserID string
	// ProjectID 是已校验项目 UUIDv7。
	ProjectID string
	// IdempotencyKey 是 Session 内 first-write-wins UUIDv7。
	IdempotencyKey string
	// StoryboardPreviewRef 是 Workspace 当前权威 Card 绑定的 Draft 引用。
	StoryboardPreviewRef writeprompts.StoryboardPreviewRef
	// IntentJSON 是严格 Tool Intent 原始 JSON。
	IntentJSON []byte
	// AccessScopeRef 是内部认证范围引用。
	AccessScopeRef string
	// AccessScopeDigest 是内部认证范围摘要。
	AccessScopeDigest string
	// IntentKeyVersion 是 Intent 密文密钥版本。
	IntentKeyVersion string
}

// EnqueueResponse 是 Business BFF 可安全转发的 202 DTO，不含 Intent、PromptPreview 摘要或内部 pins。
type EnqueueResponse struct {
	// SchemaVersion 是响应 DTO 版本。
	SchemaVersion string `json:"schema_version"`
	// Status 本批固定为 pending。
	Status string `json:"status"`
	// InputID 是 typed Input UUIDv7。
	InputID string `json:"input_id"`
	// TurnID 是稳定 Turn UUIDv7。
	TurnID string `json:"turn_id"`
	// RunID 是稳定 Run UUIDv7。
	RunID string `json:"run_id"`
	// ToolCallID 是唯一 write_prompts ToolCall UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// Replayed 表示同义幂等重放。
	Replayed bool `json:"replayed"`
}

// EnqueueStore 是 Service 所需的最小事务端口。
type EnqueueStore interface {
	// Enqueue 在单事务内创建或同义重放 typed Input 与全部初始事实。
	Enqueue(context.Context, EnqueueCommand, time.Time) (EnqueueResult, error)
}

// Clock 为入队与 Processor 状态迁移注入可测试 UTC 时间。
type Clock interface {
	// Now 返回当前时间，调用方统一转换为 UTC。
	Now() time.Time
}

// Service 负责 strict Intent、可信 StoryboardPreviewRef、Access Scope pin 与成功入队唤醒。
type Service struct {
	store EnqueueStore
	clock Clock
	wake  func()
}

// NewService 创建 Prompt typed Intent 入队 Service。
func NewService(store EnqueueStore, clock Clock, wake func()) (*Service, error) {
	if store == nil || clock == nil || wake == nil {
		return nil, fmt.Errorf("create write prompts runtime service: invalid dependency")
	}
	return &Service{store: store, clock: clock, wake: wake}, nil
}

// Enqueue 验证 exact-set 后调用单事务 Store，并在提交成功后触发可丢失 wake。
func (s *Service) Enqueue(ctx context.Context, request EnqueueRequest) (EnqueueResponse, error) {
	if !canonicalUUIDv7.MatchString(request.RequestID) || !canonicalUUIDv7.MatchString(request.SessionID) ||
		!canonicalUUIDv7.MatchString(request.UserID) || !canonicalUUIDv7.MatchString(request.ProjectID) ||
		!canonicalUUIDv7.MatchString(request.IdempotencyKey) || !canonicalUUIDv7.MatchString(request.StoryboardPreviewRef.ID) ||
		request.StoryboardPreviewRef.Version != 1 || !canonicalSHA256.MatchString(request.StoryboardPreviewRef.ContentDigest) ||
		request.AccessScopeRef == "" || !canonicalSHA256.MatchString(request.AccessScopeDigest) || request.IntentKeyVersion == "" {
		return EnqueueResponse{}, fmt.Errorf("%w: trusted request", ErrInvalidInput)
	}
	intent, err := DecodeIntent(request.IntentJSON)
	if err != nil {
		return EnqueueResponse{}, fmt.Errorf("%w: strict Intent", ErrInvalidInput)
	}
	result, err := s.store.Enqueue(ctx, EnqueueCommand{
		RequestID: request.RequestID, SessionID: request.SessionID, UserID: request.UserID, ProjectID: request.ProjectID,
		IdempotencyKey: request.IdempotencyKey, StoryboardPreviewRef: request.StoryboardPreviewRef, IntentJSON: intent.JSON,
		AccessScopeRef: request.AccessScopeRef, AccessScopeDigest: request.AccessScopeDigest,
		IntentKeyVersion: request.IntentKeyVersion,
	}, s.clock.Now().UTC())
	if err != nil {
		return EnqueueResponse{}, err
	}
	for _, identity := range []string{
		result.InputID, result.TurnID, result.RunID, result.ToolCallID, result.BusinessCommandID,
		result.RouterModelCallID, result.GraphModelCallID, result.AcceptedEventID, result.TerminalEventID,
	} {
		if !canonicalUUIDv7.MatchString(identity) {
			return EnqueueResponse{}, ErrPersistence
		}
	}
	s.wake()
	return EnqueueResponse{
		SchemaVersion: EnqueueResponseSchemaVersion, Status: EnqueuePendingStatus,
		InputID: result.InputID, TurnID: result.TurnID, RunID: result.RunID,
		ToolCallID: result.ToolCallID, Replayed: result.Replayed,
	}, nil
}
