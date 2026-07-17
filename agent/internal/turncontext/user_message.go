package turncontext

import "context"

// UserMessageTurnContextSchemaVersion 是 Development Preview 已批准的最小不可变上下文版本。
const UserMessageTurnContextSchemaVersion = "user_message.turn_context.v2preview1"

// UserMessageTurnContext 是 Ensure/legacy apply 事务冻结并由 Runtime 逐值重验的不可变上下文。
// 它只保存精确引用与摘要，不携带用户正文、模型响应、Tool Registry 内容或可变运行时配置。
type UserMessageTurnContext struct {
	SchemaVersion string
	TurnID        string
	SessionID     string
	InputID       string
	MessageID     string
	UserID        string
	ProjectID     string

	MessageCutoffSeq     int64
	MessageContentDigest string
	SkillSnapshotRef     string
	SkillSnapshotDigest  string
	PromptRef            string
	PromptDigest         string
	ToolRegistryRef      string
	ToolRegistryDigest   string
	RuntimePolicyRef     string
	RuntimePolicyDigest  string
	ModelRouteRef        string
	ModelRouteDigest     string
	BudgetRef            string
	BudgetDigest         string
	AccessScopeRef       string
	AccessScopeDigest    string
	ContextDigest        string
}

// UserMessageRuntime 保存单次 Claim 的可信执行身份与对应不可变 Turn Context。
// RunID/Fence 由首次成功 Claim 冻结；其余稳定 ID 由 Ensure/legacy apply 预分配。
type UserMessageRuntime struct {
	Profile     string
	Owner       string
	RunID       string
	ModelCallID string
	OutputID    string
	FenceToken  int64
	Context     UserMessageTurnContext
}

type userMessageRuntimeKey struct{}

// WithUserMessageRuntime 返回携带值副本的新 Context，调用方后续修改原值不会改变可信上下文。
func WithUserMessageRuntime(ctx context.Context, value UserMessageRuntime) context.Context {
	return context.WithValue(ctx, userMessageRuntimeKey{}, value)
}

// UserMessageRuntimeFrom 读取 Runtime 注入的可信 User Message 上下文；缺失时模型必须失败关闭。
func UserMessageRuntimeFrom(ctx context.Context) (UserMessageRuntime, bool) {
	value, ok := ctx.Value(userMessageRuntimeKey{}).(UserMessageRuntime)
	return value, ok
}
