// Package turncontext 定义 Runner、主 Agent 与 Graph Tool 共享的不可变可信 Turn 上下文。
package turncontext

import (
	"context"
	"time"
)

// Preview 保存 CreationSpec Preview 单次稳定执行身份；所有字段由 Runtime 从 PostgreSQL Claim 事实注入。
type Preview struct {
	// Owner 是本次 Session Lane Lease 的 Processor 实例标识。
	Owner string
	// RequestID 是 Business 身份断言携带的请求 UUIDv7。
	RequestID string
	// UserID 是可信 Business Principal UUIDv7。
	UserID string
	// ProjectID 是可信 Business Project UUIDv7。
	ProjectID string
	// SessionID 是当前 Agent Session UUIDv7。
	SessionID string
	// InputID 是稳定 Session Input UUIDv7。
	InputID string
	// TurnID 是稳定 Runner Turn UUIDv7。
	TurnID string
	// RunID 是稳定 Runner Run UUIDv7。
	RunID string
	// ToolCallID 是稳定 plan_creation_spec Tool Call UUIDv7。
	ToolCallID string
	// BusinessCommandID 是 Save/Query 复用的 Business Command UUIDv7。
	BusinessCommandID string
	// PromptVersion 是入队冻结的 Graph Prompt pin。
	PromptVersion string
	// ValidatorVersion 是入队冻结的 Validator pin。
	ValidatorVersion string
	// FenceToken 是当前 Session Lane Claim Fence。
	FenceToken int64
}

type previewKey struct{}

// WithPreview 返回携带不可变值副本的新 Context；调用方不得把用户输入写入这些字段。
func WithPreview(ctx context.Context, value Preview) context.Context {
	return context.WithValue(ctx, previewKey{}, value)
}

// PreviewFrom 读取 Runtime 注入的可信 Preview 上下文；缺失时 Graph Tool 必须失败关闭。
func PreviewFrom(ctx context.Context) (Preview, bool) {
	value, ok := ctx.Value(previewKey{}).(Preview)
	return value, ok
}

// MaterialAnalysisPreview 保存 analyze_materials V2 Tool Core 的可信调用身份。
// 本类型不包含模型可控 Asset ID、Evidence 正文或权限查询结果，也不承诺已持久化 Receipt。
type MaterialAnalysisPreview struct {
	// Owner 是当前 Session Lane Lease owner；本批只校验非空并为后续 Fence 接入保留。
	Owner string
	// UserID 是可信 Business Principal UUIDv7。
	UserID string
	// ProjectID 是可信 Business Project UUIDv7。
	ProjectID string
	// SessionID 是当前 Agent Session UUIDv7。
	SessionID string
	// InputID 是稳定 Session Input UUIDv7。
	InputID string
	// TurnID 是稳定 Runner Turn UUIDv7。
	TurnID string
	// RunID 是当前 Runner Run UUIDv7。
	RunID string
	// ToolCallID 是稳定 analyze_materials Tool Call UUIDv7。
	ToolCallID string
	// FenceToken 是当前 Session Lane Claim Fence。
	FenceToken int64
	// PromptVersion 是调用前冻结的 Prompt pin。
	PromptVersion string
	// ValidatorVersion 是调用前冻结的 Validator pin。
	ValidatorVersion string
	// EvidencePolicyVersion 是调用前冻结的 Evidence Policy pin。
	EvidencePolicyVersion string
}

type materialAnalysisPreviewKey struct{}

// WithMaterialAnalysisPreview 返回携带不可变值副本的新 Context。
func WithMaterialAnalysisPreview(ctx context.Context, value MaterialAnalysisPreview) context.Context {
	return context.WithValue(ctx, materialAnalysisPreviewKey{}, value)
}

// MaterialAnalysisPreviewFrom 读取 Runtime 注入的可信素材分析上下文；缺失时 Tool 必须失败关闭。
func MaterialAnalysisPreviewFrom(ctx context.Context) (MaterialAnalysisPreview, bool) {
	value, ok := ctx.Value(materialAnalysisPreviewKey{}).(MaterialAnalysisPreview)
	return value, ok
}

// MediaPreview 保存 generate_media/assemble_output 两类可信结构化输入的稳定执行身份。
// Intent、Prompt 正文、Object Key 与 ffmpeg 参数不属于本上下文。
type MediaPreview struct {
	// Owner 是当前全局 Session Lane Claim owner。
	Owner string
	// RequestID 是 Business BFF 身份断言绑定的请求 UUIDv7。
	RequestID string
	// IdempotencyKey 是首次入队冻结并只用于同语义重放的 UUIDv7。
	IdempotencyKey string
	// UserID 是 Business 已认证用户 UUIDv7。
	UserID string
	// ProjectID 是 Business 已绑定项目 UUIDv7。
	ProjectID string
	// SessionID 是当前 Agent Session UUIDv7。
	SessionID string
	// InputID 是当前媒体请求 Input UUIDv7。
	InputID string
	// TurnID 是技术恢复复用的稳定 Turn UUIDv7。
	TurnID string
	// RunID 是 Lease takeover 复用的稳定 Run UUIDv7。
	RunID string
	// ToolCallID 是 deterministic dispatcher 产生的稳定 Tool Call UUIDv7。
	ToolCallID string
	// FenceToken 是当前 Session Lane Claim 的正整数 Fence。
	FenceToken int64
	// DeadlineAt 是入队冻结且下游不得延长的绝对 UTC Deadline。
	DeadlineAt time.Time
}

type mediaPreviewKey struct{}
type generateMediaPreviewKey struct{}
type assembleOutputPreviewKey struct{}

// WithMediaPreview 返回携带不可变值副本的新 Context。
func WithMediaPreview(ctx context.Context, value MediaPreview) context.Context {
	return context.WithValue(ctx, mediaPreviewKey{}, value)
}

// MediaPreviewFrom 读取媒体 Runtime 注入的可信上下文；缺失时 Tool 必须失败关闭。
func MediaPreviewFrom(ctx context.Context) (MediaPreview, bool) {
	value, ok := ctx.Value(mediaPreviewKey{}).(MediaPreview)
	return value, ok
}

// WithGenerateMediaPreview 注入 generate_media 专用可信路由标记及共用媒体身份。
func WithGenerateMediaPreview(ctx context.Context, value MediaPreview) context.Context {
	ctx = WithMediaPreview(ctx, value)
	return context.WithValue(ctx, generateMediaPreviewKey{}, value)
}

// GenerateMediaPreviewFrom 只读取 generate_media Runtime 的私有可信路由标记。
func GenerateMediaPreviewFrom(ctx context.Context) (MediaPreview, bool) {
	value, ok := ctx.Value(generateMediaPreviewKey{}).(MediaPreview)
	return value, ok
}

// WithAssembleOutputPreview 注入 assemble_output 专用可信路由标记及共用媒体身份。
func WithAssembleOutputPreview(ctx context.Context, value MediaPreview) context.Context {
	ctx = WithMediaPreview(ctx, value)
	return context.WithValue(ctx, assembleOutputPreviewKey{}, value)
}

// AssembleOutputPreviewFrom 只读取 assemble_output Runtime 的私有可信路由标记。
func AssembleOutputPreviewFrom(ctx context.Context) (MediaPreview, bool) {
	value, ok := ctx.Value(assembleOutputPreviewKey{}).(MediaPreview)
	return value, ok
}
