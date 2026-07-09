package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

func TestStoryboardDesignerToolSavesSnapshot(t *testing.T) {
	store := &fakeStoryboardSnapshotStore{}
	tool := NewStoryboardDesignerTool(StoryboardDesignerToolConfig{Storyboards: store})

	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"expected_spec_version":2,
		"expected_storyboard_version":3,
		"action":"design_storyboard",
		"payload":{
			"storyboard_id":"storyboard-1",
			"spec_id":"spec-1",
			"status":"reviewing",
			"key_elements":[{"key":"suji","type":"character","name":"苏寂","status":"planned"}],
			"shots":[{"shot_id":"shot-1","index":1,"scene_description":"竹林归隐","status":"planned"}],
			"audio_layers":[{"layer_id":"music-1","type":"music","description":"悲凉沉郁"}]
		}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	var got ToolResultEnvelope[StoryboardDesignerResult]
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.Status != ToolStatusOK || got.SpecVersion != 2 || got.StoryboardVersion != 4 {
		t.Fatalf("result envelope = %#v", got)
	}
	if got.Data.Storyboard.ID != "storyboard-1" || got.Data.Storyboard.Shots[0].ShotID != "shot-1" {
		t.Fatalf("result data = %#v", got.Data)
	}
	if store.seen.Version != 4 || store.seen.SessionID != "s1" {
		t.Fatalf("saved board = %#v", store.seen)
	}
}

func TestStoryboardDesignerToolDefaultsStoryboardIDFromEnvelope(t *testing.T) {
	store := &fakeStoryboardSnapshotStore{}
	tool := NewStoryboardDesignerTool(StoryboardDesignerToolConfig{Storyboards: store})

	_, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"design_storyboard",
		"payload":{
			"shots":[{"shot_id":"shot-1","index":1}]
		}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	if store.seen.ID != "storyboard:s1" || store.seen.Version != 1 {
		t.Fatalf("saved board = %#v", store.seen)
	}
}

func TestStoryboardDesignerToolRejectsDirectPayload(t *testing.T) {
	store := &fakeStoryboardSnapshotStore{}
	tool := NewStoryboardDesignerTool(StoryboardDesignerToolConfig{Storyboards: store})

	_, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"shots":[{"shot_id":"shot-1","index":1}]
	}`)
	if err == nil {
		t.Fatal("expected direct payload to be rejected")
	}
}

func TestStoryboardDesignerToolNormalizesMissingStableIDs(t *testing.T) {
	store := &fakeStoryboardSnapshotStore{}
	tool := NewStoryboardDesignerTool(StoryboardDesignerToolConfig{Storyboards: store})

	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"design_storyboard",
		"payload":{
			"key_elements":[
				{"type":"product","name":"蓝牙音箱主体","status":"planned"},
				{"type":"scene","name":"户外便携场景","status":"planned"}
			],
			"shots":[
				{"scene_description":"开场展示产品","status":"planned"},
				{"scene_description":"户外使用场景","status":"planned"}
			],
			"audio_layers":[
				{"type":"background_music","description":"电子节奏"},
				{"type":"sound_effects","description":"低频脉冲"}
			]
		}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	var got ToolResultEnvelope[StoryboardDesignerResult]
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	board := got.Data.Storyboard
	if board.KeyElements[0].Key == "" || board.KeyElements[1].Key == "" || board.KeyElements[0].Key == board.KeyElements[1].Key {
		t.Fatalf("key element ids were not normalized: %#v", board.KeyElements)
	}
	if board.Shots[0].ShotID != "shot-1" || board.Shots[1].ShotID != "shot-2" {
		t.Fatalf("shot ids were not normalized: %#v", board.Shots)
	}
	if board.Shots[0].Index != 1 || board.Shots[1].Index != 2 {
		t.Fatalf("shot indexes were not normalized: %#v", board.Shots)
	}
	if board.AudioLayers[0].LayerID == "" || board.AudioLayers[1].LayerID == "" || board.AudioLayers[0].LayerID == board.AudioLayers[1].LayerID {
		t.Fatalf("audio layer ids were not normalized: %#v", board.AudioLayers)
	}
	if store.seen.KeyElements[0].Key != board.KeyElements[0].Key || store.seen.Shots[0].ShotID != "shot-1" {
		t.Fatalf("saved board did not use normalized ids: %#v", store.seen)
	}
}

type fakeStoryboardSnapshotStore struct {
	seen storyboard.Storyboard
}

func (s *fakeStoryboardSnapshotStore) SaveSnapshot(_ context.Context, board storyboard.Storyboard) error {
	s.seen = board
	return nil
}
