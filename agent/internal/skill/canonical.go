package skill

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

var graphToolKeysInProductOrder = []string{
	"plan_creation_spec",
	"analyze_materials",
	"plan_storyboard",
	"generate_media",
	"write_prompts",
	"assemble_output",
}

// snapshotMetadataItemV1 固定 Snapshot set digest 的字段顺序且明确排除 Runtime 明文。
type snapshotMetadataItemV1 struct {
	LoadOrder                   int32                     `json:"load_order"`
	Priority                    int32                     `json:"priority"`
	Namespace                   SkillNamespaceV1          `json:"namespace"`
	SkillID                     string                    `json:"skill_id"`
	PublisherUserID             string                    `json:"publisher_user_id"`
	PublishedSnapshotID         string                    `json:"published_snapshot_id"`
	PublicationRevision         int64                     `json:"publication_revision"`
	DefinitionSchemaVersion     string                    `json:"definition_schema_version"`
	ContentDigest               string                    `json:"content_digest"`
	RuntimeContentSchemaVersion string                    `json:"runtime_content_schema_version"`
	RuntimeContentDigest        string                    `json:"runtime_content_digest"`
	AllowedGraphToolKeys        []string                  `json:"allowed_graph_tool_keys"`
	PublicToolRefs              []PublicToolSnapshotRefV1 `json:"public_tool_refs"`
	PermissionSnapshotDigest    string                    `json:"permission_snapshot_digest"`
	RuntimePolicyRef            string                    `json:"runtime_policy_ref"`
	GovernanceEpoch             int64                     `json:"governance_epoch"`
	PublishedAtUnixMS           int64                     `json:"published_at_unix_ms"`
}

// ensureRequestCanonicalV2 固定 Ensure v2 业务语义字段顺序并排除追踪、命令和时间字段。
type ensureRequestCanonicalV2 struct {
	SchemaVersion              string                     `json:"schema_version"`
	ProjectID                  string                     `json:"project_id"`
	OwnerUserID                string                     `json:"owner_user_id"`
	CreationSource             string                     `json:"creation_source"`
	PromptPresent              bool                       `json:"prompt_present"`
	PromptDigest               string                     `json:"prompt_digest"`
	SkillSnapshotSchemaVersion string                     `json:"skill_snapshot_schema_version"`
	SkillSnapshotKind          SessionSkillSnapshotKindV1 `json:"skill_snapshot_kind"`
	SkillCount                 int32                      `json:"skill_count"`
	SkillSnapshotDigest        string                     `json:"skill_snapshot_digest"`
}

// CanonicalRuntimeContentV1 规范化并校验 Runtime Content，返回唯一 compact JSON 和摘要。
// 数组必须由 Business 按冻结规则排序；Agent 只验证顺序，绝不静默重排改变 wire 语义。
func CanonicalRuntimeContentV1(input SkillRuntimeContentV1, limits LimitsProfileV1) (SkillRuntimeContentV1, []byte, Digest, error) {
	if err := limits.Validate(); err != nil {
		return SkillRuntimeContentV1{}, nil, Digest{}, err
	}
	normalized, err := normalizeRuntimeContentV1(input, limits)
	if err != nil {
		return SkillRuntimeContentV1{}, nil, Digest{}, err
	}
	encoded, err := encodeCanonicalJSON(normalized)
	if err != nil {
		return SkillRuntimeContentV1{}, nil, Digest{}, fmt.Errorf("%w: encode runtime content: %v", ErrInvalidContract, err)
	}
	if len(encoded) > limits.MaxRuntimeContentBytesPerItem {
		return SkillRuntimeContentV1{}, nil, Digest{}, fmt.Errorf("%w: runtime content bytes", ErrLimitExceeded)
	}
	return normalized, encoded, sha256.Sum256(encoded), nil
}

// ParseCanonicalRuntimeContentV1 严格恢复加密前保存的 canonical Runtime Content。
// 它拒绝 unknown、trailing、null、nil list、重复对象键产生的非唯一编码及 HTML 转义别名。
func ParseCanonicalRuntimeContentV1(encoded []byte, limits LimitsProfileV1) (SkillRuntimeContentV1, Digest, error) {
	if err := limits.Validate(); err != nil {
		return SkillRuntimeContentV1{}, Digest{}, err
	}
	if len(encoded) == 0 || len(encoded) > limits.MaxRuntimeContentBytesPerItem {
		return SkillRuntimeContentV1{}, Digest{}, fmt.Errorf("%w: runtime content bytes", ErrLimitExceeded)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var content SkillRuntimeContentV1
	if err := decoder.Decode(&content); err != nil {
		return SkillRuntimeContentV1{}, Digest{}, fmt.Errorf("%w: decode runtime content", ErrInvalidContract)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return SkillRuntimeContentV1{}, Digest{}, fmt.Errorf("%w: trailing runtime content", ErrInvalidContract)
	}
	normalized, canonical, digest, err := CanonicalRuntimeContentV1(content, limits)
	if err != nil {
		return SkillRuntimeContentV1{}, Digest{}, err
	}
	if !bytes.Equal(encoded, canonical) {
		return SkillRuntimeContentV1{}, Digest{}, fmt.Errorf("%w: runtime content is not canonical", ErrInvalidContract)
	}
	return normalized, digest, nil
}

// CanonicalSnapshotSetV1 独立重算每项 Runtime digest 和集合 metadata digest，并验证调用方摘要。
// 失败时不得保存部分 Snapshot，也不得截断或跳过非法 Skill。
func CanonicalSnapshotSetV1(input SessionSkillSnapshotV1, limits LimitsProfileV1) (SessionSkillSnapshotV1, []byte, Digest, error) {
	if err := limits.Validate(); err != nil {
		return SessionSkillSnapshotV1{}, nil, Digest{}, err
	}
	if input.SchemaVersion != SnapshotSchemaVersionV1 {
		return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: snapshot schema_version", ErrInvalidContract)
	}
	if input.Skills == nil {
		return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: skills must be a non-nil list", ErrInvalidContract)
	}
	if input.SkillCount != int32(len(input.Skills)) || len(input.Skills) > limits.MaxItems {
		if len(input.Skills) > limits.MaxItems {
			return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: skill count", ErrLimitExceeded)
		}
		return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: skill_count mismatch", ErrInvalidContract)
	}
	if (input.SnapshotKind == SessionSkillSnapshotKindEmptyV1) != (len(input.Skills) == 0) ||
		(input.SnapshotKind != SessionSkillSnapshotKindEmptyV1 && input.SnapshotKind != SessionSkillSnapshotKindPublishedRefsV1) {
		return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: snapshot kind/count combination", ErrInvalidContract)
	}

	normalized := input
	normalized.Skills = make([]PublishedSkillSnapshotRefV1, len(input.Skills))
	metadata := make([]snapshotMetadataItemV1, len(input.Skills))
	seenSkillIDs := make(map[string]struct{}, len(input.Skills))
	seenPublishedIDs := make(map[string]struct{}, len(input.Skills))
	totalRuntimeBytes := 0
	for index, item := range input.Skills {
		normalizedItem, itemMetadata, runtimeBytes, err := normalizeSnapshotItemV1(item, int32(index+1), limits)
		if err != nil {
			return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("skill[%d]: %w", index, err)
		}
		if _, exists := seenSkillIDs[normalizedItem.SkillID]; exists {
			return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: duplicate skill_id", ErrInvalidContract)
		}
		if _, exists := seenPublishedIDs[normalizedItem.PublishedSnapshotID]; exists {
			return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: duplicate published_snapshot_id", ErrInvalidContract)
		}
		seenSkillIDs[normalizedItem.SkillID] = struct{}{}
		seenPublishedIDs[normalizedItem.PublishedSnapshotID] = struct{}{}
		normalized.Skills[index] = normalizedItem
		metadata[index] = itemMetadata
		totalRuntimeBytes += runtimeBytes
		if totalRuntimeBytes > limits.MaxTotalRuntimeContentBytes {
			return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: total runtime content bytes", ErrLimitExceeded)
		}
	}
	encoded, err := encodeCanonicalJSON(metadata)
	if err != nil {
		return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: encode snapshot metadata: %v", ErrInvalidContract, err)
	}
	if len(encoded) > limits.MaxSnapshotMetadataBytes {
		return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: snapshot metadata bytes", ErrLimitExceeded)
	}
	digest := Digest(sha256.Sum256(encoded))
	claimed, err := ParseDigest(input.SnapshotSetDigest)
	if err != nil || claimed != digest {
		return SessionSkillSnapshotV1{}, nil, Digest{}, fmt.Errorf("%w: snapshot_set_digest", ErrDigestMismatch)
	}
	normalized.SnapshotSetDigest = digest.Hex()
	return normalized, encoded, digest, nil
}

// CanonicalEnsureProjectSessionV2 规范化 Prompt、验证完整 Snapshot，并计算 Ensure v2 语义摘要。
func CanonicalEnsureProjectSessionV2(input EnsureProjectSessionInputV2, limits LimitsProfileV1) (EnsureProjectSessionCanonicalV2, error) {
	if input.SchemaVersion != EnsureProjectSessionSchemaVersionV2 {
		return EnsureProjectSessionCanonicalV2{}, fmt.Errorf("%w: ensure schema_version", ErrInvalidContract)
	}
	projectID, err := canonicalUUIDv7(input.ProjectID)
	if err != nil {
		return EnsureProjectSessionCanonicalV2{}, fmt.Errorf("%w: project_id", ErrInvalidContract)
	}
	ownerUserID, err := canonicalUUIDv7(input.OwnerUserID)
	if err != nil {
		return EnsureProjectSessionCanonicalV2{}, fmt.Errorf("%w: owner_user_id", ErrInvalidContract)
	}
	if input.CreationSource != CreationSourceQuickCreate {
		return EnsureProjectSessionCanonicalV2{}, fmt.Errorf("%w: creation_source", ErrInvalidContract)
	}
	prompt, promptPresent, err := normalizePromptV2(input.InitialPrompt)
	if err != nil {
		return EnsureProjectSessionCanonicalV2{}, err
	}
	promptDigest := ""
	if promptPresent {
		promptDigest = Digest(sha256.Sum256([]byte(prompt))).Hex()
	}
	snapshot, _, _, err := CanonicalSnapshotSetV1(input.SkillSnapshot, limits)
	if err != nil {
		return EnsureProjectSessionCanonicalV2{}, err
	}
	canonical := ensureRequestCanonicalV2{
		SchemaVersion:              EnsureProjectSessionSchemaVersionV2,
		ProjectID:                  projectID,
		OwnerUserID:                ownerUserID,
		CreationSource:             CreationSourceQuickCreate,
		PromptPresent:              promptPresent,
		PromptDigest:               promptDigest,
		SkillSnapshotSchemaVersion: snapshot.SchemaVersion,
		SkillSnapshotKind:          snapshot.SnapshotKind,
		SkillCount:                 snapshot.SkillCount,
		SkillSnapshotDigest:        snapshot.SnapshotSetDigest,
	}
	encoded, err := encodeCanonicalJSON(canonical)
	if err != nil {
		return EnsureProjectSessionCanonicalV2{}, fmt.Errorf("%w: encode ensure request: %v", ErrInvalidContract, err)
	}
	return EnsureProjectSessionCanonicalV2{
		NormalizedPrompt: prompt,
		PromptPresent:    promptPresent,
		PromptDigest:     promptDigest,
		SkillSnapshot:    snapshot,
		CanonicalJSON:    encoded,
		RequestDigest:    sha256.Sum256(encoded),
	}, nil
}

// normalizeRuntimeContentV1 执行上游 Definition 已冻结的 trim/NFC、互斥、顺序和数量校验。
func normalizeRuntimeContentV1(input SkillRuntimeContentV1, limits LimitsProfileV1) (SkillRuntimeContentV1, error) {
	if input.SchemaVersion != RuntimeContentSchemaVersionV1 {
		return SkillRuntimeContentV1{}, fmt.Errorf("%w: runtime schema_version", ErrInvalidContract)
	}
	if input.Examples == nil || input.StarterPrompts == nil {
		return SkillRuntimeContentV1{}, fmt.Errorf("%w: runtime lists must be non-nil", ErrInvalidContract)
	}
	if len(input.Examples) > limits.MaxExamplesPerItem || len(input.StarterPrompts) > limits.MaxStarterPromptsPerItem {
		return SkillRuntimeContentV1{}, fmt.Errorf("%w: runtime list count", ErrLimitExceeded)
	}
	var result SkillRuntimeContentV1
	result.SchemaVersion = RuntimeContentSchemaVersionV1
	var err error
	if result.Name, err = normalizeRuntimeText(input.Name, true, maxRuntimeNameBytes); err != nil {
		return SkillRuntimeContentV1{}, fmt.Errorf("name: %w", err)
	}
	if result.InputDescription, err = normalizeRuntimeText(input.InputDescription, false, maxRuntimeTextBytes); err != nil {
		return SkillRuntimeContentV1{}, fmt.Errorf("input_description: %w", err)
	}
	if result.OutputDescription, err = normalizeRuntimeText(input.OutputDescription, false, maxRuntimeTextBytes); err != nil {
		return SkillRuntimeContentV1{}, fmt.Errorf("output_description: %w", err)
	}
	if result.InvocationRules, err = normalizeRuntimeText(input.InvocationRules, false, maxRuntimeTextBytes); err != nil {
		return SkillRuntimeContentV1{}, fmt.Errorf("invocation_rules: %w", err)
	}
	guidances := []struct {
		name  string
		input CapabilityGuidanceV1
		set   func(CapabilityGuidanceV1)
	}{
		{"plan_creation_spec", input.PlanCreationSpec, func(value CapabilityGuidanceV1) { result.PlanCreationSpec = value }},
		{"analyze_materials", input.AnalyzeMaterials, func(value CapabilityGuidanceV1) { result.AnalyzeMaterials = value }},
		{"plan_storyboard", input.PlanStoryboard, func(value CapabilityGuidanceV1) { result.PlanStoryboard = value }},
		{"generate_media", input.GenerateMedia, func(value CapabilityGuidanceV1) { result.GenerateMedia = value }},
		{"write_prompts", input.WritePrompts, func(value CapabilityGuidanceV1) { result.WritePrompts = value }},
		{"assemble_output", input.AssembleOutput, func(value CapabilityGuidanceV1) { result.AssembleOutput = value }},
	}
	for _, guidance := range guidances {
		normalized, normalizeErr := normalizeGuidanceV1(guidance.input)
		if normalizeErr != nil {
			return SkillRuntimeContentV1{}, fmt.Errorf("%s: %w", guidance.name, normalizeErr)
		}
		guidance.set(normalized)
	}
	result.Examples = make([]SkillExampleV1, len(input.Examples))
	for index, example := range input.Examples {
		inputText, inputErr := normalizeRuntimeText(example.Input, true, maxRuntimeTextBytes)
		outputText, outputErr := normalizeRuntimeText(example.Output, true, maxRuntimeTextBytes)
		if inputErr != nil || outputErr != nil {
			return SkillRuntimeContentV1{}, fmt.Errorf("%w: examples[%d]", ErrInvalidContract, index)
		}
		result.Examples[index] = SkillExampleV1{Input: inputText, Output: outputText}
		if index > 0 && compareExample(result.Examples[index-1], result.Examples[index]) >= 0 {
			return SkillRuntimeContentV1{}, fmt.Errorf("%w: examples must be unique and sorted", ErrInvalidContract)
		}
	}
	result.StarterPrompts = make([]string, len(input.StarterPrompts))
	for index, prompt := range input.StarterPrompts {
		normalized, normalizeErr := normalizeRuntimeText(prompt, true, maxRuntimeTextBytes)
		if normalizeErr != nil {
			return SkillRuntimeContentV1{}, fmt.Errorf("starter_prompts[%d]: %w", index, normalizeErr)
		}
		result.StarterPrompts[index] = normalized
		if index > 0 && result.StarterPrompts[index-1] >= normalized {
			return SkillRuntimeContentV1{}, fmt.Errorf("%w: starter_prompts must be unique and sorted", ErrInvalidContract)
		}
	}
	return result, nil
}

// normalizeSnapshotItemV1 校验单项固定 metadata、Runtime 内容、能力声明和整数边界。
func normalizeSnapshotItemV1(input PublishedSkillSnapshotRefV1, expectedOrder int32, limits LimitsProfileV1) (PublishedSkillSnapshotRefV1, snapshotMetadataItemV1, int, error) {
	if input.LoadOrder != expectedOrder || input.Priority < 0 || input.PublicationRevision <= 0 ||
		input.GovernanceEpoch < 0 || input.PublishedAtUnixMS <= 0 {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: item integer boundary", ErrInvalidContract)
	}
	if input.Namespace != SkillNamespaceSystemV1 && input.Namespace != SkillNamespaceUserV1 {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: namespace", ErrInvalidContract)
	}
	skillID, err := canonicalUUIDv7(input.SkillID)
	if err != nil {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: skill_id", ErrInvalidContract)
	}
	publisherID, err := canonicalUUIDv7(input.PublisherUserID)
	if err != nil {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: publisher_user_id", ErrInvalidContract)
	}
	publishedID, err := canonicalUUIDv7(input.PublishedSnapshotID)
	if err != nil {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: published_snapshot_id", ErrInvalidContract)
	}
	if input.DefinitionSchemaVersion != DefinitionSchemaVersionV1 ||
		input.RuntimeContentSchemaVersion != RuntimeContentSchemaVersionV1 ||
		input.RuntimePolicyRef != RuntimePolicyRefV1 {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: item version or policy", ErrInvalidContract)
	}
	if _, err := ParseDigest(input.ContentDigest); err != nil {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: content_digest", ErrInvalidContract)
	}
	if _, err := ParseDigest(input.PermissionSnapshotDigest); err != nil {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: permission_snapshot_digest", ErrInvalidContract)
	}
	normalizedRuntime, runtimeCanonical, runtimeDigest, err := CanonicalRuntimeContentV1(input.RuntimeContent, limits)
	if err != nil {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, err
	}
	claimedRuntimeDigest, err := ParseDigest(input.RuntimeContentDigest)
	if err != nil || claimedRuntimeDigest != runtimeDigest {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: runtime_content_digest", ErrDigestMismatch)
	}
	if input.AllowedGraphToolKeys == nil || len(input.AllowedGraphToolKeys) > limits.MaxAllowedGraphToolKeysPerItem {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: allowed_graph_tool_keys", ErrInvalidContract)
	}
	expectedKeys := enabledGraphToolKeysV1(normalizedRuntime)
	if !equalStrings(input.AllowedGraphToolKeys, expectedKeys) {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: allowed_graph_tool_keys do not match enabled guidance", ErrInvalidContract)
	}
	if input.PublicToolRefs == nil || len(input.PublicToolRefs) != 0 || len(input.PublicToolRefs) > limits.MaxPublicToolRefsPerItem {
		return PublishedSkillSnapshotRefV1{}, snapshotMetadataItemV1{}, 0, fmt.Errorf("%w: public_tool_refs must be a non-nil empty list", ErrInvalidContract)
	}
	normalized := input
	normalized.SkillID = skillID
	normalized.PublisherUserID = publisherID
	normalized.PublishedSnapshotID = publishedID
	normalized.RuntimeContent = normalizedRuntime
	normalized.RuntimeContentDigest = runtimeDigest.Hex()
	normalized.AllowedGraphToolKeys = append([]string(nil), input.AllowedGraphToolKeys...)
	normalized.PublicToolRefs = make([]PublicToolSnapshotRefV1, 0)
	metadata := snapshotMetadataItemV1{
		LoadOrder: normalized.LoadOrder, Priority: normalized.Priority, Namespace: normalized.Namespace,
		SkillID: normalized.SkillID, PublisherUserID: normalized.PublisherUserID,
		PublishedSnapshotID: normalized.PublishedSnapshotID, PublicationRevision: normalized.PublicationRevision,
		DefinitionSchemaVersion: normalized.DefinitionSchemaVersion, ContentDigest: normalized.ContentDigest,
		RuntimeContentSchemaVersion: normalized.RuntimeContentSchemaVersion, RuntimeContentDigest: normalized.RuntimeContentDigest,
		AllowedGraphToolKeys: normalized.AllowedGraphToolKeys, PublicToolRefs: normalized.PublicToolRefs,
		PermissionSnapshotDigest: normalized.PermissionSnapshotDigest, RuntimePolicyRef: normalized.RuntimePolicyRef,
		GovernanceEpoch: normalized.GovernanceEpoch, PublishedAtUnixMS: normalized.PublishedAtUnixMS,
	}
	return normalized, metadata, len(runtimeCanonical), nil
}

// normalizeGuidanceV1 校验 applicability 与 guidance/reason 的严格互斥关系。
func normalizeGuidanceV1(input CapabilityGuidanceV1) (CapabilityGuidanceV1, error) {
	guidance, err := normalizeRuntimeText(input.Guidance, false, maxRuntimeTextBytes)
	if err != nil {
		return CapabilityGuidanceV1{}, err
	}
	reason, err := normalizeRuntimeText(input.NotApplicableReason, false, maxRuntimeTextBytes)
	if err != nil {
		return CapabilityGuidanceV1{}, err
	}
	result := CapabilityGuidanceV1{Applicability: input.Applicability, Guidance: guidance, NotApplicableReason: reason}
	switch result.Applicability {
	case SkillGuidanceEnabledV1:
		if result.Guidance == "" || result.NotApplicableReason != "" {
			return CapabilityGuidanceV1{}, fmt.Errorf("%w: enabled guidance invariant", ErrInvalidContract)
		}
	case SkillGuidanceNotApplicableV1:
		if result.Guidance != "" || result.NotApplicableReason == "" {
			return CapabilityGuidanceV1{}, fmt.Errorf("%w: not_applicable guidance invariant", ErrInvalidContract)
		}
	default:
		return CapabilityGuidanceV1{}, fmt.Errorf("%w: guidance applicability", ErrInvalidContract)
	}
	return result, nil
}

// normalizeRuntimeText 复用上游 Published Definition 的 trim、NFC、控制字符和 UTF-8 字节边界。
func normalizeRuntimeText(input string, required bool, maxBytes int) (string, error) {
	if !utf8.ValidString(input) {
		return "", fmt.Errorf("%w: invalid UTF-8", ErrInvalidContract)
	}
	value := strings.TrimSpace(norm.NFC.String(input))
	if required && value == "" {
		return "", fmt.Errorf("%w: required text", ErrInvalidContract)
	}
	if len(value) > maxBytes {
		return "", fmt.Errorf("%w: text bytes", ErrLimitExceeded)
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t' {
			return "", fmt.Errorf("%w: forbidden control character", ErrInvalidContract)
		}
	}
	return value, nil
}

// normalizePromptV2 沿用 W0 Prompt 规则：只做 NFC，纯 Unicode 空白折叠，非空正文保留边界空格。
func normalizePromptV2(input string) (string, bool, error) {
	if !utf8.ValidString(input) {
		return "", false, fmt.Errorf("%w: initial_prompt invalid UTF-8", ErrInvalidContract)
	}
	value := norm.NFC.String(input)
	if len(value) > maxInitialPromptBytes {
		return "", false, fmt.Errorf("%w: initial_prompt bytes", ErrLimitExceeded)
	}
	if strings.TrimFunc(value, unicode.IsSpace) == "" {
		return "", false, nil
	}
	return value, true, nil
}

// canonicalUUIDv7 只接受小写、连字符齐全的 RFC 9562 UUIDv7 唯一表示。
func canonicalUUIDv7(value string) (string, error) {
	id, err := uuid.Parse(value)
	if err != nil || id.Version() != 7 || id.String() != value {
		return "", fmt.Errorf("UUIDv7 must use canonical lowercase form")
	}
	return value, nil
}

// enabledGraphToolKeysV1 按产品冻结顺序投影 enabled guidance，结果只代表声明而非可执行性。
func enabledGraphToolKeysV1(content SkillRuntimeContentV1) []string {
	guidances := []CapabilityGuidanceV1{
		content.PlanCreationSpec,
		content.AnalyzeMaterials,
		content.PlanStoryboard,
		content.GenerateMedia,
		content.WritePrompts,
		content.AssembleOutput,
	}
	result := make([]string, 0, len(graphToolKeysInProductOrder))
	for index, guidance := range guidances {
		if guidance.Applicability == SkillGuidanceEnabledV1 {
			result = append(result, graphToolKeysInProductOrder[index])
		}
	}
	return result
}

// compareExample 按 input ASC、output ASC 比较两个规范化示例。
func compareExample(left, right SkillExampleV1) int {
	if comparison := strings.Compare(left.Input, right.Input); comparison != 0 {
		return comparison
	}
	return strings.Compare(left.Output, right.Output)
}

// equalStrings 比较两个有序字符串数组，避免集合相同但顺序不同产生多种摘要。
func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// encodeCanonicalJSON 使用固定 struct 字段顺序、compact JSON 且禁止 HTML escape。
func encodeCanonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}
