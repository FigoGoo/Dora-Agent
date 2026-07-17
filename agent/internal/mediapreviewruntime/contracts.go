// Package mediapreviewruntime 把两类媒体 typed ingress、统一 Session Lane 与确定性 Graph Tool 连接起来。
package mediapreviewruntime

import (
	"context"
	"errors"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
)

const (
	// EnqueueSchemaVersion 是两个媒体入口共用的 202 响应版本。
	EnqueueSchemaVersion = "media_preview.enqueue.v1"
	// GenerateSourceType 是 generate_media 请求的 Session Input 来源。
	GenerateSourceType = "generate_media_preview_request"
	// AssembleSourceType 是 assemble_output 请求的 Session Input 来源。
	AssembleSourceType = "assemble_output_preview_request"
	// TerminalSourceType 是 Worker 终态经 Bridge 追加的 Session Input 来源。
	TerminalSourceType = "media_job_preview_terminal"
	// PendingStatus 表示请求已可靠入队但异步任务尚未派发。
	PendingStatus = "pending"
	// DefaultRequestTTL 是本地预览首次入队冻结的最大执行窗口。
	DefaultRequestTTL = 10 * time.Minute
)

var (
	// ErrInvalidInput 表示 typed ingress、Claim 或终态不满足严格契约。
	ErrInvalidInput = errors.New("media preview runtime invalid input")
	// ErrNotFound 表示 Session 或绑定资源不存在。
	ErrNotFound = errors.New("media preview runtime not found")
	// ErrIdempotencyConflict 表示同 Session 幂等键发生异义重放。
	ErrIdempotencyConflict = errors.New("media preview runtime idempotency conflict")
	// ErrLaneBlocked 表示 Session 当前 HOL 属于其他互斥来源。
	ErrLaneBlocked = errors.New("media preview runtime session lane blocked")
	// ErrFenceLost 表示当前 owner/fence 已失效。
	ErrFenceLost = errors.New("media preview runtime fence lost")
	// ErrOutputContract 表示 Agent/Graph 或 Worker Terminal 输出违反 exact-set。
	ErrOutputContract = errors.New("media preview runtime output contract violation")
	// ErrPersistence 表示持久化结果不可确认。
	ErrPersistence = errors.New("media preview runtime persistence unavailable")
)

// EnqueueRequest 是 HTTP Handler 完成身份、外层 Ref 与 Intent 绑定后交给 Service 的 DTO。
type EnqueueRequest struct {
	RequestID      string
	SessionID      string
	UserID         string
	ProjectID      string
	IdempotencyKey string
	ToolKey        string
	IntentJSON     []byte
}

// EnqueueCommand 是 Repository 单事务 first-write-wins 命令。
type EnqueueCommand struct {
	EnqueueRequest
	IntentSchemaVersion string
	IntentDigest        string
	RequestDigest       string
	DeadlineAt          time.Time
}

// EnqueueResult 是 Repository 返回且同义重放保持不变的稳定身份集合。
type EnqueueResult struct {
	InputID    string
	TurnID     string
	RunID      string
	ToolCallID string
	ToolKey    string
	Replayed   bool
}

// EnqueueResponse 是 Business BFF 可安全透传的 202 DTO。
type EnqueueResponse struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
	InputID       string `json:"input_id"`
	TurnID        string `json:"turn_id"`
	RunID         string `json:"run_id"`
	ToolCallID    string `json:"tool_call_id"`
	ToolKey       string `json:"tool_key"`
	Replayed      bool   `json:"replayed"`
}

// Claim 是一次媒体请求 Graph 执行所需的全部可信身份与 canonical Intent。
type Claim struct {
	Owner           string
	RequestID       string
	IdempotencyKey  string
	UserID          string
	ProjectID       string
	SessionID       string
	InputID         string
	TurnID          string
	RunID           string
	ToolCallID      string
	AcceptedEventID string
	TerminalEventID string
	ToolKey         string
	IntentDigest    string
	IntentJSON      []byte
	FenceToken      int64
	Attempts        int
	DeadlineAt      time.Time
}

// TerminalAssetRef 是 Worker succeeded Result 的 ready Asset exact-set。
type TerminalAssetRef struct {
	AssetID       string `json:"asset_id"`
	Version       int64  `json:"version"`
	Status        string `json:"status"`
	MediaKind     string `json:"media_kind"`
	MIMEType      string `json:"mime_type"`
	ContentDigest string `json:"content_digest"`
	SizeBytes     int64  `json:"size_bytes"`
}

// TerminalResult 是 Worker 写入 Agent Outbox 的 succeeded/failed 严格联合。
type TerminalResult struct {
	SchemaVersion         string            `json:"schema_version"`
	Status                string            `json:"status"`
	AssetRef              *TerminalAssetRef `json:"asset_ref,omitempty"`
	FinalizationReceiptID string            `json:"finalization_receipt_id,omitempty"`
	ErrorCode             string            `json:"error_code,omitempty"`
}

// TerminalClaim 是 Terminal Processor 投影 Card 所需的 Outbox、原请求和 Bridge Input 事实。
type TerminalClaim struct {
	Owner           string
	BridgeInputID   string
	OriginalInputID string
	SessionID       string
	TurnID          string
	RunID           string
	ToolCallID      string
	ToolKey         string
	OperationID     string
	BatchID         string
	JobID           string
	// JobType 是 Agent Job 冻结的执行类型，用于把终态媒体类型绑定到原始 Job。
	JobType string
	AssetID string
	// AssetVersion 是 Agent Job target 冻结的 Business 资产版本。
	AssetVersion    int64
	TerminalEventID string
	TerminalStatus  string
	ResultDigest    string
	ResultJSON      []byte
	FenceToken      int64
	OccurredAt      time.Time
}

// Repository 是媒体 request、terminal bridge 与统一 Lane 的唯一持久真源端口。
type Repository interface {
	Enqueue(context.Context, EnqueueCommand) (EnqueueResult, error)
	ClaimNext(context.Context, string, string, time.Duration) (*Claim, error)
	MarkRunning(context.Context, Claim) error
	RenewLease(context.Context, Claim, time.Duration) error
	CompleteGraphResult(context.Context, Claim, mediapreview.GraphToolResult) error
	DeferInputRecovery(context.Context, Claim, time.Duration) error
	CompleteRuntimeFailure(context.Context, Claim, string) error
	BridgeNextTerminal(context.Context) (bool, error)
	ClaimNextTerminal(context.Context, string, time.Duration) (*TerminalClaim, error)
	CompleteTerminal(context.Context, TerminalClaim, TerminalResult) error
}

// IDGenerator 生成应用侧 UUIDv7。
type IDGenerator interface{ New() (string, error) }

// Clock 为入队 Deadline 和测试注入 UTC 时间。
type Clock interface{ Now() time.Time }
