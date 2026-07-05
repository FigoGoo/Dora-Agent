package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

type ErrorClassifier func(ctx context.Context, toolName string, callID string, err error) aigctools.ToolErrorEnvelope

type TraceIDFunc func(ctx context.Context) string

type ToolExceptionMiddleware[M adk.MessageType] struct {
	*adk.TypedBaseChatModelAgentMiddleware[M]

	Classify   ErrorClassifier
	TraceID    TraceIDFunc
	EmitEvents bool
}

func NewToolExceptionMiddleware[M adk.MessageType]() *ToolExceptionMiddleware[M] {
	return &ToolExceptionMiddleware[M]{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[M]{},
		Classify:                          DefaultToolErrorClassifier,
		EmitEvents:                        true,
	}
}

func DefaultToolErrorClassifier(ctx context.Context, toolName string, callID string, err error) aigctools.ToolErrorEnvelope {
	code := aigctools.ErrCodeProviderFailed
	retryable := true
	userMessage := "工具执行失败，可以调整输入后重试。"
	suggestedAction := "review_or_retry"

	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "timeout"):
		code = aigctools.ErrCodeTimeout
		userMessage = "工具执行超时，任务可以稍后重试。"
		suggestedAction = "retry_later"
	case strings.Contains(msg, "validation"), strings.Contains(msg, "invalid"), strings.Contains(msg, "required"):
		code = aigctools.ErrCodeValidation
		retryable = false
		userMessage = "工具参数不完整或格式不正确，需要重新组织参数。"
		suggestedAction = "fix_arguments"
	case strings.Contains(msg, "not found"):
		code = aigctools.ErrCodeAssetNotFound
		retryable = false
		userMessage = "引用的资源不存在，需要重新选择或生成素材。"
		suggestedAction = "select_or_generate_asset"
	case strings.Contains(msg, "rate limit"), strings.Contains(msg, "too many request"):
		code = aigctools.ErrCodeProviderLimit
		userMessage = "生成服务暂时限流，可以稍后重试。"
		suggestedAction = "retry_later"
	case strings.Contains(msg, "dependency"):
		code = aigctools.ErrCodeDependency
		retryable = false
		userMessage = "当前阶段依赖尚未完成，需要先确认前置内容。"
		suggestedAction = "complete_dependency"
	}

	return aigctools.ToolErrorEnvelope{
		ToolKey:          toolName,
		Code:             code,
		UserMessage:      userMessage,
		TechnicalMessage: err.Error(),
		Retryable:        retryable,
		SuggestedAction:  suggestedAction,
		TraceID:          callID,
	}
}

func (m *ToolExceptionMiddleware[M]) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
		out, err := endpoint(ctx, argumentsInJSON, opts...)
		if err == nil {
			return out, nil
		}
		envelope := m.errorEnvelope(ctx, tCtx, err)
		m.emit(ctx, envelope)
		return marshalToolError(envelope), nil
	}, nil
}

func (m *ToolExceptionMiddleware[M]) WrapStreamableToolCall(ctx context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (*schema.StreamReader[string], error) {
		stream, err := endpoint(ctx, argumentsInJSON, opts...)
		if err == nil {
			return stream, nil
		}
		envelope := m.errorEnvelope(ctx, tCtx, err)
		m.emit(ctx, envelope)
		reader, writer := schema.Pipe[string](1)
		go func() {
			defer writer.Close()
			writer.Send(marshalToolError(envelope), nil)
		}()
		return reader, nil
	}, nil
}

func (m *ToolExceptionMiddleware[M]) WrapEnhancedInvokableToolCall(ctx context.Context, endpoint adk.EnhancedInvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.EnhancedInvokableToolCallEndpoint, error) {
	return func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...einotool.Option) (*schema.ToolResult, error) {
		out, err := endpoint(ctx, toolArgument, opts...)
		if err == nil {
			return out, nil
		}
		envelope := m.errorEnvelope(ctx, tCtx, err)
		m.emit(ctx, envelope)
		return textToolResult(marshalToolError(envelope)), nil
	}, nil
}

func (m *ToolExceptionMiddleware[M]) WrapEnhancedStreamableToolCall(ctx context.Context, endpoint adk.EnhancedStreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.EnhancedStreamableToolCallEndpoint, error) {
	return func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...einotool.Option) (*schema.StreamReader[*schema.ToolResult], error) {
		stream, err := endpoint(ctx, toolArgument, opts...)
		if err == nil {
			return stream, nil
		}
		envelope := m.errorEnvelope(ctx, tCtx, err)
		m.emit(ctx, envelope)
		reader, writer := schema.Pipe[*schema.ToolResult](1)
		go func() {
			defer writer.Close()
			writer.Send(textToolResult(marshalToolError(envelope)), nil)
		}()
		return reader, nil
	}, nil
}

func (m *ToolExceptionMiddleware[M]) errorEnvelope(ctx context.Context, tCtx *adk.ToolContext, err error) aigctools.ToolErrorEnvelope {
	toolName := ""
	callID := ""
	if tCtx != nil {
		toolName = tCtx.Name
		callID = tCtx.CallID
	}

	classifier := m.Classify
	if classifier == nil {
		classifier = DefaultToolErrorClassifier
	}
	envelope := classifier(ctx, toolName, callID, err)
	if m.TraceID != nil {
		envelope.TraceID = m.TraceID(ctx)
	}
	return envelope
}

func (m *ToolExceptionMiddleware[M]) emit(ctx context.Context, envelope aigctools.ToolErrorEnvelope) {
	if !m.EmitEvents {
		return
	}
	_ = adk.TypedSendEvent(ctx, &adk.TypedAgentEvent[M]{
		AgentName: "tool_exception_middleware",
		Output: &adk.TypedAgentOutput[M]{
			CustomizedOutput: map[string]any{
				"event": "tool.error",
				"error": envelope,
			},
		},
	})
}

func marshalToolError(envelope aigctools.ToolErrorEnvelope) string {
	out := aigctools.ErrorResult[map[string]any](envelope)
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf(`{"status":"error","error":{"code":"%s","user_message":"tool failed"}}`, aigctools.ErrCodeFatal)
	}
	return string(b)
}

func textToolResult(text string) *schema.ToolResult {
	return &schema.ToolResult{
		Parts: []schema.ToolOutputPart{
			{Type: schema.ToolPartTypeText, Text: text},
		},
	}
}
