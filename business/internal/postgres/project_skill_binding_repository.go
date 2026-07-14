package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateQuickV2 原子创建 Project、初始 Skill Binding、不可变 Resolution、默认 Session Binding 与完整加密 Outbox。
// 本方法不注册 HTTP 或执行 Agent RPC；保护器必须已经在事务前完成密钥加载且 Protect 只能做本地有界运算。
func (r *ProjectRepository) CreateQuickV2(
	ctx context.Context,
	command projectskillbinding.QuickCreateV2Command,
	limits projectskillbinding.LimitsV1,
	protector projectskillbinding.OutboxPayloadProtectorV2,
) (projectskillbinding.QuickCreateV2Result, error) {
	if err := command.Validate(limits); err != nil || protector == nil {
		return projectskillbinding.QuickCreateV2Result{}, projectskillbinding.ErrInvalidBinding
	}
	initialReceipt := projectCreationReceiptV2FromCommand(command)
	var result projectskillbinding.QuickCreateV2Result
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactionDB := tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		// 回执必须先于任何候选 Project/Binding 争夺唯一幂等范围；并发 loser 等待 winner 提交后只重放冻结结果。
		insertReceipt := transactionDB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "owner_user_id"}, {Name: "command_type"}, {Name: "key_digest"}},
			DoNothing: true,
		}).Create(&initialReceipt)
		if insertReceipt.Error != nil {
			return fmt.Errorf("claim quick create v2 receipt: %w", insertReceipt.Error)
		}
		if insertReceipt.RowsAffected == 0 {
			existing, err := readProjectCreationReceiptV2(transactionDB, command.OwnerUserID, command.KeyDigest)
			if err != nil {
				return err
			}
			// 同一 key 在 V1/V2 间绝不互相重放；schema 或语义任一不同都稳定冲突且不新建 Project。
			if existing.RequestSchemaVersion != projectskillbinding.QuickCreateSchemaVersionV2 ||
				!bytes.Equal(existing.SemanticDigest, command.SemanticDigest[:]) {
				return project.ErrIdempotencyConflict
			}
			result, err = projectSkillBindingResultFromReceipt(existing, true)
			return err
		}

		if err := transactionDB.Create(&projectModel{
			ID: command.ProjectID, OwnerUserID: command.OwnerUserID, Title: project.DefaultProjectTitle,
			LifecycleStatus: string(project.LifecycleStatusActive), RecentRunStatus: quickCreateRecentStatus(command.PromptPresent),
			InitialPromptStatus: quickCreateInitialPromptStatus(command.PromptPresent), Version: 1,
			CreatedAt: command.OccurredAt, UpdatedAt: command.OccurredAt,
		}).Error; err != nil {
			return fmt.Errorf("insert quick create v2 project: %w", err)
		}
		if err := transactionDB.Create(&projectSkillBindingSetModel{
			ProjectID: command.ProjectID, OwnerUserID: command.OwnerUserID,
			SchemaVersion: projectskillbinding.BindingSetSchemaVersionV1, SetVersion: 1,
			SelectionDigest: append([]byte(nil), command.SelectionDigest[:]...), EnabledCount: len(command.Bindings),
			CreatedAt: command.OccurredAt, UpdatedAt: command.OccurredAt,
		}).Error; err != nil {
			return fmt.Errorf("insert project skill binding set: %w", err)
		}
		bindings, audits := projectSkillBindingModelsFromCommand(command)
		if len(bindings) > 0 {
			// 初始 Binding 与 Audit 都使用单次 batch insert，SQL 数量不会随 Skill 数量增长。
			if err := transactionDB.Create(&bindings).Error; err != nil {
				return fmt.Errorf("batch insert project skill bindings: %w", err)
			}
			if err := transactionDB.Create(&audits).Error; err != nil {
				return fmt.Errorf("batch insert project skill binding audits: %w", err)
			}
		}

		rows, err := readPublishedSkillsForQuickCreateV2(transactionDB, command)
		if err != nil {
			return err
		}
		if len(rows) != len(command.Bindings) {
			// INNER JOIN 缺行代表未发布、不存在或指针不完整；统一 existence-safe 失败，禁止退化为空 Snapshot。
			return projectskillbinding.ErrSkillUnavailable
		}
		resolution, err := projectskillbinding.ResolveProjectSkillSnapshotsV1(projectskillbinding.ResolveInputV1{
			ResolutionID: command.ResolutionID, ProjectID: command.ProjectID, OwnerUserID: command.OwnerUserID,
			CommandID: command.CommandID, BindingSetVersion: 1,
			BindingSelectionDigest: command.SelectionDigest, ResolvedAt: command.OccurredAt,
		}, rows, limits)
		if err != nil {
			return fmt.Errorf("resolve project skill snapshots v1: %w", err)
		}
		// Published 指针仍受本事务 SHARE 锁保护；在这里完成本地加密可确保 payload 与同一事务 Resolution 完全一致。
		prepared, err := projectskillbinding.PrepareOutboxV2(ctx, command, resolution, limits, protector)
		if err != nil {
			return fmt.Errorf("prepare session bootstrap outbox v2: %w", err)
		}
		if err := writeQuickCreateV2FrozenFacts(transactionDB, command, resolution, prepared); err != nil {
			return fmt.Errorf("write quick create v2 frozen facts: %w", err)
		}
		result = projectskillbinding.QuickCreateV2Result{
			ProjectID: command.ProjectID, RequestSchemaVersion: projectskillbinding.QuickCreateSchemaVersionV2,
			SnapshotDigest: prepared.SnapshotDigest, SkillCount: prepared.SkillCount,
			BindingSetVersion: 1, ResolutionID: command.ResolutionID, IdempotentReplay: false,
		}
		return nil
	})
	if err != nil {
		return projectskillbinding.QuickCreateV2Result{}, mapProjectSkillBindingRepositoryError(err)
	}
	return result, nil
}

// readPublishedSkillsForQuickCreateV2 使用一次显式集合 JOIN 读取全部 enabled Binding、Skill 当前发布指针与不可变来源修订。
// FOR SHARE 锁定 Skill 指针直到事务结束，避免一次 Snapshot 混入两个 publication revision。
func readPublishedSkillsForQuickCreateV2(tx *gorm.DB, command projectskillbinding.QuickCreateV2Command) ([]projectskillbinding.PublishedSkillReadDTO, error) {
	if len(command.Bindings) == 0 {
		return make([]projectskillbinding.PublishedSkillReadDTO, 0), nil
	}
	skillIDs := make([]string, len(command.Bindings))
	for index, binding := range command.Bindings {
		skillIDs[index] = binding.SkillID
	}
	var records []projectSkillPublishedReadRecord
	err := tx.Raw(`
		SELECT
			project.id AS project_id,
			project.owner_user_id AS project_owner_user_id,
			project.lifecycle_status AS project_lifecycle_status,
			binding.id AS binding_id,
			binding.version AS binding_version,
			binding.status AS binding_status,
			binding.namespace AS namespace,
			binding.priority AS priority,
			skill.id AS skill_id,
			skill.owner_user_id AS skill_owner_user_id,
			skill.current_published_snapshot_id AS current_published_snapshot_id,
			skill.publication_revision AS skill_publication_revision,
			skill.governance_status AS governance_status,
			skill.governance_epoch AS governance_epoch,
			published.id AS published_snapshot_id,
			published.skill_id AS published_skill_id,
			published.source_content_revision_id AS source_content_revision_id,
			published.publication_revision AS published_publication_revision,
			published.definition_schema_version AS definition_schema_version,
			published.definition_json AS definition_json,
			published.content_digest AS content_digest,
			published.published_by_user_id AS publisher_user_id,
			published.published_at AS published_at,
			revision.id AS revision_id,
			revision.skill_id AS revision_skill_id,
			revision.definition_schema_version AS revision_definition_schema_version,
			revision.definition_json AS revision_definition_json,
			revision.content_digest AS revision_content_digest
		FROM business.project AS project
		JOIN business.project_skill_binding AS binding
		  ON binding.project_id = project.id AND binding.status = 'enabled'
		JOIN business.skill AS skill
		  ON skill.id = binding.skill_id
		JOIN business.skill_published_snapshot AS published
		  ON published.id = skill.current_published_snapshot_id
		JOIN business.skill_content_revision AS revision
		  ON revision.id = published.source_content_revision_id
		WHERE project.id = ? AND project.owner_user_id = ? AND binding.skill_id IN ?
		ORDER BY binding.priority DESC, binding.namespace ASC, binding.skill_id ASC
		FOR SHARE OF skill, published, revision`, command.ProjectID, command.OwnerUserID, skillIDs).Scan(&records).Error
	if err != nil {
		return nil, fmt.Errorf("read project skill published set: %w", err)
	}
	rows := make([]projectskillbinding.PublishedSkillReadDTO, len(records))
	for index, record := range records {
		contentDigest, err := projectSkillBindingDigest(record.ContentDigest)
		if err != nil {
			return nil, err
		}
		revisionDigest, err := projectSkillBindingDigest(record.RevisionContentDigest)
		if err != nil {
			return nil, err
		}
		rows[index] = projectskillbinding.PublishedSkillReadDTO{
			ProjectID: record.ProjectID, ProjectOwnerUserID: record.ProjectOwnerUserID,
			ProjectLifecycleStatus: record.ProjectLifecycleStatus,
			BindingID:              record.BindingID, BindingVersion: record.BindingVersion, BindingStatus: record.BindingStatus,
			Namespace: record.Namespace, Priority: record.Priority, SkillID: record.SkillID,
			SkillOwnerUserID: record.SkillOwnerUserID, CurrentPublishedSnapshotID: record.CurrentPublishedSnapshotID,
			SkillPublicationRevision: record.SkillPublicationRevision, GovernanceStatus: record.GovernanceStatus,
			GovernanceEpoch: record.GovernanceEpoch, PublishedSnapshotID: record.PublishedSnapshotID,
			PublishedSkillID: record.PublishedSkillID, SourceContentRevisionID: record.SourceContentRevisionID,
			PublishedPublicationRevision: record.PublishedPublicationRevision,
			DefinitionSchemaVersion:      record.DefinitionSchemaVersion, DefinitionJSON: append([]byte(nil), record.DefinitionJSON...),
			ContentDigest: contentDigest, PublisherUserID: record.PublisherUserID, PublishedAt: record.PublishedAt,
			RevisionID: record.RevisionID, RevisionSkillID: record.RevisionSkillID,
			RevisionDefinitionSchemaVersion: record.RevisionDefinitionSchemaVersion,
			RevisionDefinitionJSON:          append([]byte(nil), record.RevisionDefinitionJSON...), RevisionContentDigest: revisionDigest,
		}
	}
	return rows, nil
}

// writeQuickCreateV2FrozenFacts 在解析与加密成功后批量写 Resolution，并创建 v2 Session Binding/Outbox 与最终冻结 Receipt。
func writeQuickCreateV2FrozenFacts(
	tx *gorm.DB,
	command projectskillbinding.QuickCreateV2Command,
	resolution projectskillbinding.ResolutionV1,
	prepared projectskillbinding.PreparedOutboxV2,
) error {
	header := projectSessionSkillResolutionModelFromEntity(resolution.Header)
	if err := tx.Create(&header).Error; err != nil {
		return fmt.Errorf("insert project session skill resolution: %w", err)
	}
	items, err := projectSessionSkillResolutionItemModels(resolution.Items)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		if err := tx.Create(&items).Error; err != nil {
			return fmt.Errorf("batch insert project session skill resolution items: %w", err)
		}
	}
	setVersion := int64(1)
	resolutionID := command.ResolutionID
	algorithm := prepared.Envelope.Algorithm
	keyVersion := prepared.Envelope.KeyVersion
	binding := projectSessionBindingV2Model{
		ID: command.SessionBindingID, ProjectID: command.ProjectID, CommandID: command.CommandID,
		RequestDigest: append([]byte(nil), prepared.RequestDigest[:]...), ProvisioningStatus: string(project.ProvisioningStatusPending),
		Version: 1, CreatedAt: command.OccurredAt, UpdatedAt: command.OccurredAt,
		RequestSchemaVersion: projectskillbinding.EnsureProjectSessionSchemaVersionV2,
		SkillSnapshotDigest:  append([]byte(nil), prepared.SnapshotDigest[:]...), SkillCount: prepared.SkillCount,
		BindingSetVersion: &setVersion, ResolutionID: &resolutionID,
	}
	if err := tx.Create(&binding).Error; err != nil {
		return fmt.Errorf("insert project session binding v2: %w", err)
	}
	outbox := projectSessionOutboxV2Model{
		ID: command.CommandID, EventType: project.EnsureSessionEventType,
		SchemaVersion: projectskillbinding.OutboxPayloadSchemaVersionV2,
		AggregateID:   command.ProjectID, OwnerUserID: command.OwnerUserID,
		RequestDigest: append([]byte(nil), prepared.RequestDigest[:]...), HasInitialPrompt: command.PromptPresent,
		PayloadEncryptionAlgorithm: &algorithm, PayloadKeyVersion: &keyVersion,
		PayloadNonce:      append([]byte(nil), prepared.Envelope.Nonce...),
		PayloadCiphertext: append([]byte(nil), prepared.Envelope.CiphertextAndTag...),
		PayloadDigest:     append([]byte(nil), prepared.PayloadDigest[:]...), Status: string(project.OutboxStatusPending),
		AvailableAt: command.OccurredAt, LeaseVersion: 0, AttemptCount: 0, MaxAttempts: command.MaxAttempts,
		CreatedAt: command.OccurredAt, UpdatedAt: command.OccurredAt,
		SkillSnapshotDigest: append([]byte(nil), prepared.SnapshotDigest[:]...), SkillCount: prepared.SkillCount,
		BindingSetVersion: &setVersion, ResolutionID: &resolutionID,
	}
	if err := tx.Create(&outbox).Error; err != nil {
		return fmt.Errorf("insert project session outbox v2: %w", err)
	}
	// Claim 时只使用不可观察的 32-byte placeholder；提交前必须在同一事务把回执替换为真实 Snapshot 摘要。
	updated := tx.Model(&projectCreationReceiptV2Model{}).
		Where("id = ? AND request_schema_version = ? AND semantic_digest = ?", command.ReceiptID, projectskillbinding.QuickCreateSchemaVersionV2, command.SemanticDigest[:]).
		Updates(map[string]any{
			"skill_snapshot_digest": prepared.SnapshotDigest[:], "skill_count": prepared.SkillCount,
			"binding_set_version": setVersion, "resolution_id": resolutionID,
		})
	if updated.Error != nil {
		return fmt.Errorf("freeze quick create v2 receipt: %w", updated.Error)
	}
	if updated.RowsAffected != 1 {
		return projectskillbinding.ErrSnapshotInvalid
	}
	return nil
}

// projectSkillBindingModelsFromCommand 构造初始 enabled Binding 与同批 append-only Audit；调用方使用两次固定 batch SQL 写入。
func projectSkillBindingModelsFromCommand(command projectskillbinding.QuickCreateV2Command) ([]projectSkillBindingModel, []projectSkillBindingAuditModel) {
	bindings := make([]projectSkillBindingModel, len(command.Bindings))
	audits := make([]projectSkillBindingAuditModel, len(command.Bindings))
	for index, seed := range command.Bindings {
		bindings[index] = projectSkillBindingModel{
			ID: seed.ID, ProjectID: command.ProjectID, SkillID: seed.SkillID,
			Namespace: projectskillbinding.SkillNamespaceUser, Priority: projectskillbinding.BindingPriorityW1,
			Status: projectskillbinding.BindingStatusEnabled, Source: projectskillbinding.BindingSourceQuickCreate,
			EnabledByUserID: command.OwnerUserID, EnabledAt: command.OccurredAt,
			Version: 1, CreatedAt: command.OccurredAt, UpdatedAt: command.OccurredAt,
		}
		audits[index] = projectSkillBindingAuditModel{
			ID: seed.AuditID, ProjectID: command.ProjectID, BindingID: seed.ID, SkillID: seed.SkillID,
			BindingSetVersion: 1, Action: "enabled", ToStatus: projectskillbinding.BindingStatusEnabled,
			Source: projectskillbinding.BindingSourceQuickCreate, ActorUserID: command.OwnerUserID,
			CommandReceiptID: command.ReceiptID, OccurredAt: command.OccurredAt,
		}
	}
	return bindings, audits
}

// projectSessionSkillResolutionModelFromEntity 显式映射不可变 Resolution Header。
func projectSessionSkillResolutionModelFromEntity(header projectskillbinding.ResolutionHeader) projectSessionSkillResolutionModel {
	return projectSessionSkillResolutionModel{
		ID: header.ID, CommandID: header.CommandID, ProjectID: header.ProjectID, OwnerUserID: header.OwnerUserID,
		BindingSetVersion: header.BindingSetVersion, BindingSelectionDigest: append([]byte(nil), header.BindingSelectionDigest[:]...),
		SnapshotSchemaVersion: header.SnapshotSchemaVersion, SnapshotKind: header.SnapshotKind,
		SkillCount: header.SkillCount, SnapshotSetDigest: append([]byte(nil), header.SnapshotSetDigest[:]...),
		RuntimePolicyRef: header.RuntimePolicyRef, ResolvedAt: header.ResolvedAt,
	}
}

// projectSessionSkillResolutionItemModels 将 metadata 编码为两个有界 JSONB 数组，不复制 Runtime Content 明文。
func projectSessionSkillResolutionItemModels(items []projectskillbinding.ResolutionItem) ([]projectSessionSkillResolutionItemModel, error) {
	models := make([]projectSessionSkillResolutionItemModel, len(items))
	for index, item := range items {
		allowedKeys, err := json.Marshal(item.AllowedGraphToolKeys)
		if err != nil {
			return nil, projectskillbinding.ErrSnapshotInvalid
		}
		publicRefs, err := json.Marshal(item.PublicToolRefs)
		if err != nil {
			return nil, projectskillbinding.ErrSnapshotInvalid
		}
		models[index] = projectSessionSkillResolutionItemModel{
			ResolutionID: item.ResolutionID, ProjectID: item.ProjectID, CommandID: item.CommandID,
			LoadOrder: item.LoadOrder, Priority: item.Priority, Namespace: item.Namespace,
			BindingID: item.BindingID, BindingVersion: item.BindingVersion, SkillID: item.SkillID,
			PublisherUserID: item.PublisherUserID, PublishedSnapshotID: item.PublishedSnapshotID,
			PublicationRevision: item.PublicationRevision, DefinitionSchemaVersion: item.DefinitionSchemaVersion,
			ContentDigest:               append([]byte(nil), item.ContentDigest[:]...),
			RuntimeContentSchemaVersion: item.RuntimeContentSchemaVersion,
			RuntimeContentDigest:        append([]byte(nil), item.RuntimeContentDigest[:]...),
			AllowedGraphToolKeys:        jsonbValue(allowedKeys), PublicToolRefs: jsonbValue(publicRefs),
			PermissionSnapshotDigest: append([]byte(nil), item.PermissionSnapshotDigest[:]...),
			RuntimePolicyRef:         item.RuntimePolicyRef, GovernanceEpoch: item.GovernanceEpoch,
			PublishedAtUnixMS: item.PublishedAtUnixMS, CreatedAt: item.CreatedAt,
		}
	}
	return models, nil
}

// projectCreationReceiptV2FromCommand 构造 Claim 行；Snapshot 摘要在解析后、事务提交前替换为真实冻结值。
func projectCreationReceiptV2FromCommand(command projectskillbinding.QuickCreateV2Command) projectCreationReceiptV2Model {
	setVersion := int64(1)
	resolutionID := command.ResolutionID
	return projectCreationReceiptV2Model{
		ID: command.ReceiptID, OwnerUserID: command.OwnerUserID, CommandType: project.QuickCreateCommandType,
		KeyDigest: append([]byte(nil), command.KeyDigest[:]...), SemanticDigest: append([]byte(nil), command.SemanticDigest[:]...),
		ProjectID: command.ProjectID, LifecycleStatus: string(project.LifecycleStatusActive),
		RecentRunStatus:           quickCreateRecentStatus(command.PromptPresent),
		SessionProvisioningStatus: string(project.ProvisioningStatusPending),
		InitialPromptStatus:       quickCreateInitialPromptStatus(command.PromptPresent), CreatedAt: command.OccurredAt,
		RequestSchemaVersion: projectskillbinding.QuickCreateSchemaVersionV2,
		// 先写 32-byte selection digest 仅为满足不可观察的事务内 CHECK；提交前必须原子替换为 Snapshot set digest。
		SkillSnapshotDigest: append([]byte(nil), command.SelectionDigest[:]...), SkillCount: len(command.Bindings),
		BindingSetVersion: &setVersion, ResolutionID: &resolutionID,
	}
}

// readProjectCreationReceiptV2 读取唯一幂等范围中的已提交回执；v1 行也会读出并由调用方稳定判定 version conflict。
func readProjectCreationReceiptV2(tx *gorm.DB, ownerUserID string, keyDigest projectskillbinding.Digest) (projectCreationReceiptV2Model, error) {
	var receipt projectCreationReceiptV2Model
	if err := tx.Where("owner_user_id = ? AND command_type = ? AND key_digest = ?", ownerUserID, project.QuickCreateCommandType, keyDigest[:]).Take(&receipt).Error; err != nil {
		return projectCreationReceiptV2Model{}, fmt.Errorf("read quick create v2 receipt: %w", err)
	}
	return receipt, nil
}

// projectSkillBindingResultFromReceipt 显式映射冻结回执；缺失 v2 audit 字段视为损坏数据并失败关闭。
func projectSkillBindingResultFromReceipt(receipt projectCreationReceiptV2Model, replay bool) (projectskillbinding.QuickCreateV2Result, error) {
	if receipt.RequestSchemaVersion != projectskillbinding.QuickCreateSchemaVersionV2 || receipt.BindingSetVersion == nil ||
		receipt.ResolutionID == nil || *receipt.BindingSetVersion < 1 || len(receipt.SkillSnapshotDigest) != 32 ||
		receipt.SkillCount < 0 || receipt.SkillCount > 32 {
		return projectskillbinding.QuickCreateV2Result{}, projectskillbinding.ErrSnapshotInvalid
	}
	digest, err := projectSkillBindingDigest(receipt.SkillSnapshotDigest)
	if err != nil {
		return projectskillbinding.QuickCreateV2Result{}, err
	}
	return projectskillbinding.QuickCreateV2Result{
		ProjectID: receipt.ProjectID, RequestSchemaVersion: receipt.RequestSchemaVersion,
		SnapshotDigest: digest, SkillCount: receipt.SkillCount, BindingSetVersion: *receipt.BindingSetVersion,
		ResolutionID: *receipt.ResolutionID, IdempotentReplay: replay,
	}, nil
}

// projectSkillBindingDigest 将数据库 bytea 严格恢复为固定 32-byte Digest。
func projectSkillBindingDigest(value []byte) (projectskillbinding.Digest, error) {
	var digest projectskillbinding.Digest
	if len(value) != len(digest) {
		return projectskillbinding.Digest{}, projectskillbinding.ErrSnapshotInvalid
	}
	copy(digest[:], value)
	return digest, nil
}

// quickCreateRecentStatus 返回与 W0 相同的首 Prompt 项目运行摘要，确保页面状态兼容。
func quickCreateRecentStatus(promptPresent bool) string {
	if promptPresent {
		return string(project.RecentRunStatusQueued)
	}
	return string(project.RecentRunStatusIdle)
}

// quickCreateInitialPromptStatus 返回与 W0 相同的首 Prompt 接受状态，v2 只改变 Snapshot/Outbox 版本。
func quickCreateInitialPromptStatus(promptPresent bool) string {
	if promptPresent {
		return string(project.InitialPromptStatusPending)
	}
	return string(project.InitialPromptStatusAbsent)
}

// mapProjectSkillBindingRepositoryError 保留稳定业务分类并收敛所有未分类数据库错误，禁止泄露 SQL 或连接信息。
func mapProjectSkillBindingRepositoryError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, project.ErrIdempotencyConflict):
		return err
	case errors.Is(err, projectskillbinding.ErrInvalidBinding):
		return err
	case errors.Is(err, projectskillbinding.ErrSkillUnavailable):
		return err
	case errors.Is(err, projectskillbinding.ErrGovernanceUnavailable):
		return err
	case errors.Is(err, projectskillbinding.ErrPublicToolUnavailable):
		return err
	case errors.Is(err, projectskillbinding.ErrSnapshotInvalid):
		return err
	case errors.Is(err, projectskillbinding.ErrSnapshotLimitExceeded):
		return err
	case errors.Is(err, projectskillbinding.ErrContentProtection):
		return err
	default:
		return project.ErrPersistence
	}
}
