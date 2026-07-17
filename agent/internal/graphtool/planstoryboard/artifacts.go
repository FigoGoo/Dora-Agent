package planstoryboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// CanonicalToolInfo 返回全新且严格的 plan_storyboard Tool Definition。
// 调用方不得把外部 ToolInfo 指针直接保存为 Runtime Registry 真源。
func CanonicalToolInfo(ctx context.Context) (*schema.ToolInfo, error) {
	return (&Tool{}).Info(ctx)
}

// ValidateToolInfo 对名称、描述和完整 JSON Schema 做 canonical exact-match。
func ValidateToolInfo(info *schema.ToolInfo) error {
	actual, err := toolDefinitionDigest(info)
	if err != nil {
		return fmt.Errorf("validate plan_storyboard tool definition: %w", err)
	}
	if actual != ToolDefinitionDigest() {
		return fmt.Errorf("validate plan_storyboard tool definition: canonical definition digest mismatch")
	}
	return nil
}

// ToolDefinitionDigest 计算真实 Tool 名称、描述与严格 JSON Schema 的摘要。
func ToolDefinitionDigest() string {
	info, err := CanonicalToolInfo(context.Background())
	if err != nil {
		panic(fmt.Sprintf("build canonical plan_storyboard tool definition: %v", err))
	}
	digest, err := toolDefinitionDigest(info)
	if err != nil {
		panic(fmt.Sprintf("digest canonical plan_storyboard tool definition: %v", err))
	}
	return digest
}

// PromptArtifactDigest 计算真实 Prompt 模板内容与格式边界的摘要，而不是版本 Ref 的摘要。
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
		SchemaVersion: "plan_storyboard.prompt-artifact.v1", PromptKey: PromptKey,
		PromptVersion: PromptVersion, Format: "f-string", System: primaryPromptSystem,
		User: primaryPromptUser, CandidateSchemaVersion: CandidateSchemaVersion,
	}, "prompt artifact")
}

// ValidatorArtifactDigest 计算候选与 Draft Content 校验器的真实冻结策略摘要。
func ValidatorArtifactDigest() string {
	return mustArtifactDigest(struct {
		SchemaVersion string   `json:"schema_version"`
		Version       string   `json:"version"`
		StrictJSON    []string `json:"strict_json"`
		MaxJSONBytes  int      `json:"max_json_bytes"`
		Patterns      struct {
			Phase, Section, Element, Slot string
		} `json:"patterns"`
		Enums struct {
			Deliverable, Locale, Element, Slot []string
		} `json:"enums"`
		Limits struct {
			IntentInstructionMin, IntentInstructionMax     int
			IntentTargetSecondsMin, IntentTargetSecondsMax int
			CreationSpecTitleMax, CreationSpecGoalMax      int
			CreationSpecAudienceMax                        int
			CreationSpecPhasesMax                          int
			CreationSpecConstraintsMax                     int
			CreationSpecAcceptanceMin                      int
			CreationSpecAcceptanceMax                      int
			SectionsMax, ElementsMax, SlotsMax             int
			SectionTitleMax, SectionObjectiveMax           int
			ElementTitleMax, ElementNarrativeMax           int
			ElementDurationMin, ElementDurationMax         int
			ElementDependenciesMax, SlotsPerElementMax     int
			SlotPurposeMax                                 int
			TotalDurationMin, TotalDurationMax             int
			TargetTolerancePercent, TargetToleranceMinimum int
		} `json:"limits"`
		Invariants []string `json:"invariants"`
	}{
		SchemaVersion: "plan_storyboard.validator-artifact.v1", Version: ValidatorVersion,
		StrictJSON:   []string{"utf8", "valid_surrogate_pairs", "nfc_text", "trimmed_text", "no_control_text", "no_duplicate_fields", "no_unknown_fields", "no_null_root", "single_json_value"},
		MaxJSONBytes: maxJSONBytes,
		Patterns: struct {
			Phase, Section, Element, Slot string
		}{phaseKeyPattern.String(), sectionKeyPattern.String(), elementKeyPattern.String(), slotKeyPattern.String()},
		Enums: struct {
			Deliverable, Locale, Element, Slot []string
		}{
			Deliverable: append([]string(nil), deliverableValues[:]...),
			Locale:      append([]string(nil), localeValues[:]...),
			Element:     append([]string(nil), elementTypeValues[:]...),
			Slot:        append([]string(nil), slotTypeValues[:]...),
		},
		Limits: struct {
			IntentInstructionMin, IntentInstructionMax     int
			IntentTargetSecondsMin, IntentTargetSecondsMax int
			CreationSpecTitleMax, CreationSpecGoalMax      int
			CreationSpecAudienceMax                        int
			CreationSpecPhasesMax                          int
			CreationSpecConstraintsMax                     int
			CreationSpecAcceptanceMin                      int
			CreationSpecAcceptanceMax                      int
			SectionsMax, ElementsMax, SlotsMax             int
			SectionTitleMax, SectionObjectiveMax           int
			ElementTitleMax, ElementNarrativeMax           int
			ElementDurationMin, ElementDurationMax         int
			ElementDependenciesMax, SlotsPerElementMax     int
			SlotPurposeMax                                 int
			TotalDurationMin, TotalDurationMax             int
			TargetTolerancePercent, TargetToleranceMinimum int
		}{
			intentInstructionMin, intentInstructionMax, intentTargetMin, intentTargetMax,
			creationTitleMax, creationGoalMax, creationAudienceMax, creationPhasesMax,
			creationListsMax, creationAcceptanceMin, creationListsMax,
			maxSections, maxElements, maxSlots, sectionTitleMax, sectionObjectiveMax,
			elementTitleMax, elementNarrativeMax, elementDurationMin, elementDurationMax,
			maxElementDeps, maxSlotsPerElement, slotPurposeMax, totalDurationMin, totalDurationMax,
			int(targetToleranceRate * 100), targetToleranceMin,
		},
		Invariants: []string{
			"creation_spec_phase_keys_unique", "candidate_keys_dense_and_ordered", "candidate_phase_keys_from_creation_spec",
			"every_section_has_element", "dependency_keys_unique", "slot_owner_exists", "content_digest_canonical_json",
		},
	}, "validator artifact")
}

// DAGValidatorArtifactDigest 计算独立依赖图校验器的真实策略摘要。
func DAGValidatorArtifactDigest() string {
	return mustArtifactDigest(struct {
		SchemaVersion string   `json:"schema_version"`
		Version       string   `json:"version"`
		ElementLimit  int      `json:"element_limit"`
		ElementKey    string   `json:"element_key_pattern"`
		SlotKey       string   `json:"slot_key_pattern"`
		Rules         []string `json:"rules"`
	}{
		SchemaVersion: "plan_storyboard.dag-validator-artifact.v1", Version: DAGValidatorVersion,
		ElementLimit: maxElements, ElementKey: elementKeyPattern.String(), SlotKey: slotKeyPattern.String(),
		Rules: []string{"unique_elements", "no_self_dependency", "no_unknown_dependency", "acyclic_kahn", "slot_owner_exists"},
	}, "DAG validator artifact")
}

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
		SchemaVersion: "plan_storyboard.tool-definition-artifact.v1",
		Name:          info.Name, Description: info.Desc, Parameters: params,
	}, "tool definition")
}

func mustArtifactDigest(value any, label string) string {
	digest, err := artifactDigest(value, label)
	if err != nil {
		panic(err)
	}
	return digest
}

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
