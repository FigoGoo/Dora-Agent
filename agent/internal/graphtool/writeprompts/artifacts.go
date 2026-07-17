package writeprompts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// CanonicalToolInfo 返回全新且严格的 write_prompts Tool Definition。
// 调用方不得把外部 ToolInfo 指针保存为 Runtime Registry 真源。
func CanonicalToolInfo(ctx context.Context) (*schema.ToolInfo, error) {
	return (&Tool{}).Info(ctx)
}

// ValidateToolInfo 对名称、描述和完整 JSON Schema 做 canonical exact-match。
func ValidateToolInfo(info *schema.ToolInfo) error {
	actual, err := toolDefinitionDigest(info)
	if err != nil {
		return fmt.Errorf("validate write_prompts tool definition: %w", err)
	}
	if actual != ToolDefinitionDigest() {
		return fmt.Errorf("validate write_prompts tool definition: canonical definition digest mismatch")
	}
	return nil
}

// ToolDefinitionDigest 计算真实 Tool 名称、描述与严格 JSON Schema 的摘要。
func ToolDefinitionDigest() string {
	info, err := CanonicalToolInfo(context.Background())
	if err != nil {
		panic(fmt.Sprintf("build canonical write_prompts tool definition: %v", err))
	}
	digest, err := toolDefinitionDigest(info)
	if err != nil {
		panic(fmt.Sprintf("digest canonical write_prompts tool definition: %v", err))
	}
	return digest
}

// PromptArtifactDigest 计算真实 Prompt 模板内容与格式边界的摘要，而不是版本 Ref 摘要。
func PromptArtifactDigest() string {
	return mustArtifactDigest(struct {
		SchemaVersion          string `json:"schema_version"`
		PromptKey              string `json:"prompt_key"`
		PromptVersion          string `json:"prompt_version"`
		Format                 string `json:"format"`
		System                 string `json:"system"`
		User                   string `json:"user"`
		CandidateSchemaVersion string `json:"candidate_schema_version"`
	}{
		SchemaVersion: "write_prompts.prompt-artifact.v1", PromptKey: PromptKey,
		PromptVersion: PromptVersion, Format: "fmt.Sprintf", System: primaryPromptSystem,
		User: primaryPromptUser, CandidateSchemaVersion: CandidateSchemaVersion,
	}, "prompt artifact")
}

// ValidatorArtifactDigest 计算候选协议、Unicode 与 Prompt Content 校验器的真实冻结策略摘要。
func ValidatorArtifactDigest() string {
	return mustArtifactDigest(struct {
		SchemaVersion string   `json:"schema_version"`
		Version       string   `json:"version"`
		StrictJSON    []string `json:"strict_json"`
		MaxJSONBytes  int      `json:"max_json_bytes"`
		Patterns      struct {
			Element string `json:"element"`
			Slot    string `json:"slot"`
		} `json:"patterns"`
		Enums struct {
			Locale    []string `json:"locale"`
			SlotType  []string `json:"slot_type"`
			MediaKind []string `json:"media_kind"`
		} `json:"enums"`
		Limits struct {
			InstructionMin         int `json:"instruction_min"`
			InstructionMax         int `json:"instruction_max"`
			SourceTitleMax         int `json:"source_title_max"`
			SourceSummaryMax       int `json:"source_summary_max"`
			ElementTitleMax        int `json:"element_title_max"`
			ElementNarrativeMax    int `json:"element_narrative_max"`
			SlotPurposeMax         int `json:"slot_purpose_max"`
			PositivePromptMax      int `json:"positive_prompt_max"`
			NegativeConstraintMax  int `json:"negative_constraint_max"`
			NegativeConstraintsMax int `json:"negative_constraints_max"`
			ElementsMax            int `json:"elements_max"`
			TargetsMax             int `json:"targets_max"`
			ResultSummaryMax       int `json:"result_summary_max"`
		} `json:"limits"`
		Invariants []string `json:"invariants"`
	}{
		SchemaVersion: "write_prompts.validator-artifact.v1", Version: ValidatorVersion,
		StrictJSON: []string{
			"utf8", "valid_surrogate_pairs", "nfc_text", "trimmed_text", "no_control_text",
			"no_duplicate_fields", "no_unknown_fields", "no_null_root", "single_json_value", "canonical_html_escape",
		},
		MaxJSONBytes: maxJSONBytes,
		Patterns: struct {
			Element string `json:"element"`
			Slot    string `json:"slot"`
		}{Element: elementKeyPattern.String(), Slot: slotKeyPattern.String()},
		Enums: struct {
			Locale    []string `json:"locale"`
			SlotType  []string `json:"slot_type"`
			MediaKind []string `json:"media_kind"`
		}{
			Locale: append([]string(nil), localeValues[:]...), SlotType: append([]string(nil), slotTypeValues[:]...),
			MediaKind: append([]string(nil), mediaKindValues[:]...),
		},
		Limits: struct {
			InstructionMin         int `json:"instruction_min"`
			InstructionMax         int `json:"instruction_max"`
			SourceTitleMax         int `json:"source_title_max"`
			SourceSummaryMax       int `json:"source_summary_max"`
			ElementTitleMax        int `json:"element_title_max"`
			ElementNarrativeMax    int `json:"element_narrative_max"`
			SlotPurposeMax         int `json:"slot_purpose_max"`
			PositivePromptMax      int `json:"positive_prompt_max"`
			NegativeConstraintMax  int `json:"negative_constraint_max"`
			NegativeConstraintsMax int `json:"negative_constraints_max"`
			ElementsMax            int `json:"elements_max"`
			TargetsMax             int `json:"targets_max"`
			ResultSummaryMax       int `json:"result_summary_max"`
		}{
			intentInstructionMin, intentInstructionMax, sourceTitleMax, sourceSummaryMax,
			elementTitleMax, elementNarrativeMax, slotPurposeMax, positivePromptMax,
			negativeConstraintMax, maxNegativeConstraints, maxSourceElements, maxSourceSlots, resultSummaryMax,
		},
		Invariants: []string{
			"source_elements_dense_and_ordered", "source_slots_dense_and_unique", "source_slot_owner_exists",
			"candidate_negative_constraints_unique", "content_trusted_fields_derived_from_targets",
			"content_digest_canonical_json", "business_resource_exact_command_binding",
		},
	}, "validator artifact")
}

// ExactSetValidatorArtifactDigest 计算独立目标全集校验器的真实冻结策略摘要。
func ExactSetValidatorArtifactDigest() string {
	return mustArtifactDigest(struct {
		SchemaVersion string   `json:"schema_version"`
		Version       string   `json:"version"`
		TargetLimit   int      `json:"target_limit"`
		ElementKey    string   `json:"element_key_pattern"`
		SlotKey       string   `json:"slot_key_pattern"`
		MediaMapping  []string `json:"media_mapping"`
		Rules         []string `json:"rules"`
	}{
		SchemaVersion: "write_prompts.exact-set-validator-artifact.v1", Version: ExactSetValidatorVersion,
		TargetLimit: maxSourceSlots, ElementKey: elementKeyPattern.String(), SlotKey: slotKeyPattern.String(),
		MediaMapping: []string{"image:image", "video:video", "audio:audio", "voiceover:audio", "caption:text"},
		Rules: []string{
			"all_source_slots_required", "zero_targets_rejected", "over_budget_rejected_without_truncation",
			"candidate_count_exact", "candidate_order_exact", "candidate_target_keys_exact",
			"duplicate_missing_extra_unknown_targets_rejected", "trusted_target_fields_backfilled_after_validation",
		},
	}, "exact-set validator artifact")
}

// RuntimePolicyDigest 校验并计算本次启动冻结策略值的 canonical 摘要。
func RuntimePolicyDigest(policy Policy) (string, error) {
	if err := ValidatePolicy(policy); err != nil {
		return "", err
	}
	return artifactDigest(struct {
		SchemaVersion         string `json:"schema_version"`
		Version               string `json:"version"`
		MaxTargets            int    `json:"max_targets"`
		DefaultOutputLanguage string `json:"default_output_language"`
		MaxCommandResends     int    `json:"max_command_resends"`
	}{
		SchemaVersion: "write_prompts.runtime-policy-artifact.v1", Version: policy.Version,
		MaxTargets: policy.MaxTargets, DefaultOutputLanguage: policy.DefaultOutputLanguage,
		MaxCommandResends: policy.MaxCommandResends,
	}, "runtime policy")
}

// toolDefinitionDigest 规范化真实 Tool 名称、描述与 JSON Schema；缺少参数 Schema 时失败关闭。
func toolDefinitionDigest(info *schema.ToolInfo) (string, error) {
	if info == nil || info.ParamsOneOf == nil {
		return "", fmt.Errorf("tool info and parameters are required")
	}
	params, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || params == nil {
		return "", fmt.Errorf("convert parameters to JSON Schema")
	}
	return artifactDigest(struct {
		SchemaVersion string `json:"schema_version"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		Parameters    any    `json:"parameters"`
	}{
		SchemaVersion: "write_prompts.tool-definition-artifact.v1",
		Name:          info.Name, Description: info.Desc, Parameters: params,
	}, "tool definition")
}

// mustArtifactDigest 用于启动期不可变常量工件；编码失败表示代码契约损坏，因此主动 panic 阻止启动。
func mustArtifactDigest(value any, label string) string {
	digest, err := artifactDigest(value, label)
	if err != nil {
		panic(err)
	}
	return digest
}

// artifactDigest 把具名工件先标准化再计算小写 SHA-256，禁止只对版本 Ref 求摘要。
func artifactDigest(value any, label string) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("digest %s: encode: %w", label, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return "", fmt.Errorf("digest %s: normalize: %w", label, err)
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("digest %s: canonicalize: %w", label, err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}
