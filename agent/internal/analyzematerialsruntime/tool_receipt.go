package analyzematerialsruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ToolReceiptStage 是素材分析 Tool Result 的 exact-set 状态。
type ToolReceiptStage string

const (
	ToolReceiptOpen      ToolReceiptStage = "open"
	ToolReceiptCompleted ToolReceiptStage = "completed"
	ToolReceiptPartial   ToolReceiptStage = "partial"
	ToolReceiptFailed    ToolReceiptStage = "failed"
)

// ToolReceiptIdentity 把 Tool 回执绑定到稳定 ToolCall 与当前 owner/fence。
type ToolReceiptIdentity struct {
	Owner      string
	FenceToken int64
	SessionID  string
	InputID    string
	TurnID     string
	RunID      string
	ToolCallID string
}

// ToolReceiptSnapshot 是 Processor 与 Tool Wrapper 共用的 PostgreSQL 真源读取形状。
type ToolReceiptSnapshot struct {
	Stage         ToolReceiptStage
	RequestDigest string
	ResultJSON    []byte
	ResultDigest  string
}

// ToolReceiptStore 由 PostgreSQL Adapter 实现 open 占位、首写冻结和 stale Fence 拒绝。
type ToolReceiptStore interface {
	ReplayOrOpenTool(context.Context, ToolReceiptIdentity, string) (ToolReceiptSnapshot, bool, error)
	FreezeToolResult(context.Context, ToolReceiptIdentity, string, ToolReceiptStage, []byte, string) error
}

// ReceiptTool 在现有 Tool Core 外实现 first-write-wins 回执，不改变 Graph Node 集合。
type ReceiptTool struct {
	base  einotool.InvokableTool
	store ToolReceiptStore
	info  *schema.ToolInfo
}

// NewReceiptTool 只接受 exact analyze_materials Tool，拒绝任何额外或别名能力。
func NewReceiptTool(ctx context.Context, base einotool.InvokableTool, store ToolReceiptStore) (*ReceiptTool, error) {
	if base == nil || store == nil {
		return nil, fmt.Errorf("create analyze materials receipt tool: tool and store are required")
	}
	info, err := base.Info(ctx)
	if err != nil || info == nil || info.Name != analyzematerials.ToolKey {
		return nil, fmt.Errorf("create analyze materials receipt tool: exact analyze_materials tool is required")
	}
	return &ReceiptTool{base: base, store: store, info: info}, nil
}

// Info 返回已验证的唯一 Tool Schema。
func (t *ReceiptTool) Info(context.Context) (*schema.ToolInfo, error) {
	if t == nil || t.info == nil {
		return nil, fmt.Errorf("read analyze materials receipt tool info: tool is not initialized")
	}
	return t.info, nil
}

// InvokableRun 先重放 frozen Result；open 时执行一次 Core，再冻结并重读首写结果。
func (t *ReceiptTool) InvokableRun(ctx context.Context, arguments string, options ...einotool.Option) (string, error) {
	trusted, ok := turncontext.MaterialAnalysisRuntimeFrom(ctx)
	if !ok || trusted.Context.Profile != Profile || trusted.Owner == "" || trusted.FenceToken < 1 {
		return "", fmt.Errorf("run analyze materials receipt tool: trusted runtime context is invalid")
	}
	intent, err := ValidateCanonicalIntent([]byte(arguments), trusted.Context.IntentDigest)
	if err != nil || arguments != trusted.IntentJSON {
		return "", fmt.Errorf("run analyze materials receipt tool: router arguments changed frozen intent")
	}
	identity := ToolReceiptIdentity{Owner: trusted.Owner, FenceToken: trusted.FenceToken, SessionID: trusted.Context.SessionID, InputID: trusted.Context.InputID, TurnID: trusted.Context.TurnID, RunID: trusted.Context.RunID, ToolCallID: trusted.Context.ToolCallID}
	requestDigest := digestToolRequest(trusted.Context, intent.Digest)
	snapshot, execute, err := t.store.ReplayOrOpenTool(ctx, identity, requestDigest)
	if err != nil {
		return "", err
	}
	if snapshot.Stage != ToolReceiptOpen {
		return replayToolResult(snapshot, trusted)
	}
	if !execute {
		return "", fmt.Errorf("%w: open Tool receipt is already owned by this fence", ErrReceiptConflict)
	}
	resultJSON, runErr := t.base.InvokableRun(ctx, arguments, options...)
	if runErr != nil {
		return "", runErr
	}
	result, canonical, err := decodeToolResult([]byte(resultJSON), trusted)
	if err != nil {
		return "", err
	}
	stage := ToolReceiptStage(result.Status)
	resultDigest := digestBytes(canonical)
	if err := t.store.FreezeToolResult(ctx, identity, requestDigest, stage, canonical, resultDigest); err != nil {
		return "", err
	}
	snapshot, execute, err = t.store.ReplayOrOpenTool(ctx, identity, requestDigest)
	if err != nil {
		return "", err
	}
	if execute || snapshot.Stage == ToolReceiptOpen {
		return "", fmt.Errorf("%w: frozen Tool result is unavailable", ErrReceiptConflict)
	}
	return replayToolResult(snapshot, trusted)
}

func replayToolResult(snapshot ToolReceiptSnapshot, trusted turncontext.MaterialAnalysisRuntime) (string, error) {
	if snapshot.Stage != ToolReceiptCompleted && snapshot.Stage != ToolReceiptPartial && snapshot.Stage != ToolReceiptFailed {
		return "", ErrReceiptConflict
	}
	result, canonical, err := decodeToolResult(snapshot.ResultJSON, trusted)
	if err != nil || ToolReceiptStage(result.Status) != snapshot.Stage || digestBytes(canonical) != snapshot.ResultDigest {
		return "", ErrReceiptConflict
	}
	return string(canonical), nil
}

func decodeToolResult(encoded []byte, trusted turncontext.MaterialAnalysisRuntime) (analyzematerials.Result, []byte, error) {
	var result analyzematerials.Result
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return analyzematerials.Result{}, nil, fmt.Errorf("%w: decode Tool result", ErrOutputContract)
	}
	if decoder.More() {
		return analyzematerials.Result{}, nil, ErrOutputContract
	}
	core := analyzematerials.TrustedContext{Owner: trusted.Owner, UserID: trusted.Context.UserID, ProjectID: trusted.Context.ProjectID, SessionID: trusted.Context.SessionID, InputID: trusted.Context.InputID, TurnID: trusted.Context.TurnID, RunID: trusted.Context.RunID, ToolCallID: trusted.Context.ToolCallID, FenceToken: trusted.FenceToken, PromptVersion: analyzematerials.PromptVersion, ValidatorVersion: analyzematerials.ValidatorVersion, EvidencePolicyVersion: analyzematerials.EvidencePolicyVersion}
	if err := analyzematerials.ValidateResultForContext(result, core); err != nil {
		return analyzematerials.Result{}, nil, fmt.Errorf("%w: invalid Tool result", ErrOutputContract)
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return analyzematerials.Result{}, nil, fmt.Errorf("%w: non-canonical Tool result", ErrOutputContract)
	}
	return result, canonical, nil
}

func digestToolRequest(ctx turncontext.MaterialAnalysisTurnContext, intentDigest string) string {
	wire := ctx.ContextDigest + "\n" + ctx.ToolDefinitionRef + "\n" + ctx.ToolDefinitionDigest + "\n" + ctx.IntentSchemaRef + "\n" + ctx.ResultSchemaRef + "\n" + intentDigest
	return digestText(wire)
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

var _ einotool.InvokableTool = (*ReceiptTool)(nil)
