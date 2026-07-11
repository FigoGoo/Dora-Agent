package sessionruntime

import (
	"context"
	"encoding/json"
	"time"
)

type EnqueueResult struct {
	Input    SessionInputRecord
	Enqueued bool
}

type ClaimOptions struct {
	Fence       Fence
	ClaimTTL    time.Duration
	MaxAttempts int
}

type Failure struct {
	Code    string
	Message string
}

// Store groups the persistence operations required by the durable TurnLoop.
// Implementations guarantee that all mutating operations carrying a Fence
// reject an expired lease or a stale owner/token.
type Store interface {
	EnqueueInput(ctx context.Context, sessionID string, input SessionInput) (EnqueueResult, error)
	GetInput(ctx context.Context, inputID string) (SessionInputRecord, error)
	ListRunnableSessions(ctx context.Context, limit int) ([]string, error)
	ClaimNext(ctx context.Context, options ClaimOptions) (SessionInputRecord, error)
	MarkInputRunning(ctx context.Context, fence Fence, inputID string, lease time.Duration) (SessionInputRecord, error)
	RetryInput(ctx context.Context, fence Fence, inputID string, availableAt time.Time, failure Failure) (SessionInputRecord, error)
	ResolveInput(ctx context.Context, fence Fence, inputID string) (SessionInputRecord, error)
	DeadInput(ctx context.Context, fence Fence, inputID string, failure Failure) (SessionInputRecord, error)
	RecoverExpiredInputs(ctx context.Context, fence Fence) (int64, error)

	AcquireLease(ctx context.Context, sessionID, ownerID string, ttl time.Duration) (SessionRuntimeLease, error)
	RenewLease(ctx context.Context, fence Fence, ttl time.Duration) (SessionRuntimeLease, error)
	HandoffLease(ctx context.Context, fence Fence, newOwnerID string, ttl time.Duration) (SessionRuntimeLease, error)
	ReleaseLease(ctx context.Context, fence Fence) error
	ValidateFence(ctx context.Context, fence Fence) error

	GetOrCreateTurn(ctx context.Context, fence Fence, inputID string, spec TurnSpec) (SessionTurnRun, bool, error)
	GetTurn(ctx context.Context, turnID string) (SessionTurnRun, error)
	BeginTurn(ctx context.Context, fence Fence, turnID string) (SessionTurnRun, error)
	FreezeTurnContextMessageSeq(ctx context.Context, fence Fence, turnID string, throughSeq int64) (SessionTurnRun, error)
	FreezeTurnContextFromTerminalUserInputs(ctx context.Context, fence Fence, turnID string) (SessionTurnRun, error)
	SaveTurnCheckpoint(ctx context.Context, fence Fence, turnID, checkpointID string) (SessionTurnRun, error)
	SaveTurnOutput(ctx context.Context, fence Fence, turnID string, payload json.RawMessage, digest string) (SessionTurnRun, error)
	WaitForInterrupt(ctx context.Context, fence Fence, turnID, checkpointID string) (SessionTurnRun, error)
	RetryTurn(ctx context.Context, fence Fence, turnID string, failure Failure) (SessionTurnRun, error)
	RetryTurnAt(ctx context.Context, fence Fence, turnID string, availableAt time.Time, failure Failure) (SessionTurnRun, error)
	CommitTurn(ctx context.Context, fence Fence, turnID, outputDigest string) (SessionTurnRun, error)
	DeadTurn(ctx context.Context, fence Fence, turnID string, failure Failure) (SessionTurnRun, error)

	RequestContinuation(ctx context.Context, continuation ApprovalContinuation) (ApprovalContinuation, bool, error)
	GetContinuation(ctx context.Context, approvalID string, decisionVersion int) (ApprovalContinuation, error)
	ClaimContinuation(ctx context.Context, claim ContinuationClaim, ttl time.Duration) (ApprovalContinuation, error)
	ApplyContinuation(ctx context.Context, claim ContinuationClaim, commands []ApprovalCommandLedger) (ApprovalContinuation, error)
	FailContinuation(ctx context.Context, claim ContinuationClaim, failure Failure) (ApprovalContinuation, error)
	FallbackContinuation(ctx context.Context, approvalID string, decisionVersion int, expectedEpoch int64) (ApprovalContinuation, error)
	GetCommand(ctx context.Context, approvalID string, decisionVersion int, commandKind string) (ApprovalCommandLedger, error)
}
