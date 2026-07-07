package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
)

const TextEditorToolKey = "text_editor"

type FinalVideoSpecStore interface {
	Save(ctx context.Context, in spec.FinalVideoSpec) (spec.FinalVideoSpec, error)
}

type TextEditorToolConfig struct {
	Specs FinalVideoSpecStore
}

type TextEditorTool struct {
	cfg TextEditorToolConfig
}

type TextEditorPayload struct {
	DocumentType    string         `json:"document_type"`
	SessionID       string         `json:"session_id,omitempty"`
	SpecID          string         `json:"spec_id,omitempty"`
	Status          string         `json:"status,omitempty"`
	Title           string         `json:"title,omitempty"`
	VideoType       string         `json:"video_type,omitempty"`
	TargetAudience  string         `json:"target_audience,omitempty"`
	OutputLanguage  string         `json:"output_language,omitempty"`
	DurationSeconds int            `json:"duration_seconds,omitempty"`
	AspectRatio     string         `json:"aspect_ratio,omitempty"`
	NarrativeDriver string         `json:"narrative_driver,omitempty"`
	VisualStyle     string         `json:"visual_style,omitempty"`
	SoundStyle      string         `json:"sound_style,omitempty"`
	ModelPreference string         `json:"model_preference,omitempty"`
	Markdown        string         `json:"markdown,omitempty"`
	Fields          map[string]any `json:"fields,omitempty"`
}

type TextEditorResult struct {
	DocumentType string              `json:"document_type"`
	Spec         spec.FinalVideoSpec `json:"spec"`
}

func NewTextEditorTool(cfg TextEditorToolConfig) TextEditorTool {
	return TextEditorTool{cfg: cfg}
}

func (TextEditorTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: TextEditorToolKey,
		Desc: "Create or update structured text documents for the AIGC flow. Demo supports final_video_spec: title, type, aspect ratio, duration, visual style, sound style, model preferences, and the canonical Final_Video_Spec.md markdown.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"document_type": {
				Type:     schema.String,
				Desc:     "Document type. Use final_video_spec for Final_Video_Spec.md.",
				Required: true,
				Enum:     []string{"final_video_spec"},
			},
			"spec_id": {
				Type: schema.String,
				Desc: "Optional final video spec ID. Defaults to final_video_spec:<session_id>.",
			},
			"title": {
				Type: schema.String,
				Desc: "Video title.",
			},
			"video_type": {
				Type: schema.String,
				Desc: "Video type, such as 武侠短片 / 故事驱动型叙事视频.",
			},
			"duration_seconds": {
				Type: schema.Integer,
				Desc: "Target duration in seconds.",
			},
			"aspect_ratio": {
				Type: schema.String,
				Desc: "Aspect ratio, such as 16:9.",
			},
			"markdown": {
				Type:     schema.String,
				Desc:     "Canonical Final_Video_Spec.md content.",
				Required: true,
			},
			"status": {
				Type: schema.String,
				Desc: "Spec status: draft, reviewing, or confirmed.",
				Enum: []string{spec.StatusDraft, spec.StatusReviewing, spec.StatusConfirmed},
			},
		}),
	}, nil
}

func (t TextEditorTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t.cfg.Specs == nil {
		return "", fmt.Errorf("final video spec store is required")
	}
	invocation, err := decodeTextEditorInvocation(argumentsInJSON)
	if err != nil {
		return "", err
	}
	payload := invocation.Payload
	sessionID := strings.TrimSpace(firstNonEmpty(invocation.SessionID, payload.SessionID, sessionIDFromContext(ctx)))
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(payload.DocumentType) != "final_video_spec" {
		return "", fmt.Errorf("unsupported document_type %q", payload.DocumentType)
	}
	if strings.TrimSpace(payload.Markdown) == "" {
		return "", fmt.Errorf("markdown is required")
	}

	specID := strings.TrimSpace(payload.SpecID)
	if specID == "" {
		specID = "final_video_spec:" + sessionID
	}
	saved, err := t.cfg.Specs.Save(ctx, spec.FinalVideoSpec{
		ID:              specID,
		SessionID:       sessionID,
		Status:          firstNonEmpty(payload.Status, spec.StatusDraft),
		Title:           payload.Title,
		VideoType:       payload.VideoType,
		TargetAudience:  payload.TargetAudience,
		OutputLanguage:  payload.OutputLanguage,
		DurationSeconds: payload.DurationSeconds,
		AspectRatio:     payload.AspectRatio,
		NarrativeDriver: payload.NarrativeDriver,
		VisualStyle:     payload.VisualStyle,
		SoundStyle:      payload.SoundStyle,
		ModelPreference: payload.ModelPreference,
		Markdown:        payload.Markdown,
		Fields:          payload.Fields,
	})
	if err != nil {
		return "", err
	}

	out, err := json.Marshal(ToolResultEnvelope[TextEditorResult]{
		Status:            ToolStatusOK,
		RequestID:         invocation.RequestID,
		IdempotencyKey:    invocation.IdempotencyKey,
		SpecVersion:       saved.Version,
		StoryboardVersion: invocation.ExpectedStoryboardVersion,
		Data: TextEditorResult{
			DocumentType: "final_video_spec",
			Spec:         saved,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal text editor result: %w", err)
	}
	return string(out), nil
}

func decodeTextEditorInvocation(argumentsInJSON string) (ToolInvocationEnvelope[TextEditorPayload], error) {
	var enveloped ToolInvocationEnvelope[TextEditorPayload]
	if err := json.Unmarshal([]byte(argumentsInJSON), &enveloped); err == nil && enveloped.Payload.DocumentType != "" {
		return enveloped, nil
	}

	var direct TextEditorPayload
	if err := json.Unmarshal([]byte(argumentsInJSON), &direct); err != nil {
		return ToolInvocationEnvelope[TextEditorPayload]{}, fmt.Errorf("decode text editor input: %w", err)
	}
	return ToolInvocationEnvelope[TextEditorPayload]{
		SessionID:      direct.SessionID,
		RequestID:      "direct",
		IdempotencyKey: direct.SpecID,
		Action:         "write_text_document",
		Payload:        direct,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

var _ einotool.InvokableTool = TextEditorTool{}
