package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

const StoryboardDesignerToolKey = "storyboard_designer"

type StoryboardSnapshotStore interface {
	SaveSnapshot(ctx context.Context, board storyboard.Storyboard) error
}

type StoryboardDesignerToolConfig struct {
	Storyboards StoryboardSnapshotStore
}

type StoryboardDesignerTool struct {
	cfg StoryboardDesignerToolConfig
}

type StoryboardDesignerPayload struct {
	SessionID    string                  `json:"session_id,omitempty"`
	StoryboardID string                  `json:"storyboard_id,omitempty"`
	SpecID       string                  `json:"spec_id,omitempty"`
	Version      int                     `json:"version,omitempty"`
	Status       string                  `json:"status,omitempty"`
	KeyElements  []storyboard.KeyElement `json:"key_elements,omitempty"`
	Shots        []storyboard.Shot       `json:"shots,omitempty"`
	AudioLayers  []storyboard.AudioLayer `json:"audio_layers,omitempty"`
	Metadata     map[string]any          `json:"metadata,omitempty"`
}

type StoryboardDesignerResult struct {
	Storyboard storyboard.Storyboard `json:"storyboard"`
	Metadata   map[string]any        `json:"metadata,omitempty"`
}

func NewStoryboardDesignerTool(cfg StoryboardDesignerToolConfig) StoryboardDesignerTool {
	return StoryboardDesignerTool{cfg: cfg}
}

func (StoryboardDesignerTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: StoryboardDesignerToolKey,
		Desc: "Create or update the storyboard plan for AIGC video creation. It saves key elements, shot list, narration/music/audio layers, prompts, references, and per-item statuses as the left storyboard panel snapshot.",
		ParamsOneOf: schema.NewParamsOneOfByParams(toolInvocationEnvelopeParams(map[string]*schema.ParameterInfo{
			"storyboard_id": {
				Type: schema.String,
				Desc: "Optional storyboard ID. Defaults to storyboard:<session_id>.",
			},
			"spec_id": {
				Type: schema.String,
				Desc: "Final video spec ID this storyboard is derived from.",
			},
			"status": {
				Type: schema.String,
				Desc: "Storyboard status: draft, reviewing, confirmed, generating, or ready.",
				Enum: []string{storyboard.StatusDraft, storyboard.StatusReviewing, storyboard.StatusConfirmed, storyboard.StatusGenerating, storyboard.StatusReady},
			},
			"key_elements": {
				Type: schema.Array,
				Desc: "Characters, scenes, props, style references, or other reusable elements.",
			},
			"shots": {
				Type:     schema.Array,
				Desc:     "Ordered shot list with scene description, camera design, narration, references, prompt, and generation status.",
				Required: true,
			},
			"audio_layers": {
				Type: schema.Array,
				Desc: "Narration, music, ambience, and sound effect layers.",
			},
		})),
	}, nil
}

func (t StoryboardDesignerTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t.cfg.Storyboards == nil {
		return "", fmt.Errorf("storyboard store is required")
	}
	invocation, err := decodeStoryboardDesignerInvocation(argumentsInJSON)
	if err != nil {
		return "", err
	}
	payload := invocation.Payload
	sessionID := strings.TrimSpace(firstNonEmpty(invocation.SessionID, payload.SessionID, sessionIDFromContext(ctx)))
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	if len(payload.Shots) == 0 {
		return "", fmt.Errorf("shots are required")
	}

	storyboardID := strings.TrimSpace(payload.StoryboardID)
	if storyboardID == "" {
		storyboardID = "storyboard:" + sessionID
	}
	version := payload.Version
	if version <= 0 {
		version = invocation.ExpectedStoryboardVersion + 1
		if version <= 0 {
			version = 1
		}
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = storyboard.StatusReviewing
	}
	board := storyboard.Storyboard{
		ID:          storyboardID,
		SessionID:   sessionID,
		SpecID:      strings.TrimSpace(payload.SpecID),
		Version:     version,
		Status:      status,
		KeyElements: append([]storyboard.KeyElement(nil), payload.KeyElements...),
		Shots:       append([]storyboard.Shot(nil), payload.Shots...),
		AudioLayers: append([]storyboard.AudioLayer(nil), payload.AudioLayers...),
	}
	board = normalizeStoryboardIDs(board)
	if err := t.cfg.Storyboards.SaveSnapshot(ctx, board); err != nil {
		return "", err
	}

	out, err := json.Marshal(ToolResultEnvelope[StoryboardDesignerResult]{
		Status:            ToolStatusOK,
		RequestID:         invocation.RequestID,
		IdempotencyKey:    invocation.IdempotencyKey,
		SpecVersion:       invocation.ExpectedSpecVersion,
		StoryboardVersion: board.Version,
		Data: StoryboardDesignerResult{
			Storyboard: board,
			Metadata:   payload.Metadata,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal storyboard designer result: %w", err)
	}
	return string(out), nil
}

func decodeStoryboardDesignerInvocation(argumentsInJSON string) (ToolInvocationEnvelope[StoryboardDesignerPayload], error) {
	return decodeToolInvocationEnvelope(StoryboardDesignerToolKey, argumentsInJSON, func(payload StoryboardDesignerPayload) bool {
		return len(payload.Shots) > 0
	})
}

func normalizeStoryboardIDs(board storyboard.Storyboard) storyboard.Storyboard {
	seenElements := map[string]int{}
	typeCounters := map[string]int{}
	for i := range board.KeyElements {
		key := strings.TrimSpace(board.KeyElements[i].Key)
		if key == "" {
			base := normalizeIDPart(board.KeyElements[i].Type)
			if base == "" {
				base = "element"
			}
			typeCounters[base]++
			key = fmt.Sprintf("%s_%d", base, typeCounters[base])
		}
		board.KeyElements[i].Key = uniqueID(key, seenElements)
	}

	seenShots := map[string]int{}
	for i := range board.Shots {
		if board.Shots[i].Index <= 0 {
			board.Shots[i].Index = i + 1
		}
		shotID := strings.TrimSpace(board.Shots[i].ShotID)
		if shotID == "" {
			shotID = fmt.Sprintf("shot-%d", board.Shots[i].Index)
		}
		board.Shots[i].ShotID = uniqueID(shotID, seenShots)
	}

	seenLayers := map[string]int{}
	layerCounters := map[string]int{}
	for i := range board.AudioLayers {
		layerID := strings.TrimSpace(board.AudioLayers[i].LayerID)
		if layerID == "" {
			base := normalizeIDPart(board.AudioLayers[i].Type)
			if base == "" {
				base = "audio"
			}
			layerCounters[base]++
			layerID = fmt.Sprintf("%s_%d", base, layerCounters[base])
		}
		board.AudioLayers[i].LayerID = uniqueID(layerID, seenLayers)
	}
	return board
}

func uniqueID(value string, seen map[string]int) string {
	base := normalizeIDPart(value)
	if base == "" {
		base = "item"
	}
	seen[base]++
	if seen[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, seen[base])
}

func normalizeIDPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '-' || r == '_':
			if b.Len() > 0 && !lastUnderscore {
				b.WriteRune(r)
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

var _ einotool.InvokableTool = StoryboardDesignerTool{}
