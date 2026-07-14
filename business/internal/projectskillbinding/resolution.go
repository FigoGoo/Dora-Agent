package projectskillbinding

import (
	"bytes"
	"context"
	"errors"
	"sort"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
)

// ProjectRuntimeContentV1 从完成规范化的 Published Definition 投影运行时最小子集、声明键与 Canonical 摘要。
// 该投影不包含市场、分类、标签、价格、权限原文或可执行 Tool 注册信息。
func ProjectRuntimeContentV1(definition skill.SkillDefinitionV1) (SkillRuntimeContentV1, []string, []byte, Digest, error) {
	normalized, err := skill.NormalizeDefinitionV1(definition)
	if err != nil {
		if errors.Is(err, skill.ErrToolReferenceUnavailable) {
			return SkillRuntimeContentV1{}, nil, nil, Digest{}, ErrPublicToolUnavailable
		}
		return SkillRuntimeContentV1{}, nil, nil, Digest{}, ErrSnapshotInvalid
	}
	if normalized.PublicToolRefs == nil || len(normalized.PublicToolRefs) != 0 {
		return SkillRuntimeContentV1{}, nil, nil, Digest{}, ErrPublicToolUnavailable
	}
	content := SkillRuntimeContentV1{
		SchemaVersion: RuntimeContentSchemaVersionV1,
		Name:          normalized.Name, InputDescription: normalized.InputDescription,
		OutputDescription: normalized.OutputDescription, InvocationRules: normalized.InvocationRules,
		PlanCreationSpec: mapCapability(normalized.PlanCreationSpec), AnalyzeMaterials: mapCapability(normalized.AnalyzeMaterials),
		PlanStoryboard: mapCapability(normalized.PlanStoryboard), GenerateMedia: mapCapability(normalized.GenerateMedia),
		WritePrompts: mapCapability(normalized.WritePrompts), AssembleOutput: mapCapability(normalized.AssembleOutput),
		Examples:       make([]SkillExampleV1, len(normalized.Examples)),
		StarterPrompts: make([]string, len(normalized.StarterPrompts)),
	}
	copy(content.StarterPrompts, normalized.StarterPrompts)
	for index, example := range normalized.Examples {
		content.Examples[index] = SkillExampleV1{Input: example.Input, Output: example.Output}
	}
	keys := allowedGraphToolKeys(content)
	encoded, digest, err := CanonicalRuntimeContentV1(content)
	if err != nil {
		return SkillRuntimeContentV1{}, nil, nil, Digest{}, err
	}
	return content, keys, encoded, digest, nil
}

// ResolveProjectSkillSnapshotsV1 在 Repository 的同一事务集合查询后冻结精确 Published Snapshot、权限和 Runtime Content。
// Session Snapshot 结构继续为 v1；每个 Item 的权限按 Owner 关系严格选择 v1 owner-private 或 v2 public-market。
// 任一 Item 不可用都会使整个解析失败；只有原始 Binding Set 为空才能产生 empty Snapshot。
func ResolveProjectSkillSnapshotsV1(input ResolveInputV1, rows []PublishedSkillReadDTO, limits LimitsV1) (ResolutionV1, error) {
	if err := validateResolutionInput(input); err != nil {
		return ResolutionV1{}, err
	}
	if err := limits.Validate(); err != nil {
		return ResolutionV1{}, err
	}
	if rows == nil {
		return ResolutionV1{}, ErrSnapshotInvalid
	}
	if len(rows) > limits.MaxItems {
		return ResolutionV1{}, ErrSnapshotLimitExceeded
	}
	orderedRows := append([]PublishedSkillReadDTO(nil), rows...)
	sort.Slice(orderedRows, func(left, right int) bool { return orderedRows[left].SkillID < orderedRows[right].SkillID })
	selection := make([]BindingSelectionItemV1, len(orderedRows))
	for index, row := range orderedRows {
		if index > 0 && row.SkillID == orderedRows[index-1].SkillID {
			return ResolutionV1{}, ErrInvalidBinding
		}
		selection[index] = BindingSelectionItemV1{Priority: row.Priority, Namespace: row.Namespace, SkillID: row.SkillID}
	}
	_, actualSelectionDigest, err := CanonicalBindingSelectionV1(selection)
	if err != nil || actualSelectionDigest != input.BindingSelectionDigest {
		return ResolutionV1{}, ErrInvalidBinding
	}

	items := make([]ResolutionItem, 0, len(orderedRows))
	snapshotSkills := make([]PublishedSkillSnapshotRefV1, 0, len(orderedRows))
	totalRuntimeBytes := 0
	for index, row := range orderedRows {
		if err := validatePublishedSkillRead(input, row); err != nil {
			return ResolutionV1{}, err
		}
		definition, definitionDigest, err := skill.DefinitionFromCanonicalV1(row.DefinitionJSON)
		if err != nil {
			return ResolutionV1{}, wrapSnapshotError("decode published definition", ErrSnapshotInvalid)
		}
		var calculatedDefinitionDigest Digest
		copy(calculatedDefinitionDigest[:], definitionDigest[:])
		if calculatedDefinitionDigest != row.ContentDigest || row.ContentDigest != row.RevisionContentDigest ||
			!bytes.Equal(row.DefinitionJSON, row.RevisionDefinitionJSON) {
			return ResolutionV1{}, wrapSnapshotError("verify published definition digest", ErrSnapshotInvalid)
		}
		runtimeContent, allowedKeys, runtimeBytes, runtimeDigest, err := ProjectRuntimeContentV1(definition)
		if err != nil {
			return ResolutionV1{}, wrapSnapshotError("project runtime content", err)
		}
		if len(runtimeContent.Examples) > limits.MaxExamplesPerItem || len(runtimeContent.StarterPrompts) > limits.MaxStarterPromptsPerItem ||
			len(runtimeBytes) > limits.MaxRuntimeContentBytesPerItem {
			return ResolutionV1{}, ErrSnapshotLimitExceeded
		}
		totalRuntimeBytes += len(runtimeBytes)
		if totalRuntimeBytes > limits.MaxTotalRuntimeContentBytes {
			return ResolutionV1{}, ErrSnapshotLimitExceeded
		}
		permissionDigest, err := canonicalPermissionDigestForPublishedSkill(input, row)
		if err != nil {
			return ResolutionV1{}, wrapSnapshotError("canonical permission snapshot", err)
		}
		publishedAtUnixMS := row.PublishedAt.UTC().UnixMilli()
		wireItem := PublishedSkillSnapshotRefV1{
			LoadOrder: index + 1, Priority: BindingPriorityW1, Namespace: SkillNamespaceUser,
			SkillID: row.SkillID,
			// Publisher 永远是 Skill 权威 Owner；执行批准发布的 Reviewer 不进入权限或 Agent Snapshot。
			PublisherUserID:     row.SkillOwnerUserID,
			PublishedSnapshotID: row.PublishedSnapshotID, PublicationRevision: row.PublishedPublicationRevision,
			DefinitionSchemaVersion: row.DefinitionSchemaVersion, ContentDigest: row.ContentDigest.Hex(),
			RuntimeContentSchemaVersion: RuntimeContentSchemaVersionV1, RuntimeContentDigest: runtimeDigest.Hex(), RuntimeContent: runtimeContent,
			AllowedGraphToolKeys: append([]string(nil), allowedKeys...), PublicToolRefs: make([]PublicToolSnapshotRefV1, 0),
			PermissionSnapshotDigest: permissionDigest.Hex(), RuntimePolicyRef: RuntimePolicyRefV1,
			GovernanceEpoch: row.GovernanceEpoch, PublishedAtUnixMS: publishedAtUnixMS,
		}
		snapshotSkills = append(snapshotSkills, wireItem)
		items = append(items, ResolutionItem{
			ResolutionID: input.ResolutionID, ProjectID: input.ProjectID, CommandID: input.CommandID,
			LoadOrder: index + 1, Priority: BindingPriorityW1, Namespace: SkillNamespaceUser,
			BindingID: row.BindingID, BindingVersion: row.BindingVersion, SkillID: row.SkillID,
			PublisherUserID: row.SkillOwnerUserID, PublishedSnapshotID: row.PublishedSnapshotID,
			PublicationRevision: row.PublishedPublicationRevision, DefinitionSchemaVersion: row.DefinitionSchemaVersion,
			ContentDigest: row.ContentDigest, RuntimeContentSchemaVersion: RuntimeContentSchemaVersionV1,
			RuntimeContentDigest: runtimeDigest, AllowedGraphToolKeys: append([]string(nil), allowedKeys...),
			PublicToolRefs: make([]PublicToolSnapshotRefV1, 0), PermissionSnapshotDigest: permissionDigest,
			RuntimePolicyRef: RuntimePolicyRefV1, GovernanceEpoch: row.GovernanceEpoch,
			PublishedAtUnixMS: publishedAtUnixMS, CreatedAt: input.ResolvedAt.UTC(),
		})
	}
	metadataBytes, snapshotDigest, err := CanonicalSnapshotSetV1(snapshotSkills)
	if err != nil {
		return ResolutionV1{}, wrapSnapshotError("canonical snapshot set", err)
	}
	if len(metadataBytes) > limits.MaxSnapshotMetadataBytes {
		return ResolutionV1{}, ErrSnapshotLimitExceeded
	}
	kind := SnapshotKindEmpty
	if len(snapshotSkills) > 0 {
		kind = SnapshotKindPublishedRefs
	}
	snapshot := SessionSkillSnapshotV1{
		SchemaVersion: SessionSnapshotSchemaVersionV1, SnapshotKind: kind,
		SkillCount: len(snapshotSkills), SnapshotSetDigest: snapshotDigest.Hex(), Skills: snapshotSkills,
	}
	return ResolutionV1{
		Header: ResolutionHeader{
			ID: input.ResolutionID, CommandID: input.CommandID, ProjectID: input.ProjectID, OwnerUserID: input.OwnerUserID,
			BindingSetVersion: input.BindingSetVersion, BindingSelectionDigest: input.BindingSelectionDigest,
			SnapshotSchemaVersion: SessionSnapshotSchemaVersionV1, SnapshotKind: kind, SkillCount: len(snapshotSkills),
			SnapshotSetDigest: snapshotDigest, RuntimePolicyRef: RuntimePolicyRefV1, ResolvedAt: input.ResolvedAt.UTC(),
		},
		Items: items, Snapshot: snapshot,
	}, nil
}

// PrepareOutboxV2 从同一事务中冻结的 Resolution 构造 request/payload/AAD，完成限额校验并调用预加载本地保护器。
// 本方法不得记录或返回 plaintext；保护失败时调用方必须回滚 Project、Binding、Resolution 与 Receipt。
func PrepareOutboxV2(ctx context.Context, command QuickCreateV2Command, resolution ResolutionV1, limits LimitsV1, protector OutboxPayloadProtectorV2) (PreparedOutboxV2, error) {
	if protector == nil || resolution.Header.ID != command.ResolutionID || resolution.Header.CommandID != command.CommandID ||
		resolution.Header.ProjectID != command.ProjectID || resolution.Header.OwnerUserID != command.OwnerUserID ||
		resolution.Header.BindingSetVersion != 1 || resolution.Header.BindingSelectionDigest != command.SelectionDigest {
		return PreparedOutboxV2{}, ErrSnapshotInvalid
	}
	_, requestDigest, err := CanonicalEnsureRequestV2(command.ProjectID, command.OwnerUserID, command.PromptPresent, command.PromptDigest, resolution.Snapshot)
	if err != nil {
		return PreparedOutboxV2{}, err
	}
	promptDigestHex := ""
	if command.PromptPresent {
		promptDigestHex = command.PromptDigest.Hex()
	}
	payload := SessionBootstrapOutboxPayloadV2{
		SchemaVersion: OutboxPayloadSchemaVersionV2, CommandID: command.CommandID,
		ProjectID: command.ProjectID, OwnerUserID: command.OwnerUserID, CreationSource: project.QuickCreateCreationSource,
		PromptPresent: command.PromptPresent, InitialPrompt: command.NormalizedPrompt, PromptDigest: promptDigestHex,
		SkillSnapshot: resolution.Snapshot, AcceptedAtUnixMS: command.OccurredAt.UnixMilli(), RequestDigest: requestDigest.Hex(),
	}
	plaintext, payloadDigest, err := CanonicalOutboxPayloadV2(payload)
	if err != nil {
		return PreparedOutboxV2{}, err
	}
	if err := limits.Validate(); err != nil || len(plaintext) > limits.MaxOutboxPlaintextBytes {
		return PreparedOutboxV2{}, ErrSnapshotLimitExceeded
	}
	aad, err := CanonicalOutboxAADV2(OutboxAADV2{
		SchemaVersion: OutboxPayloadSchemaVersionV2, CommandID: command.CommandID,
		ProjectID: command.ProjectID, OwnerUserID: command.OwnerUserID,
		RequestDigest: requestDigest.Hex(), SkillSnapshotDigest: resolution.Header.SnapshotSetDigest.Hex(),
	})
	if err != nil {
		return PreparedOutboxV2{}, err
	}
	envelope, err := protector.Protect(ctx, plaintext, aad)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return PreparedOutboxV2{}, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return PreparedOutboxV2{}, context.DeadlineExceeded
		}
		return PreparedOutboxV2{}, ErrContentProtection
	}
	if err := validateEnvelopeV2(envelope); err != nil {
		return PreparedOutboxV2{}, err
	}
	return PreparedOutboxV2{
		RequestDigest: requestDigest, PayloadDigest: payloadDigest,
		SnapshotDigest: resolution.Header.SnapshotSetDigest, SkillCount: resolution.Header.SkillCount,
		Envelope: EncryptedEnvelopeV2{
			Algorithm: envelope.Algorithm, KeyVersion: envelope.KeyVersion,
			Nonce: append([]byte(nil), envelope.Nonce...), CiphertextAndTag: append([]byte(nil), envelope.CiphertextAndTag...),
		},
	}, nil
}

// validatePublishedSkillRead 核对一次集合查询行的 Project、Binding、Skill、Published Pointer 与来源 Revision 一致性。
func validatePublishedSkillRead(input ResolveInputV1, row PublishedSkillReadDTO) error {
	if row.ProjectID != input.ProjectID || row.ProjectOwnerUserID != input.OwnerUserID || row.ProjectLifecycleStatus != "active" ||
		!isCanonicalUUIDv7(row.BindingID) || row.BindingVersion < 1 || row.BindingStatus != BindingStatusEnabled ||
		row.Namespace != SkillNamespaceUser || row.Priority != BindingPriorityW1 || !isCanonicalUUIDv7(row.SkillID) {
		return ErrSkillUnavailable
	}
	if !isCanonicalUUIDv7(row.SkillOwnerUserID) || row.PublisherUserID != row.SkillOwnerUserID ||
		row.CurrentPublishedSnapshotID == "" ||
		row.CurrentPublishedSnapshotID != row.PublishedSnapshotID || row.SkillPublicationRevision != row.PublishedPublicationRevision ||
		row.PublishedSkillID != row.SkillID || row.RevisionSkillID != row.SkillID ||
		row.SourceContentRevisionID != row.RevisionID || row.PublishedPublicationRevision < 1 ||
		!isCanonicalUUIDv7(row.PublishedSnapshotID) || !isCanonicalUUIDv7(row.SourceContentRevisionID) {
		return ErrSkillUnavailable
	}
	if row.GovernanceStatus != "active" || row.GovernanceEpoch < 1 {
		return ErrGovernanceUnavailable
	}
	if row.DefinitionSchemaVersion != skill.DefinitionSchemaVersionV1 || row.RevisionDefinitionSchemaVersion != skill.DefinitionSchemaVersionV1 ||
		len(row.DefinitionJSON) == 0 || len(row.RevisionDefinitionJSON) == 0 || isZeroDigest(row.ContentDigest) ||
		isZeroDigest(row.RevisionContentDigest) || row.PublishedAt.IsZero() || row.PublishedAt.UnixMilli() <= 0 {
		return ErrSnapshotInvalid
	}
	return nil
}

// canonicalPermissionDigestForPublishedSkill 按冻结 Owner 关系构造唯一权限版本，禁止调用方覆盖 basis 或 policy。
func canonicalPermissionDigestForPublishedSkill(input ResolveInputV1, row PublishedSkillReadDTO) (Digest, error) {
	if row.SkillOwnerUserID == input.OwnerUserID {
		permission := PermissionSnapshotV1{
			SchemaVersion: PermissionSnapshotSchemaVersionV1, Decision: "allow", Basis: PermissionBasisOwnerPrivate,
			SubjectUserID: input.OwnerUserID, ProjectID: input.ProjectID, ProjectOwnerUserID: input.OwnerUserID,
			BindingID: row.BindingID, BindingVersion: row.BindingVersion, BindingSetVersion: input.BindingSetVersion,
			Namespace: SkillNamespaceUser, SkillID: row.SkillID, SkillOwnerUserID: row.SkillOwnerUserID,
			PublishedSnapshotID: row.PublishedSnapshotID, AllowedActions: []string{"session_snapshot"},
			PolicyRef: PermissionPolicyRefOwnerPrivateV1,
		}
		_, digest, err := CanonicalPermissionSnapshotV1(permission)
		return digest, err
	}
	permission := PermissionSnapshotV2{
		SchemaVersion: PermissionSnapshotSchemaVersionV2, Decision: "allow", Basis: PermissionBasisPublicMarket,
		SubjectUserID: input.OwnerUserID, ProjectID: input.ProjectID, ProjectOwnerUserID: input.OwnerUserID,
		BindingID: row.BindingID, BindingVersion: row.BindingVersion, BindingSetVersion: input.BindingSetVersion,
		Namespace: SkillNamespaceUser, SkillID: row.SkillID, SkillOwnerUserID: row.SkillOwnerUserID,
		PublishedSnapshotID: row.PublishedSnapshotID, AllowedActions: []string{"session_snapshot"},
		PolicyRef: PermissionPolicyRefPublicMarketV1,
	}
	_, digest, err := CanonicalPermissionSnapshotV2(permission)
	return digest, err
}

// mapCapability 显式映射 Business Published Definition 与跨 Module Runtime DTO，避免反射或 JSON 往返。
func mapCapability(value skill.CapabilityGuidanceV1) CapabilityGuidanceV1 {
	return CapabilityGuidanceV1{
		Applicability: value.Applicability, Guidance: value.Guidance, NotApplicableReason: value.NotApplicableReason,
	}
}

// allowedGraphToolKeys 按冻结产品顺序投影 enabled capability；该结果不证明任何 Tool 已注册或可调用。
func allowedGraphToolKeys(content SkillRuntimeContentV1) []string {
	ordered := []struct {
		key        string
		capability CapabilityGuidanceV1
	}{
		{"plan_creation_spec", content.PlanCreationSpec},
		{"analyze_materials", content.AnalyzeMaterials},
		{"plan_storyboard", content.PlanStoryboard},
		{"generate_media", content.GenerateMedia},
		{"write_prompts", content.WritePrompts},
		{"assemble_output", content.AssembleOutput},
	}
	keys := make([]string, 0, len(ordered))
	for _, entry := range ordered {
		if entry.capability.Applicability == "enabled" {
			keys = append(keys, entry.key)
		}
	}
	return keys
}
