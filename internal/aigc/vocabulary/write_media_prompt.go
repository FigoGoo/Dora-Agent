package vocabulary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
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
		return Result{Fail: &Failure{Code: "empty_prompt", Message: "prompt writer returned an empty prompt", Retryable: true}}, nil
	}
	return Result{Outputs: map[string]any{"prompt": prompt}}, nil
}

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func cloneJSONMap(value map[string]any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("value is not JSON-compatible: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var cloned map[string]any
	if err := decoder.Decode(&cloned); err != nil {
		return nil, fmt.Errorf("decode JSON-compatible value: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode JSON-compatible value: unexpected trailing value")
		}
		return nil, fmt.Errorf("decode JSON-compatible value: %w", err)
	}
	return cloned, nil
}
