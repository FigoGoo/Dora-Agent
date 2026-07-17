package planstoryboardruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ToolReceiptStage 是 Storyboard Tool Receipt 的 exact-set 状态。
type ToolReceiptStage string

const (
	// ToolReceiptOpen 表示 ToolCall 已创建但尚未越过 Business 副作用边界。
	ToolReceiptOpen ToolReceiptStage = "open"
	// ToolReceiptBusinessPrepared 表示完整 Business 命令已冻结，可能尚未发送。
	ToolReceiptBusinessPrepared ToolReceiptStage = "business_prepared"
	// ToolReceiptBusinessUnknown 表示 Save 可能提交且查询未消除歧义。
	ToolReceiptBusinessUnknown ToolReceiptStage = "business_unknown"
	// ToolReceiptCompleted 表示 completed Tool Result 已 first-write-wins 冻结。
	ToolReceiptCompleted ToolReceiptStage = "completed"
	// ToolReceiptFailed 表示确定性 failed Tool Result 已 first-write-wins 冻结。
	ToolReceiptFailed ToolReceiptStage = "failed"
)

// ToolReceiptIdentity 把 Tool 回执绑定到稳定 ToolCall 与当前 owner/fence。
type ToolReceiptIdentity struct {
	// Owner 是当前 Lease owner。
	Owner string
	// FenceToken 是当前 Claim 隔离令牌。
	FenceToken int64
	// SessionID 是 Session UUIDv7。
	SessionID string
	// InputID 是 Input UUIDv7。
	InputID string
	// TurnID 是 Turn UUIDv7。
	TurnID string
	// RunID 是 Run UUIDv7。
	RunID string
	// ToolCallID 是 ToolCall UUIDv7。
	ToolCallID string
	// BusinessCommandID 是 prepared/save/query 共用命令 UUIDv7。
	BusinessCommandID string
}

// ToolReceiptSnapshot 是 Processor、ReceiptTool 与 Recovery 共用的 PostgreSQL 真源形状。
type ToolReceiptSnapshot struct {
	// Stage 是回执状态。
	Stage ToolReceiptStage
	// RequestDigest 是外层 Tool 语义摘要。
	RequestDigest string
	// PreparedCommand 是 prepared/unknown 阶段由当前 Fence 重建的完整命令。
	PreparedCommand *planstoryboard.DraftCommand
	// PreparedCommandDigest 是排除易变 Owner/Fence 的命令工件摘要。
	PreparedCommandDigest string
	// ContentDigest 是 prepared 命令正文摘要。
	ContentDigest string
	// Recovery 是 business_unknown 阶段的持久化恢复预算与命令事实。
	Recovery *planstoryboard.RecoveryDeferred
	// ResultJSON 是 completed/failed canonical Tool Result。
	ResultJSON []byte
	// ResultDigest 是 ResultJSON 的小写 SHA-256。
	ResultDigest string
}

// ToolReceiptStore 由 PostgreSQL Adapter 实现 open、prepared、unknown、终态和 stale Fence CAS。
type ToolReceiptStore interface {
	// ReplayOrOpenTool 同键同义重放，首次调用创建或认领 open 回执。
	ReplayOrOpenTool(context.Context, ToolReceiptIdentity, string) (ToolReceiptSnapshot, bool, error)
	// PrepareToolCommand 在 Save RPC 前冻结完整稳定命令、摘要和重发预算。
	PrepareToolCommand(context.Context, ToolReceiptIdentity, string, planstoryboard.DraftCommand, string, string, int) error
	// MarkToolBusinessUnknown 只把 prepared/unknown 保持为 business_unknown，不冻结 Tool Result。
	MarkToolBusinessUnknown(context.Context, ToolReceiptIdentity, string) error
	// ReserveToolCommandResend 在权威 not_found 后原子消耗一次持久化重发预算。
	ReserveToolCommandResend(context.Context, ToolReceiptIdentity, string, planstoryboard.RecoveryDeferred) (planstoryboard.RecoveryDeferred, bool, error)
	// FreezeToolResult 从 open/prepared/unknown first-write-wins 冻结 completed/failed 结果。
	FreezeToolResult(context.Context, ToolReceiptIdentity, string, ToolReceiptStage, []byte, string) error
}

// CommandJournal 把 Graph 的 PrepareCommand/ReserveCommandResend 端口接到 Tool Receipt 真源。
type CommandJournal struct{ store ToolReceiptStore }

// NewCommandJournal 创建 Save RPC 前必须调用的持久化命令 Journal。
func NewCommandJournal(store ToolReceiptStore) (*CommandJournal, error) {
	if store == nil {
		return nil, fmt.Errorf("create plan storyboard command journal: store is required")
	}
	return &CommandJournal{store: store}, nil
}

// PrepareCommand 在副作用前验证 Runtime identity 与 Command 一致，并冻结完整稳定正文。
func (j *CommandJournal) PrepareCommand(ctx context.Context, command planstoryboard.DraftCommand) error {
	trusted, ok := turncontext.PlanStoryboardRuntimeFrom(ctx)
	if !ok || trusted.Context.Profile != Profile || trusted.Owner == "" || trusted.FenceToken < 1 {
		return fmt.Errorf("prepare plan storyboard command: trusted runtime context is invalid")
	}
	core := CoreContextFromRuntime(trusted)
	if command.TrustedContext != core {
		return fmt.Errorf("prepare plan storyboard command: trusted command changed")
	}
	recomputed, err := planstoryboard.SaveRequestDigest(command)
	if err != nil || recomputed != command.RequestDigest {
		return fmt.Errorf("prepare plan storyboard command: request digest mismatch")
	}
	commandDigest, err := digestPreparedCommand(command)
	if err != nil {
		return err
	}
	contentDigest, err := planstoryboard.ContentDigest(command.Content)
	if err != nil {
		return err
	}
	outerDigest := digestToolRequest(trusted.Context, trusted.Context.IntentDigest)
	return j.store.PrepareToolCommand(ctx, toolReceiptIdentity(trusted), outerDigest, command, commandDigest, contentDigest, BusinessResendLimit)
}

// ReserveCommandResend 只代理 PostgreSQL 原子预算预留；Graph 内不形成循环。
func (j *CommandJournal) ReserveCommandResend(
	ctx context.Context,
	trustedContext planstoryboard.TrustedContext,
	recovery planstoryboard.RecoveryDeferred,
) (planstoryboard.RecoveryDeferred, bool, error) {
	trusted, ok := turncontext.PlanStoryboardRuntimeFrom(ctx)
	if !ok || trustedContext != CoreContextFromRuntime(trusted) {
		return planstoryboard.RecoveryDeferred{}, false, ErrFenceLost
	}
	outerDigest := digestToolRequest(trusted.Context, trusted.Context.IntentDigest)
	return j.store.ReserveToolCommandResend(ctx, toolReceiptIdentity(trusted), outerDigest, recovery)
}

// ReceiptTool 在 M1 Tool Core 外实现 first-write-wins 终态回执和 Business Unknown 阶段。
type ReceiptTool struct {
	base  einotool.InvokableTool
	store ToolReceiptStore
	info  *schema.ToolInfo
}

// NewReceiptTool 只接受 exact plan_storyboard Tool，拒绝任何额外或别名能力。
func NewReceiptTool(ctx context.Context, base einotool.InvokableTool, store ToolReceiptStore) (*ReceiptTool, error) {
	if base == nil || store == nil {
		return nil, fmt.Errorf("create plan storyboard receipt tool: tool and store are required")
	}
	info, err := base.Info(ctx)
	if err != nil || planstoryboard.ValidateToolInfo(info) != nil {
		return nil, fmt.Errorf("create plan storyboard receipt tool: exact plan_storyboard tool is required")
	}
	canonical, err := planstoryboard.CanonicalToolInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("create plan storyboard receipt tool: canonical tool definition: %w", err)
	}
	return &ReceiptTool{base: base, store: store, info: canonical}, nil
}

// Info 返回已验证的唯一 Tool Schema。
func (t *ReceiptTool) Info(context.Context) (*schema.ToolInfo, error) {
	if t == nil || t.info == nil {
		return nil, fmt.Errorf("read plan storyboard receipt tool info: tool is not initialized")
	}
	return t.info, nil
}

// InvokableRun 先重放 frozen Result；open 时执行一次 Core，prepared/unknown 交给 Processor 恢复。
func (t *ReceiptTool) InvokableRun(ctx context.Context, arguments string, options ...einotool.Option) (string, error) {
	trusted, ok := turncontext.PlanStoryboardRuntimeFrom(ctx)
	if !ok || trusted.Context.Profile != Profile || trusted.Owner == "" || trusted.FenceToken < 1 {
		return "", fmt.Errorf("run plan storyboard receipt tool: trusted runtime context is invalid")
	}
	intent, err := ValidateCanonicalIntent([]byte(arguments), trusted.Context.IntentDigest)
	if err != nil || arguments != trusted.IntentJSON {
		return "", fmt.Errorf("run plan storyboard receipt tool: router arguments changed frozen intent")
	}
	identity := toolReceiptIdentity(trusted)
	requestDigest := digestToolRequest(trusted.Context, intent.Digest)
	snapshot, execute, err := t.store.ReplayOrOpenTool(ctx, identity, requestDigest)
	if err != nil {
		return "", err
	}
	switch snapshot.Stage {
	case ToolReceiptCompleted, ToolReceiptFailed:
		return replayToolResult(snapshot, trusted)
	case ToolReceiptBusinessPrepared, ToolReceiptBusinessUnknown:
		return "", planstoryboard.ErrBusinessUnknownOutcome
	case ToolReceiptOpen:
		if !execute {
			return "", fmt.Errorf("%w: open Tool receipt is already owned by this fence", ErrReceiptConflict)
		}
	default:
		return "", ErrReceiptConflict
	}
	resultJSON, runErr := t.base.InvokableRun(ctx, arguments, options...)
	if runErr != nil {
		if errors.Is(runErr, planstoryboard.ErrBusinessUnknownOutcome) {
			if err := t.store.MarkToolBusinessUnknown(ctx, identity, requestDigest); err != nil {
				return "", err
			}
		}
		return "", runErr
	}
	result, canonical, err := decodeToolResult([]byte(resultJSON), trusted)
	if err != nil {
		return "", err
	}
	stage := ToolReceiptStage(result.Status)
	if err := t.store.FreezeToolResult(ctx, identity, requestDigest, stage, canonical, digestBytes(canonical)); err != nil {
		return "", err
	}
	snapshot, execute, err = t.store.ReplayOrOpenTool(ctx, identity, requestDigest)
	if err != nil {
		return "", err
	}
	if execute || snapshot.Stage == ToolReceiptOpen || snapshot.Stage == ToolReceiptBusinessPrepared || snapshot.Stage == ToolReceiptBusinessUnknown {
		return "", fmt.Errorf("%w: frozen Tool result is unavailable", ErrReceiptConflict)
	}
	return replayToolResult(snapshot, trusted)
}

func toolReceiptIdentity(value turncontext.PlanStoryboardRuntime) ToolReceiptIdentity {
	ctx := value.Context
	return ToolReceiptIdentity{
		Owner: value.Owner, FenceToken: value.FenceToken, SessionID: ctx.SessionID, InputID: ctx.InputID,
		TurnID: ctx.TurnID, RunID: ctx.RunID, ToolCallID: ctx.ToolCallID, BusinessCommandID: ctx.BusinessCommandID,
	}
}

func replayToolResult(snapshot ToolReceiptSnapshot, trusted turncontext.PlanStoryboardRuntime) (string, error) {
	if snapshot.Stage != ToolReceiptCompleted && snapshot.Stage != ToolReceiptFailed {
		return "", ErrReceiptConflict
	}
	result, canonical, err := decodeToolResult(snapshot.ResultJSON, trusted)
	if err != nil || ToolReceiptStage(result.Status) != snapshot.Stage || digestBytes(canonical) != snapshot.ResultDigest {
		return "", ErrReceiptConflict
	}
	return string(canonical), nil
}

func decodeToolResult(encoded []byte, trusted turncontext.PlanStoryboardRuntime) (planstoryboard.Result, []byte, error) {
	var result planstoryboard.Result
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return planstoryboard.Result{}, nil, fmt.Errorf("%w: decode Tool result", ErrOutputContract)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return planstoryboard.Result{}, nil, fmt.Errorf("%w: trailing Tool result", ErrOutputContract)
	}
	core := CoreContextFromRuntime(trusted)
	if result.SchemaVersion != planstoryboard.ResultSchemaVersion ||
		result.InvocationRef.ToolCallID != core.ToolCallID || result.InvocationRef.BusinessCommandID != core.BusinessCommandID {
		return planstoryboard.Result{}, nil, fmt.Errorf("%w: Tool result identity", ErrOutputContract)
	}
	switch result.Status {
	case "completed":
		if result.ResultCode != planstoryboard.ResultCodeCompleted || result.ResourceRef == nil || result.Summary != "" || result.Retryable != nil ||
			!canonicalUUIDv7.MatchString(result.ResourceRef.StoryboardPreviewID) || result.ResourceRef.Version < 1 ||
			result.ResourceRef.Status != "draft" || !canonicalSHA256.MatchString(result.ResourceRef.Digest) || result.ResourceRef.CreationSpecRef != core.CreationSpecRef {
			return planstoryboard.Result{}, nil, fmt.Errorf("%w: completed Tool result", ErrOutputContract)
		}
	case "failed":
		if result.ResultCode == "" || result.ResourceRef != nil || result.Summary == "" || result.Retryable == nil {
			return planstoryboard.Result{}, nil, fmt.Errorf("%w: failed Tool result", ErrOutputContract)
		}
	default:
		return planstoryboard.Result{}, nil, fmt.Errorf("%w: Tool result status", ErrOutputContract)
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return planstoryboard.Result{}, nil, fmt.Errorf("%w: non-canonical Tool result", ErrOutputContract)
	}
	return result, canonical, nil
}

func digestToolRequest(ctx turncontext.PlanStoryboardTurnContext, intentDigest string) string {
	wire := ctx.ContextDigest + "\n" + ctx.ToolDefinitionRef + "\n" + ctx.ToolDefinitionDigest + "\n" +
		ctx.IntentSchemaRef + "\n" + ctx.CandidateSchemaRef + "\n" + ctx.ResultSchemaRef + "\n" + intentDigest + "\n" +
		ctx.CreationSpecID + "\n" + fmt.Sprint(ctx.CreationSpecVersion) + "\n" + ctx.CreationSpecContentDigest
	return digestText(wire)
}

func digestPreparedCommand(command planstoryboard.DraftCommand) (string, error) {
	wire := struct {
		SchemaVersion          string                         `json:"schema_version"`
		RequestID              string                         `json:"request_id"`
		BusinessCommandID      string                         `json:"business_command_id"`
		RequestDigest          string                         `json:"request_digest"`
		UserID                 string                         `json:"user_id"`
		ProjectID              string                         `json:"project_id"`
		ExpectedProjectVersion int64                          `json:"expected_project_version"`
		CreationSpecRef        planstoryboard.CreationSpecRef `json:"creation_spec_ref"`
		ToolCallID             string                         `json:"tool_call_id"`
		PromptVersion          string                         `json:"prompt_version"`
		ValidatorVersion       string                         `json:"validator_version"`
		DAGValidatorVersion    string                         `json:"dag_validator_version"`
		Content                planstoryboard.Content         `json:"content"`
	}{
		SchemaVersion: "storyboard.preview.prepared-command.v1", RequestID: command.TrustedContext.RequestID,
		BusinessCommandID: command.TrustedContext.BusinessCommandID, RequestDigest: command.RequestDigest,
		UserID: command.TrustedContext.UserID, ProjectID: command.TrustedContext.ProjectID,
		ExpectedProjectVersion: command.DomainContext.ProjectVersion, CreationSpecRef: command.TrustedContext.CreationSpecRef,
		ToolCallID: command.TrustedContext.ToolCallID, PromptVersion: command.TrustedContext.PromptVersion,
		ValidatorVersion: command.TrustedContext.ValidatorVersion, DAGValidatorVersion: command.TrustedContext.DAGValidatorVersion,
		Content: command.Content,
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("digest plan storyboard prepared command: %w", err)
	}
	return digestBytes(encoded), nil
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

var _ planstoryboard.CommandJournal = (*CommandJournal)(nil)
var _ einotool.InvokableTool = (*ReceiptTool)(nil)
