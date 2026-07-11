package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

func TestWritePromptToolUsesDeepSeekAndPatchesStoryboard(t *testing.T) {
	model := &fakePromptChatModel{
		response: `{"prompts":[{"target_type":"shot","target_id":"shot-1","prompt":"真人电影实拍风格，35mm 胶片，冷郁竹林中苏寂缓慢拔出旧剑，镜头克制，真实光影。"}]}`,
	}
	specs := &fakePromptSpecStore{
		latest: spec.FinalVideoSpec{
			ID:              "spec-1",
			SessionID:       "s1",
			Version:         2,
			Title:           "归隐·藏锋",
			VisualStyle:     "真人电影实拍风格，Kodak Vision3 500T，冷郁写实。",
			ModelPreference: "Image2 + Seedance 2.0",
			Markdown:        "Final Video Spec",
		},
	}
	storyboards := &fakePromptStoryboardStore{
		board: storyboard.Storyboard{
			ID:        "storyboard-1",
			SessionID: "s1",
			SpecID:    "spec-1",
			Version:   3,
			Status:    storyboard.StatusReviewing,
			KeyElements: []storyboard.KeyElement{{
				Key:         "suji",
				Type:        "character",
				Name:        "苏寂",
				Description: "粗布麻衣，鬓角染霜，佩剑覆黑布。",
			}},
			Shots: []storyboard.Shot{{
				ShotID:            "shot-1",
				Index:             1,
				DurationSec:       6,
				SceneDescription:  "竹林归隐",
				CameraDesign:      "冷色自然光，固定长镜头。",
				ReferenceElements: []string{"suji"},
				Status:            "planned",
			}},
		},
	}
	tool := NewWritePromptTool(WritePromptToolConfig{
		Model:       model,
		Specs:       specs,
		Storyboards: storyboards,
	})

	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"expected_spec_version":2,
		"expected_storyboard_version":3,
		"action":"write_prompts",
		"payload":{
			"storyboard_id":"storyboard-1",
			"target_type":"shot",
			"target_ids":["shot-1"],
			"prompt_purpose":"shot_keyframe"
		}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	if len(model.messages) != 2 {
		t.Fatalf("model messages = %#v", model.messages)
	}
	if !strings.Contains(model.messages[1].Content, "归隐·藏锋") || !strings.Contains(model.messages[1].Content, "竹林归隐") {
		t.Fatalf("model prompt missing spec/storyboard context: %s", model.messages[1].Content)
	}

	var got ToolResultEnvelope[WritePromptResult]
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.Status != ToolStatusOK || got.RequestID != "req-1" || got.StoryboardVersion != 4 {
		t.Fatalf("result envelope = %#v", got)
	}
	if len(got.Data.UpdatedTargets) != 1 || got.Data.UpdatedTargets[0].TargetID != "shot-1" {
		t.Fatalf("updated targets = %#v", got.Data.UpdatedTargets)
	}
	if strings.Contains(out, "真人电影实拍风格，35mm 胶片，冷郁竹林中苏寂缓慢拔出旧剑，镜头克制，真实光影。") {
		t.Fatalf("tool result should not return full prompt text: %s", out)
	}
	if strings.Contains(out, "render_events") || strings.Contains(out, "a2ui.surface_update") {
		t.Fatalf("tool result should not include render events: %s", out)
	}

	if storyboards.patch.BaseVersion != 3 || storyboards.patch.Source != WritePromptToolKey {
		t.Fatalf("patch request = %#v", storyboards.patch)
	}
	if len(storyboards.patch.Ops) != 2 {
		t.Fatalf("patch ops = %#v", storyboards.patch.Ops)
	}
	if storyboards.patch.Ops[0].Path != "/shots/0/prompt" || storyboards.patch.Ops[1].Path != "/shots/0/status" {
		t.Fatalf("patch ops = %#v", storyboards.patch.Ops)
	}
}

type fakePromptChatModel struct {
	response string
	messages []*schema.Message
}

func (m *fakePromptChatModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.messages = append([]*schema.Message(nil), input...)
	return schema.AssistantMessage(m.response, nil), nil
}

func (m *fakePromptChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, io.ErrClosedPipe
}

type fakePromptSpecStore struct {
	latest spec.FinalVideoSpec
}

func (s *fakePromptSpecStore) Get(_ context.Context, specID string) (spec.FinalVideoSpec, error) {
	if specID == s.latest.ID {
		return s.latest, nil
	}
	return spec.FinalVideoSpec{}, errors.New("spec not found")
}

func (s *fakePromptSpecStore) GetLatestBySession(_ context.Context, sessionID string) (spec.FinalVideoSpec, error) {
	if sessionID == s.latest.SessionID {
		return s.latest, nil
	}
	return spec.FinalVideoSpec{}, errors.New("spec not found")
}

type fakePromptStoryboardStore struct {
	board storyboard.Storyboard
	patch storyboard.PatchRequest
}

func (s *fakePromptStoryboardStore) Get(_ context.Context, storyboardID string) (storyboard.Storyboard, error) {
	if storyboardID == s.board.ID {
		return s.board, nil
	}
	return storyboard.Storyboard{}, errors.New("storyboard not found")
}

func (s *fakePromptStoryboardStore) GetLatestBySession(_ context.Context, sessionID string) (storyboard.Storyboard, error) {
	if sessionID == s.board.SessionID {
		return s.board, nil
	}
	return storyboard.Storyboard{}, errors.New("storyboard not found")
}

func (s *fakePromptStoryboardStore) ApplyPatch(_ context.Context, req storyboard.PatchRequest) (storyboard.Storyboard, storyboard.EventRecord, error) {
	s.patch = req
	patched := s.board
	patched.Version = req.BaseVersion + 1
	for _, op := range req.Ops {
		if op.Path == "/shots/0/prompt" {
			patched.Shots[0].Prompt, _ = op.Value.(string)
		}
		if op.Path == "/shots/0/status" {
			patched.Shots[0].Status, _ = op.Value.(string)
		}
	}
	event := storyboard.EventRecord{
		ID:           "event-1",
		SessionID:    req.SessionID,
		StoryboardID: req.StoryboardID,
		BaseVersion:  req.BaseVersion,
		NextVersion:  patched.Version,
		Source:       req.Source,
		Ops:          req.Ops,
		CreatedAt:    time.Now(),
	}
	return patched, event, nil
}
