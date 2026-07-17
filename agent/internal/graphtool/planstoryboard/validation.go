package planstoryboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

var (
	phaseKeyPattern   = regexp.MustCompile(`^phase_[1-6]$`)
	sectionKeyPattern = regexp.MustCompile(`^section_[1-8]$`)
	elementKeyPattern = regexp.MustCompile(`^element_(?:[1-9]|1[0-9]|2[0-4])$`)
	slotKeyPattern    = regexp.MustCompile(`^slot_(?:[1-9]|[1-8][0-9]|9[0-6])$`)
)

// DecodeIntent 对 Tool 原始 JSON 执行大小、UTF-8、重复字段、未知字段、尾随值和领域边界校验。
func DecodeIntent(encoded []byte) (Intent, error) {
	if len(encoded) == 0 || len(encoded) > maxJSONBytes || !utf8.Valid(encoded) || !validJSONSurrogateEscapes(encoded) {
		return Intent{}, fmt.Errorf("decode storyboard intent: invalid JSON size or UTF-8")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return Intent{}, fmt.Errorf("decode storyboard intent: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var intent Intent
	if err := decoder.Decode(&intent); err != nil {
		return Intent{}, fmt.Errorf("decode storyboard intent: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Intent{}, fmt.Errorf("decode storyboard intent: %w", err)
	}
	if err := ValidateIntent(intent); err != nil {
		return Intent{}, err
	}
	return intent, nil
}

// ValidateIntent 校验模型唯一可控的规划指令与可选目标时长。
func ValidateIntent(intent Intent) error {
	if intent.SchemaVersion != IntentSchemaVersion || !validText(intent.PlanningInstruction, intentInstructionMin, intentInstructionMax, false) {
		return fmt.Errorf("validate storyboard intent: invalid schema_version or planning_instruction")
	}
	if intent.TargetDurationSeconds != nil && (*intent.TargetDurationSeconds < intentTargetMin || *intent.TargetDurationSeconds > intentTargetMax) {
		return fmt.Errorf("validate storyboard intent: invalid target_duration_seconds")
	}
	return nil
}

// IntentDigest 计算具名严格 Intent JSON 的小写 SHA-256。
func IntentDigest(intent Intent) (string, error) {
	if err := ValidateIntent(intent); err != nil {
		return "", err
	}
	return digestJSON(intent, "storyboard intent")
}

// ValidateTrustedContext 校验 Runtime 私有身份、Fence、CreationSpec 引用和实现版本 Pin。
func ValidateTrustedContext(trusted TrustedContext) error {
	for _, value := range []string{
		trusted.RequestID, trusted.UserID, trusted.ProjectID, trusted.SessionID, trusted.InputID,
		trusted.TurnID, trusted.RunID, trusted.ToolCallID, trusted.BusinessCommandID,
	} {
		if !canonicalUUIDv7(value) {
			return fmt.Errorf("validate storyboard trusted context: invalid UUIDv7")
		}
	}
	if !validText(trusted.Owner, trustedOwnerMin, trustedOwnerMax, false) || trusted.FenceToken < 1 || !canonicalUUIDv7(trusted.CreationSpecRef.ID) ||
		trusted.CreationSpecRef.Version != 1 || !validLowerSHA256(trusted.CreationSpecRef.ContentDigest) ||
		trusted.PromptVersion != PromptVersion || trusted.ValidatorVersion != ValidatorVersion ||
		trusted.DAGValidatorVersion != DAGValidatorVersion {
		return fmt.Errorf("validate storyboard trusted context: invalid owner, fence, CreationSpec ref or version pin")
	}
	return nil
}

// ValidatePlanningContext 校验 Business 一致读取快照与 Runtime 冻结引用完全绑定。
func ValidatePlanningContext(value PlanningContext, trusted TrustedContext) error {
	if err := ValidateTrustedContext(trusted); err != nil {
		return err
	}
	resource := value.CreationSpec
	if !canonicalUUIDv7(value.ProjectID) || value.ProjectID != trusted.ProjectID || value.ProjectVersion < 1 ||
		!validText(value.ProjectTitle, projectTitleMin, projectTitleMax, false) || resource.ID != trusted.CreationSpecRef.ID ||
		resource.ProjectID != trusted.ProjectID || resource.Version != trusted.CreationSpecRef.Version ||
		resource.Status != "draft" || resource.ContentDigest != trusted.CreationSpecRef.ContentDigest {
		return fmt.Errorf("validate storyboard planning context: identity, version or digest mismatch")
	}
	if err := ValidateCreationSpecContent(resource.Content); err != nil {
		return err
	}
	digest, err := digestJSON(resource.Content, "CreationSpec content")
	if err != nil || digest != resource.ContentDigest {
		return fmt.Errorf("validate storyboard planning context: CreationSpec content digest mismatch")
	}
	return nil
}

// ValidateCreationSpecContent 校验进入 Prompt 的 CreationSpec 最小上下文。
func ValidateCreationSpecContent(content CreationSpecContent) error {
	if !validText(content.Title, creationTitleMin, creationTitleMax, false) || !validText(content.Goal, creationGoalMin, creationGoalMax, false) ||
		!validText(content.Audience, 0, creationAudienceMax, true) || !validDeliverable(content.DeliverableType) ||
		!validLocale(content.Locale) || len(content.Phases) < creationPhasesMin || len(content.Phases) > creationPhasesMax ||
		content.Constraints == nil || len(content.Constraints) > creationListsMax || content.AcceptanceCriteria == nil ||
		len(content.AcceptanceCriteria) < creationAcceptanceMin || len(content.AcceptanceCriteria) > creationListsMax {
		return fmt.Errorf("validate storyboard CreationSpec content: invalid boundary")
	}
	phaseKeys := make(map[string]struct{}, len(content.Phases))
	for _, phase := range content.Phases {
		if !phaseKeyPattern.MatchString(phase.Key) ||
			!validText(phase.Title, creationPhaseTextMin, creationPhaseTitleMax, false) ||
			!validText(phase.Objective, creationPhaseTextMin, creationPhaseBodyMax, false) ||
			!validText(phase.Output, creationPhaseTextMin, creationPhaseBodyMax, false) {
			return fmt.Errorf("validate storyboard CreationSpec content: invalid phase")
		}
		if _, duplicated := phaseKeys[phase.Key]; duplicated {
			return fmt.Errorf("validate storyboard CreationSpec content: duplicate phase")
		}
		phaseKeys[phase.Key] = struct{}{}
	}
	if !validUniqueStrings(content.Constraints, creationConstraintMin, creationConstraintMax) ||
		!validUniqueStrings(content.AcceptanceCriteria, creationAcceptanceTextMin, creationAcceptanceTextMax) {
		return fmt.Errorf("validate storyboard CreationSpec content: invalid list")
	}
	return nil
}

// DecodeAndValidateCandidate 严格解析一次模型候选并校验字段、枚举、Phase、数量和目标时长。
func DecodeAndValidateCandidate(encoded []byte, intent Intent, context PlanningContext) (Content, Candidate, error) {
	if err := ValidateIntent(intent); err != nil {
		return Content{}, Candidate{}, err
	}
	if len(encoded) == 0 || len(encoded) > maxJSONBytes || !utf8.Valid(encoded) || !validJSONSurrogateEscapes(encoded) {
		return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: invalid JSON size or UTF-8")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var candidate Candidate
	if err := decoder.Decode(&candidate); err != nil {
		return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: %w", err)
	}
	if candidate.SchemaVersion != CandidateSchemaVersion || !validText(candidate.Title, candidateTitleMin, candidateTitleMax, false) ||
		!validText(candidate.Summary, candidateSummaryMin, candidateSummaryMax, false) ||
		len(candidate.Sections) < 1 || len(candidate.Sections) > maxSections ||
		len(candidate.Elements) < 1 || len(candidate.Elements) > maxElements || candidate.Slots == nil || len(candidate.Slots) > maxSlots {
		return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: invalid scalar or collection boundary")
	}
	phaseKeys := make(map[string]struct{}, len(context.CreationSpec.Content.Phases))
	for _, phase := range context.CreationSpec.Content.Phases {
		phaseKeys[phase.Key] = struct{}{}
	}
	sectionKeys := make(map[string]struct{}, len(candidate.Sections))
	for index, section := range candidate.Sections {
		expectedKey := fmt.Sprintf("section_%d", index+1)
		if section.Key != expectedKey || !sectionKeyPattern.MatchString(section.Key) ||
			!validText(section.Title, sectionTitleMin, sectionTitleMax, false) ||
			!validText(section.Objective, sectionObjectiveMin, sectionObjectiveMax, false) {
			return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: invalid section")
		}
		sectionKeys[section.Key] = struct{}{}
	}
	elementKeys := make(map[string]struct{}, len(candidate.Elements))
	elementsPerSection := make(map[string]int, len(candidate.Sections))
	totalDuration := 0
	for index, element := range candidate.Elements {
		expectedKey := fmt.Sprintf("element_%d", index+1)
		_, sectionExists := sectionKeys[element.SectionKey]
		_, phaseExists := phaseKeys[element.SourcePhaseKey]
		if element.Key != expectedKey || !elementKeyPattern.MatchString(element.Key) || element.Order != index+1 ||
			!sectionExists || !phaseExists || !validElementType(element.ElementType) ||
			!validText(element.Title, elementTitleMin, elementTitleMax, false) ||
			!validText(element.NarrativePurpose, elementNarrativeMin, elementNarrativeMax, false) ||
			element.DurationSeconds < elementDurationMin || element.DurationSeconds > elementDurationMax || element.DependencyKeys == nil ||
			len(element.DependencyKeys) > maxElementDeps || !validUniqueLocalKeys(element.DependencyKeys, elementKeyPattern) {
			return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: invalid element")
		}
		elementKeys[element.Key] = struct{}{}
		elementsPerSection[element.SectionKey]++
		totalDuration += element.DurationSeconds
	}
	for sectionKey := range sectionKeys {
		if elementsPerSection[sectionKey] < 1 {
			return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: section has no element")
		}
	}
	if totalDuration < totalDurationMin || totalDuration > totalDurationMax {
		return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: total duration out of range")
	}
	if intent.TargetDurationSeconds != nil {
		tolerance := int(math.Ceil(float64(*intent.TargetDurationSeconds) * targetToleranceRate))
		if tolerance < targetToleranceMin {
			tolerance = targetToleranceMin
		}
		if absoluteInt(totalDuration-*intent.TargetDurationSeconds) > tolerance {
			return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: target duration mismatch")
		}
	}
	slotKeys := make(map[string]struct{}, len(candidate.Slots))
	slotsPerElement := make(map[string]int, len(candidate.Elements))
	for index, slot := range candidate.Slots {
		expectedKey := fmt.Sprintf("slot_%d", index+1)
		_, elementExists := elementKeys[slot.ElementKey]
		if slot.Key != expectedKey || !slotKeyPattern.MatchString(slot.Key) || !elementExists ||
			!validSlotType(slot.SlotType) || !validText(slot.Purpose, slotPurposeMin, slotPurposeMax, false) {
			return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: invalid slot")
		}
		if _, duplicated := slotKeys[slot.Key]; duplicated {
			return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: duplicate slot")
		}
		slotKeys[slot.Key] = struct{}{}
		slotsPerElement[slot.ElementKey]++
		if slotsPerElement[slot.ElementKey] > maxSlotsPerElement {
			return Content{}, Candidate{}, fmt.Errorf("validate storyboard candidate: too many slots for element")
		}
	}
	content := Content{
		Title: candidate.Title, Summary: candidate.Summary,
		Sections: cloneSections(candidate.Sections), Elements: cloneElements(candidate.Elements), Slots: cloneSlots(candidate.Slots),
	}
	return content, candidate, nil
}

// ValidateContent 校验 Agent 与 Business 共用 Draft Content 的全部非上下文不变量。
func ValidateContent(content Content) error {
	if !validText(content.Title, candidateTitleMin, candidateTitleMax, false) ||
		!validText(content.Summary, candidateSummaryMin, candidateSummaryMax, false) ||
		len(content.Sections) < 1 || len(content.Sections) > maxSections ||
		len(content.Elements) < 1 || len(content.Elements) > maxElements || content.Slots == nil || len(content.Slots) > maxSlots {
		return fmt.Errorf("validate storyboard content: invalid scalar or collection boundary")
	}
	sections := make(map[string]struct{}, len(content.Sections))
	for index, section := range content.Sections {
		if section.Key != fmt.Sprintf("section_%d", index+1) || !validText(section.Title, sectionTitleMin, sectionTitleMax, false) ||
			!validText(section.Objective, sectionObjectiveMin, sectionObjectiveMax, false) {
			return fmt.Errorf("validate storyboard content: invalid section")
		}
		sections[section.Key] = struct{}{}
	}
	elements := make(map[string]struct{}, len(content.Elements))
	elementsPerSection := make(map[string]int, len(content.Sections))
	totalDuration := 0
	for index, element := range content.Elements {
		_, sectionExists := sections[element.SectionKey]
		if element.Key != fmt.Sprintf("element_%d", index+1) || element.Order != index+1 || !sectionExists ||
			!validElementType(element.ElementType) || !validText(element.Title, elementTitleMin, elementTitleMax, false) ||
			!validText(element.NarrativePurpose, elementNarrativeMin, elementNarrativeMax, false) ||
			element.DurationSeconds < elementDurationMin || element.DurationSeconds > elementDurationMax ||
			!phaseKeyPattern.MatchString(element.SourcePhaseKey) ||
			element.DependencyKeys == nil || len(element.DependencyKeys) > maxElementDeps ||
			!validUniqueLocalKeys(element.DependencyKeys, elementKeyPattern) {
			return fmt.Errorf("validate storyboard content: invalid element")
		}
		elements[element.Key] = struct{}{}
		elementsPerSection[element.SectionKey]++
		totalDuration += element.DurationSeconds
	}
	if totalDuration < totalDurationMin || totalDuration > totalDurationMax {
		return fmt.Errorf("validate storyboard content: total duration out of range")
	}
	for sectionKey := range sections {
		if elementsPerSection[sectionKey] < 1 {
			return fmt.Errorf("validate storyboard content: section has no element")
		}
	}
	slotsPerElement := make(map[string]int, len(elements))
	for index, slot := range content.Slots {
		_, elementExists := elements[slot.ElementKey]
		if slot.Key != fmt.Sprintf("slot_%d", index+1) || !elementExists || !validSlotType(slot.SlotType) ||
			!validText(slot.Purpose, slotPurposeMin, slotPurposeMax, false) {
			return fmt.Errorf("validate storyboard content: invalid slot")
		}
		slotsPerElement[slot.ElementKey]++
		if slotsPerElement[slot.ElementKey] > maxSlotsPerElement {
			return fmt.Errorf("validate storyboard content: too many slots for element")
		}
	}
	return nil
}

// ValidateDependencyGraph 校验所有依赖引用、自依赖和有向环，并再次复核 Slot 归属。
func ValidateDependencyGraph(content Content) error {
	if len(content.Elements) < 1 || len(content.Elements) > maxElements || content.Slots == nil {
		return fmt.Errorf("validate storyboard dependency graph: invalid content")
	}
	elements := make(map[string]Element, len(content.Elements))
	indegree := make(map[string]int, len(content.Elements))
	dependents := make(map[string][]string, len(content.Elements))
	for _, element := range content.Elements {
		if _, duplicated := elements[element.Key]; duplicated {
			return fmt.Errorf("validate storyboard dependency graph: duplicate element")
		}
		elements[element.Key] = element
		indegree[element.Key] = 0
	}
	for _, element := range content.Elements {
		for _, dependency := range element.DependencyKeys {
			if dependency == element.Key {
				return fmt.Errorf("validate storyboard dependency graph: self dependency")
			}
			if _, exists := elements[dependency]; !exists {
				return fmt.Errorf("validate storyboard dependency graph: unknown dependency")
			}
			indegree[element.Key]++
			dependents[dependency] = append(dependents[dependency], element.Key)
		}
	}
	for _, slot := range content.Slots {
		if _, exists := elements[slot.ElementKey]; !exists {
			return fmt.Errorf("validate storyboard dependency graph: unknown slot owner")
		}
	}
	queue := make([]string, 0, len(elements))
	for _, element := range content.Elements {
		if indegree[element.Key] == 0 {
			queue = append(queue, element.Key)
		}
	}
	visited := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visited++
		for _, dependent := range dependents[current] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	if visited != len(elements) {
		return fmt.Errorf("validate storyboard dependency graph: cycle")
	}
	return nil
}

// CandidateDigest 计算严格候选 canonical JSON 摘要。
func CandidateDigest(candidate Candidate) (string, error) {
	return digestJSON(candidate, "storyboard candidate")
}

// ContentDigest 计算 Business Draft Content 的冻结 JSON 摘要。
func ContentDigest(content Content) (string, error) {
	if err := ValidateContent(content); err != nil {
		return "", err
	}
	if err := ValidateDependencyGraph(content); err != nil {
		return "", err
	}
	return digestJSON(content, "storyboard content")
}

// SaveRequestDigest 按跨 Module 固定字段顺序计算 Agent 到 Business 保存命令摘要。
func SaveRequestDigest(command DraftCommand) (string, error) {
	trusted := command.TrustedContext
	if !canonicalUUIDv7(trusted.UserID) || !canonicalUUIDv7(trusted.ProjectID) ||
		!canonicalUUIDv7(trusted.ToolCallID) || !canonicalUUIDv7(trusted.CreationSpecRef.ID) ||
		trusted.CreationSpecRef.Version != 1 || !validLowerSHA256(trusted.CreationSpecRef.ContentDigest) ||
		trusted.PromptVersion != PromptVersion || trusted.ValidatorVersion != ValidatorVersion ||
		command.DomainContext.ProjectID != trusted.ProjectID || command.DomainContext.ProjectVersion < 1 {
		return "", fmt.Errorf("compute storyboard save request digest: invalid stable command fields")
	}
	if _, err := ContentDigest(command.Content); err != nil {
		return "", err
	}
	wire := saveDigestWire{
		SchemaVersion: SaveDigestSchemaVersion, UserID: trusted.UserID,
		ProjectID: trusted.ProjectID, ExpectedProjectVersion: command.DomainContext.ProjectVersion,
		CreationSpecRef: trusted.CreationSpecRef, ToolCallID: trusted.ToolCallID,
		PromptVersion: trusted.PromptVersion, ValidatorVersion: trusted.ValidatorVersion,
		Content: command.Content,
	}
	return digestJSON(wire, "storyboard save request")
}

// ValidateResourceForCommand 校验 Business 回传资源自洽且精确绑定原命令。
func ValidateResourceForCommand(resource Resource, command DraftCommand) error {
	if !canonicalUUIDv7(resource.StoryboardPreviewID) || resource.ProjectID != command.TrustedContext.ProjectID ||
		resource.CreationSpecRef != command.TrustedContext.CreationSpecRef || resource.Version < 1 ||
		resource.Status != "draft" || !validLowerSHA256(resource.ContentDigest) {
		return fmt.Errorf("validate storyboard resource: invalid identity or state")
	}
	digest, err := ContentDigest(resource.Content)
	if err != nil || digest != resource.ContentDigest {
		return fmt.Errorf("validate storyboard resource: content digest mismatch")
	}
	expectedDigest, err := ContentDigest(command.Content)
	if err != nil || expectedDigest != resource.ContentDigest {
		return fmt.Errorf("validate storyboard resource: command content mismatch")
	}
	return nil
}

// ValidateCard 校验持久化 Workspace/Event 共用 Card 的完整安全边界。
func ValidateCard(card Card) error {
	if card.SchemaVersion != CardSchemaVersion || !canonicalUUIDv7(card.StoryboardPreviewID) ||
		!canonicalUUIDv7(card.ProjectID) || !canonicalUUIDv7(card.CreationSpecRef.ID) || card.CreationSpecRef.Version != 1 ||
		!validLowerSHA256(card.CreationSpecRef.ContentDigest) || card.Version < 1 || card.Status != "draft" ||
		!validLowerSHA256(card.ContentDigest) || card.UpdatedAt.IsZero() || card.UpdatedAt.Location() != time.UTC {
		return fmt.Errorf("validate storyboard card: invalid identity or state")
	}
	content := Content{Title: card.Title, Summary: card.Summary, Sections: card.Sections, Elements: card.Elements, Slots: card.Slots}
	digest, err := ContentDigest(content)
	if err != nil || digest != card.ContentDigest {
		return fmt.Errorf("validate storyboard card: content digest mismatch")
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
		return fmt.Errorf("validate storyboard result: invocation mismatch")
	}
	switch result.Status {
	case "completed":
		if result.ResultCode != ResultCodeCompleted || result.ResourceRef == nil || result.Card == nil ||
			result.Summary != "" || result.Retryable != nil {
			return fmt.Errorf("validate storyboard result: invalid completed shape")
		}
		if err := ValidateCard(*result.Card); err != nil {
			return err
		}
		if result.ResourceRef.StoryboardPreviewID != result.Card.StoryboardPreviewID ||
			result.ResourceRef.Version != result.Card.Version || result.ResourceRef.Digest != result.Card.ContentDigest ||
			result.ResourceRef.Status != result.Card.Status || result.ResourceRef.CreationSpecRef != result.Card.CreationSpecRef ||
			result.Card.ProjectID != trusted.ProjectID {
			return fmt.Errorf("validate storyboard result: resource/card mismatch")
		}
	case "failed":
		if result.ResultCode == "" || result.ResourceRef != nil || result.Card != nil ||
			!validText(result.Summary, resultSummaryMin, resultSummaryMax, false) || result.Retryable == nil {
			return fmt.Errorf("validate storyboard result: invalid failed shape")
		}
	default:
		return fmt.Errorf("validate storyboard result: invalid terminal status")
	}
	return nil
}

type saveDigestWire struct {
	SchemaVersion          string          `json:"schema_version"`
	UserID                 string          `json:"user_id"`
	ProjectID              string          `json:"project_id"`
	ExpectedProjectVersion int64           `json:"expected_project_version"`
	CreationSpecRef        CreationSpecRef `json:"creation_spec_ref"`
	ToolCallID             string          `json:"tool_call_id"`
	PromptVersion          string          `json:"prompt_version"`
	ValidatorVersion       string          `json:"validator_version"`
	Content                Content         `json:"content"`
}

func digestJSON(value any, label string) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) == 0 || len(encoded) > maxJSONBytes {
		return "", fmt.Errorf("compute %s digest: canonical JSON failed", label)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

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

func validUniqueLocalKeys(values []string, pattern *regexp.Regexp) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !pattern.MatchString(value) {
			return false
		}
		if _, duplicated := seen[value]; duplicated {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validDeliverable(value string) bool {
	return stringInSet(value, deliverableValues[:])
}

func validLocale(value string) bool { return stringInSet(value, localeValues[:]) }

func validElementType(value string) bool {
	return stringInSet(value, elementTypeValues[:])
}

func validSlotType(value string) bool {
	return stringInSet(value, slotTypeValues[:])
}

func stringInSet(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

func cloneSections(values []Section) []Section { return append([]Section(nil), values...) }

func cloneElements(values []Element) []Element {
	result := make([]Element, len(values))
	copy(result, values)
	for index := range result {
		result[index].DependencyKeys = make([]string, len(values[index].DependencyKeys))
		copy(result[index].DependencyKeys, values[index].DependencyKeys)
	}
	return result
}

func cloneSlots(values []Slot) []Slot {
	result := make([]Slot, len(values))
	copy(result, values)
	return result
}

func absoluteInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

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
