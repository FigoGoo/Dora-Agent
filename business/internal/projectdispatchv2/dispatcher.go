// Package projectdispatchv2 实现完整加密 Session Bootstrap v2 Outbox 的独立派发状态机。
package projectdispatchv2

import (
	"context"
	"errors"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"github.com/google/uuid"
)

const (
	dispatchErrorV2Conflict         = "AGENT_SESSION_V2_COMMAND_CONFLICT"
	dispatchErrorV2Invalid          = "AGENT_SESSION_V2_COMMAND_INVALID"
	dispatchErrorV2Receipt          = "AGENT_SESSION_V2_RECEIPT_INVALID"
	dispatchErrorV2Payload          = "SESSION_BOOTSTRAP_V2_DECRYPT_FAILED"
	dispatchErrorV2AttemptsExceeded = "AGENT_SESSION_V2_ATTEMPTS_EXCEEDED"
)

// Receipt 是 Agent v2 首次完成或重放的安全回执，不包含 Runtime Content。
type Receipt struct {
	CommandID           string
	SessionID           string
	InputID             *string
	SkillSnapshotDigest projectskillbinding.Digest
	SkillCount          int32
	Replayed            bool
}

// QueryResult 是 QueryProjectSessionCommandV2 的严格三态结果。
type QueryResult struct {
	Status  project.QueryStatus
	Receipt *Receipt
}

// AgentClient 是 Dispatcher 消费 Agent v2 RPC 的最小边界。
type AgentClient interface {
	EnsureV2(ctx context.Context, requestID string, payload projectskillbinding.SessionBootstrapOutboxPayloadV2) (Receipt, error)
	QueryV2(ctx context.Context, commandID string, expectedDigest projectskillbinding.Digest) (QueryResult, error)
}

// PayloadRevealer 使用完整 Outbox envelope 和 Canonical AAD 做认证解密。
type PayloadRevealer interface {
	Open(ctx context.Context, envelope projectskillbinding.EncryptedEnvelopeV2, aad []byte) ([]byte, error)
}

// Config 冻结 v2 重试间隔和 Business Producer limits。
type Config struct {
	RetryDelay time.Duration
	Limits     projectskillbinding.LimitsV1
}

// Dispatcher 只处理共享 v1/v2 Claim 已经领取的 v2 命令。
type Dispatcher struct {
	repository project.DispatchRepository
	client     AgentClient
	revealer   PayloadRevealer
	idgen      project.IDGenerator
	config     Config
}

// New 校验双版本派发依赖和 Producer limits。
func New(repository project.DispatchRepository, client AgentClient, revealer PayloadRevealer, idgen project.IDGenerator, config Config) (*Dispatcher, error) {
	if repository == nil || client == nil || revealer == nil || idgen == nil || config.RetryDelay <= 0 {
		return nil, errors.New("create Project Session v2 dispatcher: required dependency is missing")
	}
	if err := config.Limits.Validate(); err != nil {
		return nil, errors.New("create Project Session v2 dispatcher: limits are invalid")
	}
	return &Dispatcher{repository: repository, client: client, revealer: revealer, idgen: idgen, config: config}, nil
}

// DispatchClaimedV2 遵循 Ensure unknown 后 Query、只有 not_found 才重发同一 payload 的固定顺序。
func (dispatcher *Dispatcher) DispatchClaimedV2(ctx context.Context, outbox project.SessionOutbox, claimedAt time.Time) error {
	if err := outbox.Validate(); err != nil || outbox.SchemaVersion != project.EnsureSessionSchemaVersionV2 ||
		outbox.Status != project.OutboxStatusProcessing {
		return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2Invalid, claimedAt)
	}
	if outbox.AttemptCount > outbox.MaxAttempts {
		return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2AttemptsExceeded, claimedAt)
	}
	if outbox.RecoveryRequired {
		return dispatcher.recoverUnknown(ctx, outbox, nil, claimedAt)
	}
	payload, err := dispatcher.openPayload(ctx, outbox)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2Payload, claimedAt)
	}
	receipt, ensureErr := dispatcher.ensure(ctx, payload)
	if ensureErr == nil {
		return dispatcher.commitReceipt(ctx, outbox, &payload, receipt, claimedAt)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(ensureErr, project.ErrAgentSessionConflict) {
		return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2Conflict, claimedAt)
	}
	if errors.Is(ensureErr, project.ErrAgentSessionInvalid) {
		return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2Invalid, claimedAt)
	}
	return dispatcher.recoverUnknown(ctx, outbox, &payload, claimedAt)
}

func (dispatcher *Dispatcher) recoverUnknown(ctx context.Context, outbox project.SessionOutbox, payload *projectskillbinding.SessionBootstrapOutboxPayloadV2, now time.Time) error {
	expectedDigest := toBindingDigest(outbox.RequestDigest)
	query, err := dispatcher.client.QueryV2(ctx, outbox.ID, expectedDigest)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return dispatcher.retryOrDead(ctx, outbox, now)
	}
	switch query.Status {
	case project.QueryStatusCompleted:
		if query.Receipt == nil {
			return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2Receipt, now)
		}
		return dispatcher.commitReceipt(ctx, outbox, payload, *query.Receipt, now)
	case project.QueryStatusConflict:
		return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2Conflict, now)
	case project.QueryStatusNotFound:
		if payload == nil {
			opened, openErr := dispatcher.openPayload(ctx, outbox)
			if openErr != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2Payload, now)
			}
			payload = &opened
		}
		receipt, ensureErr := dispatcher.ensure(ctx, *payload)
		if ensureErr == nil {
			return dispatcher.commitReceipt(ctx, outbox, payload, receipt, now)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(ensureErr, project.ErrAgentSessionConflict) || errors.Is(ensureErr, project.ErrAgentSessionInvalid) {
			code := dispatchErrorV2Invalid
			if errors.Is(ensureErr, project.ErrAgentSessionConflict) {
				code = dispatchErrorV2Conflict
			}
			return dispatcher.repository.MarkDead(ctx, outbox, code, now)
		}
		return dispatcher.retryOrDead(ctx, outbox, now)
	default:
		return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2Receipt, now)
	}
}

func (dispatcher *Dispatcher) openPayload(ctx context.Context, outbox project.SessionOutbox) (projectskillbinding.SessionBootstrapOutboxPayloadV2, error) {
	if outbox.EncryptedPayload == nil {
		return projectskillbinding.SessionBootstrapOutboxPayloadV2{}, projectskillbinding.ErrContentProtection
	}
	aad, err := projectskillbinding.CanonicalOutboxAADV2(projectskillbinding.OutboxAADV2{
		SchemaVersion: projectskillbinding.OutboxPayloadSchemaVersionV2,
		CommandID:     outbox.ID, ProjectID: outbox.AggregateID, OwnerUserID: outbox.OwnerUserID,
		RequestDigest: outbox.RequestDigest.Hex(), SkillSnapshotDigest: outbox.SkillSnapshotDigest.Hex(),
	})
	if err != nil {
		return projectskillbinding.SessionBootstrapOutboxPayloadV2{}, err
	}
	plaintext, err := dispatcher.revealer.Open(ctx, projectskillbinding.EncryptedEnvelopeV2{
		Algorithm: outbox.EncryptedPayload.Algorithm, KeyVersion: outbox.EncryptedPayload.KeyVersion,
		Nonce:            append([]byte(nil), outbox.EncryptedPayload.Nonce...),
		CiphertextAndTag: append([]byte(nil), outbox.EncryptedPayload.Ciphertext...),
	}, aad)
	if err != nil {
		return projectskillbinding.SessionBootstrapOutboxPayloadV2{}, projectskillbinding.ErrContentProtection
	}
	return projectskillbinding.ParseCanonicalOutboxPayloadV2(plaintext, projectskillbinding.OutboxExpectedV2{
		CommandID: outbox.ID, ProjectID: outbox.AggregateID, OwnerUserID: outbox.OwnerUserID,
		RequestDigest: toBindingDigest(outbox.RequestDigest), SnapshotDigest: toBindingDigest(outbox.SkillSnapshotDigest),
		PayloadDigest: toBindingDigest(outbox.EncryptedPayload.PayloadDigest), SkillCount: int(outbox.SkillCount),
	}, dispatcher.config.Limits)
}

func (dispatcher *Dispatcher) ensure(ctx context.Context, payload projectskillbinding.SessionBootstrapOutboxPayloadV2) (Receipt, error) {
	requestID, err := dispatcher.idgen.New()
	if err != nil {
		return Receipt{}, project.ErrAgentSessionUnavailable
	}
	return dispatcher.client.EnsureV2(ctx, requestID, payload)
}

func (dispatcher *Dispatcher) commitReceipt(ctx context.Context, outbox project.SessionOutbox, payload *projectskillbinding.SessionBootstrapOutboxPayloadV2, receipt Receipt, now time.Time) error {
	promptPresent := outbox.HasInitialPrompt
	if payload != nil {
		promptPresent = payload.PromptPresent
	}
	if receipt.CommandID != outbox.ID || !isUUIDv7(receipt.SessionID) ||
		receipt.SkillSnapshotDigest != toBindingDigest(outbox.SkillSnapshotDigest) || receipt.SkillCount != outbox.SkillCount ||
		promptPresent != (receipt.InputID != nil) || (receipt.InputID != nil && !isUUIDv7(*receipt.InputID)) {
		return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2Receipt, now)
	}
	return dispatcher.repository.MarkDelivered(ctx, outbox, project.EnsureSessionReceipt{
		CommandID: outbox.ID, RequestDigest: outbox.RequestDigest, SessionID: receipt.SessionID,
		InputID: cloneString(receipt.InputID), Replayed: receipt.Replayed,
		SkillSnapshotDigest: outbox.SkillSnapshotDigest, SkillCount: outbox.SkillCount,
	}, now)
}

func (dispatcher *Dispatcher) retryOrDead(ctx context.Context, outbox project.SessionOutbox, now time.Time) error {
	if outbox.AttemptCount >= outbox.MaxAttempts {
		return dispatcher.repository.MarkDead(ctx, outbox, dispatchErrorV2AttemptsExceeded, now)
	}
	return dispatcher.repository.MarkRetry(ctx, outbox, now.Add(dispatcher.config.RetryDelay), now)
}

func toBindingDigest(value project.Digest) projectskillbinding.Digest {
	var result projectskillbinding.Digest
	copy(result[:], value[:])
	return result
}

func isUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.String() == value
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

var _ project.ClaimedV2Dispatcher = (*Dispatcher)(nil)
