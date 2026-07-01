package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/boardgraph"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrBoardVersionConflict = errors.New("board version conflict")

func (r *Repository) SaveGenericCreationState(
	ctx context.Context,
	template boardgraph.GraphTemplate,
	plan boardgraph.GraphPlan,
	board boardgraph.CreativeBoard,
	elements []boardgraph.CreativeElement,
	events []foundation.AGUIEnvelope,
) error {
	if err := boardgraph.ValidateGraphTemplate(template); err != nil {
		return fmt.Errorf("graph_template: %w", err)
	}
	if err := boardgraph.ValidateGraphPlan(plan); err != nil {
		return fmt.Errorf("graph_plan: %w", err)
	}
	if err := boardgraph.ValidateBoardCreation(board, elements); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	if err := foundation.ValidateAGUISequence(events); err != nil {
		return fmt.Errorf("events: %w", err)
	}
	if plan.RunID != board.RunID || plan.BoardID != board.BoardID || events[0].RunID != plan.RunID {
		return errors.New("graph plan, board and events must belong to the same run")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "run_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status",
				"current_board_id",
				"current_graph_plan_id",
				"trace_id",
				"updated_at",
			}),
		}).Create(agentRunRecord(plan, board, events)).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(graphTemplateRecord(template)).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(graphPlanRecord(plan)).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(creativeBoardRecord(board)).Error; err != nil {
			return err
		}
		for _, element := range elements {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(creativeElementRecord(element)).Error; err != nil {
				return err
			}
		}
		for _, event := range events {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(runEventRecord(event)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) SaveBoardGraphForWorkbenchRun(
	ctx context.Context,
	run *model.Run,
	template boardgraph.GraphTemplate,
	plan boardgraph.GraphPlan,
	board boardgraph.CreativeBoard,
	elements []boardgraph.CreativeElement,
	events []foundation.AGUIEnvelope,
	routerDecisionDigest string,
	extraSelection map[string]any,
) error {
	if run == nil {
		return errors.New("run is required")
	}
	if err := validateGenericCreationPersistence(template, plan, board, elements, events); err != nil {
		return err
	}
	if plan.RunID != run.ID || board.RunID != run.ID {
		return errors.New("graph plan and board must belong to workbench run")
	}
	now := board.UpdatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var currentRun model.Run
		if err := tx.Where("id = ? AND deleted_at IS NULL", run.ID).First(&currentRun).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(graphTemplateRecord(template)).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(graphPlanRecord(plan)).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(creativeBoardRecord(board)).Error; err != nil {
			return err
		}
		for _, element := range elements {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(creativeElementRecord(element)).Error; err != nil {
				return err
			}
		}
		for _, event := range events {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(runEventRecord(event)).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.Run{}).
			Where("id = ? AND deleted_at IS NULL", run.ID).
			Updates(map[string]any{
				"status":     foundation.RunStatusWaitingConfirmation,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		selection := map[string]any{}
		if len(currentRun.SkillSelection) > 0 {
			_ = json.Unmarshal(currentRun.SkillSelection, &selection)
		}
		selection["router_decision_digest"] = routerDecisionDigest
		selection["current_board_id"] = board.BoardID
		selection["current_graph_plan_id"] = plan.GraphPlanID
		selection["board_status"] = board.Status
		selection["board_version"] = board.Version
		for key, value := range extraSelection {
			selection[key] = value
		}
		if err := tx.Model(&model.Run{}).
			Where("id = ? AND deleted_at IS NULL", run.ID).
			Update("skill_selection", mustJSON(selection)).Error; err != nil {
			return err
		}
		if len(events) > 0 {
			last := events[len(events)-1]
			if err := tx.Model(&model.Session{}).
				Where("id = ? AND deleted_at IS NULL", run.SessionID).
				Updates(map[string]any{
					"last_run_id":         run.ID,
					"last_event_sequence": last.Seq,
					"updated_at":          last.CreatedAt,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func validateGenericCreationPersistence(
	template boardgraph.GraphTemplate,
	plan boardgraph.GraphPlan,
	board boardgraph.CreativeBoard,
	elements []boardgraph.CreativeElement,
	events []foundation.AGUIEnvelope,
) error {
	if err := boardgraph.ValidateGraphTemplate(template); err != nil {
		return fmt.Errorf("graph_template: %w", err)
	}
	if err := boardgraph.ValidateGraphPlan(plan); err != nil {
		return fmt.Errorf("graph_plan: %w", err)
	}
	if err := boardgraph.ValidateBoardCreation(board, elements); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	for index, event := range events {
		if err := foundation.ValidateAGUIEnvelope(event); err != nil {
			return fmt.Errorf("event %d: %w", index+1, err)
		}
	}
	if len(events) == 0 || plan.RunID != board.RunID || plan.BoardID != board.BoardID || events[0].RunID != plan.RunID {
		return errors.New("graph plan, board and events must belong to the same run")
	}
	return nil
}

func (r *Repository) GetAgentRunV1(ctx context.Context, runID string) (model.AgentRunRecord, error) {
	if r.db.Migrator().HasColumn(&model.AgentRunRecord{}, "run_id") {
		var record model.AgentRunRecord
		err := r.db.WithContext(ctx).Where("run_id = ?", runID).First(&record).Error
		if err == nil {
			return record, nil
		}
	}
	var run model.Run
	if fallbackErr := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", runID).First(&run).Error; fallbackErr != nil {
		return model.AgentRunRecord{}, fallbackErr
	}
	return agentRunRecordFromWorkbenchRun(run), nil
}

func (r *Repository) GetGraphPlanV1(ctx context.Context, graphPlanID string) (boardgraph.GraphPlan, error) {
	var record model.GraphPlanRecord
	if err := r.db.WithContext(ctx).Where("graph_plan_id = ?", graphPlanID).First(&record).Error; err != nil {
		return boardgraph.GraphPlan{}, err
	}
	return graphPlanContract(record)
}

func (r *Repository) GetCreativeBoardV1(ctx context.Context, boardID string) (boardgraph.CreativeBoard, error) {
	var record model.CreativeBoardRecord
	if err := r.db.WithContext(ctx).Where("board_id = ?", boardID).First(&record).Error; err != nil {
		return boardgraph.CreativeBoard{}, err
	}
	return creativeBoardContract(record)
}

func (r *Repository) GetBoardSnapshotV1(ctx context.Context, boardID string) (boardgraph.BoardSnapshot, error) {
	var board model.CreativeBoardRecord
	if err := r.db.WithContext(ctx).Where("board_id = ?", boardID).First(&board).Error; err != nil {
		return boardgraph.BoardSnapshot{}, err
	}
	var elementRecords []model.CreativeElementRecord
	if err := r.db.WithContext(ctx).
		Where("board_id = ?", boardID).
		Order("created_at ASC, element_id ASC").
		Find(&elementRecords).Error; err != nil {
		return boardgraph.BoardSnapshot{}, err
	}
	elements := make([]boardgraph.CreativeElement, 0, len(elementRecords))
	for _, record := range elementRecords {
		element, err := creativeElementContract(record)
		if err != nil {
			return boardgraph.BoardSnapshot{}, err
		}
		elements = append(elements, element)
	}
	var lastPatch model.BoardPatchRecord
	var lastPatchID *string
	err := r.db.WithContext(ctx).
		Where("board_id = ?", boardID).
		Order("target_version DESC").
		First(&lastPatch).Error
	if err == nil {
		value := lastPatch.PatchID
		lastPatchID = &value
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return boardgraph.BoardSnapshot{}, err
	}
	snapshot := boardgraph.BoardSnapshot{
		SchemaVersion: boardgraph.SchemaVersionBoardSnapshot,
		BoardID:       board.BoardID,
		Version:       board.Version,
		Status:        board.Status,
		LastPatchID:   lastPatchID,
		Elements:      elements,
		BoardDigest:   board.BoardDigest,
		CreatedAt:     board.UpdatedAt,
	}
	if err := boardgraph.ValidateBoardSnapshot(snapshot); err != nil {
		return boardgraph.BoardSnapshot{}, err
	}
	return snapshot, nil
}

func (r *Repository) ListRunEventsV1AfterSeq(ctx context.Context, runID string, afterSeq int64, limit int) ([]model.RunEventRecord, error) {
	limit = normalizeLimit(limit, 10, 200)
	var events []model.RunEventRecord
	err := r.db.WithContext(ctx).
		Where("run_id = ? AND seq > ?", runID, afterSeq).
		Order("seq ASC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

func (r *Repository) NextRunEventSeqV1(ctx context.Context, runID string) (int64, error) {
	var maxSeq int64
	if err := r.db.WithContext(ctx).
		Model(&model.RunEventRecord{}).
		Where("run_id = ?", runID).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&maxSeq).Error; err != nil {
		return 0, err
	}
	return maxSeq + 1, nil
}

func (r *Repository) AppendRunEventsV1(ctx context.Context, events []foundation.AGUIEnvelope) error {
	if len(events) == 0 {
		return errors.New("events are required")
	}
	runID := events[0].RunID
	for index, event := range events {
		if err := foundation.ValidateAGUIEnvelope(event); err != nil {
			return fmt.Errorf("event %d: %w", index+1, err)
		}
		if event.RunID != runID {
			return fmt.Errorf("event %d: mixed run_id %q and %q", index+1, runID, event.RunID)
		}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, event := range events {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(runEventRecord(event)).Error; err != nil {
				return err
			}
		}
		last := events[len(events)-1]
		if tx.Migrator().HasTable(&model.Session{}) {
			if err := tx.Model(&model.Session{}).
				Where("id = ? AND deleted_at IS NULL", last.SessionID).
				Updates(map[string]any{
					"last_run_id":         last.RunID,
					"last_event_sequence": last.Seq,
					"updated_at":          last.CreatedAt,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) ApplyBoardApprovalV1(ctx context.Context, patch boardgraph.BoardPatch, after boardgraph.CreativeBoard) error {
	if err := boardgraph.ValidateBoardPatch(patch); err != nil {
		return fmt.Errorf("patch: %w", err)
	}
	if err := boardgraph.ValidateCreativeBoard(after); err != nil {
		return fmt.Errorf("after: %w", err)
	}
	if patch.Operation != boardgraph.BoardPatchOperationApproveBoard || patch.Actor != boardgraph.BoardPatchActorUser {
		return errors.New("only user approve_board patch is supported")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(boardPatchRecord(patch)).Error; err != nil {
			return err
		}
		updates := map[string]any{
			"status":            after.Status,
			"version":           after.Version,
			"board_digest":      after.BoardDigest,
			"approved_at":       after.ApprovedAt,
			"approved_by":       stringValue(after.ApprovedBy),
			"tool_plan_allowed": after.ToolPlanAllowed,
			"updated_at":        after.UpdatedAt,
		}
		result := tx.Model(&model.CreativeBoardRecord{}).
			Where("board_id = ? AND version = ?", patch.BoardID, patch.BaseVersion).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return updateRunAfterBoardApproval(tx, after)
		}
		var current model.CreativeBoardRecord
		if err := tx.Where("board_id = ?", patch.BoardID).First(&current).Error; err != nil {
			return err
		}
		if current.Version == patch.TargetVersion && current.BoardDigest == after.BoardDigest {
			return updateRunAfterBoardApproval(tx, after)
		}
		return fmt.Errorf("%w: board_id=%s base_version=%d", ErrBoardVersionConflict, patch.BoardID, patch.BaseVersion)
	})
}

func (r *Repository) ApplyBoardPatchAfterStateV1(ctx context.Context, patch boardgraph.BoardPatch, after boardgraph.CreativeBoard, elements []boardgraph.CreativeElement) error {
	if err := boardgraph.ValidateBoardPatch(patch); err != nil {
		return fmt.Errorf("patch: %w", err)
	}
	if patch.Operation == boardgraph.BoardPatchOperationApproveBoard {
		return errors.New("approve_board patch must use board approval flow")
	}
	if err := boardgraph.ValidateBoardCreation(after, elements); err != nil {
		return fmt.Errorf("after_state: %w", err)
	}
	if patch.BoardID != after.BoardID || patch.TargetVersion != after.Version {
		return errors.New("patch and after board must match board_id and target_version")
	}
	for _, element := range elements {
		if element.BoardID != after.BoardID {
			return errors.New("all after-state elements must belong to board")
		}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(boardPatchRecord(patch)).Error; err != nil {
			return err
		}
		result := tx.Model(&model.CreativeBoardRecord{}).
			Where("board_id = ? AND version = ?", patch.BoardID, patch.BaseVersion).
			Updates(creativeBoardUpdates(after))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			if err := replaceBoardElements(tx, after.BoardID, elements); err != nil {
				return err
			}
			return updateRunTouch(tx, after)
		}
		var current model.CreativeBoardRecord
		if err := tx.Where("board_id = ?", patch.BoardID).First(&current).Error; err != nil {
			return err
		}
		if current.Version == patch.TargetVersion && current.BoardDigest == after.BoardDigest {
			return nil
		}
		return fmt.Errorf("%w: board_id=%s base_version=%d", ErrBoardVersionConflict, patch.BoardID, patch.BaseVersion)
	})
}

func updateRunAfterBoardApproval(tx *gorm.DB, board boardgraph.CreativeBoard) error {
	if tx.Migrator().HasColumn(&model.AgentRunRecord{}, "run_id") && tx.Migrator().HasColumn(&model.AgentRunRecord{}, "current_board_id") {
		if err := tx.Model(&model.AgentRunRecord{}).
			Where("run_id = ? AND current_board_id = ?", board.RunID, board.BoardID).
			Updates(map[string]any{
				"status":     foundation.RunStatusPlanning,
				"updated_at": board.UpdatedAt,
			}).Error; err != nil {
			return err
		}
		return nil
	}
	return tx.Model(&model.Run{}).
		Where("id = ? AND deleted_at IS NULL", board.RunID).
		Updates(map[string]any{
			"status":     foundation.RunStatusPlanning,
			"updated_at": board.UpdatedAt,
		}).Error
}

func updateRunTouch(tx *gorm.DB, board boardgraph.CreativeBoard) error {
	if tx.Migrator().HasColumn(&model.AgentRunRecord{}, "run_id") && tx.Migrator().HasColumn(&model.AgentRunRecord{}, "current_board_id") {
		if err := tx.Model(&model.AgentRunRecord{}).
			Where("run_id = ? AND current_board_id = ?", board.RunID, board.BoardID).
			Update("updated_at", board.UpdatedAt).Error; err != nil {
			return err
		}
		return nil
	}
	return tx.Model(&model.Run{}).
		Where("id = ? AND deleted_at IS NULL", board.RunID).
		Update("updated_at", board.UpdatedAt).Error
}

func creativeBoardUpdates(board boardgraph.CreativeBoard) map[string]any {
	return map[string]any{
		"project_id":        board.ProjectID,
		"session_id":        board.SessionID,
		"run_id":            board.RunID,
		"graph_plan_id":     stringValue(board.GraphPlanID),
		"title":             board.Title,
		"status":            board.Status,
		"version":           board.Version,
		"elements_count":    board.ElementsCount,
		"board_digest":      board.BoardDigest,
		"approved_at":       board.ApprovedAt,
		"approved_by":       stringValue(board.ApprovedBy),
		"tool_plan_allowed": board.ToolPlanAllowed,
		"created_at":        board.CreatedAt,
		"updated_at":        board.UpdatedAt,
	}
}

func replaceBoardElements(tx *gorm.DB, boardID string, elements []boardgraph.CreativeElement) error {
	if err := tx.Where("board_id = ?", boardID).Delete(&model.CreativeElementRecord{}).Error; err != nil {
		return err
	}
	for _, element := range elements {
		if err := tx.Create(creativeElementRecord(element)).Error; err != nil {
			return err
		}
	}
	return nil
}

func agentRunRecord(plan boardgraph.GraphPlan, board boardgraph.CreativeBoard, events []foundation.AGUIEnvelope) *model.AgentRunRecord {
	traceID := ""
	if len(events) > 0 {
		traceID = stringValue(events[0].TraceID)
	}
	createdAt := plan.CreatedAt
	if board.CreatedAt.Before(createdAt) {
		createdAt = board.CreatedAt
	}
	return &model.AgentRunRecord{
		RunID:              plan.RunID,
		SessionID:          board.SessionID,
		ProjectID:          board.ProjectID,
		Status:             foundation.RunStatusWaitingConfirmation,
		CurrentBoardID:     board.BoardID,
		CurrentGraphPlanID: plan.GraphPlanID,
		TraceID:            traceID,
		CreatedAt:          createdAt,
		UpdatedAt:          board.UpdatedAt,
	}
}

func agentRunRecordFromWorkbenchRun(run model.Run) model.AgentRunRecord {
	return model.AgentRunRecord{
		RunID:     run.ID,
		SessionID: run.SessionID,
		ProjectID: run.ProjectID,
		Status:    run.Status,
		TraceID:   run.TraceID,
		CreatedAt: run.CreatedAt,
		UpdatedAt: run.UpdatedAt,
	}
}

func graphTemplateRecord(template boardgraph.GraphTemplate) *model.GraphTemplateRecord {
	return &model.GraphTemplateRecord{
		GraphTemplateID: template.GraphTemplateID,
		Name:            template.Name,
		Version:         template.Version,
		GraphType:       template.GraphType,
		SkillLevel:      template.SkillLevel,
		EntryNode:       template.EntryNode,
		TerminalNodes:   mustJSON(template.TerminalNodes),
		Nodes:           mustJSON(template.Nodes),
		Edges:           mustJSON(template.Edges),
		TemplateDigest:  template.TemplateDigest,
		CreatedAt:       template.CreatedAt,
	}
}

func graphPlanRecord(plan boardgraph.GraphPlan) *model.GraphPlanRecord {
	return &model.GraphPlanRecord{
		GraphPlanID:          plan.GraphPlanID,
		GraphTemplateID:      plan.GraphTemplateID,
		GraphTemplateVersion: plan.GraphTemplateVersion,
		RunID:                plan.RunID,
		BoardID:              plan.BoardID,
		Status:               plan.Status,
		CurrentNode:          stringValue(plan.CurrentNode),
		ValueDeliveredStage:  plan.ValueDeliveredStage,
		Nodes:                mustJSON(plan.Nodes),
		Edges:                mustJSON(plan.Edges),
		GraphPlanDigest:      plan.GraphPlanDigest,
		CreatedAt:            plan.CreatedAt,
		UpdatedAt:            plan.UpdatedAt,
	}
}

func creativeBoardRecord(board boardgraph.CreativeBoard) *model.CreativeBoardRecord {
	return &model.CreativeBoardRecord{
		BoardID:         board.BoardID,
		ProjectID:       board.ProjectID,
		SessionID:       board.SessionID,
		RunID:           board.RunID,
		GraphPlanID:     stringValue(board.GraphPlanID),
		Title:           board.Title,
		Status:          board.Status,
		Version:         board.Version,
		ElementsCount:   board.ElementsCount,
		BoardDigest:     board.BoardDigest,
		ApprovedAt:      board.ApprovedAt,
		ApprovedBy:      stringValue(board.ApprovedBy),
		ToolPlanAllowed: board.ToolPlanAllowed,
		CreatedAt:       board.CreatedAt,
		UpdatedAt:       board.UpdatedAt,
	}
}

func creativeElementRecord(element boardgraph.CreativeElement) *model.CreativeElementRecord {
	return &model.CreativeElementRecord{
		ElementID:      element.ElementID,
		BoardID:        element.BoardID,
		ElementType:    element.ElementType,
		Source:         element.Source,
		Status:         element.Status,
		Position:       mustJSON(element.Position),
		Content:        mustJSON(element.Content),
		LinkedAssetIDs: mustJSON(element.LinkedAssetIDs),
		ContentDigest:  element.ContentDigest,
		CreatedAt:      element.CreatedAt,
		UpdatedAt:      element.UpdatedAt,
	}
}

func boardPatchRecord(patch boardgraph.BoardPatch) *model.BoardPatchRecord {
	return &model.BoardPatchRecord{
		PatchID:        patch.PatchID,
		BoardID:        patch.BoardID,
		BaseVersion:    patch.BaseVersion,
		TargetVersion:  patch.TargetVersion,
		Operation:      patch.Operation,
		Actor:          patch.Actor,
		IdempotencyKey: patch.IdempotencyKey,
		Payload:        mustJSON(patch.Payload),
		PatchDigest:    patch.PatchDigest,
		CreatedAt:      patch.CreatedAt,
	}
}

func runEventRecord(event foundation.AGUIEnvelope) *model.RunEventRecord {
	return &model.RunEventRecord{
		EventID:              event.EventID,
		RunID:                event.RunID,
		Seq:                  event.Seq,
		EventType:            event.EventType,
		PayloadSchemaVersion: event.PayloadSchemaVersion,
		DedupeKey:            event.DedupeKey,
		PayloadDigest:        event.PayloadDigest,
		Payload:              mustJSON(event.Payload),
		TraceID:              stringValue(event.TraceID),
		CreatedAt:            event.CreatedAt,
	}
}

func creativeBoardContract(record model.CreativeBoardRecord) (boardgraph.CreativeBoard, error) {
	board := boardgraph.CreativeBoard{
		SchemaVersion:   boardgraph.SchemaVersionCreativeBoard,
		BoardID:         record.BoardID,
		ProjectID:       record.ProjectID,
		SessionID:       record.SessionID,
		RunID:           record.RunID,
		GraphPlanID:     stringPointer(record.GraphPlanID),
		Title:           record.Title,
		Status:          record.Status,
		Version:         record.Version,
		ElementsCount:   record.ElementsCount,
		BoardDigest:     record.BoardDigest,
		ApprovedAt:      record.ApprovedAt,
		ApprovedBy:      stringPointer(record.ApprovedBy),
		ToolPlanAllowed: record.ToolPlanAllowed,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
	}
	if err := boardgraph.ValidateCreativeBoard(board); err != nil {
		return boardgraph.CreativeBoard{}, err
	}
	return board, nil
}

func graphPlanContract(record model.GraphPlanRecord) (boardgraph.GraphPlan, error) {
	var nodes []boardgraph.GraphPlanNode
	if err := json.Unmarshal(record.Nodes, &nodes); err != nil {
		return boardgraph.GraphPlan{}, err
	}
	var edges []boardgraph.GraphPlanEdge
	if err := json.Unmarshal(record.Edges, &edges); err != nil {
		return boardgraph.GraphPlan{}, err
	}
	currentNode := stringPointer(record.CurrentNode)
	plan := boardgraph.GraphPlan{
		SchemaVersion:        boardgraph.SchemaVersionGraphPlan,
		GraphPlanID:          record.GraphPlanID,
		GraphTemplateID:      record.GraphTemplateID,
		GraphTemplateVersion: record.GraphTemplateVersion,
		RunID:                record.RunID,
		BoardID:              record.BoardID,
		Status:               record.Status,
		CurrentNode:          currentNode,
		ValueDeliveredStage:  record.ValueDeliveredStage,
		Nodes:                nodes,
		Edges:                edges,
		GraphPlanDigest:      record.GraphPlanDigest,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
	if err := boardgraph.ValidateGraphPlan(plan); err != nil {
		return boardgraph.GraphPlan{}, err
	}
	return plan, nil
}

func creativeElementContract(record model.CreativeElementRecord) (boardgraph.CreativeElement, error) {
	var position boardgraph.ElementPosition
	if err := json.Unmarshal(record.Position, &position); err != nil {
		return boardgraph.CreativeElement{}, err
	}
	var content map[string]any
	if err := json.Unmarshal(record.Content, &content); err != nil {
		return boardgraph.CreativeElement{}, err
	}
	var linkedAssetIDs []string
	if err := json.Unmarshal(record.LinkedAssetIDs, &linkedAssetIDs); err != nil {
		return boardgraph.CreativeElement{}, err
	}
	element := boardgraph.CreativeElement{
		SchemaVersion:  boardgraph.SchemaVersionCreativeElement,
		ElementID:      record.ElementID,
		BoardID:        record.BoardID,
		ElementType:    record.ElementType,
		Source:         record.Source,
		Status:         record.Status,
		Position:       position,
		Content:        content,
		LinkedAssetIDs: linkedAssetIDs,
		ContentDigest:  record.ContentDigest,
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
	if err := boardgraph.ValidateCreativeElement(element); err != nil {
		return boardgraph.CreativeElement{}, err
	}
	return element, nil
}

func mustJSON(value any) datatypes.JSON {
	body, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte(`null`))
	}
	return datatypes.JSON(body)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
