package tools

import "strings"

const (
	renderEventDataModelUpdate = "a2ui.data_model_update"
	renderEventStoryboardPatch = "storyboard.patch"
)

type GeneratedAssetInfo struct {
	AssetID         string `json:"asset_id,omitempty"`
	Kind            string `json:"kind"`
	URL             string `json:"url,omitempty"`
	TargetType      string `json:"target_type,omitempty"`
	TargetID        string `json:"target_id,omitempty"`
	Field           string `json:"field,omitempty"`
	Status          string `json:"status"`
	MediaType       string `json:"media_type,omitempty"`
	StorageProvider string `json:"storage_provider,omitempty"`
	Bucket          string `json:"bucket,omitempty"`
	ObjectKey       string `json:"object_key,omitempty"`
}

type StoryboardUpdateHint struct {
	TargetType string   `json:"target_type,omitempty"`
	TargetID   string   `json:"target_id,omitempty"`
	Field      string   `json:"field,omitempty"`
	AssetKind  string   `json:"asset_kind,omitempty"`
	AssetIDs   []string `json:"asset_ids,omitempty"`
	Status     string   `json:"status"`
}

type RenderEventHint struct {
	Event        string `json:"event"`
	SurfaceID    string `json:"surface_id,omitempty"`
	DataModelKey string `json:"data_model_key,omitempty"`
	Payload      any    `json:"payload,omitempty"`
}

func generativeArtifactIDs(assets []GeneratedAssetInfo) []string {
	out := make([]string, 0, len(assets))
	for _, item := range assets {
		if strings.TrimSpace(item.AssetID) != "" {
			out = append(out, strings.TrimSpace(item.AssetID))
		}
	}
	return out
}

func generativeStoryboardUpdates(assets []GeneratedAssetInfo) []StoryboardUpdateHint {
	byTarget := map[string]int{}
	out := make([]StoryboardUpdateHint, 0, len(assets))
	for _, item := range assets {
		if strings.TrimSpace(item.TargetType) == "" || strings.TrimSpace(item.TargetID) == "" {
			continue
		}
		key := strings.Join([]string{
			strings.TrimSpace(item.TargetType),
			strings.TrimSpace(item.TargetID),
			strings.TrimSpace(item.Field),
			strings.TrimSpace(item.Kind),
		}, "\x00")
		if idx, ok := byTarget[key]; ok {
			if strings.TrimSpace(item.AssetID) != "" {
				out[idx].AssetIDs = append(out[idx].AssetIDs, strings.TrimSpace(item.AssetID))
			}
			continue
		}
		update := StoryboardUpdateHint{
			TargetType: strings.TrimSpace(item.TargetType),
			TargetID:   strings.TrimSpace(item.TargetID),
			Field:      strings.TrimSpace(item.Field),
			AssetKind:  strings.TrimSpace(item.Kind),
			Status:     item.Status,
		}
		if strings.TrimSpace(item.AssetID) != "" {
			update.AssetIDs = []string{strings.TrimSpace(item.AssetID)}
		}
		byTarget[key] = len(out)
		out = append(out, update)
	}
	return out
}

func generativeRenderEvents(assets []GeneratedAssetInfo, updates []StoryboardUpdateHint) []RenderEventHint {
	payload := map[string]any{
		"assets":             assets,
		"storyboard_updates": updates,
	}
	return []RenderEventHint{
		{
			Event:        renderEventDataModelUpdate,
			SurfaceID:    "storyboard",
			DataModelKey: "assets",
			Payload:      payload,
		},
		{
			Event:        renderEventStoryboardPatch,
			SurfaceID:    "storyboard",
			DataModelKey: "storyboard",
			Payload: map[string]any{
				"updates": updates,
			},
		},
	}
}

func generativeAssetField(kind string, targetType string) string {
	kind = strings.TrimSpace(kind)
	targetType = strings.TrimSpace(targetType)
	switch {
	case kind == "video":
		return "video_asset_id"
	case kind == "audio":
		return "asset_id"
	case kind == "image" && targetType == "shot":
		return "keyframe_asset_id"
	case kind == "image" && targetType == "key_element":
		return "asset_ids"
	default:
		return ""
	}
}
