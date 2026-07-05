package storyboard

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
)

func TestAssetBindingOpsForKeyElement(t *testing.T) {
	ops, err := AssetBindingOps(Storyboard{
		KeyElements: []KeyElement{{Key: "suji", Name: "苏寂"}},
	}, AssetBindingRequest{
		AssetID:    "asset-1",
		AssetKind:  asset.KindImage,
		TargetType: "key_element",
		TargetID:   "suji",
	})
	if err != nil {
		t.Fatalf("AssetBindingOps() error = %v", err)
	}
	if len(ops) != 2 || ops[0].Path != "/key_elements/0/asset_ids" || ops[1].Path != "/key_elements/0/status" {
		t.Fatalf("ops = %#v", ops)
	}
}

func TestAssetBindingOpsForShotVideo(t *testing.T) {
	ops, err := AssetBindingOps(Storyboard{
		Shots: []Shot{{ShotID: "shot-1"}},
	}, AssetBindingRequest{
		AssetID:    "asset-video",
		AssetKind:  asset.KindVideo,
		TargetType: "shot",
		TargetID:   "shot-1",
	})
	if err != nil {
		t.Fatalf("AssetBindingOps() error = %v", err)
	}
	if len(ops) != 2 || ops[0].Path != "/shots/0/video_asset_id" || ops[1].Path != "/shots/0/status" {
		t.Fatalf("ops = %#v", ops)
	}
}
