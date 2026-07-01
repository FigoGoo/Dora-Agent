package pr3

import (
	"errors"
	"fmt"
	"strings"
)

const (
	ToolTaskCompletedStream = "tool:task:completed"
)

type ToolTaskCompletedStreamEvent struct {
	Stream         string `json:"stream"`
	DedupeKey      string `json:"dedupe_key"`
	ToolTaskID     string `json:"tool_task_id"`
	ProviderStatus string `json:"provider_status"`
	OutputDigest   string `json:"output_digest"`
}

func ValidateToolTaskCompletedStreamEvent(event ToolTaskCompletedStreamEvent) error {
	if event.Stream != ToolTaskCompletedStream {
		return fmt.Errorf("stream must be %s", ToolTaskCompletedStream)
	}
	if strings.TrimSpace(event.DedupeKey) == "" {
		return errors.New("dedupe_key is required")
	}
	if err := validatePrefixID(event.ToolTaskID, "ttask_"); err != nil {
		return fmt.Errorf("tool_task_id: %w", err)
	}
	if !isAllowed(event.ProviderStatus, []string{"succeeded", "failed", "cancelled"}) {
		return fmt.Errorf("invalid provider_status %q", event.ProviderStatus)
	}
	if err := validateDigest(event.OutputDigest); err != nil {
		return fmt.Errorf("output_digest: %w", err)
	}
	return nil
}

func ValidateProviderAsyncResume(before ToolTask, event ToolTaskCompletedStreamEvent, after ToolTask) error {
	if err := ValidateToolTask(before); err != nil {
		return fmt.Errorf("tool_task_before_restart: %w", err)
	}
	if err := ValidateToolTaskCompletedStreamEvent(event); err != nil {
		return fmt.Errorf("redis_stream_event: %w", err)
	}
	if err := ValidateToolTask(after); err != nil {
		return fmt.Errorf("tool_task_after_resume: %w", err)
	}
	if before.ToolTaskID != event.ToolTaskID || after.ToolTaskID != before.ToolTaskID {
		return errors.New("tool_task_id must be stable across resume")
	}
	if before.ToolPlanID != after.ToolPlanID ||
		before.ToolPlanItemID != after.ToolPlanItemID ||
		before.RunID != after.RunID ||
		before.IdempotencyKey != after.IdempotencyKey ||
		before.InputDigest != after.InputDigest {
		return errors.New("tool task identity and input must be stable across resume")
	}
	if before.Status != "running" || after.Status != "succeeded" || after.Progress != 100 {
		return errors.New("provider resume fixture requires running -> succeeded with progress 100")
	}
	if after.OutputDigest == nil || *after.OutputDigest != event.OutputDigest {
		return errors.New("resumed task output_digest must come from provider completion event")
	}
	return nil
}
