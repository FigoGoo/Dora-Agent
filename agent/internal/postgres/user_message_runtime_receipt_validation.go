package postgres

import (
	"context"
	"errors"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// validateUserMessageTerminalModelReceipt 将终态 Output 绑定到同一稳定 Model Call。
// 成功必须存在 completed Model Receipt；确定输入失败必须没有模型调用；执行失败只接受无调用或终态模型事实。
func (r *UserMessageRuntimeRepository) validateUserMessageTerminalModelReceipt(
	ctx context.Context,
	tx *gorm.DB,
	claim usermessageruntime.Claim,
	output usermessageruntime.Output,
) error {
	var record userMessageModelReceiptModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("model_call_id = ?", claim.ModelCallID).
		Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if output.Failure != nil && output.Failure.ErrorCode == usermessageruntime.FailureCodeInvalidInput {
			return nil
		}
		if output.Failure != nil && output.Failure.ErrorCode == usermessageruntime.FailureCodeProcessingFailed {
			return nil
		}
		return usermessageruntime.ErrOutputContract
	}
	if err != nil {
		return err
	}
	if record.ModelCallID != claim.ModelCallID || record.RunID != claim.RunID ||
		record.TurnID != claim.Context.TurnID || record.InputID != claim.Context.InputID ||
		record.ExecutionFence < 1 {
		return usermessageruntime.ErrOutputContract
	}
	if output.DirectResponse != nil {
		if record.Status != "completed" {
			return usermessageruntime.ErrOutputContract
		}
		response, openErr := r.openUserMessageModelResponse(ctx, record)
		if openErr != nil {
			return openErr
		}
		return usermessageruntime.ValidateCompletedModelResponse(response, claim, output)
	}
	if output.Failure == nil || output.Failure.ErrorCode == usermessageruntime.FailureCodeInvalidInput {
		return usermessageruntime.ErrOutputContract
	}
	if output.Failure.ErrorCode != usermessageruntime.FailureCodeProcessingFailed {
		return usermessageruntime.ErrOutputContract
	}
	switch record.Status {
	case "failed":
		if record.ErrorCode == nil || *record.ErrorCode != usermessageruntime.ModelFailureCodeExecutionFailed {
			return usermessageruntime.ErrOutputContract
		}
		return nil
	case "completed":
		_, openErr := r.openUserMessageModelResponse(ctx, record)
		if openErr != nil {
			return openErr
		}
		// Runner 还会校验 AgentEvent/Action/CustomizedOutput；即使模型正文可解析，
		// 事件层契约失败仍必须允许提交固定 Processing Failure Card。
		return nil
	default:
		return usermessageruntime.ErrOutputContract
	}
}

func (r *UserMessageRuntimeRepository) openUserMessageModelResponse(
	ctx context.Context,
	record userMessageModelReceiptModel,
) (*schema.Message, error) {
	if record.ResponseDigest == nil || record.ResponseKeyVersion == nil || len(record.ResponseCiphertext) == 0 ||
		record.ErrorCode != nil {
		return nil, usermessageruntime.ErrOutputContract
	}
	plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
		Ciphertext: append([]byte(nil), record.ResponseCiphertext...), KeyVersion: *record.ResponseKeyVersion,
	}, *record.ResponseDigest)
	if err != nil {
		return nil, usermessageruntime.ErrOutputContract
	}
	var response schema.Message
	if err := strictJSONDecode(plaintext, &response); err != nil {
		return nil, usermessageruntime.ErrOutputContract
	}
	return &response, nil
}
