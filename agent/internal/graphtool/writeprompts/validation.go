package writeprompts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const (
	storyboardDraftSchemaVersion   = "storyboard.preview.draft.v1"
	exactTargetDigestSchemaVersion = "prompt.preview.exact-target-set.digest.v1"
)

var (
	// ErrNoTargets 表示 Source Storyboard 没有任何可写 Prompt 的 Slot。
	ErrNoTargets = errors.New("prompt preview source has no targets")
	// ErrTargetBudgetExceeded 表示 Source Storyboard 的完整 Slot 集超过冻结策略预算。
	ErrTargetBudgetExceeded = errors.New("prompt preview target budget exceeded")

	elementKeyPattern = regexp.MustCompile(`^element_([1-9]|1[0-9]|2[0-4])$`)
	slotKeyPattern    = regexp.MustCompile(`^slot_([1-9]|[1-8][0-9]|9[0-6])$`)
)

// DecodeIntent 对 Tool 原始 JSON 执行大小、Unicode、重复字段、未知字段和尾随值校验。
func DecodeIntent(encoded []byte) (Intent, error) {
	if len(encoded) == 0 || len(encoded) > maxJSONBytes || !utf8.Valid(encoded) || !validJSONSurrogateEscapes(encoded) {
		return Intent{}, fmt.Errorf("decode prompt preview intent: invalid JSON size or UTF-8")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return Intent{}, fmt.Errorf("decode prompt preview intent: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var intent Intent
	if err := decoder.Decode(&intent); err != nil {
		return Intent{}, fmt.Errorf("decode prompt preview intent: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Intent{}, fmt.Errorf("decode prompt preview intent: %w", err)
	}
	if err := ValidateIntent(intent); err != nil {
		return Intent{}, err
	}
	return intent, nil
}

// ValidateIntent 校验模型唯一可控的写作指令与可选输出语言。
func ValidateIntent(intent Intent) error {
	if intent.SchemaVersion != IntentSchemaVersion ||
		!validText(intent.WritingInstruction, intentInstructionMin, intentInstructionMax, false) ||
		(intent.OutputLanguage != "" && !validLocale(intent.OutputLanguage)) {
		return fmt.Errorf("validate prompt preview intent: invalid schema_version, writing_instruction or output_language")
	}
	return nil
}

// IntentDigest 计算具名严格 Intent JSON 的小写 SHA-256。
func IntentDigest(intent Intent) (string, error) {
	if err := ValidateIntent(intent); err != nil {
		return "", err
	}
	return digestJSON(intent, "prompt preview intent")
}

// ValidatePolicy 校验启动时冻结的目标预算与缺省语言策略。
func ValidatePolicy(policy Policy) error {
	if policy.Version != RuntimePolicyVersion || policy.MaxTargets < 1 || policy.MaxTargets > maxSourceSlots ||
		!validLocale(policy.DefaultOutputLanguage) || policy.MaxCommandResends < 0 || policy.MaxCommandResends > 1 {
		return fmt.Errorf("validate prompt preview policy: invalid version, target budget, language or resend budget")
	}
	return nil
}

// ValidateTrustedContext 校验 Runtime 私有身份、Source 引用、Fence 与全部实现版本 Pin。
func ValidateTrustedContext(trusted TrustedContext) error {
	for _, value := range []string{
		trusted.RequestID, trusted.UserID, trusted.ProjectID, trusted.SessionID, trusted.InputID,
		trusted.TurnID, trusted.RunID, trusted.ToolCallID, trusted.BusinessCommandID,
	} {
		if !canonicalUUIDv7(value) {
			return fmt.Errorf("validate prompt preview trusted context: invalid UUIDv7")
		}
	}
	if !validText(trusted.Owner, trustedOwnerMin, trustedOwnerMax, false) || trusted.FenceToken < 1 ||
		!validStoryboardPreviewRef(trusted.StoryboardPreviewRef) || trusted.PromptVersion != PromptVersion ||
		trusted.ValidatorVersion != ValidatorVersion || trusted.ExactSetValidatorVersion != ExactSetValidatorVersion {
		return fmt.Errorf("validate prompt preview trusted context: invalid owner, fence, source ref or version pin")
	}
	if err := ValidatePolicy(trusted.Policy); err != nil {
		return err
	}
	return nil
}

// ValidateGenerationContext 校验 Business 一致读取快照与 Runtime 冻结引用完全绑定。
// Source Content 是为 Prompt 裁剪后的最小投影，因此这里只校验 Business 已返回的权威原文摘要，不能用投影重算原聚合摘要。
func ValidateGenerationContext(value GenerationContext, trusted TrustedContext) error {
	if err := ValidateTrustedContext(trusted); err != nil {
		return err
	}
	resource := value.Storyboard
	if !canonicalUUIDv7(value.ProjectID) || value.ProjectID != trusted.ProjectID || value.ProjectVersion < 1 ||
		!validText(value.ProjectTitle, projectTitleMin, projectTitleMax, false) || resource.ID != trusted.StoryboardPreviewRef.ID ||
		resource.ProjectID != trusted.ProjectID || resource.Version != trusted.StoryboardPreviewRef.Version ||
		resource.Status != "draft" || resource.ContentDigest != trusted.StoryboardPreviewRef.ContentDigest ||
		!validLowerSHA256(resource.ContentDigest) {
		return fmt.Errorf("validate prompt generation context: identity, version or digest mismatch")
	}
	if err := validateStoryboardContent(resource.Content); err != nil {
		return err
	}
	return nil
}

// ResolveExactTargets 从完整 Source Slot 集确定性生成目标、最终语言与覆盖全部实现 Pin 的 Scope 摘要。
func ResolveExactTargets(value GenerationContext, intent Intent, intentDigest string, trusted TrustedContext) ([]PromptTarget, string, string, error) {
	if err := ValidateGenerationContext(value, trusted); err != nil {
		return nil, "", "", err
	}
	if err := ValidateIntent(intent); err != nil {
		return nil, "", "", err
	}
	recomputedIntentDigest, err := IntentDigest(intent)
	if err != nil || recomputedIntentDigest != intentDigest {
		return nil, "", "", fmt.Errorf("resolve prompt targets: intent digest mismatch")
	}
	if len(value.Storyboard.Content.Slots) == 0 {
		return nil, "", "", ErrNoTargets
	}
	if len(value.Storyboard.Content.Slots) > trusted.Policy.MaxTargets {
		return nil, "", "", ErrTargetBudgetExceeded
	}

	elements := make(map[string]StoryboardElement, len(value.Storyboard.Content.Elements))
	for _, element := range value.Storyboard.Content.Elements {
		elements[element.Key] = element
	}
	targets := make([]PromptTarget, len(value.Storyboard.Content.Slots))
	for index, slot := range value.Storyboard.Content.Slots {
		element := elements[slot.ElementKey]
		mediaKind, ok := mediaKindForSlotType(slot.SlotType)
		if !ok {
			return nil, "", "", fmt.Errorf("resolve prompt targets: invalid slot type")
		}
		targets[index] = PromptTarget{
			TargetLocalKey: slot.Key, ElementLocalKey: slot.ElementKey, ElementTitle: element.Title,
			NarrativePurpose: element.NarrativePurpose, SlotType: slot.SlotType, MediaKind: mediaKind,
			Purpose: slot.Purpose, Required: slot.Required,
		}
	}
	sort.Slice(targets, func(left, right int) bool {
		leftElement := elements[targets[left].ElementLocalKey].Order
		rightElement := elements[targets[right].ElementLocalKey].Order
		if leftElement != rightElement {
			return leftElement < rightElement
		}
		return localKeyNumber(targets[left].TargetLocalKey) < localKeyNumber(targets[right].TargetLocalKey)
	})
	if err := validateExactTargets(targets); err != nil {
		return nil, "", "", err
	}

	effectiveLanguage := intent.OutputLanguage
	if effectiveLanguage == "" {
		effectiveLanguage = trusted.Policy.DefaultOutputLanguage
	}
	digest, err := exactTargetSetDigest(trusted.StoryboardPreviewRef, targets, intentDigest, trusted.Policy)
	if err != nil {
		return nil, "", "", err
	}
	return cloneTargets(targets), digest, effectiveLanguage, nil
}

// DecodeAndValidateCandidate 严格解析模型候选并仅校验协议、文本与单项数组边界。
// 目标多、少、重复、未知和顺序由独立 exact-set Validator 处理。
func DecodeAndValidateCandidate(encoded []byte) (Candidate, error) {
	if len(encoded) == 0 || len(encoded) > maxJSONBytes || !utf8.Valid(encoded) || !validJSONSurrogateEscapes(encoded) {
		return Candidate{}, fmt.Errorf("validate prompt candidate: invalid JSON size or UTF-8")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return Candidate{}, fmt.Errorf("validate prompt candidate: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var candidate Candidate
	if err := decoder.Decode(&candidate); err != nil {
		return Candidate{}, fmt.Errorf("validate prompt candidate: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Candidate{}, fmt.Errorf("validate prompt candidate: %w", err)
	}
	if err := validateCandidate(candidate); err != nil {
		return Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

// ValidateExactTargetSet 校验候选与冻结目标在数量、顺序和 key 上完全相同，并回填全部可信字段。
func ValidateExactTargetSet(candidate Candidate, targets []PromptTarget, outputLanguage string, sourceRef StoryboardPreviewRef) (Content, error) {
	if err := validateCandidate(candidate); err != nil {
		return Content{}, err
	}
	if err := validateExactTargets(targets); err != nil {
		return Content{}, err
	}
	if !validLocale(outputLanguage) || !validStoryboardPreviewRef(sourceRef) || len(candidate.Prompts) != len(targets) {
		return Content{}, fmt.Errorf("validate exact prompt target set: target count, language or source ref mismatch")
	}
	prompts := make([]PromptEntry, len(targets))
	for index, target := range targets {
		candidatePrompt := candidate.Prompts[index]
		if candidatePrompt.TargetLocalKey != target.TargetLocalKey {
			return Content{}, fmt.Errorf("validate exact prompt target set: target key or order mismatch")
		}
		prompts[index] = PromptEntry{
			TargetLocalKey: target.TargetLocalKey, ElementLocalKey: target.ElementLocalKey,
			SlotType: target.SlotType, MediaKind: target.MediaKind, Purpose: target.Purpose, Required: target.Required,
			PositivePrompt:      candidatePrompt.PositivePrompt,
			NegativeConstraints: append([]string{}, candidatePrompt.NegativeConstraints...),
			OutputLanguage:      outputLanguage,
		}
	}
	content := Content{
		SchemaVersion: DraftSchemaVersion, Mode: "storyboard_preview",
		SourceStoryboardPreviewRef: sourceRef, Prompts: prompts,
	}
	if err := ValidateContent(content); err != nil {
		return Content{}, err
	}
	return cloneContent(content), nil
}

// CandidateDigest 计算严格候选 canonical JSON 摘要。
func CandidateDigest(candidate Candidate) (string, error) {
	if err := validateCandidate(candidate); err != nil {
		return "", err
	}
	return digestJSON(candidate, "prompt candidate")
}

// ValidateContent 校验 Agent 与 Business 共用 Prompt Draft Content 的全部非上下文不变量。
func ValidateContent(content Content) error {
	if content.SchemaVersion != DraftSchemaVersion || content.Mode != "storyboard_preview" ||
		!validStoryboardPreviewRef(content.SourceStoryboardPreviewRef) || content.Prompts == nil ||
		len(content.Prompts) < 1 || len(content.Prompts) > maxSourceSlots {
		return fmt.Errorf("validate prompt preview content: invalid schema, mode, source ref or collection boundary")
	}
	seen := make(map[string]struct{}, len(content.Prompts))
	outputLanguage := content.Prompts[0].OutputLanguage
	for _, prompt := range content.Prompts {
		expectedMediaKind, ok := mediaKindForSlotType(prompt.SlotType)
		if !slotKeyPattern.MatchString(prompt.TargetLocalKey) || !elementKeyPattern.MatchString(prompt.ElementLocalKey) ||
			!ok || prompt.MediaKind != expectedMediaKind || !validText(prompt.Purpose, slotPurposeMin, slotPurposeMax, false) ||
			!validText(prompt.PositivePrompt, positivePromptMin, positivePromptMax, false) ||
			prompt.NegativeConstraints == nil || len(prompt.NegativeConstraints) > maxNegativeConstraints ||
			!validUniqueStrings(prompt.NegativeConstraints, negativeConstraintMin, negativeConstraintMax) ||
			!validLocale(prompt.OutputLanguage) || prompt.OutputLanguage != outputLanguage {
			return fmt.Errorf("validate prompt preview content: invalid prompt entry")
		}
		if _, duplicated := seen[prompt.TargetLocalKey]; duplicated {
			return fmt.Errorf("validate prompt preview content: duplicate target")
		}
		seen[prompt.TargetLocalKey] = struct{}{}
	}
	encoded, err := json.Marshal(content)
	if err != nil || len(encoded) == 0 || len(encoded) > maxJSONBytes {
		return fmt.Errorf("validate prompt preview content: canonical JSON exceeds boundary")
	}
	return nil
}

// ContentDigest 计算 Business Prompt Draft Content 的冻结 JSON 摘要。
func ContentDigest(content Content) (string, error) {
	if err := ValidateContent(content); err != nil {
		return "", err
	}
	return digestJSON(content, "prompt preview content")
}

// SaveRequestDigest 按跨 Module 固定字段顺序计算 Agent 到 Business 保存命令摘要。
func SaveRequestDigest(command DraftCommand) (string, error) {
	trusted := command.TrustedContext
	if err := ValidateTrustedContext(trusted); err != nil {
		return "", err
	}
	if command.DomainContext.ProjectID != trusted.ProjectID || command.DomainContext.ProjectVersion < 1 ||
		command.DomainContext.Storyboard.ID != trusted.StoryboardPreviewRef.ID ||
		command.DomainContext.Storyboard.Version != trusted.StoryboardPreviewRef.Version ||
		command.DomainContext.Storyboard.ContentDigest != trusted.StoryboardPreviewRef.ContentDigest ||
		!validLowerSHA256(command.ExactTargetSetDigest) || len(command.Targets) < 1 ||
		command.ResendLimit != trusted.Policy.MaxCommandResends {
		return "", fmt.Errorf("compute prompt preview save request digest: invalid stable command fields")
	}
	if err := validateExactTargets(command.Targets); err != nil {
		return "", err
	}
	if err := validateContentAgainstTargets(command.Content, command.Targets, trusted.StoryboardPreviewRef); err != nil {
		return "", err
	}
	wire := saveDigestWire{
		SchemaVersion: SaveDigestSchemaVersion, UserID: trusted.UserID, ProjectID: trusted.ProjectID,
		ExpectedProjectVersion: command.DomainContext.ProjectVersion, StoryboardPreviewRef: trusted.StoryboardPreviewRef,
		ToolCallID: trusted.ToolCallID, PromptVersion: trusted.PromptVersion,
		ValidatorVersion: trusted.ValidatorVersion, ExactSetValidatorVersion: trusted.ExactSetValidatorVersion,
		ExactTargetSetDigest: command.ExactTargetSetDigest, Content: command.Content,
	}
	return digestJSON(wire, "prompt preview save request")
}

// ValidateResourceForCommand 校验 Business 回传资源自洽且精确绑定原保存命令。
func ValidateResourceForCommand(resource Resource, command DraftCommand) error {
	recomputedRequestDigest, requestErr := SaveRequestDigest(command)
	if requestErr != nil || !validLowerSHA256(command.RequestDigest) || recomputedRequestDigest != command.RequestDigest {
		return fmt.Errorf("validate prompt preview resource: command request digest mismatch")
	}
	if !canonicalUUIDv7(resource.PromptPreviewID) || resource.ProjectID != command.TrustedContext.ProjectID ||
		resource.StoryboardPreviewRef != command.TrustedContext.StoryboardPreviewRef || resource.Version != 1 ||
		resource.Status != "draft" || !validLowerSHA256(resource.ContentDigest) ||
		resource.ExactTargetSetDigest != command.ExactTargetSetDigest {
		return fmt.Errorf("validate prompt preview resource: invalid identity, state or source binding")
	}
	digest, err := ContentDigest(resource.Content)
	if err != nil || digest != resource.ContentDigest {
		return fmt.Errorf("validate prompt preview resource: content digest mismatch")
	}
	expectedDigest, err := ContentDigest(command.Content)
	if err != nil || expectedDigest != resource.ContentDigest {
		return fmt.Errorf("validate prompt preview resource: command content mismatch")
	}
	return nil
}

// ValidateCard 校验持久化 Workspace/Event 共用 Card 的完整安全边界。
func ValidateCard(card Card) error {
	if card.SchemaVersion != CardSchemaVersion || !canonicalUUIDv7(card.InputID) || !canonicalUUIDv7(card.TurnID) ||
		!canonicalUUIDv7(card.RunID) || !canonicalUUIDv7(card.ToolCallID) || card.ResultCode == "" ||
		card.UpdatedAt.IsZero() || card.UpdatedAt.Location() != time.UTC {
		return fmt.Errorf("validate prompt preview card: invalid common identity or state")
	}
	switch card.Status {
	case "completed":
		if card.ResultCode != ResultCodeCompleted || !canonicalUUIDv7(card.PromptPreviewID) ||
			!canonicalUUIDv7(card.ProjectID) || card.StoryboardPreviewRef == nil ||
			!validStoryboardPreviewRef(*card.StoryboardPreviewRef) || card.Version != 1 ||
			!validLowerSHA256(card.ContentDigest) || card.TargetCount < 1 || card.TargetCount != len(card.Prompts) ||
			card.Prompts == nil || card.FailureKind != "" || card.Summary != "" || card.Retryable != nil {
			return fmt.Errorf("validate prompt preview card: invalid completed shape")
		}
		content := Content{
			SchemaVersion: DraftSchemaVersion, Mode: "storyboard_preview",
			SourceStoryboardPreviewRef: *card.StoryboardPreviewRef, Prompts: card.Prompts,
		}
		digest, err := ContentDigest(content)
		if err != nil || digest != card.ContentDigest {
			return fmt.Errorf("validate prompt preview card: content digest mismatch")
		}
	case "failed":
		if card.PromptPreviewID != "" || card.ProjectID != "" || card.StoryboardPreviewRef != nil ||
			card.Version != 0 || card.ContentDigest != "" || card.TargetCount != 0 || len(card.Prompts) != 0 ||
			!validFailedResultCode(card.ResultCode) || (card.FailureKind != "tool" && card.FailureKind != "runtime") ||
			!validText(card.Summary, resultSummaryMin, resultSummaryMax, false) || card.Retryable == nil {
			return fmt.Errorf("validate prompt preview card: invalid failed shape")
		}
	default:
		return fmt.Errorf("validate prompt preview card: invalid status")
	}
	return nil
}

// ValidateTerminalResult 校验可冻结 completed/failed Result；Recovery 绝不能进入终态。
func ValidateTerminalResult(result Result, trusted TrustedContext) error {
	if err := ValidateTrustedContext(trusted); err != nil {
		return err
	}
	if result.SchemaVersion != ResultSchemaVersion || result.InvocationRef.ToolCallID != trusted.ToolCallID ||
		result.InvocationRef.BusinessCommandID != trusted.BusinessCommandID {
		return fmt.Errorf("validate prompt preview result: invocation mismatch")
	}
	switch result.Status {
	case "completed":
		if result.ResultCode != ResultCodeCompleted || result.PromptPreviewRef == nil || result.StoryboardPreviewRef == nil ||
			*result.StoryboardPreviewRef != trusted.StoryboardPreviewRef || result.TargetCount < 1 || result.Card == nil ||
			result.Summary != "" || result.Retryable != nil {
			return fmt.Errorf("validate prompt preview result: invalid completed shape")
		}
		if err := ValidateCard(*result.Card); err != nil {
			return err
		}
		if result.PromptPreviewRef.ID != result.Card.PromptPreviewID || result.PromptPreviewRef.Version != result.Card.Version ||
			result.PromptPreviewRef.ContentDigest != result.Card.ContentDigest || result.TargetCount != result.Card.TargetCount ||
			result.Card.ProjectID != trusted.ProjectID || result.Card.StoryboardPreviewRef == nil ||
			*result.Card.StoryboardPreviewRef != trusted.StoryboardPreviewRef || result.Card.InputID != trusted.InputID ||
			result.Card.TurnID != trusted.TurnID || result.Card.RunID != trusted.RunID ||
			result.Card.ToolCallID != trusted.ToolCallID || result.Card.Status != result.Status ||
			result.Card.ResultCode != result.ResultCode {
			return fmt.Errorf("validate prompt preview result: resource/card mismatch")
		}
	case "failed":
		if !validFailedResultCode(result.ResultCode) || result.PromptPreviewRef != nil || result.StoryboardPreviewRef != nil || result.TargetCount != 0 ||
			result.Card == nil || !validText(result.Summary, resultSummaryMin, resultSummaryMax, false) || result.Retryable == nil {
			return fmt.Errorf("validate prompt preview result: invalid failed shape")
		}
		if err := ValidateCard(*result.Card); err != nil {
			return err
		}
		if result.Card.InputID != trusted.InputID || result.Card.TurnID != trusted.TurnID ||
			result.Card.RunID != trusted.RunID || result.Card.ToolCallID != trusted.ToolCallID ||
			result.Card.Status != result.Status || result.Card.ResultCode != result.ResultCode ||
			result.Card.FailureKind != "tool" || result.Card.Summary != result.Summary ||
			result.Card.Retryable == nil || *result.Card.Retryable != *result.Retryable {
			return fmt.Errorf("validate prompt preview result: failure card mismatch")
		}
	default:
		return fmt.Errorf("validate prompt preview result: invalid terminal status")
	}
	return nil
}

// validFailedResultCode 只允许设计批准的确定性 Tool 失败码进入终态；技术错误必须返回 Runtime error。
func validFailedResultCode(code string) bool {
	switch code {
	case ResultCodeInvalidArgument, ResultCodeStoryboardNotFound, ResultCodeStoryboardConflict,
		ResultCodeNoTargets, ResultCodeTargetBudgetExceeded, ResultCodeCandidateInvalid,
		ResultCodeExactSetInvalid, ResultCodeBusinessConflict, ResultCodeBusinessDisabled:
		return true
	default:
		return false
	}
}

// saveDigestWire 固定跨 Module 保存摘要的字段顺序，禁止改为 map。
type saveDigestWire struct {
	// SchemaVersion 是保存请求摘要协议版本。
	SchemaVersion string `json:"schema_version"`
	// UserID 是 Project Owner UUIDv7。
	UserID string `json:"user_id"`
	// ProjectID 是目标 Project UUIDv7。
	ProjectID string `json:"project_id"`
	// ExpectedProjectVersion 是读取时冻结的 Project 版本。
	ExpectedProjectVersion int64 `json:"expected_project_version"`
	// StoryboardPreviewRef 是保存 Guard 使用的 Source 引用。
	StoryboardPreviewRef StoryboardPreviewRef `json:"storyboard_preview_ref"`
	// ToolCallID 是来源 ToolCall UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// PromptVersion 是冻结 Prompt 版本。
	PromptVersion string `json:"prompt_version"`
	// ValidatorVersion 是候选校验器版本。
	ValidatorVersion string `json:"validator_version"`
	// ExactSetValidatorVersion 是目标全集校验器版本。
	ExactSetValidatorVersion string `json:"exact_set_validator_version"`
	// ExactTargetSetDigest 是冻结 Scope 摘要。
	ExactTargetSetDigest string `json:"exact_target_set_digest"`
	// Content 是双 Validator 通过的完整正文。
	Content Content `json:"content"`
}

// exactTargetDigestEntry 是 Scope 摘要中不含 Prompt 辅助文本的冻结目标。
type exactTargetDigestEntry struct {
	// TargetLocalKey 是 Source Slot 局部键。
	TargetLocalKey string `json:"target_local_key"`
	// ElementLocalKey 是 Source Element 局部键。
	ElementLocalKey string `json:"element_local_key"`
	// SlotType 是 Source Slot 类型。
	SlotType string `json:"slot_type"`
	// MediaKind 是确定性媒体映射。
	MediaKind string `json:"media_kind"`
	// Purpose 是 Source Slot 用途。
	Purpose string `json:"purpose"`
	// Required 是 Source Slot 必须性。
	Required bool `json:"required"`
}

// exactTargetSetDigestWire 固定 Scope 摘要的字段顺序与全部实现工件 Pin。
type exactTargetSetDigestWire struct {
	// SchemaVersion 是 exact target set 摘要协议版本。
	SchemaVersion string `json:"schema_version"`
	// StoryboardPreviewRef 是 Source 精确引用。
	StoryboardPreviewRef StoryboardPreviewRef `json:"storyboard_preview_ref"`
	// Targets 是已排序的完整目标集。
	Targets []exactTargetDigestEntry `json:"targets"`
	// IntentDigest 是 canonical Intent 摘要。
	IntentDigest string `json:"intent_digest"`
	// ToolDefinitionVersion 是 Tool Definition 版本。
	ToolDefinitionVersion string `json:"tool_definition_version"`
	// ToolDefinitionDigest 是真实 Tool Schema 工件摘要。
	ToolDefinitionDigest string `json:"tool_definition_digest"`
	// PromptKey 是 Prompt 稳定键。
	PromptKey string `json:"prompt_key"`
	// PromptVersion 是 Prompt 稳定版本。
	PromptVersion string `json:"prompt_version"`
	// PromptArtifactDigest 是 Prompt 内容工件摘要。
	PromptArtifactDigest string `json:"prompt_artifact_digest"`
	// ValidatorVersion 是候选 Validator 版本。
	ValidatorVersion string `json:"validator_version"`
	// ValidatorArtifactDigest 是候选 Validator 工件摘要。
	ValidatorArtifactDigest string `json:"validator_artifact_digest"`
	// ExactSetValidatorVersion 是全集 Validator 版本。
	ExactSetValidatorVersion string `json:"exact_set_validator_version"`
	// ExactSetValidatorArtifactDigest 是全集 Validator 工件摘要。
	ExactSetValidatorArtifactDigest string `json:"exact_set_validator_artifact_digest"`
	// RuntimePolicyVersion 是启动策略版本。
	RuntimePolicyVersion string `json:"runtime_policy_version"`
	// RuntimePolicyDigest 是具体策略值摘要。
	RuntimePolicyDigest string `json:"runtime_policy_digest"`
}

// exactTargetSetDigest 绑定 Source、全部目标、Intent、Tool/Prompt/Validator 与具体 Policy，防止模型后静默换 Scope。
func exactTargetSetDigest(sourceRef StoryboardPreviewRef, targets []PromptTarget, intentDigest string, policy Policy) (string, error) {
	if !validStoryboardPreviewRef(sourceRef) || !validLowerSHA256(intentDigest) {
		return "", fmt.Errorf("compute exact prompt target set digest: invalid source or intent digest")
	}
	if err := validateExactTargets(targets); err != nil {
		return "", err
	}
	policyDigest, err := RuntimePolicyDigest(policy)
	if err != nil {
		return "", err
	}
	entries := make([]exactTargetDigestEntry, len(targets))
	for index, target := range targets {
		entries[index] = exactTargetDigestEntry{
			TargetLocalKey: target.TargetLocalKey, ElementLocalKey: target.ElementLocalKey,
			SlotType: target.SlotType, MediaKind: target.MediaKind, Purpose: target.Purpose, Required: target.Required,
		}
	}
	wire := exactTargetSetDigestWire{
		SchemaVersion: exactTargetDigestSchemaVersion, StoryboardPreviewRef: sourceRef, Targets: entries,
		IntentDigest: intentDigest, ToolDefinitionVersion: ToolDefinitionVersion, ToolDefinitionDigest: ToolDefinitionDigest(),
		PromptKey: PromptKey, PromptVersion: PromptVersion, PromptArtifactDigest: PromptArtifactDigest(),
		ValidatorVersion: ValidatorVersion, ValidatorArtifactDigest: ValidatorArtifactDigest(),
		ExactSetValidatorVersion: ExactSetValidatorVersion, ExactSetValidatorArtifactDigest: ExactSetValidatorArtifactDigest(),
		RuntimePolicyVersion: RuntimePolicyVersion, RuntimePolicyDigest: policyDigest,
	}
	return digestJSON(wire, "exact prompt target set")
}

// validateStoryboardContent 校验 Business 最小投影的 dense Element/Slot、Owner 引用与文本边界；零 Slot 留给 Scope 节点形成稳定失败。
func validateStoryboardContent(content StoryboardContent) error {
	if content.SchemaVersion != storyboardDraftSchemaVersion ||
		!validText(content.Title, sourceTitleMin, sourceTitleMax, false) ||
		!validText(content.Summary, sourceSummaryMin, sourceSummaryMax, false) ||
		content.Elements == nil || len(content.Elements) < 1 || len(content.Elements) > maxSourceElements ||
		content.Slots == nil || len(content.Slots) > maxSourceSlots {
		return fmt.Errorf("validate prompt source content: invalid schema or collection boundary")
	}
	elements := make(map[string]struct{}, len(content.Elements))
	for index, element := range content.Elements {
		if element.Key != fmt.Sprintf("element_%d", index+1) || element.Order != index+1 ||
			!elementKeyPattern.MatchString(element.Key) || !validText(element.Title, elementTitleMin, elementTitleMax, false) ||
			!validText(element.NarrativePurpose, elementNarrativeMin, elementNarrativeMax, false) {
			return fmt.Errorf("validate prompt source content: invalid element")
		}
		elements[element.Key] = struct{}{}
	}
	slots := make(map[string]struct{}, len(content.Slots))
	for _, slot := range content.Slots {
		if !slotKeyPattern.MatchString(slot.Key) || !validSlotType(slot.SlotType) ||
			!validText(slot.Purpose, slotPurposeMin, slotPurposeMax, false) {
			return fmt.Errorf("validate prompt source content: invalid slot")
		}
		if _, exists := elements[slot.ElementKey]; !exists {
			return fmt.Errorf("validate prompt source content: slot owner does not exist")
		}
		if _, duplicated := slots[slot.Key]; duplicated {
			return fmt.Errorf("validate prompt source content: duplicate slot")
		}
		slots[slot.Key] = struct{}{}
	}
	for index := 1; index <= len(content.Slots); index++ {
		if _, exists := slots[fmt.Sprintf("slot_%d", index)]; !exists {
			return fmt.Errorf("validate prompt source content: slot keys are not dense")
		}
	}
	return nil
}

// validateCandidate 只校验模型可控协议和文本，不在此混入可信目标全集判断。
func validateCandidate(candidate Candidate) error {
	if candidate.SchemaVersion != CandidateSchemaVersion || candidate.Prompts == nil ||
		len(candidate.Prompts) < 1 || len(candidate.Prompts) > maxSourceSlots {
		return fmt.Errorf("validate prompt candidate: invalid schema or collection boundary")
	}
	for index, prompt := range candidate.Prompts {
		if !slotKeyPattern.MatchString(prompt.TargetLocalKey) ||
			!validText(prompt.PositivePrompt, positivePromptMin, positivePromptMax, false) ||
			prompt.NegativeConstraints == nil || len(prompt.NegativeConstraints) > maxNegativeConstraints ||
			!validUniqueStrings(prompt.NegativeConstraints, negativeConstraintMin, negativeConstraintMax) {
			return fmt.Errorf("validate prompt candidate: invalid prompt entry at index %d", index)
		}
	}
	return nil
}

// validateExactTargets 校验冻结目标非空、唯一、媒体映射正确且按 Element/Slot 数值稳定排序。
func validateExactTargets(targets []PromptTarget) error {
	if targets == nil || len(targets) < 1 || len(targets) > maxSourceSlots {
		return fmt.Errorf("validate exact prompt targets: invalid collection boundary")
	}
	seen := make(map[string]struct{}, len(targets))
	lastElement := 0
	lastSlot := 0
	for _, target := range targets {
		elementNumber := localKeyNumber(target.ElementLocalKey)
		slotNumber := localKeyNumber(target.TargetLocalKey)
		expectedMediaKind, ok := mediaKindForSlotType(target.SlotType)
		if !slotKeyPattern.MatchString(target.TargetLocalKey) || !elementKeyPattern.MatchString(target.ElementLocalKey) ||
			!validText(target.ElementTitle, elementTitleMin, elementTitleMax, false) ||
			!validText(target.NarrativePurpose, elementNarrativeMin, elementNarrativeMax, false) ||
			!ok || target.MediaKind != expectedMediaKind || !validText(target.Purpose, slotPurposeMin, slotPurposeMax, false) {
			return fmt.Errorf("validate exact prompt targets: invalid target")
		}
		if _, duplicated := seen[target.TargetLocalKey]; duplicated {
			return fmt.Errorf("validate exact prompt targets: duplicate target")
		}
		if elementNumber < lastElement || (elementNumber == lastElement && slotNumber <= lastSlot) {
			return fmt.Errorf("validate exact prompt targets: unstable target order")
		}
		seen[target.TargetLocalKey] = struct{}{}
		lastElement = elementNumber
		lastSlot = slotNumber
	}
	for index := 1; index <= len(targets); index++ {
		if _, exists := seen[fmt.Sprintf("slot_%d", index)]; !exists {
			return fmt.Errorf("validate exact prompt targets: target keys are not dense")
		}
	}
	return nil
}

// validateContentAgainstTargets 逐项复核 Content 的全部可信字段来自原目标，防止保存前被模型或适配器篡改。
func validateContentAgainstTargets(content Content, targets []PromptTarget, sourceRef StoryboardPreviewRef) error {
	if err := ValidateContent(content); err != nil {
		return err
	}
	if content.SourceStoryboardPreviewRef != sourceRef || len(content.Prompts) != len(targets) {
		return fmt.Errorf("validate prompt preview content: source or target count mismatch")
	}
	for index, target := range targets {
		prompt := content.Prompts[index]
		if prompt.TargetLocalKey != target.TargetLocalKey || prompt.ElementLocalKey != target.ElementLocalKey ||
			prompt.SlotType != target.SlotType || prompt.MediaKind != target.MediaKind ||
			prompt.Purpose != target.Purpose || prompt.Required != target.Required {
			return fmt.Errorf("validate prompt preview content: trusted target fields mismatch")
		}
	}
	return nil
}

// mediaKindForSlotType 执行设计冻结的五类 Slot 到四类媒体的唯一确定性映射。
func mediaKindForSlotType(slotType string) (string, bool) {
	switch slotType {
	case "image":
		return "image", true
	case "video":
		return "video", true
	case "audio", "voiceover":
		return "audio", true
	case "caption":
		return "text", true
	default:
		return "", false
	}
}

// localKeyNumber 只提取已通过正则的局部键数字用于稳定排序；非法格式返回零并由调用方失败关闭。
func localKeyNumber(value string) int {
	separator := strings.LastIndexByte(value, '_')
	if separator < 0 || separator == len(value)-1 {
		return 0
	}
	number, err := strconv.Atoi(value[separator+1:])
	if err != nil {
		return 0
	}
	return number
}

// cloneTargets 隔离 Graph State 与调用方切片，避免目标顺序在摘要后被修改。
func cloneTargets(values []PromptTarget) []PromptTarget {
	result := make([]PromptTarget, len(values))
	copy(result, values)
	return result
}

// cloneCandidate 深拷贝候选列表，并保留协议允许的非 null 空 negative_constraints。
func cloneCandidate(value Candidate) Candidate {
	result := Candidate{SchemaVersion: value.SchemaVersion, Prompts: make([]CandidatePrompt, len(value.Prompts))}
	copy(result.Prompts, value.Prompts)
	for index := range result.Prompts {
		result.Prompts[index].NegativeConstraints = append([]string{}, value.Prompts[index].NegativeConstraints...)
	}
	return result
}

// cloneContent 深拷贝 Business 命令正文，并保留协议允许的非 null 空 negative_constraints。
func cloneContent(value Content) Content {
	result := value
	result.Prompts = make([]PromptEntry, len(value.Prompts))
	copy(result.Prompts, value.Prompts)
	for index := range result.Prompts {
		result.Prompts[index].NegativeConstraints = append([]string{}, value.Prompts[index].NegativeConstraints...)
	}
	return result
}

// digestJSON 对具名 typed 值使用 Go JSON 固定字段顺序与默认 HTML escape 计算摘要，并执行 128 KiB 上限。
func digestJSON(value any, label string) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) == 0 || len(encoded) > maxJSONBytes {
		return "", fmt.Errorf("compute %s digest: canonical JSON failed", label)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// requireJSONEOF 要求首个 JSON 值后立即 EOF，拒绝任何尾随对象或标量。
func requireJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

// rejectDuplicateJSONFields 递归扫描完整 JSON Token 树，拒绝任意层级重复字段与尾随值。
func rejectDuplicateJSONFields(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return fmt.Errorf("JSON contains trailing value")
	}
	return nil
}

// consumeUniqueJSONValue 递归消费单个非 null JSON 值，并在每个对象作用域维护独立字段集合。
func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("JSON null is not allowed")
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			fieldToken, fieldErr := decoder.Token()
			if fieldErr != nil {
				return fieldErr
			}
			field, ok := fieldToken.(string)
			if !ok {
				return fmt.Errorf("JSON object field is not string")
			}
			if _, duplicated := seen[field]; duplicated {
				return fmt.Errorf("JSON contains duplicate field %q", field)
			}
			seen[field] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return fmt.Errorf("JSON object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return fmt.Errorf("JSON array is not closed")
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	return nil
}

// validText 以 Unicode scalar 计数并要求 UTF-8、NFC、无边界空白与控制字符。
func validText(value string, minimum, maximum int, allowEmpty bool) bool {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) || strings.TrimSpace(value) != value {
		return false
	}
	length := utf8.RuneCountInString(value)
	if allowEmpty && length == 0 {
		return true
	}
	if length < minimum || length > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return false
		}
	}
	return true
}

// validUniqueStrings 校验有序字符串列表逐项合法且按完整文本精确去重。
func validUniqueStrings(values []string, minimum, maximum int) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validText(value, minimum, maximum, false) {
			return false
		}
		if _, duplicated := seen[value]; duplicated {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

// validLocale 判断输出语言是否属于冻结枚举。
func validLocale(value string) bool { return stringInSet(value, localeValues[:]) }

// validSlotType 判断 Source Slot 类型是否属于冻结枚举。
func validSlotType(value string) bool { return stringInSet(value, slotTypeValues[:]) }

// stringInSet 使用精确大小写匹配冻结枚举，不接受别名或规范化回退。
func stringInSet(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// validStoryboardPreviewRef 校验 UUIDv7、固定版本一与小写 SHA-256 的 Source 精确引用。
func validStoryboardPreviewRef(value StoryboardPreviewRef) bool {
	return canonicalUUIDv7(value.ID) && value.Version == 1 && validLowerSHA256(value.ContentDigest)
}

// validLowerSHA256 校验六十四位小写十六进制 SHA-256，不接受前缀或大写形式。
func validLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// canonicalUUIDv7 要求规范小写文本与 UUID version 7，拒绝可解析但非 canonical 的别名。
func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

// validJSONSurrogateEscapes 在 Go JSON 替换非法代理项前预扫描原文，只接受成对高低代理项。
func validJSONSurrogateEscapes(raw []byte) bool {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(raw) {
				continue
			}
			if raw[index+1] != 'u' {
				index++
				continue
			}
			code, ok := parseJSONHexCodeUnit(raw, index+2)
			if !ok {
				return false
			}
			if code >= 0xD800 && code <= 0xDBFF {
				next := index + 6
				if next+6 > len(raw) || raw[next] != '\\' || raw[next+1] != 'u' {
					return false
				}
				low, lowOK := parseJSONHexCodeUnit(raw, next+2)
				if !lowOK || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				index += 11
				continue
			}
			if code >= 0xDC00 && code <= 0xDFFF {
				return false
			}
			index += 5
		}
	}
	return true
}

// parseJSONHexCodeUnit 严格解析 JSON `\uXXXX` 的四位十六进制 code unit，越界或非法字符返回 false。
func parseJSONHexCodeUnit(raw []byte, start int) (uint16, bool) {
	if start < 0 || start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, character := range raw[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value += uint16(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += uint16(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}
