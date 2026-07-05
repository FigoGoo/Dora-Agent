package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
)

func TestTextEditorToolSavesFinalVideoSpec(t *testing.T) {
	store := &fakeSpecStore{}
	tool := NewTextEditorTool(TextEditorToolConfig{Specs: store})

	out, err := tool.InvokableRun(context.Background(), `{
		"session_id":"s1",
		"request_id":"req-1",
		"idempotency_key":"idem-1",
		"action":"write_final_video_spec",
		"payload":{
			"document_type":"final_video_spec",
			"spec_id":"spec-1",
			"title":"归隐·藏锋",
			"video_type":"武侠短片",
			"duration_seconds":120,
			"markdown":"# Final Video Spec\n\nVideo Title: 归隐·藏锋",
			"status":"reviewing"
		}
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	var got ToolResultEnvelope[TextEditorResult]
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.Status != ToolStatusOK || got.RequestID != "req-1" {
		t.Fatalf("result envelope = %#v", got)
	}
	if got.SpecVersion != 7 || got.Data.Spec.ID != "spec-1" || got.Data.Spec.Markdown == "" {
		t.Fatalf("result data = %#v", got.Data)
	}
	if store.seen.SessionID != "s1" || store.seen.Title != "归隐·藏锋" {
		t.Fatalf("saved spec = %#v", store.seen)
	}
}

func TestTextEditorToolDefaultsSpecIDForDirectInput(t *testing.T) {
	store := &fakeSpecStore{}
	tool := NewTextEditorTool(TextEditorToolConfig{Specs: store})

	_, err := tool.InvokableRun(context.Background(), `{
		"document_type":"final_video_spec",
		"session_id":"s1",
		"markdown":"# Final Video Spec"
	}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	if store.seen.ID != "final_video_spec:s1" {
		t.Fatalf("spec id = %q", store.seen.ID)
	}
}

type fakeSpecStore struct {
	seen spec.FinalVideoSpec
}

func (s *fakeSpecStore) Save(_ context.Context, in spec.FinalVideoSpec) (spec.FinalVideoSpec, error) {
	s.seen = in
	in.Version = 7
	return in, nil
}
