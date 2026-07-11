package storyboard

import (
	"context"
	"fmt"
	"strings"
)

type CreatePendingRevisionCommand struct {
	CommandID              string             `json:"command_id"`
	IdempotencyKey         string             `json:"idempotency_key,omitempty"`
	StoryboardID           string             `json:"storyboard_id"`
	BaseVersion            int                `json:"base_version"`
	Actor                  string             `json:"actor,omitempty"`
	Source                 string             `json:"source,omitempty"`
	Candidate              StoryboardRevision `json:"candidate"`
	PreserveApprovedAssets bool               `json:"preserve_approved_assets,omitempty"`
}

type DecidePendingRevisionCommand struct {
	CommandID      string `json:"command_id"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	StoryboardID   string `json:"storyboard_id"`
	BaseVersion    int    `json:"base_version"`
	RevisionID     string `json:"revision_id"`
	Decision       string `json:"decision"`
	Actor          string `json:"actor,omitempty"`
	Source         string `json:"source,omitempty"`
}

type CommandService struct {
	repository AggregateRepository
}

func NewCommandService(repository AggregateRepository) (*CommandService, error) {
	if repository == nil {
		return nil, fmt.Errorf("storyboard aggregate repository is required")
	}
	return &CommandService{repository: repository}, nil
}

func (s *CommandService) Create(ctx context.Context, storyboardID, sessionID string) (StoryboardAggregate, error) {
	aggregate, err := NewStoryboardAggregate(storyboardID, sessionID)
	if err != nil {
		return StoryboardAggregate{}, err
	}
	if err := s.repository.CreateAggregate(ctx, aggregate); err != nil {
		return StoryboardAggregate{}, err
	}
	return aggregate, nil
}

func (s *CommandService) CreatePending(ctx context.Context, command CreatePendingRevisionCommand) (StoryboardAggregate, RevisionDiff, error) {
	aggregate, duplicate, err := s.loadForCommand(ctx, command.StoryboardID, command.CommandID, commandFingerprint(command))
	if err != nil || duplicate {
		return aggregate, RevisionDiff{}, err
	}
	before := aggregate.Version
	diff, err := aggregate.CreatePendingRevision(command.CommandID, command.BaseVersion, command.Candidate, command.PreserveApprovedAssets)
	if err != nil {
		return StoryboardAggregate{}, RevisionDiff{}, err
	}
	aggregate.markAppliedCommand(command.CommandID, commandFingerprint(command))
	event := DomainEvent{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		Type:           "storyboard.revision_requested",
		Actor:          command.Actor,
		Source:         command.Source,
		Payload: map[string]any{
			"revision_id":   aggregate.PendingRevisionID,
			"plan_revision": aggregate.PlanRevision + 1,
			"diff":          diff,
		},
	}
	if err := s.repository.SaveAggregate(ctx, aggregate, before, event); err != nil {
		return StoryboardAggregate{}, RevisionDiff{}, err
	}
	return aggregate, diff, nil
}

func (s *CommandService) DecidePending(ctx context.Context, command DecidePendingRevisionCommand) (StoryboardAggregate, RevisionDiff, error) {
	aggregate, duplicate, err := s.loadForCommand(ctx, command.StoryboardID, command.CommandID, commandFingerprint(command))
	if err != nil || duplicate {
		return aggregate, RevisionDiff{}, err
	}
	before := aggregate.Version
	decision := strings.ToLower(strings.TrimSpace(command.Decision))
	diff := RevisionDiff{}
	eventType := ""
	switch decision {
	case "approved", "approve":
		diff, err = aggregate.PromotePendingRevision(command.CommandID, command.BaseVersion, command.RevisionID)
		eventType = "storyboard.revision_promoted"
	case "rejected", "reject":
		err = aggregate.RejectPendingRevision(command.CommandID, command.BaseVersion, command.RevisionID)
		eventType = "storyboard.revision_rejected"
	case "stale", "cancelled", "canceled":
		err = aggregate.RejectPendingRevision(command.CommandID, command.BaseVersion, command.RevisionID)
		eventType = "storyboard.revision_" + decision
	default:
		return StoryboardAggregate{}, RevisionDiff{}, fmt.Errorf("%w: unsupported decision %q", ErrInvalidMutation, decision)
	}
	if err != nil {
		return StoryboardAggregate{}, RevisionDiff{}, err
	}
	aggregate.markAppliedCommand(command.CommandID, commandFingerprint(command))
	event := DomainEvent{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		Type:           eventType,
		Actor:          command.Actor,
		Source:         command.Source,
		Payload: map[string]any{
			"revision_id": command.RevisionID,
			"decision":    decision,
			"diff":        diff,
		},
	}
	if err := s.repository.SaveAggregate(ctx, aggregate, before, event); err != nil {
		return StoryboardAggregate{}, RevisionDiff{}, err
	}
	return aggregate, diff, nil
}

func (s *CommandService) Mutate(ctx context.Context, command StoryboardMutationCommand) (StoryboardAggregate, []string, error) {
	aggregate, duplicate, err := s.loadForCommand(ctx, command.StoryboardID, command.CommandID, commandFingerprint(command))
	if err != nil || duplicate {
		return aggregate, nil, err
	}
	before := aggregate.Version
	stale, err := aggregate.ApplyMutation(command)
	if err != nil {
		return StoryboardAggregate{}, nil, err
	}
	event := DomainEvent{CommandID: command.CommandID, IdempotencyKey: command.IdempotencyKey, Type: "storyboard.target_updated", Actor: command.Actor, Source: command.Source, Payload: map[string]any{
		"target_id": command.TargetID, "field": command.Field, "operation": command.Operation, "stale_targets": stale,
	}}
	if err := s.repository.SaveAggregate(ctx, aggregate, before, event); err != nil {
		return StoryboardAggregate{}, nil, err
	}
	return aggregate, stale, nil
}

func (s *CommandService) UpdatePrompt(ctx context.Context, command UpdatePromptCommand) (StoryboardAggregate, []string, error) {
	aggregate, duplicate, err := s.loadForCommand(ctx, command.StoryboardID, command.CommandID, commandFingerprint(command))
	if err != nil || duplicate {
		return aggregate, nil, err
	}
	before := aggregate.Version
	stale, err := aggregate.UpdatePrompt(command)
	if err != nil {
		return StoryboardAggregate{}, nil, err
	}
	event := DomainEvent{CommandID: command.CommandID, Type: "storyboard.prompt_updated", Source: "user", Payload: map[string]any{
		"target_id": command.TargetID, "purpose": command.Purpose, "stale_targets": stale,
	}}
	if err := s.repository.SaveAggregate(ctx, aggregate, before, event); err != nil {
		return StoryboardAggregate{}, nil, err
	}
	return aggregate, stale, nil
}

func (s *CommandService) Regenerate(ctx context.Context, command RegenerateAssetCommand) (StoryboardAggregate, int, error) {
	aggregate, duplicate, err := s.loadForCommand(ctx, command.StoryboardID, command.CommandID, commandFingerprint(command))
	if err != nil || duplicate {
		return aggregate, 0, err
	}
	before := aggregate.Version
	epoch, err := aggregate.RegenerateAsset(command)
	if err != nil {
		return StoryboardAggregate{}, 0, err
	}
	event := DomainEvent{CommandID: command.CommandID, Type: "storyboard.regeneration_requested", Source: "user", Payload: map[string]any{
		"target_id": command.TargetID, "asset_slot": command.AssetSlot, "generation_epoch": epoch, "dispatch_snapshot": command.DispatchSnapshot,
	}}
	if err := s.repository.SaveAggregate(ctx, aggregate, before, event); err != nil {
		return StoryboardAggregate{}, 0, err
	}
	return aggregate, epoch, nil
}

func (s *CommandService) Bind(ctx context.Context, command BindAssetCommand) (StoryboardAggregate, BindingDisposition, error) {
	aggregate, duplicate, err := s.loadForCommand(ctx, command.StoryboardID, command.CommandID, commandFingerprint(command))
	if err != nil {
		return aggregate, "", err
	}
	if duplicate {
		for _, binding := range aggregate.Bindings {
			if binding.ID == command.BindingID {
				return aggregate, dispositionForState(binding.State), nil
			}
		}
		return aggregate, "", ErrDuplicateCommand
	}
	before := aggregate.Version
	disposition, err := aggregate.BindAsset(command)
	if err != nil {
		return StoryboardAggregate{}, "", err
	}
	event := DomainEvent{CommandID: command.CommandID, Type: "storyboard.asset_bound", Source: "worker", Payload: map[string]any{
		"target_id": command.TargetID, "asset_slot": command.AssetSlot, "asset_id": command.AssetID, "binding_id": command.BindingID, "disposition": disposition,
	}}
	if err := s.repository.SaveAggregate(ctx, aggregate, before, event); err != nil {
		return StoryboardAggregate{}, "", err
	}
	return aggregate, disposition, nil
}

func (s *CommandService) Activate(ctx context.Context, command ActivateBindingCommand) (StoryboardAggregate, []string, error) {
	aggregate, duplicate, err := s.loadForCommand(ctx, command.StoryboardID, command.CommandID, commandFingerprint(command))
	if err != nil || duplicate {
		return aggregate, nil, err
	}
	before := aggregate.Version
	stale, err := aggregate.ActivateBinding(command)
	if err != nil {
		return StoryboardAggregate{}, nil, err
	}
	event := DomainEvent{CommandID: command.CommandID, Type: "storyboard.asset_activated", Source: "approval", Payload: map[string]any{
		"binding_id": command.BindingID, "stale_targets": stale,
	}}
	if err := s.repository.SaveAggregate(ctx, aggregate, before, event); err != nil {
		return StoryboardAggregate{}, nil, err
	}
	return aggregate, stale, nil
}

func (s *CommandService) RejectBinding(ctx context.Context, command RejectBindingCommand) (StoryboardAggregate, error) {
	aggregate, duplicate, err := s.loadForCommand(ctx, command.StoryboardID, command.CommandID, commandFingerprint(command))
	if err != nil || duplicate {
		return aggregate, err
	}
	before := aggregate.Version
	if err := aggregate.RejectBinding(command); err != nil {
		return StoryboardAggregate{}, err
	}
	event := DomainEvent{CommandID: command.CommandID, Type: "storyboard.asset_rejected", Source: "approval", Payload: map[string]any{
		"binding_id": command.BindingID,
	}}
	if err := s.repository.SaveAggregate(ctx, aggregate, before, event); err != nil {
		return StoryboardAggregate{}, err
	}
	return aggregate, nil
}

func (s *CommandService) loadForCommand(ctx context.Context, storyboardID, commandID, fingerprint string) (StoryboardAggregate, bool, error) {
	if strings.TrimSpace(storyboardID) == "" {
		return StoryboardAggregate{}, false, fmt.Errorf("storyboard id is required")
	}
	aggregate, err := s.repository.GetAggregate(ctx, storyboardID)
	if err != nil {
		return StoryboardAggregate{}, false, err
	}
	duplicate, err := aggregate.checkAppliedCommand(commandID, fingerprint)
	return aggregate, duplicate, err
}
