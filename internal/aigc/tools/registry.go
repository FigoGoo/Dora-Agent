package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	einotool "github.com/cloudwego/eino/components/tool"
)

var (
	ErrToolAlreadyRegistered = errors.New("tool already registered")
	ErrToolNotFound          = errors.New("tool not found")
)

type ToolMeta struct {
	Key         string   `json:"key"`
	Category    string   `json:"category,omitempty"`
	StageHints  []string `json:"stage_hints,omitempty"`
	OutputKinds []string `json:"output_kinds,omitempty"`
	Provider    string   `json:"provider,omitempty"`
}

type ToolSummary struct {
	ToolMeta
	Name string `json:"name"`
	Desc string `json:"desc"`
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]einotool.BaseTool
	metas map[string]ToolMeta
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]einotool.BaseTool),
		metas: make(map[string]ToolMeta),
	}
}

func (r *Registry) Register(toolKey string, t einotool.BaseTool, meta ToolMeta) error {
	if toolKey == "" {
		return fmt.Errorf("tool key is required")
	}
	if t == nil {
		return fmt.Errorf("tool %q is nil", toolKey)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[toolKey]; ok {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, toolKey)
	}
	meta.Key = toolKey
	r.tools[toolKey] = t
	r.metas[toolKey] = meta
	return nil
}

func (r *Registry) Get(toolKey string) (einotool.BaseTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[toolKey]
	return t, ok
}

func (r *Registry) MustGet(toolKey string) (einotool.BaseTool, error) {
	t, ok := r.Get(toolKey)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, toolKey)
	}
	return t, nil
}

func (r *Registry) ListByKeys(keys []string) []einotool.BaseTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]einotool.BaseTool, 0, len(keys))
	for _, key := range keys {
		if t, ok := r.tools[key]; ok {
			out = append(out, t)
		}
	}
	return out
}

func (r *Registry) ListSummaries(ctx context.Context) ([]ToolSummary, error) {
	r.mu.RLock()
	keys := make([]string, 0, len(r.tools))
	for key := range r.tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tools := make(map[string]einotool.BaseTool, len(r.tools))
	metas := make(map[string]ToolMeta, len(r.metas))
	for key, t := range r.tools {
		tools[key] = t
	}
	for key, meta := range r.metas {
		metas[key] = meta
	}
	r.mu.RUnlock()

	summaries := make([]ToolSummary, 0, len(keys))
	for _, key := range keys {
		info, err := tools[key].Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("load tool info %q: %w", key, err)
		}
		summaries = append(summaries, ToolSummary{
			ToolMeta: metas[key],
			Name:     info.Name,
			Desc:     info.Desc,
		})
	}
	return summaries, nil
}

func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
