package writeprompts

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// candidateRoute 是候选协议 Branch 的显式联合；只允许严格 JSON 候选进入 exact-set Validator。
type candidateRoute struct {
	// Route 是 valid 或 invalid。
	Route string
	// TrustedContext 是原样透传的可信上下文。
	TrustedContext TrustedContext
	// Intent 是已校验写作意图。
	Intent Intent
	// Context 是 Business 一致读取快照。
	Context GenerationContext
	// Targets 是冻结目标全集。
	Targets []PromptTarget
	// ExactTargetSetDigest 是冻结 Scope 摘要。
	ExactTargetSetDigest string
	// OutputLanguage 是冻结输出语言。
	OutputLanguage string
	// Candidate 仅 valid 存在，是不含可信字段的模型候选。
	Candidate *Candidate
	// CandidateDigest 仅 valid 存在，是 canonical 候选摘要。
	CandidateDigest string
	// FailureCode 仅 invalid 存在且固定为候选协议错误码。
	FailureCode string
}

// contentRoute 是 exact-set Branch 的显式联合；只有可信字段回填后的完整 Content 才能进入 Save。
type contentRoute struct {
	// Route 是 valid 或 invalid。
	Route string
	// TrustedContext 是保存与失败结果使用的可信上下文。
	TrustedContext TrustedContext
	// Context 是保存 Guard 使用的 Business 一致快照。
	Context GenerationContext
	// Targets 是保存前仍需复核的冻结目标全集。
	Targets []PromptTarget
	// ExactTargetSetDigest 是保存命令绑定的 Scope 摘要。
	ExactTargetSetDigest string
	// Content 仅 valid 存在，包含按目标顺序回填的可信字段。
	Content *Content
	// FailureCode 仅 invalid 存在且固定为 exact-set 错误码。
	FailureCode string
}

// captureModelMessage 只接受无 ToolCall、Reasoning 或 Extra 的非空 Assistant JSON，并把最小副本写入单次 State。
func (*graphBuilder) captureModelMessage(_ context.Context, message *schema.Message, state *State) (*schema.Message, error) {
	if message == nil || message.Role != schema.Assistant || strings.TrimSpace(message.Content) == "" ||
		len(message.ToolCalls) != 0 || message.ReasoningContent != "" || len(message.Extra) != 0 {
		return nil, fmt.Errorf("capture prompt preview model message: invalid assistant response")
	}
	cloned := &schema.Message{Role: schema.Assistant, Content: message.Content}
	state.ModelMessage = cloned
	return &schema.Message{Role: cloned.Role, Content: cloned.Content}, nil
}

// validateCandidate 严格解析模型 JSON、Unicode 与文本边界；协议失败进入独立 Tool failed 分支，不产生保存副作用。
func (*graphBuilder) validateCandidate(ctx context.Context, message *schema.Message) (candidateRoute, error) {
	if message == nil {
		return candidateRoute{}, fmt.Errorf("validate prompt preview candidate: model message is nil")
	}
	var snapshot State
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		snapshot = *state
		return nil
	}); err != nil {
		return candidateRoute{}, err
	}
	candidate, err := DecodeAndValidateCandidate([]byte(message.Content))
	if err != nil {
		route := candidateRoute{Route: routeInvalid, TrustedContext: snapshot.TrustedContext, Intent: snapshot.Intent,
			Context: snapshot.StoryboardContext, Targets: snapshot.ExactTargets,
			ExactTargetSetDigest: snapshot.ExactTargetSetDigest, OutputLanguage: snapshot.OutputLanguage,
			FailureCode: ResultCodeCandidateInvalid}
		stateErr := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
			state.ValidationReport = ValidationReport{CandidateValid: false, Code: ResultCodeCandidateInvalid}
			state.Error = ResultCodeCandidateInvalid
			return nil
		})
		return route, stateErr
	}
	digest, err := CandidateDigest(candidate)
	if err != nil {
		return candidateRoute{}, err
	}
	route := candidateRoute{Route: routeValid, TrustedContext: snapshot.TrustedContext, Intent: snapshot.Intent,
		Context: snapshot.StoryboardContext, Targets: snapshot.ExactTargets,
		ExactTargetSetDigest: snapshot.ExactTargetSetDigest, OutputLanguage: snapshot.OutputLanguage,
		Candidate: &candidate, CandidateDigest: digest}
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		candidateCopy := candidate
		state.Candidate = &candidateCopy
		state.CandidateDigest = digest
		state.ValidationReport = ValidationReport{CandidateValid: true}
		return nil
	})
	return route, err
}

// validateExactSet 要求候选 key 数量与顺序精确等于冻结全集，并在通过后回填模型无权生成的 Source 字段。
func (*graphBuilder) validateExactSet(ctx context.Context, input candidateRoute) (contentRoute, error) {
	if input.Route != routeValid || input.Candidate == nil {
		return contentRoute{}, fmt.Errorf("validate prompt preview exact target set: invalid upstream route")
	}
	content, err := ValidateExactTargetSet(*input.Candidate, input.Targets, input.OutputLanguage, input.TrustedContext.StoryboardPreviewRef)
	if err != nil {
		route := contentRoute{Route: routeInvalid, TrustedContext: input.TrustedContext, Context: input.Context,
			Targets: input.Targets, ExactTargetSetDigest: input.ExactTargetSetDigest, FailureCode: ResultCodeExactSetInvalid}
		stateErr := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
			state.ValidationReport.ExactSetValid = false
			state.ValidationReport.Code = ResultCodeExactSetInvalid
			state.Error = ResultCodeExactSetInvalid
			return nil
		})
		return route, stateErr
	}
	route := contentRoute{Route: routeValid, TrustedContext: input.TrustedContext, Context: input.Context,
		Targets: input.Targets, ExactTargetSetDigest: input.ExactTargetSetDigest, Content: &content}
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.ValidationReport.ExactSetValid = true
		contentCopy := content
		state.Content = &contentCopy
		return nil
	})
	return route, err
}
