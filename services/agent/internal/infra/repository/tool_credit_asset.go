package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/toolasset"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) SaveToolPlanV1(ctx context.Context, plan toolasset.ToolPlan) error {
	if err := toolasset.ValidateToolPlan(plan); err != nil {
		return fmt.Errorf("tool_plan: %w", err)
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(toolPlanRecord(plan)).Error
}

func (r *Repository) GetToolPlanV1(ctx context.Context, toolPlanID string) (toolasset.ToolPlan, error) {
	var record model.ToolPlanRecord
	if err := r.db.WithContext(ctx).Where("tool_plan_id = ?", toolPlanID).First(&record).Error; err != nil {
		return toolasset.ToolPlan{}, err
	}
	return toolPlanContract(record)
}

func (r *Repository) GetToolPlanByBoardVersionV1(ctx context.Context, boardID string, boardVersion int) (toolasset.ToolPlan, error) {
	var record model.ToolPlanRecord
	if err := r.db.WithContext(ctx).
		Where("board_id = ? AND board_version = ?", boardID, boardVersion).
		Order("created_at DESC, tool_plan_id DESC").
		First(&record).Error; err != nil {
		return toolasset.ToolPlan{}, err
	}
	return toolPlanContract(record)
}

func (r *Repository) SaveToolTaskV1(ctx context.Context, task toolasset.ToolTask) error {
	if err := toolasset.ValidateToolTask(task); err != nil {
		return fmt.Errorf("tool_task: %w", err)
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(toolTaskRecord(task)).Error
}

func (r *Repository) GetToolTaskV1(ctx context.Context, toolTaskID string) (toolasset.ToolTask, error) {
	var record model.ToolTaskRecord
	if err := r.db.WithContext(ctx).Where("tool_task_id = ?", toolTaskID).First(&record).Error; err != nil {
		return toolasset.ToolTask{}, err
	}
	return toolTaskContract(record)
}

func (r *Repository) ApplyToolTaskCompletedEventV1(ctx context.Context, event toolasset.ToolTaskCompletedStreamEvent, completedAt time.Time) (toolasset.ToolTask, error) {
	if err := toolasset.ValidateToolTaskCompletedStreamEvent(event); err != nil {
		return toolasset.ToolTask{}, fmt.Errorf("provider_event: %w", err)
	}
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	var updated toolasset.ToolTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record model.ToolTaskRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_task_id = ?", event.ToolTaskID).First(&record).Error; err != nil {
			return err
		}
		before, err := toolTaskContract(record)
		if err != nil {
			return err
		}
		if before.Status == "succeeded" && before.OutputDigest != nil && *before.OutputDigest == event.OutputDigest {
			updated = before
			return nil
		}
		after := before
		after.Progress = 100
		after.UpdatedAt = completedAt.UTC()
		outputDigest := event.OutputDigest
		after.OutputDigest = &outputDigest
		switch event.ProviderStatus {
		case "succeeded":
			after.Status = "succeeded"
			after.ErrorCode = nil
			if err := toolasset.ValidateProviderAsyncResume(before, event, after); err != nil {
				return err
			}
		case "failed":
			after.Status = "failed"
			errorCode := "PROVIDER_TERMINAL_FAILURE"
			after.ErrorCode = &errorCode
			if err := toolasset.ValidateToolTask(after); err != nil {
				return err
			}
		case "cancelled":
			after.Status = "cancelled"
			after.ErrorCode = nil
			if err := toolasset.ValidateToolTask(after); err != nil {
				return err
			}
		default:
			return errors.New("unsupported provider_status")
		}
		updates := map[string]any{
			"status":        after.Status,
			"progress":      after.Progress,
			"output_digest": stringValue(after.OutputDigest),
			"error_code":    stringValue(after.ErrorCode),
			"updated_at":    after.UpdatedAt,
		}
		if err := tx.Model(&model.ToolTaskRecord{}).Where("tool_task_id = ?", after.ToolTaskID).Updates(updates).Error; err != nil {
			return err
		}
		updated = after
		return nil
	})
	if err != nil {
		return toolasset.ToolTask{}, err
	}
	return updated, nil
}

func toolPlanRecord(plan toolasset.ToolPlan) *model.ToolPlanRecord {
	return &model.ToolPlanRecord{
		ToolPlanID:           plan.ToolPlanID,
		RunID:                plan.RunID,
		BoardID:              plan.BoardID,
		BoardVersion:         plan.BoardVersion,
		GraphPlanID:          plan.GraphPlanID,
		Status:               plan.Status,
		Items:                mustJSON(plan.Items),
		EstimatedCredits:     plan.EstimatedCredits,
		Currency:             plan.Currency,
		ConfirmationRequired: plan.ConfirmationRequired,
		ExpiresAt:            plan.ExpiresAt,
		ToolPlanDigest:       plan.ToolPlanDigest,
		CreatedAt:            plan.CreatedAt,
		UpdatedAt:            plan.UpdatedAt,
	}
}

func toolTaskRecord(task toolasset.ToolTask) *model.ToolTaskRecord {
	return &model.ToolTaskRecord{
		ToolTaskID:     task.ToolTaskID,
		ToolPlanID:     task.ToolPlanID,
		ToolPlanItemID: task.ToolPlanItemID,
		RunID:          task.RunID,
		Status:         task.Status,
		Progress:       task.Progress,
		ProviderPolicy: mustJSON(task.ProviderPolicy),
		IdempotencyKey: task.IdempotencyKey,
		InputDigest:    task.InputDigest,
		OutputDigest:   stringValue(task.OutputDigest),
		ErrorCode:      stringValue(task.ErrorCode),
		CreatedAt:      task.CreatedAt,
		UpdatedAt:      task.UpdatedAt,
	}
}

func toolPlanContract(record model.ToolPlanRecord) (toolasset.ToolPlan, error) {
	var items []toolasset.ToolPlanItem
	if err := json.Unmarshal(record.Items, &items); err != nil {
		return toolasset.ToolPlan{}, err
	}
	plan := toolasset.ToolPlan{
		SchemaVersion:        toolasset.SchemaVersionToolPlan,
		ToolPlanID:           record.ToolPlanID,
		RunID:                record.RunID,
		BoardID:              record.BoardID,
		BoardVersion:         record.BoardVersion,
		GraphPlanID:          record.GraphPlanID,
		Status:               record.Status,
		Items:                items,
		EstimatedCredits:     record.EstimatedCredits,
		Currency:             record.Currency,
		ConfirmationRequired: record.ConfirmationRequired,
		ExpiresAt:            record.ExpiresAt,
		ToolPlanDigest:       record.ToolPlanDigest,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
	if err := toolasset.ValidateToolPlan(plan); err != nil {
		return toolasset.ToolPlan{}, err
	}
	return plan, nil
}

func toolTaskContract(record model.ToolTaskRecord) (toolasset.ToolTask, error) {
	var policy toolasset.ProviderPolicy
	if err := json.Unmarshal(record.ProviderPolicy, &policy); err != nil {
		return toolasset.ToolTask{}, err
	}
	task := toolasset.ToolTask{
		SchemaVersion:  toolasset.SchemaVersionToolTask,
		ToolTaskID:     record.ToolTaskID,
		ToolPlanID:     record.ToolPlanID,
		ToolPlanItemID: record.ToolPlanItemID,
		RunID:          record.RunID,
		Status:         record.Status,
		Progress:       record.Progress,
		ProviderPolicy: policy,
		IdempotencyKey: record.IdempotencyKey,
		InputDigest:    record.InputDigest,
		OutputDigest:   stringPointer(record.OutputDigest),
		ErrorCode:      stringPointer(record.ErrorCode),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
	if err := toolasset.ValidateToolTask(task); err != nil {
		return toolasset.ToolTask{}, err
	}
	return task, nil
}
