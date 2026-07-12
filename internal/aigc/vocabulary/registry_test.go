package vocabulary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	if err := registry.Register(echoTool{key: "echo"}); !errors.Is(err, ErrToolAlreadyRegistered) {
		t.Fatalf("duplicate key must return ErrToolAlreadyRegistered, got %v", err)
	}
	if err := registry.Register(echoTool{key: " "}); !errors.Is(err, ErrToolKeyRequired) {
		t.Fatalf("empty key must return ErrToolKeyRequired, got %v", err)
	}
	if err := registry.Register(nil); !errors.Is(err, ErrToolKeyRequired) {
		t.Fatalf("nil tool must return an error, got %v", err)
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
	if !strings.Contains(catalog, "入参:text*") {
		t.Fatalf("catalog must render required inputs: %s", catalog)
	}
	if !strings.Contains(catalog, "出参:text") {
		t.Fatalf("catalog must render outputs (agent 引用 $step.output 的依据): %s", catalog)
	}
}

// TestRegistryConcurrentAccess 在 -race 下用 50 个 goroutine 混跑
// Get/Keys/CatalogText 与若干 Register，验证 RWMutex 契约无数据竞争。
func TestRegistryConcurrentAccess(t *testing.T) {
	registry := NewRegistry()
	// 预置若干工具，保证只读方法有内容可遍历。
	const seeds = 8
	for i := 0; i < seeds; i++ {
		if err := registry.Register(echoTool{key: fmt.Sprintf("seed-%d", i)}); err != nil {
			t.Fatalf("seed register: %v", err)
		}
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			switch i % 4 {
			case 0:
				// 与只读方法竞争的写入；重复 key 会被拒，这里只关心竞态安全。
				_ = registry.Register(echoTool{key: fmt.Sprintf("race-%d", i%6)})
			case 1:
				registry.Get(fmt.Sprintf("seed-%d", i%seeds))
			case 2:
				_ = registry.Keys()
			default:
				_ = registry.CatalogText()
			}
		}(i)
	}
	wg.Wait()

	// 竞态跑完后种子工具仍应可读。
	if _, ok := registry.Get("seed-0"); !ok {
		t.Fatal("seed tool must survive concurrent access")
	}
}
