package storyboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	AggregateStatusDraft     = "draft"
	AggregateStatusReviewing = "reviewing"
	AggregateStatusActive    = "active"

	RevisionStatusDraft      = "draft"
	RevisionStatusReviewing  = "reviewing"
	RevisionStatusActive     = "active"
	RevisionStatusRejected   = "rejected"
	RevisionStatusSuperseded = "superseded"
	RevisionStatusArchived   = "archived"

	PromptStatusMissing    = "missing"
	PromptStatusGenerating = "generating"
	PromptStatusReviewing  = "reviewing"
	PromptStatusReady      = "ready"
	PromptStatusStale      = "stale"
	PromptStatusFailed     = "failed"

	AssetSlotStatusMissing   = "missing"
	AssetSlotStatusCandidate = "candidate"
	AssetSlotStatusActive    = "active"
	AssetSlotStatusStale     = "stale"
	AssetSlotStatusFailed    = "failed"

	BindingStateCandidate  = "candidate"
	BindingStateActive     = "active"
	BindingStateRejected   = "rejected"
	BindingStateSuperseded = "superseded"
)

var (
	ErrAggregateNotFound   = errors.New("storyboard aggregate not found")
	ErrTargetNotFound      = errors.New("storyboard target not found")
	ErrSlotNotFound        = errors.New("storyboard slot not found")
	ErrRevisionNotFound    = errors.New("storyboard revision not found")
	ErrPendingRevision     = errors.New("storyboard already has a pending revision")
	ErrNoPendingRevision   = errors.New("storyboard has no pending revision")
	ErrRevisionMismatch    = errors.New("storyboard target revision mismatch")
	ErrInvalidMutation     = errors.New("invalid storyboard mutation")
	ErrIdempotencyConflict = errors.New("storyboard command idempotency conflict")
	ErrDependencyNotReady  = errors.New("storyboard dependency is not ready")
)

// StoryboardAggregate is the versioned root for the dynamic storyboard model.
// Version changes for every persisted domain mutation; PlanRevision changes only
// when a complete pending plan is promoted.
type StoryboardAggregate struct {
	ID                   string               `json:"id"`
	SessionID            string               `json:"session_id"`
	Version              int                  `json:"version"`
	PlanRevision         int                  `json:"plan_revision"`
	ActiveRevisionID     string               `json:"active_revision_id,omitempty"`
	PendingRevisionID    string               `json:"pending_revision_id,omitempty"`
	Status               string               `json:"status"`
	Revisions            []StoryboardRevision `json:"revisions,omitempty"`
	Bindings             []ArtifactBinding    `json:"bindings,omitempty"`
	AppliedCommandIDs    []string             `json:"applied_command_ids,omitempty"`
	AppliedCommandHashes map[string]string    `json:"applied_command_hashes,omitempty"`
	CreatedAt            time.Time            `json:"created_at,omitempty"`
	UpdatedAt            time.Time            `json:"updated_at,omitempty"`
}

// PublicView removes the internal command-id ledger while preserving the
// versioned storyboard state required by clients and A2UI projections.
func (a StoryboardAggregate) PublicView() StoryboardAggregate {
	view := a.Clone()
	view.AppliedCommandIDs = nil
	view.AppliedCommandHashes = nil
	return view
}

type StoryboardRevision struct {
	ID                         string             `json:"id"`
	StoryboardID               string             `json:"storyboard_id"`
	BaseRevisionID             string             `json:"base_revision_id,omitempty"`
	DerivedFromSpecVersion     int                `json:"derived_from_spec_version,omitempty"`
	DerivedFromAnalysisVersion int                `json:"derived_from_analysis_version,omitempty"`
	PreserveApprovedAssets     bool               `json:"preserve_approved_assets,omitempty"`
	Status                     string             `json:"status"`
	Scenario                   string             `json:"scenario,omitempty"`
	Modules                    []StoryboardModule `json:"modules,omitempty"`
	Dependencies               []DependencyEdge   `json:"dependencies,omitempty"`
	CreatedAt                  time.Time          `json:"created_at,omitempty"`
	UpdatedAt                  time.Time          `json:"updated_at,omitempty"`
}

type StoryboardModule struct {
	ID             string              `json:"id"`
	Key            string              `json:"key"`
	SemanticType   string              `json:"semantic_type"`
	Title          string              `json:"title"`
	Description    string              `json:"description,omitempty"`
	Order          int                 `json:"order"`
	PlannedCount   int                 `json:"planned_count"`
	Required       bool                `json:"required"`
	ReviewRequired bool                `json:"review_required"`
	Revision       int                 `json:"revision"`
	Capabilities   ModuleCapabilities  `json:"capabilities"`
	Elements       []StoryboardElement `json:"elements,omitempty"`
	Metadata       map[string]any      `json:"metadata,omitempty"`
}

type ModuleCapabilities struct {
	HasQuantity    bool   `json:"has_quantity"`
	RequiresPrompt bool   `json:"requires_prompt"`
	RequiresAsset  bool   `json:"requires_asset"`
	HasTimeline    bool   `json:"has_timeline"`
	OutputModality string `json:"output_modality,omitempty"`
}

type StoryboardElement struct {
	ID           string         `json:"id"`
	Key          string         `json:"key"`
	SemanticType string         `json:"semantic_type"`
	Title        string         `json:"title"`
	Revision     int            `json:"revision"`
	Content      map[string]any `json:"content,omitempty"`
	LockedFields []string       `json:"locked_fields,omitempty"`
	PromptSlots  []PromptSlot   `json:"prompt_slots,omitempty"`
	AssetSlots   []AssetSlot    `json:"asset_slots,omitempty"`
	ReviewState  string         `json:"review_state,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type PromptSlot struct {
	Purpose      string `json:"purpose"`
	Prompt       string `json:"prompt,omitempty"`
	PromptRef    string `json:"prompt_ref,omitempty"`
	Revision     int    `json:"revision"`
	Status       string `json:"status"`
	LockedByUser bool   `json:"locked_by_user,omitempty"`
}

type AssetSlot struct {
	Key             string   `json:"key"`
	Role            string   `json:"role,omitempty"`
	MediaKind       string   `json:"media_kind"`
	Required        bool     `json:"required"`
	ReviewRequired  bool     `json:"review_required,omitempty"`
	GenerationEpoch int      `json:"generation_epoch"`
	ActiveBindingID string   `json:"active_binding_id,omitempty"`
	CandidateIDs    []string `json:"candidate_ids,omitempty"`
	Status          string   `json:"status"`
}

type ArtifactBinding struct {
	ID               string    `json:"id"`
	StoryboardID     string    `json:"storyboard_id"`
	TargetID         string    `json:"target_id"`
	AssetSlot        string    `json:"asset_slot"`
	AssetID          string    `json:"asset_id"`
	State            string    `json:"state"`
	ArtifactRevision int       `json:"artifact_revision"`
	AttemptID        string    `json:"attempt_id,omitempty"`
	ApprovalID       string    `json:"approval_id,omitempty"`
	TargetRevision   int       `json:"target_revision"`
	PromptRevision   int       `json:"prompt_revision"`
	GenerationEpoch  int       `json:"generation_epoch"`
	InputFingerprint string    `json:"input_fingerprint,omitempty"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
}

type DependencyEdge struct {
	FromTargetID string `json:"from_target_id"`
	FromSlot     string `json:"from_slot,omitempty"`
	ToTargetID   string `json:"to_target_id"`
	ToSlot       string `json:"to_slot,omitempty"`
	Relation     string `json:"relation,omitempty"`
}

func NewStoryboardAggregate(id, sessionID string) (StoryboardAggregate, error) {
	id = strings.TrimSpace(id)
	sessionID = strings.TrimSpace(sessionID)
	if id == "" {
		return StoryboardAggregate{}, fmt.Errorf("storyboard id is required")
	}
	if sessionID == "" {
		return StoryboardAggregate{}, fmt.Errorf("session id is required")
	}
	now := time.Now().UTC()
	return StoryboardAggregate{
		ID:        id,
		SessionID: sessionID,
		Status:    AggregateStatusDraft,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (a StoryboardAggregate) Clone() StoryboardAggregate {
	raw, _ := json.Marshal(a)
	var clone StoryboardAggregate
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func (a StoryboardAggregate) ActiveRevision() (*StoryboardRevision, error) {
	return a.revisionByID(a.ActiveRevisionID)
}

func (a StoryboardAggregate) PendingRevision() (*StoryboardRevision, error) {
	return a.revisionByID(a.PendingRevisionID)
}

func (a StoryboardAggregate) revisionByID(id string) (*StoryboardRevision, error) {
	id = strings.TrimSpace(id)
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

func (a StoryboardAggregate) HasApplied(commandID string) bool {
	commandID = strings.TrimSpace(commandID)
	return commandID != "" && slices.Contains(a.AppliedCommandIDs, commandID)
}

func (a StoryboardAggregate) checkAppliedCommand(commandID, fingerprint string) (bool, error) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return false, nil
	}
	if existing, ok := a.AppliedCommandHashes[commandID]; ok {
		if existing != fingerprint {
			return false, fmt.Errorf("%w: command %s", ErrIdempotencyConflict, commandID)
		}
		return true, nil
	}
	// Backward-compatible replay for aggregates persisted before hashes existed.
	return slices.Contains(a.AppliedCommandIDs, commandID), nil
}

func commandFingerprint(command any) string {
	raw, _ := json.Marshal(command)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (a *StoryboardAggregate) markApplied(commandID string) {
	commandID = strings.TrimSpace(commandID)
	if commandID != "" && !slices.Contains(a.AppliedCommandIDs, commandID) {
		a.AppliedCommandIDs = append(a.AppliedCommandIDs, commandID)
	}
}

func (a *StoryboardAggregate) markAppliedCommand(commandID, fingerprint string) {
	a.markApplied(commandID)
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return
	}
	if a.AppliedCommandHashes == nil {
		a.AppliedCommandHashes = map[string]string{}
	}
	a.AppliedCommandHashes[commandID] = fingerprint
}

func (a *StoryboardAggregate) touchCommand(commandID, fingerprint string) {
	a.Version++
	a.UpdatedAt = time.Now().UTC()
	a.markAppliedCommand(commandID, fingerprint)
}

func (a *StoryboardAggregate) touch(commandID string) {
	a.Version++
	a.UpdatedAt = time.Now().UTC()
	a.markApplied(commandID)
}

func normalizePromptSlot(slot PromptSlot) PromptSlot {
	slot.Purpose = strings.TrimSpace(slot.Purpose)
	slot.Prompt = strings.TrimSpace(slot.Prompt)
	slot.PromptRef = strings.TrimSpace(slot.PromptRef)
	if slot.Revision <= 0 {
		slot.Revision = 1
	}
	if strings.TrimSpace(slot.Status) == "" {
		if slot.Prompt == "" {
			slot.Status = PromptStatusMissing
		} else {
			slot.Status = PromptStatusReady
		}
	}
	return slot
}

func normalizeAssetSlot(slot AssetSlot) AssetSlot {
	slot.Key = strings.TrimSpace(slot.Key)
	slot.Role = strings.TrimSpace(slot.Role)
	slot.MediaKind = strings.ToLower(strings.TrimSpace(slot.MediaKind))
	if slot.GenerationEpoch < 0 {
		slot.GenerationEpoch = 0
	}
	if strings.TrimSpace(slot.Status) == "" {
		if slot.ActiveBindingID != "" {
			slot.Status = AssetSlotStatusActive
		} else if len(slot.CandidateIDs) > 0 {
			slot.Status = AssetSlotStatusCandidate
		} else {
			slot.Status = AssetSlotStatusMissing
		}
	}
	return slot
}
