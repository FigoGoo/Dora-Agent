package planstoryboardruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
)

// Recovery 是 Processor 在 prepared/unknown 边界后消费的单步恢复端口。
// 每次调用只允许一次 Query；只有权威 not_found 才可原子预留并执行一次同键 Save。
type Recovery interface {
	Recover(context.Context, Claim, ToolReceiptSnapshot) error
}

// RecoveryCoordinator 以 PostgreSQL prepared Receipt 为命令真源执行 Query/defer/有界同键重发。
type RecoveryCoordinator struct {
	store    planstoryboard.BusinessDraftStore
	receipts ToolReceiptStore
	clock    Clock
}

// NewRecoveryCoordinator 创建不包含循环和内存重试预算的恢复协调器。
func NewRecoveryCoordinator(
	store planstoryboard.BusinessDraftStore,
	receipts ToolReceiptStore,
	clock Clock,
) (*RecoveryCoordinator, error) {
	if store == nil || receipts == nil || clock == nil {
		return nil, fmt.Errorf("create plan storyboard recovery coordinator: dependencies are required")
	}
	return &RecoveryCoordinator{store: store, receipts: receipts, clock: clock}, nil
}

// Recover 查询原 BusinessCommand；未消除歧义时返回 ErrRecoveryDeferred，绝不伪造 Tool 终态。
func (r *RecoveryCoordinator) Recover(ctx context.Context, claim Claim, snapshot ToolReceiptSnapshot) error {
	if r == nil || r.store == nil || r.receipts == nil || r.clock == nil || ValidateClaim(claim) != nil {
		return ErrInvalidClaim
	}
	command, recovery, err := validateRecoverySnapshot(claim, snapshot)
	if err != nil {
		return err
	}
	identity := toolReceiptIdentity(RuntimeContextFromClaim(claim))
	status, resource, queryErr := r.store.QueryStoryboardDraftCommand(ctx, command)
	if queryErr != nil {
		if terminal, ok := deterministicBusinessFailure(command.TrustedContext, queryErr); ok {
			return r.freeze(ctx, identity, snapshot.RequestDigest, command.TrustedContext, terminal)
		}
		return ErrRecoveryDeferred
	}
	switch status {
	case "completed":
		if resource == nil || planstoryboard.ValidateResourceForCommand(*resource, command) != nil {
			return ErrReceiptConflict
		}
		return r.freeze(ctx, identity, snapshot.RequestDigest, command.TrustedContext, completedRecoveryResult(command, *resource, r.clock.Now().UTC()))
	case "conflict":
		terminal, _ := deterministicBusinessFailure(command.TrustedContext, planstoryboard.ErrBusinessConflict)
		return r.freeze(ctx, identity, snapshot.RequestDigest, command.TrustedContext, terminal)
	case "not_found":
		// prepared 表示进程可能在 Save 前后崩溃；权威 not_found 消除原调用歧义后，
		// 先进入持久化 unknown/recovery 阶段，再原子消费同键重发预算。
		if snapshot.Stage == ToolReceiptBusinessPrepared {
			if markErr := r.receipts.MarkToolBusinessUnknown(ctx, identity, snapshot.RequestDigest); markErr != nil {
				return markErr
			}
		}
		reserved, execute, reserveErr := r.receipts.ReserveToolCommandResend(ctx, identity, snapshot.RequestDigest, recovery)
		if reserveErr != nil {
			return reserveErr
		}
		if !execute {
			if err := validateReservedRecovery(command, reserved); err != nil {
				return err
			}
			// 较旧快照看到已被并发调用预留的预算时，必须等待首写者完成 Save；
			// 只有本次 Query 基于同一已耗尽 attempts 再次得到权威 not_found，才能安全终结 HOL。
			if recovery.ResendAttempts < reserved.ResendAttempts {
				return ErrRecoveryDeferred
			}
			if reserved.ResendExhausted {
				return ErrRecoveryExhausted
			}
			return ErrReceiptConflict
		}
		if err := validateReservedRecovery(command, reserved); err != nil {
			return err
		}
		disposition, saved, saveErr := r.store.SaveStoryboardDraft(ctx, command)
		if saveErr != nil {
			if errors.Is(saveErr, planstoryboard.ErrBusinessUnknownOutcome) || errors.Is(saveErr, planstoryboard.ErrBusinessTechnical) ||
				errors.Is(saveErr, context.Canceled) || errors.Is(saveErr, context.DeadlineExceeded) {
				if markErr := r.receipts.MarkToolBusinessUnknown(ctx, identity, snapshot.RequestDigest); markErr != nil {
					return markErr
				}
				return ErrRecoveryDeferred
			}
			if terminal, ok := deterministicBusinessFailure(command.TrustedContext, saveErr); ok {
				return r.freeze(ctx, identity, snapshot.RequestDigest, command.TrustedContext, terminal)
			}
			return ErrRecoveryDeferred
		}
		if disposition != planstoryboard.SaveDispositionCreated && disposition != planstoryboard.SaveDispositionReplayed {
			return ErrReceiptConflict
		}
		if err := planstoryboard.ValidateResourceForCommand(saved, command); err != nil {
			return ErrReceiptConflict
		}
		return r.freeze(ctx, identity, snapshot.RequestDigest, command.TrustedContext, completedRecoveryResult(command, saved, r.clock.Now().UTC()))
	default:
		return ErrRecoveryDeferred
	}
}

func validateRecoverySnapshot(claim Claim, snapshot ToolReceiptSnapshot) (planstoryboard.DraftCommand, planstoryboard.RecoveryDeferred, error) {
	if snapshot.Stage != ToolReceiptBusinessPrepared && snapshot.Stage != ToolReceiptBusinessUnknown ||
		snapshot.PreparedCommand == nil || !canonicalSHA256.MatchString(snapshot.RequestDigest) ||
		!canonicalSHA256.MatchString(snapshot.PreparedCommandDigest) || !canonicalSHA256.MatchString(snapshot.ContentDigest) ||
		len(snapshot.ResultJSON) != 0 || snapshot.ResultDigest != "" {
		return planstoryboard.DraftCommand{}, planstoryboard.RecoveryDeferred{}, ErrReceiptConflict
	}
	command := *snapshot.PreparedCommand
	trusted := CoreContextFromRuntime(RuntimeContextFromClaim(claim))
	if command.TrustedContext != trusted {
		return planstoryboard.DraftCommand{}, planstoryboard.RecoveryDeferred{}, ErrReceiptConflict
	}
	commandDigest, err := digestPreparedCommand(command)
	if err != nil || commandDigest != snapshot.PreparedCommandDigest {
		return planstoryboard.DraftCommand{}, planstoryboard.RecoveryDeferred{}, ErrReceiptConflict
	}
	contentDigest, err := planstoryboard.ContentDigest(command.Content)
	if err != nil || contentDigest != snapshot.ContentDigest {
		return planstoryboard.DraftCommand{}, planstoryboard.RecoveryDeferred{}, ErrReceiptConflict
	}
	requestDigest, err := planstoryboard.SaveRequestDigest(command)
	if err != nil || requestDigest != command.RequestDigest {
		return planstoryboard.DraftCommand{}, planstoryboard.RecoveryDeferred{}, ErrReceiptConflict
	}
	recovery := planstoryboard.RecoveryDeferred{
		ToolCallID: command.TrustedContext.ToolCallID, BusinessCommandID: command.TrustedContext.BusinessCommandID,
		RequestDigest: command.RequestDigest, ContentDigest: contentDigest, Command: command, ResendLimit: BusinessResendLimit,
	}
	if snapshot.Recovery != nil {
		recovery = *snapshot.Recovery
		if err := validateRecoveryIdentity(command, recovery); err != nil {
			return planstoryboard.DraftCommand{}, planstoryboard.RecoveryDeferred{}, err
		}
	}
	return command, recovery, nil
}

func validateReservedRecovery(command planstoryboard.DraftCommand, recovery planstoryboard.RecoveryDeferred) error {
	if err := validateRecoveryIdentity(command, recovery); err != nil {
		return err
	}
	if recovery.ResendAttempts < 1 || recovery.ResendAttempts > recovery.ResendLimit || recovery.ResendLimit != BusinessResendLimit {
		return ErrReceiptConflict
	}
	return nil
}

func validateRecoveryIdentity(command planstoryboard.DraftCommand, recovery planstoryboard.RecoveryDeferred) error {
	contentDigest, err := planstoryboard.ContentDigest(command.Content)
	if err != nil || recovery.ToolCallID != command.TrustedContext.ToolCallID ||
		recovery.BusinessCommandID != command.TrustedContext.BusinessCommandID || recovery.RequestDigest != command.RequestDigest ||
		recovery.ContentDigest != contentDigest || recovery.Command.TrustedContext != command.TrustedContext ||
		recovery.Command.RequestDigest != command.RequestDigest || recovery.ResendAttempts < 0 ||
		recovery.ResendLimit != BusinessResendLimit || recovery.ResendAttempts > recovery.ResendLimit ||
		recovery.ResendExhausted != (recovery.ResendAttempts >= recovery.ResendLimit) {
		return ErrReceiptConflict
	}
	recoveryDigest, digestErr := digestPreparedCommand(recovery.Command)
	commandDigest, commandDigestErr := digestPreparedCommand(command)
	if digestErr != nil || commandDigestErr != nil || recoveryDigest != commandDigest {
		return ErrReceiptConflict
	}
	return nil
}

func (r *RecoveryCoordinator) freeze(
	ctx context.Context,
	identity ToolReceiptIdentity,
	requestDigest string,
	trusted planstoryboard.TrustedContext,
	result planstoryboard.Result,
) error {
	if err := planstoryboard.ValidateTerminalResult(result, trusted); err != nil {
		return ErrReceiptConflict
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("freeze plan storyboard recovery result: %w", err)
	}
	return r.receipts.FreezeToolResult(ctx, identity, requestDigest, ToolReceiptStage(result.Status), encoded, digestBytes(encoded))
}

func completedRecoveryResult(command planstoryboard.DraftCommand, resource planstoryboard.Resource, now time.Time) planstoryboard.Result {
	card := &planstoryboard.Card{
		SchemaVersion: planstoryboard.CardSchemaVersion, StoryboardPreviewID: resource.StoryboardPreviewID,
		ProjectID: resource.ProjectID, CreationSpecRef: resource.CreationSpecRef, Version: resource.Version,
		Status: resource.Status, ContentDigest: resource.ContentDigest, Title: resource.Content.Title,
		Summary: resource.Content.Summary, Sections: cloneStoryboardSections(resource.Content.Sections),
		Elements: cloneStoryboardElements(resource.Content.Elements), Slots: cloneStoryboardSlots(resource.Content.Slots),
		UpdatedAt: now.UTC(),
	}
	return planstoryboard.Result{
		SchemaVersion: planstoryboard.ResultSchemaVersion, Status: "completed", ResultCode: planstoryboard.ResultCodeCompleted,
		ResourceRef: &planstoryboard.ResourceRef{StoryboardPreviewID: resource.StoryboardPreviewID, Version: resource.Version,
			Digest: resource.ContentDigest, Status: resource.Status, CreationSpecRef: resource.CreationSpecRef},
		InvocationRef: planstoryboard.InvocationRef{ToolCallID: command.TrustedContext.ToolCallID, BusinessCommandID: command.TrustedContext.BusinessCommandID},
		Card:          card,
	}
}

func cloneStoryboardSections(input []planstoryboard.Section) []planstoryboard.Section {
	result := make([]planstoryboard.Section, len(input))
	copy(result, input)
	return result
}

func cloneStoryboardElements(input []planstoryboard.Element) []planstoryboard.Element {
	result := make([]planstoryboard.Element, len(input))
	copy(result, input)
	for index := range result {
		dependencies := make([]string, len(input[index].DependencyKeys))
		copy(dependencies, input[index].DependencyKeys)
		result[index].DependencyKeys = dependencies
	}
	return result
}

func cloneStoryboardSlots(input []planstoryboard.Slot) []planstoryboard.Slot {
	result := make([]planstoryboard.Slot, len(input))
	copy(result, input)
	return result
}

func deterministicBusinessFailure(trusted planstoryboard.TrustedContext, err error) (planstoryboard.Result, bool) {
	code := ""
	summary := ""
	switch {
	case errors.Is(err, planstoryboard.ErrBusinessNotFound):
		code, summary = planstoryboard.ResultCodeCreationSpecNotFound, "CreationSpec 不存在或不可访问。"
	case errors.Is(err, planstoryboard.ErrBusinessCreationSpecConflict):
		code, summary = planstoryboard.ResultCodeCreationSpecConflict, "CreationSpec 已发生变化，请刷新后重试。"
	case errors.Is(err, planstoryboard.ErrBusinessConflict):
		code, summary = planstoryboard.ResultCodeBusinessConflict, "Storyboard 保存命令发生冲突，请刷新后重试。"
	case errors.Is(err, planstoryboard.ErrBusinessDisabled):
		code, summary = planstoryboard.ResultCodeBusinessDisabled, "Storyboard 预览当前未启用。"
	default:
		return planstoryboard.Result{}, false
	}
	retryable := false
	return planstoryboard.Result{
		SchemaVersion: planstoryboard.ResultSchemaVersion, Status: "failed", ResultCode: code,
		InvocationRef: planstoryboard.InvocationRef{ToolCallID: trusted.ToolCallID, BusinessCommandID: trusted.BusinessCommandID},
		Summary:       summary, Retryable: &retryable,
	}, true
}

var _ Recovery = (*RecoveryCoordinator)(nil)
