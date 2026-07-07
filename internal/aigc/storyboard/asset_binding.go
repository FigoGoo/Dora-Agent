package storyboard

import (
	"errors"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/patch"
)

// ErrAssetAlreadyBound signals that the asset is already bound to the target, so
// the binding is a no-op. Callers (e.g. the generation worker's conflict-retry
// loop) treat it as success rather than a failure.
var ErrAssetAlreadyBound = errors.New("asset already bound to target")

type AssetBindingRequest struct {
	AssetID    string
	AssetKind  string
	TargetType string
	TargetID   string
	Field      string
}

func AssetBindingOps(board Storyboard, req AssetBindingRequest) ([]patch.JSONPatchOp, error) {
	req.AssetID = strings.TrimSpace(req.AssetID)
	req.AssetKind = asset.NormalizeKind(req.AssetKind)
	req.TargetType = strings.TrimSpace(req.TargetType)
	req.TargetID = strings.TrimSpace(req.TargetID)
	req.Field = strings.TrimSpace(req.Field)
	if req.AssetID == "" {
		return nil, fmt.Errorf("asset id is required")
	}
	if req.TargetType == "" {
		return nil, fmt.Errorf("target type is required")
	}
	if req.TargetID == "" {
		return nil, fmt.Errorf("target id is required")
	}
	switch req.TargetType {
	case "key_element":
		return keyElementBindingOps(board, req)
	case "shot":
		return shotBindingOps(board, req)
	case "audio_layer":
		return audioLayerBindingOps(board, req)
	default:
		return nil, fmt.Errorf("unsupported target type")
	}
}

func keyElementBindingOps(board Storyboard, req AssetBindingRequest) ([]patch.JSONPatchOp, error) {
	for i, element := range board.KeyElements {
		if element.Key != req.TargetID {
			continue
		}
		for _, existing := range element.AssetIDs {
			if existing == req.AssetID {
				return nil, ErrAssetAlreadyBound
			}
		}
		if len(element.AssetIDs) == 0 {
			return appendReadyStatus([]patch.JSONPatchOp{{
				Op:    "add",
				Path:  fmt.Sprintf("/key_elements/%d/asset_ids", i),
				Value: []string{req.AssetID},
			}}, fmt.Sprintf("/key_elements/%d/status", i)), nil
		}
		return appendReadyStatus([]patch.JSONPatchOp{{
			Op:    "add",
			Path:  fmt.Sprintf("/key_elements/%d/asset_ids/%d", i, len(element.AssetIDs)),
			Value: req.AssetID,
		}}, fmt.Sprintf("/key_elements/%d/status", i)), nil
	}
	return nil, fmt.Errorf("key element not found")
}

func shotBindingOps(board Storyboard, req AssetBindingRequest) ([]patch.JSONPatchOp, error) {
	field := shotAssetField(req.AssetKind, req.Field)
	if field == "" {
		return nil, fmt.Errorf("unsupported shot asset field")
	}
	for i, shot := range board.Shots {
		if shot.ShotID != req.TargetID {
			continue
		}
		if (field == "video_asset_id" && shot.VideoAssetID == req.AssetID) ||
			(field == "keyframe_asset_id" && shot.KeyframeAssetID == req.AssetID) {
			return nil, ErrAssetAlreadyBound
		}
		return appendReadyStatus([]patch.JSONPatchOp{{
			Op:    "add",
			Path:  fmt.Sprintf("/shots/%d/%s", i, field),
			Value: req.AssetID,
		}}, fmt.Sprintf("/shots/%d/status", i)), nil
	}
	return nil, fmt.Errorf("shot not found")
}

func audioLayerBindingOps(board Storyboard, req AssetBindingRequest) ([]patch.JSONPatchOp, error) {
	for i, layer := range board.AudioLayers {
		if layer.LayerID != req.TargetID {
			continue
		}
		if layer.AssetID == req.AssetID {
			return nil, ErrAssetAlreadyBound
		}
		return appendReadyStatus([]patch.JSONPatchOp{{
			Op:    "add",
			Path:  fmt.Sprintf("/audio_layers/%d/asset_id", i),
			Value: req.AssetID,
		}}, fmt.Sprintf("/audio_layers/%d/status", i)), nil
	}
	return nil, fmt.Errorf("audio layer not found")
}

func appendReadyStatus(ops []patch.JSONPatchOp, path string) []patch.JSONPatchOp {
	return append(ops, patch.JSONPatchOp{
		Op:    "replace",
		Path:  path,
		Value: StatusReady,
	})
}

func shotAssetField(assetKind string, requested string) string {
	switch strings.TrimSpace(requested) {
	case "keyframe_asset_id", "video_asset_id":
		return requested
	case "":
		if assetKind == asset.KindVideo {
			return "video_asset_id"
		}
		return "keyframe_asset_id"
	default:
		return ""
	}
}
