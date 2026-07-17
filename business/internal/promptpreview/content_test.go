package promptpreview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
	"github.com/google/uuid"
)

// promptPreviewDigestFixture 是跨 Module Prompt Preview 保存摘要的固定测试向量。
type promptPreviewDigestFixture struct {
	// SchemaVersion 是共享 corpus 自身版本。
	SchemaVersion string `json:"schema_version"`
	// Canonical 是按 SaveDigest 字段顺序冻结的完整请求语义。
	Canonical struct {
		SchemaVersion            string               `json:"schema_version"`
		UserID                   string               `json:"user_id"`
		ProjectID                string               `json:"project_id"`
		ExpectedProjectVersion   int64                `json:"expected_project_version"`
		StoryboardPreviewRef     StoryboardPreviewRef `json:"storyboard_preview_ref"`
		ToolCallID               string               `json:"tool_call_id"`
		PromptVersion            string               `json:"prompt_version"`
		ValidatorVersion         string               `json:"validator_version"`
		ExactSetValidatorVersion string               `json:"exact_set_validator_version"`
		ExactTargetSetDigest     string               `json:"exact_target_set_digest"`
		Content                  Content              `json:"content"`
	} `json:"canonical"`
	// CanonicalJSON 是默认 HTML escape 与无多余空白的精确 JSON 字节文本。
	CanonicalJSON string `json:"canonical_json"`
	// ExpectedSHA256 是 CanonicalJSON 的小写 SHA-256。
	ExpectedSHA256 string `json:"expected_sha256"`
}

// TestValidateContentMatchesAgentTextAndTargetContract 验证正文边界与 Agent M1 的 Unicode 规则保持一致。
func TestValidateContentMatchesAgentTextAndTargetContract(t *testing.T) {
	_, content := validPromptPreviewFixture(t)
	if err := ValidateContent(content); err != nil {
		t.Fatalf("valid content rejected: %v", err)
	}

	withReplacement := cloneContent(content)
	withReplacement.Prompts[0].PositivePrompt = "保留合法替换字符 �"
	if err := ValidateContent(withReplacement); err != nil {
		t.Fatalf("legal U+FFFD rejected: %v", err)
	}
	for _, separator := range []string{"\u2028", "\u2029"} {
		candidate := cloneContent(content)
		candidate.Prompts[0].PositivePrompt = "禁止" + separator + "分隔符"
		if err := ValidateContent(candidate); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("separator %q error=%v, want ErrInvalidInput", separator, err)
		}
	}

	duplicate := cloneContent(content)
	duplicate.Prompts[0].NegativeConstraints = []string{"无水印", "无水印"}
	if err := ValidateContent(duplicate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate negative constraints error=%v", err)
	}
}

// TestValidateContentAgainstStoryboardRequiresFullSortedSlotSet 验证 Business 重新派生全部 Slot 且按 Element/Slot 顺序 exact-match。
func TestValidateContentAgainstStoryboardRequiresFullSortedSlotSet(t *testing.T) {
	source, content := validPromptPreviewFixture(t)
	if err := ValidateContentAgainstStoryboard(content, source); err != nil {
		t.Fatalf("valid exact target set rejected: %v", err)
	}

	missing := cloneContent(content)
	missing.Prompts = missing.Prompts[:1]
	if err := ValidateContentAgainstStoryboard(missing, source); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing target error=%v", err)
	}
	reordered := cloneContent(content)
	reordered.Prompts[0], reordered.Prompts[1] = reordered.Prompts[1], reordered.Prompts[0]
	if err := ValidateContentAgainstStoryboard(reordered, source); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("reordered target error=%v", err)
	}
	tampered := cloneContent(content)
	tampered.Prompts[0].MediaKind = "video"
	if err := ValidateContentAgainstStoryboard(tampered, source); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("tampered trusted field error=%v", err)
	}
}

// TestSaveRequestDigestIsStable 消费共享 corpus，验证 Business 摘要与 Agent 固定字段顺序完全一致。
func TestSaveRequestDigestIsStable(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Prompt Preview digest fixture path")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "docs", "design", "cross-module", "testdata", "prompt_preview_save_digest_v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Prompt Preview digest fixture: %v", err)
	}
	var fixture promptPreviewDigestFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode Prompt Preview digest fixture: %v", err)
	}
	if fixture.SchemaVersion != "prompt.preview.save-draft.digest.corpus.v1" ||
		fixture.Canonical.SchemaVersion != SaveDigestSchemaVersion {
		t.Fatalf("unexpected fixture schema: corpus=%q canonical=%q", fixture.SchemaVersion, fixture.Canonical.SchemaVersion)
	}
	canonicalJSON, err := json.Marshal(fixture.Canonical)
	if err != nil || string(canonicalJSON) != fixture.CanonicalJSON {
		t.Fatalf("fixture canonical JSON drift: encoded=%q error=%v", string(canonicalJSON), err)
	}
	exactDigest, err := ParseDigest(fixture.Canonical.ExactTargetSetDigest)
	if err != nil {
		t.Fatalf("ParseDigest() error = %v", err)
	}
	digest, err := SaveRequestDigest(
		fixture.Canonical.UserID, fixture.Canonical.ProjectID, fixture.Canonical.ExpectedProjectVersion,
		fixture.Canonical.StoryboardPreviewRef, fixture.Canonical.ToolCallID,
		fixture.Canonical.PromptVersion, fixture.Canonical.ValidatorVersion,
		fixture.Canonical.ExactSetValidatorVersion, exactDigest, fixture.Canonical.Content,
	)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	if digest.Hex() != fixture.ExpectedSHA256 {
		t.Fatalf("SaveRequestDigest() = %q, want %q", digest.Hex(), fixture.ExpectedSHA256)
	}
}

// TestServiceSaveAndUnknownOutcomeQuery 验证 Service 在 Repository 前复核摘要，并按原命令查询权威结果。
func TestServiceSaveAndUnknownOutcomeQuery(t *testing.T) {
	source, content := validPromptPreviewFixture(t)
	userID := source.UserID
	projectID := source.ProjectID
	commandID := newPromptPreviewTestUUIDv7(t)
	toolCallID := newPromptPreviewTestUUIDv7(t)
	exactDigest := Digest{9, 8, 7}
	requestDigest, err := SaveRequestDigest(
		userID, projectID, 3, content.SourceStoryboardPreviewRef, toolCallID,
		"graph_tool.write_prompts.preview.v1", "write_prompts.preview.validator.v1",
		"write_prompts.preview.exact-set-validator.v1", exactDigest, content,
	)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	repository := &promptPreviewServiceRepositoryFake{}
	clock := promptPreviewClockFake{value: time.Date(2026, 7, 17, 3, 4, 5, 0, time.UTC)}
	ids := &promptPreviewIDsFake{values: []string{newPromptPreviewTestUUIDv7(t), newPromptPreviewTestUUIDv7(t)}}
	service, err := NewService(repository, clock, ids)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	command := SaveCommand{
		CommandID: commandID, RequestDigestHex: requestDigest.Hex(), UserID: userID, ProjectID: projectID,
		ExpectedProjectVersion: 3, StoryboardPreviewRef: content.SourceStoryboardPreviewRef,
		ToolCallID: toolCallID, PromptVersion: "graph_tool.write_prompts.preview.v1",
		ValidatorVersion:         "write_prompts.preview.validator.v1",
		ExactSetValidatorVersion: "write_prompts.preview.exact-set-validator.v1",
		ExactTargetSetDigestHex:  exactDigest.Hex(), Content: content,
	}
	result, err := service.SaveDraft(context.Background(), command)
	if err != nil || result.Disposition != CommandDispositionCreated || result.Draft.ID == "" {
		t.Fatalf("SaveDraft() result=%+v error=%v", result, err)
	}
	repository.queryResult = QueryResult{Status: QueryStatusCompleted, Draft: &result.Draft}
	query, err := service.QueryCommand(context.Background(), commandID, requestDigest.Hex(), userID, projectID)
	if err != nil || query.Status != QueryStatusCompleted || query.Draft == nil || query.Draft.ID != result.Draft.ID {
		t.Fatalf("QueryCommand() result=%+v error=%v", query, err)
	}
}

// TestServiceGetGenerationContextDistinguishesAuthorizedVersionDrift 验证已授权 Source 的摘要漂移返回版本冲突而非持久化细节。
func TestServiceGetGenerationContextDistinguishesAuthorizedVersionDrift(t *testing.T) {
	source, _ := validPromptPreviewFixture(t)
	repository := &promptPreviewServiceRepositoryFake{generationContext: GenerationContext{
		ProjectID: source.ProjectID, ProjectVersion: 3, ProjectTitle: "Prompt Preview 测试项目", Storyboard: source,
	}}
	service, err := NewService(
		repository, promptPreviewClockFake{value: time.Now().UTC()},
		&promptPreviewIDsFake{values: []string{newPromptPreviewTestUUIDv7(t), newPromptPreviewTestUUIDv7(t)}},
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	reference := StoryboardPreviewRef{ID: source.ID, Version: source.Version, ContentDigest: source.ContentDigest.Hex()}
	result, err := service.GetGenerationContext(context.Background(), ContextQuery{
		UserID: source.UserID, ProjectID: source.ProjectID, StoryboardPreviewRef: reference,
	})
	if err != nil || result.Storyboard.ID != source.ID {
		t.Fatalf("GetGenerationContext() result=%+v error=%v", result, err)
	}
	driftedDigest, err := ParseDigest(reference.ContentDigest)
	if err != nil {
		t.Fatalf("ParseDigest() error = %v", err)
	}
	driftedDigest[0] ^= 0xff
	reference.ContentDigest = driftedDigest.Hex()
	_, err = service.GetGenerationContext(context.Background(), ContextQuery{
		UserID: source.UserID, ProjectID: source.ProjectID, StoryboardPreviewRef: reference,
	})
	if !errors.Is(err, ErrStoryboardVersionConflict) {
		t.Fatalf("drifted Source error=%v, want ErrStoryboardVersionConflict", err)
	}
}

// promptPreviewServiceRepositoryFake 是 Service 单元测试使用的无网络 Repository。
type promptPreviewServiceRepositoryFake struct {
	generationContext GenerationContext
	queryResult       QueryResult
}

// FindGenerationContext 返回测试预设的 Owner 校验后联合快照。
func (fake *promptPreviewServiceRepositoryFake) FindGenerationContext(context.Context, ContextQuery) (GenerationContext, error) {
	return fake.generationContext, nil
}

// SaveDraft 返回调用方构造的合法 Draft，模拟首次原子创建。
func (fake *promptPreviewServiceRepositoryFake) SaveDraft(_ context.Context, aggregate SaveAggregate) (SaveResult, error) {
	return SaveResult{Disposition: CommandDispositionCreated, Draft: aggregate.Draft}, nil
}

// QueryCommand 返回测试预设的权威查询结果。
func (fake *promptPreviewServiceRepositoryFake) QueryCommand(context.Context, QueryCommand) (QueryResult, error) {
	return fake.queryResult, nil
}

// promptPreviewClockFake 为 Service 测试冻结 UTC 时间。
type promptPreviewClockFake struct {
	value time.Time
}

// Now 返回冻结时间。
func (fake promptPreviewClockFake) Now() time.Time { return fake.value }

// promptPreviewIDsFake 按顺序返回预分配 UUIDv7。
type promptPreviewIDsFake struct {
	values []string
}

// New 返回并消费下一个 UUIDv7；耗尽时返回稳定错误。
func (fake *promptPreviewIDsFake) New() (string, error) {
	if len(fake.values) == 0 {
		return "", errors.New("test ID sequence exhausted")
	}
	value := fake.values[0]
	fake.values = fake.values[1:]
	return value, nil
}

// validPromptPreviewFixture 返回包含跨 Element 排序的 Storyboard Source 与完整 Prompt Content。
func validPromptPreviewFixture(t *testing.T) (StoryboardSnapshot, Content) {
	t.Helper()
	storyboardContent := storyboardpreview.Content{
		Title: "夏日品牌短片故事板", Summary: "以产品亮相和使用场景串联两段叙事。",
		Sections: []storyboardpreview.Section{{Key: "section_1", Title: "开场", Objective: "建立夏日氛围"}},
		Elements: []storyboardpreview.Element{
			{Key: "element_1", SectionKey: "section_1", Order: 1, Type: storyboardpreview.ElementTypeScene, Title: "海边开场", NarrativePurpose: "建立情绪", DurationSeconds: 10, SourcePhaseKey: "phase_1", DependencyKeys: []string{}},
			{Key: "element_2", SectionKey: "section_1", Order: 2, Type: storyboardpreview.ElementTypeShot, Title: "产品特写", NarrativePurpose: "突出卖点", DurationSeconds: 20, SourcePhaseKey: "phase_1", DependencyKeys: []string{"element_1"}},
		},
		Slots: []storyboardpreview.Slot{
			{Key: "slot_1", ElementKey: "element_2", Type: storyboardpreview.SlotTypeVideo, Purpose: "产品动态特写", Required: true},
			{Key: "slot_2", ElementKey: "element_1", Type: storyboardpreview.SlotTypeImage, Purpose: "海边环境图", Required: false},
		},
	}
	storyboardDigest, err := storyboardpreview.ContentDigest(storyboardContent)
	if err != nil {
		t.Fatalf("storyboard ContentDigest() error = %v", err)
	}
	digest, err := DigestFromBytes(storyboardDigest.Bytes())
	if err != nil {
		t.Fatalf("DigestFromBytes() error = %v", err)
	}
	source := StoryboardSnapshot{
		ID: newPromptPreviewTestUUIDv7(t), ProjectID: newPromptPreviewTestUUIDv7(t),
		UserID: newPromptPreviewTestUUIDv7(t), Status: storyboardpreview.DraftStatus,
		Version: storyboardpreview.InitialDraftVersion, SchemaVersion: storyboardpreview.DraftSchemaVersion,
		Content: storyboardContent, ContentDigest: digest,
	}
	reference := StoryboardPreviewRef{ID: source.ID, Version: source.Version, ContentDigest: digest.Hex()}
	content := Content{
		SchemaVersion: DraftSchemaVersion, Mode: DraftMode, SourceStoryboardPreviewRef: reference,
		Prompts: []PromptEntry{
			{TargetLocalKey: "slot_2", ElementLocalKey: "element_1", SlotType: "image", MediaKind: "image", Purpose: "海边环境图", Required: false, PositivePrompt: "明亮夏日海边，品牌色调自然融入", NegativeConstraints: []string{}, OutputLanguage: "zh-CN"},
			{TargetLocalKey: "slot_1", ElementLocalKey: "element_2", SlotType: "video", MediaKind: "video", Purpose: "产品动态特写", Required: true, PositivePrompt: "产品包装动态特写，镜头平稳推进", NegativeConstraints: []string{"无水印"}, OutputLanguage: "zh-CN"},
		},
	}
	return source, content
}

// newPromptPreviewTestUUIDv7 生成测试专用 UUIDv7。
func newPromptPreviewTestUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	return id.String()
}
