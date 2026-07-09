package tools

import (
	"context"
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
		ParamsOneOf: schema.NewParamsOneOfByParams(toolInvocationEnvelopeParams(map[string]*schema.ParameterInfo{
			"message": {
				Type:     schema.String,
				Desc:     "Message to echo.",
				Required: true,
			},
		})),
	}, nil
}

func (EchoTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	enveloped, err := decodeToolInvocationEnvelope("echo_tool", argumentsInJSON, func(payload EchoInput) bool {
		return payload.Message != ""
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"message":%q}`, enveloped.Payload.Message), nil
}

var _ einotool.InvokableTool = EchoTool{}
