package vocabulary

import (
	"context"
	"strings"
	"testing"
)

type echoTool struct{ key string }

func (t echoTool) Descriptor() Descriptor {
	return Descriptor{Key: t.key, Name: "回声", Description: "测试工具", Category: "cognition",
		Inputs:  map[string]ParamSpec{"text": {Type: "string", Desc: "输入", Required: true}},
		Outputs: map[string]ParamSpec{"text": {Type: "string", Desc: "原样返回"}},
	}
}

func (t echoTool) Run(_ context.Context, call Call) (Result, error) {
	return Result{Outputs: map[string]any{"text": call.Inputs["text"]}}, nil
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(echoTool{key: "echo"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.Register(echoTool{key: "echo"}); err == nil {
		t.Fatal("duplicate key must be rejected")
	}
	if err := registry.Register(echoTool{key: " "}); err == nil {
		t.Fatal("empty key must be rejected")
	}
	tool, ok := registry.Get("echo")
	if !ok || tool.Descriptor().Name != "回声" {
		t.Fatalf("lookup failed: %v %v", ok, tool)
	}
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("missing tool must not resolve")
	}
	catalog := registry.CatalogText()
	if !strings.Contains(catalog, "echo") || !strings.Contains(catalog, "回声") {
		t.Fatalf("catalog must list three-elements: %s", catalog)
	}
}
