package postgres

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/storyboardpreview"
)

// TestStoryboardPreviewPersistenceMappingRoundTrip 验证 Draft/Receipt 的 JSON、摘要、依赖引用与首次结果字段无损往返。
func TestStoryboardPreviewPersistenceMappingRoundTrip(t *testing.T) {
	creationSpecDigest, err := storyboardpreview.ParseDigest("1111111111111111111111111111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("ParseDigest() error = %v", err)
	}
	content := storyboardpreview.Content{
		Title: "测试故事板", Summary: "使用局部键保存严格 Preview JSON",
		Sections: []storyboardpreview.Section{{Key: "section_1", Title: "开场", Objective: "建立背景"}},
		Elements: []storyboardpreview.Element{{
			Key: "element_1", SectionKey: "section_1", Order: 1, Type: storyboardpreview.ElementTypeScene,
			Title: "开场画面", NarrativePurpose: "建立叙事", DurationSeconds: 10,
			SourcePhaseKey: "phase_1", DependencyKeys: []string{},
		}},
		Slots: []storyboardpreview.Slot{{
			Key: "slot_1", ElementKey: "element_1", Type: storyboardpreview.SlotTypeVideo,
			Purpose: "开场环境画面", Required: true,
		}},
	}
	contentDigest, err := storyboardpreview.ContentDigest(content)
	if err != nil {
		t.Fatalf("ContentDigest() error = %v", err)
	}
	reference := storyboardpreview.CreationSpecRef{
		ID: "019f68e8-0010-7000-8000-000000000010", Version: 1, ContentDigest: creationSpecDigest,
	}
	requestDigest, err := storyboardpreview.SaveRequestDigest(
		"019f68e8-0001-7000-8000-000000000001", "019f68e8-0002-7000-8000-000000000002", 1,
		reference, "019f68e8-0003-7000-8000-000000000003", "prompt.v1", "validator.v1", content,
	)
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	aggregate := storyboardpreview.SaveAggregate{
		Draft: storyboardpreview.Draft{
			ID: "019f68e8-0004-7000-8000-000000000004", ProjectID: "019f68e8-0002-7000-8000-000000000002",
			UserID: "019f68e8-0001-7000-8000-000000000001", CreationSpecRef: reference,
			Status: storyboardpreview.DraftStatus, Version: 1, SchemaVersion: storyboardpreview.DraftSchemaVersion,
			Content: content, ContentDigest: contentDigest, SourceToolCallID: "019f68e8-0003-7000-8000-000000000003",
			SourcePromptVersion: "prompt.v1", SourceValidatorVersion: "validator.v1", CreatedAt: now, UpdatedAt: now,
		},
		Receipt: storyboardpreview.CommandReceipt{
			ID: "019f68e8-0005-7000-8000-000000000005", CommandID: "019f68e8-0006-7000-8000-000000000006",
			RequestDigest: requestDigest, UserID: "019f68e8-0001-7000-8000-000000000001",
			ProjectID: "019f68e8-0002-7000-8000-000000000002", ExpectedProjectVersion: 1,
			CreationSpecRef: reference, SourceToolCallID: "019f68e8-0003-7000-8000-000000000003",
			SourcePromptVersion: "prompt.v1", SourceValidatorVersion: "validator.v1",
			StoryboardPreviewID: "019f68e8-0004-7000-8000-000000000004", ResultVersion: 1,
			ResultStatus: storyboardpreview.DraftStatus, ResultContentDigest: contentDigest, CreatedAt: now,
		},
	}
	draftModel, receiptModel, err := storyboardPreviewModelsFromAggregate(aggregate)
	if err != nil {
		t.Fatalf("storyboardPreviewModelsFromAggregate() error = %v", err)
	}
	draft, err := storyboardPreviewDraftEntity(draftModel)
	if err != nil {
		t.Fatalf("storyboardPreviewDraftEntity() error = %v", err)
	}
	receipt, err := storyboardPreviewReceiptEntity(receiptModel)
	if err != nil {
		t.Fatalf("storyboardPreviewReceiptEntity() error = %v", err)
	}
	if err := storyboardpreview.ValidateAggregate(storyboardpreview.SaveAggregate{Draft: draft, Receipt: receipt}); err != nil {
		t.Fatalf("round-trip aggregate is invalid: %v", err)
	}
	if draft.Content.Elements[0].Key != "element_1" || draft.Content.Slots[0].ElementKey != "element_1" {
		t.Fatalf("local-key references changed during round-trip: %+v", draft.Content)
	}
}
