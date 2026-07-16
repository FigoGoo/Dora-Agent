package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"gorm.io/gorm"
)

// WorkspaceRepository 使用 GORM 执行固定三查询 Snapshot 与 PostgreSQL EventLog 有界补读。
// Repository 只返回内部加密记录，明文解密和前端事件投影由 Workspace Service 负责。
type WorkspaceRepository struct {
	db *gorm.DB
}

// NewWorkspaceRepository 从 Agent PostgreSQL Client 创建只读 Workspace Repository。
// 构造过程不执行 Migration 或 AutoMigrate，所有表必须由 Agent Migration 预先建立。
func NewWorkspaceRepository(client *Client) (*WorkspaceRepository, error) {
	if client == nil || client.db == nil {
		return nil, fmt.Errorf("create Workspace repository: postgres client is required")
	}
	return &WorkspaceRepository{db: client.db}, nil
}

// workspaceSessionRow 承接 Snapshot 第一条 JOIN 查询，不扩散 GORM Model 或 Skill 内部字段。
type workspaceSessionRow struct {
	ID              string       `gorm:"column:id"`
	ProjectID       string       `gorm:"column:project_id"`
	UserID          string       `gorm:"column:user_id"`
	Status          string       `gorm:"column:status"`
	Version         int64        `gorm:"column:version"`
	CreatedAt       sql.NullTime `gorm:"column:created_at"`
	UpdatedAt       sql.NullTime `gorm:"column:updated_at"`
	LastSeq         int64        `gorm:"column:last_seq"`
	MinAvailableSeq int64        `gorm:"column:min_available_seq"`
}

// LoadSnapshot 在短 READ ONLY, REPEATABLE READ 事务中固定执行 Session JOIN、Message 集合、Input 集合三次查询。
// 每个集合用 limit+1 检测超界；超界返回稳定错误而不是截断完整 Snapshot。
func (r *WorkspaceRepository) LoadSnapshot(
	ctx context.Context,
	identity workspace.Identity,
	limits workspace.SnapshotLimits,
) (workspace.SnapshotRecord, error) {
	if identity.SessionID == "" || identity.UserID == "" || limits.MaxMessages <= 0 || limits.MaxInputs <= 0 {
		return workspace.SnapshotRecord{}, workspace.ErrNotFound
	}
	var result workspace.SnapshotRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sessionRow workspaceSessionRow
		query := tx.Raw(`
			SELECT session_record.id, session_record.project_id, session_record.user_id,
			       session_record.status, session_record.version, session_record.created_at,
			       session_record.updated_at, event_counter.last_seq, event_counter.min_available_seq
			FROM agent.session AS session_record
			JOIN agent.session_skill_snapshot AS skill_snapshot
			  ON skill_snapshot.session_id = session_record.id
			JOIN agent.session_event_counter AS event_counter
			  ON event_counter.session_id = session_record.id
			WHERE session_record.id = ? AND session_record.user_id = ?
			LIMIT 1`, identity.SessionID, identity.UserID).Scan(&sessionRow)
		if query.Error != nil {
			return fmt.Errorf("load Workspace session projection: %w", query.Error)
		}
		if query.RowsAffected != 1 {
			return workspace.ErrNotFound
		}

		var messageModels []sessionMessageModel
		if err := tx.Where("session_id = ?", identity.SessionID).
			Order("message_seq ASC, id ASC").Limit(limits.MaxMessages + 1).Find(&messageModels).Error; err != nil {
			return fmt.Errorf("load Workspace messages: %w", err)
		}
		if len(messageModels) > limits.MaxMessages {
			return workspace.ErrSnapshotTooLarge
		}

		var inputModels []sessionInputModel
		if err := tx.Where("session_id = ?", identity.SessionID).
			Order("enqueue_seq ASC, id ASC").Limit(limits.MaxInputs + 1).Find(&inputModels).Error; err != nil {
			return fmt.Errorf("load Workspace inputs: %w", err)
		}
		if len(inputModels) > limits.MaxInputs {
			return workspace.ErrSnapshotTooLarge
		}

		messages := make([]workspace.MessageRecord, len(messageModels))
		for index, model := range messageModels {
			messages[index] = workspace.MessageRecord{
				ID: model.ID, SessionID: model.SessionID, Seq: model.MessageSeq, Role: model.Role,
				Content: session.ProtectedContent{
					Ciphertext: append([]byte(nil), model.ContentCiphertext...), KeyVersion: model.ContentKeyVersion,
				},
				ContentDigest: model.ContentDigest, CreatedAt: model.CreatedAt,
			}
		}
		inputs := make([]workspace.InputRecord, len(inputModels))
		for index, model := range inputModels {
			inputs[index] = workspace.InputRecord{
				ID: model.ID, SessionID: model.SessionID, MessageID: cloneStringPointer(model.MessageID),
				SourceType: model.SourceType, Status: model.Status, EnqueueSeq: model.EnqueueSeq,
				AvailableAt: model.AvailableAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
			}
		}
		result = workspace.SnapshotRecord{
			Session: workspace.SessionRecord{
				ID: sessionRow.ID, ProjectID: sessionRow.ProjectID, UserID: sessionRow.UserID,
				Status: sessionRow.Status, Version: sessionRow.Version,
				CreatedAt: sessionRow.CreatedAt.Time, UpdatedAt: sessionRow.UpdatedAt.Time,
			},
			Messages: messages, Inputs: inputs, EventHighWatermark: sessionRow.LastSeq,
			MinAvailableSeq: sessionRow.MinAvailableSeq,
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return workspace.SnapshotRecord{}, mapWorkspaceRepositoryError(err)
	}
	return result, nil
}

// eventBoundsRow 承接 Event 补读时 User/Project/Session 三重绑定与水位查询。
type eventBoundsRow struct {
	LastSeq         int64 `gorm:"column:last_seq"`
	MinAvailableSeq int64 `gorm:"column:min_available_seq"`
}

// LoadEventBatch 在一次只读一致性事务内先校验三重绑定和水位，再执行固定 seq>cursor 的有界升序查询。
// PostgreSQL 始终是真源；调用方周期执行本方法即可补偿任何非权威通知丢失。
func (r *WorkspaceRepository) LoadEventBatch(
	ctx context.Context,
	identity workspace.Identity,
	cursor int64,
	limit int,
) (workspace.EventBatchRecord, error) {
	if identity.SessionID == "" || identity.UserID == "" || identity.ProjectID == "" || cursor < 0 || limit <= 0 {
		return workspace.EventBatchRecord{}, workspace.ErrInvalidCursor
	}
	var result workspace.EventBatchRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var bounds eventBoundsRow
		query := tx.Raw(`
			SELECT event_counter.last_seq, event_counter.min_available_seq
			FROM agent.session AS session_record
			JOIN agent.session_event_counter AS event_counter
			  ON event_counter.session_id = session_record.id
			WHERE session_record.id = ?
			  AND session_record.user_id = ?
			  AND session_record.project_id = ?
			LIMIT 1`, identity.SessionID, identity.UserID, identity.ProjectID).Scan(&bounds)
		if query.Error != nil {
			return fmt.Errorf("load Workspace event bounds: %w", query.Error)
		}
		if query.RowsAffected != 1 {
			return workspace.ErrNotFound
		}

		var models []sessionEventLogModel
		if err := tx.Where("session_id = ? AND seq > ?", identity.SessionID, cursor).
			Order("seq ASC").Limit(limit).Find(&models).Error; err != nil {
			return fmt.Errorf("load Workspace event batch: %w", err)
		}
		events := make([]workspace.EventRecord, len(models))
		for index, model := range models {
			events[index] = workspace.EventRecord{
				EventID: model.EventID, SessionID: model.SessionID, Seq: model.Seq,
				EventType: model.EventType, SchemaVersion: model.SchemaVersion,
				AggregateType: model.AggregateType, AggregateID: model.AggregateID,
				AggregateVersion: model.AggregateVersion, Payload: []byte(model.Payload), CreatedAt: model.CreatedAt,
			}
		}
		result = workspace.EventBatchRecord{
			LastSeq: bounds.LastSeq, MinAvailableSeq: bounds.MinAvailableSeq, Events: events,
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return workspace.EventBatchRecord{}, mapWorkspaceRepositoryError(err)
	}
	return result, nil
}

// mapWorkspaceRepositoryError 隐藏 SQL、表名和参数，同时保留取消、稳定边界与 Reset 语义。
func mapWorkspaceRepositoryError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, workspace.ErrNotFound), errors.Is(err, workspace.ErrSnapshotTooLarge),
		errors.Is(err, workspace.ErrInvalidCursor):
		return err
	default:
		return workspace.ErrPersistence
	}
}

var _ workspace.Repository = (*WorkspaceRepository)(nil)
