package storyboard

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

type StoryboardMutationCommand struct {
	CommandID        string `json:"command_id"`
	IdempotencyKey   string `json:"idempotency_key"`
	StoryboardID     string `json:"storyboard_id"`
	BaseVersion      int    `json:"base_version"`
	Actor            string `json:"actor,omitempty"`
	Source           string `json:"source"`
	TargetID         string `json:"target_id"`
	ExpectedRevision int    `json:"expected_revision,omitempty"`
	Field            string `json:"field"`
	Operation        string `json:"operation"`
	Value            any    `json:"value,omitempty"`
}

type UpdatePromptCommand struct {
	CommandID              string `json:"command_id"`
	StoryboardID           string `json:"storyboard_id"`
	BaseVersion            int    `json:"base_version"`
	TargetID               string `json:"target_id"`
	ExpectedTargetRevision int    `json:"expected_target_revision,omitempty"`
	Purpose                string `json:"purpose"`
	ExpectedRevision       int    `json:"expected_revision"`
	Prompt                 string `json:"prompt"`
	PromptRef              string `json:"prompt_ref,omitempty"`
	LockedByUser           bool   `json:"locked_by_user"`
}

type RegenerateAssetCommand struct {
	CommandID        string                       `json:"command_id"`
	StoryboardID     string                       `json:"storyboard_id"`
	BaseVersion      int                          `json:"base_version"`
	TargetID         string                       `json:"target_id"`
	AssetSlot        string                       `json:"asset_slot"`
	DispatchSnapshot RegenerationDispatchSnapshot `json:"dispatch_snapshot,omitempty"`
}

// RegenerationDispatchSnapshot freezes the exact accepted provider request at
// the same aggregate version that advances GenerationEpoch. It lets the HTTP
// saga create/recover its workflow after a crash without reading newer prompts
// or storyboard state.
type RegenerationDispatchSnapshot struct {
	Provider          string          `json:"provider"`
	MediaKind         string          `json:"media_kind"`
	UserID            string          `json:"user_id"`
	SpecVersion       int             `json:"spec_version"`
	StoryboardVersion int             `json:"storyboard_version"`
	EstimatedPoints   int64           `json:"estimated_points"`
	Input             GenerationInput `json:"input"`
	Payload           map[string]any  `json:"payload"`
}

type BindAssetCommand struct {
	CommandID        string `json:"command_id"`
	StoryboardID     string `json:"storyboard_id"`
	BaseVersion      int    `json:"base_version"`
	BindingID        string `json:"binding_id"`
	TargetID         string `json:"target_id"`
	AssetSlot        string `json:"asset_slot"`
	AssetID          string `json:"asset_id"`
	AttemptID        string `json:"attempt_id,omitempty"`
	ApprovalID       string `json:"approval_id,omitempty"`
	TargetRevision   int    `json:"target_revision"`
	PromptRevision   int    `json:"prompt_revision"`
	GenerationEpoch  int    `json:"generation_epoch"`
	InputFingerprint string `json:"input_fingerprint,omitempty"`
	Activate         bool   `json:"activate,omitempty"`
}

type ActivateBindingCommand struct {
	CommandID    string `json:"command_id"`
	StoryboardID string `json:"storyboard_id"`
	BaseVersion  int    `json:"base_version"`
	BindingID    string `json:"binding_id"`
}

type RejectBindingCommand struct {
	CommandID    string `json:"command_id"`
	StoryboardID string `json:"storyboard_id"`
	BaseVersion  int    `json:"base_version"`
	BindingID    string `json:"binding_id"`
}

type BindingDisposition string

const (
	BindingDispositionCandidate  BindingDisposition = "bound_candidate"
	BindingDispositionActive     BindingDisposition = "bound_active"
	BindingDispositionSuperseded BindingDisposition = "superseded"
)

func (a *StoryboardAggregate) ApplyMutation(command StoryboardMutationCommand) ([]string, error) {
	if command.StoryboardID != "" && command.StoryboardID != a.ID {
		return nil, fmt.Errorf("%w: storyboard id mismatch", ErrInvalidMutation)
	}
	fingerprint := commandFingerprint(command)
	if replay, err := a.checkAppliedCommand(command.CommandID, fingerprint); err != nil {
		return nil, err
	} else if replay {
		return nil, nil
	}
	if err := a.checkVersion(command.BaseVersion); err != nil {
		return nil, err
	}
	field, err := normalizeMutationField(command.Field)
	if err != nil {
		return nil, err
	}
	revision, err := a.mutableReviewRevision()
	if err != nil {
		return nil, err
	}
	if module, ok := findModule(revision, command.TargetID); ok {
		if command.ExpectedRevision > 0 && module.Revision != command.ExpectedRevision {
			return nil, fmt.Errorf("%w: current=%d expected=%d", ErrRevisionMismatch, module.Revision, command.ExpectedRevision)
		}
		generationChanged, err := mutateModule(module, field, command.Operation, command.Value)
		if err != nil {
			return nil, err
		}
		module.Revision++
		stale := []string{}
		if generationChanged {
			for i := range module.Elements {
				markElementStale(&module.Elements[i], "")
				stale = append(stale, module.Elements[i].ID)
				stale = append(stale, markDependencyClosure(revision, module.Elements[i].ID, "")...)
			}
			a.supersedeInvalidCandidates()
		}
		revision.UpdatedAt = nowUTC()
		a.touchCommand(command.CommandID, fingerprint)
		return uniqueSorted(stale), nil
	}

	element, ok := findElement(revision, command.TargetID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTargetNotFound, command.TargetID)
	}
	if command.ExpectedRevision > 0 && element.Revision != command.ExpectedRevision {
		return nil, fmt.Errorf("%w: current=%d expected=%d", ErrRevisionMismatch, element.Revision, command.ExpectedRevision)
	}
	generationChanged, err := mutateElement(element, field, command.Operation, command.Value)
	if err != nil {
		return nil, err
	}
	element.Revision++
	stale := []string{}
	if generationChanged {
		markElementStale(element, "")
		stale = append(stale, element.ID)
		stale = append(stale, markDependencyClosure(revision, element.ID, "")...)
		a.supersedeInvalidCandidates()
	}
	revision.UpdatedAt = nowUTC()
	a.touchCommand(command.CommandID, fingerprint)
	return uniqueSorted(stale), nil
}

// UpdatePrompt changes exactly one prompt slot. It does not increment the
// element revision, but it does invalidate the corresponding generation slot
// and its dependency closure.
func (a *StoryboardAggregate) UpdatePrompt(command UpdatePromptCommand) ([]string, error) {
	fingerprint := commandFingerprint(command)
	if replay, err := a.checkAppliedCommand(command.CommandID, fingerprint); err != nil {
		return nil, err
	} else if replay {
		return nil, nil
	}
	if err := a.checkVersion(command.BaseVersion); err != nil {
		return nil, err
	}
	revision, err := a.mutableReviewRevision()
	if err != nil {
		return nil, err
	}
	element, ok := findElement(revision, command.TargetID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTargetNotFound, command.TargetID)
	}
	if command.ExpectedTargetRevision > 0 && element.Revision != command.ExpectedTargetRevision {
		return nil, fmt.Errorf("%w: target current=%d expected=%d", ErrRevisionMismatch, element.Revision, command.ExpectedTargetRevision)
	}
	purpose := strings.TrimSpace(command.Purpose)
	for i := range element.PromptSlots {
		slot := &element.PromptSlots[i]
		if slot.Purpose != purpose {
			continue
		}
		if command.ExpectedRevision > 0 && slot.Revision != command.ExpectedRevision {
			return nil, fmt.Errorf("%w: prompt current=%d expected=%d", ErrRevisionMismatch, slot.Revision, command.ExpectedRevision)
		}
		slot.Prompt = strings.TrimSpace(command.Prompt)
		slot.PromptRef = strings.TrimSpace(command.PromptRef)
		if slot.Prompt == "" {
			slot.Status = PromptStatusMissing
		} else {
			slot.Status = PromptStatusReady
		}
		slot.LockedByUser = command.LockedByUser
		slot.Revision++
		markElementAssetSlotsStale(element, purpose)
		stale := append([]string{element.ID}, markDependencyClosure(revision, element.ID, purpose)...)
		a.supersedeInvalidCandidates()
		revision.UpdatedAt = nowUTC()
		a.touchCommand(command.CommandID, fingerprint)
		return uniqueSorted(stale), nil
	}
	return nil, fmt.Errorf("%w: prompt %s on target %s", ErrSlotNotFound, purpose, command.TargetID)
}

// RegenerateAsset increments only GenerationEpoch. Existing active assets stay
// active until a valid candidate is explicitly promoted.
func (a *StoryboardAggregate) RegenerateAsset(command RegenerateAssetCommand) (int, error) {
	fingerprint := commandFingerprint(command)
	if replay, err := a.checkAppliedCommand(command.CommandID, fingerprint); err != nil {
		return 0, err
	} else if replay {
		slot, _ := a.assetSlot(command.TargetID, command.AssetSlot)
		if slot == nil {
			return 0, ErrSlotNotFound
		}
		return slot.GenerationEpoch, nil
	}
	if err := a.checkVersion(command.BaseVersion); err != nil {
		return 0, err
	}
	slot, err := a.mutableAssetSlot(command.TargetID, command.AssetSlot)
	if err != nil {
		return 0, err
	}
	slot.GenerationEpoch++
	if snapshot := command.DispatchSnapshot; strings.TrimSpace(snapshot.Provider) != "" {
		input, resolveErr := a.ResolveGenerationInput(command.TargetID, command.AssetSlot)
		if resolveErr != nil || !reflect.DeepEqual(input, snapshot.Input) || snapshot.StoryboardVersion != a.Version+1 {
			return 0, fmt.Errorf("%w: regeneration dispatch snapshot does not match accepted input", ErrInvalidMutation)
		}
		active, activeErr := a.ActiveRevision()
		if activeErr != nil || snapshot.SpecVersion != active.DerivedFromSpecVersion || strings.TrimSpace(snapshot.MediaKind) != strings.TrimSpace(slot.MediaKind) || snapshot.Payload == nil {
			return 0, fmt.Errorf("%w: regeneration dispatch metadata does not match storyboard", ErrInvalidMutation)
		}
	}
	a.supersedeInvalidCandidates()
	a.touchCommand(command.CommandID, fingerprint)
	return slot.GenerationEpoch, nil
}

// BindAsset appends a candidate binding after verifying the complete semantic
// generation token. A stale worker result is retained as superseded history and
// can never overwrite the current slot.
func (a *StoryboardAggregate) BindAsset(command BindAssetCommand) (BindingDisposition, error) {
	fingerprint := commandFingerprint(command)
	if replay, err := a.checkAppliedCommand(command.CommandID, fingerprint); err != nil {
		return "", err
	} else if replay {
		for _, binding := range a.Bindings {
			if binding.ID == command.BindingID {
				return dispositionForState(binding.State), nil
			}
		}
		return "", ErrDuplicateCommand
	}
	if err := a.checkVersion(command.BaseVersion); err != nil {
		return "", err
	}
	if strings.TrimSpace(command.BindingID) == "" || strings.TrimSpace(command.AssetID) == "" {
		return "", fmt.Errorf("%w: binding_id and asset_id are required", ErrInvalidMutation)
	}
	for _, existing := range a.Bindings {
		if existing.ID == strings.TrimSpace(command.BindingID) {
			return "", fmt.Errorf("%w: binding id %s already exists", ErrInvalidMutation, command.BindingID)
		}
	}
	revision, err := a.mutableActiveRevision()
	if err != nil {
		return "", err
	}
	element, ok := findElement(revision, command.TargetID)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrTargetNotFound, command.TargetID)
	}
	slot := findAssetSlot(element, command.AssetSlot)
	if slot == nil {
		return "", fmt.Errorf("%w: asset %s", ErrSlotNotFound, command.AssetSlot)
	}
	promptRevision := promptRevisionForSlot(*element, *slot)
	currentInput, inputErr := a.ResolveGenerationInput(element.ID, slot.Key)
	inputResolvable := inputErr == nil || errors.Is(inputErr, ErrDependencyNotReady)
	valid := element.Revision == command.TargetRevision &&
		promptRevision == command.PromptRevision &&
		slot.GenerationEpoch == command.GenerationEpoch &&
		inputResolvable && strings.TrimSpace(command.InputFingerprint) != "" &&
		strings.TrimSpace(command.InputFingerprint) == currentInput.Fingerprint
	state := BindingStateCandidate
	disposition := BindingDispositionCandidate
	if !valid {
		state = BindingStateSuperseded
		disposition = BindingDispositionSuperseded
	}
	binding := ArtifactBinding{
		ID:               strings.TrimSpace(command.BindingID),
		StoryboardID:     a.ID,
		TargetID:         element.ID,
		AssetSlot:        slot.Key,
		AssetID:          strings.TrimSpace(command.AssetID),
		State:            state,
		ArtifactRevision: nextArtifactRevision(a.Bindings, element.ID, slot.Key),
		AttemptID:        strings.TrimSpace(command.AttemptID),
		ApprovalID:       strings.TrimSpace(command.ApprovalID),
		TargetRevision:   command.TargetRevision,
		PromptRevision:   command.PromptRevision,
		GenerationEpoch:  command.GenerationEpoch,
		InputFingerprint: strings.TrimSpace(command.InputFingerprint),
		CreatedAt:        nowUTC(),
	}
	a.Bindings = append(a.Bindings, binding)
	if valid {
		slot.CandidateIDs = appendUniqueString(slot.CandidateIDs, binding.ID)
		slot.Status = AssetSlotStatusCandidate
		if command.Activate && !slot.ReviewRequired {
			disposition, err = a.activateBinding(revision, binding.ID)
			if err != nil {
				return "", err
			}
			if disposition == BindingDispositionActive {
				markDependencyClosure(revision, binding.TargetID, binding.AssetSlot)
				a.supersedeInvalidCandidates()
			}
		}
	}
	a.touchCommand(command.CommandID, fingerprint)
	return disposition, nil
}

func (a *StoryboardAggregate) ActivateBinding(command ActivateBindingCommand) ([]string, error) {
	fingerprint := commandFingerprint(command)
	if replay, err := a.checkAppliedCommand(command.CommandID, fingerprint); err != nil {
		return nil, err
	} else if replay {
		return nil, nil
	}
	if err := a.checkVersion(command.BaseVersion); err != nil {
		return nil, err
	}
	revision, err := a.mutableActiveRevision()
	if err != nil {
		return nil, err
	}
	binding := a.bindingByID(command.BindingID)
	if binding == nil || binding.State != BindingStateCandidate {
		return nil, fmt.Errorf("%w: candidate binding %s", ErrInvalidMutation, command.BindingID)
	}
	disposition, err := a.activateBinding(revision, binding.ID)
	if err != nil {
		return nil, err
	}
	if disposition == BindingDispositionSuperseded {
		return nil, fmt.Errorf("%w: candidate binding input changed", ErrRevisionMismatch)
	}
	stale := markDependencyClosure(revision, binding.TargetID, binding.AssetSlot)
	a.supersedeInvalidCandidates()
	a.touchCommand(command.CommandID, fingerprint)
	return uniqueSorted(stale), nil
}

// RejectBinding deterministically removes a candidate from its slot without
// disturbing the currently active asset.
func (a *StoryboardAggregate) RejectBinding(command RejectBindingCommand) error {
	fingerprint := commandFingerprint(command)
	if replay, err := a.checkAppliedCommand(command.CommandID, fingerprint); err != nil {
		return err
	} else if replay {
		return nil
	}
	if err := a.checkVersion(command.BaseVersion); err != nil {
		return err
	}
	revision, err := a.mutableActiveRevision()
	if err != nil {
		return err
	}
	binding := a.bindingByID(command.BindingID)
	if binding == nil || binding.State != BindingStateCandidate {
		return fmt.Errorf("%w: candidate binding %s", ErrInvalidMutation, command.BindingID)
	}
	element, ok := findElement(revision, binding.TargetID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrTargetNotFound, binding.TargetID)
	}
	slot := findAssetSlot(element, binding.AssetSlot)
	if slot == nil {
		return fmt.Errorf("%w: %s", ErrSlotNotFound, binding.AssetSlot)
	}
	binding.State = BindingStateRejected
	slot.CandidateIDs = removeString(slot.CandidateIDs, binding.ID)
	if slot.ActiveBindingID != "" {
		slot.Status = AssetSlotStatusActive
	} else if len(slot.CandidateIDs) > 0 {
		slot.Status = AssetSlotStatusCandidate
	} else {
		slot.Status = AssetSlotStatusMissing
	}
	a.touchCommand(command.CommandID, fingerprint)
	return nil
}

func (a *StoryboardAggregate) activateBinding(revision *StoryboardRevision, bindingID string) (BindingDisposition, error) {
	binding := a.bindingByID(bindingID)
	if binding == nil {
		return "", fmt.Errorf("%w: binding %s", ErrInvalidMutation, bindingID)
	}
	element, ok := findElement(revision, binding.TargetID)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrTargetNotFound, binding.TargetID)
	}
	slot := findAssetSlot(element, binding.AssetSlot)
	if slot == nil {
		return "", fmt.Errorf("%w: %s", ErrSlotNotFound, binding.AssetSlot)
	}
	if !slices.Contains(slot.CandidateIDs, binding.ID) {
		return "", fmt.Errorf("%w: candidate binding %s is not attached to the active slot", ErrInvalidMutation, binding.ID)
	}
	currentInput, inputErr := a.ResolveGenerationInput(element.ID, slot.Key)
	inputResolvable := inputErr == nil || errors.Is(inputErr, ErrDependencyNotReady)
	if binding.TargetRevision != element.Revision || binding.PromptRevision != promptRevisionForSlot(*element, *slot) || binding.GenerationEpoch != slot.GenerationEpoch || !inputResolvable || binding.InputFingerprint == "" || binding.InputFingerprint != currentInput.Fingerprint {
		binding.State = BindingStateSuperseded
		return BindingDispositionSuperseded, nil
	}
	for i := range a.Bindings {
		other := &a.Bindings[i]
		if other.TargetID == binding.TargetID && other.AssetSlot == binding.AssetSlot && other.State == BindingStateActive {
			other.State = BindingStateSuperseded
		}
	}
	binding.State = BindingStateActive
	slot.ActiveBindingID = binding.ID
	slot.CandidateIDs = removeString(slot.CandidateIDs, binding.ID)
	slot.Status = AssetSlotStatusActive
	return BindingDispositionActive, nil
}

// supersedeInvalidCandidates prevents stale candidate IDs from blocking a
// slot after a prompt, target, epoch or upstream active asset changes.
func (a *StoryboardAggregate) supersedeInvalidCandidates() {
	revision, err := a.mutableActiveRevision()
	if err != nil {
		return
	}
	for index := range a.Bindings {
		binding := &a.Bindings[index]
		if binding.State != BindingStateCandidate {
			continue
		}
		current, resolveErr := a.ResolveGenerationInput(binding.TargetID, binding.AssetSlot)
		valid := resolveErr == nil &&
			binding.TargetRevision == current.TargetRevision &&
			binding.PromptRevision == current.PromptRevision &&
			binding.GenerationEpoch == current.GenerationEpoch &&
			strings.TrimSpace(binding.InputFingerprint) != "" &&
			binding.InputFingerprint == current.Fingerprint
		if valid {
			continue
		}
		binding.State = BindingStateSuperseded
		element, ok := findElement(revision, binding.TargetID)
		if !ok {
			continue
		}
		slot := findAssetSlot(element, binding.AssetSlot)
		if slot == nil {
			continue
		}
		slot.CandidateIDs = removeString(slot.CandidateIDs, binding.ID)
		switch {
		case slot.ActiveBindingID != "" && slot.Status != AssetSlotStatusStale:
			slot.Status = AssetSlotStatusActive
		case slot.ActiveBindingID == "" && slot.Status != AssetSlotStatusStale:
			slot.Status = AssetSlotStatusMissing
		}
	}
}

func (a *StoryboardAggregate) supersedeUnreferencedBindings(revision *StoryboardRevision) {
	if revision == nil {
		return
	}
	referenced := map[string]struct{}{}
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			for _, slot := range element.AssetSlots {
				if slot.ActiveBindingID != "" {
					referenced[slot.ActiveBindingID] = struct{}{}
				}
				for _, bindingID := range slot.CandidateIDs {
					referenced[bindingID] = struct{}{}
				}
			}
		}
	}
	for index := range a.Bindings {
		if a.Bindings[index].State != BindingStateCandidate && a.Bindings[index].State != BindingStateActive {
			continue
		}
		if _, exists := referenced[a.Bindings[index].ID]; !exists {
			a.Bindings[index].State = BindingStateSuperseded
		}
	}
}

func (a *StoryboardAggregate) mutableActiveRevision() (*StoryboardRevision, error) {
	if a.ActiveRevisionID == "" {
		return nil, ErrRevisionNotFound
	}
	for i := range a.Revisions {
		if a.Revisions[i].ID == a.ActiveRevisionID {
			return &a.Revisions[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrRevisionNotFound, a.ActiveRevisionID)
}

// mutableReviewRevision lets the UI edit prompts inside the pending plan the
// user is currently reviewing. Once no pending revision exists, edits target
// the active revision as usual.
func (a *StoryboardAggregate) mutableReviewRevision() (*StoryboardRevision, error) {
	id := a.PendingRevisionID
	if id == "" {
		id = a.ActiveRevisionID
	}
	if id == "" {
		return nil, ErrRevisionNotFound
	}
	for i := range a.Revisions {
		if a.Revisions[i].ID == id {
			return &a.Revisions[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrRevisionNotFound, id)
}

func (a *StoryboardAggregate) mutableAssetSlot(targetID, slotKey string) (*AssetSlot, error) {
	revision, err := a.mutableActiveRevision()
	if err != nil {
		return nil, err
	}
	element, ok := findElement(revision, targetID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTargetNotFound, targetID)
	}
	slot := findAssetSlot(element, slotKey)
	if slot == nil {
		return nil, fmt.Errorf("%w: %s", ErrSlotNotFound, slotKey)
	}
	return slot, nil
}

func (a StoryboardAggregate) assetSlot(targetID, slotKey string) (*AssetSlot, error) {
	revision, err := a.ActiveRevision()
	if err != nil {
		return nil, err
	}
	element, ok := findElement(revision, targetID)
	if !ok {
		return nil, ErrTargetNotFound
	}
	slot := findAssetSlot(element, slotKey)
	if slot == nil {
		return nil, ErrSlotNotFound
	}
	return slot, nil
}

func (a *StoryboardAggregate) bindingByID(id string) *ArtifactBinding {
	for i := range a.Bindings {
		if a.Bindings[i].ID == id {
			return &a.Bindings[i]
		}
	}
	return nil
}

func findModule(revision *StoryboardRevision, targetID string) (*StoryboardModule, bool) {
	for i := range revision.Modules {
		if revision.Modules[i].ID == targetID {
			return &revision.Modules[i], true
		}
	}
	return nil, false
}

func findElement(revision *StoryboardRevision, targetID string) (*StoryboardElement, bool) {
	for i := range revision.Modules {
		for j := range revision.Modules[i].Elements {
			if revision.Modules[i].Elements[j].ID == targetID {
				return &revision.Modules[i].Elements[j], true
			}
		}
	}
	return nil, false
}

func findAssetSlot(element *StoryboardElement, key string) *AssetSlot {
	for i := range element.AssetSlots {
		if element.AssetSlots[i].Key == key {
			return &element.AssetSlots[i]
		}
	}
	return nil
}

func normalizeMutationField(field string) (string, error) {
	field = strings.Trim(strings.TrimSpace(field), "/")
	field = strings.ReplaceAll(field, "/", ".")
	for _, part := range strings.Split(field, ".") {
		if part == "" {
			continue
		}
		if _, err := strconv.Atoi(part); err == nil || strings.ContainsAny(part, "[]") {
			return "", fmt.Errorf("%w: array-index paths are not accepted", ErrInvalidMutation)
		}
	}
	if field == "" {
		return "", fmt.Errorf("%w: field is required", ErrInvalidMutation)
	}
	return field, nil
}

func mutateModule(module *StoryboardModule, field, operation string, value any) (bool, error) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "set"
	}
	switch {
	case field == "title" && operation == "set":
		module.Title = fmt.Sprint(value)
		return true, nil
	case field == "description" && operation == "set":
		module.Description = fmt.Sprint(value)
		return true, nil
	case field == "order" && (operation == "set" || operation == "reorder"):
		order, ok := asInt(value)
		if !ok || order <= 0 {
			return false, fmt.Errorf("%w: order must be a positive integer", ErrInvalidMutation)
		}
		module.Order = order
		return true, nil
	case strings.HasPrefix(field, "metadata."):
		return false, mutateMap(&module.Metadata, strings.TrimPrefix(field, "metadata."), operation, value)
	default:
		return false, fmt.Errorf("%w: unsupported module field %s", ErrInvalidMutation, field)
	}
}

func mutateElement(element *StoryboardElement, field, operation string, value any) (bool, error) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "set"
	}
	switch {
	case field == "title" && operation == "set":
		element.Title = fmt.Sprint(value)
		return true, nil
	case field == "semantic_type" && operation == "set":
		element.SemanticType = fmt.Sprint(value)
		return true, nil
	case field == "review_state" && operation == "set":
		element.ReviewState = fmt.Sprint(value)
		return false, nil
	case strings.HasPrefix(field, "metadata."):
		return false, mutateMap(&element.Metadata, strings.TrimPrefix(field, "metadata."), operation, value)
	case strings.HasPrefix(field, "content."):
		return true, mutateMap(&element.Content, strings.TrimPrefix(field, "content."), operation, value)
	case field == "locked_fields":
		valueString := strings.TrimSpace(fmt.Sprint(value))
		switch operation {
		case "add_ref":
			element.LockedFields = appendUniqueString(element.LockedFields, valueString)
		case "remove_ref":
			element.LockedFields = removeString(element.LockedFields, valueString)
		default:
			return false, fmt.Errorf("%w: locked_fields supports add_ref/remove_ref", ErrInvalidMutation)
		}
		return false, nil
	default:
		return false, fmt.Errorf("%w: unsupported element field %s", ErrInvalidMutation, field)
	}
}

func mutateMap(target *map[string]any, key, operation string, value any) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%w: map key is required", ErrInvalidMutation)
	}
	if *target == nil {
		*target = map[string]any{}
	}
	switch operation {
	case "set":
		(*target)[key] = value
	case "unset":
		delete(*target, key)
	default:
		return fmt.Errorf("%w: map fields support set/unset", ErrInvalidMutation)
	}
	return nil
}

func markElementStale(element *StoryboardElement, slot string) {
	for i := range element.PromptSlots {
		if slot == "" || element.PromptSlots[i].Purpose == slot {
			element.PromptSlots[i].Status = PromptStatusStale
		}
	}
	markElementAssetSlotsStale(element, slot)
}

func markElementAssetSlotsStale(element *StoryboardElement, slot string) {
	for i := range element.AssetSlots {
		assetSlot := &element.AssetSlots[i]
		if slot == "" || assetSlot.Key == slot || assetSlot.Role == slot || assetSlot.MediaKind == slot || strings.Contains(assetSlot.Key, slot) {
			if assetSlot.ActiveBindingID != "" || len(assetSlot.CandidateIDs) > 0 {
				assetSlot.Status = AssetSlotStatusStale
			}
		}
	}
}

func markDependencyClosure(revision *StoryboardRevision, targetID, slot string) []string {
	type dependencyTarget struct{ id, slot string }
	queue := []dependencyTarget{{id: targetID, slot: slot}}
	seen := map[dependencyTarget]bool{queue[0]: true}
	stale := []string{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range revision.Dependencies {
			if edge.FromTargetID != current.id || (edge.FromSlot != "" && current.slot != "" && edge.FromSlot != current.slot) {
				continue
			}
			next := dependencyTarget{id: edge.ToTargetID, slot: edge.ToSlot}
			if seen[next] {
				continue
			}
			seen[next] = true
			if element, ok := findElement(revision, next.id); ok {
				markElementStale(element, next.slot)
				stale = append(stale, next.id)
			}
			queue = append(queue, next)
		}
	}
	return stale
}

func promptRevisionForSlot(element StoryboardElement, slot AssetSlot) int {
	// ResolveGenerationInput is the semantic source of truth for dispatch and
	// finalization. Reuse its prompt selection (including the single-prompt
	// fallback) so a dynamically worded purpose cannot be accepted at dispatch
	// and then rejected as stale while binding the exact same input.
	_, revision := generationPrompt(element, slot)
	return revision
}

func nextArtifactRevision(bindings []ArtifactBinding, targetID, slot string) int {
	maxRevision := 0
	for _, binding := range bindings {
		if binding.TargetID == targetID && binding.AssetSlot == slot && binding.ArtifactRevision > maxRevision {
			maxRevision = binding.ArtifactRevision
		}
	}
	return maxRevision + 1
}

func dispositionForState(state string) BindingDisposition {
	switch state {
	case BindingStateActive:
		return BindingDispositionActive
	case BindingStateCandidate:
		return BindingDispositionCandidate
	default:
		return BindingDispositionSuperseded
	}
}

func appendUniqueString(values []string, value string) []string {
	if value != "" && !slices.Contains(values, value) {
		return append(values, value)
	}
	return values
}

func removeString(values []string, value string) []string {
	return slices.DeleteFunc(values, func(existing string) bool { return existing == value })
}

func asInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), reflect.DeepEqual(typed, float64(int(typed)))
	default:
		return 0, false
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
