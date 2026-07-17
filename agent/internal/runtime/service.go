package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/google/uuid"
)

// Service 只负责严格入队计划：验证、加密、生成稳定 ID、短事务持久化和非权威唤醒。
type Service struct {
	repository EnqueueRepository
	protector  ContentProtector
	ids        IDGenerator
	clock      Clock
	wake       func()
}

// NewService 创建 Preview 入队用例；wake 允许 nil，PostgreSQL 轮询仍可恢复丢失通知。
func NewService(repository EnqueueRepository, protector ContentProtector, ids IDGenerator, clock Clock, wake func()) (*Service, error) {
	if repository == nil || protector == nil || ids == nil || clock == nil {
		return nil, fmt.Errorf("create preview runtime service: dependency is nil")
	}
	return &Service{repository: repository, protector: protector, ids: ids, clock: clock, wake: wake}, nil
}

// Enqueue 只持久化 pending 输入并返回；不得在 HTTP 请求中等待 Runner、模型、Graph 或 Business RPC。
func (s *Service) Enqueue(ctx context.Context, command EnqueueCommand) (EnqueueResult, error) {
	if !canonicalUUIDv7(command.RequestID) || !canonicalUUIDv7(command.IdempotencyKey) ||
		!canonicalUUIDv7(command.UserID) || !canonicalUUIDv7(command.ProjectID) || !canonicalUUIDv7(command.SessionID) {
		return EnqueueResult{}, ErrInvalidInput
	}
	if err := plancreationspec.ValidateIntent(command.Intent); err != nil {
		return EnqueueResult{}, ErrInvalidInput
	}
	intentJSON, err := json.Marshal(command.Intent)
	if err != nil {
		return EnqueueResult{}, ErrInvalidInput
	}
	requestDigest, err := plancreationspec.IntentDigest(command.Intent)
	if err != nil {
		return EnqueueResult{}, ErrInvalidInput
	}
	preflight, err := s.repository.LookupEnqueue(
		ctx, command.IdempotencyKey, requestDigest, command.UserID, command.ProjectID, command.SessionID,
	)
	if err != nil {
		return EnqueueResult{}, err
	}
	if preflight != nil {
		result := *preflight
		result.RequestID = command.RequestID
		if s.wake != nil {
			s.wake()
		}
		return result, nil
	}
	protected, err := s.protector.Protect(ctx, intentJSON)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("protect preview intent: %w", err)
	}
	ids := make([]string, 8)
	for index := range ids {
		ids[index], err = s.ids.New()
		if err != nil || !canonicalUUIDv7(ids[index]) {
			return EnqueueResult{}, ErrPersistence
		}
	}
	now := s.clock.Now().UTC()
	if now.IsZero() {
		return EnqueueResult{}, ErrPersistence
	}
	result, err := s.repository.Enqueue(ctx, EnqueuePlan{
		RequestID: command.RequestID, IdempotencyKey: command.IdempotencyKey, RequestDigest: requestDigest,
		UserID: command.UserID, ProjectID: command.ProjectID, SessionID: command.SessionID,
		MessageID: ids[0], InputID: ids[1], TurnID: ids[2], RunID: ids[3], ToolCallID: ids[4],
		BusinessCommandID: ids[5], EventID: ids[6], TerminalEventID: ids[7], PromptVersion: plancreationspec.PromptVersion,
		ValidatorVersion: plancreationspec.ValidatorVersion, Content: protected, CreatedAt: now,
	})
	if err != nil {
		return EnqueueResult{}, err
	}
	result.RequestID = command.RequestID
	if s.wake != nil {
		// 唤醒是 best-effort；丢失或进程重启由 PostgreSQL 周期 Claim 恢复，不能让 Redis/内存通知改变 202 事实。
		s.wake()
	}
	return result, nil
}

// canonicalUUIDv7 只接受唯一小写 RFC 9562 UUIDv7 表示。
func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}
