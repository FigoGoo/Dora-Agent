package storyboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type GenerationInput struct {
	TargetID        string   `json:"target_id"`
	AssetSlot       string   `json:"asset_slot"`
	TargetRevision  int      `json:"target_revision"`
	Prompt          string   `json:"prompt"`
	PromptRevision  int      `json:"prompt_revision"`
	GenerationEpoch int      `json:"generation_epoch"`
	InputAssetIDs   []string `json:"input_asset_ids,omitempty"`
	Fingerprint     string   `json:"fingerprint"`
}

// ResolveGenerationInput is the single semantic source for dispatch, worker
// finalization and approval-time candidate validation.
func (a StoryboardAggregate) ResolveGenerationInput(targetID, slotKey string) (GenerationInput, error) {
	revision, err := a.ActiveRevision()
	if err != nil {
		return GenerationInput{}, err
	}
	element, ok := findElement(revision, strings.TrimSpace(targetID))
	if !ok {
		return GenerationInput{}, fmt.Errorf("%w: %s", ErrTargetNotFound, targetID)
	}
	slot := findAssetSlot(element, strings.TrimSpace(slotKey))
	if slot == nil {
		return GenerationInput{}, fmt.Errorf("%w: %s", ErrSlotNotFound, slotKey)
	}
	prompt, promptRevision := generationPrompt(*element, *slot)
	inputAssets, dependencyErr := a.resolveDependencyAssets(*revision, element.ID, slot.Key)
	dependencyState := "ready"
	if dependencyErr != nil {
		dependencyState = dependencyErr.Error()
	}
	raw, _ := json.Marshal([]any{
		a.ID, revision.DerivedFromSpecVersion, revision.DerivedFromAnalysisVersion, revision.Scenario,
		element.ID, element.Revision, slot.Key, slot.GenerationEpoch, prompt, promptRevision,
		inputAssets, dependencyState,
	})
	sum := sha256.Sum256(raw)
	input := GenerationInput{TargetID: element.ID, AssetSlot: slot.Key, TargetRevision: element.Revision, Prompt: prompt, PromptRevision: promptRevision, GenerationEpoch: slot.GenerationEpoch, InputAssetIDs: inputAssets, Fingerprint: hex.EncodeToString(sum[:])}
	return input, dependencyErr
}

func (a StoryboardAggregate) resolveDependencyAssets(revision StoryboardRevision, targetID, slotKey string) ([]string, error) {
	elements := map[string]StoryboardElement{}
	bindingAssets := map[string]string{}
	for _, binding := range a.Bindings {
		if binding.State == BindingStateActive {
			bindingAssets[binding.ID] = binding.AssetID
		}
	}
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			elements[element.ID] = element
		}
	}
	assets := make([]string, 0)
	seen := map[string]struct{}{}
	appendAsset := func(bindingID string) {
		assetID := bindingAssets[bindingID]
		if assetID == "" {
			return
		}
		if _, exists := seen[assetID]; exists {
			return
		}
		seen[assetID] = struct{}{}
		assets = append(assets, assetID)
	}
	for _, edge := range revision.Dependencies {
		if edge.ToTargetID != targetID || (edge.ToSlot != "" && edge.ToSlot != slotKey) {
			continue
		}
		upstream, ok := elements[edge.FromTargetID]
		if !ok {
			return nil, fmt.Errorf("%w: missing upstream target %s", ErrDependencyNotReady, edge.FromTargetID)
		}
		if edge.FromSlot != "" {
			found := false
			for _, upstreamSlot := range upstream.AssetSlots {
				if upstreamSlot.Key == edge.FromSlot && upstreamSlot.Status == AssetSlotStatusActive && upstreamSlot.ActiveBindingID != "" {
					found = true
					appendAsset(upstreamSlot.ActiveBindingID)
				}
			}
			if !found {
				return nil, fmt.Errorf("%w: %s:%s", ErrDependencyNotReady, edge.FromTargetID, edge.FromSlot)
			}
			continue
		}
		for _, upstreamSlot := range upstream.AssetSlots {
			if upstreamSlot.Required && (upstreamSlot.Status != AssetSlotStatusActive || upstreamSlot.ActiveBindingID == "") {
				return nil, fmt.Errorf("%w: %s:%s", ErrDependencyNotReady, edge.FromTargetID, upstreamSlot.Key)
			}
			appendAsset(upstreamSlot.ActiveBindingID)
		}
	}
	return assets, nil
}

func generationPrompt(element StoryboardElement, slot AssetSlot) (string, int) {
	for _, prompt := range element.PromptSlots {
		if prompt.Purpose == slot.Key || prompt.Purpose == slot.Role || prompt.Purpose == slot.MediaKind || strings.Contains(slot.Key, prompt.Purpose) {
			return prompt.Prompt, prompt.Revision
		}
	}
	if len(element.PromptSlots) == 1 {
		return element.PromptSlots[0].Prompt, element.PromptSlots[0].Revision
	}
	return "", 0
}
