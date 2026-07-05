package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const (
	ResourcePrepareAnalyzeToolKey = "resource_prepare_and_analyze"
	MultimodalAnalyzeToolKey      = "multimodal_analyze_tool"
	WritePromptToolKey            = "write_the_prompt"
	VideoAssemblerToolKey         = "video_assembler"
)

type ResourcePrepareAnalyzeTool struct{}

type MultimodalAnalyzeTool struct{}

type VideoAssemblerTool struct{}

func (ResourcePrepareAnalyzeTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ResourcePrepareAnalyzeToolKey,
		Desc: "Prepare and analyze uploaded resources for the AIGC creation flow. Demo version returns a normalized analysis envelope for scripts, images, PDFs, text, audio, and video references.",
		ParamsOneOf: schema.NewParamsOneOfByParams(commonPipelineParams(map[string]*schema.ParameterInfo{
			"asset_ids": {
				Type: schema.Array,
				Desc: "Uploaded or existing asset ids to analyze.",
			},
			"brief": {
				Type: schema.String,
				Desc: "Short analysis goal, such as summarizing a script or extracting usable elements.",
			},
		})),
	}, nil
}

func (ResourcePrepareAnalyzeTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	return pipelineToolResult(ResourcePrepareAnalyzeToolKey, "resource_analysis_ready", argumentsInJSON)
}

func (MultimodalAnalyzeTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: MultimodalAnalyzeToolKey,
		Desc: "Analyze multimodal resources such as images, video, audio, PDFs, and text for reusable AIGC elements. Demo version returns a normalized analysis envelope.",
		ParamsOneOf: schema.NewParamsOneOfByParams(commonPipelineParams(map[string]*schema.ParameterInfo{
			"asset_ids": {
				Type: schema.Array,
				Desc: "Asset ids to analyze.",
			},
			"brief": {
				Type: schema.String,
				Desc: "Analysis goal, such as extracting visual references or summarizing uploaded resources.",
			},
		})),
	}, nil
}

func (MultimodalAnalyzeTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	return pipelineToolResult(MultimodalAnalyzeToolKey, "multimodal_analysis_ready", argumentsInJSON)
}

func (VideoAssemblerTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: VideoAssemblerToolKey,
		Desc: "Assemble generated video and audio assets into a final deliverable. Demo version returns an assembly plan envelope and export status placeholder.",
		ParamsOneOf: schema.NewParamsOneOfByParams(commonPipelineParams(map[string]*schema.ParameterInfo{
			"storyboard_id": {
				Type: schema.String,
				Desc: "Storyboard id to assemble.",
			},
			"video_asset_ids": {
				Type: schema.Array,
				Desc: "Video asset ids to include in order.",
			},
			"audio_asset_ids": {
				Type: schema.Array,
				Desc: "Audio asset ids to mix into the final video.",
			},
		})),
	}, nil
}

func (VideoAssemblerTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	return pipelineToolResult(VideoAssemblerToolKey, "assembly_plan_ready", argumentsInJSON)
}

func commonPipelineParams(extra map[string]*schema.ParameterInfo) map[string]*schema.ParameterInfo {
	params := map[string]*schema.ParameterInfo{
		"session_id": {
			Type:     schema.String,
			Desc:     "Current AIGC session id.",
			Required: true,
		},
		"request_id": {
			Type: schema.String,
			Desc: "Optional request id for idempotent UI correlation.",
		},
	}
	for key, value := range extra {
		params[key] = value
	}
	return params
}

func pipelineToolResult(toolKey string, state string, argumentsInJSON string) (string, error) {
	payload, err := decodePipelinePayload(argumentsInJSON)
	if err != nil {
		return "", err
	}
	payload["tool_key"] = toolKey
	payload["state"] = state
	if _, ok := payload["summary"]; !ok {
		payload["summary"] = defaultPipelineSummary(toolKey)
	}
	out, err := json.Marshal(ToolResultEnvelope[map[string]any]{
		Status: ToolStatusOK,
		Data:   payload,
	})
	if err != nil {
		return "", fmt.Errorf("marshal %s result: %w", toolKey, err)
	}
	return string(out), nil
}

func decodePipelinePayload(argumentsInJSON string) (map[string]any, error) {
	var enveloped ToolInvocationEnvelope[map[string]any]
	if err := json.Unmarshal([]byte(argumentsInJSON), &enveloped); err == nil && enveloped.Payload != nil {
		return normalizePipelinePayload(enveloped.Payload), nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &payload); err != nil {
		return nil, fmt.Errorf("decode pipeline tool input: %w", err)
	}
	return normalizePipelinePayload(payload), nil
}

func normalizePipelinePayload(payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	if sessionID, _ := payload["session_id"].(string); strings.TrimSpace(sessionID) != sessionID {
		payload["session_id"] = strings.TrimSpace(sessionID)
	}
	return payload
}

func defaultPipelineSummary(toolKey string) string {
	switch toolKey {
	case ResourcePrepareAnalyzeToolKey:
		return "resources prepared and analyzed"
	case MultimodalAnalyzeToolKey:
		return "multimodal resources analyzed"
	case WritePromptToolKey:
		return "prompt prepared"
	case VideoAssemblerToolKey:
		return "assembly plan prepared"
	default:
		return "tool completed"
	}
}

var _ einotool.InvokableTool = ResourcePrepareAnalyzeTool{}
var _ einotool.InvokableTool = MultimodalAnalyzeTool{}
var _ einotool.InvokableTool = VideoAssemblerTool{}
