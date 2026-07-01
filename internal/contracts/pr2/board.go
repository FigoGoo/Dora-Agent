package pr2

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

const (
	SchemaVersionCreativeBoard   = "creative_board.v1"
	SchemaVersionCreativeElement = "creative_element.v1"
	SchemaVersionBoardPatch      = "board_patch.v1"
	SchemaVersionBoardSnapshot   = "board_snapshot.v1"
)

const (
	BoardElementTypeStoryScene          = "story_scene"
	BoardElementTypeStoryboardFrame     = "storyboard_frame"
	BoardElementTypePromptBlock         = "prompt_block"
	BoardElementTypeReferenceAsset      = "reference_asset"
	BoardElementTypeTextNote            = "text_note"
	BoardElementTypeToolSlot            = "tool_slot"
	BoardElementTypeSkillRecommendation = "skill_recommendation"
)

const (
	BoardElementSourceUser          = "user"
	BoardElementSourceAgent         = "agent"
	BoardElementSourceGraph         = "graph"
	BoardElementSourceTool          = "tool"
	BoardElementSourceImportedAsset = "imported_asset"
)

const (
	BoardElementStatusDraft    = "draft"
	BoardElementStatusReady    = "ready"
	BoardElementStatusApproved = "approved"
	BoardElementStatusArchived = "archived"
)

const (
	BoardPatchOperationAddElement      = "add_element"
	BoardPatchOperationUpdateElement   = "update_element"
	BoardPatchOperationRemoveElement   = "remove_element"
	BoardPatchOperationReorderElements = "reorder_elements"
	BoardPatchOperationReplaceBoard    = "replace_board"
	BoardPatchOperationApproveBoard    = "approve_board"
)

const (
	BoardPatchActorUser  = "user"
	BoardPatchActorAgent = "agent"
	BoardPatchActorGraph = "graph"
)

type CreativeBoard struct {
	SchemaVersion   string     `json:"schema_version"`
	BoardID         string     `json:"board_id"`
	ProjectID       string     `json:"project_id"`
	SessionID       string     `json:"session_id"`
	RunID           string     `json:"run_id"`
	GraphPlanID     *string    `json:"graph_plan_id,omitempty"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	Version         int        `json:"version"`
	ElementsCount   int        `json:"elements_count"`
	ApprovedAt      *time.Time `json:"approved_at"`
	ApprovedBy      *string    `json:"approved_by"`
	ToolPlanAllowed bool       `json:"tool_plan_allowed"`
	BoardDigest     string     `json:"board_digest"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CreativeElement struct {
	SchemaVersion  string          `json:"schema_version"`
	ElementID      string          `json:"element_id"`
	BoardID        string          `json:"board_id"`
	ElementType    string          `json:"element_type"`
	Source         string          `json:"source"`
	Status         string          `json:"status"`
	Position       ElementPosition `json:"position"`
	Content        map[string]any  `json:"content"`
	LinkedAssetIDs []string        `json:"linked_asset_ids"`
	ContentDigest  string          `json:"content_digest"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type ElementPosition struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Order  int     `json:"order"`
}

type BoardPatch struct {
	SchemaVersion  string         `json:"schema_version"`
	PatchID        string         `json:"patch_id"`
	BoardID        string         `json:"board_id"`
	BaseVersion    int            `json:"base_version"`
	TargetVersion  int            `json:"target_version"`
	Operation      string         `json:"operation"`
	Actor          string         `json:"actor"`
	IdempotencyKey string         `json:"idempotency_key"`
	Payload        map[string]any `json:"payload"`
	PatchDigest    string         `json:"patch_digest"`
	CreatedAt      time.Time      `json:"created_at"`
}

type BoardSnapshot struct {
	SchemaVersion string            `json:"schema_version"`
	BoardID       string            `json:"board_id"`
	Version       int               `json:"version"`
	Status        string            `json:"status"`
	LastPatchID   *string           `json:"last_patch_id"`
	Elements      []CreativeElement `json:"elements"`
	BoardDigest   string            `json:"board_digest"`
	CreatedAt     time.Time         `json:"created_at"`
}

func ValidateCreativeBoard(board CreativeBoard) error {
	if board.SchemaVersion != SchemaVersionCreativeBoard {
		return fmt.Errorf("schema_version must be %s", SchemaVersionCreativeBoard)
	}
	if err := validatePrefixID(board.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if strings.TrimSpace(board.ProjectID) == "" || strings.TrimSpace(board.SessionID) == "" || strings.TrimSpace(board.RunID) == "" {
		return errors.New("project_id, session_id and run_id are required")
	}
	if board.GraphPlanID != nil {
		if err := validatePrefixID(*board.GraphPlanID, "gplan_"); err != nil {
			return fmt.Errorf("graph_plan_id: %w", err)
		}
	}
	if title := strings.TrimSpace(board.Title); title == "" || len([]rune(title)) > 128 {
		return fmt.Errorf("invalid title %q", board.Title)
	}
	if !pr1.IsValidState(pr1.StateBoardStatus, board.Status) {
		return fmt.Errorf("invalid board status %q", board.Status)
	}
	if board.Version < 1 || board.ElementsCount < 0 {
		return errors.New("version must be >= 1 and elements_count must be >= 0")
	}
	if board.Status == "approved" {
		if board.ApprovedAt == nil || board.ApprovedBy == nil || strings.TrimSpace(*board.ApprovedBy) == "" {
			return errors.New("approved board requires approved_at and approved_by")
		}
		if !board.ToolPlanAllowed {
			return errors.New("approved board must allow tool plan")
		}
	}
	if err := pr1.ValidateDigest(board.BoardDigest); err != nil {
		return fmt.Errorf("board_digest: %w", err)
	}
	if board.CreatedAt.IsZero() || board.UpdatedAt.IsZero() {
		return errors.New("created_at and updated_at are required")
	}
	if board.UpdatedAt.Before(board.CreatedAt) {
		return errors.New("updated_at must not be before created_at")
	}
	return nil
}

func ValidateCreativeElement(element CreativeElement) error {
	if element.SchemaVersion != SchemaVersionCreativeElement {
		return fmt.Errorf("schema_version must be %s", SchemaVersionCreativeElement)
	}
	if err := validatePrefixID(element.ElementID, "elem_"); err != nil {
		return fmt.Errorf("element_id: %w", err)
	}
	if err := validatePrefixID(element.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if !isAllowed(element.ElementType, []string{
		BoardElementTypeStoryScene,
		BoardElementTypeStoryboardFrame,
		BoardElementTypePromptBlock,
		BoardElementTypeReferenceAsset,
		BoardElementTypeTextNote,
		BoardElementTypeToolSlot,
		BoardElementTypeSkillRecommendation,
	}) {
		return fmt.Errorf("invalid element_type %q", element.ElementType)
	}
	if !isAllowed(element.Source, []string{
		BoardElementSourceUser,
		BoardElementSourceAgent,
		BoardElementSourceGraph,
		BoardElementSourceTool,
		BoardElementSourceImportedAsset,
	}) {
		return fmt.Errorf("invalid source %q", element.Source)
	}
	if !isAllowed(element.Status, []string{
		BoardElementStatusDraft,
		BoardElementStatusReady,
		BoardElementStatusApproved,
		BoardElementStatusArchived,
	}) {
		return fmt.Errorf("invalid element status %q", element.Status)
	}
	if element.Position.Width < 1 || element.Position.Height < 1 || element.Position.Order < 1 {
		return errors.New("position width, height and order must be positive")
	}
	if element.Content == nil {
		return errors.New("content is required")
	}
	for _, assetID := range element.LinkedAssetIDs {
		if strings.TrimSpace(assetID) == "" {
			return errors.New("linked_asset_ids cannot contain empty values")
		}
	}
	if err := pr1.ValidateDigest(element.ContentDigest); err != nil {
		return fmt.Errorf("content_digest: %w", err)
	}
	if element.CreatedAt.IsZero() || element.UpdatedAt.IsZero() {
		return errors.New("created_at and updated_at are required")
	}
	if element.UpdatedAt.Before(element.CreatedAt) {
		return errors.New("updated_at must not be before created_at")
	}
	return nil
}

func ValidateBoardPatch(patch BoardPatch) error {
	if patch.SchemaVersion != SchemaVersionBoardPatch {
		return fmt.Errorf("schema_version must be %s", SchemaVersionBoardPatch)
	}
	if err := validatePrefixID(patch.PatchID, "patch_"); err != nil {
		return fmt.Errorf("patch_id: %w", err)
	}
	if err := validatePrefixID(patch.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if patch.BaseVersion < 0 || patch.TargetVersion < 1 {
		return errors.New("base_version must be >= 0 and target_version must be >= 1")
	}
	if patch.TargetVersion != patch.BaseVersion+1 {
		return errors.New("target_version must equal base_version + 1")
	}
	if !isAllowed(patch.Operation, []string{
		BoardPatchOperationAddElement,
		BoardPatchOperationUpdateElement,
		BoardPatchOperationRemoveElement,
		BoardPatchOperationReorderElements,
		BoardPatchOperationReplaceBoard,
		BoardPatchOperationApproveBoard,
	}) {
		return fmt.Errorf("invalid operation %q", patch.Operation)
	}
	if !isAllowed(patch.Actor, []string{BoardPatchActorUser, BoardPatchActorAgent, BoardPatchActorGraph}) {
		return fmt.Errorf("invalid actor %q", patch.Actor)
	}
	if strings.TrimSpace(patch.IdempotencyKey) == "" || len(patch.IdempotencyKey) > 160 {
		return errors.New("idempotency_key is required and must be <= 160 characters")
	}
	if patch.Payload == nil {
		return errors.New("payload is required")
	}
	if err := pr1.ValidateDigest(patch.PatchDigest); err != nil {
		return fmt.Errorf("patch_digest: %w", err)
	}
	if patch.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	return nil
}

func ValidateBoardSnapshot(snapshot BoardSnapshot) error {
	if snapshot.SchemaVersion != SchemaVersionBoardSnapshot {
		return fmt.Errorf("schema_version must be %s", SchemaVersionBoardSnapshot)
	}
	if err := validatePrefixID(snapshot.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if snapshot.Version < 1 {
		return errors.New("version must be >= 1")
	}
	if !pr1.IsValidState(pr1.StateBoardStatus, snapshot.Status) {
		return fmt.Errorf("invalid board status %q", snapshot.Status)
	}
	if snapshot.LastPatchID != nil {
		if err := validatePrefixID(*snapshot.LastPatchID, "patch_"); err != nil {
			return fmt.Errorf("last_patch_id: %w", err)
		}
	}
	for index, element := range snapshot.Elements {
		if element.BoardID != snapshot.BoardID {
			return fmt.Errorf("element %d belongs to board %q, expected %q", index+1, element.BoardID, snapshot.BoardID)
		}
		if err := ValidateCreativeElement(element); err != nil {
			return fmt.Errorf("element %d: %w", index+1, err)
		}
	}
	if err := pr1.ValidateDigest(snapshot.BoardDigest); err != nil {
		return fmt.Errorf("board_digest: %w", err)
	}
	if snapshot.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	return nil
}

func ValidateBoardCreation(board CreativeBoard, elements []CreativeElement) error {
	if err := ValidateCreativeBoard(board); err != nil {
		return err
	}
	if board.ElementsCount != len(elements) {
		return fmt.Errorf("elements_count=%d does not match elements length %d", board.ElementsCount, len(elements))
	}
	if board.Status != "ready" {
		return fmt.Errorf("created board must be ready, got %q", board.Status)
	}
	if board.ToolPlanAllowed {
		return errors.New("created board must not allow tool plan before approval")
	}
	for index, element := range elements {
		if element.BoardID != board.BoardID {
			return fmt.Errorf("element %d belongs to board %q, expected %q", index+1, element.BoardID, board.BoardID)
		}
		if err := ValidateCreativeElement(element); err != nil {
			return fmt.Errorf("element %d: %w", index+1, err)
		}
	}
	return nil
}

func ValidateBoardApproval(before CreativeBoard, patch BoardPatch, after CreativeBoard) error {
	if err := ValidateCreativeBoard(before); err != nil {
		return fmt.Errorf("before: %w", err)
	}
	if err := ValidateBoardPatch(patch); err != nil {
		return fmt.Errorf("patch: %w", err)
	}
	if err := ValidateCreativeBoard(after); err != nil {
		return fmt.Errorf("after: %w", err)
	}
	if before.BoardID != patch.BoardID || after.BoardID != before.BoardID {
		return errors.New("approval board_id must be stable")
	}
	if before.Status != "ready" || before.ToolPlanAllowed {
		return errors.New("approval requires a ready board that has not allowed tool plan")
	}
	if patch.Operation != BoardPatchOperationApproveBoard || patch.Actor != BoardPatchActorUser {
		return errors.New("approval patch must be user approve_board")
	}
	if patch.BaseVersion != before.Version || patch.TargetVersion != after.Version {
		return errors.New("approval patch version must bridge before and after board")
	}
	if after.Status != "approved" || !after.ToolPlanAllowed {
		return errors.New("approved board must set status=approved and tool_plan_allowed=true")
	}
	if after.ApprovedAt == nil || after.ApprovedBy == nil || strings.TrimSpace(*after.ApprovedBy) == "" {
		return errors.New("approved board requires approved_at and approved_by")
	}
	if before.ElementsCount != after.ElementsCount {
		return errors.New("approval must not change elements_count")
	}
	return nil
}

func ValidatePatchReplay(initial BoardSnapshot, patches []BoardPatch, expected BoardSnapshot) error {
	if err := ValidateBoardSnapshot(initial); err != nil {
		return fmt.Errorf("initial_snapshot: %w", err)
	}
	if err := ValidateBoardSnapshot(expected); err != nil {
		return fmt.Errorf("expected_snapshot: %w", err)
	}
	if len(patches) == 0 {
		return errors.New("patches are required")
	}
	currentVersion := initial.Version
	var lastPatchID string
	for index, patch := range patches {
		if err := ValidateBoardPatch(patch); err != nil {
			return fmt.Errorf("patch %d: %w", index+1, err)
		}
		if patch.BoardID != initial.BoardID {
			return fmt.Errorf("patch %d board_id %q does not match initial board %q", index+1, patch.BoardID, initial.BoardID)
		}
		if patch.BaseVersion != currentVersion {
			return fmt.Errorf("patch %d base_version=%d, expected %d", index+1, patch.BaseVersion, currentVersion)
		}
		currentVersion = patch.TargetVersion
		lastPatchID = patch.PatchID
	}
	if expected.BoardID != initial.BoardID {
		return errors.New("expected snapshot board_id must match initial snapshot")
	}
	if expected.Version != currentVersion {
		return fmt.Errorf("expected snapshot version=%d, replay version=%d", expected.Version, currentVersion)
	}
	if expected.LastPatchID == nil || *expected.LastPatchID != lastPatchID {
		return fmt.Errorf("expected snapshot last_patch_id must be %q", lastPatchID)
	}
	return nil
}

func validatePrefixID(value, prefix string) error {
	if err := pr1.ValidateID(value); err != nil {
		return err
	}
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("must start with %s", prefix)
	}
	return nil
}

func isAllowed(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}
