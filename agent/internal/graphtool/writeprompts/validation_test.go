package writeprompts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// promptDigestVector 是 Agent 与 Business 共同消费的保存摘要固定语料。
type promptDigestVector struct {
	SchemaVersion  string         `json:"schema_version"`
	Canonical      saveDigestWire `json:"canonical"`
	CanonicalJSON  string         `json:"canonical_json"`
	ExpectedSHA256 string         `json:"expected_sha256"`
}

func TestDecodeIntentRejectsUnknownDuplicateTrailingAndIllegalLanguage(t *testing.T) {
	t.Parallel()
	valid := `{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"为全部目标编写提示词","output_language":"zh-CN"}`
	for _, test := range []struct {
		name         string
		raw          string
		wantAccepted bool
	}{
		{name: "valid", raw: valid, wantAccepted: true},
		{name: "unknown storyboard", raw: strings.TrimSuffix(valid, "}") + `,"storyboard_preview_id":"` + previewTestStoryboardID + `"}`},
		{name: "unknown target", raw: strings.TrimSuffix(valid, "}") + `,"target_local_key":"slot_1"}`},
		{name: "unknown prompt", raw: strings.TrimSuffix(valid, "}") + `,"positive_prompt":"不得由 Tool Intent 填写"}`},
		{name: "unknown identity", raw: strings.TrimSuffix(valid, "}") + `,"user_id":"` + previewTestUserID + `"}`},
		{name: "duplicate", raw: strings.TrimSuffix(valid, "}") + `,"writing_instruction":"覆盖原要求"}`},
		{name: "trailing", raw: valid + `{}`},
		{name: "illegal language", raw: strings.Replace(valid, "zh-CN", "ja-JP", 1)},
		{name: "boundary whitespace", raw: strings.Replace(valid, "为全部目标编写提示词", " 带边界空白", 1)},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeIntent([]byte(test.raw))
			if (err == nil) != test.wantAccepted {
				t.Fatalf("DecodeIntent() error=%v wantAccepted=%v", err, test.wantAccepted)
			}
		})
	}
}

func TestIntentRejectsInvalidSurrogatesAndAcceptsValidPair(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"\uD800"}`,
		`{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"\uDC00"}`,
		`{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"\uD800\u0041"}`,
	} {
		if _, err := DecodeIntent([]byte(raw)); err == nil {
			t.Fatalf("非法 surrogate 被接受: %s", raw)
		}
	}
	valid := `{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"\uD83D\uDE00"}`
	if _, err := DecodeIntent([]byte(valid)); err != nil {
		t.Fatalf("合法 surrogate pair 被拒绝: %v", err)
	}
}

func TestTrustedContextRejectsNonV7UUIDAndNonCanonicalDigest(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	for _, test := range []struct {
		name   string
		mutate func(*TrustedContext)
	}{
		{name: "uuid v4", mutate: func(value *TrustedContext) { value.RunID = "550e8400-e29b-41d4-a716-446655440000" }},
		{name: "uppercase digest", mutate: func(value *TrustedContext) {
			value.StoryboardPreviewRef.ContentDigest = strings.ToUpper(value.StoryboardPreviewRef.ContentDigest)
		}},
		{name: "resend budget", mutate: func(value *TrustedContext) { value.Policy.MaxCommandResends = 2 }},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			trusted := previewTestTrustedContext(contextValue, 8)
			test.mutate(&trusted)
			if err := ValidateTrustedContext(trusted); err == nil {
				t.Fatal("非法可信上下文被接受")
			}
		})
	}
}

// TestSaveRequestDigestMatchesBusinessGolden 钉住 Agent 与 Business 不共享 Go 包时仍逐字一致的跨 Module 摘要。
func TestSaveRequestDigestMatchesBusinessGolden(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "..", "docs", "design", "cross-module", "testdata", "prompt_preview_save_digest_v1.json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 Prompt Preview 跨 Module 摘要向量失败: %v", err)
	}
	var vector promptDigestVector
	if err := json.Unmarshal(encoded, &vector); err != nil {
		t.Fatalf("解析 Prompt Preview 跨 Module 摘要向量失败: %v", err)
	}
	canonical, err := json.Marshal(vector.Canonical)
	if err != nil || string(canonical) != vector.CanonicalJSON {
		t.Fatalf("canonical JSON 漂移: err=%v\n got=%s\nwant=%s", err, canonical, vector.CanonicalJSON)
	}
	sourceRef := vector.Canonical.StoryboardPreviewRef
	contextValue := previewTestGenerationContext()
	contextValue.ProjectID = vector.Canonical.ProjectID
	contextValue.ProjectVersion = vector.Canonical.ExpectedProjectVersion
	contextValue.Storyboard.ID = sourceRef.ID
	contextValue.Storyboard.ProjectID = vector.Canonical.ProjectID
	contextValue.Storyboard.ContentDigest = sourceRef.ContentDigest
	trusted := previewTestTrustedContext(contextValue, 2)
	trusted.UserID = vector.Canonical.UserID
	trusted.ProjectID = vector.Canonical.ProjectID
	trusted.ToolCallID = vector.Canonical.ToolCallID
	trusted.StoryboardPreviewRef = sourceRef
	trusted.PromptVersion = vector.Canonical.PromptVersion
	trusted.ValidatorVersion = vector.Canonical.ValidatorVersion
	trusted.ExactSetValidatorVersion = vector.Canonical.ExactSetValidatorVersion
	targets := []PromptTarget{
		{TargetLocalKey: "slot_2", ElementLocalKey: "element_1", ElementTitle: "海边开场", NarrativePurpose: "建立情绪", SlotType: "image", MediaKind: "image", Purpose: "海边环境图", Required: false},
		{TargetLocalKey: "slot_1", ElementLocalKey: "element_2", ElementTitle: "产品特写", NarrativePurpose: "突出卖点", SlotType: "video", MediaKind: "video", Purpose: "产品动态特写", Required: true},
	}
	digest, err := SaveRequestDigest(DraftCommand{
		TrustedContext: trusted, DomainContext: contextValue, Targets: targets,
		ExactTargetSetDigest: vector.Canonical.ExactTargetSetDigest, Content: vector.Canonical.Content,
		ResendLimit: trusted.Policy.MaxCommandResends,
	})
	if err != nil {
		t.Fatalf("SaveRequestDigest() error=%v", err)
	}
	expectedRaw := sha256.Sum256([]byte(vector.CanonicalJSON))
	if digest != vector.ExpectedSHA256 || digest != hex.EncodeToString(expectedRaw[:]) {
		t.Fatalf("SaveRequestDigest()=%q want=%q", digest, vector.ExpectedSHA256)
	}
}

func TestResolveExactTargetsSortsAllSlotsAndMapsMediaKinds(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	contextValue.Storyboard.Content.Slots = []StoryboardSlot{
		{Key: "slot_5", ElementKey: "element_2", SlotType: "caption", Purpose: "结尾字幕", Required: true},
		{Key: "slot_3", ElementKey: "element_2", SlotType: "audio", Purpose: "环境音效", Required: false},
		{Key: "slot_1", ElementKey: "element_1", SlotType: "image", Purpose: "开场主视觉", Required: true},
		{Key: "slot_4", ElementKey: "element_2", SlotType: "voiceover", Purpose: "产品旁白", Required: true},
		{Key: "slot_2", ElementKey: "element_1", SlotType: "video", Purpose: "开场动态镜头", Required: true},
	}
	intent := Intent{SchemaVersion: IntentSchemaVersion, WritingInstruction: "按故事板目标逐项编写", OutputLanguage: "en-US"}
	intentDigest, err := IntentDigest(intent)
	if err != nil {
		t.Fatal(err)
	}
	targets, digest, language, err := ResolveExactTargets(contextValue, intent, intentDigest, previewTestTrustedContext(contextValue, 5))
	if err != nil {
		t.Fatalf("ResolveExactTargets() error=%v", err)
	}
	wantKeys := []string{"slot_1", "slot_2", "slot_3", "slot_4", "slot_5"}
	wantKinds := []string{"image", "video", "audio", "audio", "text"}
	gotKeys := make([]string, len(targets))
	gotKinds := make([]string, len(targets))
	for index, target := range targets {
		gotKeys[index] = target.TargetLocalKey
		gotKinds[index] = target.MediaKind
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) || !reflect.DeepEqual(gotKinds, wantKinds) ||
		language != "en-US" || !validLowerSHA256(digest) {
		t.Fatalf("keys=%v kinds=%v language=%q digest=%q", gotKeys, gotKinds, language, digest)
	}
}

func TestResolveExactTargetsRejectsZeroTargetsAndFrozenBudgetOverflow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		maxTargets int
		mutate     func(*GenerationContext)
		wantErr    error
	}{
		{name: "zero", maxTargets: 8, mutate: func(value *GenerationContext) { value.Storyboard.Content.Slots = []StoryboardSlot{} }, wantErr: ErrNoTargets},
		{name: "budget", maxTargets: 2, mutate: func(*GenerationContext) {}, wantErr: ErrTargetBudgetExceeded},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			contextValue := previewTestGenerationContext()
			test.mutate(&contextValue)
			intent := Intent{SchemaVersion: IntentSchemaVersion, WritingInstruction: "按全部目标编写"}
			digest, err := IntentDigest(intent)
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, err = ResolveExactTargets(contextValue, intent, digest, previewTestTrustedContext(contextValue, test.maxTargets))
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ResolveExactTargets() error=%v want=%v", err, test.wantErr)
			}
		})
	}
}

func TestCandidateProtocolAndTextBoundaries(t *testing.T) {
	t.Parallel()
	maxConstraints := make([]string, maxNegativeConstraints)
	for index := range maxConstraints {
		maxConstraints[index] = fmt.Sprintf("%02d", index) + strings.Repeat("禁", negativeConstraintMax-2)
	}
	for _, test := range []struct {
		name         string
		candidate    Candidate
		wantAccepted bool
	}{
		{
			name: "minimum",
			candidate: Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: []CandidatePrompt{
				{TargetLocalKey: "slot_1", PositivePrompt: "图", NegativeConstraints: []string{}},
			}},
			wantAccepted: true,
		},
		{
			name: "maximum",
			candidate: Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: []CandidatePrompt{
				{TargetLocalKey: "slot_1", PositivePrompt: strings.Repeat("图", positivePromptMax), NegativeConstraints: maxConstraints},
			}},
			wantAccepted: true,
		},
		{
			name: "empty positive",
			candidate: Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: []CandidatePrompt{
				{TargetLocalKey: "slot_1", PositivePrompt: "", NegativeConstraints: []string{}},
			}},
		},
		{
			name: "positive too long",
			candidate: Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: []CandidatePrompt{
				{TargetLocalKey: "slot_1", PositivePrompt: strings.Repeat("图", positivePromptMax+1), NegativeConstraints: []string{}},
			}},
		},
		{
			name: "nil negative",
			candidate: Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: []CandidatePrompt{
				{TargetLocalKey: "slot_1", PositivePrompt: "图", NegativeConstraints: nil},
			}},
		},
		{
			name: "too many negative",
			candidate: Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: []CandidatePrompt{
				{TargetLocalKey: "slot_1", PositivePrompt: "图", NegativeConstraints: append(maxConstraints, "额外约束")},
			}},
		},
		{
			name: "duplicate negative",
			candidate: Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: []CandidatePrompt{
				{TargetLocalKey: "slot_1", PositivePrompt: "图", NegativeConstraints: []string{"无水印", "无水印"}},
			}},
		},
		{
			name: "non NFC",
			candidate: Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: []CandidatePrompt{
				{TargetLocalKey: "slot_1", PositivePrompt: "e\u0301", NegativeConstraints: []string{}},
			}},
		},
		{
			name: "control character",
			candidate: Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: []CandidatePrompt{
				{TargetLocalKey: "slot_1", PositivePrompt: "图\n像", NegativeConstraints: []string{}},
			}},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.Marshal(test.candidate)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeAndValidateCandidate(encoded)
			if (err == nil) != test.wantAccepted {
				t.Fatalf("DecodeAndValidateCandidate() error=%v wantAccepted=%v", err, test.wantAccepted)
			}
		})
	}

	for name, raw := range map[string]string{
		"unknown":   `{"schema_version":"prompt.preview.candidate.v1","prompts":[{"target_local_key":"slot_1","positive_prompt":"图","negative_constraints":[],"prompt":"禁止字段"}]}`,
		"duplicate": `{"schema_version":"prompt.preview.candidate.v1","prompts":[{"target_local_key":"slot_1","positive_prompt":"图","positive_prompt":"覆盖","negative_constraints":[]}]}`,
		"trailing":  `{"schema_version":"prompt.preview.candidate.v1","prompts":[{"target_local_key":"slot_1","positive_prompt":"图","negative_constraints":[]}]}{}`,
	} {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeAndValidateCandidate([]byte(raw)); err == nil {
				t.Fatal("非法 Candidate JSON 被接受")
			}
		})
	}
}

func TestExactSetRejectsMissingExtraDuplicateUnknownAndReorder(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	intent := Intent{SchemaVersion: IntentSchemaVersion, WritingInstruction: "按全部目标编写"}
	intentDigest, err := IntentDigest(intent)
	if err != nil {
		t.Fatal(err)
	}
	targets, _, language, err := ResolveExactTargets(contextValue, intent, intentDigest, previewTestTrustedContext(contextValue, 8))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name         string
		mutate       func(*Candidate)
		wantAccepted bool
	}{
		{name: "valid", mutate: func(*Candidate) {}, wantAccepted: true},
		{name: "missing", mutate: func(value *Candidate) { value.Prompts = value.Prompts[:2] }},
		{name: "extra", mutate: func(value *Candidate) {
			value.Prompts = append(value.Prompts, CandidatePrompt{TargetLocalKey: "slot_4", PositivePrompt: "额外", NegativeConstraints: []string{}})
		}},
		{name: "duplicate", mutate: func(value *Candidate) { value.Prompts[1].TargetLocalKey = "slot_1" }},
		{name: "unknown", mutate: func(value *Candidate) { value.Prompts[1].TargetLocalKey = "slot_96" }},
		{name: "reorder", mutate: func(value *Candidate) { value.Prompts[0], value.Prompts[1] = value.Prompts[1], value.Prompts[0] }},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate := previewTestCandidate()
			test.mutate(&candidate)
			content, err := ValidateExactTargetSet(candidate, targets, language, previewTestStoryboardRef(contextValue))
			if (err == nil) != test.wantAccepted {
				t.Fatalf("ValidateExactTargetSet() error=%v wantAccepted=%v", err, test.wantAccepted)
			}
			if test.wantAccepted && (len(content.Prompts) != len(targets) || content.Prompts[1].MediaKind != "audio") {
				t.Fatalf("content=%+v", content)
			}
		})
	}
}

func TestEmptyNegativeConstraintsRemainNonNullAcrossValidationClones(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	intent := Intent{SchemaVersion: IntentSchemaVersion, WritingInstruction: "按全部目标编写"}
	intentDigest, err := IntentDigest(intent)
	if err != nil {
		t.Fatal(err)
	}
	targets, _, language, err := ResolveExactTargets(contextValue, intent, intentDigest, previewTestTrustedContext(contextValue, 8))
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{SchemaVersion: CandidateSchemaVersion, Prompts: make([]CandidatePrompt, len(targets))}
	for index, target := range targets {
		candidate.Prompts[index] = CandidatePrompt{
			TargetLocalKey: target.TargetLocalKey, PositivePrompt: "清晰提示词", NegativeConstraints: []string{},
		}
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAndValidateCandidate(encoded)
	if err != nil {
		t.Fatalf("DecodeAndValidateCandidate() error=%v", err)
	}
	if _, err := CandidateDigest(decoded); err != nil {
		t.Fatalf("CandidateDigest() error=%v", err)
	}
	content, err := ValidateExactTargetSet(decoded, targets, language, previewTestStoryboardRef(contextValue))
	if err != nil {
		t.Fatalf("ValidateExactTargetSet() error=%v", err)
	}
	for index, prompt := range content.Prompts {
		if prompt.NegativeConstraints == nil || len(prompt.NegativeConstraints) != 0 {
			t.Fatalf("prompts[%d].negative_constraints=%v，应保持非 null 空数组", index, prompt.NegativeConstraints)
		}
	}
}

func previewTestStoryboardRef(value GenerationContext) StoryboardPreviewRef {
	return StoryboardPreviewRef{ID: value.Storyboard.ID, Version: value.Storyboard.Version, ContentDigest: value.Storyboard.ContentDigest}
}
