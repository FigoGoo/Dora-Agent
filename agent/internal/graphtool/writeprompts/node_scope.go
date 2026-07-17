package writeprompts

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/compose"
)

// validatedInput 是 validate_intent 的无副作用输出，绑定可信上下文与 canonical Intent 摘要。
type validatedInput struct {
	// TrustedContext 是 Runtime 注入且已经过 UUID、Fence、Source 与版本 Pin 校验的上下文。
	TrustedContext TrustedContext
	// Intent 是严格解码且不含 Source/目标/身份的模型可控字段。
	Intent Intent
	// IntentDigest 是后续 Scope 摘要必须复用的小写 SHA-256。
	IntentDigest string
}

// contextInput 是 load_storyboard_preview 的输出，只承载一次 Business 一致快照。
type contextInput struct {
	// TrustedContext 是原样透传的可信上下文。
	TrustedContext TrustedContext
	// Intent 是已校验写作意图。
	Intent Intent
	// IntentDigest 是已校验写作意图摘要。
	IntentDigest string
	// Context 是 Owner 与 Source exact-match 后的最小 Project/Storyboard 快照。
	Context GenerationContext
}

// targetRoute 是 Scope Branch 的显式联合；valid 必须含完整目标与摘要，invalid 只含稳定失败码。
type targetRoute struct {
	// Route 是 valid 或 invalid。
	Route string
	// TrustedContext 是后续 Prompt、失败结果与保存共同使用的可信上下文。
	TrustedContext TrustedContext
	// Intent 是已校验写作意图。
	Intent Intent
	// IntentDigest 是 Scope 摘要输入。
	IntentDigest string
	// Context 是 Business 一致读取快照。
	Context GenerationContext
	// Targets 是 Source 全部 Slot 的稳定有序副本；invalid 时为空。
	Targets []PromptTarget
	// ExactTargetSetDigest 覆盖 Source、目标、Intent 与全部实现 Pin；invalid 时为空。
	ExactTargetSetDigest string
	// OutputLanguage 是 Intent 或冻结 Policy 解析出的最终语言。
	OutputLanguage string
	// FailureCode 仅 invalid 存在，只允许零目标或预算超限。
	FailureCode string
}

// validateIntent 先校验 Runtime 私有上下文，再严格解码 Intent 并写入单次 State；任何错误都在模型调用前失败且无副作用。
func (b *graphBuilder) validateIntent(ctx context.Context, input GraphInput) (validatedInput, error) {
	if err := ValidateTrustedContext(input.TrustedContext); err != nil {
		return validatedInput{}, err
	}
	intent, err := DecodeIntent(input.IntentJSON)
	if err != nil {
		return validatedInput{}, err
	}
	digest, err := IntentDigest(intent)
	if err != nil {
		return validatedInput{}, err
	}
	result := validatedInput{TrustedContext: input.TrustedContext, Intent: intent, IntentDigest: digest}
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.TrustedContext = input.TrustedContext
		state.Intent = intent
		state.IntentDigest = digest
		state.StoryboardPreviewRef = input.TrustedContext.StoryboardPreviewRef
		return nil
	})
	return result, err
}

// loadStoryboardPreview 通过单次 Business Query 读取 Source 快照并复核 Owner/version/digest；失败不会调用模型或写 Draft。
func (b *graphBuilder) loadStoryboardPreview(ctx context.Context, input validatedInput) (contextInput, error) {
	value, err := b.reader.GetPromptGenerationContext(ctx, input.TrustedContext)
	if err != nil {
		return contextInput{}, err
	}
	if err := ValidateGenerationContext(value, input.TrustedContext); err != nil {
		return contextInput{}, err
	}
	result := contextInput{TrustedContext: input.TrustedContext, Intent: input.Intent, IntentDigest: input.IntentDigest, Context: value}
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.StoryboardContext = value
		return nil
	})
	return result, err
}

// resolveExactTargets 冻结全部 Source Slot 的顺序、媒体映射、语言与 Scope 摘要；零目标或超预算转为确定性失败分支，绝不截断。
func (*graphBuilder) resolveExactTargets(ctx context.Context, input contextInput) (targetRoute, error) {
	targets, digest, outputLanguage, err := ResolveExactTargets(input.Context, input.Intent, input.IntentDigest, input.TrustedContext)
	if err != nil {
		failureCode := ""
		switch {
		case errors.Is(err, ErrNoTargets):
			failureCode = ResultCodeNoTargets
		case errors.Is(err, ErrTargetBudgetExceeded):
			failureCode = ResultCodeTargetBudgetExceeded
		default:
			return targetRoute{}, err
		}
		route := targetRoute{Route: routeInvalid, TrustedContext: input.TrustedContext, Intent: input.Intent,
			IntentDigest: input.IntentDigest, Context: input.Context, FailureCode: failureCode}
		stateErr := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
			state.ValidationReport.Code = failureCode
			state.Error = failureCode
			return nil
		})
		return route, stateErr
	}
	route := targetRoute{Route: routeValid, TrustedContext: input.TrustedContext, Intent: input.Intent,
		IntentDigest: input.IntentDigest, Context: input.Context, Targets: targets,
		ExactTargetSetDigest: digest, OutputLanguage: outputLanguage}
	err = compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.ExactTargets = append([]PromptTarget(nil), targets...)
		state.ExactTargetSetDigest = digest
		state.OutputLanguage = outputLanguage
		return nil
	})
	return route, err
}
