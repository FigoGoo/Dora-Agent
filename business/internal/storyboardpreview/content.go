// Package storyboardpreview 定义 Business-owned Storyboard Development Preview 草稿及其幂等保存边界。
package storyboardpreview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"golang.org/x/text/unicode/norm"
)

const (
	// MaxSections 是 Development Preview 允许的最大章节数量。
	MaxSections = 8
	// MaxElements 是 Development Preview 允许的最大元素数量。
	MaxElements = 24
	// MaxSlots 是 Development Preview 允许的最大媒体槽数量。
	MaxSlots = 96
	// MaxDependenciesPerElement 是单个元素允许引用的最大前置元素数量。
	MaxDependenciesPerElement = 8
	maxCanonicalContentBytes  = 64 * 1024
)

// ElementType 是 Storyboard Preview 元素类型的稳定小写代码。
type ElementType string

const (
	// ElementTypeScene 表示叙事场景元素。
	ElementTypeScene ElementType = "scene"
	// ElementTypeShot 表示镜头元素。
	ElementTypeShot ElementType = "shot"
	// ElementTypeNarration 表示旁白元素。
	ElementTypeNarration ElementType = "narration"
	// ElementTypeCaption 表示字幕元素。
	ElementTypeCaption ElementType = "caption"
	// ElementTypeAudio 表示声音设计元素。
	ElementTypeAudio ElementType = "audio"
)

// SlotType 是 Storyboard Preview 媒体槽类型的稳定小写代码。
type SlotType string

const (
	// SlotTypeImage 表示图片输入或产出槽。
	SlotTypeImage SlotType = "image"
	// SlotTypeVideo 表示视频输入或产出槽。
	SlotTypeVideo SlotType = "video"
	// SlotTypeAudio 表示音频输入或产出槽。
	SlotTypeAudio SlotType = "audio"
	// SlotTypeVoiceover 表示配音槽。
	SlotTypeVoiceover SlotType = "voiceover"
	// SlotTypeCaption 表示字幕槽。
	SlotTypeCaption SlotType = "caption"
)

// Section 是 Preview 内容内按数组位置冻结顺序的章节。
// Key 只在当前 Draft JSON 内稳定，不是生产 Storyboard Section ID。
type Section struct {
	// Key 是 section_1 至 section_8 的连续局部键。
	Key string `json:"key"`
	// Title 是章节标题。
	Title string `json:"title"`
	// Objective 是章节的叙事目标。
	Objective string `json:"objective"`
}

// Element 是 Preview 内容内的故事板元素。
// Key 和引用只服务当前 Draft 的确定性校验，不承诺生产 Element 身份。
type Element struct {
	// Key 是 element_1 至 element_24 的连续局部键。
	Key string `json:"key"`
	// SectionKey 引用同一内容中的章节局部键。
	SectionKey string `json:"section_key"`
	// Order 是全局从一开始的连续顺序。
	Order int32 `json:"order"`
	// Type 是稳定元素类型。
	Type ElementType `json:"element_type"`
	// Title 是元素标题。
	Title string `json:"title"`
	// NarrativePurpose 是元素在整体故事中的叙事作用。
	NarrativePurpose string `json:"narrative_purpose"`
	// DurationSeconds 是元素的正整数秒时长。
	DurationSeconds int32 `json:"duration_seconds"`
	// SourcePhaseKey 引用来源 CreationSpec 的 phase 局部键。
	SourcePhaseKey string `json:"source_phase_key"`
	// DependencyKeys 是零至八个前置元素局部键。
	DependencyKeys []string `json:"dependency_keys"`
}

// Slot 是 Preview 内容内附着到元素的媒体槽。
// Key 只在当前 Draft JSON 内稳定，不是生产 Storyboard Slot ID。
type Slot struct {
	// Key 是 slot_1 至 slot_96 的连续局部键。
	Key string `json:"key"`
	// ElementKey 引用同一内容中的元素局部键。
	ElementKey string `json:"element_key"`
	// Type 是稳定槽类型。
	Type SlotType `json:"slot_type"`
	// Purpose 描述槽的业务用途，不包含最终媒体 Prompt。
	Purpose string `json:"purpose"`
	// Required 表示后续生产规划中该槽是否必须满足；本 Preview 不触发生产。
	Required bool `json:"required"`
}

// Content 是 Storyboard Preview Draft 的严格结构化内容。
// 字段顺序同时是冻结的 Canonical JSON 顺序，变更时必须升级 Schema Version。
type Content struct {
	// Title 是 Storyboard Draft 标题。
	Title string `json:"title"`
	// Summary 是 Storyboard Draft 摘要。
	Summary string `json:"summary"`
	// Sections 是一至八个按数组位置排序的章节。
	Sections []Section `json:"sections"`
	// Elements 是一至二十四个全局连续排序的元素。
	Elements []Element `json:"elements"`
	// Slots 是零至九十六个按局部键排序的媒体槽。
	Slots []Slot `json:"slots"`
}

// CanonicalJSON 校验 Content，并按冻结字段顺序生成紧凑 JSON。
func (content Content) CanonicalJSON() ([]byte, error) {
	if err := ValidateContent(content); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(content)
	if err != nil || len(encoded) > maxCanonicalContentBytes {
		return nil, ErrInvalidInput
	}
	return encoded, nil
}

// ParseContentJSON 从 PostgreSQL jsonb 严格恢复 Content，并拒绝未知字段和尾随值。
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
	return content, nil
}

// ValidateContent 校验字段、枚举、连续局部键、引用、数量、顺序、时长和依赖 DAG。
func ValidateContent(content Content) error {
	if !validText(content.Title, 1, 120, false) || !validText(content.Summary, 1, 1000, false) ||
		content.Sections == nil || len(content.Sections) < 1 || len(content.Sections) > MaxSections ||
		content.Elements == nil || len(content.Elements) < 1 || len(content.Elements) > MaxElements ||
		content.Slots == nil || len(content.Slots) > MaxSlots {
		return ErrInvalidInput
	}

	sectionKeys := make(map[string]struct{}, len(content.Sections))
	sectionElementCounts := make(map[string]int, len(content.Sections))
	for index, section := range content.Sections {
		expectedKey := fmt.Sprintf("section_%d", index+1)
		if section.Key != expectedKey || !validText(section.Title, 1, 100, false) ||
			!validText(section.Objective, 1, 500, false) {
			return ErrInvalidInput
		}
		sectionKeys[section.Key] = struct{}{}
	}

	elementKeys := make(map[string]struct{}, len(content.Elements))
	totalDuration := int64(0)
	for index, element := range content.Elements {
		expectedKey := fmt.Sprintf("element_%d", index+1)
		if element.Key != expectedKey || element.Order != int32(index+1) ||
			!validElementType(element.Type) || !validText(element.Title, 1, 120, false) ||
			!validText(element.NarrativePurpose, 1, 1000, false) || element.DurationSeconds < 1 ||
			element.DurationSeconds > 600 || !validLocalKey(element.SourcePhaseKey, "phase_", 1, 6) ||
			element.DependencyKeys == nil || len(element.DependencyKeys) > MaxDependenciesPerElement {
			return ErrInvalidInput
		}
		if _, exists := sectionKeys[element.SectionKey]; !exists {
			return ErrInvalidInput
		}
		elementKeys[element.Key] = struct{}{}
		sectionElementCounts[element.SectionKey]++
		totalDuration += int64(element.DurationSeconds)
	}
	if totalDuration < 5 || totalDuration > 600 {
		return ErrInvalidInput
	}
	for sectionKey := range sectionKeys {
		if sectionElementCounts[sectionKey] == 0 {
			return ErrInvalidInput
		}
	}

	for _, element := range content.Elements {
		seen := make(map[string]struct{}, len(element.DependencyKeys))
		for _, dependencyKey := range element.DependencyKeys {
			if dependencyKey == element.Key {
				return ErrInvalidInput
			}
			if _, exists := elementKeys[dependencyKey]; !exists {
				return ErrInvalidInput
			}
			if _, exists := seen[dependencyKey]; exists {
				return ErrInvalidInput
			}
			seen[dependencyKey] = struct{}{}
		}
	}
	if !acyclicDependencies(content.Elements) {
		return ErrInvalidInput
	}

	slotCounts := make(map[string]int, len(content.Elements))
	for index, slot := range content.Slots {
		expectedKey := fmt.Sprintf("slot_%d", index+1)
		if slot.Key != expectedKey || !validSlotType(slot.Type) || !validText(slot.Purpose, 1, 500, false) {
			return ErrInvalidInput
		}
		if _, exists := elementKeys[slot.ElementKey]; !exists {
			return ErrInvalidInput
		}
		slotCounts[slot.ElementKey]++
		if slotCounts[slot.ElementKey] > 4 {
			return ErrInvalidInput
		}
	}
	return nil
}

// ValidateAgainstCreationSpec 校验全部 source_phase_key 都引用同一权威 CreationSpec Draft 的现有 phase。
func ValidateAgainstCreationSpec(content Content, source creationspec.Content) error {
	if err := ValidateContent(content); err != nil {
		return err
	}
	if err := creationspec.ValidateContent(source); err != nil {
		return ErrPersistence
	}
	phaseKeys := make(map[string]struct{}, len(source.Phases))
	for _, phase := range source.Phases {
		phaseKeys[phase.Key] = struct{}{}
	}
	for _, element := range content.Elements {
		if _, exists := phaseKeys[element.SourcePhaseKey]; !exists {
			return ErrInvalidInput
		}
	}
	return nil
}

// acyclicDependencies 使用 Kahn 拓扑排序验证元素依赖无环，时间和空间均受 MaxElements 限制。
func acyclicDependencies(elements []Element) bool {
	indegree := make(map[string]int, len(elements))
	dependents := make(map[string][]string, len(elements))
	for _, element := range elements {
		indegree[element.Key] = len(element.DependencyKeys)
		for _, dependency := range element.DependencyKeys {
			dependents[dependency] = append(dependents[dependency], element.Key)
		}
	}
	queue := make([]string, 0, len(elements))
	for _, element := range elements {
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
	return visited == len(elements)
}

// validElementType 判断元素类型是否属于 Development Preview 冻结枚举。
func validElementType(value ElementType) bool {
	switch value {
	case ElementTypeScene, ElementTypeShot, ElementTypeNarration, ElementTypeCaption, ElementTypeAudio:
		return true
	default:
		return false
	}
}

// validSlotType 判断媒体槽类型是否属于 Development Preview 冻结枚举。
func validSlotType(value SlotType) bool {
	switch value {
	case SlotTypeImage, SlotTypeVideo, SlotTypeAudio, SlotTypeVoiceover, SlotTypeCaption:
		return true
	default:
		return false
	}
}

// validLocalKey 校验局部键使用冻结前缀和有界十进制序号。
func validLocalKey(value string, prefix string, minimum int, maximum int) bool {
	for number := minimum; number <= maximum; number++ {
		if value == fmt.Sprintf("%s%d", prefix, number) {
			return true
		}
	}
	return false
}

// validText 校验 UTF-8、NFC、Rune 长度、边界空白与不可见控制字符。
func validText(value string, minimumRunes int, maximumRunes int, allowEmpty bool) bool {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) || strings.TrimSpace(value) != value {
		return false
	}
	count := utf8.RuneCountInString(value)
	if count == 0 && allowEmpty {
		return true
	}
	if count < minimumRunes || count > maximumRunes {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return false
		}
	}
	return true
}
