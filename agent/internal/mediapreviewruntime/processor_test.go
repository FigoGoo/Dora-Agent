package mediapreviewruntime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
)

type processorTestClock struct{ now time.Time }

func (clock processorTestClock) Now() time.Time { return clock.now }

type processorTestRunner struct {
	mu       sync.Mutex
	calls    int
	deadline time.Time
	err      error
	done     chan struct{}
}

func (runner *processorTestRunner) Run(ctx context.Context, _ Claim) (mediapreview.GraphToolResult, error) {
	defer close(runner.done)
	runner.mu.Lock()
	runner.calls++
	runner.deadline, _ = ctx.Deadline()
	runner.mu.Unlock()
	<-ctx.Done()
	runner.mu.Lock()
	runner.err = ctx.Err()
	runner.mu.Unlock()
	return mediapreview.GraphToolResult{}, ctx.Err()
}

func (runner *processorTestRunner) snapshot() (int, time.Time, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.calls, runner.deadline, runner.err
}

type processorTestRepository struct {
	mu                sync.Mutex
	claim             *Claim
	terminalClaim     *TerminalClaim
	completedTerminal *TerminalResult
	markRunningCalls  int
	renewDeadlines    []time.Time
	runtimeFailures   int
}

func (repository *processorTestRepository) Enqueue(context.Context, EnqueueCommand) (EnqueueResult, error) {
	panic("unexpected Enqueue")
}

func (repository *processorTestRepository) ClaimNext(context.Context, string, string, time.Duration) (*Claim, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	claim := repository.claim
	repository.claim = nil
	return claim, nil
}

func (repository *processorTestRepository) MarkRunning(context.Context, Claim) error {
	repository.mu.Lock()
	repository.markRunningCalls++
	repository.mu.Unlock()
	return nil
}

func (repository *processorTestRepository) RenewLease(ctx context.Context, _ Claim, _ time.Duration) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return ErrInvalidInput
	}
	repository.mu.Lock()
	repository.renewDeadlines = append(repository.renewDeadlines, deadline)
	repository.mu.Unlock()
	return ctx.Err()
}

func (repository *processorTestRepository) CompleteGraphResult(context.Context, Claim, mediapreview.GraphToolResult) error {
	panic("unexpected CompleteGraphResult")
}

func (repository *processorTestRepository) DeferInputRecovery(context.Context, Claim, time.Duration) error {
	panic("unexpected DeferInputRecovery")
}

func (repository *processorTestRepository) CompleteRuntimeFailure(context.Context, Claim, string) error {
	repository.mu.Lock()
	repository.runtimeFailures++
	repository.mu.Unlock()
	return nil
}

func (repository *processorTestRepository) BridgeNextTerminal(context.Context) (bool, error) {
	return false, nil
}

func (repository *processorTestRepository) ClaimNextTerminal(context.Context, string, time.Duration) (*TerminalClaim, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	claim := repository.terminalClaim
	repository.terminalClaim = nil
	return claim, nil
}

func (repository *processorTestRepository) CompleteTerminal(_ context.Context, _ TerminalClaim, result TerminalResult) error {
	repository.mu.Lock()
	repository.completedTerminal = &result
	repository.mu.Unlock()
	return nil
}

func (repository *processorTestRepository) snapshot() (int, []time.Time, int) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.markRunningCalls, append([]time.Time(nil), repository.renewDeadlines...), repository.runtimeFailures
}

func TestProcessorBindsFrozenDeadlineToRunnerAndLeaseRenewal(t *testing.T) {
	now := time.Now().UTC()
	claim := validProcessorTestClaim(t, now.Add(80*time.Millisecond))
	repository := &processorTestRepository{claim: &claim}
	runner := &processorTestRunner{done: make(chan struct{})}
	processor, err := NewProcessor(repository, runner, processorTestClock{now: now}, "agent-test",
		GenerateSourceType, ProcessorConfig{LeaseDuration: 100 * time.Millisecond,
			HeartbeatInterval: 5 * time.Millisecond, RecoveryDelay: time.Second, MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := processor.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessNext result: processed=%t err=%v", processed, err)
	}
	select {
	case <-runner.done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after frozen deadline")
	}
	runnerCalls, runnerDeadline, runnerErr := runner.snapshot()
	if runnerCalls != 1 || !runnerDeadline.Equal(claim.DeadlineAt) || runnerErr != context.DeadlineExceeded {
		t.Fatalf("runner deadline mismatch: calls=%d deadline=%s err=%v", runnerCalls, runnerDeadline, runnerErr)
	}
	markCalls, renewDeadlines, failures := repository.snapshot()
	if markCalls != 1 || len(renewDeadlines) == 0 || failures != 1 {
		t.Fatalf("processor lifecycle mismatch: mark=%d renew=%d failures=%d", markCalls, len(renewDeadlines), failures)
	}
	for _, deadline := range renewDeadlines {
		if !deadline.Equal(claim.DeadlineAt) {
			t.Fatalf("renewal escaped frozen deadline: got=%s want=%s", deadline, claim.DeadlineAt)
		}
	}
	renewCount := len(renewDeadlines)
	time.Sleep(15 * time.Millisecond)
	_, afterDeadlineRenewals, _ := repository.snapshot()
	if len(afterDeadlineRenewals) != renewCount {
		t.Fatalf("renewal continued after deadline: before=%d after=%d", renewCount, len(afterDeadlineRenewals))
	}
}

func TestProcessorUsesInjectedClockToRejectExpiredClaim(t *testing.T) {
	now := time.Now().UTC()
	claim := validProcessorTestClaim(t, now.Add(-time.Second))
	repository := &processorTestRepository{claim: &claim}
	runner := &processorTestRunner{done: make(chan struct{})}
	processor, err := NewProcessor(repository, runner, processorTestClock{now: now}, "agent-test",
		GenerateSourceType, ProcessorConfig{LeaseDuration: time.Second,
			HeartbeatInterval: 100 * time.Millisecond, RecoveryDelay: time.Second, MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := processor.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessNext result: processed=%t err=%v", processed, err)
	}
	runnerCalls, _, _ := runner.snapshot()
	markCalls, renewDeadlines, failures := repository.snapshot()
	if runnerCalls != 0 || markCalls != 0 || len(renewDeadlines) != 0 || failures != 1 {
		t.Fatalf("expired claim was executed: runner=%d mark=%d renew=%d failures=%d",
			runnerCalls, markCalls, len(renewDeadlines), failures)
	}
}

func TestValidateTerminalResultBindingRejectsAuthorityMismatch(t *testing.T) {
	baseClaim := validTerminalBindingClaim()
	baseResult := validTerminalBindingResult()
	if err := ValidateTerminalResultBinding(baseClaim, baseResult); err != nil {
		t.Fatalf("valid terminal binding rejected: %v", err)
	}
	assembleClaim, assembleResult := baseClaim, baseResult
	assembleAsset := *baseResult.AssetRef
	assembleResult.AssetRef = &assembleAsset
	assembleClaim.ToolKey, assembleClaim.JobType = mediapreview.AssembleOutputToolKey, mediapreview.JobTypeAssembleMP4
	assembleResult.AssetRef.MediaKind, assembleResult.AssetRef.MIMEType = "video", "video/mp4"
	if err := ValidateTerminalResultBinding(assembleClaim, assembleResult); err != nil {
		t.Fatalf("valid assemble terminal binding rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*TerminalClaim, *TerminalResult)
	}{
		{name: "asset id", mutate: func(_ *TerminalClaim, result *TerminalResult) {
			result.AssetRef.AssetID = "019f68e8-0421-7000-8000-000000000421"
		}},
		{name: "asset version", mutate: func(_ *TerminalClaim, result *TerminalResult) {
			result.AssetRef.Version = 2
		}},
		{name: "media kind", mutate: func(_ *TerminalClaim, result *TerminalResult) {
			result.AssetRef.MediaKind, result.AssetRef.MIMEType = "video", "video/mp4"
		}},
		{name: "mime type", mutate: func(_ *TerminalClaim, result *TerminalResult) {
			result.AssetRef.MIMEType = "image/jpeg"
		}},
		{name: "tool key", mutate: func(claim *TerminalClaim, _ *TerminalResult) {
			claim.ToolKey = mediapreview.AssembleOutputToolKey
		}},
		{name: "job type", mutate: func(claim *TerminalClaim, _ *TerminalResult) {
			claim.JobType = mediapreview.JobTypeAssembleMP4
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim, result := baseClaim, baseResult
			asset := *baseResult.AssetRef
			result.AssetRef = &asset
			test.mutate(&claim, &result)
			if err := ValidateTerminalResultBinding(claim, result); err != ErrOutputContract {
				t.Fatalf("binding mismatch error = %v, want ErrOutputContract", err)
			}
		})
	}
}

func TestTerminalProcessorDowngradesCrossJobAssetToRuntimeFailure(t *testing.T) {
	claim := validTerminalBindingClaim()
	result := validTerminalBindingResult()
	result.AssetRef.AssetID = "019f68e8-0421-7000-8000-000000000421"
	encoded, err := mediapreview.CanonicalJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	claim.ResultJSON = encoded
	claim.ResultDigest = digest(encoded)
	claim.TerminalStatus = "succeeded"
	repository := &processorTestRepository{terminalClaim: &claim}
	processor, err := NewTerminalProcessor(repository, "agent-test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := processor.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessNext result: processed=%t err=%v", processed, err)
	}
	repository.mu.Lock()
	completed := repository.completedTerminal
	repository.mu.Unlock()
	if completed == nil || completed.Status != "failed" || completed.ErrorCode != "MEDIA_PREVIEW_RUNTIME_FAILED" || completed.AssetRef != nil {
		t.Fatalf("cross-job asset was not downgraded safely: %+v", completed)
	}
}

func validTerminalBindingClaim() TerminalClaim {
	return TerminalClaim{
		ToolKey: mediapreview.GenerateMediaToolKey,
		JobType: mediapreview.JobTypeGeneratePNG,
		AssetID: "019f68e8-0420-7000-8000-000000000420", AssetVersion: 1,
	}
}

func validTerminalBindingResult() TerminalResult {
	return TerminalResult{
		SchemaVersion: mediapreview.JobResultSchemaVersion, Status: "succeeded",
		AssetRef: &TerminalAssetRef{
			AssetID: "019f68e8-0420-7000-8000-000000000420", Version: 1, Status: "ready",
			MediaKind: "image", MIMEType: "image/png",
			ContentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SizeBytes: 1024,
		},
		FinalizationReceiptID: "019f68e8-0422-7000-8000-000000000422",
	}
}

func validProcessorTestClaim(t *testing.T, deadline time.Time) Claim {
	t.Helper()
	intent, err := mediapreview.CanonicalJSON(mediapreview.GenerateMediaIntent{
		SchemaVersion:   mediapreview.GenerateMediaIntentVersion,
		PromptPreviewID: "019f68e8-0400-7000-8000-000000000400", ExpectedPromptVersion: 1,
		ExpectedPromptContentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TargetLocalKey:              "slot_1", OutputProfile: mediapreview.GenerateOutputProfile,
	})
	if err != nil {
		t.Fatal(err)
	}
	return Claim{
		Owner: "agent-test", RequestID: "019f68e8-0401-7000-8000-000000000401",
		IdempotencyKey: "019f68e8-0402-7000-8000-000000000402",
		UserID:         "019f68e8-0403-7000-8000-000000000403", ProjectID: "019f68e8-0404-7000-8000-000000000404",
		SessionID: "019f68e8-0405-7000-8000-000000000405", InputID: "019f68e8-0406-7000-8000-000000000406",
		TurnID: "019f68e8-0407-7000-8000-000000000407", RunID: "019f68e8-0408-7000-8000-000000000408",
		ToolCallID:      "019f68e8-0409-7000-8000-000000000409",
		AcceptedEventID: "019f68e8-0410-7000-8000-000000000410",
		TerminalEventID: "019f68e8-0411-7000-8000-000000000411",
		ToolKey:         mediapreview.GenerateMediaToolKey, IntentDigest: digest(intent), IntentJSON: intent,
		FenceToken: 1, Attempts: 1, DeadlineAt: deadline,
	}
}

var _ Repository = (*processorTestRepository)(nil)
var _ Runner = (*processorTestRunner)(nil)
