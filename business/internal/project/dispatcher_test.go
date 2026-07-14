package project

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type dispatchRepository struct {
	outbox         SessionOutbox
	claimErr       error
	delivered      int
	retried        int
	dead           int
	deadCode       string
	receipt        EnsureSessionReceipt
	retryAvailable time.Time
}

func (repository *dispatchRepository) ClaimNext(_ context.Context, _ string, _, _ time.Time) (SessionOutbox, error) {
	return repository.outbox, repository.claimErr
}
func (repository *dispatchRepository) MarkDelivered(_ context.Context, _ SessionOutbox, receipt EnsureSessionReceipt, _ time.Time) error {
	repository.delivered++
	repository.receipt = receipt
	return nil
}
func (repository *dispatchRepository) MarkRetry(_ context.Context, _ SessionOutbox, availableAt, _ time.Time) error {
	repository.retried++
	repository.retryAvailable = availableAt
	return nil
}
func (repository *dispatchRepository) MarkDead(_ context.Context, _ SessionOutbox, code string, _ time.Time) error {
	repository.dead++
	repository.deadCode = code
	return nil
}

type dispatchClient struct {
	ensureResults []EnsureSessionReceipt
	ensureErrors  []error
	ensureCalls   int
	query         QuerySessionResult
	queryErr      error
	queryCalls    int
	order         []string
}

func (client *dispatchClient) Ensure(_ context.Context, _ EnsureSessionRequest) (EnsureSessionReceipt, error) {
	client.order = append(client.order, "ensure")
	index := client.ensureCalls
	client.ensureCalls++
	var result EnsureSessionReceipt
	var err error
	if index < len(client.ensureResults) {
		result = client.ensureResults[index]
	}
	if index < len(client.ensureErrors) {
		err = client.ensureErrors[index]
	}
	return result, err
}
func (client *dispatchClient) Query(_ context.Context, _ string, _ Digest) (QuerySessionResult, error) {
	client.order = append(client.order, "query")
	client.queryCalls++
	return client.query, client.queryErr
}

type dispatchRevealer struct {
	prompt string
	err    error
}

func (revealer dispatchRevealer) Reveal(_ context.Context, _ EncryptedPayload) (string, error) {
	return revealer.prompt, revealer.err
}

type panicDispatchRevealer struct{}

func (panicDispatchRevealer) Reveal(_ context.Context, _ EncryptedPayload) (string, error) {
	panic("v2 outbox must not be revealed by the v1 prompt revealer")
}

type claimedV2DispatcherStub struct {
	calls     int
	outbox    SessionOutbox
	claimedAt time.Time
	err       error
}

func (dispatcher *claimedV2DispatcherStub) DispatchClaimedV2(_ context.Context, outbox SessionOutbox, claimedAt time.Time) error {
	dispatcher.calls++
	dispatcher.outbox = outbox
	dispatcher.claimedAt = claimedAt
	return dispatcher.err
}

func processingOutboxForDispatcher(t *testing.T, prompt string) SessionOutbox {
	t.Helper()
	projectID, _ := uuid.NewV7()
	ownerID, _ := uuid.NewV7()
	commandID, _ := uuid.NewV7()
	now := time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)
	digest := SHA256Digest([]byte(prompt))
	requestDigest, err := CalculateEnsureSessionRequestDigest(projectID.String(), ownerID.String(), prompt != "", digest)
	if prompt == "" {
		requestDigest, err = CalculateEnsureSessionRequestDigest(projectID.String(), ownerID.String(), false, Digest{})
	}
	if err != nil {
		t.Fatal(err)
	}
	leaseOwner := "business-1"
	leaseUntil := now.Add(time.Minute)
	outbox := SessionOutbox{
		ID: commandID.String(), EventType: EnsureSessionEventType, SchemaVersion: EnsureSessionSchemaVersion,
		AggregateID: projectID.String(), OwnerUserID: ownerID.String(), RequestDigest: requestDigest,
		HasInitialPrompt: prompt != "", Status: OutboxStatusProcessing, AvailableAt: now,
		LeaseOwner: &leaseOwner, LeaseVersion: 1, LeaseExpiresAt: &leaseUntil, AttemptCount: 1, MaxAttempts: 3,
		CreatedAt: now, UpdatedAt: now,
	}
	if prompt != "" {
		outbox.EncryptedPayload = &EncryptedPayload{
			Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("123456789012"),
			Ciphertext: []byte("ciphertext-with-authentication-tag"), PayloadDigest: digest,
		}
	}
	if err := outbox.Validate(); err != nil {
		t.Fatalf("validate outbox: %v", err)
	}
	return outbox
}

func newDispatcherForTest(t *testing.T, repository *dispatchRepository, client *dispatchClient, revealer PromptRevealer) *Dispatcher {
	t.Helper()
	requestID, _ := uuid.NewV7()
	dispatcher, err := NewDispatcher(repository, client, revealer,
		serviceClock{now: time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)},
		&serviceIDGenerator{values: []string{requestID.String()}},
		DispatcherConfig{LeaseOwner: "business-1", LeaseDuration: time.Minute, RetryDelay: 5 * time.Second})
	if err != nil {
		t.Fatalf("create dispatcher: %v", err)
	}
	return dispatcher
}

func processingV2OutboxForDispatcher(t *testing.T) SessionOutbox {
	t.Helper()
	projectID, _ := uuid.NewV7()
	ownerID, _ := uuid.NewV7()
	commandID, _ := uuid.NewV7()
	resolutionID, _ := uuid.NewV7()
	now := time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)
	leaseOwner := "business-1"
	leaseUntil := now.Add(time.Minute)
	bindingSetVersion := int64(1)
	outbox := SessionOutbox{
		ID: commandID.String(), EventType: EnsureSessionEventType, SchemaVersion: EnsureSessionSchemaVersionV2,
		AggregateID: projectID.String(), OwnerUserID: ownerID.String(), RequestDigest: SHA256Digest([]byte("request-v2")),
		HasInitialPrompt: false,
		EncryptedPayload: &EncryptedPayload{
			Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v2", Nonce: []byte("123456789012"),
			Ciphertext: []byte("encrypted-bootstrap-with-auth-tag"), PayloadDigest: SHA256Digest([]byte("payload-v2")),
		},
		SkillSnapshotDigest: SHA256Digest([]byte("snapshot-v2")), SkillCount: 1,
		BindingSetVersion: &bindingSetVersion, ResolutionID: stringPointer(resolutionID.String()),
		Status: OutboxStatusProcessing, AvailableAt: now,
		LeaseOwner: &leaseOwner, LeaseVersion: 1, LeaseExpiresAt: &leaseUntil, AttemptCount: 1, MaxAttempts: 3,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := outbox.Validate(); err != nil {
		t.Fatalf("validate v2 outbox: %v", err)
	}
	return outbox
}

func stringPointer(value string) *string { return &value }

func receiptForOutbox(t *testing.T, outbox SessionOutbox, withInput bool) EnsureSessionReceipt {
	t.Helper()
	sessionID, _ := uuid.NewV7()
	receipt := EnsureSessionReceipt{CommandID: outbox.ID, RequestDigest: outbox.RequestDigest, SessionID: sessionID.String()}
	if withInput {
		inputID, _ := uuid.NewV7()
		inputValue := inputID.String()
		receipt.InputID = &inputValue
	}
	return receipt
}

func TestDispatcherDeliversFirstEnsureReceipt(t *testing.T) {
	outbox := processingOutboxForDispatcher(t, "prompt")
	repository := &dispatchRepository{outbox: outbox}
	receipt := receiptForOutbox(t, outbox, true)
	client := &dispatchClient{ensureResults: []EnsureSessionReceipt{receipt}}
	dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{prompt: "prompt"})

	if err := dispatcher.DispatchNext(context.Background()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if repository.delivered != 1 || repository.retried != 0 || repository.dead != 0 || client.queryCalls != 0 {
		t.Fatalf("unexpected dispatch effects: repository=%+v client=%+v", repository, client)
	}
}

func TestDispatcherRoutesV2OutboxWithoutCallingV1Dependencies(t *testing.T) {
	outbox := processingV2OutboxForDispatcher(t)
	repository := &dispatchRepository{outbox: outbox}
	client := &dispatchClient{}
	v2 := &claimedV2DispatcherStub{}
	requestID, _ := uuid.NewV7()
	dispatcher, err := NewDispatcherWithV2(
		repository, client, panicDispatchRevealer{},
		serviceClock{now: outbox.CreatedAt}, &serviceIDGenerator{values: []string{requestID.String()}},
		DispatcherConfig{LeaseOwner: "business-1", LeaseDuration: time.Minute, RetryDelay: 5 * time.Second},
		v2,
	)
	if err != nil {
		t.Fatalf("create v2 dispatcher: %v", err)
	}

	if err := dispatcher.DispatchNext(context.Background()); err != nil {
		t.Fatalf("dispatch v2 outbox: %v", err)
	}
	if v2.calls != 1 || v2.outbox.ID != outbox.ID || !v2.claimedAt.Equal(outbox.CreatedAt) {
		t.Fatalf("v2 outbox was not routed exactly once: stub=%+v", v2)
	}
	if client.ensureCalls != 0 || client.queryCalls != 0 || repository.delivered+repository.retried+repository.dead != 0 {
		t.Fatalf("v2 outbox leaked into v1 path: client=%+v repository=%+v", client, repository)
	}
}

func TestDispatcherUnknownOutcomeQueriesBeforeOriginalRetry(t *testing.T) {
	outbox := processingOutboxForDispatcher(t, "")
	repository := &dispatchRepository{outbox: outbox}
	receipt := receiptForOutbox(t, outbox, false)
	client := &dispatchClient{
		ensureResults: []EnsureSessionReceipt{{}, receipt},
		ensureErrors:  []error{ErrAgentSessionUnavailable, nil},
		query:         QuerySessionResult{Status: QueryStatusNotFound},
	}
	dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{})

	if err := dispatcher.DispatchNext(context.Background()); err != nil {
		t.Fatalf("dispatch unknown outcome: %v", err)
	}
	if len(client.order) != 3 || client.order[0] != "ensure" || client.order[1] != "query" || client.order[2] != "ensure" || repository.delivered != 1 {
		t.Fatalf("unknown outcome order drifted: order=%v repository=%+v", client.order, repository)
	}
}

func TestDispatcherDeadlineUnknownOutcomeStillQueriesBeforeDecision(t *testing.T) {
	outbox := processingOutboxForDispatcher(t, "")
	repository := &dispatchRepository{outbox: outbox}
	receipt := receiptForOutbox(t, outbox, false)
	client := &dispatchClient{
		ensureErrors: []error{context.DeadlineExceeded},
		query:        QuerySessionResult{Status: QueryStatusCompleted, Receipt: &receipt},
	}
	dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{})

	if err := dispatcher.DispatchNext(context.Background()); err != nil {
		t.Fatalf("dispatch deadline unknown outcome: %v", err)
	}
	if len(client.order) != 2 || client.order[0] != "ensure" || client.order[1] != "query" || repository.delivered != 1 {
		t.Fatalf("deadline bypassed Query recovery: order=%v repository=%+v", client.order, repository)
	}
}

func TestDispatcherRecoveredAttemptQueriesBeforeAnyWriteOrPromptReveal(t *testing.T) {
	outbox := processingOutboxForDispatcher(t, "prompt")
	outbox.RecoveryRequired = true
	repository := &dispatchRepository{outbox: outbox}
	receipt := receiptForOutbox(t, outbox, true)
	client := &dispatchClient{query: QuerySessionResult{Status: QueryStatusCompleted, Receipt: &receipt}}
	dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{err: errors.New("must not reveal before query")})

	if err := dispatcher.DispatchNext(context.Background()); err != nil {
		t.Fatalf("dispatch recovered attempt: %v", err)
	}
	if len(client.order) != 1 || client.order[0] != "query" || client.ensureCalls != 0 || repository.delivered != 1 {
		t.Fatalf("recovered attempt wrote before Query: order=%v repository=%+v", client.order, repository)
	}
}

func TestDispatcherRecoveredNotFoundUsesOriginalCommandAfterQuery(t *testing.T) {
	outbox := processingOutboxForDispatcher(t, "")
	outbox.RecoveryRequired = true
	repository := &dispatchRepository{outbox: outbox}
	receipt := receiptForOutbox(t, outbox, false)
	client := &dispatchClient{
		query: QuerySessionResult{Status: QueryStatusNotFound}, ensureResults: []EnsureSessionReceipt{receipt},
	}
	dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{})

	if err := dispatcher.DispatchNext(context.Background()); err != nil {
		t.Fatalf("dispatch recovered not found: %v", err)
	}
	if len(client.order) != 2 || client.order[0] != "query" || client.order[1] != "ensure" || repository.delivered != 1 {
		t.Fatalf("recovered not-found order drifted: order=%v repository=%+v", client.order, repository)
	}
}

func TestDispatcherCompletedQueryReplaysReceiptWithoutSecondWrite(t *testing.T) {
	outbox := processingOutboxForDispatcher(t, "prompt")
	repository := &dispatchRepository{outbox: outbox}
	receipt := receiptForOutbox(t, outbox, true)
	client := &dispatchClient{
		ensureErrors: []error{ErrAgentSessionUnavailable},
		query:        QuerySessionResult{Status: QueryStatusCompleted, Receipt: &receipt},
	}
	dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{prompt: "prompt"})

	if err := dispatcher.DispatchNext(context.Background()); err != nil {
		t.Fatalf("dispatch query replay: %v", err)
	}
	if client.ensureCalls != 1 || client.queryCalls != 1 || repository.delivered != 1 {
		t.Fatalf("completed query retried write: client=%+v repository=%+v", client, repository)
	}
}

func TestDispatcherRejectsReceiptAndPromptDrift(t *testing.T) {
	t.Run("receipt", func(t *testing.T) {
		outbox := processingOutboxForDispatcher(t, "")
		repository := &dispatchRepository{outbox: outbox}
		bad := receiptForOutbox(t, outbox, true)
		client := &dispatchClient{ensureResults: []EnsureSessionReceipt{bad}}
		dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{})
		if err := dispatcher.DispatchNext(context.Background()); err != nil {
			t.Fatalf("dispatch invalid receipt: %v", err)
		}
		if repository.deadCode != dispatchErrorReceipt || repository.delivered != 0 {
			t.Fatalf("invalid receipt was accepted: %+v", repository)
		}
	})

	t.Run("prompt", func(t *testing.T) {
		outbox := processingOutboxForDispatcher(t, "prompt")
		repository := &dispatchRepository{outbox: outbox}
		client := &dispatchClient{}
		dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{prompt: "tampered"})
		if err := dispatcher.DispatchNext(context.Background()); err != nil {
			t.Fatalf("dispatch tampered prompt: %v", err)
		}
		if repository.deadCode != dispatchErrorPromptReveal || client.ensureCalls != 0 {
			t.Fatalf("tampered prompt reached Agent: repository=%+v client=%+v", repository, client)
		}
	})
}

func TestDispatcherSchedulesRetryAndStopsAtAttemptBudget(t *testing.T) {
	outbox := processingOutboxForDispatcher(t, "")
	repository := &dispatchRepository{outbox: outbox}
	client := &dispatchClient{ensureErrors: []error{ErrAgentSessionUnavailable}, queryErr: ErrAgentSessionUnavailable}
	dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{})
	if err := dispatcher.DispatchNext(context.Background()); err != nil {
		t.Fatalf("dispatch retry: %v", err)
	}
	if repository.retried != 1 || repository.retryAvailable.IsZero() {
		t.Fatalf("transient failure did not schedule retry: %+v", repository)
	}

	outbox.AttemptCount = outbox.MaxAttempts
	repository = &dispatchRepository{outbox: outbox}
	client = &dispatchClient{ensureErrors: []error{ErrAgentSessionUnavailable}, queryErr: ErrAgentSessionUnavailable}
	dispatcher = newDispatcherForTest(t, repository, client, dispatchRevealer{})
	if err := dispatcher.DispatchNext(context.Background()); err != nil {
		t.Fatalf("dispatch exhausted: %v", err)
	}
	if repository.deadCode != dispatchErrorAttemptsExceeded || repository.retried != 0 {
		t.Fatalf("attempt budget was exceeded: %+v", repository)
	}
}

func TestDispatcherPreservesCancellationWithoutOverwritingLease(t *testing.T) {
	outbox := processingOutboxForDispatcher(t, "")
	repository := &dispatchRepository{outbox: outbox}
	client := &dispatchClient{ensureErrors: []error{context.Canceled}}
	dispatcher := newDispatcherForTest(t, repository, client, dispatchRevealer{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := dispatcher.DispatchNext(ctx)
	if !errors.Is(err, context.Canceled) || repository.delivered+repository.retried+repository.dead != 0 {
		t.Fatalf("cancellation overwrote lease state: err=%v repository=%+v", err, repository)
	}
}
