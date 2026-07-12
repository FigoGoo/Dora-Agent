package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
)

type captureApprovalPublisher struct {
	event a2ui.SSEEvent
}

func (p *captureApprovalPublisher) Publish(_ context.Context, event a2ui.SSEEvent) error {
	p.event = event
	return nil
}

func TestPublishApprovalCardUsesAuthoritativeDecisionForm(t *testing.T) {
	publisher := &captureApprovalPublisher{}
	record := approval.Approval{
		ID:           "approval-storyboard-1",
		SessionID:    "session-1",
		ArtifactType: "review_revision",
		Binding: approval.VersionBinding{
			ArtifactID:      "revision-1",
			ArtifactVersion: 3,
			StoryboardID:    "missing-board",
		},
		Status:     approval.StatusPending,
		ReviewMode: approval.ReviewModeDurable,
	}

	if err := publishApprovalCard(context.Background(), publisher, record, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	envelope, ok := publisher.event.Payload.(a2ui.ActionEnvelope)
	if !ok || len(envelope.Actions) != 1 || envelope.Actions[0].Card == nil {
		t.Fatalf("approval event payload = %#v", publisher.event.Payload)
	}
	card := envelope.Actions[0].Card
	if card.SubmitLabel != "提交" {
		t.Fatalf("submit label = %q, want 提交", card.SubmitLabel)
	}
	rawCard, _ := json.Marshal(card)
	if strings.Contains(string(rawCard), record.Binding.ArtifactID) || strings.Contains(string(rawCard), record.ArtifactType+" ·") {
		t.Fatalf("approval card exposes internal artifact identifiers: %s", rawCard)
	}
	data, ok := card.Data.(map[string]any)
	if !ok || data["approval_id"] != record.ID {
		t.Fatalf("approval card data = %#v", card.Data)
	}

	decisionFields := 0
	for _, component := range card.Components {
		raw, exists := component.Component[a2ui.ComponentSingleChoice]
		if !exists {
			continue
		}
		choice, ok := raw.(a2ui.ChoiceComp)
		if !ok || choice.Key != "decision" || !choice.Required {
			t.Fatalf("decision field = %#v", raw)
		}
		decisionFields++
		if len(choice.Options) != 2 || choice.Options[0].Value != approval.DecisionApprove || choice.Options[1].Value != approval.DecisionReject {
			t.Fatalf("decision options = %#v", choice.Options)
		}
		if choice.Options[0].Label != "确认" || choice.Options[1].Label != "拒绝" {
			t.Fatalf("decision option labels = %#v", choice.Options)
		}
	}
	if decisionFields != 1 {
		t.Fatalf("decision field count = %d, want 1", decisionFields)
	}
}
