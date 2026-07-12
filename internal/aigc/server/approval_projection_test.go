package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

func TestPublishApprovalDecisionClosesPlanningToolRun(t *testing.T) {
	for _, test := range []struct {
		name           string
		artifactType   string
		approvalStatus approval.Status
		toolKey        string
		toolStatus     string
	}{
		{name: "approved creation spec", artifactType: "creation_spec_revision", approvalStatus: approval.StatusApproved, toolKey: capability.PlanCreationSpecToolKey, toolStatus: "completed"},
		{name: "rejected storyboard", artifactType: "storyboard_revision", approvalStatus: approval.StatusRejected, toolKey: capability.PlanStoryboardToolKey, toolStatus: "cancelled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			broker := &fakeEventSubscriber{}
			payload := json.RawMessage(`{"tool_call_id":"call-review","stage_run_id":"stage:call-review"}`)
			record := approval.Approval{
				ID: "approval-1", SessionID: "session-1", ArtifactType: test.artifactType,
				Status: test.approvalStatus, DecisionVersion: 1,
				ApproveCommand: approval.FrozenCommand{Payload: payload},
				RejectCommand:  approval.FrozenCommand{Payload: payload},
			}
			cfg := Config{Events: broker, Now: fixedNow}
			if err := cfg.publishApprovalDecision(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			if len(broker.published) != 1 {
				t.Fatalf("published events = %#v", broker.published)
			}
			envelope, ok := broker.published[0].Payload.(a2ui.ActionEnvelope)
			if !ok || len(envelope.Actions) != 2 {
				t.Fatalf("decision envelope = %#v", broker.published[0].Payload)
			}
			approvalAction, toolAction := envelope.Actions[0], envelope.Actions[1]
			if approvalAction.Target == nil || approvalAction.Target.CardID != "approval:approval-1" {
				t.Fatalf("approval terminal action = %#v", approvalAction)
			}
			if toolAction.Target == nil || toolAction.Target.Surface != "tool_runs" || toolAction.Target.CardID != "tool_run:call-review" {
				t.Fatalf("ToolRun terminal target = %#v", toolAction)
			}
			values, ok := toolAction.Payload.(map[string]any)
			if !ok {
				t.Fatalf("ToolRun payload = %#v", toolAction.Payload)
			}
			toolRun, ok := values["tool_run"].(map[string]any)
			if !ok || toolRun["tool_key"] != test.toolKey || toolRun["status"] != test.toolStatus || toolRun["stage_run_id"] != "stage:call-review" {
				t.Fatalf("ToolRun projection = %#v", values["tool_run"])
			}
		})
	}
}

func TestPublishApprovalDecisionDoesNotGuessToolRunWithoutFrozenCorrelation(t *testing.T) {
	broker := &fakeEventSubscriber{}
	record := approval.Approval{
		ID: "approval-legacy", SessionID: "session-1", ArtifactType: "creation_spec_revision",
		Status: approval.StatusApproved, DecisionVersion: 1,
		ApproveCommand: approval.FrozenCommand{Payload: json.RawMessage(`{"spec_id":"spec-1"}`)},
		RejectCommand:  approval.FrozenCommand{Payload: json.RawMessage(`{"spec_id":"spec-1"}`)},
	}
	if err := (Config{Events: broker, Now: fixedNow}).publishApprovalDecision(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	envelope := broker.published[0].Payload.(a2ui.ActionEnvelope)
	if len(envelope.Actions) != 1 || envelope.Actions[0].Target.CardID != "approval:approval-legacy" {
		t.Fatalf("legacy Decision guessed a ToolRun: %#v", envelope.Actions)
	}
}
