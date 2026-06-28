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
