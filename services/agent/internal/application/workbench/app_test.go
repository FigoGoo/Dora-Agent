package workbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/modeltool"
)

func TestEstimateItemsForArtifactsDoesNotReuseLineItem(t *testing.T) {
	estimate := CreditEstimateDTO{LineItems: []CreditEstimateLineItemDTO{
		{EstimateItemID: "est_img_1", ItemType: "model_generation", ResourceType: "image"},
		{EstimateItemID: "est_img_2", ItemType: "model_generation", ResourceType: "image"},
	}}
	items, err := estimateItemsForArtifacts(estimate, []modeltool.Artifact{
		{ArtifactID: "art_1", ResourceType: "image"},
		{ArtifactID: "art_2", ResourceType: "image"},
	})
	if err != nil {
		t.Fatalf("map estimate items: %v", err)
	}
	if items["art_1"] == items["art_2"] {
		t.Fatalf("estimate item reused for multiple artifacts: %#v", items)
	}
}

func TestEstimateItemsForArtifactsFailsWhenQuantityMissing(t *testing.T) {
	_, err := estimateItemsForArtifacts(CreditEstimateDTO{LineItems: []CreditEstimateLineItemDTO{
		{EstimateItemID: "est_img_1", ItemType: "model_generation", ResourceType: "image"},
	}}, []modeltool.Artifact{
		{ArtifactID: "art_1", ResourceType: "image"},
		{ArtifactID: "art_2", ResourceType: "image"},
	})
	if err == nil {
		t.Fatal("expected error when generated artifacts exceed estimate line items")
	}
}

func TestOutputElementPlanKeepsDraftAndFinalDeclarations(t *testing.T) {
	plan := buildOutputElementPlan([]SkillOutputElementDTO{
		{ElementType: "image_ref", ElementName: "草稿", UseDraft: true, DisplayOrder: 1},
		{ElementType: "image_ref", ElementName: "最终", UseFinal: true, DisplayOrder: 7},
	}, []modeltool.Artifact{{ArtifactID: "art_1", ElementType: "image_ref"}})
	if !plan.UseDraft("image_ref") || plan.DraftElement("image_ref").ElementName != "草稿" {
		t.Fatalf("draft declaration lost: %#v", plan.DraftElement("image_ref"))
	}
	finals := plan.FinalElementsForArtifact(modeltool.Artifact{ArtifactID: "art_1", ElementType: "image_ref"})
	if len(finals) != 1 || finals[0].ElementName != "最终" || finals[0].DisplayOrder != 7 {
		t.Fatalf("final declaration lost: %#v", finals)
	}
}

func TestSkillMissingFallbackDoesNotRecommendSkillCreation(t *testing.T) {
	payload := skillMissingPayload("", "")
	if payload["fallback_mode"] != "text_model" || payload["recommend_create_skill"] != false {
		t.Fatalf("fallback payload must be text-only and not recommend creating Skill: %#v", payload)
	}
	if payload["reason"] != "no_published_skill" {
		t.Fatalf("fallback reason not defaulted: %#v", payload)
	}
}

func TestToolPolicyRiskContextRequiresPerToolWhitelistCheck(t *testing.T) {
	ctx := toolPolicyRiskContext("web_fetch:browser")
	if ctx["runtime_whitelist_check"] != "required_per_tool" || ctx["tool_ref"] != "web_fetch:browser" {
		t.Fatalf("tool policy runtime recheck context not explicit: %#v", ctx)
	}
}

func TestSkillCapabilityQuestionDetection(t *testing.T) {
	for _, prompt := range []string{"你好", "你好，你有什么能力", "你能做什么？", "what can you do", "help"} {
		if !isSkillCapabilityQuestion(prompt) {
			t.Fatalf("expected capability question: %q", prompt)
		}
	}
	for _, prompt := range []string{"你好，帮我生成一张海报", "请生成 3 张故事板", "lookup with web fetch", "help me generate a poster"} {
		if isSkillCapabilityQuestion(prompt) {
			t.Fatalf("unexpected capability question match: %q", prompt)
		}
	}
}

func TestRouteSkillUsesSelectedPublishedSkillBeforePromptRoute(t *testing.T) {
	app := New(nil, nil, "test")
	route, selectedUnavailable := app.routeSkill("lookup with web fetch", "sk_selected", []SkillSummaryDTO{
		{SkillID: "sk_prompt", SkillName: "Prompt Skill", Version: "1.0.0", Status: "published", RouteHints: map[string]string{"intent": "lookup"}},
		{SkillID: "sk_selected", SkillName: "Selected Skill", Version: "2.0.0", Status: "published", RouteHints: map[string]string{"intent": "manual only"}},
	})
	if selectedUnavailable || !route.Matched || route.Skill.SkillID != "sk_selected" || route.Reason != "selected_skill_id" {
		t.Fatalf("selected Skill should win before prompt route: route=%#v unavailable=%v", route, selectedUnavailable)
	}
}

func TestRouteSkillFallsBackToPromptRouteWhenSelectedSkillUnavailable(t *testing.T) {
	app := New(nil, nil, "test")
	route, selectedUnavailable := app.routeSkill("lookup with web fetch", "sk_missing", []SkillSummaryDTO{
		{SkillID: "sk_prompt", SkillName: "Prompt Skill", Version: "1.0.0", Status: "published", RouteHints: map[string]string{"intent": "lookup"}},
	})
	if !selectedUnavailable || !route.Matched || route.Skill.SkillID != "sk_prompt" || route.Reason != "route_hint:intent" {
		t.Fatalf("missing selected Skill should fall back to prompt route: route=%#v unavailable=%v", route, selectedUnavailable)
	}
}

func TestStreamingArtifactUploaderConsumesStream(t *testing.T) {
	body := []byte("streamed artifact")
	sum := sha256.Sum256(body)
	checksum := "sha256:" + fmt.Sprintf("%x", sum[:])
	artifact := modeltool.Artifact{
		ArtifactID: "art_stream", ResourceType: "image", ContentType: "image/png",
		SizeBytes: int64(len(body)), Checksum: checksum,
		OpenStream: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		},
	}
	uploaded, err := NewStreamingArtifactUploader(nil).Upload(context.Background(), GeneratedUploadSlotDTO{
		ArtifactID: "art_stream", Bucket: "dora-local", ObjectKey: "local/spaces/sp/projects/prj/runs/run/artifacts/art_stream.png",
		UploadURL: "memory://tos/local/spaces/sp/projects/prj/runs/run/artifacts/art_stream.png",
	}, artifact)
	if err != nil {
		t.Fatalf("stream upload: %v", err)
	}
	if uploaded.Checksum != checksum || uploaded.Etag == "" || uploaded.Etag[:9] != "uploaded-" {
		t.Fatalf("unexpected uploaded object: %#v", uploaded)
	}
}
