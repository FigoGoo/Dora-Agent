package analyzematerialsruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
)

var canonicalUUIDv7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

var (
	// ErrInvalidClaim 表示持久化 Claim 不满足 Profile exact-set。
	ErrInvalidClaim = errors.New("analyze materials runtime claim is invalid")
	// ErrReceiptConflict 表示同稳定键出现异义摘要或损坏回执。
	ErrReceiptConflict = errors.New("analyze materials runtime receipt conflict")
	// ErrOutputContract 表示 Eino 或 Tool 输出不满足严格结果契约。
	ErrOutputContract = errors.New("analyze materials runtime output contract violation")
	// ErrModelReceiptReserved 表示同 Fence 已经占位但没有可重放终态。
	ErrModelReceiptReserved = errors.New("analyze materials model receipt is reserved")
	// ErrFenceLost 表示 owner/fence 已失效；调用方不得继续写 Receipt、Event 或终态。
	ErrFenceLost = errors.New("analyze materials runtime fence lost")
	// ErrPersistence 表示持久化提交结果未知或读取暂时不可用，应进入 receipt/projection 恢复。
	ErrPersistence = errors.New("analyze materials runtime persistence unavailable")
	// ErrIdempotencyConflict 表示同 Session 与幂等键承载了不同 typed Intent。
	ErrIdempotencyConflict = errors.New("analyze materials runtime idempotency conflict")
	// ErrInvalidInput 表示 Handler 可稳定映射为 400 的严格请求失败。
	ErrInvalidInput = errors.New("analyze materials runtime invalid input")
	// ErrNotFound 表示 Session 或 Project 不存在、不可见，Handler 可稳定映射为 404。
	ErrNotFound = errors.New("analyze materials runtime resource not found")
	// ErrSessionLaneBlocked 表示当前 Session Lane 不能接受本 Profile 输入，稳定映射为 409。
	ErrSessionLaneBlocked = errors.New("analyze materials runtime session lane blocked")
)

// EnqueueCommand 是显式 typed Intent 入队端口，不创建 session_message。
type EnqueueCommand struct {
	RequestID         string
	SessionID         string
	UserID            string
	ProjectID         string
	IdempotencyKey    string
	IntentJSON        []byte
	AccessScopeRef    string
	AccessScopeDigest string
	IntentKeyVersion  string
}

// EnqueueResult 是首次事务预分配的全部稳定身份；幂等重放必须返回同一组值。
type EnqueueResult struct {
	InputID           string
	TurnID            string
	RunID             string
	ToolCallID        string
	RouterModelCallID string
	GraphModelCallID  string
	AcceptedEventID   string
	TerminalEventID   string
	Replayed          bool
}

// Claim 是 Repository 从全 Source HOL 交给本 Profile 的稳定执行事实。
type Claim struct {
	Owner             string
	FenceToken        int64
	Attempts          int
	EnqueueSeq        int64
	RouterModelCallID string
	GraphModelCallID  string
	TerminalEventID   string
	IntentJSON        []byte
	Context           turncontext.MaterialAnalysisTurnContext
}

// RuntimeFailure 是与合法 Tool failed Result 分离的稳定运行时失败投影。
type RuntimeFailure struct {
	SchemaVersion string `json:"schema_version"`
	InputID       string `json:"input_id"`
	TurnID        string `json:"turn_id"`
	RunID         string `json:"run_id"`
	Code          string `json:"code"`
	Summary       string `json:"summary"`
	Retryable     bool   `json:"retryable"`
}

// ValidateClaim 逐值复核稳定身份、Profile pins、canonical Intent 和 Context digest。
func ValidateClaim(claim Claim) error {
	ctx := claim.Context
	if claim.Owner == "" || claim.FenceToken < 1 || claim.Attempts < 1 || claim.EnqueueSeq < 1 ||
		ctx.SchemaVersion != turncontext.MaterialAnalysisTurnContextSchemaVersion || ctx.Profile != Profile ||
		!canonicalUUIDv7.MatchString(ctx.SessionID) || !canonicalUUIDv7.MatchString(ctx.InputID) ||
		!canonicalUUIDv7.MatchString(ctx.TurnID) || !canonicalUUIDv7.MatchString(ctx.RunID) ||
		!canonicalUUIDv7.MatchString(ctx.ToolCallID) || !canonicalUUIDv7.MatchString(ctx.UserID) ||
		!canonicalUUIDv7.MatchString(ctx.ProjectID) || !canonicalUUIDv7.MatchString(claim.RouterModelCallID) ||
		!canonicalUUIDv7.MatchString(claim.GraphModelCallID) || !canonicalUUIDv7.MatchString(claim.TerminalEventID) ||
		ctx.IntentKeyVersion == "" {
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

func validatePins(ctx turncontext.MaterialAnalysisTurnContext) error {
	pins := ApprovedPins()
	if ctx.ToolRegistryRef != pins.ToolRegistryRef || ctx.ToolRegistryDigest != pins.ToolRegistryDigest ||
		ctx.ToolDefinitionRef != pins.ToolDefinitionRef || ctx.ToolDefinitionDigest != pins.ToolDefinitionDigest ||
		ctx.IntentSchemaRef != analyzematerials.IntentSchemaVersion || ctx.ResultSchemaRef != analyzematerials.ResultSchemaVersion ||
		ctx.PromptRef != pins.PromptRef || ctx.PromptDigest != pins.PromptDigest ||
		ctx.ValidatorRef != pins.ValidatorRef || ctx.ValidatorDigest != pins.ValidatorDigest ||
		ctx.EvidencePolicyRef != pins.EvidencePolicyRef || ctx.EvidencePolicyDigest != pins.EvidencePolicyDigest ||
		ctx.RouterModelRouteRef != pins.RouterModelRouteRef || ctx.RouterModelRouteDigest != pins.RouterModelRouteDigest ||
		ctx.AnalysisModelRouteRef != pins.AnalysisModelRouteRef || ctx.AnalysisModelRouteDigest != pins.AnalysisModelRouteDigest ||
		ctx.RuntimePolicyRef != pins.RuntimePolicyRef || ctx.RuntimePolicyDigest != pins.RuntimePolicyDigest ||
		ctx.BudgetRef != pins.BudgetRef || ctx.BudgetDigest != pins.BudgetDigest ||
		ctx.AccessScopeRef == "" || len(ctx.AccessScopeDigest) != 64 {
		return fmt.Errorf("profile pins mismatch")
	}
	return nil
}

// DigestTurnContext 对不含 ContextDigest 自身的具名 wire DTO 计算稳定摘要。
func DigestTurnContext(value turncontext.MaterialAnalysisTurnContext) (string, error) {
	value.ContextDigest = ""
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("digest material analysis turn context: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// RuntimeContextFromClaim 构造 Router、Graph Model 与 Tool Wrapper 共用的可信 Context 值。
func RuntimeContextFromClaim(claim Claim) turncontext.MaterialAnalysisRuntime {
	return turncontext.MaterialAnalysisRuntime{
		Owner: claim.Owner, FenceToken: claim.FenceToken,
		RouterModelCallID: claim.RouterModelCallID, GraphModelCallID: claim.GraphModelCallID,
		IntentJSON: string(append([]byte(nil), claim.IntentJSON...)), Context: claim.Context,
	}
}

// CoreContextFromClaim 构造现有 Tool Core 已批准的最小可信上下文。
func CoreContextFromClaim(claim Claim) turncontext.MaterialAnalysisPreview {
	ctx := claim.Context
	return turncontext.MaterialAnalysisPreview{
		Owner: claim.Owner, UserID: ctx.UserID, ProjectID: ctx.ProjectID, SessionID: ctx.SessionID,
		InputID: ctx.InputID, TurnID: ctx.TurnID, RunID: ctx.RunID, ToolCallID: ctx.ToolCallID,
		FenceToken: claim.FenceToken, PromptVersion: analyzematerials.PromptVersion,
		ValidatorVersion: analyzematerials.ValidatorVersion, EvidencePolicyVersion: analyzematerials.EvidencePolicyVersion,
	}
}
