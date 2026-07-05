package tools

import (
	"context"
	"encoding/json"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type EchoTool struct{}

type EchoInput struct {
	Message string `json:"message"`
}

func (EchoTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "echo_tool",
		Desc: "Echoes a message. Used by the Phase 0 Eino agent spike to verify tool call and tool result plumbing.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"message": {
				Type:     schema.String,
				Desc:     "Message to echo.",
				Required: true,
			},
		}),
	}, nil
}

func (EchoTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	var direct EchoInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &direct); err == nil && direct.Message != "" {
		return fmt.Sprintf(`{"message":%q}`, direct.Message), nil
	}

	var enveloped ToolInvocationEnvelope[EchoInput]
	if err := json.Unmarshal([]byte(argumentsInJSON), &enveloped); err != nil {
		return "", fmt.Errorf("decode echo input: %w", err)
	}
	if enveloped.Payload.Message == "" {
		return "", fmt.Errorf("message is required")
	}
	return fmt.Sprintf(`{"message":%q}`, enveloped.Payload.Message), nil
}

var _ einotool.InvokableTool = EchoTool{}
