package storyboardpreview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// storyboardDigestFixture 是跨 Module 保存摘要语料的最小测试映射。
type storyboardDigestFixture struct {
	Canonical struct {
		UserID                 string `json:"user_id"`
		ProjectID              string `json:"project_id"`
		ExpectedProjectVersion int64  `json:"expected_project_version"`
		CreationSpecRef        struct {
			ID            string `json:"id"`
			Version       int64  `json:"version"`
			ContentDigest string `json:"content_digest"`
		} `json:"creation_spec_ref"`
		ToolCallID       string  `json:"tool_call_id"`
		PromptVersion    string  `json:"prompt_version"`
		ValidatorVersion string  `json:"validator_version"`
		Content          Content `json:"content"`
	} `json:"canonical"`
	ExpectedSHA256 string `json:"expected_sha256"`
}

// TestSaveRequestDigestMatchesCrossModuleFixture 验证 Business 与 Agent 共享的嵌套 creation_spec_ref 字段顺序和摘要。
func TestSaveRequestDigestMatchesCrossModuleFixture(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Storyboard digest fixture path")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "docs", "design", "cross-module", "testdata", "storyboard_preview_save_digest_v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Storyboard digest fixture: %v", err)
	}
	var fixture storyboardDigestFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode Storyboard digest fixture: %v", err)
	}
	creationSpecDigest, err := ParseDigest(fixture.Canonical.CreationSpecRef.ContentDigest)
	if err != nil {
		t.Fatalf("parse CreationSpec digest: %v", err)
	}
	digest, err := SaveRequestDigest(
		fixture.Canonical.UserID, fixture.Canonical.ProjectID, fixture.Canonical.ExpectedProjectVersion,
		CreationSpecRef{
			ID: fixture.Canonical.CreationSpecRef.ID, Version: fixture.Canonical.CreationSpecRef.Version,
			ContentDigest: creationSpecDigest,
		},
		fixture.Canonical.ToolCallID, fixture.Canonical.PromptVersion,
		fixture.Canonical.ValidatorVersion, fixture.Canonical.Content,
	)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	if digest.Hex() != fixture.ExpectedSHA256 {
		t.Fatalf("SaveRequestDigest() = %s, want %s", digest.Hex(), fixture.ExpectedSHA256)
	}
}

// TestValidateContentEnforcesReferencesDAGAndPreviewLimits 覆盖章节覆盖、每元素槽上限、依赖环和文本长度边界。
func TestValidateContentEnforcesReferencesDAGAndPreviewLimits(t *testing.T) {
	valid := validStoryboardContentForTest()
	if err := ValidateContent(valid); err != nil {
		t.Fatalf("valid content rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Content)
	}{
		{name: "empty section", mutate: func(content *Content) {
			content.Sections = append(content.Sections, Section{Key: "section_2", Title: "空章节", Objective: "不得没有元素"})
		}},
		{name: "dependency cycle", mutate: func(content *Content) {
			content.Elements[0].DependencyKeys = []string{"element_2"}
		}},
		{name: "five slots on one element", mutate: func(content *Content) {
			for index := 3; index <= 5; index++ {
				content.Slots = append(content.Slots, Slot{Key: "slot_" + string(rune('0'+index)), ElementKey: "element_1", Type: SlotTypeImage, Purpose: "额外槽", Required: false})
			}
		}},
		{name: "title too long", mutate: func(content *Content) { content.Title = strings.Repeat("长", 121) }},
		{name: "summary too long", mutate: func(content *Content) { content.Summary = strings.Repeat("长", 1001) }},
		{name: "section title too long", mutate: func(content *Content) { content.Sections[0].Title = strings.Repeat("长", 101) }},
		{name: "section objective too long", mutate: func(content *Content) { content.Sections[0].Objective = strings.Repeat("长", 501) }},
		{name: "element title too long", mutate: func(content *Content) { content.Elements[0].Title = strings.Repeat("长", 121) }},
		{name: "element purpose too long", mutate: func(content *Content) { content.Elements[0].NarrativePurpose = strings.Repeat("长", 1001) }},
		{name: "slot purpose too long", mutate: func(content *Content) { content.Slots[0].Purpose = strings.Repeat("长", 501) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := validStoryboardContentForTest()
			test.mutate(&candidate)
			if err := ValidateContent(candidate); err == nil {
				t.Fatal("invalid Storyboard content was accepted")
			}
		})
	}
}

// validStoryboardContentForTest 返回包含连续局部键、合法引用和无环依赖的最小内容。
func validStoryboardContentForTest() Content {
	return Content{
		Title: "夏日品牌短片故事板", Summary: "以产品亮相和使用场景串联两段叙事。",
		Sections: []Section{{Key: "section_1", Title: "开场", Objective: "建立夏日氛围"}},
		Elements: []Element{
			{Key: "element_1", SectionKey: "section_1", Order: 1, Type: ElementTypeScene, Title: "海边开场", NarrativePurpose: "建立情绪", DurationSeconds: 10, SourcePhaseKey: "phase_1", DependencyKeys: []string{}},
			{Key: "element_2", SectionKey: "section_1", Order: 2, Type: ElementTypeShot, Title: "产品特写", NarrativePurpose: "突出卖点", DurationSeconds: 20, SourcePhaseKey: "phase_1", DependencyKeys: []string{"element_1"}},
		},
		Slots: []Slot{
			{Key: "slot_1", ElementKey: "element_1", Type: SlotTypeVideo, Purpose: "环境画面", Required: true},
			{Key: "slot_2", ElementKey: "element_1", Type: SlotTypeImage, Purpose: "产品图片", Required: true},
		},
	}
}
