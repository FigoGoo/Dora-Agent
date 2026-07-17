package creationspec

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

const (
	testUserID     = "019f68e8-0001-7000-8000-000000000001"
	testProjectID  = "019f68e8-0002-7000-8000-000000000002"
	testToolCallID = "019f68e8-0003-7000-8000-000000000003"
	testCommandID  = "019f68e8-0004-7000-8000-000000000004"
	testDraftID    = "019f68e8-0005-7000-8000-000000000005"
	testReceiptID  = "019f68e8-0006-7000-8000-000000000006"
	testPrompt     = "graph_tool.plan_creation_spec.preview.v1"
	testValidator  = "plan_creation_spec.preview.validator.v1"
	testSaveDigest = "e160eca05528f124570c84fd2d8c2a7e8c2871bf4c31980c5671f455940c8e05"
)

// TestSaveRequestDigestMatchesCrossModuleVector 镜像共享固定向量，防止 Agent/Business Canonical 字段顺序漂移。
func TestSaveRequestDigestMatchesCrossModuleVector(t *testing.T) {
	digest, err := SaveRequestDigest(testUserID, testProjectID, 1, testToolCallID, testPrompt, testValidator, testCreationSpecContent())
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	if digest.Hex() != testSaveDigest {
		t.Fatalf("save digest=%s, want %s", digest.Hex(), testSaveDigest)
	}
	canonical := struct {
		SchemaVersion          string  `json:"schema_version"`
		UserID                 string  `json:"user_id"`
		ProjectID              string  `json:"project_id"`
		ExpectedProjectVersion int64   `json:"expected_project_version"`
		ToolCallID             string  `json:"tool_call_id"`
		PromptVersion          string  `json:"prompt_version"`
		ValidatorVersion       string  `json:"validator_version"`
		Content                Content `json:"content"`
	}{SaveDigestSchemaVersion, testUserID, testProjectID, 1, testToolCallID, testPrompt, testValidator, testCreationSpecContent()}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"acceptance_criteria":["成片时长为 30 秒","画面中出现产品名称"]`)) {
		t.Fatalf("canonical content fields changed: %s", encoded)
	}
}

// TestContentValidationRejectsNonCanonicalBoundaries 覆盖非 NFC、重复列表、未知枚举与尾随 JSON。
func TestContentValidationRejectsNonCanonicalBoundaries(t *testing.T) {
	tests := []func(*Content){
		func(content *Content) { content.Title = " é" },
		func(content *Content) { content.DeliverableType = "document" },
		func(content *Content) { content.Phases[1].Key = content.Phases[0].Key },
		func(content *Content) { content.Constraints = []string{"重复", "重复"} },
		func(content *Content) { content.AcceptanceCriteria = nil },
	}
	for index, mutate := range tests {
		content := testCreationSpecContent()
		mutate(&content)
		if err := ValidateContent(content); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d error=%v", index, err)
		}
	}
	canonical, err := testCreationSpecContent().CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if _, err := ParseContentJSON(append(canonical, []byte(` {}`)...)); !errors.Is(err, ErrPersistence) {
		t.Fatalf("trailing JSON error=%v", err)
	}
}

// TestValidateAggregateBindsReceiptDigest 验证回执摘要必须覆盖 Draft 内容与全部保存语义。
func TestValidateAggregateBindsReceiptDigest(t *testing.T) {
	aggregate := testCreationSpecAggregate(t)
	if err := ValidateAggregate(aggregate); err != nil {
		t.Fatalf("ValidateAggregate() error = %v", err)
	}
	aggregate.Receipt.ExpectedProjectVersion++
	if err := ValidateAggregate(aggregate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("tampered expected version error=%v", err)
	}
}

func testCreationSpecContent() Content {
	return Content{
		Title: "夏日品牌短片", Goal: "为新品制作一支 30 秒品牌短片",
		DeliverableType: DeliverableTypeVideo, Audience: "年轻消费者", Locale: "zh-CN",
		Phases: []Phase{
			{Key: "phase_1", Title: "创意规划", Objective: "确定核心叙事与视觉方向", Output: "可执行创意方案"},
			{Key: "phase_2", Title: "制作交付", Objective: "完成素材制作与质量检查", Output: "30 秒竖屏成片"},
		},
		Constraints:        []string{"竖屏 9:16", "总时长 30 秒"},
		AcceptanceCriteria: []string{"成片时长为 30 秒", "画面中出现产品名称"},
	}
}

func testCreationSpecAggregate(t *testing.T) SaveAggregate {
	t.Helper()
	content := testCreationSpecContent()
	contentDigest, err := ContentDigest(content)
	if err != nil {
		t.Fatalf("ContentDigest() error = %v", err)
	}
	requestDigest, err := SaveRequestDigest(testUserID, testProjectID, 1, testToolCallID, testPrompt, testValidator, content)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	now := time.Date(2026, 7, 16, 1, 2, 3, 456000000, time.UTC)
	draft := Draft{
		ID: testDraftID, ProjectID: testProjectID, UserID: testUserID, Status: DraftStatus,
		Version: InitialDraftVersion, SchemaVersion: DraftSchemaVersion, Content: content, ContentDigest: contentDigest,
		SourceToolCallID: testToolCallID, SourcePromptVersion: testPrompt, SourceValidatorVersion: testValidator,
		CreatedAt: now, UpdatedAt: now,
	}
	return SaveAggregate{Draft: draft, Receipt: CommandReceipt{
		ID: testReceiptID, CommandID: testCommandID, RequestDigest: requestDigest,
		UserID: testUserID, ProjectID: testProjectID, ExpectedProjectVersion: 1,
		SourceToolCallID: testToolCallID, SourcePromptVersion: testPrompt, SourceValidatorVersion: testValidator,
		CreationSpecID: testDraftID, ResultVersion: InitialDraftVersion, ResultStatus: DraftStatus,
		ResultContentDigest: contentDigest, CreatedAt: now,
	}}
}
