package vocabulary

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// 注册表哨兵错误，供 errors.Is 比较（对齐 internal/aigc/tools/registry.go 约定）。
var (
	ErrToolKeyRequired       = errors.New("vocabulary tool key is required")
	ErrToolAlreadyRegistered = errors.New("vocabulary tool already registered")
)

// Registry 是原子工具词汇表（§1 唯一固定词汇表）。注册面只在装配根
// 调用；运行期只读。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("%w: tool is nil", ErrToolKeyRequired)
	}
	key := strings.TrimSpace(tool.Descriptor().Key)
	if key == "" {
		return ErrToolKeyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[key]; exists {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, key)
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
	return r.sortedKeysLocked()
}

// sortedKeysLocked 返回排序后的全部工具 key。调用方必须已持有 r.mu（读或写锁）。
func (r *Registry) sortedKeysLocked() []string {
	keys := make([]string, 0, len(r.tools))
	for key := range r.tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// CatalogText 生成注入 Agent 上下文的词汇目录（三要素 + 入参/出参概要）。
// 出参概要是 Agent 书写 $step.output 引用的唯一依据，必须完整渲染。
func (r *Registry) CatalogText() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var builder strings.Builder
	for _, key := range r.sortedKeysLocked() {
		d := r.tools[key].Descriptor()
		builder.WriteString(fmt.Sprintf("- %s（%s，%s）：%s", d.Key, d.Name, d.Category, d.Description))
		if len(d.Inputs) > 0 {
			builder.WriteString(" 入参:" + strings.Join(paramNames(d.Inputs, true), ","))
		}
		if len(d.Outputs) > 0 {
			builder.WriteString(" 出参:" + strings.Join(paramNames(d.Outputs, false), ","))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

// paramNames 返回排序后的参数名列表；markRequired 为真时给必填项追加 * 标记。
func paramNames(specs map[string]ParamSpec, markRequired bool) []string {
	names := make([]string, 0, len(specs))
	for name, spec := range specs {
		if markRequired && spec.Required {
			name += "*"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
