package vocabulary

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type PromptWriter interface {
	WritePrompt(ctx context.Context, inputs map[string]any) (string, error)
}

type writeMediaPromptTool struct {
	writer PromptWriter
}

func NewWriteMediaPromptTool(writer PromptWriter) Tool {
	return &writeMediaPromptTool{writer: writer}
}

func (t *writeMediaPromptTool) Descriptor() Descriptor {
	return Descriptor{
		Key:         "write_media_prompt",
		Name:        "提示词编写",
		Description: "为媒体生成目标编写提示词，不选择模型或发起生成",
		Category:    "cognition",
		Inputs: map[string]ParamSpec{
			"target_desc": {Type: "string", Desc: "生成目标描述", Required: true},
		},
		Outputs: map[string]ParamSpec{
			"prompt": {Type: "string", Desc: "媒体生成提示词"},
		},
	}
}

func (t *writeMediaPromptTool) Run(ctx context.Context, call Call) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("write_media_prompt context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if isNilDependency(t.writer) {
		return Result{}, errors.New("write_media_prompt prompt writer is required")
	}
	inputs, err := cloneJSONMap(call.Inputs)
	if err != nil {
		return Result{}, fmt.Errorf("write_media_prompt inputs: %w", err)
	}
	targetDesc, ok := inputs["target_desc"].(string)
	if !ok || strings.TrimSpace(targetDesc) == "" {
		return Result{Fail: &Failure{Code: "invalid_request", Message: "target_desc must be a non-empty string"}}, nil
	}
	prompt, err := t.writer.WritePrompt(ctx, inputs)
	if err != nil {
		return Result{}, fmt.Errorf("write_media_prompt: %w", err)
	}
	if strings.TrimSpace(prompt) == "" {
		return Result{}, errors.New("write_media_prompt: prompt writer returned an empty prompt")
	}
	return Result{Outputs: map[string]any{"prompt": prompt}}, nil
}
