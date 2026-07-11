package generation

import (
	"context"
	"errors"
	"fmt"
)

// JobHandlerProviderAdapter lets the existing synchronous Image2/Seedance/demo
// handlers participate in LifecycleWorker while providers are migrated to true
// Submit/Poll adapters. It always completes during Submit and therefore never
// invents a provider task for Poll.
type JobHandlerProviderAdapter struct {
	Handler JobHandler
}

func NewJobHandlerProviderAdapter(handler JobHandler) JobHandlerProviderAdapter {
	return JobHandlerProviderAdapter{Handler: handler}
}

func (a JobHandlerProviderAdapter) Submit(ctx context.Context, job GenerationJob) (ProviderResponse, error) {
	if a.Handler == nil {
		return ProviderResponse{}, fmt.Errorf("legacy generation job handler is required")
	}
	result, err := a.Handler.Handle(ctx, job)
	if err != nil {
		var execution *ExecutionError
		if errors.As(err, &execution) {
			return ProviderResponse{}, err
		}
		return ProviderResponse{}, NewExecutionError(ErrorStageProvider, "provider_handler_failed", ProviderErrorRetryable(err), err)
	}
	return ProviderResponse{
		State:  ProviderStateCompleted,
		Status: ProviderStateCompleted,
		Result: ProviderResult{AssetIDs: append([]string(nil), result.AssetIDs...), Payload: cloneMap(result.Result)},
	}, nil
}

func (JobHandlerProviderAdapter) Poll(context.Context, GenerationJob) (ProviderResponse, error) {
	return ProviderResponse{}, NewExecutionError(ErrorStageProvider, "legacy_provider_poll_unsupported", false, fmt.Errorf("legacy synchronous provider cannot be polled"))
}

func (JobHandlerProviderAdapter) Cancel(context.Context, GenerationJob) (ProviderCancelResult, error) {
	return ProviderCancelResult{Confirmed: false}, NewExecutionError(ErrorStageProvider, "legacy_provider_cancel_unsupported", true, fmt.Errorf("legacy synchronous provider cannot be cancelled after submit"))
}

var _ ProviderAdapter = JobHandlerProviderAdapter{}
