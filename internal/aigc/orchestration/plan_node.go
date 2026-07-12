// Package orchestration 是编排库的进程内起点：模板节点 schema 与
// 审批续作决策表（终版设计 §3.5/§6.1 第 1 步）。表为纯数据、可 JSON
// 序列化；评估点谓词按设计立场留在 Go 代码，数据只引用谓词名。
package orchestration

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

type NodeKind string

const (
	NodeKindCapability NodeKind = "capability"
	// NodeKindAtomic 为 L2 原子词汇预留（终版 §6.1 第 1 步）；v1 模板
	// 不使用，schema 认识它以保证未来模板数据无需迁移。
	NodeKindAtomic NodeKind = "atomic"
)

// PlanNode 是编排模板里的一个可执行节点：调用哪个工具、用什么参数。
type PlanNode struct {
	Kind      NodeKind        `json:"kind"`
	ToolKey   string          `json:"tool_key"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func (n PlanNode) Validate() error {
	toolKey := strings.TrimSpace(n.ToolKey)
	if toolKey == "" {
		return fmt.Errorf("plan node tool_key is required")
	}
	if len(n.Arguments) > 0 && !json.Valid(n.Arguments) {
		return fmt.Errorf("plan node arguments must be valid JSON")
	}
	switch n.Kind {
	case NodeKindCapability:
		if !slices.Contains(capability.AgentToolKeys, toolKey) {
			return fmt.Errorf("plan node capability %q is not registered", toolKey)
		}
		return nil
	case NodeKindAtomic:
		return nil
	default:
		return fmt.Errorf("plan node kind %q is not supported", n.Kind)
	}
}
