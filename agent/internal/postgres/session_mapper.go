package postgres

import (
	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
)

// mapSessionModel 将无持久化标签的 Session Entity 显式映射为 GORM Model。
func mapSessionModel(entity session.Session) sessionModel {
	return sessionModel{
		ID: entity.ID, ProjectID: entity.ProjectID, UserID: entity.UserID,
		Status: string(entity.Status), Version: entity.Version,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt, ArchivedAt: entity.ArchivedAt,
	}
}

// mapSessionSkillSnapshotModel 将不可变 Skill Snapshot 显式映射为 GORM Model。
func mapSessionSkillSnapshotModel(entity session.SkillSnapshot) sessionSkillSnapshotModel {
	return sessionSkillSnapshotModel{
		SessionID: entity.SessionID, SchemaVersion: entity.SchemaVersion,
		SnapshotKind: string(entity.Kind), SkillCount: entity.SkillCount, SnapshotDigest: entity.Digest,
		PublishedSnapshotRefs: entity.PublishedSnapshotRefsJSON, CreatedAt: entity.CreatedAt,
	}
}

// mapSessionSkillSnapshotItemModels 把全部已加密 Item 映射为一次 batch insert 使用的 GORM Models。
// 该函数只复制 Envelope，不执行加密、解密、排序或数据库访问。
func mapSessionSkillSnapshotItemModels(items []session.SkillSnapshotItem) []sessionSkillSnapshotItemModel {
	models := make([]sessionSkillSnapshotItemModel, len(items))
	for index, item := range items {
		models[index] = sessionSkillSnapshotItemModel{
			SessionID: item.SessionID, LoadOrder: item.LoadOrder, Priority: item.Priority,
			Namespace: item.Namespace, SkillID: item.SkillID, PublisherUserID: item.PublisherUserID,
			PublishedSnapshotID: item.PublishedSnapshotID, PublicationRevision: item.PublicationRevision,
			DefinitionSchemaVersion: item.DefinitionSchemaVersion, ContentDigest: item.ContentDigest,
			RuntimeContentSchemaVersion: item.RuntimeContentSchemaVersion,
			RuntimeContentDigest:        item.RuntimeContentDigest,
			RuntimeContentCiphertext:    append([]byte(nil), item.RuntimeContent.Ciphertext...),
			RuntimeContentKeyVersion:    item.RuntimeContent.KeyVersion,
			AllowedGraphToolKeys:        item.AllowedGraphToolKeysJSON, PublicToolRefs: item.PublicToolRefsJSON,
			PermissionSnapshotDigest: item.PermissionSnapshotDigest, RuntimePolicyRef: item.RuntimePolicyRef,
			GovernanceEpoch: item.GovernanceEpoch, PublishedAtUnixMS: item.PublishedAtUnixMS,
			CreatedAt: item.CreatedAt,
		}
	}
	return models
}

// mapStoredSkillSnapshot 把 Repository 两条 SQL 的 Header/Item Models 显式映射为仍含密文的领域读取结果。
func mapStoredSkillSnapshot(header sessionSkillSnapshotModel, items []sessionSkillSnapshotItemModel) session.StoredSkillSnapshot {
	result := session.StoredSkillSnapshot{
		Header: session.SkillSnapshot{
			SessionID: header.SessionID, SchemaVersion: header.SchemaVersion,
			Kind: session.SkillSnapshotKind(header.SnapshotKind), SkillCount: header.SkillCount,
			Digest: header.SnapshotDigest, PublishedSnapshotRefsJSON: header.PublishedSnapshotRefs,
			CreatedAt: header.CreatedAt,
		},
		Items: make([]session.SkillSnapshotItem, len(items)),
	}
	for index, item := range items {
		result.Items[index] = session.SkillSnapshotItem{
			SessionID: item.SessionID, LoadOrder: item.LoadOrder, Priority: item.Priority,
			Namespace: item.Namespace, SkillID: item.SkillID, PublisherUserID: item.PublisherUserID,
			PublishedSnapshotID: item.PublishedSnapshotID, PublicationRevision: item.PublicationRevision,
			DefinitionSchemaVersion: item.DefinitionSchemaVersion, ContentDigest: item.ContentDigest,
			RuntimeContentSchemaVersion: item.RuntimeContentSchemaVersion,
			RuntimeContentDigest:        item.RuntimeContentDigest,
			RuntimeContent: session.ProtectedContent{
				Ciphertext: append([]byte(nil), item.RuntimeContentCiphertext...),
				KeyVersion: item.RuntimeContentKeyVersion,
			},
			AllowedGraphToolKeysJSON: item.AllowedGraphToolKeys, PublicToolRefsJSON: item.PublicToolRefs,
			PermissionSnapshotDigest: item.PermissionSnapshotDigest, RuntimePolicyRef: item.RuntimePolicyRef,
			GovernanceEpoch: item.GovernanceEpoch, PublishedAtUnixMS: item.PublishedAtUnixMS,
			CreatedAt: item.CreatedAt,
		}
	}
	return result
}

// mapSessionSequenceCounterModel 映射 Message/Input 初始单调序号事实。
func mapSessionSequenceCounterModel(entity session.SequenceCounter) sessionSequenceCounterModel {
	return sessionSequenceCounterModel{
		SessionID: entity.SessionID, LastMessageSeq: entity.LastMessageSeq,
		LastInputEnqueueSeq: entity.LastInputEnqueueSeq, UpdatedAt: entity.UpdatedAt,
	}
}

// mapSessionMessageModel 显式映射自描述 AEAD Envelope，禁止把明文或缺算法/Nonce/认证标签的裸密文引入持久化 Model。
func mapSessionMessageModel(entity session.Message) sessionMessageModel {
	return sessionMessageModel{
		ID: entity.ID, SessionID: entity.SessionID, MessageSeq: entity.Seq, Role: string(entity.Role),
		ContentCiphertext: append([]byte(nil), entity.Content.Ciphertext...), ContentKeyVersion: entity.Content.KeyVersion,
		ContentDigest: entity.ContentDigest, SourceKind: entity.SourceKind, SourceID: entity.SourceID,
		CreatedAt: entity.CreatedAt,
	}
}

// mapSessionInputModel 显式映射 Input 状态和可选 Lease 字段。
func mapSessionInputModel(entity session.Input) sessionInputModel {
	messageID := entity.MessageID
	return sessionInputModel{
		ID: entity.ID, SessionID: entity.SessionID, SourceType: string(entity.SourceType), SourceID: entity.SourceID,
		MessageID: &messageID, Status: string(entity.Status), EnqueueSeq: entity.EnqueueSeq, Attempts: entity.Attempts,
		AvailableAt: entity.AvailableAt, LeaseOwner: entity.LeaseOwner, LeaseUntil: entity.LeaseUntil,
		FenceToken: entity.FenceToken, CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

// mapSessionRuntimeLeaseModel 映射 Session Lane 初始空 Lease/Fence 事实。
func mapSessionRuntimeLeaseModel(entity session.RuntimeLease) sessionRuntimeLeaseModel {
	return sessionRuntimeLeaseModel{
		SessionID: entity.SessionID, LeaseOwner: entity.LeaseOwner, LeaseUntil: entity.LeaseUntil,
		FenceToken: entity.FenceToken, Version: entity.Version, UpdatedAt: entity.UpdatedAt,
	}
}

func mapUserMessageTurnModel(entity session.UserMessageTurn) userMessageTurnModel {
	return userMessageTurnModel{
		TurnID: entity.TurnID, InputID: entity.InputID, SessionID: entity.SessionID,
		MessageID: entity.MessageID, UserID: entity.UserID, ProjectID: entity.ProjectID,
		OutputID: entity.OutputID, ModelCallID: entity.ModelCallID,
		RecoveryEventID: entity.RecoveryEventID, TerminalEventID: entity.TerminalEventID,
		Status: entity.Status, Version: entity.Version, CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

func mapUserMessageContextModel(entity session.UserMessageContext) userMessageContextModel {
	return userMessageContextModel{
		TurnID: entity.TurnID, SchemaVersion: entity.SchemaVersion, SessionID: entity.SessionID,
		InputID: entity.InputID, MessageID: entity.MessageID, UserID: entity.UserID, ProjectID: entity.ProjectID,
		MessageCutoffSeq: entity.MessageCutoffSeq, MessageContentDigest: entity.MessageContentDigest,
		SkillSnapshotRef: entity.SkillSnapshotRef, SkillSnapshotDigest: entity.SkillSnapshotDigest,
		PromptRef: entity.PromptRef, PromptDigest: entity.PromptDigest,
		ToolRegistryRef: entity.ToolRegistryRef, ToolRegistryDigest: entity.ToolRegistryDigest,
		RuntimePolicyRef: entity.RuntimePolicyRef, RuntimePolicyDigest: entity.RuntimePolicyDigest,
		ModelRouteRef: entity.ModelRouteRef, ModelRouteDigest: entity.ModelRouteDigest,
		BudgetRef: entity.BudgetRef, BudgetDigest: entity.BudgetDigest,
		AccessScopeRef: entity.AccessScopeRef, AccessScopeDigest: entity.AccessScopeDigest,
		ContextDigest: entity.ContextDigest, CreatedAt: entity.CreatedAt,
	}
}

// mapSessionCommandReceiptModel 显式映射不包含 Prompt 正文的冻结命令回执。
func mapSessionCommandReceiptModel(entity session.CommandReceipt) sessionCommandReceiptModel {
	return sessionCommandReceiptModel{
		CommandID: entity.CommandID, CommandType: entity.CommandType, RequestDigest: entity.RequestDigest,
		SessionID: entity.SessionID, MessageID: cloneStringPointer(entity.MessageID), InputID: cloneStringPointer(entity.InputID),
		ResultVersion: entity.ResultVersion, SkillSnapshotDigest: entity.SkillSnapshotDigest,
		SkillCount: entity.SkillCount, CompletedAt: entity.CompletedAt,
	}
}

// mapSessionEventLogModels 批量分配连续 Seq 并映射 Event，整个过程不执行 SQL。
// Repository 随后使用一次批量 INSERT，避免事件数量增长导致同构 SQL 循环。
func mapSessionEventLogModels(records []event.Record) []sessionEventLogModel {
	models := make([]sessionEventLogModel, len(records))
	for index, record := range records {
		models[index] = sessionEventLogModel{
			EventID: record.EventID, SessionID: record.SessionID, Seq: int64(index + 1),
			EventType: string(record.Type), SchemaVersion: record.SchemaVersion,
			SourceKind: record.SourceKind, SourceID: record.SourceID, ProjectionIndex: record.ProjectionIndex,
			AggregateType: string(record.AggregateType), AggregateID: record.AggregateID,
			AggregateVersion: record.AggregateVersion, Payload: string(record.PayloadJSON), CreatedAt: record.CreatedAt,
		}
	}
	return models
}

// mapReceiptResult 把持久化 Receipt 映射为对 RPC Mapper 安全的重放结果 DTO。
func mapReceiptResult(model sessionCommandReceiptModel, disposition session.EnsureDisposition) session.EnsureResult {
	return session.EnsureResult{
		CommandID: model.CommandID, SessionID: model.SessionID,
		MessageID: cloneStringPointer(model.MessageID), InputID: cloneStringPointer(model.InputID),
		Disposition: disposition, ResultVersion: model.ResultVersion,
		SkillSnapshotDigest: model.SkillSnapshotDigest, SkillCount: model.SkillCount, AcceptedAt: model.CompletedAt,
	}
}

// cloneStringPointer 复制可选标识，避免领域 DTO 与 GORM 扫描结构共享可变地址。
func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
