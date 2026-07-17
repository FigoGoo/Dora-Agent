package writeprompts

import (
	"context"
	"errors"
	"fmt"
)

// savePromptDraft 只接受双 Validator 通过的 Content，先冻结完整 Command/重发预算，再调用一次 Business Save；响应未知不在本节点重发。
func (b *graphBuilder) savePromptDraft(ctx context.Context, input contentRoute) (SaveOutcome, error) {
	if input.Route != routeValid || input.Content == nil {
		return SaveOutcome{}, fmt.Errorf("save prompt preview draft: invalid validator route")
	}
	command := DraftCommand{TrustedContext: input.TrustedContext, DomainContext: input.Context,
		Targets: append([]PromptTarget(nil), input.Targets...), ExactTargetSetDigest: input.ExactTargetSetDigest,
		Content: cloneContent(*input.Content), ResendLimit: input.TrustedContext.Policy.MaxCommandResends}
	digest, err := SaveRequestDigest(command)
	if err != nil {
		return SaveOutcome{}, err
	}
	command.RequestDigest = digest
	if err := b.journal.PrepareCommand(ctx, command); err != nil {
		return SaveOutcome{}, err
	}
	disposition, resource, err := b.store.SavePromptPreviewDraft(ctx, command)
	if err != nil {
		if errors.Is(err, ErrBusinessUnknownOutcome) {
			return SaveOutcome{Status: routeUnknown, Command: command}, nil
		}
		return SaveOutcome{}, err
	}
	if disposition != SaveDispositionCreated && disposition != SaveDispositionReplayed {
		return SaveOutcome{}, fmt.Errorf("save prompt preview draft: invalid disposition")
	}
	if err := ValidateResourceForCommand(resource, command); err != nil {
		return SaveOutcome{}, err
	}
	return SaveOutcome{Status: routeSaved, Disposition: disposition, Resource: &resource, Command: command}, nil
}

// querySaveReceipt 只查询原 command_id 与 request_digest 一次；completed 恢复原资源，not_found/技术不确定转为内部恢复工件，冲突失败关闭。
func (b *graphBuilder) querySaveReceipt(ctx context.Context, input SaveOutcome) (SaveOutcome, error) {
	if input.Status != routeUnknown {
		return SaveOutcome{}, fmt.Errorf("query prompt preview save receipt: invalid upstream route")
	}
	status, resource, err := b.store.QueryPromptPreviewCommand(ctx, input.Command)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrBusinessConflict) {
			return SaveOutcome{}, err
		}
		return recoverySaveOutcome(input.Command)
	}
	switch status {
	case "completed":
		if resource == nil {
			return SaveOutcome{}, fmt.Errorf("query prompt preview save receipt: completed resource is nil")
		}
		if err := ValidateResourceForCommand(*resource, input.Command); err != nil {
			return SaveOutcome{}, err
		}
		return SaveOutcome{Status: routeSaved, Disposition: SaveDispositionReplayed, Resource: resource, Command: input.Command}, nil
	case "not_found":
		return recoverySaveOutcome(input.Command)
	case "conflict":
		return SaveOutcome{}, ErrBusinessConflict
	default:
		return recoverySaveOutcome(input.Command)
	}
}

// recoverySaveOutcome 构造不进入 Tool Result 的持久化恢复工件；其命令、摘要和重发上限都来自 Save 前已冻结事实。
func recoverySaveOutcome(command DraftCommand) (SaveOutcome, error) {
	contentDigest, err := ContentDigest(command.Content)
	if err != nil {
		return SaveOutcome{}, err
	}
	recovery := &RecoveryDeferred{
		ToolCallID: command.TrustedContext.ToolCallID, BusinessCommandID: command.TrustedContext.BusinessCommandID,
		RequestDigest: command.RequestDigest, ContentDigest: contentDigest, Command: command, ResendLimit: command.ResendLimit,
		ResendExhausted: command.ResendLimit == 0,
	}
	return SaveOutcome{Status: routeRecoveryPending, Command: command, Recovery: recovery}, nil
}
