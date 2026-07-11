package generation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	ProviderStateAccepted  = "accepted"
	ProviderStatePending   = "pending"
	ProviderStateCompleted = "completed"
	ProviderStateFailed    = "failed"
	ProviderStateCancelled = "cancelled"
)

type ProviderResponse struct {
	State      string
	TaskID     string
	RequestID  string
	Status     string
	RetryAfter time.Duration
	Result     ProviderResult
}

type ProviderCancelResult struct {
	Confirmed bool
	Status    string
}

// ProviderAdapter separates submit from poll so a process restart never needs
// to submit the same media task again merely to continue observing it.
type ProviderAdapter interface {
	Submit(ctx context.Context, job GenerationJob) (ProviderResponse, error)
	Poll(ctx context.Context, job GenerationJob) (ProviderResponse, error)
	Cancel(ctx context.Context, job GenerationJob) (ProviderCancelResult, error)
}

type ProviderResultRecovery interface {
	RecoverProviderResult(context.Context, GenerationJob) (ProviderResult, bool, error)
}

type LifecycleWorkerConfig struct {
	Store          WorkflowStore
	Queue          JobQueue
	Providers      map[string]ProviderAdapter
	Finalizer      *FinalizationEngine
	Barrier        *BatchBarrier
	ResultRecovery ProviderResultRecovery
	Clock          func() time.Time
	WorkerID       string
	LeaseDuration  time.Duration
	PollTimeout    time.Duration
}

// LifecycleWorker executes one durable provider/finalization phase at a time.
// Every return point is restartable from fields persisted on GenerationJob.
type LifecycleWorker struct {
	store          WorkflowStore
	queue          JobQueue
	providers      map[string]ProviderAdapter
	finalizer      *FinalizationEngine
	barrier        *BatchBarrier
	resultRecovery ProviderResultRecovery
	clock          func() time.Time
	workerID       string
	leaseDuration  time.Duration
	pollTimeout    time.Duration
}

func NewLifecycleWorker(config LifecycleWorkerConfig) *LifecycleWorker {
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.PollTimeout <= 0 {
		config.PollTimeout = time.Second
	}
	if strings.TrimSpace(config.WorkerID) == "" {
		config.WorkerID = "generation-worker-" + defaultID()
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 30 * time.Second
	}
	if config.Providers == nil {
		config.Providers = map[string]ProviderAdapter{}
	}
	if config.Barrier == nil && config.Store != nil {
		config.Barrier = NewBatchBarrier(config.Store, config.Clock)
	}
	if config.Finalizer == nil && config.Store != nil {
		config.Finalizer = NewFinalizationEngine(FinalizationEngineConfig{Store: config.Store, Barrier: config.Barrier, Clock: config.Clock})
	}
	return &LifecycleWorker{
		store: config.Store, queue: config.Queue, providers: config.Providers,
		finalizer: config.Finalizer, barrier: config.Barrier, clock: config.Clock,
		workerID: config.WorkerID, leaseDuration: config.LeaseDuration, resultRecovery: config.ResultRecovery,
		pollTimeout: config.PollTimeout,
	}
}

func (w *LifecycleWorker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil || w.store == nil {
		return false, fmt.Errorf("lifecycle worker store is required")
	}
	if w.queue == nil {
		return false, fmt.Errorf("lifecycle worker queue is required")
	}
	payload, ok, err := w.queue.Dequeue(ctx, w.pollTimeout)
	if err != nil || !ok {
		return ok, err
	}
	_, err = w.Process(ctx, payload.JobID)
	return true, err
}

func (w *LifecycleWorker) Process(ctx context.Context, jobID string) (GenerationJob, error) {
	if w == nil || w.store == nil {
		return GenerationJob{}, fmt.Errorf("lifecycle worker store is required")
	}
	job, err := w.store.GetJob(ctx, jobID)
	if err != nil {
		return GenerationJob{}, err
	}
	if IsTerminalJobStatus(job.Status) {
		return job, nil
	}
	if (job.Status == StatusRunning || job.Status == StatusFinalizing) && job.LeaseUntil != nil && job.LeaseUntil.After(w.clock()) {
		return job, nil
	}
	if (job.Status == StatusWaitingProvider || job.Status == StatusRetryWait) && !job.NextRunAt.IsZero() && job.NextRunAt.After(w.clock()) {
		return job, nil
	}
	if job.Attempt > 0 && w.resultRecovery != nil {
		var recoveredResult ProviderResult
		var complete bool
		job, recoveredResult, complete, err = w.recoverProviderReceipt(ctx, job)
		if err != nil {
			return GenerationJob{}, err
		}
		if complete {
			return w.finalizeWithHeartbeat(ctx, job, recoveredResult)
		}
	}
	provider := w.providers[strings.TrimSpace(job.Provider)]
	if provider == nil {
		return w.failProvider(ctx, job, "provider_not_registered", fmt.Errorf("provider %q is not registered", job.Provider))
	}
	// Once provider output has moved into finalization, cancellation is a
	// settlement concern: FinalizationEngine either stops before charge or
	// records compensation after charge. Calling provider.Cancel here could
	// strand a completed provider task in an endless cancel loop.
	if job.Status == StatusFinalizing || (job.Status == StatusRetryWait && (job.Phase == PhaseArtifactFinalize || job.Phase == PhaseBillingCharge)) {
		claimed, claimErr := w.claimFinalization(ctx, job)
		if claimErr != nil {
			return GenerationJob{}, claimErr
		}
		return w.finalizeWithHeartbeat(ctx, claimed, providerResultFromJob(claimed))
	}
	if job.CancelRequested {
		// Submit may have reached the provider before persisting its task receipt.
		// Replaying Submit with the stable job idempotency key recovers that task;
		// cancelling locally at this point would orphan provider work and cost.
		if strings.TrimSpace(job.ProviderTaskID) == "" && job.Attempt > 0 {
			submitted, submitErr := w.submit(ctx, job, provider)
			if submitErr != nil || IsTerminalJobStatus(submitted.Status) || submitted.Status == StatusFinalizing {
				return submitted, submitErr
			}
			if strings.TrimSpace(submitted.ProviderTaskID) != "" {
				return w.processCancellation(ctx, submitted, provider)
			}
			return submitted, nil
		}
		return w.processCancellation(ctx, job, provider)
	}
	if strings.TrimSpace(job.ProviderTaskID) == "" {
		return w.submit(ctx, job, provider)
	}
	return w.poll(ctx, job, provider)
}

func (w *LifecycleWorker) submit(ctx context.Context, job GenerationJob, provider ProviderAdapter) (GenerationJob, error) {
	running, err := w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusRunning
		current.Phase = PhaseProviderSubmit
		leaseUntil := w.clock().Add(w.leaseDuration)
		current.LeaseOwner = w.workerID
		current.LeaseUntil = &leaseUntil
		current.Attempt++
		current.Retryable = false
		return nil, nil
	})
	if err != nil {
		return GenerationJob{}, err
	}
	callCtx, finishHeartbeat := w.startProviderHeartbeat(ctx, running)
	response, err := provider.Submit(callCtx, running)
	heartbeatErr := finishHeartbeat()
	if err != nil {
		if heartbeatErr != nil {
			return GenerationJob{}, heartbeatErr
		}
		return w.handleProviderError(ctx, running, err)
	}
	return w.applyProviderResponse(ctx, running, response)
}

func (w *LifecycleWorker) poll(ctx context.Context, job GenerationJob, provider ProviderAdapter) (GenerationJob, error) {
	if job.ProviderPollAttempts >= normalizedMaxProviderPollAttempts(job.MaxProviderPollAttempts) {
		return w.failProvider(ctx, job, "provider_poll_exhausted", fmt.Errorf("provider task did not finish after %d polls", job.ProviderPollAttempts))
	}
	running, err := w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusRunning
		current.Phase = PhaseProviderPoll
		leaseUntil := w.clock().Add(w.leaseDuration)
		current.LeaseOwner = w.workerID
		current.LeaseUntil = &leaseUntil
		current.Attempt++
		current.ProviderPollAttempts++
		current.Retryable = false
		return nil, nil
	})
	if err != nil {
		return GenerationJob{}, err
	}
	callCtx, finishHeartbeat := w.startProviderHeartbeat(ctx, running)
	response, err := provider.Poll(callCtx, running)
	heartbeatErr := finishHeartbeat()
	if err != nil {
		if heartbeatErr != nil {
			return GenerationJob{}, heartbeatErr
		}
		return w.handleProviderError(ctx, running, err)
	}
	return w.applyProviderResponse(ctx, running, response)
}

func (w *LifecycleWorker) applyProviderResponse(ctx context.Context, job GenerationJob, response ProviderResponse) (GenerationJob, error) {
	state := strings.TrimSpace(response.State)
	switch state {
	case ProviderStateAccepted, ProviderStatePending:
		if job.Phase == PhaseProviderPoll && job.ProviderPollAttempts >= normalizedMaxProviderPollAttempts(job.MaxProviderPollAttempts) {
			return w.failProvider(ctx, job, "provider_poll_exhausted", fmt.Errorf("provider task did not finish after %d polls", job.ProviderPollAttempts))
		}
		delay := response.RetryAfter
		if delay <= 0 {
			delay = time.Second
		}
		taskID := valueOrDefault(response.TaskID, job.ProviderTaskID)
		if strings.TrimSpace(taskID) == "" {
			return w.failProvider(ctx, job, "provider_protocol_error", fmt.Errorf("provider %s response is missing task id", state))
		}
		return w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
			current.Status = StatusWaitingProvider
			current.Phase = PhaseProviderPoll
			current.ProviderTaskID = taskID
			current.ProviderRequestID = valueOrDefault(response.RequestID, current.ProviderRequestID)
			current.ProviderStatus = valueOrDefault(response.Status, state)
			current.NextRunAt = w.clock().Add(delay)
			current.LeaseOwner = ""
			current.LeaseUntil = nil
			return []OutboxEvent{{
				IdempotencyKey: fmt.Sprintf("job:%s:poll:%d", current.ID, current.StatusVersion+1),
				EventType:      EventJobDispatch, Destination: DestinationMediaJobs,
				AvailableAt: current.NextRunAt,
				Payload:     map[string]any{"job_id": current.ID, "provider_task_id": current.ProviderTaskID},
			}}, nil
		})
	case ProviderStateCompleted:
		providerResult := response.Result
		providerResult.TaskID = valueOrDefault(providerResult.TaskID, response.TaskID)
		providerResult.RequestID = valueOrDefault(providerResult.RequestID, response.RequestID)
		providerResult.Status = valueOrDefault(providerResult.Status, response.Status)
		finalizing, err := w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
			current.Status = StatusFinalizing
			current.Phase = PhaseArtifactFinalize
			current.ProviderTaskID = valueOrDefault(providerResult.TaskID, current.ProviderTaskID)
			current.ProviderRequestID = valueOrDefault(providerResult.RequestID, current.ProviderRequestID)
			current.ProviderStatus = valueOrDefault(providerResult.Status, ProviderStateCompleted)
			current.Result = cloneMap(providerResult.Payload)
			current.ResultAssetIDs = append([]string(nil), providerResult.AssetIDs...)
			if !current.ProviderUsageRecorded {
				current.ProviderUsageRecorded = true
				current.ProviderUsageReported = providerUsageWasReported(providerResult)
				current.ProviderActualPoints = providerResult.ActualPoints
				current.ProviderCostBreakdown = cloneInt64Map(providerResult.CostBreakdown)
			}
			leaseUntil := w.clock().Add(w.leaseDuration)
			current.LeaseOwner = w.workerID
			current.LeaseUntil = &leaseUntil
			return nil, nil
		})
		if err != nil {
			return GenerationJob{}, err
		}
		return w.finalizeWithHeartbeat(ctx, finalizing, providerResult)
	case ProviderStateFailed:
		return w.failProvider(ctx, job, "provider_failed", fmt.Errorf("provider task failed with status %q", response.Status))
	case ProviderStateCancelled:
		return w.cancelJob(ctx, job, "provider_cancelled")
	default:
		return w.failProvider(ctx, job, "provider_protocol_error", fmt.Errorf("unsupported provider response state %q", state))
	}
}

func (w *LifecycleWorker) processCancellation(ctx context.Context, job GenerationJob, provider ProviderAdapter) (GenerationJob, error) {
	if strings.TrimSpace(job.ProviderTaskID) == "" {
		return w.cancelJob(ctx, job, "cancelled_before_submit")
	}
	running, err := w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusRunning
		current.Phase = PhaseProviderPoll
		until := w.clock().Add(w.leaseDuration)
		current.LeaseOwner, current.LeaseUntil = w.workerID, &until
		return nil, nil
	})
	if err != nil {
		return GenerationJob{}, err
	}
	callCtx, finishHeartbeat := w.startProviderHeartbeat(ctx, running)
	result, err := provider.Cancel(callCtx, running)
	heartbeatErr := finishHeartbeat()
	if err != nil {
		if heartbeatErr != nil {
			return GenerationJob{}, heartbeatErr
		}
		_, _, retryable := classifyExecutionError(err, ErrorStageProvider, "provider_cancel_failed")
		if retryable {
			return w.scheduleProviderRetry(ctx, running, "provider_cancel_failed", err)
		}
		// A provider may reject cancellation because the task already reached a
		// terminal state. Observe that state instead of retrying cancel forever.
		return w.poll(ctx, running, provider)
	}
	if result.Confirmed {
		return w.cancelJob(ctx, running, "provider_cancelled")
	}
	// Async cancellation and a concurrent provider completion are both settled
	// by Poll. If still pending, the next wake will request cancellation again.
	return w.poll(ctx, running, provider)
}

func (w *LifecycleWorker) startProviderHeartbeat(parent context.Context, job GenerationJob) (context.Context, func() error) {
	callCtx, cancel := context.WithCancel(parent)
	stop := make(chan struct{})
	done := make(chan struct{})
	errorsOut := make(chan error, 1)
	interval := w.leaseDuration / 3
	if interval < time.Second {
		interval = time.Second
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-callCtx.Done():
				return
			case <-ticker.C:
				if err := w.store.RenewJobLease(parent, job.ID, w.workerID, w.clock().Add(w.leaseDuration)); err != nil {
					select {
					case errorsOut <- err:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	return callCtx, func() error {
		close(stop)
		cancel()
		<-done
		select {
		case err := <-errorsOut:
			return err
		default:
			return nil
		}
	}
}

func (w *LifecycleWorker) cancelJob(ctx context.Context, job GenerationJob, code string) (GenerationJob, error) {
	if err := w.finalizer.DiscardUndelivered(ctx, job, "cancelled"); err != nil {
		return GenerationJob{}, err
	}
	updated, err := w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusCancelled
		current.ErrorCode = code
		current.ErrorMessage = code
		current.CompensationStatus = CompensationNotRequired
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":cancelled",
			EventType:      "job.cancelled", Destination: DestinationSessionSignal,
			Payload: map[string]any{"status": StatusCancelled, "error_code": code},
		}, newBatchFinalizeRequestedEvent(*current, "terminal")}, nil
	})
	if err == nil {
		tryFinalizeBatchBestEffort(ctx, w.barrier, updated.BatchID)
	}
	return updated, err
}

func (w *LifecycleWorker) handleProviderError(ctx context.Context, job GenerationJob, cause error) (GenerationJob, error) {
	if w.resultRecovery != nil {
		recovered, result, complete, recoveryErr := w.recoverProviderReceipt(ctx, job)
		if recoveryErr != nil {
			return GenerationJob{}, recoveryErr
		}
		job = recovered
		if complete {
			return w.finalizeWithHeartbeat(ctx, job, result)
		}
	}
	_, code, retryable := classifyExecutionError(cause, ErrorStageProvider, "provider_error")
	// Attempt counts normal submit/poll calls for observability. A long-running
	// async provider may be polled many times successfully, so only RetryCount
	// may consume the transient-failure retry budget.
	if retryable && (job.MaxAttempts <= 0 || job.RetryCount+1 < job.MaxAttempts) {
		return w.scheduleProviderRetry(ctx, job, code, cause)
	}
	return w.failProvider(ctx, job, code, cause)
}

func (w *LifecycleWorker) recoverProviderReceipt(ctx context.Context, job GenerationJob) (GenerationJob, ProviderResult, bool, error) {
	if w.resultRecovery == nil {
		return job, ProviderResult{}, false, nil
	}
	result, complete, err := w.resultRecovery.RecoverProviderResult(ctx, job)
	if err != nil || len(result.AssetIDs) == 0 {
		return job, result, complete, err
	}
	if complete {
		updated, mutateErr := w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
			current.Status = StatusFinalizing
			current.Phase = PhaseArtifactFinalize
			current.Result = cloneMap(result.Payload)
			current.ResultAssetIDs = append([]string(nil), result.AssetIDs...)
			if !current.ProviderUsageRecorded {
				current.ProviderUsageRecorded = true
				current.ProviderUsageReported = providerUsageWasReported(result)
				current.ProviderActualPoints = result.ActualPoints
				current.ProviderCostBreakdown = cloneInt64Map(result.CostBreakdown)
			}
			until := w.clock().Add(w.leaseDuration)
			current.LeaseOwner, current.LeaseUntil = w.workerID, &until
			return nil, nil
		})
		return updated, result, true, mutateErr
	}
	// A synchronous provider may have uploaded only some deterministic outputs
	// before a transport/store error. Persist that partial receipt so terminal
	// failure/cancellation can quarantine it; a later retry may still complete
	// the same output indexes.
	if sameStringSet(job.ResultAssetIDs, result.AssetIDs) {
		return job, result, false, nil
	}
	updated, mutateErr := w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Result = cloneMap(result.Payload)
		current.ResultAssetIDs = append([]string(nil), result.AssetIDs...)
		return nil, nil
	})
	return updated, result, false, mutateErr
}

func (w *LifecycleWorker) scheduleProviderRetry(ctx context.Context, job GenerationJob, code string, cause error) (GenerationJob, error) {
	updated, err := w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusRetryWait
		current.ErrorStage = ErrorStageProvider
		current.ErrorCode = code
		current.ErrorMessage = cause.Error()
		current.Retryable = true
		current.RetryCount++
		current.NextRunAt = w.clock().Add(retryBackoff(current.RetryCount))
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		return []OutboxEvent{{
			IdempotencyKey: fmt.Sprintf("job:%s:retry:%d", current.ID, current.StatusVersion+1),
			EventType:      EventJobDispatch, Destination: DestinationMediaJobs,
			AvailableAt: current.NextRunAt,
			Payload:     map[string]any{"job_id": current.ID, "retry_count": current.RetryCount},
		}}, nil
	})
	if err != nil {
		return GenerationJob{}, err
	}
	return updated, cause
}

func (w *LifecycleWorker) failProvider(ctx context.Context, job GenerationJob, code string, cause error) (GenerationJob, error) {
	if err := w.finalizer.DiscardUndelivered(ctx, job, "provider_failed"); err != nil {
		return GenerationJob{}, err
	}
	updated, err := w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusFailed
		current.ErrorStage = ErrorStageProvider
		current.ErrorCode = code
		current.ErrorMessage = cause.Error()
		current.Retryable = false
		current.CompensationStatus = CompensationNotRequired
		current.LeaseOwner = ""
		current.LeaseUntil = nil
		return []OutboxEvent{{
			IdempotencyKey: "job:" + current.ID + ":failed:" + code,
			EventType:      "job.failed", Destination: DestinationSessionSignal,
			Payload: map[string]any{"status": StatusFailed, "error_code": code},
		}, newBatchFinalizeRequestedEvent(*current, "terminal")}, nil
	})
	if err == nil {
		tryFinalizeBatchBestEffort(ctx, w.barrier, updated.BatchID)
	}
	if err != nil {
		return GenerationJob{}, err
	}
	return updated, cause
}

func (w *LifecycleWorker) claimFinalization(ctx context.Context, job GenerationJob) (GenerationJob, error) {
	return w.store.MutateJob(ctx, job.ID, job.StatusVersion, func(current *GenerationJob) ([]OutboxEvent, error) {
		current.Status = StatusFinalizing
		until := w.clock().Add(w.leaseDuration)
		current.LeaseOwner, current.LeaseUntil = w.workerID, &until
		return nil, nil
	})
}

func (w *LifecycleWorker) finalizeWithHeartbeat(ctx context.Context, job GenerationJob, result ProviderResult) (GenerationJob, error) {
	callCtx, finishHeartbeat := w.startProviderHeartbeat(ctx, job)
	updated, finalizeErr := w.finalizer.Finalize(callCtx, job.ID, result)
	heartbeatErr := finishHeartbeat()
	if finalizeErr != nil {
		return updated, finalizeErr
	}
	if heartbeatErr != nil {
		latest, getErr := w.store.GetJob(ctx, job.ID)
		if getErr != nil {
			return GenerationJob{}, heartbeatErr
		}
		// Completing or scheduling a retry intentionally releases the lease. A
		// different non-empty owner, however, means this worker lost the fence.
		if latest.LeaseOwner != "" && latest.LeaseOwner != w.workerID {
			return GenerationJob{}, heartbeatErr
		}
	}
	return updated, nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, value := range left {
		seen[value]++
	}
	for _, value := range right {
		if seen[value] == 0 {
			return false
		}
		seen[value]--
	}
	return true
}

func providerResultFromJob(job GenerationJob) ProviderResult {
	actualPoints := job.ProviderActualPoints
	breakdown := cloneInt64Map(job.ProviderCostBreakdown)
	if !job.ProviderUsageRecorded && job.BillingStatus == BillingCharged {
		actualPoints = job.ChargedPoints
		breakdown = cloneInt64Map(job.CostBreakdown)
	}
	return ProviderResult{
		TaskID: job.ProviderTaskID, RequestID: job.ProviderRequestID, Status: job.ProviderStatus,
		AssetIDs: append([]string(nil), job.ResultAssetIDs...), Payload: cloneMap(job.Result),
		UsageReported: job.ProviderUsageReported, ActualPoints: actualPoints, CostBreakdown: breakdown,
	}
}
