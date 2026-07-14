package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
)

// Service 编排 Session W0 创建用例，负责规范化、摘要复核、正文保护和完整原子创建计划。
// Service 不调用 RPC、Redis、模型或 Runner；所有数据库事实由 Repository 在一个本地事务中提交。
type Service struct {
	// repository 在单个 Agent PostgreSQL 事务中提交完整 Session 基础事实。
	repository Repository
	// idGenerator 为全部新领域事实生成 UUIDv7。
	idGenerator IDGenerator
	// clock 为单次 Ensure 冻结同一 UTC 时间。
	clock Clock
	// protector 在事务前保护非空 Prompt，避免明文落库。
	protector ContentProtector
}

// NewService 创建 Session 基础用例并校验全部强依赖。
// 缺少正文保护器时即使当前请求可能为空也拒绝构造，避免未来非空输入被意外明文持久化。
func NewService(repository Repository, idGenerator IDGenerator, clock Clock, protector ContentProtector) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("create session service: repository is required")
	}
	if idGenerator == nil {
		return nil, fmt.Errorf("create session service: id generator is required")
	}
	if clock == nil {
		return nil, fmt.Errorf("create session service: clock is required")
	}
	if protector == nil {
		return nil, fmt.Errorf("create session service: content protector is required")
	}
	return &Service{repository: repository, idGenerator: idGenerator, clock: clock, protector: protector}, nil
}

// EnsureProjectSession 幂等建立默认 Session、显式空 Skill Snapshot、可选 Message/Input、Receipt 和 EventLog。
// 已完成同键重放先只读返回冻结 Receipt；未命中才保护正文和生成 ID，最终竞态仍由 Repository 事务锁判定。
// 空 Prompt 不调用正文保护器，也不创建 Message/Input。
func (s *Service) EnsureProjectSession(ctx context.Context, command EnsureCommand) (EnsureResult, error) {
	canonical, err := canonicalizeCommand(command)
	if err != nil {
		return EnsureResult{}, err
	}

	// 预检必须发生在加密、时钟和随机 ID 之前：一旦命令已经完成，短暂的 KMS/随机源故障
	// 不能阻断冻结 Receipt 重放。并发双 miss 仍由 Ensure 的命令锁保证 first-write-wins。
	preflight, err := s.repository.Query(ctx, QueryCommand{
		SchemaVersion:         QueryCommandSchemaVersionV1,
		RequestID:             canonical.requestID,
		CommandID:             canonical.commandID,
		ExpectedRequestDigest: canonical.requestDigest,
	})
	if err != nil {
		return EnsureResult{}, err
	}
	switch preflight.Status {
	case QueryCommandStatusCompleted:
		if preflight.Receipt == nil {
			return EnsureResult{}, fmt.Errorf("query completed command: missing frozen receipt")
		}
		result := *preflight.Receipt
		result.Disposition = EnsureDispositionReplayed
		return result, nil
	case QueryCommandStatusConflict:
		return EnsureResult{}, ErrCommandConflict
	case QueryCommandStatusNotFound:
		// 继续构造新 Session 计划。
	default:
		return EnsureResult{}, fmt.Errorf("query command: unsupported status %q", preflight.Status)
	}

	var protected ProtectedContent
	if canonical.promptPresent {
		protected, err = s.protector.Protect(ctx, []byte(canonical.normalizedPrompt))
		if err != nil {
			return EnsureResult{}, mapContentProtectionError(err)
		}
		// Service 执行 Envelope magic/version/algorithm/nonce/min-length 结构校验；未来加密适配器仍负责 AEAD Open 认证。
		// 双层校验禁止适配器把裸密文误当完整持久化格式，避免后续因缺少算法、Nonce 或认证标签而无法解密。
		if protected.KeyVersion == "" || ValidateEnvelopeV1(protected.Ciphertext) != nil {
			return EnsureResult{}, ErrContentProtection
		}
	}

	now := s.clock.Now().UTC()
	sessionID, err := s.newUUIDv7("session")
	if err != nil {
		return EnsureResult{}, err
	}
	plan := EnsurePlan{
		Session: Session{
			ID: sessionID, ProjectID: canonical.projectID, UserID: canonical.ownerUserID,
			Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		SkillSnapshot: SkillSnapshot{
			SessionID: sessionID, Kind: SkillSnapshotKindEmpty, Digest: EmptySkillSnapshotDigest,
			PublishedSnapshotRefsJSON: "[]", CreatedAt: now,
		},
		SequenceCounter: SequenceCounter{SessionID: sessionID, UpdatedAt: now},
		RuntimeLease:    RuntimeLease{SessionID: sessionID, FenceToken: 0, Version: 1, UpdatedAt: now},
		Receipt: CommandReceipt{
			CommandID: canonical.commandID, CommandType: CommandTypeEnsureProjectSessionV1,
			RequestDigest: canonical.requestDigest, SessionID: sessionID,
			ResultVersion: ResultVersionV1, CompletedAt: now,
		},
	}

	if canonical.promptPresent {
		messageID, idErr := s.newUUIDv7("message")
		if idErr != nil {
			return EnsureResult{}, idErr
		}
		inputID, idErr := s.newUUIDv7("input")
		if idErr != nil {
			return EnsureResult{}, idErr
		}
		plan.SequenceCounter.LastMessageSeq = 1
		plan.SequenceCounter.LastInputEnqueueSeq = 1
		plan.Message = &Message{
			ID: messageID, SessionID: sessionID, Seq: 1, Role: MessageRoleUser,
			Content: protected, ContentDigest: canonical.promptDigest,
			SourceKind: event.SourceKindEnsureProjectSession, SourceID: canonical.commandID, CreatedAt: now,
		}
		plan.Input = &Input{
			ID: inputID, SessionID: sessionID, SourceType: InputSourceTypeUserMessage,
			SourceID: canonical.commandID, MessageID: messageID, Status: InputStatusPending,
			EnqueueSeq: 1, Attempts: 0, AvailableAt: now, FenceToken: 0,
			CreatedAt: now, UpdatedAt: now,
		}
		plan.Receipt.MessageID = stringPointer(messageID)
		plan.Receipt.InputID = stringPointer(inputID)
	}

	createdEventID, err := s.newUUIDv7("session event")
	if err != nil {
		return EnsureResult{}, err
	}
	createdEvent, err := event.NewSessionCreated(
		createdEventID, sessionID, canonical.projectID, string(StatusActive), canonical.commandID, 1, now,
	)
	if err != nil {
		return EnsureResult{}, fmt.Errorf("build session.created event: %w", err)
	}
	plan.Events = []event.Record{createdEvent}
	if plan.Input != nil && plan.Message != nil {
		acceptedEventID, idErr := s.newUUIDv7("input event")
		if idErr != nil {
			return EnsureResult{}, idErr
		}
		acceptedEvent, eventErr := event.NewSessionInputAccepted(
			acceptedEventID, sessionID, plan.Input.ID, plan.Message.ID, canonical.commandID,
			string(InputStatusPending), plan.Input.EnqueueSeq, now,
		)
		if eventErr != nil {
			return EnsureResult{}, fmt.Errorf("build session.input.accepted event: %w", eventErr)
		}
		plan.Events = append(plan.Events, acceptedEvent)
	}

	result, err := s.repository.Ensure(ctx, plan)
	if err != nil {
		return EnsureResult{}, err
	}
	return result, nil
}

// QueryProjectSessionCommand 查询原 Ensure 命令的权威状态，用于 Unknown Outcome 恢复。
// 查询只读取 Receipt：同摘要返回 completed，摘要不同返回 conflict，不存在返回 not_found；任何状态都不会触发创建或重试。
func (s *Service) QueryProjectSessionCommand(ctx context.Context, command QueryCommand) (QueryCommandResult, error) {
	if command.SchemaVersion != QueryCommandSchemaVersionV1 {
		return QueryCommandResult{}, fmt.Errorf("%w: unsupported query schema_version", ErrInvalidCommand)
	}
	requestID, err := normalizeUUIDv7(command.RequestID)
	if err != nil {
		return QueryCommandResult{}, fmt.Errorf("%w: query request_id: %v", ErrInvalidCommand, err)
	}
	commandID, err := normalizeUUIDv7(command.CommandID)
	if err != nil {
		return QueryCommandResult{}, fmt.Errorf("%w: query command_id: %v", ErrInvalidCommand, err)
	}
	if !validSHA256Hex(command.ExpectedRequestDigest) {
		return QueryCommandResult{}, fmt.Errorf("%w: expected_request_digest must be lowercase SHA-256", ErrInvalidCommand)
	}
	return s.repository.Query(ctx, QueryCommand{
		SchemaVersion: command.SchemaVersion, RequestID: requestID, CommandID: commandID,
		ExpectedRequestDigest: command.ExpectedRequestDigest,
	})
}

// mapContentProtectionError 保留请求取消与 Deadline 控制语义，并截断其他加密/KMS 内部详情。
// 普通错误只能对外表现为稳定 ErrContentProtection，避免算法、密钥服务地址或 Secret 沿 RPC 错误链泄漏。
func mapContentProtectionError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrContentProtection
}

// newUUIDv7 生成并复核 UUIDv7，防止错误的 ID Generator 把不兼容标识写入领域计划。
func (s *Service) newUUIDv7(kind string) (string, error) {
	value, err := s.idGenerator.New()
	if err != nil {
		return "", fmt.Errorf("generate %s UUIDv7: %w", kind, err)
	}
	normalized, err := normalizeUUIDv7(value)
	if err != nil {
		return "", fmt.Errorf("generate %s UUIDv7: %w", kind, err)
	}
	return normalized, nil
}

// stringPointer 返回字符串副本地址，用于明确可选 Receipt 字段。
func stringPointer(value string) *string {
	copyValue := value
	return &copyValue
}
