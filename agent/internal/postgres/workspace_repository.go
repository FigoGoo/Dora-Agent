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
	ID                        string         `gorm:"column:id"`
	ProjectID                 string         `gorm:"column:project_id"`
	UserID                    string         `gorm:"column:user_id"`
	Status                    string         `gorm:"column:status"`
	Version                   int64          `gorm:"column:version"`
	CreatedAt                 sql.NullTime   `gorm:"column:created_at"`
	UpdatedAt                 sql.NullTime   `gorm:"column:updated_at"`
	LastSeq                   int64          `gorm:"column:last_seq"`
	MinAvailableSeq           int64          `gorm:"column:min_available_seq"`
	PreviewSessionID          sql.NullString `gorm:"column:preview_session_id"`
	PreviewSchemaVersion      sql.NullString `gorm:"column:preview_schema_version"`
	PreviewResourceID         sql.NullString `gorm:"column:preview_resource_id"`
	PreviewProjectID          sql.NullString `gorm:"column:preview_project_id"`
	PreviewResourceVersion    sql.NullInt64  `gorm:"column:preview_resource_version"`
	PreviewContentDigest      sql.NullString `gorm:"column:preview_content_digest"`
	PreviewStatus             sql.NullString `gorm:"column:preview_status"`
	PreviewTitle              sql.NullString `gorm:"column:preview_title"`
	PreviewGoal               sql.NullString `gorm:"column:preview_goal"`
	PreviewDeliverableType    sql.NullString `gorm:"column:preview_deliverable_type"`
	PreviewAudience           sql.NullString `gorm:"column:preview_audience"`
	PreviewLocale             sql.NullString `gorm:"column:preview_locale"`
	PreviewPhases             sql.NullString `gorm:"column:preview_phases"`
	PreviewConstraints        sql.NullString `gorm:"column:preview_constraints"`
	PreviewAcceptanceCriteria sql.NullString `gorm:"column:preview_acceptance_criteria"`
	PreviewUpdatedAt          sql.NullTime   `gorm:"column:preview_updated_at"`
	OutputSessionID           sql.NullString `gorm:"column:output_session_id"`
	OutputSourceInputID       sql.NullString `gorm:"column:output_source_input_id"`
	OutputSourceEnqueueSeq    sql.NullInt64  `gorm:"column:output_source_enqueue_seq"`
	OutputTurnID              sql.NullString `gorm:"column:output_turn_id"`
	OutputRunID               sql.NullString `gorm:"column:output_run_id"`
	OutputSchemaVersion       sql.NullString `gorm:"column:output_schema_version"`
	OutputStatus              sql.NullString `gorm:"column:output_status"`
	OutputPayload             sql.NullString `gorm:"column:output_payload"`
	OutputProjectionVersion   sql.NullInt64  `gorm:"column:output_projection_version"`
	OutputUpdatedAt           sql.NullTime   `gorm:"column:output_updated_at"`
	AnalyzeSessionID          sql.NullString `gorm:"column:analyze_session_id"`
	AnalyzeSourceInputID      sql.NullString `gorm:"column:analyze_source_input_id"`
	AnalyzeSourceEnqueueSeq   sql.NullInt64  `gorm:"column:analyze_source_enqueue_seq"`
	AnalyzeTurnID             sql.NullString `gorm:"column:analyze_turn_id"`
	AnalyzeRunID              sql.NullString `gorm:"column:analyze_run_id"`
	AnalyzeToolCallID         sql.NullString `gorm:"column:analyze_tool_call_id"`
	AnalyzeSchemaVersion      sql.NullString `gorm:"column:analyze_schema_version"`
	AnalyzeOutcomeKind        sql.NullString `gorm:"column:analyze_outcome_kind"`
	AnalyzeStatus             sql.NullString `gorm:"column:analyze_status"`
	AnalyzeResultDigest       sql.NullString `gorm:"column:analyze_result_digest"`
	AnalyzePayload            sql.NullString `gorm:"column:analyze_payload"`
	AnalyzeProjectionVersion  sql.NullInt64  `gorm:"column:analyze_projection_version"`
	AnalyzeCreatedAt          sql.NullTime   `gorm:"column:analyze_created_at"`
	PlanSessionID             sql.NullString `gorm:"column:plan_session_id"`
	PlanSourceInputID         sql.NullString `gorm:"column:plan_source_input_id"`
	PlanSourceEnqueueSeq      sql.NullInt64  `gorm:"column:plan_source_enqueue_seq"`
	PlanTurnID                sql.NullString `gorm:"column:plan_turn_id"`
	PlanRunID                 sql.NullString `gorm:"column:plan_run_id"`
	PlanToolCallID            sql.NullString `gorm:"column:plan_tool_call_id"`
	PlanEventType             sql.NullString `gorm:"column:plan_event_type"`
	PlanPayload               sql.NullString `gorm:"column:plan_payload"`
	PlanAggregateVersion      sql.NullInt64  `gorm:"column:plan_aggregate_version"`
	PlanCreatedAt             sql.NullTime   `gorm:"column:plan_created_at"`
	WriteSessionID            sql.NullString `gorm:"column:write_session_id"`
	WriteSourceInputID        sql.NullString `gorm:"column:write_source_input_id"`
	WriteSourceEnqueueSeq     sql.NullInt64  `gorm:"column:write_source_enqueue_seq"`
	WriteTurnID               sql.NullString `gorm:"column:write_turn_id"`
	WriteRunID                sql.NullString `gorm:"column:write_run_id"`
	WriteToolCallID           sql.NullString `gorm:"column:write_tool_call_id"`
	WriteEventType            sql.NullString `gorm:"column:write_event_type"`
	WritePayload              sql.NullString `gorm:"column:write_payload"`
	WriteAggregateVersion     sql.NullInt64  `gorm:"column:write_aggregate_version"`
	WriteCreatedAt            sql.NullTime   `gorm:"column:write_created_at"`
}

type workspaceMediaPreviewRow struct {
	Seq              int64        `gorm:"column:seq"`
	EventID          string       `gorm:"column:event_id"`
	SessionID        string       `gorm:"column:session_id"`
	EventType        string       `gorm:"column:event_type"`
	AggregateType    string       `gorm:"column:aggregate_type"`
	AggregateID      string       `gorm:"column:aggregate_id"`
	AggregateVersion int64        `gorm:"column:aggregate_version"`
	Payload          string       `gorm:"column:payload"`
	CreatedAt        sql.NullTime `gorm:"column:created_at"`
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
			       session_record.updated_at, event_counter.last_seq, event_counter.min_available_seq,
			       preview.session_id AS preview_session_id,
			       preview.schema_version AS preview_schema_version,
			       preview.resource_id AS preview_resource_id,
			       preview.project_id AS preview_project_id,
			       preview.resource_version AS preview_resource_version,
			       preview.content_digest AS preview_content_digest,
			       preview.status AS preview_status, preview.title AS preview_title,
			       preview.goal AS preview_goal, preview.deliverable_type AS preview_deliverable_type,
			       preview.audience AS preview_audience, preview.locale AS preview_locale,
			       preview.phases::text AS preview_phases,
			       preview.constraints::text AS preview_constraints,
			       preview.acceptance_criteria::text AS preview_acceptance_criteria,
			       preview.updated_at AS preview_updated_at,
			       turn_output.session_id AS output_session_id,
			       turn_output.source_input_id AS output_source_input_id,
			       turn_output.source_enqueue_seq AS output_source_enqueue_seq,
			       turn_output.turn_id AS output_turn_id,
			       turn_output.run_id AS output_run_id,
			       turn_output.schema_version AS output_schema_version,
			       turn_output.status AS output_status,
			       turn_output.payload::text AS output_payload,
			       turn_output.projection_version AS output_projection_version,
			       turn_output.updated_at AS output_updated_at,
			       analyze_projection.session_id AS analyze_session_id,
			       analyze_projection.source_input_id AS analyze_source_input_id,
			       analyze_projection.source_enqueue_seq AS analyze_source_enqueue_seq,
			       analyze_projection.turn_id AS analyze_turn_id,
			       analyze_projection.run_id AS analyze_run_id,
			       analyze_projection.tool_call_id AS analyze_tool_call_id,
			       analyze_projection.schema_version AS analyze_schema_version,
			       analyze_projection.outcome_kind AS analyze_outcome_kind,
			       analyze_projection.status AS analyze_status,
			       analyze_projection.result_digest AS analyze_result_digest,
			       analyze_projection.payload::text AS analyze_payload,
			       analyze_projection.projection_version AS analyze_projection_version,
			       analyze_projection.created_at AS analyze_created_at,
			       plan_projection.session_id AS plan_session_id,
			       plan_projection.source_input_id AS plan_source_input_id,
			       plan_projection.source_enqueue_seq AS plan_source_enqueue_seq,
			       plan_projection.turn_id AS plan_turn_id,
			       plan_projection.run_id AS plan_run_id,
			       plan_projection.tool_call_id AS plan_tool_call_id,
			       plan_projection.event_type AS plan_event_type,
			       plan_projection.payload::text AS plan_payload,
			       plan_projection.aggregate_version AS plan_aggregate_version,
			       plan_projection.created_at AS plan_created_at,
			       write_projection.session_id AS write_session_id,
			       write_projection.source_input_id AS write_source_input_id,
			       write_projection.source_enqueue_seq AS write_source_enqueue_seq,
			       write_projection.turn_id AS write_turn_id,
			       write_projection.run_id AS write_run_id,
			       write_projection.tool_call_id AS write_tool_call_id,
			       write_projection.event_type AS write_event_type,
			       write_projection.payload::text AS write_payload,
			       write_projection.aggregate_version AS write_aggregate_version,
			       write_projection.created_at AS write_created_at
			FROM agent.session AS session_record
			JOIN agent.session_skill_snapshot AS skill_snapshot
			  ON skill_snapshot.session_id = session_record.id
			JOIN agent.session_event_counter AS event_counter
			  ON event_counter.session_id = session_record.id
				LEFT JOIN agent.creation_spec_preview_projection AS preview
				  ON preview.session_id = session_record.id
				LEFT JOIN agent.session_user_message_output_projection AS turn_output
				  ON turn_output.session_id = session_record.id
				LEFT JOIN LATERAL (
				    SELECT projection.*
				    FROM agent.analyze_materials_preview_projection AS projection
				    WHERE projection.session_id = session_record.id
				    ORDER BY projection.source_enqueue_seq DESC, projection.source_input_id DESC
				    LIMIT 1
				) AS analyze_projection ON TRUE
				LEFT JOIN LATERAL (
				    SELECT event_record.session_id,
				           event_record.aggregate_id AS source_input_id,
				           input_record.enqueue_seq AS source_enqueue_seq,
				           context_record.turn_id,
				           context_record.run_id,
				           context_record.tool_call_id,
				           event_record.event_type,
				           event_record.payload,
				           event_record.aggregate_version,
				           event_record.created_at
				    FROM agent.session_event_log AS event_record
				    JOIN agent.session_input AS input_record
				      ON input_record.id = event_record.aggregate_id
				     AND input_record.session_id = event_record.session_id
				     AND input_record.source_type = 'plan_storyboard_preview'
				    JOIN agent.plan_storyboard_preview_turn_context AS context_record
				      ON context_record.input_id = event_record.aggregate_id
				     AND context_record.session_id = event_record.session_id
				    WHERE event_record.session_id = session_record.id
				      AND event_record.aggregate_type = 'plan_storyboard_preview'
				      AND event_record.event_type IN (
				          'plan_storyboard.preview.completed',
				          'plan_storyboard.preview.failed',
				          'plan_storyboard.preview.runtime_failed'
				      )
				    ORDER BY input_record.enqueue_seq DESC, event_record.seq DESC
				    LIMIT 1
				) AS plan_projection ON TRUE
				LEFT JOIN LATERAL (
				    SELECT event_record.session_id,
				           event_record.aggregate_id AS source_input_id,
				           input_record.enqueue_seq AS source_enqueue_seq,
				           context_record.turn_id,
				           context_record.run_id,
				           context_record.tool_call_id,
				           event_record.event_type,
				           event_record.payload,
				           event_record.aggregate_version,
				           event_record.created_at
				    FROM agent.session_event_log AS event_record
				    JOIN agent.session_input AS input_record
				      ON input_record.id = event_record.aggregate_id
				     AND input_record.session_id = event_record.session_id
				     AND input_record.source_type = 'write_prompts_preview'
				    JOIN agent.write_prompts_preview_turn_context AS context_record
				      ON context_record.input_id = event_record.aggregate_id
				     AND context_record.session_id = event_record.session_id
				    WHERE event_record.session_id = session_record.id
				      AND event_record.aggregate_type = 'write_prompts_preview'
				      AND event_record.event_type IN (
				          'write_prompts.preview.completed',
				          'write_prompts.preview.failed',
				          'write_prompts.preview.runtime_failed'
				      )
				    ORDER BY input_record.enqueue_seq DESC, event_record.seq DESC
				    LIMIT 1
				) AS write_projection ON TRUE
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

		var mediaRows []workspaceMediaPreviewRow
		if limits.MaxMediaPreviews > 0 {
			if err := tx.Raw(`
			SELECT seq, event_id, session_id, event_type, aggregate_type, aggregate_id,
			       aggregate_version, payload::text AS payload, created_at
			FROM agent.session_event_log
			WHERE session_id = ? AND event_type IN (
			  'media.preview.accepted','media.preview.completed','media.preview.failed','media.preview.runtime_failed'
			)
			ORDER BY seq DESC, event_id DESC
			LIMIT ?`, identity.SessionID, limits.MaxMediaPreviews).Scan(&mediaRows).Error; err != nil {
				return fmt.Errorf("load Workspace media previews: %w", err)
			}
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
		var preview *workspace.CreationSpecPreviewRecord
		if sessionRow.PreviewSessionID.Valid {
			preview = &workspace.CreationSpecPreviewRecord{
				SchemaVersion:   sessionRow.PreviewSchemaVersion.String,
				CreationSpecID:  sessionRow.PreviewResourceID.String,
				ProjectID:       sessionRow.PreviewProjectID.String,
				Version:         sessionRow.PreviewResourceVersion.Int64,
				Status:          sessionRow.PreviewStatus.String,
				ContentDigest:   sessionRow.PreviewContentDigest.String,
				Title:           sessionRow.PreviewTitle.String,
				Goal:            sessionRow.PreviewGoal.String,
				DeliverableType: sessionRow.PreviewDeliverableType.String,
				Locale:          sessionRow.PreviewLocale.String,
				PhasesJSON:      []byte(sessionRow.PreviewPhases.String),
				ConstraintsJSON: []byte(sessionRow.PreviewConstraints.String),
				AcceptanceJSON:  []byte(sessionRow.PreviewAcceptanceCriteria.String),
				UpdatedAt:       sessionRow.PreviewUpdatedAt.Time,
			}
			if sessionRow.PreviewAudience.Valid {
				audience := sessionRow.PreviewAudience.String
				preview.Audience = &audience
			}
		}
		var latestTurnOutput *workspace.TurnOutputRecord
		if sessionRow.OutputSessionID.Valid {
			latestTurnOutput = &workspace.TurnOutputRecord{
				SessionID:         sessionRow.OutputSessionID.String,
				SourceInputID:     sessionRow.OutputSourceInputID.String,
				SourceEnqueueSeq:  sessionRow.OutputSourceEnqueueSeq.Int64,
				TurnID:            sessionRow.OutputTurnID.String,
				RunID:             sessionRow.OutputRunID.String,
				SchemaVersion:     sessionRow.OutputSchemaVersion.String,
				Status:            sessionRow.OutputStatus.String,
				PayloadJSON:       []byte(sessionRow.OutputPayload.String),
				ProjectionVersion: sessionRow.OutputProjectionVersion.Int64,
				UpdatedAt:         sessionRow.OutputUpdatedAt.Time,
			}
		}
		var analyzeMaterialsPreview *workspace.AnalyzeMaterialsPreviewRecord
		if sessionRow.AnalyzeSessionID.Valid {
			analyzeMaterialsPreview = &workspace.AnalyzeMaterialsPreviewRecord{
				SessionID: sessionRow.AnalyzeSessionID.String, SourceInputID: sessionRow.AnalyzeSourceInputID.String,
				SourceEnqueueSeq: sessionRow.AnalyzeSourceEnqueueSeq.Int64, TurnID: sessionRow.AnalyzeTurnID.String,
				RunID: sessionRow.AnalyzeRunID.String, ToolCallID: sessionRow.AnalyzeToolCallID.String,
				SchemaVersion: sessionRow.AnalyzeSchemaVersion.String, OutcomeKind: sessionRow.AnalyzeOutcomeKind.String,
				Status: sessionRow.AnalyzeStatus.String, ResultDigest: sessionRow.AnalyzeResultDigest.String,
				PayloadJSON:       []byte(sessionRow.AnalyzePayload.String),
				ProjectionVersion: sessionRow.AnalyzeProjectionVersion.Int64, CreatedAt: sessionRow.AnalyzeCreatedAt.Time,
			}
		}
		var planStoryboardPreview *workspace.PlanStoryboardPreviewRecord
		if sessionRow.PlanSessionID.Valid {
			planStoryboardPreview = &workspace.PlanStoryboardPreviewRecord{
				SessionID: sessionRow.PlanSessionID.String, SourceInputID: sessionRow.PlanSourceInputID.String,
				SourceEnqueueSeq: sessionRow.PlanSourceEnqueueSeq.Int64, TurnID: sessionRow.PlanTurnID.String,
				RunID: sessionRow.PlanRunID.String, ToolCallID: sessionRow.PlanToolCallID.String,
				EventType:   sessionRow.PlanEventType.String,
				PayloadJSON: []byte(sessionRow.PlanPayload.String), AggregateVersion: sessionRow.PlanAggregateVersion.Int64,
				CreatedAt: sessionRow.PlanCreatedAt.Time,
			}
		}
		var writePromptsPreview *workspace.WritePromptsPreviewRecord
		if sessionRow.WriteSessionID.Valid {
			writePromptsPreview = &workspace.WritePromptsPreviewRecord{
				SessionID: sessionRow.WriteSessionID.String, SourceInputID: sessionRow.WriteSourceInputID.String,
				SourceEnqueueSeq: sessionRow.WriteSourceEnqueueSeq.Int64, TurnID: sessionRow.WriteTurnID.String,
				RunID: sessionRow.WriteRunID.String, ToolCallID: sessionRow.WriteToolCallID.String,
				EventType: sessionRow.WriteEventType.String, PayloadJSON: []byte(sessionRow.WritePayload.String),
				AggregateVersion: sessionRow.WriteAggregateVersion.Int64, CreatedAt: sessionRow.WriteCreatedAt.Time,
			}
		}
		mediaPreviews := make([]workspace.MediaPreviewRecord, len(mediaRows))
		for index, row := range mediaRows {
			target := len(mediaRows) - 1 - index
			mediaPreviews[target] = workspace.MediaPreviewRecord{Seq: row.Seq, EventID: row.EventID,
				SessionID: row.SessionID, EventType: row.EventType, AggregateType: row.AggregateType,
				AggregateID: row.AggregateID, AggregateVersion: row.AggregateVersion,
				PayloadJSON: []byte(row.Payload), CreatedAt: row.CreatedAt.Time}
		}
		result = workspace.SnapshotRecord{
			Session: workspace.SessionRecord{
				ID: sessionRow.ID, ProjectID: sessionRow.ProjectID, UserID: sessionRow.UserID,
				Status: sessionRow.Status, Version: sessionRow.Version,
				CreatedAt: sessionRow.CreatedAt.Time, UpdatedAt: sessionRow.UpdatedAt.Time,
			},
			Messages: messages, Inputs: inputs, CreationSpecPreview: preview, LatestTurnOutput: latestTurnOutput,
			AnalyzeMaterialsPreview: analyzeMaterialsPreview,
			PlanStoryboardPreview:   planStoryboardPreview,
			WritePromptsPreview:     writePromptsPreview,
			MediaPreviews:           mediaPreviews,
			EventHighWatermark:      sessionRow.LastSeq,
			MinAvailableSeq:         sessionRow.MinAvailableSeq,
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

// workspaceEventRow 为 Storyboard terminal Event 额外承接冻结 Context 身份；其他 Event 三列保持 NULL。
type workspaceEventRow struct {
	EventID          string         `gorm:"column:event_id"`
	SessionID        string         `gorm:"column:session_id"`
	Seq              int64          `gorm:"column:seq"`
	EventType        string         `gorm:"column:event_type"`
	SchemaVersion    string         `gorm:"column:schema_version"`
	AggregateType    string         `gorm:"column:aggregate_type"`
	AggregateID      string         `gorm:"column:aggregate_id"`
	AggregateVersion int64          `gorm:"column:aggregate_version"`
	Payload          string         `gorm:"column:payload"`
	CreatedAt        sql.NullTime   `gorm:"column:created_at"`
	PlanTurnID       sql.NullString `gorm:"column:plan_turn_id"`
	PlanRunID        sql.NullString `gorm:"column:plan_run_id"`
	PlanToolCallID   sql.NullString `gorm:"column:plan_tool_call_id"`
	WriteTurnID      sql.NullString `gorm:"column:write_turn_id"`
	WriteRunID       sql.NullString `gorm:"column:write_run_id"`
	WriteToolCallID  sql.NullString `gorm:"column:write_tool_call_id"`
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

		var rows []workspaceEventRow
		if err := tx.Raw(`
			SELECT event_record.event_id, event_record.session_id, event_record.seq,
			       event_record.event_type, event_record.schema_version,
			       event_record.aggregate_type, event_record.aggregate_id,
			       event_record.aggregate_version, event_record.payload::text AS payload,
			       event_record.created_at,
			       context_record.turn_id AS plan_turn_id,
			       context_record.run_id AS plan_run_id,
			       context_record.tool_call_id AS plan_tool_call_id,
			       write_context.turn_id AS write_turn_id,
			       write_context.run_id AS write_run_id,
			       write_context.tool_call_id AS write_tool_call_id
			FROM agent.session_event_log AS event_record
			LEFT JOIN agent.plan_storyboard_preview_turn_context AS context_record
			  ON event_record.event_type IN (
			      'plan_storyboard.preview.completed',
			      'plan_storyboard.preview.failed',
			      'plan_storyboard.preview.runtime_failed'
			  )
			 AND event_record.aggregate_type = 'plan_storyboard_preview'
			 AND context_record.input_id = event_record.aggregate_id
			 AND context_record.session_id = event_record.session_id
			LEFT JOIN agent.write_prompts_preview_turn_context AS write_context
			  ON event_record.event_type IN (
			      'write_prompts.preview.completed',
			      'write_prompts.preview.failed',
			      'write_prompts.preview.runtime_failed'
			  )
			 AND event_record.aggregate_type = 'write_prompts_preview'
			 AND write_context.input_id = event_record.aggregate_id
			 AND write_context.session_id = event_record.session_id
			WHERE event_record.session_id = ? AND event_record.seq > ?
			ORDER BY event_record.seq ASC
			LIMIT ?`, identity.SessionID, cursor, limit).Scan(&rows).Error; err != nil {
			return fmt.Errorf("load Workspace event batch: %w", err)
		}
		events := make([]workspace.EventRecord, len(rows))
		for index, row := range rows {
			events[index] = workspace.EventRecord{
				EventID: row.EventID, SessionID: row.SessionID, Seq: row.Seq,
				EventType: row.EventType, SchemaVersion: row.SchemaVersion,
				AggregateType: row.AggregateType, AggregateID: row.AggregateID,
				AggregateVersion: row.AggregateVersion, Payload: []byte(row.Payload),
				PlanTurnID: row.PlanTurnID.String, PlanRunID: row.PlanRunID.String,
				PlanToolCallID: row.PlanToolCallID.String, CreatedAt: row.CreatedAt.Time,
				WriteTurnID: row.WriteTurnID.String, WriteRunID: row.WriteRunID.String,
				WriteToolCallID: row.WriteToolCallID.String,
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
