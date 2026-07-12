package vocabulary

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry 是原子工具词汇表（§1 唯一固定词汇表）。注册面只在装配根
// 调用；运行期只读。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(tool Tool) error {
	descriptor := tool.Descriptor()
	key := strings.TrimSpace(descriptor.Key)
	if key == "" {
		return fmt.Errorf("vocabulary tool key is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[key]; exists {
		return fmt.Errorf("vocabulary tool %q already registered", key)
	}
	r.tools[key] = tool
	return nil
}

func (r *Registry) Get(key string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[strings.TrimSpace(key)]
	return tool, ok
}

func (r *Registry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.tools))
	for key := range r.tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// CatalogText 生成注入 Agent 上下文的词汇目录（三要素+入参出参概要）。
func (r *Registry) CatalogText() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.tools))
	for key := range r.tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		d := r.tools[key].Descriptor()
		builder.WriteString(fmt.Sprintf("- %s（%s，%s）：%s", d.Key, d.Name, d.Category, d.Description))
		if len(d.Inputs) > 0 {
			names := make([]string, 0, len(d.Inputs))
			for name, spec := range d.Inputs {
				if spec.Required {
					name += "*"
				}
				names = append(names, name)
			}
			sort.Strings(names)
			builder.WriteString(" 入参:" + strings.Join(names, ","))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}
