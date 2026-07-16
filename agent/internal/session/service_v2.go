package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
)

// snapshotAuditReferenceV1 是 Header JSONB 保存的轻量审计投影，不包含 Runtime Content 明文。
type snapshotAuditReferenceV1 struct {
	// LoadOrder 是 Business 冻结的稠密加载顺序。
	LoadOrder int32 `json:"load_order"`
	// SkillID 是 Business Skill UUIDv7 逻辑引用。
	SkillID string `json:"skill_id"`
	// PublishedSnapshotID 是不可变 Published Snapshot UUIDv7 逻辑引用。
	PublishedSnapshotID string `json:"published_snapshot_id"`
	// PublicationRevision 是冻结发布修订号。
	PublicationRevision int64 `json:"publication_revision"`
}

// canonicalEnsureCommandV2 是完成 transport identity 与 skill canonical 验证后的内部命令。
type canonicalEnsureCommandV2 struct {
	// requestID 是规范本次传输追踪 UUIDv7。
	requestID string
	// commandID 是规范稳定命令 UUIDv7。
	commandID string
	// projectID 是规范 Business Project UUIDv7。
	projectID string
	// ownerUserID 是规范 Business Owner UUIDv7。
	ownerUserID string
	// normalizedPrompt 是 NFC Prompt；absent 时为空。
	normalizedPrompt string
	// promptPresent 表示规范化后存在非纯空白 Prompt。
	promptPresent bool
	// promptDigest 是规范化 Prompt 摘要；absent 时为空。
	promptDigest string
	// requestDigest 是 Agent 独立重算的 V2 语义摘要。
	requestDigest string
	// snapshot 是 Runtime/set digest 与 limits 全部验证后的不可变集合。
	snapshot skill.SessionSkillSnapshotV1
}

// EnsureProjectSessionV2 幂等创建携带 empty 或 published_refs Snapshot 的默认 Session。
// 同键重放先只读 Receipt；首次创建在事务前完成 canonical、limits、全部加密、ID/时间和 Event 准备，Repository 事务只写数据库。
func (s *Service) EnsureProjectSessionV2(ctx context.Context, command EnsureCommandV2) (EnsureResult, error) {
	canonical, err := s.canonicalizeEnsureCommandV2(command)
	if err != nil {
		return EnsureResult{}, err
	}

	// 重放预检必须早于随机 ID 与任何内容保护：已经提交的 V2 Receipt 不应被临时 key/熵源故障阻断。
	// 事务内仍会再次按 CommandID 加锁核对，避免并发双 miss 绕过 first-write-wins。
	preflight, err := s.repository.Query(ctx, QueryCommand{
		SchemaVersion: QueryCommandSchemaVersionV2, RequestID: canonical.requestID,
		CommandID: canonical.commandID, ExpectedRequestDigest: canonical.requestDigest,
		ExpectedCommandType: CommandTypeEnsureProjectSessionV2,
	})
	if err != nil {
		return EnsureResult{}, err
	}
	switch preflight.Status {
	case QueryCommandStatusCompleted:
		if preflight.Receipt == nil {
			return EnsureResult{}, ErrPersistence
		}
		result := *preflight.Receipt
		// Receipt 即使结构合法，也必须与本次已独立 canonical 的完整 V2 语义逐字段一致；
		// 否则数据库中被篡改成另一组合法 digest/count 的行会绕过事务内严格比较并返回错误冻结结果。
		if result.CommandID != canonical.commandID || result.ResultVersion != ResultVersionV2 ||
			result.SkillSnapshotDigest != canonical.snapshot.SnapshotSetDigest ||
			result.SkillCount != len(canonical.snapshot.Skills) {
			return EnsureResult{}, ErrSnapshotIntegrity
		}
		result.Disposition = EnsureDispositionReplayed
		return result, nil
	case QueryCommandStatusConflict:
		return EnsureResult{}, ErrCommandConflict
	case QueryCommandStatusNotFound:
		// 继续准备首次创建计划。
	default:
		return EnsureResult{}, ErrPersistence
	}

	// V2 binary 必须显式装配专用 Snapshot protector；即使当前集合为空，也不能借未完成装配降级走 V1。
	if s.skillSnapshotProtector == nil {
		return EnsureResult{}, ErrContentProtection
	}

	now := s.clock.Now().UTC()
	sessionID, err := s.newUUIDv7("session")
	if err != nil {
		return EnsureResult{}, err
	}

	var promptProtected ProtectedContent
	if canonical.promptPresent {
		promptProtected, err = s.protector.Protect(ctx, []byte(canonical.normalizedPrompt))
		if err != nil {
			return EnsureResult{}, mapContentProtectionError(err)
		}
		if promptProtected.KeyVersion == "" || ValidateEnvelopeV1(promptProtected.Ciphertext) != nil {
			return EnsureResult{}, ErrContentProtection
		}
	}

	items, err := s.prepareSkillSnapshotItems(ctx, sessionID, canonical.snapshot, now)
	if err != nil {
		return EnsureResult{}, err
	}
	auditJSON, err := encodeSnapshotAuditReferences(canonical.snapshot.Skills)
	if err != nil {
		return EnsureResult{}, ErrSnapshotIntegrity
	}

	plan := EnsurePlan{
		Session: Session{
			ID: sessionID, ProjectID: canonical.projectID, UserID: canonical.ownerUserID,
			Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		SkillSnapshot: SkillSnapshot{
			SessionID: sessionID, SchemaVersion: SkillSnapshotSchemaVersionV1,
			Kind: SkillSnapshotKind(canonical.snapshot.SnapshotKind), SkillCount: len(items),
			Digest: canonical.snapshot.SnapshotSetDigest, PublishedSnapshotRefsJSON: auditJSON, CreatedAt: now,
		},
		SkillSnapshotItems: items,
		SequenceCounter:    SequenceCounter{SessionID: sessionID, UpdatedAt: now},
		RuntimeLease:       RuntimeLease{SessionID: sessionID, FenceToken: 0, Version: 1, UpdatedAt: now},
		Receipt: CommandReceipt{
			CommandID: canonical.commandID, CommandType: CommandTypeEnsureProjectSessionV2,
			RequestDigest: canonical.requestDigest, SessionID: sessionID, ResultVersion: ResultVersionV2,
			SkillSnapshotDigest: canonical.snapshot.SnapshotSetDigest, SkillCount: len(items), CompletedAt: now,
		},
	}
	if canonical.promptPresent {
		if err := s.addInitialPromptToPlan(&plan, promptProtected, canonical.promptDigest, canonical.commandID, now); err != nil {
			return EnsureResult{}, err
		}
	}
	if err := s.addEnsureEventsToPlan(&plan, canonical.commandID, now); err != nil {
		return EnsureResult{}, err
	}
	return s.repository.Ensure(ctx, plan)
}

// canonicalizeEnsureCommandV2 严格校验 V2 传输字段，并独立重算 Prompt、Runtime、set 与 request digest。
func (s *Service) canonicalizeEnsureCommandV2(command EnsureCommandV2) (canonicalEnsureCommandV2, error) {
	requestID, err := normalizeUUIDv7(command.RequestID)
	if err != nil || requestID != command.RequestID {
		return canonicalEnsureCommandV2{}, ErrInvalidCommand
	}
	commandID, err := normalizeUUIDv7(command.CommandID)
	if err != nil || commandID != command.CommandID || command.RequestedAt.IsZero() {
		return canonicalEnsureCommandV2{}, ErrInvalidCommand
	}
	canonical, err := skill.CanonicalEnsureProjectSessionV2(skill.EnsureProjectSessionInputV2{
		SchemaVersion: command.SchemaVersion, ProjectID: command.ProjectID, OwnerUserID: command.OwnerUserID,
		CreationSource: command.CreationSource, InitialPrompt: command.InitialPrompt, SkillSnapshot: command.SkillSnapshot,
	}, s.skillSnapshotLimits)
	if err != nil {
		return canonicalEnsureCommandV2{}, mapSkillContractError(err)
	}
	if !validSHA256Hex(command.RequestDigest) || !sameDigest(command.RequestDigest, canonical.RequestDigest.Hex()) ||
		command.PromptDigest != canonical.PromptDigest {
		return canonicalEnsureCommandV2{}, ErrSnapshotIntegrity
	}
	return canonicalEnsureCommandV2{
		requestID: requestID, commandID: commandID, projectID: command.ProjectID, ownerUserID: command.OwnerUserID,
		normalizedPrompt: canonical.NormalizedPrompt, promptPresent: canonical.PromptPresent,
		promptDigest: canonical.PromptDigest, requestDigest: canonical.RequestDigest.Hex(), snapshot: canonical.SkillSnapshot,
	}, nil
}

// prepareSkillSnapshotItems 对全部 Runtime Content 再取得唯一 canonical bytes，并通过一次批量调用完成事务前加密。
// 返回顺序必须与 Business 冻结的稠密 load_order 一致；任何部分失败都不会形成可提交计划。
func (s *Service) prepareSkillSnapshotItems(
	ctx context.Context,
	sessionID string,
	snapshot skill.SessionSkillSnapshotV1,
	createdAt time.Time,
) ([]SkillSnapshotItem, error) {
	if len(snapshot.Skills) == 0 {
		return make([]SkillSnapshotItem, 0), nil
	}
	plaintexts := make([]SkillSnapshotPlaintext, len(snapshot.Skills))
	for index, item := range snapshot.Skills {
		_, canonical, digest, err := skill.CanonicalRuntimeContentV1(item.RuntimeContent, s.skillSnapshotLimits)
		if err != nil {
			return nil, mapSkillContractError(err)
		}
		if digest.Hex() != item.RuntimeContentDigest {
			return nil, ErrSnapshotIntegrity
		}
		plaintexts[index] = SkillSnapshotPlaintext{
			Identity: SkillSnapshotContentIdentity{
				SessionID: sessionID, SkillID: item.SkillID, PublishedSnapshotID: item.PublishedSnapshotID,
				RuntimeContentDigest: item.RuntimeContentDigest,
			},
			CanonicalBytes: canonical,
		}
	}
	protected, err := s.skillSnapshotProtector.ProtectBatch(ctx, plaintexts)
	if err != nil {
		return nil, mapContentProtectionError(err)
	}
	if len(protected) != len(snapshot.Skills) {
		return nil, ErrContentProtection
	}
	items := make([]SkillSnapshotItem, len(snapshot.Skills))
	for index, item := range snapshot.Skills {
		if protected[index].Identity != plaintexts[index].Identity || protected[index].Protected.KeyVersion == "" ||
			ValidateEnvelopeV1(protected[index].Protected.Ciphertext) != nil {
			return nil, ErrContentProtection
		}
		graphKeysJSON, marshalErr := json.Marshal(item.AllowedGraphToolKeys)
		if marshalErr != nil {
			return nil, ErrSnapshotIntegrity
		}
		publicRefsJSON, marshalErr := json.Marshal(item.PublicToolRefs)
		if marshalErr != nil {
			return nil, ErrSnapshotIntegrity
		}
		items[index] = SkillSnapshotItem{
			SessionID: sessionID, LoadOrder: int(item.LoadOrder), Priority: int(item.Priority),
			Namespace: string(item.Namespace), SkillID: item.SkillID, PublisherUserID: item.PublisherUserID,
			PublishedSnapshotID: item.PublishedSnapshotID, PublicationRevision: item.PublicationRevision,
			DefinitionSchemaVersion: item.DefinitionSchemaVersion, ContentDigest: item.ContentDigest,
			RuntimeContentSchemaVersion: item.RuntimeContentSchemaVersion,
			RuntimeContentDigest:        item.RuntimeContentDigest, RuntimeContent: protected[index].Protected,
			AllowedGraphToolKeysJSON: string(graphKeysJSON), PublicToolRefsJSON: string(publicRefsJSON),
			PermissionSnapshotDigest: item.PermissionSnapshotDigest, RuntimePolicyRef: item.RuntimePolicyRef,
			GovernanceEpoch: item.GovernanceEpoch, PublishedAtUnixMS: item.PublishedAtUnixMS, CreatedAt: createdAt,
		}
	}
	return items, nil
}

// addInitialPromptToPlan 为非空 V2 Prompt 生成首 Message/Input，全部 ID 和正文保护都发生在 Repository 事务外。
func (s *Service) addInitialPromptToPlan(
	plan *EnsurePlan,
	protected ProtectedContent,
	promptDigest string,
	commandID string,
	now time.Time,
) error {
	messageID, err := s.newUUIDv7("message")
	if err != nil {
		return err
	}
	inputID, err := s.newUUIDv7("input")
	if err != nil {
		return err
	}
	plan.SequenceCounter.LastMessageSeq = 1
	plan.SequenceCounter.LastInputEnqueueSeq = 1
	plan.Message = &Message{
		ID: messageID, SessionID: plan.Session.ID, Seq: 1, Role: MessageRoleUser,
		Content: protected, ContentDigest: promptDigest,
		SourceKind: event.SourceKindEnsureProjectSession, SourceID: commandID, CreatedAt: now,
	}
	plan.Input = &Input{
		ID: inputID, SessionID: plan.Session.ID, SourceType: InputSourceTypeUserMessage,
		SourceID: commandID, MessageID: messageID, Status: InputStatusPending,
		EnqueueSeq: 1, Attempts: 0, AvailableAt: now, FenceToken: 0, CreatedAt: now, UpdatedAt: now,
	}
	plan.Receipt.MessageID = stringPointer(messageID)
	plan.Receipt.InputID = stringPointer(inputID)
	return nil
}

// addEnsureEventsToPlan 构造不含 Prompt 或 Runtime Content 明文的固定 Event 投影。
func (s *Service) addEnsureEventsToPlan(plan *EnsurePlan, commandID string, now time.Time) error {
	createdEventID, err := s.newUUIDv7("session event")
	if err != nil {
		return err
	}
	createdEvent, err := event.NewSessionCreated(
		createdEventID, plan.Session.ID, plan.Session.ProjectID, string(StatusActive), commandID, 1, now,
	)
	if err != nil {
		return ErrInvalidCommand
	}
	plan.Events = []event.Record{createdEvent}
	if plan.Input != nil && plan.Message != nil {
		acceptedEventID, idErr := s.newUUIDv7("input event")
		if idErr != nil {
			return idErr
		}
		acceptedEvent, eventErr := event.NewSessionInputAccepted(
			acceptedEventID, plan.Session.ID, plan.Input.ID, plan.Message.ID, commandID,
			string(InputStatusPending), plan.Input.EnqueueSeq, now,
		)
		if eventErr != nil {
			return ErrInvalidCommand
		}
		plan.Events = append(plan.Events, acceptedEvent)
	}
	return nil
}

// LoadSessionSkillSnapshotV1 使用 Repository 至多两条 SQL 读取密文集合，批量解密并重验 runtime/set digest。
// 读取不回查 Business 当前绑定；任一 Item 损坏会使整个 Snapshot 失败，禁止静默跳过 Skill。
func (s *Service) LoadSessionSkillSnapshotV1(ctx context.Context, sessionID string) (LoadedSkillSnapshotV1, error) {
	normalizedSessionID, err := normalizeUUIDv7(sessionID)
	if err != nil || normalizedSessionID != sessionID {
		return LoadedSkillSnapshotV1{}, ErrInvalidCommand
	}
	stored, err := s.repository.LoadSkillSnapshot(ctx, sessionID, s.skillSnapshotLimits.MaxItems)
	if err != nil {
		return LoadedSkillSnapshotV1{}, err
	}
	if stored.Header.SessionID != sessionID || stored.Header.SchemaVersion != SkillSnapshotSchemaVersionV1 ||
		stored.Header.SkillCount != len(stored.Items) {
		return LoadedSkillSnapshotV1{}, ErrSnapshotIntegrity
	}
	if stored.Header.Kind == SkillSnapshotKindEmpty {
		if stored.Header.SkillCount != 0 || stored.Header.Digest != EmptySkillSnapshotDigest ||
			!isEmptyJSONArray(stored.Header.PublishedSnapshotRefsJSON) {
			return LoadedSkillSnapshotV1{}, ErrSnapshotIntegrity
		}
		return LoadedSkillSnapshotV1{SessionID: sessionID, Snapshot: skill.SessionSkillSnapshotV1{
			SchemaVersion: skill.SnapshotSchemaVersionV1, SnapshotKind: skill.SessionSkillSnapshotKindEmptyV1,
			SkillCount: 0, SnapshotSetDigest: skill.EmptySnapshotSetDigestHex,
			Skills: make([]skill.PublishedSkillSnapshotRefV1, 0),
		}}, nil
	}
	if stored.Header.Kind != SkillSnapshotKindPublishedRefs || len(stored.Items) == 0 || s.skillSnapshotProtector == nil {
		return LoadedSkillSnapshotV1{}, ErrContentUnavailable
	}

	ciphertexts := make([]SkillSnapshotCiphertext, len(stored.Items))
	for index, item := range stored.Items {
		if item.SessionID != sessionID || item.LoadOrder != index+1 {
			return LoadedSkillSnapshotV1{}, ErrSnapshotIntegrity
		}
		ciphertexts[index] = SkillSnapshotCiphertext{
			Identity: SkillSnapshotContentIdentity{
				SessionID: sessionID, SkillID: item.SkillID, PublishedSnapshotID: item.PublishedSnapshotID,
				RuntimeContentDigest: item.RuntimeContentDigest,
			},
			Protected: item.RuntimeContent,
		}
	}
	plaintexts, err := s.skillSnapshotProtector.OpenBatch(ctx, ciphertexts)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return LoadedSkillSnapshotV1{}, err
		}
		return LoadedSkillSnapshotV1{}, ErrContentUnavailable
	}
	if len(plaintexts) != len(stored.Items) {
		return LoadedSkillSnapshotV1{}, ErrContentUnavailable
	}

	snapshot := skill.SessionSkillSnapshotV1{
		SchemaVersion: skill.SnapshotSchemaVersionV1,
		SnapshotKind:  skill.SessionSkillSnapshotKindPublishedRefsV1,
		SkillCount:    int32(len(stored.Items)), SnapshotSetDigest: stored.Header.Digest,
		Skills: make([]skill.PublishedSkillSnapshotRefV1, len(stored.Items)),
	}
	for index, item := range stored.Items {
		runtimeContent, runtimeDigest, parseErr := skill.ParseCanonicalRuntimeContentV1(plaintexts[index], s.skillSnapshotLimits)
		if parseErr != nil || runtimeDigest.Hex() != item.RuntimeContentDigest {
			return LoadedSkillSnapshotV1{}, ErrSnapshotIntegrity
		}
		graphKeys, decodeErr := decodeStrictJSONArray[string](item.AllowedGraphToolKeysJSON)
		if decodeErr != nil {
			return LoadedSkillSnapshotV1{}, ErrSnapshotIntegrity
		}
		publicRefs, decodeErr := decodeStrictJSONArray[skill.PublicToolSnapshotRefV1](item.PublicToolRefsJSON)
		if decodeErr != nil || len(publicRefs) != 0 {
			return LoadedSkillSnapshotV1{}, ErrSnapshotIntegrity
		}
		snapshot.Skills[index] = skill.PublishedSkillSnapshotRefV1{
			LoadOrder: int32(item.LoadOrder), Priority: int32(item.Priority), Namespace: skill.SkillNamespaceV1(item.Namespace),
			SkillID: item.SkillID, PublisherUserID: item.PublisherUserID,
			PublishedSnapshotID: item.PublishedSnapshotID, PublicationRevision: item.PublicationRevision,
			DefinitionSchemaVersion: item.DefinitionSchemaVersion, ContentDigest: item.ContentDigest,
			RuntimeContentSchemaVersion: item.RuntimeContentSchemaVersion,
			RuntimeContentDigest:        item.RuntimeContentDigest, RuntimeContent: runtimeContent,
			AllowedGraphToolKeys: graphKeys, PublicToolRefs: publicRefs,
			PermissionSnapshotDigest: item.PermissionSnapshotDigest, RuntimePolicyRef: item.RuntimePolicyRef,
			GovernanceEpoch: item.GovernanceEpoch, PublishedAtUnixMS: item.PublishedAtUnixMS,
		}
	}
	normalized, _, digest, err := skill.CanonicalSnapshotSetV1(snapshot, s.skillSnapshotLimits)
	if err != nil || digest.Hex() != stored.Header.Digest {
		return LoadedSkillSnapshotV1{}, ErrSnapshotIntegrity
	}
	if err := verifySnapshotAuditReferences(stored.Header.PublishedSnapshotRefsJSON, normalized.Skills); err != nil {
		return LoadedSkillSnapshotV1{}, ErrSnapshotIntegrity
	}
	return LoadedSkillSnapshotV1{SessionID: sessionID, Snapshot: normalized}, nil
}

// encodeSnapshotAuditReferences 生成 Header 轻量审计 projection；空集合固定编码为 `[]`。
func encodeSnapshotAuditReferences(items []skill.PublishedSkillSnapshotRefV1) (string, error) {
	references := make([]snapshotAuditReferenceV1, len(items))
	for index, item := range items {
		references[index] = snapshotAuditReferenceV1{
			LoadOrder: item.LoadOrder, SkillID: item.SkillID, PublishedSnapshotID: item.PublishedSnapshotID,
			PublicationRevision: item.PublicationRevision,
		}
	}
	encoded, err := json.Marshal(references)
	return string(encoded), err
}

// verifySnapshotAuditReferences 以强类型语义核对 JSONB 投影，避免依赖 PostgreSQL JSONB 的键顺序或空格表现。
func verifySnapshotAuditReferences(encoded string, items []skill.PublishedSkillSnapshotRefV1) error {
	actual, err := decodeStrictJSONArray[snapshotAuditReferenceV1](encoded)
	if err != nil || len(actual) != len(items) {
		return ErrSnapshotIntegrity
	}
	for index, item := range items {
		expected := snapshotAuditReferenceV1{
			LoadOrder: item.LoadOrder, SkillID: item.SkillID, PublishedSnapshotID: item.PublishedSnapshotID,
			PublicationRevision: item.PublicationRevision,
		}
		if actual[index] != expected {
			return ErrSnapshotIntegrity
		}
	}
	return nil
}

// decodeStrictJSONArray 拒绝 unknown field、trailing bytes、null 和非数组 JSON，供持久化投影读取失败关闭。
func decodeStrictJSONArray[T any](encoded string) ([]T, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	var values []T
	if err := decoder.Decode(&values); err != nil || values == nil {
		return nil, ErrSnapshotIntegrity
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrSnapshotIntegrity
	}
	return values, nil
}

// isEmptyJSONArray 只接受语义为空的非 nil JSON 数组，兼容 PostgreSQL JSONB 输出空格但拒绝 null/object。
func isEmptyJSONArray(encoded string) bool {
	values, err := decodeStrictJSONArray[json.RawMessage](encoded)
	return err == nil && len(values) == 0
}

// mapSkillContractError 把 canonical 内部详情收敛为 Session 稳定错误，避免字段内容沿 RPC 错误链泄漏。
func mapSkillContractError(err error) error {
	switch {
	case errors.Is(err, skill.ErrLimitExceeded):
		return ErrSnapshotLimitExceeded
	case errors.Is(err, skill.ErrDigestMismatch):
		return ErrSnapshotIntegrity
	case errors.Is(err, skill.ErrInvalidContract):
		return ErrInvalidCommand
	default:
		return ErrInvalidCommand
	}
}
