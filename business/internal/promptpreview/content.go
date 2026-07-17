// Package promptpreview 定义 Business-owned Prompt Development Preview 草稿及其幂等保存边界。
package promptpreview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
	"golang.org/x/text/unicode/norm"
)

const (
	// MaxPrompts 是 Development Preview 允许保存的最大目标 Prompt 数量。
	MaxPrompts = storyboardpreview.MaxSlots
	// MaxNegativeConstraints 是每个 Prompt 允许保存的最大负面约束数量。
	MaxNegativeConstraints   = 16
	maxCanonicalContentBytes = 128 * 1024
)

var (
	elementLocalKeyPattern = regexp.MustCompile(`^element_([1-9]|1[0-9]|2[0-4])$`)
	targetLocalKeyPattern  = regexp.MustCompile(`^slot_([1-9]|[1-8][0-9]|9[0-6])$`)
)

// StoryboardPreviewRef 冻结 Prompt Preview 所依赖的 Storyboard Preview 精确版本。
type StoryboardPreviewRef struct {
	// ID 是 Business Storyboard Preview Draft UUIDv7。
	ID string `json:"id"`
	// Version 是 Storyboard Preview Draft 版本，当前固定为一。
	Version int64 `json:"version"`
	// ContentDigest 是 Storyboard Preview Canonical Content 的小写 SHA-256。
	ContentDigest string `json:"content_digest"`
}

// PromptEntry 是双 Validator 通过后保存的单个完整 Prompt 条目。
// 可信目标字段必须由 Source Storyboard Preview 的全部 Slot 确定性回填。
type PromptEntry struct {
	// TargetLocalKey 是 Source Slot 的局部键。
	TargetLocalKey string `json:"target_local_key"`
	// ElementLocalKey 是 Source Element 的局部键。
	ElementLocalKey string `json:"element_local_key"`
	// SlotType 是冻结的 Source Slot 类型。
	SlotType string `json:"slot_type"`
	// MediaKind 是由 SlotType 确定性映射的媒体类型。
	MediaKind string `json:"media_kind"`
	// Purpose 是 Source Slot 的业务用途。
	Purpose string `json:"purpose"`
	// Required 表示该 Source Slot 是否必须满足。
	Required bool `json:"required"`
	// PositivePrompt 是已校验的正向生成提示词。
	PositivePrompt string `json:"positive_prompt"`
	// NegativeConstraints 是已校验且保持模型顺序的负面约束。
	NegativeConstraints []string `json:"negative_constraints"`
	// OutputLanguage 是冻结的输出语言，只允许 zh-CN 或 en-US。
	OutputLanguage string `json:"output_language"`
}

// Content 是隔离 Prompt Preview Draft 的严格 JSON 内容。
// 字段顺序同时是跨 Module Canonical JSON 顺序，修改时必须升级 Schema Version。
type Content struct {
	// SchemaVersion 固定为 prompt.preview.draft.v1。
	SchemaVersion string `json:"schema_version"`
	// Mode 固定为 storyboard_preview，不支持 standalone。
	Mode string `json:"mode"`
	// SourceStoryboardPreviewRef 是生成全部 Prompt 的 Storyboard Preview 精确引用。
	SourceStoryboardPreviewRef StoryboardPreviewRef `json:"source_storyboard_preview_ref"`
	// Prompts 是按 Source Element 顺序及 Slot 数字顺序冻结的完整目标集。
	Prompts []PromptEntry `json:"prompts"`
}

// CanonicalJSON 校验 Content，并使用 Go JSON 默认 HTML escape 与冻结字段顺序生成紧凑 JSON。
func (content Content) CanonicalJSON() ([]byte, error) {
	if err := ValidateContent(content); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(content)
	if err != nil || len(encoded) == 0 || len(encoded) > maxCanonicalContentBytes {
		return nil, ErrInvalidInput
	}
	return encoded, nil
}

// ParseContentJSON 从 PostgreSQL jsonb 严格恢复 Content，并拒绝未知字段、尾随值和非法文本。
func ParseContentJSON(encoded []byte) (Content, error) {
	if len(encoded) == 0 || len(encoded) > maxCanonicalContentBytes*2 || !utf8.Valid(encoded) {
		return Content{}, ErrPersistence
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var content Content
	if err := decoder.Decode(&content); err != nil {
		return Content{}, ErrPersistence
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Content{}, ErrPersistence
	}
	if err := ValidateContent(content); err != nil {
		return Content{}, ErrPersistence
	}
	return cloneContent(content), nil
}

// ValidateContent 校验 Schema、Source 引用、目标字段、文本、枚举、去重和 128 KiB 边界。
func ValidateContent(content Content) error {
	if content.SchemaVersion != DraftSchemaVersion || content.Mode != DraftMode ||
		!ValidateStoryboardPreviewRef(content.SourceStoryboardPreviewRef) || content.Prompts == nil ||
		len(content.Prompts) < 1 || len(content.Prompts) > MaxPrompts {
		return ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(content.Prompts))
	outputLanguage := content.Prompts[0].OutputLanguage
	for _, prompt := range content.Prompts {
		expectedMediaKind, ok := MediaKindForSlotType(prompt.SlotType)
		if !targetLocalKeyPattern.MatchString(prompt.TargetLocalKey) ||
			!elementLocalKeyPattern.MatchString(prompt.ElementLocalKey) || !ok || prompt.MediaKind != expectedMediaKind ||
			!validText(prompt.Purpose, 1, 500) || !validText(prompt.PositivePrompt, 1, 4000) ||
			prompt.NegativeConstraints == nil || len(prompt.NegativeConstraints) > MaxNegativeConstraints ||
			!validUniqueTexts(prompt.NegativeConstraints, 1, 500) || !validOutputLanguage(prompt.OutputLanguage) ||
			prompt.OutputLanguage != outputLanguage {
			return ErrInvalidInput
		}
		if _, duplicated := seen[prompt.TargetLocalKey]; duplicated {
			return ErrInvalidInput
		}
		seen[prompt.TargetLocalKey] = struct{}{}
	}
	encoded, err := json.Marshal(content)
	if err != nil || len(encoded) == 0 || len(encoded) > maxCanonicalContentBytes {
		return ErrInvalidInput
	}
	return nil
}

// MediaKindForSlotType 执行设计冻结的五类 Slot 到四类媒体的唯一确定性映射。
func MediaKindForSlotType(slotType string) (string, bool) {
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

// validText 校验 NFC、Unicode scalar 数量、边界空白和控制字符。
func validText(value string, minimum int, maximum int) bool {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) || strings.TrimSpace(value) != value {
		return false
	}
	count := utf8.RuneCountInString(value)
	if count < minimum || count > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return false
		}
	}
	return true
}

// validUniqueTexts 校验负面约束非空、唯一且保留输入顺序。
func validUniqueTexts(values []string, minimum int, maximum int) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validText(value, minimum, maximum) {
			return false
		}
		if _, duplicated := seen[value]; duplicated {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

// validOutputLanguage 校验 Development Preview 唯一允许的两种输出语言。
func validOutputLanguage(value string) bool {
	return value == "zh-CN" || value == "en-US"
}

// cloneContent 深拷贝 Prompt 与负面约束列表，防止摘要完成后被调用方修改。
func cloneContent(value Content) Content {
	result := value
	result.Prompts = make([]PromptEntry, len(value.Prompts))
	copy(result.Prompts, value.Prompts)
	for index := range result.Prompts {
		result.Prompts[index].NegativeConstraints = append([]string{}, value.Prompts[index].NegativeConstraints...)
	}
	return result
}

// invalidTargetError 返回不暴露 Source 正文的稳定目标全集校验错误。
func invalidTargetError() error {
	return fmt.Errorf("%w: prompt target set does not match source storyboard", ErrInvalidInput)
}
