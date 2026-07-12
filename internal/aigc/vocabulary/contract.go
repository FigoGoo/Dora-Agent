// Package vocabulary 定义原子工具的统一契约与注册表（§1 唯一固定词汇表）。
// 原子工具无状态、互不调用、不发事件、写幂等，是动态编排的地基。
package vocabulary

import "context"

// ParamSpec 描述单个入参/出参的类型与语义。
type ParamSpec struct {
	Type     string `json:"type"` // string|int|bool|array|object|ref
	Desc     string `json:"desc"`
	Required bool   `json:"required,omitempty"`
}

// Descriptor 是工具的三要素（key/name/description）加类别与入出参概要，
// 供注册表校验并注入 Agent 上下文。
type Descriptor struct {
	Key         string               `json:"tool_key"`
	Name        string               `json:"tool_name"`
	Description string               `json:"tool_description"`
	Category    string               `json:"category"` // cognition|media|data|guard|interaction
	Inputs      map[string]ParamSpec `json:"inputs,omitempty"`
	Outputs     map[string]ParamSpec `json:"outputs,omitempty"`
}

// Call 是一次工具调用的运行期上下文与入参。
type Call struct {
	SessionID      string
	UserID         string
	PlanRunID      string
	NodeID         string
	Attempt        int
	IdempotencyKey string // runtime 注入：plan_run+node+attempt 组合
	Inputs         map[string]any
}

// Failure 表示业务失败（非基础设施故障），是编排层的决策输入而非异常。
type Failure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

// Suspension 表示工具主动声明挂起，供交互类/派发类工具等待外部条件。
type Suspension struct {
	Reason  string         `json:"reason"` // waiting_user | waiting_jobs
	Payload map[string]any `json:"payload,omitempty"`
}

// Result 是工具调用的一等结果：
//   - 仅 Outputs：同步成功。
//   - Fail：业务失败（编排层据此决策，不当作 error 抛出），与 Outputs、Suspension 互斥。
//   - Suspension：工具声明挂起（等待用户交互或异步任务），可同时携带 Outputs，
//     例如异步派发先产出 batch receipt 再进入 waiting_jobs。
//
// 与 Run 返回的 error 区分：error 表示基础设施故障（通常可重试），
// 不是业务语义。
type Result struct {
	Outputs    map[string]any `json:"outputs,omitempty"`
	Fail       *Failure       `json:"fail,omitempty"`
	Suspension *Suspension    `json:"suspension,omitempty"`
}

// Tool 是原子工具的统一契约：自描述 + 可执行。
type Tool interface {
	Descriptor() Descriptor
	Run(ctx context.Context, call Call) (Result, error)
}
