package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/modelreceipt"
)

const modelCallOrdinalRunLocalKey = "aigc.model_receipt.call_ordinal"

// ModelReceiptMiddlewareConfig configures durable, first-write-wins freezing
// for Agent ChatModel outputs.
type ModelReceiptMiddlewareConfig struct {
	Store modelreceipt.Store
}

// ModelReceiptMiddleware freezes a complete ChatModel response before the ADK
// can inspect it or invoke any tool it contains.
type ModelReceiptMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	store modelreceipt.Store
}

func NewModelReceiptMiddleware(config ModelReceiptMiddlewareConfig) (*ModelReceiptMiddleware, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("model receipt store is required")
	}
	return &ModelReceiptMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		store:                        config.Store,
	}, nil
}

func (m *ModelReceiptMiddleware) WrapModel(_ context.Context, inner model.BaseChatModel, _ *adk.ModelContext) (model.BaseChatModel, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("model receipt middleware store is required")
	}
	if inner == nil {
		return nil, fmt.Errorf("model receipt inner model is required")
	}
	return &modelReceiptChatModel{inner: inner, store: m.store}, nil
}

// WrapInvokableToolCall propagates the model's frozen ToolCall ID into the
// trusted command context. Capability wrappers can then derive idempotency from
// the logical call slot instead of from a fresh process-local ID.
func (m *ModelReceiptMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, toolContext *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		toolCtx, err := withToolCallCommandContext(ctx, toolContext)
		if err != nil {
			return "", err
		}
		return endpoint(toolCtx, argumentsInJSON, opts...)
	}, nil
}

func (m *ModelReceiptMiddleware) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, toolContext *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		toolCtx, err := withToolCallCommandContext(ctx, toolContext)
		if err != nil {
			return nil, err
		}
		return endpoint(toolCtx, argumentsInJSON, opts...)
	}, nil
}

func (m *ModelReceiptMiddleware) WrapEnhancedInvokableToolCall(_ context.Context, endpoint adk.EnhancedInvokableToolCallEndpoint, toolContext *adk.ToolContext) (adk.EnhancedInvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
		toolCtx, err := withToolCallCommandContext(ctx, toolContext)
		if err != nil {
			return nil, err
		}
		return endpoint(toolCtx, argument, opts...)
	}, nil
}

func (m *ModelReceiptMiddleware) WrapEnhancedStreamableToolCall(_ context.Context, endpoint adk.EnhancedStreamableToolCallEndpoint, toolContext *adk.ToolContext) (adk.EnhancedStreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argument *schema.ToolArgument, opts ...tool.Option) (*schema.StreamReader[*schema.ToolResult], error) {
		toolCtx, err := withToolCallCommandContext(ctx, toolContext)
		if err != nil {
			return nil, err
		}
		return endpoint(toolCtx, argument, opts...)
	}, nil
}

func withToolCallCommandContext(ctx context.Context, toolContext *adk.ToolContext) (context.Context, error) {
	command, ok := capability.CommandContextFrom(ctx)
	if !ok || toolContext == nil {
		return ctx, nil
	}
	callID := strings.TrimSpace(toolContext.CallID)
	command.ToolCallID = callID
	base := strings.TrimSpace(command.IdempotencyKey)
	// Tests and non-capability middleware probes may not carry an idempotency
	// base. Production capability invocations always do; only those need the
	// run-local checkpoint bridge.
	if base == "" || callID == "" {
		return capability.WithCommandContext(ctx, command), nil
	}
	keyDigest := sha256.Sum256([]byte(callID))
	key := fmt.Sprintf("aigc.tool_call.idempotency_base.%x", keyDigest[:16])
	frozen, found, err := adk.GetRunLocalValue(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("load logical Tool idempotency base for %s: %w", callID, err)
	}
	if found {
		frozenBase, valid := frozen.(string)
		frozenBase = strings.TrimSpace(frozenBase)
		if !valid || frozenBase == "" {
			return nil, fmt.Errorf("invalid logical Tool idempotency base for %s: %T(%v)", callID, frozen, frozen)
		}
		command.IdempotencyKey = frozenBase
		return capability.WithCommandContext(ctx, command), nil
	}
	if err := adk.SetRunLocalValue(ctx, key, base); err != nil {
		return nil, fmt.Errorf("freeze logical Tool idempotency base for %s: %w", callID, err)
	}
	command.IdempotencyKey = base
	return capability.WithCommandContext(ctx, command), nil
}

type modelReceiptChatModel struct {
	inner model.BaseChatModel
	store modelreceipt.Store
}

func (m *modelReceiptChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	turnID, durable, err := modelReceiptTurnID(ctx)
	if err != nil {
		return nil, err
	}
	if !durable {
		return m.inner.Generate(ctx, input, opts...)
	}
	ordinal, err := nextModelCallOrdinal(ctx)
	if err != nil {
		return nil, err
	}
	if frozen, found, err := loadFrozenModelMessage(ctx, m.store, turnID, ordinal); err != nil {
		return nil, err
	} else if found {
		return frozen, nil
	}

	inputDigest, err := digestModelInput(input)
	if err != nil {
		return nil, err
	}
	output, err := m.inner.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return freezeModelMessage(ctx, m.store, turnID, ordinal, inputDigest, output)
}

func (m *modelReceiptChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	turnID, durable, err := modelReceiptTurnID(ctx)
	if err != nil {
		return nil, err
	}
	if !durable {
		return m.inner.Stream(ctx, input, opts...)
	}
	ordinal, err := nextModelCallOrdinal(ctx)
	if err != nil {
		return nil, err
	}
	if frozen, found, err := loadFrozenModelMessage(ctx, m.store, turnID, ordinal); err != nil {
		return nil, err
	} else if found {
		return singleMessageStream(frozen), nil
	}

	inputDigest, err := digestModelInput(input)
	if err != nil {
		return nil, err
	}
	reader, err := m.inner.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, fmt.Errorf("model receipt inner model returned a nil stream")
	}
	defer reader.Close()
	chunks := make([]*schema.Message, 0, 8)
	for {
		chunk, recvErr := reader.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, fmt.Errorf("consume model stream before freezing: %w", recvErr)
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("model stream completed without a message")
	}
	output, err := schema.ConcatMessages(chunks)
	if err != nil {
		return nil, fmt.Errorf("concat model stream before freezing: %w", err)
	}
	authoritative, err := freezeModelMessage(ctx, m.store, turnID, ordinal, inputDigest, output)
	if err != nil {
		return nil, err
	}
	return singleMessageStream(authoritative), nil
}

func modelReceiptTurnID(ctx context.Context) (string, bool, error) {
	command, ok := capability.CommandContextFrom(ctx)
	if !ok {
		return "", false, nil
	}
	turnID := strings.TrimSpace(command.RequestID)
	if turnID == "" {
		return "", false, fmt.Errorf("model receipt requires a stable command request id")
	}
	return turnID, true, nil
}

func nextModelCallOrdinal(ctx context.Context) (int, error) {
	value, found, err := adk.GetRunLocalValue(ctx, modelCallOrdinalRunLocalKey)
	if err != nil {
		return 0, fmt.Errorf("get model-call ordinal: %w", err)
	}
	ordinal := 0
	if found {
		var ok bool
		ordinal, ok = value.(int)
		if !ok || ordinal < 0 {
			return 0, fmt.Errorf("invalid model-call ordinal state %T(%v)", value, value)
		}
	}
	ordinal++
	if err := adk.SetRunLocalValue(ctx, modelCallOrdinalRunLocalKey, ordinal); err != nil {
		return 0, fmt.Errorf("set model-call ordinal: %w", err)
	}
	return ordinal, nil
}

func digestModelInput(input []*schema.Message) (string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal model input for receipt audit: %w", err)
	}
	return modelreceipt.Digest(raw), nil
}

func loadFrozenModelMessage(ctx context.Context, store modelreceipt.Store, turnID string, ordinal int) (*schema.Message, bool, error) {
	receipt, err := store.Get(ctx, turnID, ordinal)
	if errors.Is(err, modelreceipt.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load model receipt %s/%d: %w", turnID, ordinal, err)
	}
	message, err := messageFromReceipt(receipt)
	if err != nil {
		return nil, false, fmt.Errorf("decode model receipt %s/%d: %w", turnID, ordinal, err)
	}
	return message, true, nil
}

func freezeModelMessage(ctx context.Context, store modelreceipt.Store, turnID string, ordinal int, inputDigest string, output *schema.Message) (*schema.Message, error) {
	if output == nil {
		return nil, fmt.Errorf("model returned a nil message")
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("marshal model output before freezing: %w", err)
	}
	authoritative, err := store.PutOnce(ctx, modelreceipt.Receipt{
		TurnID:       turnID,
		Ordinal:      ordinal,
		OutputJSON:   raw,
		OutputDigest: modelreceipt.Digest(raw),
		InputDigest:  inputDigest,
	})
	if err != nil {
		return nil, fmt.Errorf("freeze model receipt %s/%d: %w", turnID, ordinal, err)
	}
	message, err := messageFromReceipt(authoritative)
	if err != nil {
		return nil, fmt.Errorf("decode authoritative model receipt %s/%d: %w", turnID, ordinal, err)
	}
	return message, nil
}

func messageFromReceipt(receipt modelreceipt.Receipt) (*schema.Message, error) {
	if len(receipt.OutputJSON) == 0 || !json.Valid(receipt.OutputJSON) {
		return nil, fmt.Errorf("output_json is not valid JSON")
	}
	if strings.TrimSpace(receipt.OutputDigest) == "" || receipt.OutputDigest != modelreceipt.Digest(receipt.OutputJSON) {
		return nil, fmt.Errorf("output digest mismatch")
	}
	var message *schema.Message
	if err := json.Unmarshal(receipt.OutputJSON, &message); err != nil {
		return nil, fmt.Errorf("unmarshal schema message: %w", err)
	}
	if message == nil {
		return nil, fmt.Errorf("stored schema message is null")
	}
	// The JSON round trip produces a fresh object, so callers cannot mutate the
	// store's authoritative value or a value returned to another replay.
	return message, nil
}

func singleMessageStream(message *schema.Message) *schema.StreamReader[*schema.Message] {
	return schema.StreamReaderFromArray([]*schema.Message{message})
}

var _ adk.ChatModelAgentMiddleware = (*ModelReceiptMiddleware)(nil)
var _ model.BaseChatModel = (*modelReceiptChatModel)(nil)
