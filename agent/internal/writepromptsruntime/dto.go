package writepromptsruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
)

var canonicalUUIDv7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var canonicalSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

var (
	// ErrInvalidClaim 表示持久化 Claim 不满足 Profile exact-set。
	ErrInvalidClaim = errors.New("write prompts runtime claim is invalid")
	// ErrReceiptConflict 表示同稳定键出现异义摘要或损坏回执。
	ErrReceiptConflict = errors.New("write prompts runtime receipt conflict")
	// ErrOutputContract 表示 Eino 或 Tool 输出不满足严格结果契约。
	ErrOutputContract = errors.New("write prompts runtime output contract violation")
	// ErrModelReceiptReserved 表示同 Fence 已经占位但没有可重放终态。
	ErrModelReceiptReserved = errors.New("write prompts model receipt is reserved")
	// ErrFenceLost 表示 owner/fence 已失效；调用方不得继续写回执或终态。
	ErrFenceLost = errors.New("write prompts runtime fence lost")
	// ErrPersistence 表示持久化提交结果未知或读取暂时不可用。
	ErrPersistence = errors.New("write prompts runtime persistence unavailable")
	// ErrIdempotencyConflict 表示同 Session 与幂等键承载了不同 typed Intent 或 StoryboardPreviewRef。
	ErrIdempotencyConflict = errors.New("write prompts runtime idempotency conflict")
	// ErrInvalidInput 表示 Handler 可稳定映射为 400 的严格请求失败。
	ErrInvalidInput = errors.New("write prompts runtime invalid input")
	// ErrNotFound 表示 Session、Project 或 PromptPreview 不存在或不可见。
	ErrNotFound = errors.New("write prompts runtime resource not found")
	// ErrSessionLaneBlocked 表示当前 Session Lane 不能接受本 Profile 输入。
	ErrSessionLaneBlocked = errors.New("write prompts runtime session lane blocked")
	// ErrRecoveryDeferred 表示 prepared/unknown 命令仍未形成权威终态，必须保持 HOL 未决。
	ErrRecoveryDeferred = errors.New("write prompts runtime recovery deferred")
	// ErrRecoveryExhausted 表示最后一次同键重发后 Business 仍权威 not_found，必须终结为 runtime failure。
	ErrRecoveryExhausted = errors.New("write prompts runtime recovery exhausted")
)

// EnqueueCommand 是显式 typed Intent 与可信 StoryboardPreviewRef 的入队端口，不创建 session_message。
type EnqueueCommand struct {
	// RequestID 是当前请求 UUIDv7。
	RequestID string
	// SessionID 是目标 Session UUIDv7。
	SessionID string
	// UserID 是认证用户 UUIDv7。
	UserID string
	// ProjectID 是已校验项目 UUIDv7。
	ProjectID string
	// IdempotencyKey 是 Session 内 first-write-wins UUIDv7。
	IdempotencyKey string
	// StoryboardPreviewRef 是 BFF 绑定并由 Runtime 冻结的上游 Draft 引用。
	StoryboardPreviewRef writeprompts.StoryboardPreviewRef
	// IntentJSON 是 canonical Tool Intent 明文，持久化实现负责加密。
	IntentJSON []byte
	// AccessScopeRef 是认证范围引用。
	AccessScopeRef string
	// AccessScopeDigest 是认证范围摘要。
	AccessScopeDigest string
	// IntentKeyVersion 是 Intent 密文密钥版本。
	IntentKeyVersion string
}

// EnqueueResult 是首次事务预分配的全部稳定身份；幂等重放必须返回同一组值。
type EnqueueResult struct {
	// InputID 是 typed Session Input UUIDv7。
	InputID string
	// TurnID 是稳定 Turn UUIDv7。
	TurnID string
	// RunID 是稳定 Run UUIDv7。
	RunID string
	// ToolCallID 是唯一 ToolCall UUIDv7。
	ToolCallID string
	// BusinessCommandID 是 Prompt 保存与恢复复用的命令 UUIDv7。
	BusinessCommandID string
	// RouterModelCallID 是外层 Router 模型回执 UUIDv7。
	RouterModelCallID string
	// GraphModelCallID 是 Graph Prompt 模型回执 UUIDv7。
	GraphModelCallID string
	// AcceptedEventID 是首次 accepted Event UUIDv7。
	AcceptedEventID string
	// TerminalEventID 是预分配终态 Event UUIDv7。
	TerminalEventID string
	// Replayed 表示同义幂等重放。
	Replayed bool
}

// Claim 是 Repository 从全 Source HOL 交给本 Profile 的稳定执行事实。
type Claim struct {
	// Owner 是当前 Lease owner 实例值。
	Owner string
	// FenceToken 是当前 Claim 的隔离令牌。
	FenceToken int64
	// Attempts 是执行尝试序号，从一开始。
	Attempts int
	// EnqueueSeq 是 Session 全 Source 排序序号。
	EnqueueSeq int64
	// TerminalEventID 是预分配终态 Event UUIDv7。
	TerminalEventID string
	// IntentJSON 是认证解密并复验的 canonical Intent。
	IntentJSON []byte
	// Context 是入队事务冻结的不可变 Turn Context。
	Context turncontext.WritePromptsTurnContext
}

// RuntimeFailure 是与合法 Tool failed Result 分离的稳定运行时失败投影。
type RuntimeFailure struct {
	// SchemaVersion 是运行时失败 DTO 版本。
	SchemaVersion string `json:"schema_version"`
	// InputID 是失败输入 UUIDv7。
	InputID string `json:"input_id"`
	// TurnID 是失败 Turn UUIDv7。
	TurnID string `json:"turn_id"`
	// RunID 是失败 Run UUIDv7。
	RunID string `json:"run_id"`
	// Code 是安全稳定错误码。
	Code string `json:"code"`
	// Summary 是不含内部细节的中文摘要。
	Summary string `json:"summary"`
	// Retryable 表示本运行时终态能否由用户直接重试。
	Retryable bool `json:"retryable"`
}

// ValidateClaim 逐值复核稳定身份、Profile pins、canonical Intent、StoryboardPreviewRef 与 Context digest。
func ValidateClaim(claim Claim) error {
	ctx := claim.Context
	if claim.Owner == "" || claim.FenceToken < 1 || claim.Attempts < 1 || claim.EnqueueSeq < 1 ||
		ctx.SchemaVersion != turncontext.WritePromptsTurnContextSchemaVersion || ctx.Profile != Profile ||
		!canonicalUUIDv7.MatchString(ctx.RequestID) || !canonicalUUIDv7.MatchString(ctx.SessionID) ||
		!canonicalUUIDv7.MatchString(ctx.InputID) || !canonicalUUIDv7.MatchString(ctx.TurnID) ||
		!canonicalUUIDv7.MatchString(ctx.RunID) || !canonicalUUIDv7.MatchString(ctx.ToolCallID) ||
		!canonicalUUIDv7.MatchString(ctx.BusinessCommandID) || !canonicalUUIDv7.MatchString(ctx.RouterModelCallID) ||
		!canonicalUUIDv7.MatchString(ctx.GraphModelCallID) || !canonicalUUIDv7.MatchString(ctx.UserID) ||
		!canonicalUUIDv7.MatchString(ctx.ProjectID) || !canonicalUUIDv7.MatchString(claim.TerminalEventID) ||
		ctx.IntentKeyVersion == "" || !canonicalUUIDv7.MatchString(ctx.StoryboardPreviewID) ||
		ctx.StoryboardPreviewVersion != 1 || !canonicalSHA256.MatchString(ctx.StoryboardPreviewContentDigest) {
		return ErrInvalidClaim
	}
	if _, err := ValidateCanonicalIntent(claim.IntentJSON, ctx.IntentDigest); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	if err := validatePins(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	digest, err := DigestTurnContext(ctx)
	if err != nil || digest != ctx.ContextDigest {
		return fmt.Errorf("%w: context digest mismatch", ErrInvalidClaim)
	}
	return nil
}

func validatePins(ctx turncontext.WritePromptsTurnContext) error {
	pins := ApprovedPins()
	if ctx.ToolRegistryRef != pins.ToolRegistryRef || ctx.ToolRegistryDigest != pins.ToolRegistryDigest ||
		ctx.ToolDefinitionRef != pins.ToolDefinitionRef || ctx.ToolDefinitionDigest != pins.ToolDefinitionDigest ||
		ctx.IntentSchemaRef != writeprompts.IntentSchemaVersion || ctx.CandidateSchemaRef != writeprompts.CandidateSchemaVersion ||
		ctx.ResultSchemaRef != writeprompts.ResultSchemaVersion || ctx.PromptRef != pins.PromptRef ||
		ctx.PromptDigest != pins.PromptDigest || ctx.ValidatorRef != pins.ValidatorRef ||
		ctx.ValidatorDigest != pins.ValidatorDigest || ctx.ExactSetValidatorRef != pins.ExactSetValidatorRef ||
		ctx.ExactSetValidatorDigest != pins.ExactSetValidatorDigest || ctx.RouterModelRouteRef != pins.RouterModelRouteRef ||
		ctx.RouterModelRouteDigest != pins.RouterModelRouteDigest || ctx.PromptModelRouteRef != pins.PromptModelRouteRef ||
		ctx.PromptModelRouteDigest != pins.PromptModelRouteDigest || ctx.RuntimePolicyRef != pins.RuntimePolicyRef ||
		ctx.RuntimePolicyDigest != pins.RuntimePolicyDigest || ctx.BudgetRef != pins.BudgetRef ||
		ctx.BudgetDigest != pins.BudgetDigest || ctx.AccessScopeRef == "" || !canonicalSHA256.MatchString(ctx.AccessScopeDigest) {
		return fmt.Errorf("profile pins mismatch")
	}
	return nil
}

// DigestTurnContext 对不含 ContextDigest 自身的具名 wire DTO 计算稳定摘要。
func DigestTurnContext(value turncontext.WritePromptsTurnContext) (string, error) {
	value.ContextDigest = ""
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("digest write prompts turn context: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// RuntimeContextFromClaim 构造 Router、Graph Model、Tool Wrapper 与 CommandJournal 共用的可信值。
func RuntimeContextFromClaim(claim Claim) turncontext.WritePromptsRuntime {
	return turncontext.WritePromptsRuntime{
		Owner: claim.Owner, FenceToken: claim.FenceToken,
		IntentJSON: string(append([]byte(nil), claim.IntentJSON...)), Context: claim.Context,
	}
}

// PreviewContextFromClaim 显式映射 M1 Graph Tool Core 允许读取的最小可信 Context。
func PreviewContextFromClaim(claim Claim) turncontext.WritePromptsPreview {
	ctx := claim.Context
	return turncontext.WritePromptsPreview{
		Owner: claim.Owner, RequestID: ctx.RequestID, UserID: ctx.UserID, ProjectID: ctx.ProjectID,
		SessionID: ctx.SessionID, InputID: ctx.InputID, TurnID: ctx.TurnID, RunID: ctx.RunID,
		ToolCallID: ctx.ToolCallID, BusinessCommandID: ctx.BusinessCommandID, FenceToken: claim.FenceToken,
		StoryboardPreviewID: ctx.StoryboardPreviewID, StoryboardPreviewVersion: ctx.StoryboardPreviewVersion,
		StoryboardPreviewContentDigest: ctx.StoryboardPreviewContentDigest, PromptVersion: writeprompts.PromptVersion,
		ValidatorVersion: writeprompts.ValidatorVersion, ExactSetValidatorVersion: writeprompts.ExactSetValidatorVersion,
	}
}

// CoreContextFromRuntime 构造 Graph Tool Core 需要的最小可信上下文，模型参数无权覆盖任何字段。
func CoreContextFromRuntime(value turncontext.WritePromptsRuntime) writeprompts.TrustedContext {
	return CoreContextFromPreview(PreviewContextFromClaim(Claim{
		Owner: value.Owner, FenceToken: value.FenceToken, Context: value.Context,
	}))
}

// CoreContextFromPreview 显式映射 Runtime 注入的最小 Tool Core Context。
func CoreContextFromPreview(ctx turncontext.WritePromptsPreview) writeprompts.TrustedContext {
	return writeprompts.TrustedContext{
		Owner: ctx.Owner, RequestID: ctx.RequestID, UserID: ctx.UserID, ProjectID: ctx.ProjectID,
		SessionID: ctx.SessionID, InputID: ctx.InputID, TurnID: ctx.TurnID, RunID: ctx.RunID,
		ToolCallID: ctx.ToolCallID, BusinessCommandID: ctx.BusinessCommandID, FenceToken: ctx.FenceToken,
		StoryboardPreviewRef: writeprompts.StoryboardPreviewRef{ID: ctx.StoryboardPreviewID, Version: ctx.StoryboardPreviewVersion, ContentDigest: ctx.StoryboardPreviewContentDigest},
		PromptVersion:        ctx.PromptVersion, ValidatorVersion: ctx.ValidatorVersion,
		ExactSetValidatorVersion: ctx.ExactSetValidatorVersion, Policy: ApprovedPolicy(),
	}
}

// ResolveCoreContext 从 Runtime 私有 Context 提供 M1 Tool Core 需要的可信 Resolver。
func ResolveCoreContext(ctx context.Context) (writeprompts.TrustedContext, bool) {
	value, ok := turncontext.WritePromptsPreviewFrom(ctx)
	if !ok {
		return writeprompts.TrustedContext{}, false
	}
	return CoreContextFromPreview(value), true
}
