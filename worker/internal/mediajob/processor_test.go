package mediajob

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/mediapreview"
	"github.com/google/uuid"
)

func TestProcessorCompletesGeneratePNG(t *testing.T) {
	envelope := validTestEnvelope(t, mediapreview.JobTypeGeneratePNG, 1)
	repository := newFakeRepository()
	agent := newFakeAgent(envelope)
	business := newFakeBusiness(readyFinalizeResult(t, envelope))
	artifacts := &fakeArtifacts{receipt: validArtifactReceipt(envelope)}
	processor := newTestProcessor(t, repository, agent, business, artifacts)

	processed, err := processor.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("process PNG job: processed=%t err=%v", processed, err)
	}
	claimedAttemptID := agent.envelope.AttemptID
	if repository.status[claimedAttemptID] != AttemptStatusCompleted {
		t.Fatalf("attempt did not complete: %+v", repository.status)
	}
	if artifacts.calls != 1 || business.finalizeCalls != 1 || len(agent.commits) != 1 {
		t.Fatalf("unexpected side effects: artifact=%d finalize=%d commits=%d", artifacts.calls, business.finalizeCalls, len(agent.commits))
	}
	commit := agent.commits[0]
	if commit.Lease.Fence != envelope.Fence || commit.TerminalStatus != "succeeded" ||
		commit.ResultSchemaVersion != TerminalResultSchemaV1 || !strings.Contains(string(commit.Result), `"status":"succeeded"`) {
		t.Fatalf("unexpected terminal commit: %+v payload=%s", commit, commit.Result)
	}
}

func TestProcessorQueriesUnknownFinalizeBeforeTerminal(t *testing.T) {
	envelope := validTestEnvelope(t, mediapreview.JobTypeGeneratePNG, 1)
	repository := newFakeRepository()
	agent := newFakeAgent(envelope)
	result := readyFinalizeResult(t, envelope)
	business := newFakeBusiness(result)
	business.finalizeErrors = []error{ErrOutcomeUnknown}
	business.queryResults = []QueryFinalizationResultV1{{
		SchemaVersion: QueryFinalizationResultSchemaV1,
		RequestID:     uuid.Nil,
		Status:        "completed",
		Result:        &result,
	}}
	artifacts := &fakeArtifacts{receipt: validArtifactReceipt(envelope)}
	processor := newTestProcessor(t, repository, agent, business, artifacts)

	processed, err := processor.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("process unknown Finalize job: processed=%t err=%v", processed, err)
	}
	if business.finalizeCalls != 1 || business.queryCalls != 1 || len(agent.commits) != 1 {
		t.Fatalf("unknown Finalize did not converge by Query: finalize=%d query=%d commits=%d", business.finalizeCalls, business.queryCalls, len(agent.commits))
	}
	if business.queryRequests[0].CommandID != business.finalizeRequests[0].CommandID ||
		business.queryRequests[0].RequestDigest != business.finalizeRequests[0].RequestDigest {
		t.Fatal("Finalize unknown query changed first-write-wins command or digest")
	}
}

func TestProcessorTakeoverQueriesPriorFinalizeBeforeArtifact(t *testing.T) {
	envelope := validTestEnvelope(t, mediapreview.JobTypeGeneratePNG, 2)
	oldCommandID := mustUUIDv7(t)
	oldDigest := strings.Repeat("b", 64)
	result := readyFinalizeResult(t, envelope)
	repository := newFakeRepository()
	repository.latest = FinalizationRecovery{CommandID: oldCommandID, RequestDigest: oldDigest, QueryStatus: "completed", Result: &result}
	repository.latestFound = true
	agent := newFakeAgent(envelope)
	business := newFakeBusiness(result)
	business.queryResults = []QueryFinalizationResultV1{{
		SchemaVersion: QueryFinalizationResultSchemaV1, Status: "completed", Result: &result,
	}}
	artifacts := &fakeArtifacts{receipt: validArtifactReceipt(envelope)}
	processor := newTestProcessor(t, repository, agent, business, artifacts)

	processed, err := processor.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("process takeover job: processed=%t err=%v", processed, err)
	}
	if business.queryCalls != 1 || business.queryRequests[0].CommandID != oldCommandID ||
		business.queryRequests[0].RequestDigest != oldDigest {
		t.Fatalf("takeover did not query old Finalize identity: %+v", business.queryRequests)
	}
	if artifacts.calls != 0 || business.finalizeCalls != 0 {
		t.Fatalf("completed old Finalize must bypass artifact/new Finalize: artifact=%d finalize=%d", artifacts.calls, business.finalizeCalls)
	}
	if len(agent.commits) != 1 || agent.commits[0].Lease.Fence != 2 {
		t.Fatalf("takeover terminal must use new Fence: %+v", agent.commits)
	}
}

func TestProcessorConsumesOnlyClaimableCandidateAndUsesTakeoverFence(t *testing.T) {
	t.Run("valid_lease_hidden_by_view", func(t *testing.T) {
		envelope := validTestEnvelope(t, mediapreview.JobTypeGeneratePNG, 1)
		repository := newFakeRepository()
		agent := newFakeAgent(envelope)
		agent.candidate = false
		business := newFakeBusiness(readyFinalizeResult(t, envelope))
		artifacts := &fakeArtifacts{receipt: validArtifactReceipt(envelope)}
		processor := newTestProcessor(t, repository, agent, business, artifacts)
		processed, err := processor.ProcessNext(t.Context())
		if err != nil || processed {
			t.Fatalf("hidden valid Lease candidate: processed=%t err=%v", processed, err)
		}
		if agent.claimCalls != 0 || artifacts.calls != 0 || business.finalizeCalls != 0 {
			t.Fatalf("hidden valid Lease produced side effects: claims=%d artifacts=%d finalize=%d", agent.claimCalls, artifacts.calls, business.finalizeCalls)
		}
	})

	for _, status := range []string{"expired_running", "expired_reconciling"} {
		t.Run(status, func(t *testing.T) {
			envelope := validTestEnvelope(t, mediapreview.JobTypeGeneratePNG, 7)
			repository := newFakeRepository()
			result := readyFinalizeResult(t, envelope)
			business := newFakeBusiness(result)
			if status == "expired_reconciling" {
				repository.latestFound = true
				repository.latest = FinalizationRecovery{CommandID: mustUUIDv7(t), RequestDigest: strings.Repeat("d", 64)}
				business.queryResults = []QueryFinalizationResultV1{{
					SchemaVersion: QueryFinalizationResultSchemaV1, Status: "not_found",
				}}
			}
			agent := newFakeAgent(envelope)
			artifacts := &fakeArtifacts{receipt: validArtifactReceipt(envelope)}
			processor := newTestProcessor(t, repository, agent, business, artifacts)
			processed, err := processor.ProcessNext(t.Context())
			if err != nil || !processed {
				t.Fatalf("take over %s: processed=%t err=%v", status, processed, err)
			}
			if len(business.finalizeRequests) != 1 || business.finalizeRequests[0].Fence != 7 ||
				len(agent.commits) != 1 || agent.commits[0].Lease.Fence != 7 {
				t.Fatalf("takeover did not use new Fence: finalize=%+v terminal=%+v", business.finalizeRequests, agent.commits)
			}
			if status == "expired_reconciling" && business.queryCalls != 1 {
				t.Fatal("expired reconciling takeover did not query old Finalize before continuing")
			}
		})
	}
}

func TestProcessorRestartReusesFinalizeAndTerminalIDs(t *testing.T) {
	envelope := validTestEnvelope(t, mediapreview.JobTypeGeneratePNG, 1)
	repository := newFakeRepository()
	oldCommandID := mustUUIDv7(t)
	oldEventID := mustUUIDv7(t)
	repository.attempts[envelope.AttemptID] = ClaimIntent{
		AttemptID: envelope.AttemptID, ClaimRequestID: mustUUIDv7(t), JobID: envelope.JobID,
		WorkerID: "worker-test", Status: AttemptStatusRunning, StartedAt: time.Now().UTC(),
	}
	repository.status[envelope.AttemptID] = AttemptStatusRunning
	receipt := validArtifactReceipt(envelope)
	request := FinalizeRequestV1{
		SchemaVersion: FinalizeRequestSchemaV1, PreparationID: envelope.Target.PreparationID,
		OperationID: envelope.OperationID, BatchID: envelope.BatchID, JobID: envelope.JobID,
		AttemptID: envelope.AttemptID, Fence: envelope.Fence, TerminalStatus: "ready",
		Output: &FinalizeOutputV1{
			ContentDigest: receipt.ContentDigest, SizeBytes: receipt.SizeBytes, MIMEType: receipt.MIMEType,
			Width: receipt.Width, Height: receipt.Height,
		},
	}
	digest, err := FinalizeSemanticDigest(request)
	if err != nil {
		t.Fatalf("compute restart Finalize digest: %v", err)
	}
	repository.finalize[envelope.AttemptID] = FinalizeIntent{AttemptID: envelope.AttemptID, CommandID: oldCommandID, RequestDigest: digest}
	result := readyFinalizeResult(t, envelope)
	terminal := TerminalResultV1{
		SchemaVersion: TerminalResultSchemaV1, Status: "succeeded",
		AssetRef: &TerminalAssetRefV1{
			AssetID: result.AssetRef.AssetID, Version: 1, Status: "ready", MediaKind: "image",
			MIMEType: "image/png", ContentDigest: result.AssetRef.ContentDigest, SizeBytes: result.AssetRef.SizeBytes,
		},
		FinalizationReceiptID: &result.FinalizationReceiptID,
	}
	_, terminalDigest, err := terminalPayloadAndDigest(terminal)
	if err != nil {
		t.Fatalf("compute restart terminal digest: %v", err)
	}
	repository.terminal[envelope.AttemptID] = TerminalIntent{
		AttemptID: envelope.AttemptID, EventID: oldEventID, Status: "succeeded", ResultDigest: terminalDigest,
	}
	agent := newFakeAgent(envelope)
	business := newFakeBusiness(result)
	artifacts := &fakeArtifacts{receipt: receipt}
	processor := newTestProcessor(t, repository, agent, business, artifacts)

	if _, err := processor.ProcessNext(t.Context()); err != nil {
		t.Fatalf("process restarted attempt: %v", err)
	}
	if business.finalizeRequests[0].CommandID != oldCommandID {
		t.Fatalf("restart replaced Finalize CommandID: got %s want %s", business.finalizeRequests[0].CommandID, oldCommandID)
	}
	if agent.commits[0].EventID != oldEventID {
		t.Fatalf("restart replaced Terminal EventID: got %s want %s", agent.commits[0].EventID, oldEventID)
	}
}

func TestProcessorRestartConvergesTerminalUnknownBeforeClaimReplay(t *testing.T) {
	envelope := validTestEnvelope(t, mediapreview.JobTypeGeneratePNG, 4)
	repository := newFakeRepository()
	eventID := mustUUIDv7(t)
	digest := strings.Repeat("e", 64)
	repository.attempts[envelope.AttemptID] = ClaimIntent{
		AttemptID: envelope.AttemptID, ClaimRequestID: mustUUIDv7(t), JobID: envelope.JobID,
		WorkerID: "worker-test", Status: AttemptStatusTerminalUnknown, StartedAt: time.Now().UTC(),
	}
	repository.status[envelope.AttemptID] = AttemptStatusTerminalUnknown
	repository.terminal[envelope.AttemptID] = TerminalIntent{
		AttemptID: envelope.AttemptID, EventID: eventID, Status: "succeeded", ResultDigest: digest,
	}
	agent := newFakeAgent(envelope)
	agent.snapshot = &JobSnapshot{
		JobStatus: "succeeded", AttemptID: envelope.AttemptID, Fence: envelope.Fence,
		ResultSchemaVersion: TerminalResultSchemaV1, ResultDigest: digest, TerminalEventID: eventID,
	}
	business := newFakeBusiness(readyFinalizeResult(t, envelope))
	artifacts := &fakeArtifacts{receipt: validArtifactReceipt(envelope)}
	processor := newTestProcessor(t, repository, agent, business, artifacts)

	processed, err := processor.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("recover terminal_unknown: processed=%t err=%v", processed, err)
	}
	if repository.status[envelope.AttemptID] != AttemptStatusCompleted || agent.claimCalls != 0 || artifacts.calls != 0 {
		t.Fatalf("terminal recovery replayed Claim/artifact: status=%s claims=%d artifacts=%d", repository.status[envelope.AttemptID], agent.claimCalls, artifacts.calls)
	}
}

func TestProcessorLeaseLostCancelsArtifactAndSkipsFinalize(t *testing.T) {
	envelope := validTestEnvelope(t, mediapreview.JobTypeGeneratePNG, 1)
	repository := newFakeRepository()
	agent := newFakeAgent(envelope)
	agent.renewError = ErrLeaseLost
	business := newFakeBusiness(readyFinalizeResult(t, envelope))
	artifacts := &fakeArtifacts{blockUntilCancelled: true, receipt: validArtifactReceipt(envelope)}
	processor := newTestProcessor(t, repository, agent, business, artifacts)

	processed, err := processor.ProcessNext(t.Context())
	if !processed || !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("lease loss should stop attempt: processed=%t err=%v", processed, err)
	}
	if business.finalizeCalls != 0 || len(agent.commits) != 0 {
		t.Fatalf("lease-lost worker performed downstream side effects: finalize=%d commits=%d", business.finalizeCalls, len(agent.commits))
	}
	claimedAttemptID := agent.envelope.AttemptID
	if repository.status[claimedAttemptID] != AttemptStatusLeaseLost {
		t.Fatalf("lease-lost status not persisted: %+v", repository.status)
	}
}

func TestFinalizeSemanticDigestIgnoresTraceAndCommandIDs(t *testing.T) {
	envelope := validTestEnvelope(t, mediapreview.JobTypeGeneratePNG, 1)
	request := FinalizeRequestV1{
		SchemaVersion: FinalizeRequestSchemaV1, RequestID: mustUUIDv7(t), CommandID: mustUUIDv7(t),
		RequestDigest: strings.Repeat("0", 64), PreparationID: envelope.Target.PreparationID,
		OperationID: envelope.OperationID, BatchID: envelope.BatchID, JobID: envelope.JobID,
		AttemptID: envelope.AttemptID, Fence: envelope.Fence, TerminalStatus: "failed", ErrorCode: "ARTIFACT_INVALID",
	}
	first, err := FinalizeSemanticDigest(request)
	if err != nil {
		t.Fatalf("compute first digest: %v", err)
	}
	request.RequestID = mustUUIDv7(t)
	request.CommandID = mustUUIDv7(t)
	request.RequestDigest = strings.Repeat("f", 64)
	second, err := FinalizeSemanticDigest(request)
	if err != nil {
		t.Fatalf("compute second digest: %v", err)
	}
	if first != second {
		t.Fatalf("restart trace/command IDs changed semantic digest: %s != %s", first, second)
	}
}

type fakeRepository struct {
	mu           sync.Mutex
	attempts     map[uuid.UUID]ClaimIntent
	status       map[uuid.UUID]AttemptStatus
	finalize     map[uuid.UUID]FinalizeIntent
	terminal     map[uuid.UUID]TerminalIntent
	latest       FinalizationRecovery
	latestFound  bool
	attemptCount int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		attempts: make(map[uuid.UUID]ClaimIntent), status: make(map[uuid.UUID]AttemptStatus),
		finalize: make(map[uuid.UUID]FinalizeIntent), terminal: make(map[uuid.UUID]TerminalIntent), attemptCount: 1,
	}
}

func (r *fakeRepository) Readiness(context.Context) error { return nil }

func (r *fakeRepository) CreateClaimIntent(_ context.Context, intent ClaimIntent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts[intent.AttemptID] = intent
	r.status[intent.AttemptID] = AttemptStatusClaimPending
	return nil
}

func (r *fakeRepository) NextRecoverableClaim(_ context.Context, workerID string) (ClaimIntent, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for attemptID, intent := range r.attempts {
		status := r.status[attemptID]
		if intent.WorkerID == workerID && status != AttemptStatusCompleted && status != AttemptStatusFailed &&
			status != AttemptStatusRetryScheduled && status != AttemptStatusLeaseLost && status != AttemptStatusClaimRejected {
			return intent, true, nil
		}
	}
	return ClaimIntent{}, false, nil
}

func (r *fakeRepository) MarkClaimUnknown(_ context.Context, attemptID uuid.UUID, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status[attemptID] = AttemptStatusClaimUnknown
	return nil
}

func (r *fakeRepository) MarkClaimed(_ context.Context, envelope mediapreview.MediaJobEnvelopeV1, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status[envelope.AttemptID] = AttemptStatusRunning
	return nil
}

func (r *fakeRepository) RecordArtifact(_ context.Context, record ArtifactRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status[record.AttemptID] = AttemptStatusArtifactReady
	return nil
}

func (r *fakeRepository) EnsureFinalizeIntent(_ context.Context, proposed FinalizeIntent, _ time.Time) (FinalizeIntent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.finalize[proposed.AttemptID]; ok {
		if existing.RequestDigest != proposed.RequestDigest || existing.ErrorCode != proposed.ErrorCode {
			return FinalizeIntent{}, ErrStateConflict
		}
		return existing, nil
	}
	r.finalize[proposed.AttemptID] = proposed
	return proposed, nil
}

func (r *fakeRepository) GetTerminalIntent(_ context.Context, attemptID uuid.UUID) (TerminalIntent, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	intent, found := r.terminal[attemptID]
	return intent, found, nil
}

func (r *fakeRepository) RecordFinalizationObservation(_ context.Context, observation FinalizationObservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latest = FinalizationRecovery{
		CommandID: observation.CommandID, RequestDigest: observation.RequestDigest,
		QueryStatus: observation.QueryStatus, Result: observation.Result, ErrorCode: observation.ErrorCode,
	}
	return nil
}

func (r *fakeRepository) LatestFinalizationRecovery(context.Context, uuid.UUID) (FinalizationRecovery, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest, r.latestFound, nil
}

func (r *fakeRepository) EnsureTerminalIntent(_ context.Context, proposed TerminalIntent, _ time.Time) (TerminalIntent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.terminal[proposed.AttemptID]; ok {
		if existing.Status != proposed.Status || existing.ResultDigest != proposed.ResultDigest {
			return TerminalIntent{}, ErrStateConflict
		}
		return existing, nil
	}
	r.terminal[proposed.AttemptID] = proposed
	return proposed, nil
}

func (r *fakeRepository) UpdateAttemptStatus(_ context.Context, attemptID uuid.UUID, _ []AttemptStatus, to AttemptStatus, _ string, _ time.Time, _ *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status[attemptID] = to
	return nil
}

func (r *fakeRepository) CountAttempts(context.Context, uuid.UUID) (int, error) {
	return r.attemptCount, nil
}

type fakeAgent struct {
	mu          sync.Mutex
	candidate   bool
	envelope    mediapreview.MediaJobEnvelopeV1
	commits     []TerminalCommitRequest
	renewError  error
	reconciling int
	retries     int
	claimCalls  int
	snapshot    *JobSnapshot
}

func newFakeAgent(envelope mediapreview.MediaJobEnvelopeV1) *fakeAgent {
	return &fakeAgent{candidate: true, envelope: envelope}
}

func (a *fakeAgent) Readiness(context.Context) error { return nil }

func (a *fakeAgent) NextClaimable(context.Context) (uuid.UUID, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.candidate {
		return uuid.Nil, false, nil
	}
	a.candidate = false
	return a.envelope.JobID, true, nil
}

func (a *fakeAgent) Claim(_ context.Context, request ClaimRequest) (mediapreview.MediaJobEnvelopeV1, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.claimCalls++
	envelope := a.envelope
	envelope.AttemptID = request.AttemptID
	envelope.JobID = request.JobID
	envelope.LeaseExpiresAt = time.Now().UTC().Add(request.LeaseTTL)
	a.envelope = envelope
	return envelope, nil
}

func (a *fakeAgent) Renew(context.Context, LeaseRequest, time.Duration) (time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.renewError != nil {
		return time.Time{}, a.renewError
	}
	return time.Now().UTC().Add(time.Second), nil
}

func (a *fakeAgent) ScheduleRetry(context.Context, ScheduleRetryRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retries++
	return nil
}

func (a *fakeAgent) MarkReconciling(context.Context, LeaseRequest, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reconciling++
	return nil
}

func (a *fakeAgent) CommitTerminal(_ context.Context, request TerminalCommitRequest) (TerminalCommitResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.commits = append(a.commits, request)
	if request.TerminalStatus == "succeeded" {
		return TerminalCommitResult{JobStatus: "succeeded", BatchStatus: "completed", OperationStatus: "completed", TerminalEventID: request.EventID}, nil
	}
	return TerminalCommitResult{JobStatus: "dead", BatchStatus: "failed", OperationStatus: "failed", TerminalEventID: request.EventID}, nil
}

func (a *fakeAgent) Get(context.Context, uuid.UUID) (JobSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.snapshot != nil {
		return *a.snapshot, nil
	}
	if len(a.commits) > 0 {
		commit := a.commits[len(a.commits)-1]
		status := "succeeded"
		if commit.TerminalStatus == "failed" {
			status = "dead"
		}
		return JobSnapshot{
			JobStatus: status, AttemptID: commit.Lease.AttemptID, Fence: commit.Lease.Fence,
			ResultSchemaVersion: commit.ResultSchemaVersion, ResultDigest: commit.ResultDigest,
			TerminalEventID: commit.EventID,
		}, nil
	}
	return JobSnapshot{JobStatus: "running", AttemptID: a.envelope.AttemptID, Fence: a.envelope.Fence}, nil
}

func (a *fakeAgent) Close() error { return nil }

type fakeBusiness struct {
	mu               sync.Mutex
	result           FinalizeResultV1
	finalizeCalls    int
	queryCalls       int
	finalizeErrors   []error
	queryResults     []QueryFinalizationResultV1
	finalizeRequests []FinalizeRequestV1
	queryRequests    []QueryFinalizationRequestV1
}

func newFakeBusiness(result FinalizeResultV1) *fakeBusiness {
	return &fakeBusiness{result: result}
}

func (b *fakeBusiness) Readiness(context.Context) error { return nil }

func (b *fakeBusiness) Finalize(_ context.Context, request FinalizeRequestV1) (FinalizeResultV1, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.finalizeRequests = append(b.finalizeRequests, request)
	index := b.finalizeCalls
	b.finalizeCalls++
	if index < len(b.finalizeErrors) && b.finalizeErrors[index] != nil {
		return FinalizeResultV1{}, b.finalizeErrors[index]
	}
	result := b.result
	result.RequestID = request.RequestID
	result.CommandID = request.CommandID
	return result, nil
}

func (b *fakeBusiness) QueryFinalization(_ context.Context, request QueryFinalizationRequestV1) (QueryFinalizationResultV1, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queryRequests = append(b.queryRequests, request)
	index := b.queryCalls
	b.queryCalls++
	if index >= len(b.queryResults) {
		return QueryFinalizationResultV1{SchemaVersion: QueryFinalizationResultSchemaV1, RequestID: request.RequestID, Status: "not_found"}, nil
	}
	result := b.queryResults[index]
	result.RequestID = request.RequestID
	if result.Result != nil {
		result.Result.RequestID = request.RequestID
		result.Result.CommandID = request.CommandID
	}
	return result, nil
}

type fakeArtifacts struct {
	mu                  sync.Mutex
	receipt             mediapreview.ArtifactReceiptV1
	calls               int
	blockUntilCancelled bool
}

func (a *fakeArtifacts) GeneratePNG(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1) (mediapreview.ArtifactReceiptV1, error) {
	return a.execute(ctx, envelope)
}

func (a *fakeArtifacts) AssembleMP4(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1) (mediapreview.ArtifactReceiptV1, error) {
	return a.execute(ctx, envelope)
}

func (a *fakeArtifacts) execute(ctx context.Context, envelope mediapreview.MediaJobEnvelopeV1) (mediapreview.ArtifactReceiptV1, error) {
	a.mu.Lock()
	a.calls++
	block := a.blockUntilCancelled
	receipt := a.receipt
	a.mu.Unlock()
	if block {
		<-ctx.Done()
		return mediapreview.ArtifactReceiptV1{}, &mediapreview.ArtifactError{Code: mediapreview.ErrorCodeExecutionTimeout, Op: "fake_artifact"}
	}
	receipt.JobID = envelope.JobID
	receipt.AttemptID = envelope.AttemptID
	receipt.Fence = envelope.Fence
	return receipt, nil
}

type testIDGenerator struct{}

func (testIDGenerator) NewUUID() (uuid.UUID, error) { return uuid.NewV7() }

type testClock struct{}

func (testClock) Now() time.Time { return time.Now().UTC() }

func newTestProcessor(t *testing.T, repository *fakeRepository, agent *fakeAgent, business *fakeBusiness, artifacts *fakeArtifacts) *Processor {
	t.Helper()
	processor, err := NewProcessor(ProcessorConfig{
		WorkerID: "worker-test", PollInterval: 10 * time.Millisecond,
		LeaseTTL: 300 * time.Millisecond, HeartbeatInterval: 50 * time.Millisecond,
		AttemptTimeout: 2 * time.Second, MaxAttempts: 3,
		AgentCallTimeout: 10 * time.Millisecond, BusinessCallTimeout: 100 * time.Millisecond,
		MaxPNGBytes: 2 * 1024 * 1024, MaxMP4Bytes: 16 * 1024 * 1024,
		RetryBaseDelay: time.Millisecond, RetryMaxDelay: time.Second,
	}, repository, agent, business, artifacts, testIDGenerator{}, testClock{}, func(time.Duration) time.Duration { return time.Millisecond }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("create test Processor: %v", err)
	}
	return processor
}

func validTestEnvelope(t *testing.T, jobType mediapreview.JobType, fence int64) mediapreview.MediaJobEnvelopeV1 {
	t.Helper()
	createdAt := time.Now().UTC()
	envelope := mediapreview.MediaJobEnvelopeV1{
		SchemaVersion: mediapreview.EnvelopeSchemaVersionV1,
		JobID:         mustUUIDv7(t), BatchID: mustUUIDv7(t), OperationID: mustUUIDv7(t),
		SessionID: mustUUIDv7(t), UserID: mustUUIDv7(t), ProjectID: mustUUIDv7(t),
		JobType: jobType, ScopeDigest: strings.Repeat("1", 64),
		ArtifactRequestDigest: strings.Repeat("4", 64), AttemptID: mustUUIDv7(t), Fence: fence,
		LeaseExpiresAt: createdAt.Add(time.Second), CreatedAt: createdAt, DeadlineAt: createdAt.Add(3 * time.Second),
		SourceRef: mediapreview.SourceRefV1{
			SourceType: string(mediapreview.SourceTypePromptPreview), SourceID: mustUUIDv7(t), SourceVersion: 1,
			SourceDigest: strings.Repeat("2", 64), TargetLocalKey: "hero_image", TargetDigest: strings.Repeat("3", 64),
		},
		Target: mediapreview.TargetV1{
			AssetID: mustUUIDv7(t), AssetVersion: 1, PreparationID: mustUUIDv7(t),
			StagingObjectKey: "staging/asset/output.png",
		},
	}
	if jobType == mediapreview.JobTypeGeneratePNG {
		envelope.DefinitionVersion = mediapreview.DefinitionVersionGenerateMediaV3Preview1
		envelope.OutputProfile = mediapreview.OutputProfilePNG640x360V1
	} else {
		envelope.DefinitionVersion = mediapreview.DefinitionVersionAssembleOutputV3Preview1
		envelope.OutputProfile = mediapreview.OutputProfileMP4H264640x3602sV1
		envelope.SourceRef.SourceType = string(mediapreview.SourceTypeImageAsset)
		envelope.SourceRef.TargetLocalKey = ""
		envelope.SourceRef.TargetDigest = ""
		envelope.SourceRef.SourceObjectKey = "objects/asset/v1.png"
		envelope.Target.StagingObjectKey = "staging/asset/output.mp4"
	}
	if err := envelope.Validate(); err != nil {
		t.Fatalf("invalid test Envelope: %v", err)
	}
	return envelope
}

func validArtifactReceipt(envelope mediapreview.MediaJobEnvelopeV1) mediapreview.ArtifactReceiptV1 {
	return mediapreview.ArtifactReceiptV1{
		SchemaVersion: mediapreview.ArtifactReceiptSchemaVersionV1,
		JobID:         envelope.JobID, AttemptID: envelope.AttemptID, Fence: envelope.Fence,
		JobType: envelope.JobType, GeneratorVersion: mediapreview.GeneratorVersionPNG640x360V1,
		ArtifactRequestDigest: envelope.ArtifactRequestDigest, ObjectKey: envelope.Target.StagingObjectKey,
		ContentDigest: strings.Repeat("a", 64), SizeBytes: 4096, MIMEType: "image/png", Width: 640, Height: 360,
	}
}

func readyFinalizeResult(t *testing.T, envelope mediapreview.MediaJobEnvelopeV1) FinalizeResultV1 {
	t.Helper()
	return FinalizeResultV1{
		SchemaVersion: FinalizeResultSchemaV1, Disposition: "created",
		AssetRef: MediaAssetRefV1{
			AssetID: envelope.Target.AssetID, Version: 1, Status: "ready", MediaKind: "image",
			MIMEType: "image/png", ContentDigest: strings.Repeat("a", 64), SizeBytes: 4096,
		},
		FinalizationReceiptID: mustUUIDv7(t), CompletedAt: time.Now().UTC(),
	}
}

func mustUUIDv7(t *testing.T) uuid.UUID {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("create UUIDv7: %v", err)
	}
	return value
}

var _ Repository = (*fakeRepository)(nil)
var _ AgentClient = (*fakeAgent)(nil)
var _ BusinessClient = (*fakeBusiness)(nil)
