package pr2

import (
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

const (
	EventTypeBoardPatchApplied    = "board.patch.applied"
	EventTypeBoardSnapshotUpdated = "board.snapshot.updated"
	EventTypeGraphPlanCreated     = "graph.plan.created"
	EventTypeGraphNodeUpdated     = "graph.node.updated"
)

type BoardPatchAppliedPayload struct {
	BoardID       string `json:"board_id"`
	PatchID       string `json:"patch_id"`
	BaseVersion   int    `json:"base_version"`
	TargetVersion int    `json:"target_version"`
	Operation     string `json:"operation"`
	PatchDigest   string `json:"patch_digest"`
}

type BoardSnapshotUpdatedPayload struct {
	BoardID           string   `json:"board_id"`
	BoardVersion      int      `json:"board_version"`
	BoardStatus       string   `json:"board_status"`
	BoardDigest       string   `json:"board_digest"`
	ChangedElementIDs []string `json:"changed_element_ids"`
	SnapshotRequired  bool     `json:"snapshot_required"`
}

type GraphPlanCreatedPayload struct {
	GraphPlanID         string `json:"graph_plan_id"`
	GraphTemplateID     string `json:"graph_template_id"`
	GraphPlanStatus     string `json:"graph_plan_status"`
	GraphPlanDigest     string `json:"graph_plan_digest"`
	BoardID             string `json:"board_id"`
	ValueDeliveredStage string `json:"value_delivered_stage"`
}

type GraphNodeUpdatedPayload struct {
	GraphPlanID  string  `json:"graph_plan_id"`
	NodeID       string  `json:"node_id"`
	NodeType     string  `json:"node_type"`
	NodeStatus   string  `json:"node_status"`
	Progress     int     `json:"progress"`
	OutputDigest *string `json:"output_digest,omitempty"`
}

func ValidateBoardPatchAppliedPayload(payload BoardPatchAppliedPayload) error {
	if err := validatePrefixID(payload.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if err := validatePrefixID(payload.PatchID, "patch_"); err != nil {
		return fmt.Errorf("patch_id: %w", err)
	}
	if payload.BaseVersion < 0 || payload.TargetVersion != payload.BaseVersion+1 {
		return errors.New("target_version must equal base_version + 1")
	}
	if !isAllowed(payload.Operation, []string{
		BoardPatchOperationAddElement,
		BoardPatchOperationUpdateElement,
		BoardPatchOperationRemoveElement,
		BoardPatchOperationReorderElements,
		BoardPatchOperationReplaceBoard,
		BoardPatchOperationApproveBoard,
	}) {
		return fmt.Errorf("invalid operation %q", payload.Operation)
	}
	if err := pr1.ValidateDigest(payload.PatchDigest); err != nil {
		return fmt.Errorf("patch_digest: %w", err)
	}
	return nil
}

func ValidateBoardSnapshotUpdatedPayload(payload BoardSnapshotUpdatedPayload) error {
	if err := validatePrefixID(payload.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if payload.BoardVersion < 1 {
		return errors.New("board_version must be >= 1")
	}
	if !pr1.IsValidState(pr1.StateBoardStatus, payload.BoardStatus) {
		return fmt.Errorf("invalid board_status %q", payload.BoardStatus)
	}
	if err := pr1.ValidateDigest(payload.BoardDigest); err != nil {
		return fmt.Errorf("board_digest: %w", err)
	}
	for _, elementID := range payload.ChangedElementIDs {
		if err := validatePrefixID(elementID, "elem_"); err != nil {
			return fmt.Errorf("changed_element_ids: %w", err)
		}
	}
	return nil
}

func ValidateGraphPlanCreatedPayload(payload GraphPlanCreatedPayload) error {
	if err := validatePrefixID(payload.GraphPlanID, "gplan_"); err != nil {
		return fmt.Errorf("graph_plan_id: %w", err)
	}
	if payload.GraphTemplateID == "" {
		return errors.New("graph_template_id is required")
	}
	if !pr1.IsValidState(pr1.StateGraphPlanStatus, payload.GraphPlanStatus) {
		return fmt.Errorf("invalid graph_plan_status %q", payload.GraphPlanStatus)
	}
	if err := pr1.ValidateDigest(payload.GraphPlanDigest); err != nil {
		return fmt.Errorf("graph_plan_digest: %w", err)
	}
	if err := validatePrefixID(payload.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if !isAllowed(payload.ValueDeliveredStage, []string{
		ValueDeliveredStageBoardReady,
		ValueDeliveredStageStoryboardReady,
		ValueDeliveredStageAssetReady,
	}) {
		return fmt.Errorf("invalid value_delivered_stage %q", payload.ValueDeliveredStage)
	}
	return nil
}

func ValidateGraphNodeUpdatedPayload(payload GraphNodeUpdatedPayload) error {
	if err := validatePrefixID(payload.GraphPlanID, "gplan_"); err != nil {
		return fmt.Errorf("graph_plan_id: %w", err)
	}
	if payload.NodeID == "" || payload.NodeType == "" {
		return errors.New("node_id and node_type are required")
	}
	if !isAllowed(payload.NodeStatus, []string{
		GraphPlanNodeStatusPending,
		GraphPlanNodeStatusRunning,
		GraphPlanNodeStatusWaitingInput,
		GraphPlanNodeStatusWaitingConfirmation,
		GraphPlanNodeStatusCompleted,
		GraphPlanNodeStatusFailed,
		GraphPlanNodeStatusSkipped,
	}) {
		return fmt.Errorf("invalid node_status %q", payload.NodeStatus)
	}
	if payload.Progress < 0 || payload.Progress > 100 {
		return errors.New("progress must be between 0 and 100")
	}
	if payload.OutputDigest != nil {
		if err := pr1.ValidateDigest(*payload.OutputDigest); err != nil {
			return fmt.Errorf("output_digest: %w", err)
		}
	}
	return nil
}
