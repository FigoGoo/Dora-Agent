// Package skill 定义 Business Skill 草稿、审核和不可变发布快照的领域与应用边界。
package skill

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const (
	// DefinitionSchemaVersionV1 是 W1 Skill 结构化内容和 Canonical JSON 的冻结版本。
	DefinitionSchemaVersionV1 = "skill_definition.v1"
	// MaxCanonicalDefinitionBytes 是所有 Skill 写入、审核读取和发布统一使用的 Canonical Definition 总量上限。
	MaxCanonicalDefinitionBytes = 1 << 20
	// maxNameBytes 是 NFC 和去除首尾空白后名称的 UTF-8 字节上限。
	maxNameBytes = 160
	// maxShortTextBytes 是分类、标签和封面资源引用的 UTF-8 字节上限。
	maxShortTextBytes = 256
	// maxSummaryBytes 是摘要和市场可见短说明的 UTF-8 字节上限。
	maxSummaryBytes = 4096
	// maxBodyTextBytes 是单个能力说明、输入输出说明和示例正文的 UTF-8 字节上限。
	maxBodyTextBytes = 32 * 1024
	// maxListItems 是 tags、examples 和 starter_prompts 的单字段元素上限。
	maxListItems = 100
)

var (
	// ErrInvalidDefinition 表示结构化定义不满足冻结字段、Unicode、长度或互斥规则。
	ErrInvalidDefinition = errors.New("invalid skill definition")
	// ErrToolReferenceUnavailable 表示当前 W1 基线不允许任何公共 Tool 引用。
	ErrToolReferenceUnavailable = errors.New("skill tool reference unavailable")
)

// FieldError 是可安全返回给 Owner Builder 的稳定字段级校验错误。
type FieldError struct {
	// Field 是 SkillDefinitionV1 中的稳定 JSON 字段路径。
	Field string `json:"field"`
	// Code 是前端可稳定分支且不包含内部策略的错误代码。
	Code string `json:"code"`
	// Message 是不包含 SQL、堆栈或内部地址的安全中文说明。
	Message string `json:"message"`
}

// ValidationError 汇总一次定义校验发现的全部字段错误，并保留稳定领域错误分类。
type ValidationError struct {
	// FieldErrors 按冻结字段顺序排列，便于 Builder 稳定定位。
	FieldErrors []FieldError
	// Cause 区分普通定义非法和当前公共 Tool 入口不可用。
	Cause error
}

// Error 返回不包含用户正文的稳定错误摘要。
func (e *ValidationError) Error() string { return "skill definition validation failed" }

// Unwrap 返回稳定领域分类，调用方不得把底层用户内容拼入响应。
func (e *ValidationError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return ErrInvalidDefinition
}

// CapabilityGuidanceV1 是六个固定 Agent 能力入口之一的结构化适用性说明。
type CapabilityGuidanceV1 struct {
	// Applicability 只允许 enabled 或 not_applicable。
	Applicability string `json:"applicability"`
	// Guidance 是 enabled 时必填的能力指导正文。
	Guidance string `json:"guidance"`
	// NotApplicableReason 是 not_applicable 时必填的稳定用户说明。
	NotApplicableReason string `json:"not_applicable_reason"`
}

// SkillExampleV1 是一个分离保存的示例输入与期望输出。
type SkillExampleV1 struct {
	// Input 是示例输入正文。
	Input string `json:"input"`
	// Output 是示例期望输出正文。
	Output string `json:"output"`
}

// MarketListingV1 是当前 Owner Builder 保存的市场展示素材，不代表公开市场已经接线。
type MarketListingV1 struct {
	// CoverAssetID 当前只能为 null；Asset Owner/存在性契约冻结前任何非 null 引用都失败关闭。
	CoverAssetID *string `json:"cover_asset_id"`
	// Detail 是面向潜在用户的详情说明。
	Detail string `json:"detail"`
	// CopyrightNotice 是作者提供的版权说明。
	CopyrightNotice string `json:"copyright_notice"`
	// UserNotice 是使用者可见的限制和注意事项。
	UserNotice string `json:"user_notice"`
}

// PublicToolReferenceV1 是未冻结公共 Tool 契约前的受限原始元素载体。
// W1-A1 只用它确保任意非空合法 JSON 元素都能进入字段级失败关闭，绝不解析或持久化其未冻结语义。
type PublicToolReferenceV1 json.RawMessage

// UnmarshalJSON 暂存一个合法 JSON 元素，使任意非空引用都能进入统一字段级失败关闭。
func (reference *PublicToolReferenceV1) UnmarshalJSON(data []byte) error {
	if !json.Valid(data) {
		return ErrInvalidDefinition
	}
	*reference = append((*reference)[:0], data...)
	return nil
}

// MarshalJSON 仅供请求测试和 DTO 编码；合法非空值仍会在 NormalizeDefinitionV1 前置拒绝，永不持久化。
func (reference PublicToolReferenceV1) MarshalJSON() ([]byte, error) {
	if len(reference) == 0 {
		return []byte("null"), nil
	}
	if !json.Valid(reference) {
		return nil, ErrInvalidDefinition
	}
	return append([]byte(nil), reference...), nil
}

// SkillDefinitionV1 是 W1 冻结的完整结构化 Skill 内容 DTO。
type SkillDefinitionV1 struct {
	// SchemaVersion 固定为 skill_definition.v1。
	SchemaVersion string `json:"schema_version"`
	// Name 是 Owner 和后续市场投影使用的名称。
	Name string `json:"name"`
	// Summary 是简短用途摘要。
	Summary string `json:"summary"`
	// Category 是独立分类键或名称，不与标签拼接。
	Category string `json:"category"`
	// Tags 是去重并稳定排序的标签集合。
	Tags []string `json:"tags"`
	// InputDescription 是 Skill 期望输入的用户说明。
	InputDescription string `json:"input_description"`
	// OutputDescription 是 Skill 产出结果的用户说明。
	OutputDescription string `json:"output_description"`
	// InvocationRules 是多 Skill 选择规则，不承载权限、预算或价格。
	InvocationRules string `json:"invocation_rules"`
	// PlanCreationSpec 是流程规划能力说明。
	PlanCreationSpec CapabilityGuidanceV1 `json:"plan_creation_spec"`
	// AnalyzeMaterials 是素材分析能力说明。
	AnalyzeMaterials CapabilityGuidanceV1 `json:"analyze_materials"`
	// PlanStoryboard 是故事板规划能力说明。
	PlanStoryboard CapabilityGuidanceV1 `json:"plan_storyboard"`
	// GenerateMedia 是媒体生成能力说明。
	GenerateMedia CapabilityGuidanceV1 `json:"generate_media"`
	// WritePrompts 是提示词编写能力说明。
	WritePrompts CapabilityGuidanceV1 `json:"write_prompts"`
	// AssembleOutput 是成片组装能力说明。
	AssembleOutput CapabilityGuidanceV1 `json:"assemble_output"`
	// Examples 是去重并稳定排序的结构化示例集合。
	Examples []SkillExampleV1 `json:"examples"`
	// StarterPrompts 是去重并稳定排序的起始提示词集合。
	StarterPrompts []string `json:"starter_prompts"`
	// MarketListing 是尚未公开前仍由 Owner 编辑的市场展示内容。
	MarketListing MarketListingV1 `json:"market_listing"`
	// PublicToolRefs 当前只允许非 nil 空数组，任何元素均字段级失败关闭。
	PublicToolRefs []PublicToolReferenceV1 `json:"public_tool_refs"`
}

// Digest 是固定 32 字节 SHA-256 摘要，用于内容一致性与幂等语义。
type Digest [sha256.Size]byte

// Hex 将摘要编码为 64 位小写十六进制，供后续跨 Module 测试向量使用。
func (d Digest) Hex() string { return hex.EncodeToString(d[:]) }

// NormalizeDefinitionV1 完成 UTF-8、NFC、长度、枚举、数组去重排序和公共 Tool 失败关闭。
// 返回值可直接用于 Canonical JSON 与持久化；失败时不得保存部分规范化结果。
func NormalizeDefinitionV1(input SkillDefinitionV1) (SkillDefinitionV1, error) {
	definition := input
	errorsFound := make([]FieldError, 0)

	if input.SchemaVersion != DefinitionSchemaVersionV1 {
		errorsFound = append(errorsFound, fieldError("schema_version", "INVALID_SCHEMA_VERSION", "定义版本必须为 skill_definition.v1"))
	}
	definition.SchemaVersion = DefinitionSchemaVersionV1
	definition.Name = normalizeTextField(&errorsFound, "name", input.Name, true, true, maxNameBytes)
	definition.Summary = normalizeTextField(&errorsFound, "summary", input.Summary, false, true, maxSummaryBytes)
	definition.Category = normalizeTextField(&errorsFound, "category", input.Category, false, true, maxShortTextBytes)
	definition.InputDescription = normalizeTextField(&errorsFound, "input_description", input.InputDescription, false, true, maxBodyTextBytes)
	definition.OutputDescription = normalizeTextField(&errorsFound, "output_description", input.OutputDescription, false, true, maxBodyTextBytes)
	definition.InvocationRules = normalizeTextField(&errorsFound, "invocation_rules", input.InvocationRules, false, true, maxBodyTextBytes)

	definition.PlanCreationSpec = normalizeCapability(&errorsFound, "plan_creation_spec", input.PlanCreationSpec)
	definition.AnalyzeMaterials = normalizeCapability(&errorsFound, "analyze_materials", input.AnalyzeMaterials)
	definition.PlanStoryboard = normalizeCapability(&errorsFound, "plan_storyboard", input.PlanStoryboard)
	definition.GenerateMedia = normalizeCapability(&errorsFound, "generate_media", input.GenerateMedia)
	definition.WritePrompts = normalizeCapability(&errorsFound, "write_prompts", input.WritePrompts)
	definition.AssembleOutput = normalizeCapability(&errorsFound, "assemble_output", input.AssembleOutput)

	requireDefinitionArray(&errorsFound, "tags", input.Tags != nil)
	requireDefinitionArray(&errorsFound, "examples", input.Examples != nil)
	requireDefinitionArray(&errorsFound, "starter_prompts", input.StarterPrompts != nil)
	requireDefinitionArray(&errorsFound, "public_tool_refs", input.PublicToolRefs != nil)
	definition.Tags = normalizeStringList(&errorsFound, "tags", input.Tags, maxShortTextBytes)
	definition.StarterPrompts = normalizeStringList(&errorsFound, "starter_prompts", input.StarterPrompts, maxBodyTextBytes)
	definition.Examples = normalizeExamples(&errorsFound, input.Examples)
	definition.MarketListing = MarketListingV1{
		CoverAssetID:    nil,
		Detail:          normalizeTextField(&errorsFound, "market_listing.detail", input.MarketListing.Detail, false, true, maxBodyTextBytes),
		CopyrightNotice: normalizeTextField(&errorsFound, "market_listing.copyright_notice", input.MarketListing.CopyrightNotice, false, true, maxSummaryBytes),
		UserNotice:      normalizeTextField(&errorsFound, "market_listing.user_notice", input.MarketListing.UserNotice, false, true, maxSummaryBytes),
	}
	if input.MarketListing.CoverAssetID != nil {
		errorsFound = append(errorsFound, fieldError("market_listing.cover_asset_id", "ASSET_REFERENCE_UNAVAILABLE", "封面 Asset 引用入口尚未开放"))
	}
	definition.PublicToolRefs = make([]PublicToolReferenceV1, 0)
	toolReferenceUnavailable := len(input.PublicToolRefs) != 0
	if toolReferenceUnavailable {
		errorsFound = append(errorsFound, fieldError("public_tool_refs", "SKILL_TOOL_REFERENCE_UNAVAILABLE", "公共 Tool 引用入口尚未开放"))
	}

	if len(errorsFound) != 0 {
		cause := ErrInvalidDefinition
		if toolReferenceUnavailable {
			cause = ErrToolReferenceUnavailable
		}
		return SkillDefinitionV1{}, &ValidationError{FieldErrors: errorsFound, Cause: cause}
	}
	return definition, nil
}

// requireDefinitionArray 拒绝 JSON 中缺失或显式为 null 的数组；空数组仍是合法、确定的集合值。
func requireDefinitionArray(errorsFound *[]FieldError, field string, present bool) {
	if !present {
		*errorsFound = append(*errorsFound, fieldError(field, "REQUIRED", "数组字段必须存在且不能为 null"))
	}
}

// CanonicalDefinitionV1 返回固定字段顺序且不转义 HTML 的 Canonical JSON 及其 SHA-256 摘要。
// 调用方必须传入 NormalizeDefinitionV1 成功返回的定义，避免不同语言对非法输入产生不同摘要。
func CanonicalDefinitionV1(definition SkillDefinitionV1) ([]byte, Digest, error) {
	normalized, err := NormalizeDefinitionV1(definition)
	if err != nil {
		return nil, Digest{}, err
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(normalized); err != nil {
		return nil, Digest{}, fmt.Errorf("encode skill definition canonical JSON: %w", err)
	}
	encoded := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	if len(encoded) > MaxCanonicalDefinitionBytes {
		return nil, Digest{}, ErrInvalidDefinition
	}
	return append([]byte(nil), encoded...), sha256.Sum256(encoded), nil
}

// DefinitionFromCanonicalV1 从持久化 Canonical JSON 恢复强类型定义并重新验证摘要前置条件。
func DefinitionFromCanonicalV1(encoded []byte) (SkillDefinitionV1, Digest, error) {
	// PostgreSQL jsonb 读回文本会增加少量分隔空白；先以双倍硬上限约束解码资源，再以重编码后的 Canonical 长度执行权威 1 MiB 限制。
	if len(encoded) == 0 || len(encoded) > 2*MaxCanonicalDefinitionBytes {
		return SkillDefinitionV1{}, Digest{}, ErrInvalidDefinition
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var definition SkillDefinitionV1
	if err := decoder.Decode(&definition); err != nil {
		return SkillDefinitionV1{}, Digest{}, ErrInvalidDefinition
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return SkillDefinitionV1{}, Digest{}, ErrInvalidDefinition
	}
	normalized, err := NormalizeDefinitionV1(definition)
	if err != nil {
		return SkillDefinitionV1{}, Digest{}, ErrInvalidDefinition
	}
	canonical, digest, err := CanonicalDefinitionV1(normalized)
	if err != nil {
		return SkillDefinitionV1{}, Digest{}, ErrInvalidDefinition
	}
	if len(canonical) > MaxCanonicalDefinitionBytes {
		return SkillDefinitionV1{}, Digest{}, ErrInvalidDefinition
	}
	return normalized, digest, nil
}

// normalizeCapability 规范化一个固定能力字段并校验 enabled/not_applicable 两类正文互斥。
func normalizeCapability(errorsFound *[]FieldError, field string, input CapabilityGuidanceV1) CapabilityGuidanceV1 {
	result := CapabilityGuidanceV1{
		Applicability:       strings.TrimSpace(input.Applicability),
		Guidance:            normalizeTextField(errorsFound, field+".guidance", input.Guidance, false, true, maxBodyTextBytes),
		NotApplicableReason: normalizeTextField(errorsFound, field+".not_applicable_reason", input.NotApplicableReason, false, true, maxBodyTextBytes),
	}
	switch result.Applicability {
	case "enabled":
		if result.Guidance == "" {
			*errorsFound = append(*errorsFound, fieldError(field+".guidance", "REQUIRED", "启用能力时必须填写指导说明"))
		}
		if result.NotApplicableReason != "" {
			*errorsFound = append(*errorsFound, fieldError(field+".not_applicable_reason", "MUST_BE_EMPTY", "启用能力时不适用原因必须为空"))
		}
	case "not_applicable":
		if result.Guidance != "" {
			*errorsFound = append(*errorsFound, fieldError(field+".guidance", "MUST_BE_EMPTY", "不适用能力的指导说明必须为空"))
		}
		if result.NotApplicableReason == "" {
			*errorsFound = append(*errorsFound, fieldError(field+".not_applicable_reason", "REQUIRED", "不适用能力时必须填写原因"))
		}
	default:
		*errorsFound = append(*errorsFound, fieldError(field+".applicability", "INVALID_ENUM", "适用性必须为 enabled 或 not_applicable"))
	}
	return result
}

// normalizeStringList 规范化、拒绝空元素、去重并按 UTF-8 字节序稳定排序字符串数组。
func normalizeStringList(errorsFound *[]FieldError, field string, input []string, maxItemBytes int) []string {
	if len(input) > maxListItems {
		*errorsFound = append(*errorsFound, fieldError(field, "TOO_MANY_ITEMS", "列表元素数量超过限制"))
	}
	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, len(input))
	for index, value := range input {
		normalized := normalizeTextField(errorsFound, fmt.Sprintf("%s[%d]", field, index), value, true, true, maxItemBytes)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result
}

// normalizeExamples 规范化示例对、去重并按输入和输出稳定排序，避免数组顺序改变摘要。
func normalizeExamples(errorsFound *[]FieldError, input []SkillExampleV1) []SkillExampleV1 {
	if len(input) > maxListItems {
		*errorsFound = append(*errorsFound, fieldError("examples", "TOO_MANY_ITEMS", "示例数量超过限制"))
	}
	seen := make(map[string]struct{}, len(input))
	result := make([]SkillExampleV1, 0, len(input))
	for index, example := range input {
		normalized := SkillExampleV1{
			Input:  normalizeTextField(errorsFound, fmt.Sprintf("examples[%d].input", index), example.Input, true, true, maxBodyTextBytes),
			Output: normalizeTextField(errorsFound, fmt.Sprintf("examples[%d].output", index), example.Output, true, true, maxBodyTextBytes),
		}
		if normalized.Input == "" || normalized.Output == "" {
			continue
		}
		key := normalized.Input + "\x00" + normalized.Output
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, normalized)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Input == result[right].Input {
			return result[left].Output < result[right].Output
		}
		return result[left].Input < result[right].Input
	})
	return result
}

// normalizeTextField 对单字段执行合法 UTF-8、NFC、首尾空白、控制字符和字节长度校验。
func normalizeTextField(errorsFound *[]FieldError, field string, input string, required bool, allowNewline bool, maxBytes int) string {
	if !utf8.ValidString(input) {
		*errorsFound = append(*errorsFound, fieldError(field, "INVALID_UTF8", "字段必须是合法 UTF-8 文本"))
		return ""
	}
	value := strings.TrimSpace(norm.NFC.String(input))
	if required && value == "" {
		*errorsFound = append(*errorsFound, fieldError(field, "REQUIRED", "字段不能为空"))
	}
	if len(value) > maxBytes {
		*errorsFound = append(*errorsFound, fieldError(field, "TOO_LONG", "字段长度超过限制"))
	}
	for _, character := range value {
		if !unicode.IsControl(character) {
			continue
		}
		if allowNewline && (character == '\n' || character == '\r' || character == '\t') {
			continue
		}
		*errorsFound = append(*errorsFound, fieldError(field, "CONTROL_CHARACTER_FORBIDDEN", "字段包含不允许的控制字符"))
		break
	}
	return value
}

// fieldError 创建不包含用户输入的稳定字段错误。
func fieldError(field string, code string, message string) FieldError {
	return FieldError{Field: field, Code: code, Message: message}
}

// isUUIDv7 校验 Business 应用标识和可选 Asset 引用使用 UUIDv7 规范格式。
func isUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.String() == strings.ToLower(value)
}
