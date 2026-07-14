package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// SessionRepository 使用 GORM 在 Agent PostgreSQL 中原子持久化 W0 Session 基础事实。
// 同一 CommandID 通过事务级 Advisory Lock 串行化；不同 Command 仍由 Project 唯一约束防止双 Session。
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository 从 Agent PostgreSQL Client 创建 Session Repository。
// 该构造函数不执行 Migration 或 AutoMigrate；Schema 必须由版本化 SQL 预先建立。
func NewSessionRepository(client *Client) (*SessionRepository, error) {
	if client == nil || client.db == nil {
		return nil, fmt.Errorf("create session repository: postgres client is required")
	}
	return &SessionRepository{db: client.db}, nil
}

// Ensure 在一个短事务内完成回执判定、Session、空 Skill Snapshot、可选 Message/Input、Receipt 与 EventLog。
// 事务不调用 RPC、Redis、模型或 Runner；失败时全部回滚，调用方只能在提交成功后发送非权威唤醒。
func (r *SessionRepository) Ensure(ctx context.Context, plan session.EnsurePlan) (session.EnsureResult, error) {
	if err := validateEnsurePlan(plan); err != nil {
		return session.EnsureResult{}, err
	}
	var result session.EnsureResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Receipt 尚不存在时无法行锁；按 CommandID 获取事务级锁可让同键并发先后观察 first-write-wins 结果。
		// Advisory Hash 碰撞只会额外串行化，不会破坏正确性；Project 唯一约束覆盖不同 Command 的竞争。
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", plan.Receipt.CommandID).Error; err != nil {
			return fmt.Errorf("lock session command: %w", err)
		}

		var existingReceipt sessionCommandReceiptModel
		receiptErr := tx.Where("command_id = ?", plan.Receipt.CommandID).Take(&existingReceipt).Error
		switch {
		case receiptErr == nil:
			// 同键只能重放完全相同的语义；Digest 不同必须失败关闭，绝不能覆盖旧 Session/Input。
			if existingReceipt.RequestDigest != plan.Receipt.RequestDigest {
				return session.ErrCommandConflict
			}
			result = mapReceiptResult(existingReceipt, session.EnsureDispositionReplayed)
			return nil
		case !errors.Is(receiptErr, gorm.ErrRecordNotFound):
			return fmt.Errorf("read session command receipt: %w", receiptErr)
		}

		var existingSession sessionModel
		projectErr := tx.Select("id").Where("project_id = ?", plan.Session.ProjectID).Take(&existingSession).Error
		switch {
		case projectErr == nil:
			// 不同 CommandID 竞争同一 Project 时无法证明语义相同，拒绝隐式复用并交由 Business 查询原命令处置。
			return session.ErrProjectSessionConflict
		case !errors.Is(projectErr, gorm.ErrRecordNotFound):
			return fmt.Errorf("read project default session: %w", projectErr)
		}

		if err := tx.Create(modelPointer(mapSessionModel(plan.Session))).Error; err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		if err := tx.Create(modelPointer(mapSessionSkillSnapshotModel(plan.SkillSnapshot))).Error; err != nil {
			return fmt.Errorf("create session skill snapshot: %w", err)
		}
		if err := tx.Create(modelPointer(mapSessionSequenceCounterModel(plan.SequenceCounter))).Error; err != nil {
			return fmt.Errorf("create session sequence counter: %w", err)
		}
		if err := tx.Create(modelPointer(mapSessionRuntimeLeaseModel(plan.RuntimeLease))).Error; err != nil {
			return fmt.Errorf("create session runtime lease: %w", err)
		}

		if plan.Message != nil && plan.Input != nil {
			if err := tx.Create(modelPointer(mapSessionMessageModel(*plan.Message))).Error; err != nil {
				return fmt.Errorf("create initial session message: %w", err)
			}
			if err := tx.Create(modelPointer(mapSessionInputModel(*plan.Input))).Error; err != nil {
				return fmt.Errorf("create initial session input: %w", err)
			}
		}

		// 创建新 Session 时 Event Counter 也首次建立，因此可在内存连续分配并一次批量 INSERT；
		// 后续追加必须锁定该 Counter 行、批量分配范围并检查 RowsAffected，不能逐事件执行同构 SQL。
		eventModels := mapSessionEventLogModels(plan.Events)
		eventCounter := sessionEventCounterModel{
			SessionID: plan.Session.ID, LastSeq: int64(len(eventModels)), MinAvailableSeq: 1,
			UpdatedAt: plan.Session.CreatedAt,
		}
		if err := tx.Create(&eventCounter).Error; err != nil {
			return fmt.Errorf("create session event counter: %w", err)
		}
		if err := tx.Create(&eventModels).Error; err != nil {
			return fmt.Errorf("create session event log: %w", err)
		}

		receiptModel := mapSessionCommandReceiptModel(plan.Receipt)
		if err := tx.Create(&receiptModel).Error; err != nil {
			return fmt.Errorf("create session command receipt: %w", err)
		}
		result = mapReceiptResult(receiptModel, session.EnsureDispositionCreated)
		return nil
	})
	if err != nil {
		return session.EnsureResult{}, mapSessionRepositoryError(err)
	}
	return result, nil
}

// Query 只读取原命令 Receipt 并在 Repository 内比较摘要，避免把已冻结摘要或其他命令回执扩散到协议层。
// Query 不获取写锁、不创建事实，也不执行框架或数据库层自动重试。
func (r *SessionRepository) Query(ctx context.Context, command session.QueryCommand) (session.QueryCommandResult, error) {
	var receipt sessionCommandReceiptModel
	err := r.db.WithContext(ctx).Where("command_id = ?", command.CommandID).Take(&receipt).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return session.QueryCommandResult{Status: session.QueryCommandStatusNotFound}, nil
	case err != nil:
		return session.QueryCommandResult{}, mapSessionRepositoryError(err)
	case !sameRepositoryDigest(receipt.RequestDigest, command.ExpectedRequestDigest):
		// Conflict 不返回已存在 Receipt，避免查询方借猜测 CommandID 读取另一语义的结果引用。
		return session.QueryCommandResult{Status: session.QueryCommandStatusConflict}, nil
	default:
		result := mapReceiptResult(receipt, session.EnsureDispositionReplayed)
		return session.QueryCommandResult{Status: session.QueryCommandStatusCompleted, Receipt: &result}, nil
	}
}

// sameRepositoryDigest 比较已完成 Receipt 与调用方预期摘要；两个值都由领域/Schema 约束为固定长度小写十六进制。
func sameRepositoryDigest(left, right string) bool {
	return left == right
}

// validateEnsurePlan 对 Service→Repository 内部边界执行失败关闭校验，避免未来调用方绕过 W0 不变量。
func validateEnsurePlan(plan session.EnsurePlan) error {
	if plan.Session.ID == "" || plan.Session.ProjectID == "" || plan.Session.UserID == "" {
		return fmt.Errorf("%w: session identity is incomplete", session.ErrInvalidCommand)
	}
	if plan.SkillSnapshot.SessionID != plan.Session.ID || plan.SkillSnapshot.Kind != session.SkillSnapshotKindEmpty ||
		plan.SkillSnapshot.Digest != session.EmptySkillSnapshotDigest || plan.SkillSnapshot.PublishedSnapshotRefsJSON != "[]" {
		return fmt.Errorf("%w: explicit empty skill snapshot is invalid", session.ErrInvalidCommand)
	}
	if plan.SequenceCounter.SessionID != plan.Session.ID || plan.RuntimeLease.SessionID != plan.Session.ID {
		return fmt.Errorf("%w: session counter or lease identity mismatch", session.ErrInvalidCommand)
	}
	if (plan.Message == nil) != (plan.Input == nil) {
		return fmt.Errorf("%w: message and input must both exist or both be absent", session.ErrInvalidCommand)
	}
	expectedEventCount := 1
	if plan.Message != nil {
		expectedEventCount = 2
	}
	// 先校验长度再访问固定投影位置，避免任何绕过 Service 的畸形计划触发 Repository Panic。
	if len(plan.Events) != expectedEventCount {
		return fmt.Errorf("%w: event projection count is inconsistent", session.ErrInvalidCommand)
	}
	if plan.Message == nil {
		if plan.SequenceCounter.LastMessageSeq != 0 || plan.SequenceCounter.LastInputEnqueueSeq != 0 ||
			plan.Receipt.MessageID != nil || plan.Receipt.InputID != nil {
			return fmt.Errorf("%w: blank prompt plan contains input side effects", session.ErrInvalidCommand)
		}
	} else {
		if plan.Message.SessionID != plan.Session.ID || plan.Input.SessionID != plan.Session.ID ||
			plan.Message.SourceID != plan.Receipt.CommandID || plan.Input.SourceID != plan.Receipt.CommandID ||
			plan.Input.MessageID != plan.Message.ID ||
			plan.Message.Role != session.MessageRoleUser || plan.Input.SourceType != session.InputSourceTypeUserMessage ||
			plan.Message.Seq != 1 || plan.Input.EnqueueSeq != 1 || plan.Input.Status != session.InputStatusPending ||
			plan.SequenceCounter.LastMessageSeq != 1 || plan.SequenceCounter.LastInputEnqueueSeq != 1 ||
			plan.Receipt.MessageID == nil || *plan.Receipt.MessageID != plan.Message.ID ||
			plan.Receipt.InputID == nil || *plan.Receipt.InputID != plan.Input.ID {
			return fmt.Errorf("%w: initial input plan is inconsistent", session.ErrInvalidCommand)
		}
		// Repository 在事务前再次校验自描述 Envelope，防止未来其他 Service 绕过 Session Service 写入裸密文。
		if plan.Message.Content.KeyVersion == "" || session.ValidateEnvelopeV1(plan.Message.Content.Ciphertext) != nil {
			return fmt.Errorf("%w: initial message envelope is invalid", session.ErrInvalidCommand)
		}
	}
	if plan.Receipt.SessionID != plan.Session.ID || plan.Receipt.CommandType != session.CommandTypeEnsureProjectSessionV1 ||
		plan.Receipt.ResultVersion != session.ResultVersionV1 {
		return fmt.Errorf("%w: command receipt is inconsistent", session.ErrInvalidCommand)
	}
	if plan.Events[0].Type != event.TypeSessionCreated || plan.Events[0].ProjectionIndex != 0 ||
		plan.Events[0].SessionID != plan.Session.ID || plan.Events[0].SourceID != plan.Receipt.CommandID ||
		plan.Events[0].AggregateID != plan.Session.ID {
		return fmt.Errorf("%w: session.created event is missing or out of order", session.ErrInvalidCommand)
	}
	if len(plan.Events) == 2 {
		if plan.Events[1].Type != event.TypeSessionInputAccepted || plan.Events[1].ProjectionIndex != 1 ||
			plan.Events[1].SessionID != plan.Session.ID || plan.Events[1].SourceID != plan.Receipt.CommandID ||
			plan.Events[1].AggregateID != plan.Input.ID {
			return fmt.Errorf("%w: session.input.accepted event is missing or out of order", session.ErrInvalidCommand)
		}
	}
	return nil
}

// mapSessionRepositoryError 将 PostgreSQL/GORM 错误收敛为稳定领域错误，避免协议层依赖约束或 SQL 原文。
func mapSessionRepositoryError(err error) error {
	// 请求取消和 Deadline 是传输层决定重试、499/超时映射与资源回收的控制信号，
	// 必须在稳定数据库错误收敛前透传，不能伪装成不可区分的持久化故障。
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, session.ErrCommandConflict) || errors.Is(err, session.ErrProjectSessionConflict) ||
		errors.Is(err, session.ErrInvalidCommand) {
		return err
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		switch pgError.ConstraintName {
		case "uq_session__project_id":
			return session.ErrProjectSessionConflict
		case "pk_session_command_receipt":
			return session.ErrCommandConflict
		}
	}
	// 原始 PostgreSQL/GORM 错误可能包含 SQL、表结构或输入片段；领域边界只返回稳定错误，
	// 具体诊断必须由受控基础设施日志在脱敏后记录，不能沿 RPC/HTTP 错误链外泄。
	return session.ErrPersistence
}

// modelPointer 返回 Mapper 值的独立地址，避免在事务代码中暴露可变临时变量。
func modelPointer[T any](value T) *T { return &value }

var _ session.Repository = (*SessionRepository)(nil)
