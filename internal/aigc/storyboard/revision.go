package storyboard

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

var supportedAssetSlotMediaKinds = []string{
	"image", "illustration", "keyframe", "video",
	"audio", "music", "voice", "text", "script", "lyrics",
}

// SupportedAssetSlotMediaKinds returns the closed Storyboard protocol set.
// The first seven kinds can be generated; text/script/lyrics are satisfied by
// an explicit user asset and must never be mistaken for a Provider job.
func SupportedAssetSlotMediaKinds() []string {
	return append([]string(nil), supportedAssetSlotMediaKinds...)
}

func isSupportedAssetSlotMediaKind(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, supported := range supportedAssetSlotMediaKinds {
		if value == supported {
			return true
		}
	}
	return false
}

type RevisionDiff struct {
	AddedTargets               []string `json:"added_targets,omitempty"`
	UpdatedTargets             []string `json:"updated_targets,omitempty"`
	ReusedTargets              []string `json:"reused_targets,omitempty"`
	ArchivedTargets            []string `json:"archived_targets,omitempty"`
	ReusableAssets             []string `json:"reusable_assets,omitempty"`
	ReusableBindingIDs         []string `json:"reusable_binding_ids,omitempty"`
	StaleOutputsAfterPromotion []string `json:"stale_outputs_after_promotion,omitempty"`
}

// CreatePendingRevision installs a complete candidate without changing the
// active revision. Whole-plan approval is represented by PromotePendingRevision.
func (a *StoryboardAggregate) CreatePendingRevision(commandID string, baseVersion int, candidate StoryboardRevision, preserveApprovedAssets bool) (RevisionDiff, error) {
	fingerprint := commandFingerprint(struct {
		CommandID   string
		BaseVersion int
		Candidate   StoryboardRevision
		Preserve    bool
	}{commandID, baseVersion, candidate, preserveApprovedAssets})
	if replay, err := a.checkAppliedCommand(commandID, fingerprint); err != nil {
		return RevisionDiff{}, err
	} else if replay {
		return RevisionDiff{}, nil
	}
	if err := a.checkVersion(baseVersion); err != nil {
		return RevisionDiff{}, err
	}
	if strings.TrimSpace(a.PendingRevisionID) != "" {
		return RevisionDiff{}, ErrPendingRevision
	}

	var active *StoryboardRevision
	if a.ActiveRevisionID != "" {
		for i := range a.Revisions {
			if a.Revisions[i].ID == a.ActiveRevisionID {
				active = &a.Revisions[i]
				break
			}
		}
	}
	candidate.StoryboardID = a.ID
	if strings.TrimSpace(candidate.ID) == "" {
		candidate.ID = fmt.Sprintf("%s:revision:%d", a.ID, a.PlanRevision+1)
	}
	if active != nil {
		candidate.BaseRevisionID = active.ID
	}
	candidate.Status = RevisionStatusReviewing
	now := time.Now().UTC()
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = now
	}
	candidate.UpdatedAt = now

	normalized, diff, err := ReconcileRevision(active, candidate, preserveApprovedAssets)
	if err != nil {
		return RevisionDiff{}, err
	}
	if err := ValidateRevision(normalized); err != nil {
		return RevisionDiff{}, err
	}
	normalized.PreserveApprovedAssets = preserveApprovedAssets
	a.enrichReusableAssets(&diff)
	a.Revisions = append(a.Revisions, normalized)
	a.PendingRevisionID = normalized.ID
	a.Status = AggregateStatusReviewing
	a.touchCommand(commandID, fingerprint)
	return diff, nil
}

func (a *StoryboardAggregate) PromotePendingRevision(commandID string, baseVersion int, revisionID string) (RevisionDiff, error) {
	fingerprint := commandFingerprint(struct {
		CommandID   string
		BaseVersion int
		RevisionID  string
	}{commandID, baseVersion, revisionID})
	if replay, err := a.checkAppliedCommand(commandID, fingerprint); err != nil {
		return RevisionDiff{}, err
	} else if replay {
		return RevisionDiff{}, nil
	}
	if err := a.checkVersion(baseVersion); err != nil {
		return RevisionDiff{}, err
	}
	if a.PendingRevisionID == "" {
		return RevisionDiff{}, ErrNoPendingRevision
	}
	if revisionID = strings.TrimSpace(revisionID); revisionID != "" && revisionID != a.PendingRevisionID {
		return RevisionDiff{}, fmt.Errorf("%w: pending=%s requested=%s", ErrRevisionMismatch, a.PendingRevisionID, revisionID)
	}

	var previous *StoryboardRevision
	var pending *StoryboardRevision
	for i := range a.Revisions {
		revision := &a.Revisions[i]
		switch revision.ID {
		case a.ActiveRevisionID:
			previous = revision
		case a.PendingRevisionID:
			pending = revision
		}
	}
	if pending == nil {
		return RevisionDiff{}, fmt.Errorf("%w: %s", ErrRevisionNotFound, a.PendingRevisionID)
	}
	// A reviewed plan can coexist with jobs finishing against the old active
	// revision. Rebase newly approved, generation-compatible bindings at the
	// promotion CAS boundary so assets adopted during review are not lost.
	if previous != nil && pending.PreserveApprovedAssets {
		rebaseApprovedAssets(*previous, pending)
	}
	if previous != nil {
		previous.Status = RevisionStatusSuperseded
		previous.UpdatedAt = time.Now().UTC()
	}
	pending.Status = RevisionStatusActive
	pending.UpdatedAt = time.Now().UTC()

	diff := ComputeRevisionDiff(previous, *pending)
	a.enrichReusableAssets(&diff)
	a.ActiveRevisionID = pending.ID
	a.PendingRevisionID = ""
	a.PlanRevision++
	a.Status = AggregateStatusActive
	a.supersedeUnreferencedBindings(pending)
	a.touchCommand(commandID, fingerprint)
	return diff, nil
}

func (a *StoryboardAggregate) RejectPendingRevision(commandID string, baseVersion int, revisionID string) error {
	fingerprint := commandFingerprint(struct {
		CommandID   string
		BaseVersion int
		RevisionID  string
	}{commandID, baseVersion, revisionID})
	if replay, err := a.checkAppliedCommand(commandID, fingerprint); err != nil {
		return err
	} else if replay {
		return nil
	}
	if err := a.checkVersion(baseVersion); err != nil {
		return err
	}
	if a.PendingRevisionID == "" {
		return ErrNoPendingRevision
	}
	if revisionID = strings.TrimSpace(revisionID); revisionID != "" && revisionID != a.PendingRevisionID {
		return fmt.Errorf("%w: pending=%s requested=%s", ErrRevisionMismatch, a.PendingRevisionID, revisionID)
	}
	for i := range a.Revisions {
		if a.Revisions[i].ID == a.PendingRevisionID {
			a.Revisions[i].Status = RevisionStatusRejected
			a.Revisions[i].UpdatedAt = time.Now().UTC()
			a.PendingRevisionID = ""
			if a.ActiveRevisionID != "" {
				a.Status = AggregateStatusActive
			} else {
				a.Status = AggregateStatusDraft
			}
			a.touchCommand(commandID, fingerprint)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrRevisionNotFound, a.PendingRevisionID)
}

func (a StoryboardAggregate) checkVersion(baseVersion int) error {
	if baseVersion != a.Version {
		return fmt.Errorf("%w: current=%d expected=%d", ErrVersionConflict, a.Version, baseVersion)
	}
	return nil
}

// ReconcileRevision normalizes dynamic module/element IDs and reuses stable IDs
// by semantic keys. User-locked prompts are never silently overwritten.
func ReconcileRevision(active *StoryboardRevision, candidate StoryboardRevision, preserveApprovedAssets bool) (StoryboardRevision, RevisionDiff, error) {
	oldModules := map[string]StoryboardModule{}
	oldElements := map[string]StoryboardElement{}
	if active != nil {
		for _, module := range active.Modules {
			oldModules[strings.TrimSpace(module.Key)] = module
			for _, element := range module.Elements {
				oldElements[elementMatchKey(module.Key, element.Key)] = element
			}
		}
	}

	usedIDs := map[string]bool{}
	targetAliases := map[string]string{}
	ambiguousAliases := map[string]bool{}
	addAlias := func(alias, targetID string) {
		alias = strings.TrimSpace(alias)
		if alias == "" || ambiguousAliases[alias] {
			return
		}
		if existing, ok := targetAliases[alias]; ok && existing != targetID {
			delete(targetAliases, alias)
			ambiguousAliases[alias] = true
			return
		}
		targetAliases[alias] = targetID
	}
	for moduleIndex := range candidate.Modules {
		module := &candidate.Modules[moduleIndex]
		originalModuleID, originalModuleKey := module.ID, module.Key
		module.Key = stableKey(module.Key, module.SemanticType, moduleIndex+1)
		oldModule, moduleExists := oldModules[module.Key]
		if moduleExists {
			module.ID = oldModule.ID
		}
		if strings.TrimSpace(module.ID) == "" {
			module.ID = uniqueStableID("module_"+module.Key, usedIDs)
		} else {
			module.ID = uniqueStableID(module.ID, usedIDs)
		}
		addAlias(originalModuleID, module.ID)
		addAlias(originalModuleKey, module.ID)
		addAlias(module.ID, module.ID)
		addAlias(module.Key, module.ID)
		if module.Order <= 0 {
			module.Order = moduleIndex + 1
		}
		if module.PlannedCount <= 0 {
			module.PlannedCount = len(module.Elements)
		}
		if module.Revision <= 0 {
			module.Revision = 1
		}

		for elementIndex := range module.Elements {
			element := &module.Elements[elementIndex]
			originalElementID, originalElementKey := element.ID, element.Key
			element.Key = stableKey(element.Key, element.SemanticType, elementIndex+1)
			oldElement, elementExists := oldElements[elementMatchKey(module.Key, element.Key)]
			if elementExists {
				element.ID = oldElement.ID
				reconcileLockedPrompts(element, oldElement)
			}
			if strings.TrimSpace(element.ID) == "" {
				element.ID = uniqueStableID("element_"+module.Key+"_"+element.Key, usedIDs)
			} else {
				element.ID = uniqueStableID(element.ID, usedIDs)
			}
			addAlias(originalElementID, element.ID)
			addAlias(originalElementKey, element.ID)
			addAlias(originalModuleKey+"/"+originalElementKey, element.ID)
			addAlias(module.Key+"/"+element.Key, element.ID)
			addAlias(element.ID, element.ID)
			addAlias(element.Key, element.ID)
			for i := range element.PromptSlots {
				element.PromptSlots[i] = normalizePromptSlot(element.PromptSlots[i])
			}
			for i := range element.AssetSlots {
				// Runtime binding state is authoritative and must never be accepted
				// from an LLM-produced candidate revision. Candidate review policy is
				// also deterministic: generated storyboard media always enters review;
				// only a later trusted business command may activate it.
				element.AssetSlots[i].ActiveBindingID = ""
				element.AssetSlots[i].CandidateIDs = nil
				element.AssetSlots[i].GenerationEpoch = 0
				element.AssetSlots[i].Status = AssetSlotStatusMissing
				element.AssetSlots[i].ReviewRequired = true
				element.AssetSlots[i] = normalizeAssetSlot(element.AssetSlots[i])
			}
			if elementExists && preserveApprovedAssets {
				reconcileAssetSlots(element, oldElement, generationCompatible(oldElement, *element))
			}
			if element.Revision <= 0 {
				element.Revision = 1
			}
			if elementExists && !sameElementPlan(oldElement, *element) {
				element.Revision = max(oldElement.Revision+1, element.Revision)
			} else if elementExists {
				element.Revision = oldElement.Revision
			}
		}
		if moduleExists && !sameModulePlan(oldModule, *module) {
			module.Revision = max(oldModule.Revision+1, module.Revision)
		} else if moduleExists {
			module.Revision = oldModule.Revision
		}
	}
	for index := range candidate.Dependencies {
		edge := &candidate.Dependencies[index]
		if resolved := targetAliases[strings.TrimSpace(edge.FromTargetID)]; resolved != "" {
			edge.FromTargetID = resolved
		}
		if resolved := targetAliases[strings.TrimSpace(edge.ToTargetID)]; resolved != "" {
			edge.ToTargetID = resolved
		}
	}
	if active != nil && preserveApprovedAssets {
		invalidateDependencyIncompatibleSlots(*active, &candidate)
	}
	candidate.Modules = sortedModules(candidate.Modules)
	diff := ComputeRevisionDiff(active, candidate)
	return candidate, diff, nil
}

func ValidateRevision(revision StoryboardRevision) error {
	if strings.TrimSpace(revision.ID) == "" {
		return fmt.Errorf("storyboard revision id is required")
	}
	if strings.TrimSpace(revision.StoryboardID) == "" {
		return fmt.Errorf("storyboard id is required")
	}
	if len(revision.Modules) == 0 {
		return fmt.Errorf("storyboard modules are required")
	}
	targets := map[string]StoryboardElement{}
	targetIDs := map[string]bool{}
	moduleKeys := map[string]bool{}
	for _, module := range revision.Modules {
		if module.ID == "" || module.Key == "" || module.SemanticType == "" {
			return fmt.Errorf("module id, key, and semantic_type are required")
		}
		if moduleKeys[module.Key] || targetIDs[module.ID] {
			return fmt.Errorf("duplicate module key or target id: %s", module.Key)
		}
		moduleKeys[module.Key] = true
		targetIDs[module.ID] = true
		if module.PlannedCount != len(module.Elements) {
			return fmt.Errorf("module %s planned_count=%d does not match elements=%d", module.ID, module.PlannedCount, len(module.Elements))
		}
		elementKeys := map[string]bool{}
		for _, element := range module.Elements {
			if element.ID == "" || element.Key == "" || element.SemanticType == "" {
				return fmt.Errorf("element id, key, and semantic_type are required in module %s", module.ID)
			}
			if elementKeys[element.Key] || targetIDs[element.ID] {
				return fmt.Errorf("duplicate element key or target id: %s", element.Key)
			}
			elementKeys[element.Key] = true
			targetIDs[element.ID] = true
			targets[element.ID] = element
			if err := validateSlots(element); err != nil {
				return err
			}
		}
	}
	for _, edge := range revision.Dependencies {
		from, fromOK := targets[edge.FromTargetID]
		to, toOK := targets[edge.ToTargetID]
		if !fromOK || !toOK {
			return fmt.Errorf("dependency references unknown target: %s -> %s", edge.FromTargetID, edge.ToTargetID)
		}
		if edge.FromTargetID == edge.ToTargetID {
			return fmt.Errorf("dependency cannot reference the same target: %s", edge.FromTargetID)
		}
		if edge.FromSlot != "" && !hasAssetSlot(from, edge.FromSlot) {
			return fmt.Errorf("dependency references unknown source slot: %s:%s", edge.FromTargetID, edge.FromSlot)
		}
		if edge.ToSlot != "" && !hasAssetSlot(to, edge.ToSlot) {
			return fmt.Errorf("dependency references unknown destination slot: %s:%s", edge.ToTargetID, edge.ToSlot)
		}
	}
	if err := validateDependencyDAG(revision.Dependencies, targets); err != nil {
		return err
	}
	return nil
}

func hasAssetSlot(element StoryboardElement, key string) bool {
	for _, slot := range element.AssetSlots {
		if slot.Key == key {
			return true
		}
	}
	return false
}

func validateDependencyDAG(edges []DependencyEdge, targets map[string]StoryboardElement) error {
	indegree := make(map[string]int, len(targets))
	adjacency := make(map[string][]string, len(targets))
	seenEdges := map[string]struct{}{}
	for id := range targets {
		indegree[id] = 0
	}
	for _, edge := range edges {
		key := strings.Join([]string{edge.FromTargetID, edge.FromSlot, edge.ToTargetID, edge.ToSlot, edge.Relation}, "\x00")
		if _, duplicate := seenEdges[key]; duplicate {
			return fmt.Errorf("duplicate dependency: %s -> %s", edge.FromTargetID, edge.ToTargetID)
		}
		seenEdges[key] = struct{}{}
		adjacency[edge.FromTargetID] = append(adjacency[edge.FromTargetID], edge.ToTargetID)
		indegree[edge.ToTargetID]++
	}
	queue := make([]string, 0, len(indegree))
	for id, degree := range indegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adjacency[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(targets) {
		return fmt.Errorf("storyboard dependencies must form a DAG")
	}
	return nil
}

func validateSlots(element StoryboardElement) error {
	purposes := map[string]bool{}
	for _, slot := range element.PromptSlots {
		if slot.Purpose == "" || purposes[slot.Purpose] {
			return fmt.Errorf("element %s has empty or duplicate prompt purpose %q", element.ID, slot.Purpose)
		}
		purposes[slot.Purpose] = true
	}
	keys := map[string]bool{}
	for _, slot := range element.AssetSlots {
		if slot.Key == "" || slot.MediaKind == "" || keys[slot.Key] {
			return fmt.Errorf("element %s has invalid or duplicate asset slot %q", element.ID, slot.Key)
		}
		if !isSupportedAssetSlotMediaKind(slot.MediaKind) {
			return fmt.Errorf("element %s asset slot %s has unsupported media_kind %q", element.ID, slot.Key, slot.MediaKind)
		}
		keys[slot.Key] = true
	}
	return nil
}

func ComputeRevisionDiff(active *StoryboardRevision, candidate StoryboardRevision) RevisionDiff {
	old := revisionTargets(active)
	next := revisionTargets(&candidate)
	diff := RevisionDiff{}
	for id, nextTarget := range next {
		oldTarget, ok := old[id]
		switch {
		case !ok:
			diff.AddedTargets = append(diff.AddedTargets, id)
		case sameElementPlan(oldTarget, nextTarget):
			diff.ReusedTargets = append(diff.ReusedTargets, id)
			diff.ReusableBindingIDs = append(diff.ReusableBindingIDs, activeBindingIDs(oldTarget)...)
		default:
			diff.UpdatedTargets = append(diff.UpdatedTargets, id)
			diff.StaleOutputsAfterPromotion = append(diff.StaleOutputsAfterPromotion, id)
		}
	}
	for id := range old {
		if _, ok := next[id]; !ok {
			diff.ArchivedTargets = append(diff.ArchivedTargets, id)
			diff.StaleOutputsAfterPromotion = append(diff.StaleOutputsAfterPromotion, id)
		}
	}
	sort.Strings(diff.AddedTargets)
	sort.Strings(diff.UpdatedTargets)
	sort.Strings(diff.ReusedTargets)
	sort.Strings(diff.ArchivedTargets)
	diff.ReusableBindingIDs = uniqueSorted(diff.ReusableBindingIDs)
	diff.StaleOutputsAfterPromotion = uniqueSorted(diff.StaleOutputsAfterPromotion)
	return diff
}

func revisionTargets(revision *StoryboardRevision) map[string]StoryboardElement {
	out := map[string]StoryboardElement{}
	if revision == nil {
		return out
	}
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			out[element.ID] = element
		}
	}
	return out
}

func reconcileLockedPrompts(candidate *StoryboardElement, active StoryboardElement) {
	old := map[string]PromptSlot{}
	for _, slot := range active.PromptSlots {
		old[slot.Purpose] = slot
	}
	seen := map[string]bool{}
	for i := range candidate.PromptSlots {
		purpose := strings.TrimSpace(candidate.PromptSlots[i].Purpose)
		seen[purpose] = true
		if previous, ok := old[purpose]; ok && previous.LockedByUser {
			candidate.PromptSlots[i] = previous
		}
	}
	for purpose, previous := range old {
		if previous.LockedByUser && !seen[purpose] {
			candidate.PromptSlots = append(candidate.PromptSlots, previous)
		}
	}
}

func reconcileAssetSlots(candidate *StoryboardElement, active StoryboardElement, compatible bool) {
	old := map[string]AssetSlot{}
	for _, slot := range active.AssetSlots {
		old[slot.Key] = slot
	}
	for i := range candidate.AssetSlots {
		previous, ok := old[candidate.AssetSlots[i].Key]
		if !ok {
			continue
		}
		candidate.AssetSlots[i].CandidateIDs = nil
		if !compatible {
			candidate.AssetSlots[i].ActiveBindingID = ""
			candidate.AssetSlots[i].GenerationEpoch = max(candidate.AssetSlots[i].GenerationEpoch, previous.GenerationEpoch+1)
			if previous.ActiveBindingID != "" || previous.Status == AssetSlotStatusStale {
				candidate.AssetSlots[i].Status = AssetSlotStatusStale
			} else {
				candidate.AssetSlots[i].Status = AssetSlotStatusMissing
			}
			continue
		}
		candidate.AssetSlots[i].GenerationEpoch = max(candidate.AssetSlots[i].GenerationEpoch, previous.GenerationEpoch)
		if previous.Status != AssetSlotStatusActive || previous.ActiveBindingID == "" {
			candidate.AssetSlots[i].ActiveBindingID = ""
			if previous.Status == AssetSlotStatusStale {
				candidate.AssetSlots[i].Status = AssetSlotStatusStale
			} else {
				candidate.AssetSlots[i].Status = AssetSlotStatusMissing
			}
			continue
		}
		candidate.AssetSlots[i].ActiveBindingID = previous.ActiveBindingID
		// preserveApprovedAssets only carries the active, approved binding. Old
		// unreviewed candidates remain historical and are superseded on promote.
		candidate.AssetSlots[i].Status = AssetSlotStatusActive
	}
}

func rebaseApprovedAssets(active StoryboardRevision, candidate *StoryboardRevision) {
	activeTargets := revisionTargets(&active)
	for moduleIndex := range candidate.Modules {
		for elementIndex := range candidate.Modules[moduleIndex].Elements {
			element := &candidate.Modules[moduleIndex].Elements[elementIndex]
			previous, exists := activeTargets[element.ID]
			if !exists {
				continue
			}
			reconcileAssetSlots(element, previous, generationCompatible(previous, *element))
		}
	}
	invalidateDependencyIncompatibleSlots(active, candidate)
}

func invalidateDependencyIncompatibleSlots(active StoryboardRevision, candidate *StoryboardRevision) {
	activeTargets := revisionTargets(&active)
	for moduleIndex := range candidate.Modules {
		for elementIndex := range candidate.Modules[moduleIndex].Elements {
			element := &candidate.Modules[moduleIndex].Elements[elementIndex]
			previous, exists := activeTargets[element.ID]
			if !exists {
				continue
			}
			previousSlots := map[string]AssetSlot{}
			for _, slot := range previous.AssetSlots {
				previousSlots[slot.Key] = slot
			}
			for slotIndex := range element.AssetSlots {
				slot := &element.AssetSlots[slotIndex]
				previousSlot, exists := previousSlots[slot.Key]
				if !exists || slot.ActiveBindingID == "" {
					continue
				}
				if reflect.DeepEqual(dependencySignature(active.Dependencies, element.ID, slot.Key), dependencySignature(candidate.Dependencies, element.ID, slot.Key)) {
					continue
				}
				slot.ActiveBindingID = ""
				slot.CandidateIDs = nil
				slot.GenerationEpoch = max(slot.GenerationEpoch, previousSlot.GenerationEpoch+1)
				slot.Status = AssetSlotStatusStale
			}
		}
	}
}

func dependencySignature(edges []DependencyEdge, targetID, slotKey string) []string {
	signature := make([]string, 0)
	for _, edge := range edges {
		if edge.ToTargetID != targetID || (edge.ToSlot != "" && edge.ToSlot != slotKey) {
			continue
		}
		signature = append(signature, strings.Join([]string{edge.FromTargetID, edge.FromSlot, edge.ToTargetID, edge.ToSlot, edge.Relation}, "\x00"))
	}
	sort.Strings(signature)
	return signature
}

func generationCompatible(active, candidate StoryboardElement) bool {
	if active.SemanticType != candidate.SemanticType || !reflect.DeepEqual(active.Content, candidate.Content) {
		return false
	}
	type promptShape struct {
		Purpose   string
		Prompt    string
		PromptRef string
	}
	activePrompts := make([]promptShape, 0, len(active.PromptSlots))
	candidatePrompts := make([]promptShape, 0, len(candidate.PromptSlots))
	for _, prompt := range active.PromptSlots {
		activePrompts = append(activePrompts, promptShape{prompt.Purpose, prompt.Prompt, prompt.PromptRef})
	}
	for _, prompt := range candidate.PromptSlots {
		candidatePrompts = append(candidatePrompts, promptShape{prompt.Purpose, prompt.Prompt, prompt.PromptRef})
	}
	if !reflect.DeepEqual(activePrompts, candidatePrompts) {
		return false
	}
	type assetShape struct {
		Key, Role, MediaKind     string
		Required, ReviewRequired bool
	}
	activeAssets := make([]assetShape, 0, len(active.AssetSlots))
	candidateAssets := make([]assetShape, 0, len(candidate.AssetSlots))
	for _, slot := range active.AssetSlots {
		activeAssets = append(activeAssets, assetShape{slot.Key, slot.Role, slot.MediaKind, slot.Required, slot.ReviewRequired})
	}
	for _, slot := range candidate.AssetSlots {
		candidateAssets = append(candidateAssets, assetShape{slot.Key, slot.Role, slot.MediaKind, slot.Required, slot.ReviewRequired})
	}
	return reflect.DeepEqual(activeAssets, candidateAssets)
}

func sameModulePlan(a, b StoryboardModule) bool {
	a.ID, b.ID = "", ""
	a.Revision, b.Revision = 0, 0
	a.Elements, b.Elements = nil, nil
	return reflect.DeepEqual(a, b)
}

func sameElementPlan(a, b StoryboardElement) bool {
	a.ID, b.ID = "", ""
	a.Revision, b.Revision = 0, 0
	return reflect.DeepEqual(a, b)
}

func activeBindingIDs(element StoryboardElement) []string {
	bindings := make([]string, 0, len(element.AssetSlots))
	for _, slot := range element.AssetSlots {
		if slot.ActiveBindingID != "" {
			bindings = append(bindings, slot.ActiveBindingID)
		}
	}
	return bindings
}

func (a StoryboardAggregate) enrichReusableAssets(diff *RevisionDiff) {
	if diff == nil || len(diff.ReusableBindingIDs) == 0 {
		return
	}
	assetsByBinding := map[string]string{}
	for _, binding := range a.Bindings {
		if binding.State == BindingStateActive && binding.AssetID != "" {
			assetsByBinding[binding.ID] = binding.AssetID
		}
	}
	for _, bindingID := range diff.ReusableBindingIDs {
		if assetID := assetsByBinding[bindingID]; assetID != "" {
			diff.ReusableAssets = append(diff.ReusableAssets, assetID)
		}
	}
	diff.ReusableAssets = uniqueSorted(diff.ReusableAssets)
}

func elementMatchKey(moduleKey, elementKey string) string {
	return strings.TrimSpace(moduleKey) + "\x00" + strings.TrimSpace(elementKey)
}

func stableKey(key, semanticType string, index int) string {
	if key = normalizeDynamicID(key); key != "" {
		return key
	}
	key = normalizeDynamicID(semanticType)
	if key == "" {
		key = "item"
	}
	return fmt.Sprintf("%s_%d", key, index)
}

func uniqueStableID(value string, used map[string]bool) string {
	base := normalizeDynamicID(value)
	if base == "" {
		base = "target"
	}
	value = base
	for suffix := 2; used[value]; suffix++ {
		value = fmt.Sprintf("%s_%d", base, suffix)
	}
	used[value] = true
	return value
}

func normalizeDynamicID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	underscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			underscore = false
		default:
			if b.Len() > 0 && !underscore {
				b.WriteByte('_')
				underscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func sortedModules(modules []StoryboardModule) []StoryboardModule {
	sort.SliceStable(modules, func(i, j int) bool {
		if modules[i].Order == modules[j].Order {
			return modules[i].ID < modules[j].ID
		}
		return modules[i].Order < modules[j].Order
	})
	return modules
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
