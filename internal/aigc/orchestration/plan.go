package orchestration

import (
	"errors"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

// ErrPlanInvalid 是执行计划校验失败的哨兵错误。Validate 产出的所有结构化
// 错误都以 %w 包裹它，供修复循环（M3）用 errors.Is 识别并回喂 Agent 重写。
var ErrPlanInvalid = errors.New("execution plan is invalid")

// ExpandSpec 声明按某个数组入参做批量展开（设计 §3.5 第 4 要素）。v1 预留，
// Validate 一律拒绝——留 schema 位保证未来计划数据无需迁移。
type ExpandSpec struct {
	Over string `json:"over,omitempty"` // 预留
}

// PlanStep 是执行计划里的一个节点：引用哪个原子工具、如何绑定参数、依赖
// 哪些前置节点。卡点=节点引用交互类工具（无独立字段）；Evaluate=评估点声明。
// 参数值支持 $stepID.outputKey 引用前置节点产出；<X> 为参数槽，须实例化后提交。
type PlanStep struct {
	ID        string         `json:"id"`
	Tool      string         `json:"tool"`
	Params    map[string]any `json:"params,omitempty"`
	DependsOn []string       `json:"depends_on,omitempty"`
	Required  bool           `json:"required"`
	Evaluate  bool           `json:"evaluate,omitempty"`
	Expand    *ExpandSpec    `json:"expand,omitempty"`
}

// ExecutionPlan 是设计 §3.5"8 要素执行计划"的载体：元信息（Summary/Direction/
// Source）、节点集（Steps）、依赖边（DependsOn 纯 DAG）、批量展开（预留）、
// 卡点/评估点声明、预算预估（EstimatedJobs）、成功判据（Required/SuccessPolicy）。
type ExecutionPlan struct {
	PlanID        string     `json:"plan_id"`
	Source        string     `json:"source"` // template:<key> | dynamic
	Summary       string     `json:"summary"`
	Direction     string     `json:"direction"` // image|video|music|audio|mixed
	Steps         []PlanStep `json:"steps"`
	EstimatedJobs int        `json:"estimated_jobs,omitempty"`
	SuccessPolicy string     `json:"success_policy,omitempty"` // "" = all_required
}

// Validate 是五查的结构/参数/预留三查（权限与保障切面查在调度器执行面，
// 预算查回报事实由 Submit 决策）。结构化错误供修复循环（M3）使用。
func (p ExecutionPlan) Validate(registry *vocabulary.Registry, jobBudget int) error {
	if strings.TrimSpace(p.PlanID) == "" || len(p.Steps) == 0 {
		return fmt.Errorf("%w: plan_id and steps are required", ErrPlanInvalid)
	}
	stepIDs := map[string]int{}
	for index, step := range p.Steps {
		id := strings.TrimSpace(step.ID)
		if id == "" {
			return fmt.Errorf("%w: step %d id is required", ErrPlanInvalid, index)
		}
		if _, dup := stepIDs[id]; dup {
			return fmt.Errorf("%w: duplicate step id %q", ErrPlanInvalid, id)
		}
		stepIDs[id] = index
	}
	for _, step := range p.Steps {
		if step.Expand != nil {
			return fmt.Errorf("%w: step %s expand is reserved and not supported in v1", ErrPlanInvalid, step.ID)
		}
		tool, ok := registry.Get(step.Tool)
		if !ok {
			return fmt.Errorf("%w: step %s tool %q is not registered", ErrPlanInvalid, step.ID, step.Tool)
		}
		descriptor := tool.Descriptor()
		for name, spec := range descriptor.Inputs {
			if !spec.Required {
				continue
			}
			if _, bound := step.Params[name]; !bound {
				return fmt.Errorf("%w: step %s required param %q is not bound", ErrPlanInvalid, step.ID, name)
			}
		}
		for name, value := range step.Params {
			if err := validateParamValue(step.ID, name, value, stepIDs); err != nil {
				return err
			}
		}
		for _, dep := range step.DependsOn {
			if _, ok := stepIDs[dep]; !ok {
				return fmt.Errorf("%w: step %s depends on unknown step %q", ErrPlanInvalid, step.ID, dep)
			}
		}
	}
	if err := detectCycle(p.Steps); err != nil {
		return err
	}
	return nil
}

// ExceedsJobBudget 回报预算预估是否超限。超限不是校验错误——由 Submit 决定
// 转人工预览，Validate 仍视计划结构合法。budget<=0 表示不设限。
func (p ExecutionPlan) ExceedsJobBudget(budget int) bool {
	return budget > 0 && p.EstimatedJobs > budget
}

// validateParamValue 递归检查单个参数值：$stepID.outputKey 引用须指向已知节点；
// 非引用字符串不得残留未实例化的参数槽 <X>；数组/对象逐元素下钻。
func validateParamValue(stepID, name string, value any, stepIDs map[string]int) error {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "$") {
			parts := strings.SplitN(strings.TrimPrefix(typed, "$"), ".", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("%w: step %s param %q malformed reference %q", ErrPlanInvalid, stepID, name, typed)
			}
			if _, ok := stepIDs[parts[0]]; !ok {
				return fmt.Errorf("%w: step %s param %q reference to unknown step %q", ErrPlanInvalid, stepID, name, parts[0])
			}
			return nil
		}
		if strings.Contains(typed, "<") && strings.Contains(typed, ">") {
			return fmt.Errorf("%w: step %s param %q slot is not instantiated: %s", ErrPlanInvalid, stepID, name, typed)
		}
		return nil
	case []any:
		for _, elem := range typed {
			if err := validateParamValue(stepID, name, elem, stepIDs); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for _, elem := range typed {
			if err := validateParamValue(stepID, name, elem, stepIDs); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

// detectCycle 用 white/gray/black 三色 DFS 沿 DependsOn 边探测环：灰遇灰即回边。
// 调用前 Validate 已保证所有依赖指向已知节点，故此处只关注环，不再报悬挂依赖。
func detectCycle(steps []PlanStep) error {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(steps))
	deps := make(map[string][]string, len(steps))
	for _, step := range steps {
		deps[step.ID] = step.DependsOn
	}
	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for _, dep := range deps[id] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("%w: dependency cycle through %q", ErrPlanInvalid, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}
	for _, step := range steps {
		if color[step.ID] == white {
			if err := visit(step.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
