package planstoryboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type storyboardDigestVector struct {
	SchemaVersion  string         `json:"schema_version"`
	Canonical      saveDigestWire `json:"canonical"`
	CanonicalJSON  string         `json:"canonical_json"`
	ExpectedSHA256 string         `json:"expected_sha256"`
}

func TestSaveRequestDigestConsumesCrossModuleVector(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "..", "docs", "design", "cross-module", "testdata", "storyboard_preview_save_digest_v1.json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 Storyboard 跨 Module 摘要向量失败: %v", err)
	}
	var vector storyboardDigestVector
	if err := json.Unmarshal(encoded, &vector); err != nil {
		t.Fatalf("解析 Storyboard 跨 Module 摘要向量失败: %v", err)
	}
	canonical, err := json.Marshal(vector.Canonical)
	if err != nil || string(canonical) != vector.CanonicalJSON {
		t.Fatalf("canonical JSON 漂移: err=%v\n got=%s\nwant=%s", err, canonical, vector.CanonicalJSON)
	}
	command := DraftCommand{
		TrustedContext: TrustedContext{
			UserID: vector.Canonical.UserID, ProjectID: vector.Canonical.ProjectID,
			CreationSpecRef: vector.Canonical.CreationSpecRef, ToolCallID: vector.Canonical.ToolCallID,
			PromptVersion: vector.Canonical.PromptVersion, ValidatorVersion: vector.Canonical.ValidatorVersion,
		},
		DomainContext: PlanningContext{ProjectID: vector.Canonical.ProjectID, ProjectVersion: vector.Canonical.ExpectedProjectVersion},
		Content:       vector.Canonical.Content,
	}
	digest, err := SaveRequestDigest(command)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	expectedRaw := sha256.Sum256([]byte(vector.CanonicalJSON))
	if digest != vector.ExpectedSHA256 || digest != hex.EncodeToString(expectedRaw[:]) {
		t.Fatalf("SaveRequestDigest()=%q want=%q", digest, vector.ExpectedSHA256)
	}
}

func TestDecodeIntentRejectsTrustedAndPromptFields(t *testing.T) {
	t.Parallel()
	valid := `{"schema_version":"plan_storyboard.preview.intent.v1","planning_instruction":"规划一段夏日品牌短片"}`
	tests := map[string]string{
		"valid":            valid,
		"creation spec id": strings.TrimSuffix(valid, "}") + `,"creation_spec_id":"019f68e8-0010-7000-8000-000000000010"}`,
		"prompt":           strings.TrimSuffix(valid, "}") + `,"prompt":"生成产品广告"}`,
		"duplicate":        strings.TrimSuffix(valid, "}") + `,"planning_instruction":"覆盖"}`,
		"trailing":         valid + `{}`,
	}
	for name, raw := range tests {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeIntent([]byte(raw))
			if name == "valid" && err != nil {
				t.Fatalf("合法 Intent 被拒绝: %v", err)
			}
			if name != "valid" && err == nil {
				t.Fatal("非法 Intent 被接受")
			}
		})
	}
}

func TestCandidateRejectsPromptAndDependencyCycle(t *testing.T) {
	t.Parallel()
	intent := Intent{SchemaVersion: IntentSchemaVersion, PlanningInstruction: "规划两段式短片"}
	contextValue := testPlanningContext(t)
	candidate := testCandidate()
	encoded, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	document["prompt"] = "不得进入候选"
	withPrompt, _ := json.Marshal(document)
	if _, _, err := DecodeAndValidateCandidate(withPrompt, intent, contextValue); err == nil {
		t.Fatal("含 prompt 字段的 Candidate 被接受")
	}
	content, _, err := DecodeAndValidateCandidate(encoded, intent, contextValue)
	if err != nil {
		t.Fatalf("合法 Candidate 被拒绝: %v", err)
	}
	content.Elements[0].DependencyKeys = []string{"element_2"}
	content.Elements[1].DependencyKeys = []string{"element_1"}
	if err := ValidateDependencyGraph(content); err == nil {
		t.Fatal("依赖环被接受")
	}
}

func TestCandidateBoundaryTable(t *testing.T) {
	t.Parallel()
	contextValue := testPlanningContext(t)
	tests := map[string]struct {
		mutate       func(*Candidate)
		target       *int
		wantAccepted bool
	}{
		"valid":                  {wantAccepted: true},
		"nil slots":              {mutate: func(candidate *Candidate) { candidate.Slots = nil }},
		"section gap":            {mutate: func(candidate *Candidate) { candidate.Sections[1].Key = "section_3" }},
		"empty section":          {mutate: func(candidate *Candidate) { candidate.Elements[1].SectionKey = "section_1" }},
		"element title too long": {mutate: func(candidate *Candidate) { candidate.Elements[0].Title = strings.Repeat("甲", 121) }},
		"unknown phase":          {mutate: func(candidate *Candidate) { candidate.Elements[0].SourcePhaseKey = "phase_3" }},
		"five slots": {mutate: func(candidate *Candidate) {
			candidate.Slots = []Slot{
				{Key: "slot_1", ElementKey: "element_1", SlotType: "image", Purpose: "一", Required: true},
				{Key: "slot_2", ElementKey: "element_1", SlotType: "image", Purpose: "二", Required: true},
				{Key: "slot_3", ElementKey: "element_1", SlotType: "image", Purpose: "三", Required: true},
				{Key: "slot_4", ElementKey: "element_1", SlotType: "image", Purpose: "四", Required: true},
				{Key: "slot_5", ElementKey: "element_1", SlotType: "image", Purpose: "五", Required: true},
			}
		}},
		"target mismatch": {target: integerPointer(600)},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			candidate := testCandidate()
			if test.mutate != nil {
				test.mutate(&candidate)
			}
			encoded, _ := json.Marshal(candidate)
			intent := Intent{SchemaVersion: IntentSchemaVersion, PlanningInstruction: "规划短片", TargetDurationSeconds: test.target}
			_, _, err := DecodeAndValidateCandidate(encoded, intent, contextValue)
			if (err == nil) != test.wantAccepted {
				t.Fatalf("DecodeAndValidateCandidate() error=%v wantAccepted=%v", err, test.wantAccepted)
			}
		})
	}
}

func TestCandidateAndContentTotalDurationBoundaries(t *testing.T) {
	t.Parallel()
	contextValue := testPlanningContext(t)
	for _, test := range []struct {
		name         string
		first        int
		second       int
		wantAccepted bool
	}{
		{name: "four seconds", first: 2, second: 2},
		{name: "five seconds", first: 2, second: 3, wantAccepted: true},
		{name: "six hundred seconds", first: 300, second: 300, wantAccepted: true},
		{name: "six hundred one seconds", first: 300, second: 301},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate := testCandidate()
			candidate.Elements[0].DurationSeconds = test.first
			candidate.Elements[1].DurationSeconds = test.second
			encoded, _ := json.Marshal(candidate)
			intent := Intent{SchemaVersion: IntentSchemaVersion, PlanningInstruction: "规划短片"}
			content, _, candidateErr := DecodeAndValidateCandidate(encoded, intent, contextValue)
			directContent := Content{
				Title: candidate.Title, Summary: candidate.Summary,
				Sections: cloneSections(candidate.Sections), Elements: cloneElements(candidate.Elements), Slots: cloneSlots(candidate.Slots),
			}
			contentErr := ValidateContent(directContent)
			if (candidateErr == nil) != test.wantAccepted {
				t.Fatalf("Candidate error=%v wantAccepted=%v", candidateErr, test.wantAccepted)
			}
			if (contentErr == nil) != test.wantAccepted {
				t.Fatalf("Content error=%v wantAccepted=%v", contentErr, test.wantAccepted)
			}
			if test.wantAccepted {
				if err := ValidateContent(content); err != nil {
					t.Fatalf("Candidate 映射 Content 未通过: %v", err)
				}
			}
		})
	}
}

func TestContentDigestRejectsCanonicalJSONOver64KiB(t *testing.T) {
	t.Parallel()
	content := maximalStoryboardContent()
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) <= maxJSONBytes {
		t.Fatalf("测试正文仅 %d bytes，未超过 64KiB", len(encoded))
	}
	if err := ValidateContent(content); err != nil {
		t.Fatalf("超限正文在编码大小之外应保持结构合法: %v", err)
	}
	if _, err := ContentDigest(content); err == nil {
		t.Fatal("超过 64KiB 的 canonical Content 被计算摘要")
	}
}

func maximalStoryboardContent() Content {
	content := Content{
		Title: strings.Repeat("题", 120), Summary: strings.Repeat("摘", 1000),
		Sections: make([]Section, maxSections), Elements: make([]Element, maxElements), Slots: make([]Slot, maxSlots),
	}
	for index := range content.Sections {
		content.Sections[index] = Section{
			Key: "section_" + integerText(index+1), Title: strings.Repeat("节", 100), Objective: strings.Repeat("目", 500),
		}
	}
	for index := range content.Elements {
		content.Elements[index] = Element{
			Key: "element_" + integerText(index+1), SectionKey: "section_" + integerText(index%maxSections+1),
			Order: index + 1, ElementType: "scene", Title: strings.Repeat("元", 120),
			NarrativePurpose: strings.Repeat("叙", 1000), DurationSeconds: 25,
			SourcePhaseKey: "phase_1", DependencyKeys: []string{},
		}
	}
	for index := range content.Slots {
		content.Slots[index] = Slot{
			Key: "slot_" + integerText(index+1), ElementKey: "element_" + integerText(index/maxSlotsPerElement+1),
			SlotType: "image", Purpose: strings.Repeat("槽", 500), Required: true,
		}
	}
	return content
}

func integerText(value int) string {
	return strconv.Itoa(value)
}

func integerPointer(value int) *int { return &value }
