package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

const (
	promptStatusReady        = "prompt_ready"
	promptMaxReturnedExcerpt = 24
	renderEventSurfaceUpdate = "a2ui.surface_update"
)

type PromptSpecStore interface {
	Get(ctx context.Context, specID string) (spec.FinalVideoSpec, error)
	GetLatestBySession(ctx context.Context, sessionID string) (spec.FinalVideoSpec, error)
}

type PromptStoryboardStore interface {
	Get(ctx context.Context, storyboardID string) (storyboard.Storyboard, error)
	GetLatestBySession(ctx context.Context, sessionID string) (storyboard.Storyboard, error)
	ApplyPatch(ctx context.Context, req storyboard.PatchRequest) (storyboard.Storyboard, storyboard.EventRecord, error)
}

type WritePromptToolConfig struct {
	Model       einomodel.BaseChatModel
	Specs       PromptSpecStore
	Storyboards PromptStoryboardStore
	NewEventID  func() string
}

type WritePromptTool struct {
	cfg WritePromptToolConfig
}

type WritePromptPayload struct {
	SessionID      string   `json:"session_id,omitempty"`
	SpecID         string   `json:"spec_id,omitempty"`
	StoryboardID   string   `json:"storyboard_id,omitempty"`
	TargetType     string   `json:"target_type,omitempty"`
	TargetID       string   `json:"target_id,omitempty"`
	TargetIDs      []string `json:"target_ids,omitempty"`
	PromptPurpose  string   `json:"prompt_purpose,omitempty"`
	Prompt         string   `json:"prompt,omitempty"`
	ExtraDirection string   `json:"extra_direction,omitempty"`
}

type WritePromptResult struct {
	SessionID         string               `json:"session_id"`
	SpecID            string               `json:"spec_id,omitempty"`
	StoryboardID      string               `json:"storyboard_id"`
	PromptPurpose     string               `json:"prompt_purpose,omitempty"`
	UpdatedTargets    []PromptTargetResult `json:"updated_targets"`
	RenderEvents      []RenderEventHint    `json:"render_events,omitempty"`
	Summary           string               `json:"summary"`
	StoryboardVersion int                  `json:"storyboard_version"`
	Metadata          map[string]any       `json:"metadata,omitempty"`
}

type PromptTargetResult struct {
	TargetType    string `json:"target_type"`
	TargetID      string `json:"target_id"`
	PromptPath    string `json:"prompt_path"`
	StatusPath    string `json:"status_path"`
	PromptExcerpt string `json:"prompt_excerpt,omitempty"`
}

type promptTarget struct {
	Type       string         `json:"target_type"`
	ID         string         `json:"target_id"`
	PromptPath string         `json:"prompt_path"`
	StatusPath string         `json:"status_path"`
	Context    map[string]any `json:"context"`
}

type promptModelResponse struct {
	Prompts []promptModelItem `json:"prompts"`
}

type promptModelItem struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Prompt     string `json:"prompt"`
}

func NewWritePromptTool(cfg WritePromptToolConfig) WritePromptTool {
	return WritePromptTool{cfg: cfg}
}

func (WritePromptTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: WritePromptToolKey,
		Desc: "Use DeepSeek to write compact generation prompts for storyboard key elements, shots, or audio layers. When stores are configured it reads Final Video Spec and storyboard context, writes prompts back through storyboard patches, and returns only lightweight business/render hints.",
		ParamsOneOf: schema.NewParamsOneOfByParams(commonPipelineParams(map[string]*schema.ParameterInfo{
			"storyboard_id": {
				Type: schema.String,
				Desc: "Storyboard id. Defaults to latest storyboard for the current session.",
			},
			"spec_id": {
				Type: schema.String,
				Desc: "Final Video Spec id. Defaults to storyboard spec_id or latest spec for the session.",
			},
			"target_type": {
				Type: schema.String,
				Desc: "Target type: key_element, shot, audio_layer, or all.",
				Enum: []string{"key_element", "shot", "audio_layer", "all"},
			},
			"target_id": {
				Type: schema.String,
				Desc: "Single target id.",
			},
			"target_ids": {
				Type: schema.Array,
				Desc: "Target ids to write prompts for. Empty means all targets of target_type.",
			},
			"prompt_purpose": {
				Type: schema.String,
				Desc: "Prompt purpose, such as element_image, shot_keyframe, shot_video, audio_layer, or storyboard_review.",
			},
			"extra_direction": {
				Type: schema.String,
				Desc: "Additional user direction for the prompt rewrite.",
			},
		})),
	}, nil
}

func (t WritePromptTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t.cfg.Model == nil || t.cfg.Storyboards == nil {
		return pipelineToolResult(WritePromptToolKey, "prompt_ready", argumentsInJSON)
	}

	invocation, err := decodeWritePromptInvocation(argumentsInJSON)
	if err != nil {
		return "", err
	}
	sessionID := strings.TrimSpace(firstNonEmpty(invocation.SessionID, invocation.Payload.SessionID))
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	board, err := loadPromptStoryboard(ctx, t.cfg.Storyboards, sessionID, invocation.Payload.StoryboardID)
	if err != nil {
		return "", err
	}
	if invocation.ExpectedStoryboardVersion > 0 && board.Version != invocation.ExpectedStoryboardVersion {
		return "", fmt.Errorf("storyboard version mismatch: current=%d expected=%d", board.Version, invocation.ExpectedStoryboardVersion)
	}

	finalSpec := loadPromptSpec(ctx, t.cfg.Specs, sessionID, firstNonEmpty(invocation.Payload.SpecID, board.SpecID))
	if invocation.ExpectedSpecVersion > 0 && finalSpec.Version > 0 && finalSpec.Version != invocation.ExpectedSpecVersion {
		return "", fmt.Errorf("spec version mismatch: current=%d expected=%d", finalSpec.Version, invocation.ExpectedSpecVersion)
	}

	targets, err := selectPromptTargets(board, invocation.Payload)
	if err != nil {
		return "", err
	}
	prompts, err := t.generatePrompts(ctx, finalSpec, board, invocation.Payload, targets)
	if err != nil {
		return "", err
	}
	ops, updatedTargets, err := promptPatchOps(targets, prompts)
	if err != nil {
		return "", err
	}

	eventID := ""
	if t.cfg.NewEventID != nil {
		eventID = strings.TrimSpace(t.cfg.NewEventID())
	}
	patched, event, err := t.cfg.Storyboards.ApplyPatch(ctx, storyboard.PatchRequest{
		EventID:      eventID,
		SessionID:    sessionID,
		StoryboardID: board.ID,
		BaseVersion:  board.Version,
		Source:       WritePromptToolKey,
		Ops:          ops,
	})
	if err != nil {
		return "", err
	}
	renderEvents := promptRenderEvents(event, updatedTargets)

	out, err := json.Marshal(ToolResultEnvelope[WritePromptResult]{
		Status:            ToolStatusOK,
		RequestID:         invocation.RequestID,
		IdempotencyKey:    invocation.IdempotencyKey,
		SpecVersion:       finalSpec.Version,
		StoryboardVersion: patched.Version,
		PatchEventIDs:     []string{event.ID},
		Data: WritePromptResult{
			SessionID:         sessionID,
			SpecID:            firstNonEmpty(finalSpec.ID, board.SpecID),
			StoryboardID:      board.ID,
			PromptPurpose:     strings.TrimSpace(invocation.Payload.PromptPurpose),
			UpdatedTargets:    updatedTargets,
			RenderEvents:      renderEvents,
			Summary:           fmt.Sprintf("prepared %d generation prompt(s)", len(updatedTargets)),
			StoryboardVersion: patched.Version,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal write prompt result: %w", err)
	}
	return string(out), nil
}

func decodeWritePromptInvocation(argumentsInJSON string) (ToolInvocationEnvelope[WritePromptPayload], error) {
	var enveloped ToolInvocationEnvelope[WritePromptPayload]
	if err := json.Unmarshal([]byte(argumentsInJSON), &enveloped); err == nil && (enveloped.Payload.StoryboardID != "" || enveloped.Payload.TargetType != "" || enveloped.Payload.TargetID != "" || len(enveloped.Payload.TargetIDs) > 0 || enveloped.Payload.Prompt != "") {
		return enveloped, nil
	}

	var direct WritePromptPayload
	if err := json.Unmarshal([]byte(argumentsInJSON), &direct); err != nil {
		return ToolInvocationEnvelope[WritePromptPayload]{}, fmt.Errorf("decode write prompt input: %w", err)
	}
	if direct.TargetID != "" && len(direct.TargetIDs) == 0 {
		direct.TargetIDs = []string{direct.TargetID}
	}
	return ToolInvocationEnvelope[WritePromptPayload]{
		SessionID:      direct.SessionID,
		RequestID:      "direct",
		IdempotencyKey: firstNonEmpty(direct.StoryboardID, direct.TargetID),
		Action:         "write_prompts",
		Payload:        direct,
	}, nil
}

func loadPromptStoryboard(ctx context.Context, store PromptStoryboardStore, sessionID string, storyboardID string) (storyboard.Storyboard, error) {
	storyboardID = strings.TrimSpace(storyboardID)
	if storyboardID != "" {
		return store.Get(ctx, storyboardID)
	}
	return store.GetLatestBySession(ctx, sessionID)
}

func loadPromptSpec(ctx context.Context, store PromptSpecStore, sessionID string, specID string) spec.FinalVideoSpec {
	if store == nil {
		return spec.FinalVideoSpec{}
	}
	specID = strings.TrimSpace(specID)
	if specID != "" {
		if got, err := store.Get(ctx, specID); err == nil {
			return got
		}
	}
	if got, err := store.GetLatestBySession(ctx, sessionID); err == nil {
		return got
	}
	return spec.FinalVideoSpec{}
}

func (t WritePromptTool) generatePrompts(ctx context.Context, finalSpec spec.FinalVideoSpec, board storyboard.Storyboard, payload WritePromptPayload, targets []promptTarget) ([]promptModelItem, error) {
	if strings.TrimSpace(payload.Prompt) != "" && len(targets) == 1 {
		return []promptModelItem{{
			TargetType: targets[0].Type,
			TargetID:   targets[0].ID,
			Prompt:     strings.TrimSpace(payload.Prompt),
		}}, nil
	}

	request := map[string]any{
		"task": "为 AIGC 故事板目标生成可直接用于图片/视频/音频模型的提示词",
		"output_schema": map[string]any{
			"prompts": []map[string]string{{
				"target_type": "key_element | shot | audio_layer",
				"target_id":   "目标 ID",
				"prompt":      "可直接用于生成模型的中文提示词",
			}},
		},
		"rules": []string{
			"只输出 JSON，不要 Markdown，不要解释。",
			"prompt 要融合 Final Video Spec 的风格、画幅、模型偏好和故事板目标内容。",
			"图片/视频提示词要明确主体、场景、镜头、光影、材质、风格约束和负向约束。",
			"不要虚构故事板之外的核心角色关系；可以补充必要的镜头语言和生成模型友好的细节。",
		},
		"prompt_purpose":  firstNonEmpty(payload.PromptPurpose, defaultPromptPurpose(payload.TargetType)),
		"extra_direction": strings.TrimSpace(payload.ExtraDirection),
		"final_video_spec": map[string]any{
			"id":               finalSpec.ID,
			"title":            finalSpec.Title,
			"video_type":       finalSpec.VideoType,
			"duration_seconds": finalSpec.DurationSeconds,
			"aspect_ratio":     finalSpec.AspectRatio,
			"visual_style":     finalSpec.VisualStyle,
			"sound_style":      finalSpec.SoundStyle,
			"model_preference": finalSpec.ModelPreference,
			"markdown":         truncateString(finalSpec.Markdown, 3000),
		},
		"storyboard": map[string]any{
			"id":      board.ID,
			"version": board.Version,
		},
		"targets": targets,
	}
	requestJSON, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal prompt generation request: %w", err)
	}

	resp, err := t.cfg.Model.Generate(ctx, []*schema.Message{
		schema.SystemMessage("你是专业 AIGC 提示词导演。你只返回合法 JSON，字段必须符合用户给定 schema。"),
		schema.UserMessage(string(requestJSON)),
	})
	if err != nil {
		return nil, fmt.Errorf("generate prompts with deepseek: %w", err)
	}
	parsed, err := decodePromptModelResponse(resp.Content)
	if err != nil {
		return nil, err
	}
	return parsed.Prompts, nil
}

func decodePromptModelResponse(content string) (promptModelResponse, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return promptModelResponse{}, fmt.Errorf("deepseek returned empty prompt response")
	}
	var parsed promptModelResponse
	if err := json.Unmarshal([]byte(content), &parsed); err == nil && len(parsed.Prompts) > 0 {
		return parsed, nil
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return promptModelResponse{}, fmt.Errorf("deepseek prompt response is not JSON")
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &parsed); err != nil {
		return promptModelResponse{}, fmt.Errorf("decode deepseek prompt response: %w", err)
	}
	if len(parsed.Prompts) == 0 {
		return promptModelResponse{}, fmt.Errorf("deepseek prompt response contains no prompts")
	}
	return parsed, nil
}

func selectPromptTargets(board storyboard.Storyboard, payload WritePromptPayload) ([]promptTarget, error) {
	targetType := normalizePromptTargetType(payload.TargetType)
	ids := promptTargetIDSet(payload)
	var out []promptTarget
	if targetType == "" || targetType == "all" || targetType == "key_element" {
		for idx, element := range board.KeyElements {
			if len(ids) > 0 && !ids[element.Key] {
				continue
			}
			out = append(out, promptTarget{
				Type:       "key_element",
				ID:         element.Key,
				PromptPath: fmt.Sprintf("/key_elements/%d/prompt", idx),
				StatusPath: fmt.Sprintf("/key_elements/%d/status", idx),
				Context: map[string]any{
					"type":        element.Type,
					"name":        element.Name,
					"description": element.Description,
					"status":      element.Status,
				},
			})
		}
	}
	if targetType == "" || targetType == "all" || targetType == "shot" {
		for idx, shot := range board.Shots {
			if len(ids) > 0 && !ids[shot.ShotID] {
				continue
			}
			out = append(out, promptTarget{
				Type:       "shot",
				ID:         shot.ShotID,
				PromptPath: fmt.Sprintf("/shots/%d/prompt", idx),
				StatusPath: fmt.Sprintf("/shots/%d/status", idx),
				Context: map[string]any{
					"index":              shot.Index,
					"duration_sec":       shot.DurationSec,
					"scene_description":  shot.SceneDescription,
					"camera_design":      shot.CameraDesign,
					"narration":          shot.Narration,
					"reference_elements": shot.ReferenceElements,
					"status":             shot.Status,
				},
			})
		}
	}
	if targetType == "" || targetType == "all" || targetType == "audio_layer" {
		for idx, layer := range board.AudioLayers {
			if len(ids) > 0 && !ids[layer.LayerID] {
				continue
			}
			out = append(out, promptTarget{
				Type:       "audio_layer",
				ID:         layer.LayerID,
				PromptPath: fmt.Sprintf("/audio_layers/%d/prompt", idx),
				StatusPath: fmt.Sprintf("/audio_layers/%d/status", idx),
				Context: map[string]any{
					"type":        layer.Type,
					"description": layer.Description,
					"status":      layer.Status,
				},
			})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no storyboard targets matched target_type=%q target_ids=%v", payload.TargetType, payload.TargetIDs)
	}
	return out, nil
}

func promptTargetIDSet(payload WritePromptPayload) map[string]bool {
	ids := append([]string(nil), payload.TargetIDs...)
	if strings.TrimSpace(payload.TargetID) != "" {
		ids = append(ids, payload.TargetID)
	}
	out := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			out[id] = true
		}
	}
	return out
}

func normalizePromptTargetType(targetType string) string {
	switch strings.TrimSpace(targetType) {
	case "key_element", "key_elements", "element", "elements":
		return "key_element"
	case "shot", "shots":
		return "shot"
	case "audio_layer", "audio_layers", "audio":
		return "audio_layer"
	case "all", "":
		return strings.TrimSpace(targetType)
	default:
		return strings.TrimSpace(targetType)
	}
}

func defaultPromptPurpose(targetType string) string {
	switch normalizePromptTargetType(targetType) {
	case "key_element":
		return "element_image"
	case "shot":
		return "shot_video"
	case "audio_layer":
		return "audio_layer"
	default:
		return "storyboard_generation"
	}
}

func promptPatchOps(targets []promptTarget, prompts []promptModelItem) ([]JSONPatchOp, []PromptTargetResult, error) {
	promptByTarget := map[string]string{}
	for _, item := range prompts {
		key := promptTargetKey(normalizePromptTargetType(item.TargetType), item.TargetID)
		if key == "\x00" {
			continue
		}
		if prompt := strings.TrimSpace(item.Prompt); prompt != "" {
			promptByTarget[key] = prompt
		}
	}
	ops := make([]JSONPatchOp, 0, len(targets)*2)
	results := make([]PromptTargetResult, 0, len(targets))
	for _, target := range targets {
		prompt := promptByTarget[promptTargetKey(target.Type, target.ID)]
		if strings.TrimSpace(prompt) == "" {
			return nil, nil, fmt.Errorf("deepseek did not return prompt for %s %s", target.Type, target.ID)
		}
		ops = append(ops,
			JSONPatchOp{Op: "replace", Path: target.PromptPath, Value: prompt},
			JSONPatchOp{Op: "replace", Path: target.StatusPath, Value: promptStatusReady},
		)
		results = append(results, PromptTargetResult{
			TargetType:    target.Type,
			TargetID:      target.ID,
			PromptPath:    target.PromptPath,
			StatusPath:    target.StatusPath,
			PromptExcerpt: truncateString(prompt, promptMaxReturnedExcerpt),
		})
	}
	return ops, results, nil
}

func promptTargetKey(targetType string, targetID string) string {
	return strings.TrimSpace(targetType) + "\x00" + strings.TrimSpace(targetID)
}

func promptRenderEvents(event storyboard.EventRecord, updatedTargets []PromptTargetResult) []RenderEventHint {
	return []RenderEventHint{{
		Event:        renderEventSurfaceUpdate,
		SurfaceID:    "storyboard",
		DataModelKey: "storyboard",
		Payload: map[string]any{
			"message":          "提示词已写入故事板",
			"storyboard_id":    event.StoryboardID,
			"base_version":     event.BaseVersion,
			"next_version":     event.NextVersion,
			"source":           event.Source,
			"updated_targets":  updatedTargets,
			"redacted_payload": true,
		},
	}}
}

func truncateString(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

var _ einotool.InvokableTool = WritePromptTool{}
