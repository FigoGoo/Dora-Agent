package analyzematerialsruntime

import (
	"context"
	"fmt"
	"time"
)

const (
	// EnqueueResponseSchemaVersion 是 HTTP 202 响应的稳定 Schema。
	EnqueueResponseSchemaVersion = "analyze_materials.preview.enqueue.v1"
	// EnqueuePendingStatus 表示 typed Input 已持久化，后台尚未形成 Tool Result。
	EnqueuePendingStatus = "pending"
)

// EnqueueRequest 是 Handler 完成身份校验后交给 Service 的协议无关 DTO。
type EnqueueRequest struct {
	RequestID         string
	SessionID         string
	UserID            string
	ProjectID         string
	IdempotencyKey    string
	IntentJSON        []byte
	AccessScopeRef    string
	AccessScopeDigest string
	IntentKeyVersion  string
}

// EnqueueResponse 是 Business BFF 可安全转发的 202 DTO，不含 Intent 或内部 pins。
type EnqueueResponse struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
	InputID       string `json:"input_id"`
	TurnID        string `json:"turn_id"`
	RunID         string `json:"run_id"`
	ToolCallID    string `json:"tool_call_id"`
	Replayed      bool   `json:"replayed"`
}

// EnqueueStore 是 Service 所需的最小事务端口。
type EnqueueStore interface {
	Enqueue(context.Context, EnqueueCommand, time.Time) (EnqueueResult, error)
}

// Service 负责 strict Intent 规范化、可信 Access Scope pin 和成功入队唤醒。
type Service struct {
	store EnqueueStore
	clock Clock
	wake  func()
}

// NewService 创建素材分析 typed Intent 入队 Service。
func NewService(store EnqueueStore, clock Clock, wake func()) (*Service, error) {
	if store == nil || clock == nil || wake == nil {
		return nil, fmt.Errorf("create analyze materials runtime service: invalid dependency")
	}
	return &Service{store: store, clock: clock, wake: wake}, nil
}

// Enqueue 验证 exact-set 后调用单事务 Store，并在提交成功后触发可丢失 wake。
func (s *Service) Enqueue(ctx context.Context, request EnqueueRequest) (EnqueueResponse, error) {
	if !canonicalUUIDv7.MatchString(request.RequestID) || !canonicalUUIDv7.MatchString(request.SessionID) || !canonicalUUIDv7.MatchString(request.UserID) || !canonicalUUIDv7.MatchString(request.ProjectID) || !canonicalUUIDv7.MatchString(request.IdempotencyKey) || request.AccessScopeRef == "" || len(request.AccessScopeDigest) != 64 || request.IntentKeyVersion == "" {
		return EnqueueResponse{}, fmt.Errorf("%w: trusted request", ErrInvalidInput)
	}
	intent, err := DecodeIntent(request.IntentJSON)
	if err != nil {
		return EnqueueResponse{}, fmt.Errorf("%w: strict Intent", ErrInvalidInput)
	}
	result, err := s.store.Enqueue(ctx, EnqueueCommand{RequestID: request.RequestID, SessionID: request.SessionID, UserID: request.UserID, ProjectID: request.ProjectID, IdempotencyKey: request.IdempotencyKey, IntentJSON: intent.JSON, AccessScopeRef: request.AccessScopeRef, AccessScopeDigest: request.AccessScopeDigest, IntentKeyVersion: request.IntentKeyVersion}, s.clock.Now().UTC())
	if err != nil {
		return EnqueueResponse{}, err
	}
	if !canonicalUUIDv7.MatchString(result.InputID) || !canonicalUUIDv7.MatchString(result.TurnID) || !canonicalUUIDv7.MatchString(result.RunID) || !canonicalUUIDv7.MatchString(result.ToolCallID) {
		return EnqueueResponse{}, ErrPersistence
	}
	s.wake()
	return EnqueueResponse{SchemaVersion: EnqueueResponseSchemaVersion, Status: EnqueuePendingStatus, InputID: result.InputID, TurnID: result.TurnID, RunID: result.RunID, ToolCallID: result.ToolCallID, Replayed: result.Replayed}, nil
}
