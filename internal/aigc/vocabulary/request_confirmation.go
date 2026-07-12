package vocabulary

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type requestConfirmationTool struct{}

func NewRequestConfirmationTool() Tool {
	return requestConfirmationTool{}
}

func (requestConfirmationTool) Descriptor() Descriptor {
	return Descriptor{
		Key:         "request_confirmation",
		Name:        "确认卡点",
		Description: "请求用户从封闭选项中确认后继续计划",
		Category:    "interaction",
		Inputs: map[string]ParamSpec{
			"question": {Type: "string", Desc: "需要用户确认的问题", Required: true},
			"options":  {Type: "array", Desc: "可选的封闭选项"},
		},
	}
}

func (requestConfirmationTool) Run(ctx context.Context, call Call) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("request_confirmation context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	inputs, err := cloneJSONMap(call.Inputs)
	if err != nil {
		return Result{}, fmt.Errorf("request_confirmation inputs: %w", err)
	}
	question, ok := inputs["question"].(string)
	if !ok || strings.TrimSpace(question) == "" {
		return invalidConfirmationRequest("question must be a non-empty string"), nil
	}
	payload := map[string]any{"question": question}
	if options, exists := inputs["options"]; exists {
		if _, ok := options.([]any); !ok {
			return invalidConfirmationRequest("options must be an array"), nil
		}
		payload["options"] = options
	}
	return Result{Suspension: &Suspension{Reason: "waiting_user", Payload: payload}}, nil
}

func invalidConfirmationRequest(message string) Result {
	return Result{Fail: &Failure{Code: "invalid_request", Message: message}}
}
