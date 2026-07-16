package projectskillbinding

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
)

// snapshotMetadataV1 是 Snapshot set digest 的固定字段顺序输入；它刻意不包含 Runtime Content 明文。
type snapshotMetadataV1 struct {
	// LoadOrder 是稠密加载顺序。
	LoadOrder int `json:"load_order"`
	// Priority 是冻结加载优先级。
	Priority int `json:"priority"`
	// Namespace 是冻结 Skill namespace。
	Namespace string `json:"namespace"`
	// SkillID 是 Business Skill 标识。
	SkillID string `json:"skill_id"`
	// PublisherUserID 是冻结 Skill Owner，允许与 Project Owner 不同。
	PublisherUserID string `json:"publisher_user_id"`
	// PublishedSnapshotID 是不可变发布快照标识。
	PublishedSnapshotID string `json:"published_snapshot_id"`
	// PublicationRevision 是发布修订序号。
	PublicationRevision int64 `json:"publication_revision"`
	// DefinitionSchemaVersion 是完整定义版本。
	DefinitionSchemaVersion string `json:"definition_schema_version"`
	// ContentDigest 是完整定义摘要。
	ContentDigest string `json:"content_digest"`
	// RuntimeContentSchemaVersion 是运行时投影版本。
	RuntimeContentSchemaVersion string `json:"runtime_content_schema_version"`
	// RuntimeContentDigest 是运行时投影摘要。
	RuntimeContentDigest string `json:"runtime_content_digest"`
	// AllowedGraphToolKeys 是固定六能力声明键数组。
	AllowedGraphToolKeys []string `json:"allowed_graph_tool_keys"`
	// PublicToolRefs 是 W1 固定空数组。
	PublicToolRefs []PublicToolSnapshotRefV1 `json:"public_tool_refs"`
	// PermissionSnapshotDigest 是 v1 owner-private 或 v2 public-market 权限摘要。
	PermissionSnapshotDigest string `json:"permission_snapshot_digest"`
	// RuntimePolicyRef 是固定运行时安全策略引用。
	RuntimePolicyRef string `json:"runtime_policy_ref"`
	// GovernanceEpoch 是解析时治理纪元。
	GovernanceEpoch int64 `json:"governance_epoch"`
	// PublishedAtUnixMS 是发布 Unix 毫秒。
	PublishedAtUnixMS int64 `json:"published_at_unix_ms"`
}

// quickCreateSemanticV2 是 QuickCreate v2 幂等语义的固定字段顺序输入。
type quickCreateSemanticV2 struct {
	// SchemaVersion 是 QuickCreate v2 语义版本。
	SchemaVersion string `json:"schema_version"`
	// PromptPresent 表示是否存在非空首 Prompt。
	PromptPresent bool `json:"prompt_present"`
	// PromptDigest 是首 Prompt 摘要或空字符串。
	PromptDigest string `json:"prompt_digest"`
	// BindingSetSchemaVersion 是绑定集合版本。
	BindingSetSchemaVersion string `json:"binding_set_schema_version"`
	// BindingSetDigest 是绑定集合摘要。
	BindingSetDigest string `json:"binding_set_digest"`
}

// ensureRequestSemanticV2 是 EnsureProjectSessionV2 request digest 的固定字段顺序输入。
type ensureRequestSemanticV2 struct {
	// SchemaVersion 是 EnsureProjectSessionV2 语义版本。
	SchemaVersion string `json:"schema_version"`
	// ProjectID 是 Business Project 标识。
	ProjectID string `json:"project_id"`
	// OwnerUserID 是冻结 Owner 标识。
	OwnerUserID string `json:"owner_user_id"`
	// CreationSource 是固定 quick_create 来源。
	CreationSource string `json:"creation_source"`
	// PromptPresent 表示是否存在非空首 Prompt。
	PromptPresent bool `json:"prompt_present"`
	// PromptDigest 是首 Prompt 摘要或空字符串。
	PromptDigest string `json:"prompt_digest"`
	// SkillSnapshotSchemaVersion 是 Session Snapshot 版本。
	SkillSnapshotSchemaVersion string `json:"skill_snapshot_schema_version"`
	// SkillSnapshotKind 是 empty 或 published_refs。
	SkillSnapshotKind string `json:"skill_snapshot_kind"`
	// SkillCount 是冻结 Skill 数量。
	SkillCount int `json:"skill_count"`
	// SkillSnapshotDigest 是 metadata 集合摘要。
	SkillSnapshotDigest string `json:"skill_snapshot_digest"`
}

// CanonicalBindingSelectionV1 验证并编码按 priority、namespace、skill_id 稳定排序的绑定集合。
func CanonicalBindingSelectionV1(items []BindingSelectionItemV1) ([]byte, Digest, error) {
	if items == nil {
		return nil, Digest{}, ErrInvalidBinding
	}
	previousSkillID := ""
	for _, item := range items {
		if item.Priority != BindingPriorityW1 || item.Namespace != SkillNamespaceUser || !isCanonicalUUIDv7(item.SkillID) ||
			(previousSkillID != "" && item.SkillID <= previousSkillID) {
			return nil, Digest{}, ErrInvalidBinding
		}
		previousSkillID = item.SkillID
	}
	encoded, err := canonicalJSON(items)
	if err != nil {
		return nil, Digest{}, wrapSnapshotError("encode binding selection", err)
	}
	return encoded, SHA256Digest(encoded), nil
}

// CanonicalPermissionSnapshotV1 验证 owner-private 固定字段并返回 Canonical bytes 与摘要。
func CanonicalPermissionSnapshotV1(permission PermissionSnapshotV1) ([]byte, Digest, error) {
	if permission.SchemaVersion != PermissionSnapshotSchemaVersionV1 || permission.Decision != "allow" ||
		permission.Basis != PermissionBasisOwnerPrivate || permission.PolicyRef != PermissionPolicyRefOwnerPrivateV1 ||
		validatePermissionSnapshotCommon(permission.SubjectUserID, permission.ProjectID, permission.ProjectOwnerUserID,
			permission.BindingID, permission.BindingVersion, permission.BindingSetVersion, permission.Namespace,
			permission.SkillID, permission.SkillOwnerUserID, permission.PublishedSnapshotID, permission.AllowedActions) != nil {
		return nil, Digest{}, ErrSnapshotInvalid
	}
	if permission.SubjectUserID != permission.ProjectOwnerUserID || permission.SkillOwnerUserID != permission.ProjectOwnerUserID {
		return nil, Digest{}, ErrSkillUnavailable
	}
	encoded, err := canonicalJSON(permission)
	if err != nil {
		return nil, Digest{}, wrapSnapshotError("encode permission snapshot", err)
	}
	return encoded, SHA256Digest(encoded), nil
}

// CanonicalPermissionSnapshotV2 验证 public-market 固定字段与跨 Owner 关系后返回 Canonical bytes 与摘要。
func CanonicalPermissionSnapshotV2(permission PermissionSnapshotV2) ([]byte, Digest, error) {
	if permission.SchemaVersion != PermissionSnapshotSchemaVersionV2 || permission.Decision != "allow" ||
		permission.Basis != PermissionBasisPublicMarket || permission.PolicyRef != PermissionPolicyRefPublicMarketV1 ||
		validatePermissionSnapshotCommon(permission.SubjectUserID, permission.ProjectID, permission.ProjectOwnerUserID,
			permission.BindingID, permission.BindingVersion, permission.BindingSetVersion, permission.Namespace,
			permission.SkillID, permission.SkillOwnerUserID, permission.PublishedSnapshotID, permission.AllowedActions) != nil {
		return nil, Digest{}, ErrSnapshotInvalid
	}
	if permission.SubjectUserID != permission.ProjectOwnerUserID || permission.SkillOwnerUserID == permission.ProjectOwnerUserID {
		return nil, Digest{}, ErrSkillUnavailable
	}
	encoded, err := canonicalJSON(permission)
	if err != nil {
		return nil, Digest{}, wrapSnapshotError("encode public market permission snapshot", err)
	}
	return encoded, SHA256Digest(encoded), nil
}

// ReconstructResolutionPermissionAudit 从不可变 Header/Item 唯一重建权限版本并核对持久化摘要。
// 它不读取当前 Skill Owner、治理或发布指针，因此历史审计不会被后续业务状态改写。
func ReconstructResolutionPermissionAudit(header ResolutionHeader, item ResolutionItem) (ReconstructedPermissionAudit, error) {
	if header.ID != item.ResolutionID || header.CommandID != item.CommandID || header.ProjectID != item.ProjectID ||
		header.BindingSetVersion < 1 || item.BindingVersion < 1 || item.LoadOrder < 1 ||
		item.Priority != BindingPriorityW1 || item.Namespace != SkillNamespaceUser ||
		!isCanonicalUUIDv7(header.ID) || !isCanonicalUUIDv7(header.CommandID) ||
		!isCanonicalUUIDv7(header.OwnerUserID) || isZeroDigest(item.PermissionSnapshotDigest) {
		return ReconstructedPermissionAudit{}, ErrSnapshotInvalid
	}
	commonActions := []string{"session_snapshot"}
	if header.OwnerUserID == item.PublisherUserID {
		permission := PermissionSnapshotV1{
			SchemaVersion: PermissionSnapshotSchemaVersionV1, Decision: "allow", Basis: PermissionBasisOwnerPrivate,
			SubjectUserID: header.OwnerUserID, ProjectID: header.ProjectID, ProjectOwnerUserID: header.OwnerUserID,
			BindingID: item.BindingID, BindingVersion: item.BindingVersion, BindingSetVersion: header.BindingSetVersion,
			Namespace: item.Namespace, SkillID: item.SkillID, SkillOwnerUserID: item.PublisherUserID,
			PublishedSnapshotID: item.PublishedSnapshotID, AllowedActions: commonActions,
			PolicyRef: PermissionPolicyRefOwnerPrivateV1,
		}
		encoded, digest, err := CanonicalPermissionSnapshotV1(permission)
		if err != nil || digest != item.PermissionSnapshotDigest {
			return ReconstructedPermissionAudit{}, ErrSnapshotInvalid
		}
		return ReconstructedPermissionAudit{
			SchemaVersion: permission.SchemaVersion, Basis: permission.Basis, PolicyRef: permission.PolicyRef,
			CanonicalJSON: encoded, Digest: digest,
		}, nil
	}
	permission := PermissionSnapshotV2{
		SchemaVersion: PermissionSnapshotSchemaVersionV2, Decision: "allow", Basis: PermissionBasisPublicMarket,
		SubjectUserID: header.OwnerUserID, ProjectID: header.ProjectID, ProjectOwnerUserID: header.OwnerUserID,
		BindingID: item.BindingID, BindingVersion: item.BindingVersion, BindingSetVersion: header.BindingSetVersion,
		Namespace: item.Namespace, SkillID: item.SkillID, SkillOwnerUserID: item.PublisherUserID,
		PublishedSnapshotID: item.PublishedSnapshotID, AllowedActions: commonActions,
		PolicyRef: PermissionPolicyRefPublicMarketV1,
	}
	encoded, digest, err := CanonicalPermissionSnapshotV2(permission)
	if err != nil || digest != item.PermissionSnapshotDigest {
		return ReconstructedPermissionAudit{}, ErrSnapshotInvalid
	}
	return ReconstructedPermissionAudit{
		SchemaVersion: permission.SchemaVersion, Basis: permission.Basis, PolicyRef: permission.PolicyRef,
		CanonicalJSON: encoded, Digest: digest,
	}, nil
}

// validatePermissionSnapshotCommon 校验两个权限版本共享的 UUID、版本、namespace 与 action exact-set。
func validatePermissionSnapshotCommon(
	subjectUserID string,
	projectID string,
	projectOwnerUserID string,
	bindingID string,
	bindingVersion int64,
	bindingSetVersion int64,
	namespace string,
	skillID string,
	skillOwnerUserID string,
	publishedSnapshotID string,
	allowedActions []string,
) error {
	if namespace != SkillNamespaceUser || len(allowedActions) != 1 || allowedActions[0] != "session_snapshot" ||
		bindingVersion < 1 || bindingSetVersion < 1 {
		return ErrSnapshotInvalid
	}
	ids := []string{subjectUserID, projectID, projectOwnerUserID, bindingID, skillID, skillOwnerUserID, publishedSnapshotID}
	for _, id := range ids {
		if !isCanonicalUUIDv7(id) {
			return ErrSnapshotInvalid
		}
	}
	return nil
}

// CanonicalRuntimeContentV1 严格验证 capability 互斥、非 nil 数组和稳定排序后返回运行时内容摘要。
func CanonicalRuntimeContentV1(content SkillRuntimeContentV1) ([]byte, Digest, error) {
	if content.SchemaVersion != RuntimeContentSchemaVersionV1 || content.Name == "" || content.Examples == nil || content.StarterPrompts == nil {
		return nil, Digest{}, wrapSnapshotError("runtime content header or arrays", ErrSnapshotInvalid)
	}
	capabilities := []CapabilityGuidanceV1{content.PlanCreationSpec, content.AnalyzeMaterials, content.PlanStoryboard, content.GenerateMedia, content.WritePrompts, content.AssembleOutput}
	for _, capability := range capabilities {
		if (capability.Applicability == "enabled" && capability.Guidance != "" && capability.NotApplicableReason == "") ||
			(capability.Applicability == "not_applicable" && capability.Guidance == "" && capability.NotApplicableReason != "") {
			continue
		}
		return nil, Digest{}, wrapSnapshotError("runtime content capability", ErrSnapshotInvalid)
	}
	for index := 1; index < len(content.Examples); index++ {
		previous, current := content.Examples[index-1], content.Examples[index]
		if previous.Input > current.Input || (previous.Input == current.Input && previous.Output >= current.Output) {
			return nil, Digest{}, wrapSnapshotError("runtime content example order", ErrSnapshotInvalid)
		}
	}
	for index := 1; index < len(content.StarterPrompts); index++ {
		if content.StarterPrompts[index-1] >= content.StarterPrompts[index] {
			return nil, Digest{}, wrapSnapshotError("runtime content starter order", ErrSnapshotInvalid)
		}
	}
	encoded, err := canonicalJSON(content)
	if err != nil {
		return nil, Digest{}, wrapSnapshotError("encode runtime content", err)
	}
	return encoded, SHA256Digest(encoded), nil
}

// CanonicalSnapshotSetV1 验证 wire 顺序、声明键、空公共引用和 metadata 后计算不含 Runtime 明文的集合摘要。
func CanonicalSnapshotSetV1(skills []PublishedSkillSnapshotRefV1) ([]byte, Digest, error) {
	if skills == nil {
		return nil, Digest{}, ErrSnapshotInvalid
	}
	metadata := make([]snapshotMetadataV1, len(skills))
	for index, item := range skills {
		if item.LoadOrder != index+1 || item.Priority != BindingPriorityW1 || item.Namespace != SkillNamespaceUser ||
			!isCanonicalUUIDv7(item.SkillID) || !isCanonicalUUIDv7(item.PublisherUserID) || !isCanonicalUUIDv7(item.PublishedSnapshotID) ||
			item.PublicationRevision < 1 || item.DefinitionSchemaVersion != "skill_definition.v1" ||
			item.RuntimeContentSchemaVersion != RuntimeContentSchemaVersionV1 || item.RuntimePolicyRef != RuntimePolicyRefV1 ||
			item.GovernanceEpoch < 1 || item.PublishedAtUnixMS <= 0 || item.AllowedGraphToolKeys == nil || item.PublicToolRefs == nil || len(item.PublicToolRefs) != 0 {
			return nil, Digest{}, ErrSnapshotInvalid
		}
		if _, err := DigestFromHex(item.ContentDigest); err != nil {
			return nil, Digest{}, err
		}
		if _, err := DigestFromHex(item.RuntimeContentDigest); err != nil {
			return nil, Digest{}, err
		}
		if _, err := DigestFromHex(item.PermissionSnapshotDigest); err != nil {
			return nil, Digest{}, err
		}
		if err := validateAllowedGraphToolKeys(item.AllowedGraphToolKeys, item.RuntimeContent); err != nil {
			return nil, Digest{}, err
		}
		metadata[index] = snapshotMetadataV1{
			LoadOrder: item.LoadOrder, Priority: item.Priority, Namespace: item.Namespace, SkillID: item.SkillID,
			PublisherUserID: item.PublisherUserID, PublishedSnapshotID: item.PublishedSnapshotID,
			PublicationRevision: item.PublicationRevision, DefinitionSchemaVersion: item.DefinitionSchemaVersion,
			ContentDigest: item.ContentDigest, RuntimeContentSchemaVersion: item.RuntimeContentSchemaVersion,
			RuntimeContentDigest: item.RuntimeContentDigest, AllowedGraphToolKeys: append([]string(nil), item.AllowedGraphToolKeys...),
			PublicToolRefs: make([]PublicToolSnapshotRefV1, 0), PermissionSnapshotDigest: item.PermissionSnapshotDigest,
			RuntimePolicyRef: item.RuntimePolicyRef, GovernanceEpoch: item.GovernanceEpoch, PublishedAtUnixMS: item.PublishedAtUnixMS,
		}
	}
	encoded, err := canonicalJSON(metadata)
	if err != nil {
		return nil, Digest{}, wrapSnapshotError("encode snapshot set", err)
	}
	return encoded, SHA256Digest(encoded), nil
}

// CanonicalQuickCreateSemanticV2 计算 Prompt 语义与 Binding Set 共同组成的幂等摘要。
func CanonicalQuickCreateSemanticV2(promptPresent bool, promptDigest Digest, bindingSetDigest Digest) ([]byte, Digest, error) {
	if promptPresent == isZeroDigest(promptDigest) || isZeroDigest(bindingSetDigest) {
		return nil, Digest{}, ErrInvalidBinding
	}
	promptDigestHex := ""
	if promptPresent {
		promptDigestHex = promptDigest.Hex()
	}
	semantic := quickCreateSemanticV2{
		SchemaVersion: QuickCreateSchemaVersionV2, PromptPresent: promptPresent, PromptDigest: promptDigestHex,
		BindingSetSchemaVersion: BindingSetSchemaVersionV1, BindingSetDigest: bindingSetDigest.Hex(),
	}
	encoded, err := canonicalJSON(semantic)
	if err != nil {
		return nil, Digest{}, wrapSnapshotError("encode quick create v2 semantic", err)
	}
	return encoded, SHA256Digest(encoded), nil
}

// CanonicalEnsureRequestV2 计算 Agent EnsureProjectSessionV2 的业务语义摘要，不包含 command、request ID 或时间。
func CanonicalEnsureRequestV2(projectID string, ownerUserID string, promptPresent bool, promptDigest Digest, snapshot SessionSkillSnapshotV1) ([]byte, Digest, error) {
	if !isCanonicalUUIDv7(projectID) || !isCanonicalUUIDv7(ownerUserID) || promptPresent == isZeroDigest(promptDigest) ||
		snapshot.SchemaVersion != SessionSnapshotSchemaVersionV1 || snapshot.Skills == nil || snapshot.SkillCount != len(snapshot.Skills) {
		return nil, Digest{}, ErrSnapshotInvalid
	}
	setDigest, err := DigestFromHex(snapshot.SnapshotSetDigest)
	if err != nil {
		return nil, Digest{}, err
	}
	_, actualSetDigest, err := CanonicalSnapshotSetV1(snapshot.Skills)
	if err != nil || actualSetDigest != setDigest {
		return nil, Digest{}, ErrSnapshotInvalid
	}
	if (snapshot.SkillCount == 0 && snapshot.SnapshotKind != SnapshotKindEmpty) ||
		(snapshot.SkillCount > 0 && snapshot.SnapshotKind != SnapshotKindPublishedRefs) {
		return nil, Digest{}, ErrSnapshotInvalid
	}
	promptDigestHex := ""
	if promptPresent {
		promptDigestHex = promptDigest.Hex()
	}
	semantic := ensureRequestSemanticV2{
		SchemaVersion: EnsureProjectSessionSchemaVersionV2, ProjectID: projectID, OwnerUserID: ownerUserID,
		CreationSource: project.QuickCreateCreationSource, PromptPresent: promptPresent, PromptDigest: promptDigestHex,
		SkillSnapshotSchemaVersion: snapshot.SchemaVersion, SkillSnapshotKind: snapshot.SnapshotKind,
		SkillCount: snapshot.SkillCount, SkillSnapshotDigest: snapshot.SnapshotSetDigest,
	}
	encoded, err := canonicalJSON(semantic)
	if err != nil {
		return nil, Digest{}, wrapSnapshotError("encode ensure request v2", err)
	}
	return encoded, SHA256Digest(encoded), nil
}

// CanonicalOutboxPayloadV2 编码完整加密 plaintext 并计算损坏检测摘要；任何正文不得写入日志或数据库明文列。
func CanonicalOutboxPayloadV2(payload SessionBootstrapOutboxPayloadV2) ([]byte, Digest, error) {
	if payload.SchemaVersion != OutboxPayloadSchemaVersionV2 || !isCanonicalUUIDv7(payload.CommandID) ||
		!isCanonicalUUIDv7(payload.ProjectID) || !isCanonicalUUIDv7(payload.OwnerUserID) ||
		payload.CreationSource != project.QuickCreateCreationSource || payload.AcceptedAtUnixMS <= 0 {
		return nil, Digest{}, ErrSnapshotInvalid
	}
	if payload.PromptPresent != (payload.InitialPrompt != "") || payload.PromptPresent != (payload.PromptDigest != "") {
		return nil, Digest{}, ErrSnapshotInvalid
	}
	requestDigest, err := DigestFromHex(payload.RequestDigest)
	if err != nil || isZeroDigest(requestDigest) {
		return nil, Digest{}, ErrSnapshotInvalid
	}
	encoded, err := canonicalJSON(payload)
	if err != nil {
		return nil, Digest{}, wrapSnapshotError("encode outbox v2 payload", err)
	}
	return encoded, SHA256Digest(encoded), nil
}

// CanonicalOutboxAADV2 编码 domain-separated AAD；字段与 envelope metadata 不匹配时解密必须失败。
func CanonicalOutboxAADV2(aad OutboxAADV2) ([]byte, error) {
	if aad.SchemaVersion != OutboxPayloadSchemaVersionV2 || !isCanonicalUUIDv7(aad.CommandID) ||
		!isCanonicalUUIDv7(aad.ProjectID) || !isCanonicalUUIDv7(aad.OwnerUserID) {
		return nil, ErrSnapshotInvalid
	}
	if _, err := DigestFromHex(aad.RequestDigest); err != nil {
		return nil, err
	}
	if _, err := DigestFromHex(aad.SkillSnapshotDigest); err != nil {
		return nil, err
	}
	encoded, err := canonicalJSON(aad)
	if err != nil {
		return nil, wrapSnapshotError("encode outbox v2 aad", err)
	}
	return encoded, nil
}

// canonicalJSON 使用有类型 struct 字段顺序、compact JSON、UTF-8 和禁用 HTML escape 生成稳定字节。
func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode canonical JSON: %w", err)
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

// validateAllowedGraphToolKeys 核对声明键与六能力 enabled 集合完全一致，禁止由 Skill 内容动态新增其他 Tool。
func validateAllowedGraphToolKeys(keys []string, content SkillRuntimeContentV1) error {
	if keys == nil || len(keys) > 6 {
		return ErrSnapshotInvalid
	}
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
	expected := make([]string, 0, len(ordered))
	for _, entry := range ordered {
		if entry.capability.Applicability == "enabled" {
			expected = append(expected, entry.key)
		}
	}
	if len(expected) != len(keys) {
		return ErrSnapshotInvalid
	}
	for index := range expected {
		if expected[index] != keys[index] {
			return ErrSnapshotInvalid
		}
	}
	return nil
}
