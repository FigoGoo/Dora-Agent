package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ skill.GovernanceRepository = (*SkillRepository)(nil)

// skillGovernanceReadDTO 承载 Skill 聚合与 current published snapshot 的一次集合查询结果。
// 发布字段使用指针，便于区分“从未发布”的合法 404 与悬空 current pointer 的存储损坏。
type skillGovernanceReadDTO struct {
	SkillID                      string     `gorm:"column:skill_id"`
	CurrentDraftRevisionID       string     `gorm:"column:current_draft_revision_id"`
	CurrentPublishedSnapshotID   *string    `gorm:"column:current_published_snapshot_id"`
	PublicationRevision          int64      `gorm:"column:publication_revision"`
	GovernanceStatus             string     `gorm:"column:governance_status"`
	GovernanceEpoch              int64      `gorm:"column:governance_epoch"`
	SkillVersion                 int64      `gorm:"column:skill_version"`
	PublishedID                  *string    `gorm:"column:published_id"`
	PublishedSkillID             *string    `gorm:"column:published_skill_id"`
	PublishedSourceRevisionID    *string    `gorm:"column:published_source_revision_id"`
	PublishedReviewID            *string    `gorm:"column:published_review_id"`
	PublishedPublicationRevision *int64     `gorm:"column:published_publication_revision"`
	PublishedDefinitionSchema    *string    `gorm:"column:published_definition_schema"`
	PublishedDefinitionJSON      []byte     `gorm:"column:published_definition_json"`
	PublishedContentDigest       []byte     `gorm:"column:published_content_digest"`
	PublishedByUserID            *string    `gorm:"column:published_by_user_id"`
	PublishedAt                  *time.Time `gorm:"column:published_at"`
}

// ListGovernance 使用 current pointer JOIN 和 limit+1 keyset 查询返回指定治理状态的已发布 Skill。
func (repository *SkillRepository) ListGovernance(
	ctx context.Context,
	status skill.GovernanceStatus,
	boundary *skill.GovernanceQueueBoundary,
	limit int,
) (skill.GovernanceQueuePage, error) {
	if !validGovernanceRepositoryStatus(status) || limit <= 0 || limit > 100 {
		return skill.GovernanceQueuePage{}, skill.ErrPersistence
	}
	query := skillGovernanceSelectSQL + `
WHERE skill_record.governance_status = ?
  AND skill_record.current_published_snapshot_id IS NOT NULL`
	arguments := []any{status}
	if boundary != nil {
		query += ` AND (published_record.published_at, published_record.id) < (?, ?)`
		arguments = append(arguments, boundary.PublishedAt, boundary.PublishedSnapshotID)
	}
	// NULLS FIRST 让任意悬空 current pointer 在第一页立即触发 mapper 失败，而不是被静默隐藏。
	query += ` ORDER BY published_record.published_at DESC NULLS FIRST, published_record.id DESC NULLS FIRST LIMIT ?`
	arguments = append(arguments, limit+1)

	var records []skillGovernanceReadDTO
	if err := repository.db.WithContext(ctx).Raw(query, arguments...).Scan(&records).Error; err != nil {
		return skill.GovernanceQueuePage{}, mapSkillRepositoryError(fmt.Errorf("list skill governance: %w", err))
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	page := skill.GovernanceQueuePage{Items: make([]skill.GovernanceQueueItem, 0, len(records)), HasMore: hasMore}
	for _, record := range records {
		state, err := governanceStateFromReadDTO(record)
		if err != nil {
			return skill.GovernanceQueuePage{}, mapSkillRepositoryError(err)
		}
		page.Items = append(page.Items, skill.GovernanceQueueItem{
			SkillID: state.SkillID, PublishedSnapshotID: state.CurrentPublishedSnapshotID,
			Name: state.Published.Definition.Name, Summary: state.Published.Definition.Summary,
			Category: state.Published.Definition.Category, PublishedAt: state.Published.PublishedAt,
			GovernanceStatus: state.GovernanceStatus, GovernanceEpoch: state.GovernanceEpoch,
		})
	}
	return page, nil
}

// FindGovernanceDetail 以 Skill 为查询锚点读取 current published snapshot；从未发布统一返回治理 404。
func (repository *SkillRepository) FindGovernanceDetail(ctx context.Context, skillID string) (skill.GovernanceState, error) {
	var record skillGovernanceReadDTO
	result := repository.db.WithContext(ctx).Raw(skillGovernanceSelectSQL+` WHERE skill_record.id = ?`, skillID).Scan(&record)
	if result.Error != nil {
		return skill.GovernanceState{}, mapSkillRepositoryError(fmt.Errorf("find skill governance detail: %w", result.Error))
	}
	if result.RowsAffected != 1 || record.CurrentPublishedSnapshotID == nil {
		return skill.GovernanceState{}, skill.ErrGovernanceNotFound
	}
	state, err := governanceStateFromReadDTO(record)
	if err != nil {
		return skill.GovernanceState{}, mapSkillRepositoryError(err)
	}
	return state, nil
}

// TransitionGovernance 按 account→assignment→receipt→skill 固定锁序完成授权复核、幂等重放和治理迁移。
func (repository *SkillRepository) TransitionGovernance(
	ctx context.Context,
	command skill.GovernanceTransitionRepositoryCommand,
) (skill.GovernanceTransitionRepositoryResult, error) {
	var transitionResult skill.GovernanceTransitionRepositoryResult
	err := repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := lockActiveGovernorAccount(transaction, command.GovernorUserID); err != nil {
			return err
		}
		if err := lockActiveGovernorAssignment(transaction, command.GovernorUserID); err != nil {
			return err
		}

		var existing skillCommandReceiptModel
		receiptRead := transaction.Session(&gorm.Session{SkipDefaultTransaction: true}).
			Where("actor_user_id = ? AND command_type = ? AND scope_id = ? AND key_digest = ?",
				command.GovernorUserID, skill.CommandTypeGovernanceTransition, command.SkillID, digestBytes(command.KeyDigest)).
			Take(&existing)
		if receiptRead.Error == nil {
			if !bytes.Equal(existing.SemanticDigest, digestBytes(command.SemanticDigest)) {
				return skill.ErrIdempotencyConflict
			}
			var err error
			transitionResult, err = governanceResultFromReceipt(existing)
			if err == nil {
				transitionResult.IdempotentReplay = true
			}
			return err
		}
		if !errors.Is(receiptRead.Error, gorm.ErrRecordNotFound) {
			return fmt.Errorf("read skill governance receipt: %w", receiptRead.Error)
		}

		locked, err := lockSkillGovernanceState(transaction, command.SkillID)
		if err != nil {
			return err
		}
		state, err := governanceStateFromReadDTO(locked)
		if err != nil {
			return err
		}
		targetStatus, ok := governanceTransitionTarget(state.GovernanceStatus, command.Action)
		if !ok {
			return skill.ErrGovernanceConflict
		}
		if err := skill.VerifyGovernanceETag(command.IfMatch, state.SkillID, state.CurrentPublishedSnapshotID, state.GovernanceStatus, state.GovernanceEpoch); err != nil {
			return err
		}
		if state.GovernanceEpoch == math.MaxInt64 || state.SkillVersion == math.MaxInt64 {
			return skill.ErrPersistence
		}
		resultEpoch := state.GovernanceEpoch + 1

		update := transaction.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&skillModel{}).
			Where("id = ? AND version = ? AND governance_status = ? AND governance_epoch = ? AND current_published_snapshot_id = ?",
				state.SkillID, state.SkillVersion, state.GovernanceStatus, state.GovernanceEpoch, state.CurrentPublishedSnapshotID).
			Updates(map[string]any{
				"governance_status": targetStatus,
				"governance_epoch":  resultEpoch,
				"version":           gorm.Expr("version + 1"),
				"updated_at":        command.TransitionedAt,
			})
		if update.Error != nil {
			return fmt.Errorf("CAS skill governance transition: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return skill.ErrGovernanceConflict
		}

		snapshotID := state.CurrentPublishedSnapshotID
		resultEpochValue := resultEpoch
		requestID := command.RequestID
		receiptRecord := skillCommandReceiptModel{
			ID: command.ReceiptID, ActorUserID: command.GovernorUserID, CommandType: skill.CommandTypeGovernanceTransition,
			ScopeID: state.SkillID, KeyDigest: digestBytes(command.KeyDigest), SemanticDigest: digestBytes(command.SemanticDigest),
			ResultSkillID: state.SkillID, ResultPublishedSnapshotID: &snapshotID,
			ResponseDraftRevisionID: locked.CurrentDraftRevisionID, ResponsePublishedSnapshotID: &snapshotID,
			ResponseGovernanceStatus: string(targetStatus), ResponseGovernanceEpoch: &resultEpochValue,
			RequestID: &requestID, CreatedAt: command.TransitionedAt,
		}
		if err := transaction.Session(&gorm.Session{SkipDefaultTransaction: true}).Create(&receiptRecord).Error; err != nil {
			return fmt.Errorf("insert skill governance receipt: %w", err)
		}

		action := governanceAuditAction(command.Action)
		fromStatus := string(state.GovernanceStatus)
		toStatus := string(targetStatus)
		reasonCode := command.ReasonCode
		roleKey := skill.GovernanceRoleKey
		approvalReference := command.ApprovalReference
		sourceAddress := command.SourceAddress
		receiptID := command.ReceiptID
		auditRecord := skillGovernanceAuditModel{
			ID: command.AuditID, SkillID: state.SkillID, Action: action, FromStatus: fromStatus, ToStatus: toStatus,
			SafeReasonCode: &reasonCode, ActorUserID: command.GovernorUserID, ActorRoleKey: &roleKey,
			GovernanceEpoch: &resultEpochValue, ApprovalReference: &approvalReference,
			SourceAddress: &sourceAddress, CommandReceiptID: &receiptID, RequestID: &requestID,
			OccurredAt: command.TransitionedAt,
		}
		if err := transaction.Session(&gorm.Session{SkipDefaultTransaction: true}).Create(&auditRecord).Error; err != nil {
			return fmt.Errorf("append skill governance transition audit: %w", err)
		}

		transitionResult = skill.GovernanceTransitionRepositoryResult{
			SkillID: state.SkillID, PublishedSnapshotID: snapshotID,
			GovernanceStatus: targetStatus, GovernanceEpoch: resultEpoch,
			TransitionedAt: command.TransitionedAt,
		}
		return nil
	})
	if err != nil {
		return skill.GovernanceTransitionRepositoryResult{}, mapSkillRepositoryError(err)
	}
	return transitionResult, nil
}

// lockActiveGovernorAccount 锁定治理命令的 active 账户；失败统一为 capability required。
func lockActiveGovernorAccount(transaction *gorm.DB, governorUserID string) error {
	var id string
	result := transaction.Raw(`SELECT id FROM business.user_account WHERE id = ? AND status = 'active' FOR UPDATE`, governorUserID).Scan(&id)
	if result.Error != nil {
		return fmt.Errorf("lock active governor account: %w", result.Error)
	}
	if result.RowsAffected != 1 || id != governorUserID {
		return skill.ErrGovernanceCapabilityRequired
	}
	return nil
}

// lockActiveGovernorAssignment 锁定独立 skill_governor assignment，不能用 Reviewer 角色代替。
func lockActiveGovernorAssignment(transaction *gorm.DB, governorUserID string) error {
	var id string
	result := transaction.Raw(`
SELECT id
FROM business.user_role_assignment
WHERE user_id = ? AND role_key = 'skill_governor' AND status = 'active'
FOR UPDATE`, governorUserID).Scan(&id)
	if result.Error != nil {
		return fmt.Errorf("lock active governor assignment: %w", result.Error)
	}
	if result.RowsAffected != 1 || id == "" {
		return skill.ErrGovernanceCapabilityRequired
	}
	return nil
}

// lockSkillGovernanceState 锁定 Skill 聚合并读取 current snapshot；未发布与不存在统一为治理 404。
func lockSkillGovernanceState(transaction *gorm.DB, skillID string) (skillGovernanceReadDTO, error) {
	var record skillGovernanceReadDTO
	result := transaction.Raw(skillGovernanceSelectSQL+` WHERE skill_record.id = ? FOR UPDATE OF skill_record`, skillID).Scan(&record)
	if result.Error != nil {
		return skillGovernanceReadDTO{}, fmt.Errorf("lock skill governance state: %w", result.Error)
	}
	if result.RowsAffected != 1 || record.CurrentPublishedSnapshotID == nil {
		return skillGovernanceReadDTO{}, skill.ErrGovernanceNotFound
	}
	return record, nil
}

// governanceStateFromReadDTO 重算 current Definition 摘要并核对无物理外键下的全部逻辑关联。
func governanceStateFromReadDTO(record skillGovernanceReadDTO) (skill.GovernanceState, error) {
	if record.CurrentPublishedSnapshotID == nil || record.PublishedID == nil || record.PublishedSkillID == nil ||
		record.PublishedSourceRevisionID == nil || record.PublishedReviewID == nil ||
		record.PublishedPublicationRevision == nil || record.PublishedDefinitionSchema == nil ||
		record.PublishedByUserID == nil || record.PublishedAt == nil ||
		*record.CurrentPublishedSnapshotID != *record.PublishedID || *record.PublishedSkillID != record.SkillID ||
		record.PublicationRevision != *record.PublishedPublicationRevision ||
		*record.PublishedDefinitionSchema != skill.DefinitionSchemaVersionV1 ||
		!validPostgresUUIDv7(record.SkillID) || !validPostgresUUIDv7(*record.PublishedID) ||
		!validPostgresUUIDv7(*record.PublishedSourceRevisionID) || !validPostgresUUIDv7(*record.PublishedReviewID) ||
		!validPostgresUUIDv7(*record.PublishedByUserID) || record.GovernanceEpoch < 1 || record.SkillVersion < 1 {
		return skill.GovernanceState{}, skill.ErrPersistence
	}
	definition, digest, err := skill.DefinitionFromCanonicalV1(record.PublishedDefinitionJSON)
	if err != nil || !equalDigestBytes(record.PublishedContentDigest, digest) {
		return skill.GovernanceState{}, skill.ErrPersistence
	}
	status := skill.GovernanceStatus(record.GovernanceStatus)
	if !validGovernanceRepositoryStatus(status) {
		return skill.GovernanceState{}, skill.ErrPersistence
	}
	return skill.GovernanceState{
		SkillID: record.SkillID, CurrentPublishedSnapshotID: *record.CurrentPublishedSnapshotID,
		PublicationRevision: record.PublicationRevision, GovernanceStatus: status,
		GovernanceEpoch: record.GovernanceEpoch, SkillVersion: record.SkillVersion,
		Published: skill.PublishedSnapshot{
			ID: *record.PublishedID, SkillID: *record.PublishedSkillID,
			SourceContentRevisionID: *record.PublishedSourceRevisionID, ReviewSubmissionID: *record.PublishedReviewID,
			PublicationRevision: *record.PublishedPublicationRevision, Definition: definition,
			CanonicalJSON: append([]byte(nil), record.PublishedDefinitionJSON...), ContentDigest: digest,
			PublishedByUserID: *record.PublishedByUserID, PublishedAt: record.PublishedAt.UTC(),
		},
	}, nil
}

// governanceResultFromReceipt 恢复首次冻结结果，并拒绝跨命令或字段错配的损坏回执。
func governanceResultFromReceipt(record skillCommandReceiptModel) (skill.GovernanceTransitionRepositoryResult, error) {
	if record.CommandType != skill.CommandTypeGovernanceTransition || record.ScopeID == "" || record.ResultSkillID != record.ScopeID ||
		record.ResultContentRevisionID != nil || record.ResultReviewSubmissionID != nil ||
		record.ResultPublishedSnapshotID == nil || record.ResponsePublishedSnapshotID == nil ||
		*record.ResultPublishedSnapshotID != *record.ResponsePublishedSnapshotID ||
		record.ResponseReviewSubmissionID != nil || record.ResponseReviewStatus != nil ||
		record.ResponseReviewReasonCode != nil || record.ResponseReviewUpdatedAt != nil ||
		record.ResponseGovernanceEpoch == nil || *record.ResponseGovernanceEpoch < 2 || record.RequestID == nil ||
		!validPostgresUUIDv7(record.ResultSkillID) || !validPostgresUUIDv7(*record.ResultPublishedSnapshotID) ||
		record.ResponseDraftRevisionID == "" || record.CreatedAt.IsZero() {
		return skill.GovernanceTransitionRepositoryResult{}, skill.ErrPersistence
	}
	status := skill.GovernanceStatus(record.ResponseGovernanceStatus)
	if !validGovernanceRepositoryStatus(status) {
		return skill.GovernanceTransitionRepositoryResult{}, skill.ErrPersistence
	}
	return skill.GovernanceTransitionRepositoryResult{
		SkillID: record.ResultSkillID, PublishedSnapshotID: *record.ResultPublishedSnapshotID,
		GovernanceStatus: status, GovernanceEpoch: *record.ResponseGovernanceEpoch,
		TransitionedAt: record.CreatedAt.UTC(),
	}, nil
}

// governanceTransitionTarget 同时验证当前状态和动作，只允许 Frozen 设计中的四条边。
func governanceTransitionTarget(current skill.GovernanceStatus, action skill.GovernanceAction) (skill.GovernanceStatus, bool) {
	switch {
	case current == skill.GovernanceStatusActive && action == skill.GovernanceActionSuspend:
		return skill.GovernanceStatusSuspended, true
	case current == skill.GovernanceStatusSuspended && action == skill.GovernanceActionResume:
		return skill.GovernanceStatusActive, true
	case (current == skill.GovernanceStatusActive || current == skill.GovernanceStatusSuspended) && action == skill.GovernanceActionOffline:
		return skill.GovernanceStatusOffline, true
	default:
		return "", false
	}
}

func governanceAuditAction(action skill.GovernanceAction) string {
	switch action {
	case skill.GovernanceActionSuspend:
		return "governance_suspended"
	case skill.GovernanceActionResume:
		return "governance_resumed"
	case skill.GovernanceActionOffline:
		return "governance_offlined"
	default:
		return ""
	}
}

func validGovernanceRepositoryStatus(status skill.GovernanceStatus) bool {
	return status == skill.GovernanceStatusActive || status == skill.GovernanceStatusSuspended || status == skill.GovernanceStatusOffline
}

func validPostgresUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

const skillGovernanceSelectSQL = `
SELECT
    skill_record.id AS skill_id,
    skill_record.current_draft_revision_id,
    skill_record.current_published_snapshot_id,
    skill_record.publication_revision,
    skill_record.governance_status,
    skill_record.governance_epoch,
    skill_record.version AS skill_version,
    published_record.id AS published_id,
    published_record.skill_id AS published_skill_id,
    published_record.source_content_revision_id AS published_source_revision_id,
    published_record.review_submission_id AS published_review_id,
    published_record.publication_revision AS published_publication_revision,
    published_record.definition_schema_version AS published_definition_schema,
    published_record.definition_json AS published_definition_json,
    published_record.content_digest AS published_content_digest,
    published_record.published_by_user_id,
    published_record.published_at
FROM business.skill AS skill_record
LEFT JOIN business.skill_published_snapshot AS published_record
  ON published_record.id = skill_record.current_published_snapshot_id
 AND published_record.skill_id = skill_record.id`
