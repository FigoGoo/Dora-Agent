// Package usermessageruntime 实现 user_message.runtime.v2preview1 的隔离 B3 运行时核心。
// 本包不拥有 Migration、PostgreSQL、Workspace 或 HTTP 适配器，也不注册任何 Tool。
package usermessageruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/google/uuid"
)

const (
	// Profile 是方案 A 唯一允许的 Development Preview Profile。
	Profile = "user_message.runtime.v2preview1"
	// DirectResponseCardSchemaVersion 是成功 Output Receipt 的唯一 Schema。
	DirectResponseCardSchemaVersion = "session.turn.direct_response.card.v1"
	// FailureCardSchemaVersion 是失败 Output Receipt 的唯一 Schema。
	FailureCardSchemaVersion = "session.turn.failure.card.v1"
	// DirectResponseMessageCode 是首轮安全响应的稳定展示代码。
	DirectResponseMessageCode = "creation_request_received"
	// DirectResponseSummary 是方案 A 冻结的固定服务端中文文案。
	DirectResponseSummary = "已收到你的创作需求。你可以继续打开工具箱选择下一步流程。"
	// DirectResponseActionOpenToolbox 是方案 A 唯一允许的无副作用动作。
	DirectResponseActionOpenToolbox = "open_toolbox"
	// EmptyToolRegistryRef 是 Context 中唯一允许的空可执行 Tool Registry 引用。
	EmptyToolRegistryRef = "user_message.empty_tools@v1"
	// LocalFakeModelRouteRef 是 B3 唯一允许的本地确定性模型路由。
	LocalFakeModelRouteRef = "local.fake.user_message@v1"

	// MaxMessagePlaintextBytes 复用 QuickCreate 首消息 64KiB 硬上限，禁止 Runner 截断。
	MaxMessagePlaintextBytes = 64 * 1024
	// MaxModelOutputBytes 限制单个纯 Assistant JSON Card；超限完整失败，不做截断或部分解析。
	MaxModelOutputBytes = 4 * 1024

	FailureCodeInvalidInput     = "USER_MESSAGE_RUNTIME_INPUT_INVALID"
	FailureCodeProcessingFailed = "USER_MESSAGE_RUNTIME_PROCESSING_FAILED"
	FailureSummaryInvalidInput  = "创作需求输入无法安全处理，请重新提交。"
	FailureSummaryProcessing    = "创作需求处理失败，请重新提交。"
)

var (
	// ErrInvalidClaim 表示 Claim 身份、冻结 Context 或消息明文不一致。
	ErrInvalidClaim = errors.New("invalid user message runtime claim")
	// ErrOutputContract 表示模型/Output Receipt 违反方案 A exact-set。
	ErrOutputContract = errors.New("invalid user message runtime output contract")
	// ErrPersistence 表示持久化端口不可用；底层 SQL 不得穿透本边界。
	ErrPersistence = errors.New("user message runtime persistence unavailable")
	// ErrFenceLost 表示当前 owner 已失去 Session Lane Fence，后续写入必须为零。
	ErrFenceLost = errors.New("user message runtime session fence lost")
	// ErrModelReceiptReserved 表示模型回执仍为 reserved，但当前调用没有执行授权。
	ErrModelReceiptReserved = errors.New("user message model receipt remains reserved")
	// ErrModelFailed 表示本地模型回执已经稳定冻结为 failed。
	ErrModelFailed = errors.New("user message model receipt failed")
)

// Claim 是统一 Lane Source Dispatcher 交给 B3 Handler 的完整可信执行事实。
// MessagePlaintext 只在内存中存在，不得被写入日志、Event、Failure 或 Assistant History。
type Claim struct {
	Profile          string
	Owner            string
	RunID            string
	ModelCallID      string
	OutputID         string
	RecoveryEventID  string
	TerminalEventID  string
	FenceToken       int64
	Attempts         int
	EnqueueSeq       int64
	Context          turncontext.UserMessageTurnContext
	MessagePlaintext string
	Poisoned         bool
}

// DirectResponseCard 是方案 A 唯一成功输出；字段集合与顺序由 CanonicalJSON 固定。
type DirectResponseCard struct {
	SchemaVersion    string   `json:"schema_version"`
	TurnID           string   `json:"turn_id"`
	RunID            string   `json:"run_id"`
	InputID          string   `json:"input_id"`
	Status           string   `json:"status"`
	MessageCode      string   `json:"message_code"`
	Summary          string   `json:"summary"`
	AvailableActions []string `json:"available_actions"`
}

// FailureCard 是确定输入/执行失败的稳定脱敏输出，不包含底层错误或 Prompt。
type FailureCard struct {
	SchemaVersion string `json:"schema_version"`
	TurnID        string `json:"turn_id"`
	RunID         string `json:"run_id"`
	InputID       string `json:"input_id"`
	Status        string `json:"status"`
	ErrorCode     string `json:"error_code"`
	Retryable     bool   `json:"retryable"`
	Summary       string `json:"summary"`
}

// Output 是 Direct Response 与 Failure Card 的内存联合；恰好一个分支必须存在。
type Output struct {
	DirectResponse *DirectResponseCard
	Failure        *FailureCard
}

// OutputReceiptStage 是 Processor 可见的 Output Receipt exact-set。
type OutputReceiptStage string

const (
	OutputReceiptOpen      OutputReceiptStage = "open"
	OutputReceiptCompleted OutputReceiptStage = "completed"
	OutputReceiptFailed    OutputReceiptStage = "failed"
)

// OutputReceiptSnapshot 把 open 与两类冻结终态显式区分，避免 nil 被解释为成功。
type OutputReceiptSnapshot struct {
	Stage  OutputReceiptStage
	Output *Output
}

// ValidateClaim 在任何 Receipt/Runner 调用前校验稳定 ID、Context pins、摘要和消息上限。
func ValidateClaim(claim Claim) error {
	if claim.Profile != Profile || claim.Owner == "" || claim.FenceToken < 1 || claim.Attempts < 1 || claim.EnqueueSeq < 1 ||
		claim.Context.SchemaVersion != turncontext.UserMessageTurnContextSchemaVersion ||
		claim.Context.MessageCutoffSeq != 1 ||
		claim.Context.PromptRef != PromptRef || claim.Context.PromptDigest != PromptDigest ||
		claim.Context.ToolRegistryRef != EmptyToolRegistryRef || claim.Context.ToolRegistryDigest != EmptyToolRegistryDigest ||
		claim.Context.RuntimePolicyRef != RuntimePolicyRef || claim.Context.RuntimePolicyDigest != RuntimePolicyDigest ||
		claim.Context.ModelRouteRef != LocalFakeModelRouteRef || claim.Context.ModelRouteDigest != LocalFakeModelRouteDigest ||
		claim.Context.BudgetRef != BudgetRef || claim.Context.BudgetDigest != BudgetDigest ||
		claim.Context.SkillSnapshotRef != "session_skill_snapshot:"+claim.Context.SessionID ||
		claim.MessagePlaintext == "" ||
		len(claim.MessagePlaintext) > MaxMessagePlaintextBytes || !utf8.ValidString(claim.MessagePlaintext) {
		return ErrInvalidClaim
	}
	ids := []string{
		claim.RunID, claim.ModelCallID, claim.OutputID, claim.RecoveryEventID, claim.TerminalEventID,
		claim.Context.TurnID, claim.Context.SessionID, claim.Context.InputID, claim.Context.MessageID,
		claim.Context.UserID, claim.Context.ProjectID,
	}
	for _, value := range ids {
		if !isCanonicalUUIDv7(value) {
			return ErrInvalidClaim
		}
	}
	accessScopeID := strings.TrimPrefix(claim.Context.AccessScopeRef, "ensure_command:")
	if accessScopeID == claim.Context.AccessScopeRef || !isCanonicalUUIDv7(accessScopeID) {
		return ErrInvalidClaim
	}
	refs := []string{
		claim.Context.SkillSnapshotRef, claim.Context.PromptRef, claim.Context.ToolRegistryRef,
		claim.Context.RuntimePolicyRef, claim.Context.ModelRouteRef, claim.Context.BudgetRef,
		claim.Context.AccessScopeRef,
	}
	for _, value := range refs {
		if strings.TrimSpace(value) == "" {
			return ErrInvalidClaim
		}
	}
	digests := []string{
		claim.Context.MessageContentDigest, claim.Context.SkillSnapshotDigest, claim.Context.PromptDigest,
		claim.Context.ToolRegistryDigest, claim.Context.RuntimePolicyDigest, claim.Context.ModelRouteDigest,
		claim.Context.BudgetDigest, claim.Context.AccessScopeDigest, claim.Context.ContextDigest,
	}
	for _, value := range digests {
		if !isSHA256Hex(value) {
			return ErrInvalidClaim
		}
	}
	messageDigest := sha256.Sum256([]byte(claim.MessagePlaintext))
	if hex.EncodeToString(messageDigest[:]) != claim.Context.MessageContentDigest {
		return ErrInvalidClaim
	}
	contextDigest, err := session.DigestUserMessageContext(session.UserMessageContext{
		TurnID: claim.Context.TurnID, SchemaVersion: claim.Context.SchemaVersion,
		SessionID: claim.Context.SessionID, InputID: claim.Context.InputID, MessageID: claim.Context.MessageID,
		UserID: claim.Context.UserID, ProjectID: claim.Context.ProjectID,
		MessageCutoffSeq: claim.Context.MessageCutoffSeq, MessageContentDigest: claim.Context.MessageContentDigest,
		SkillSnapshotRef: claim.Context.SkillSnapshotRef, SkillSnapshotDigest: claim.Context.SkillSnapshotDigest,
		PromptRef: claim.Context.PromptRef, PromptDigest: claim.Context.PromptDigest,
		ToolRegistryRef: claim.Context.ToolRegistryRef, ToolRegistryDigest: claim.Context.ToolRegistryDigest,
		RuntimePolicyRef: claim.Context.RuntimePolicyRef, RuntimePolicyDigest: claim.Context.RuntimePolicyDigest,
		ModelRouteRef: claim.Context.ModelRouteRef, ModelRouteDigest: claim.Context.ModelRouteDigest,
		BudgetRef: claim.Context.BudgetRef, BudgetDigest: claim.Context.BudgetDigest,
		AccessScopeRef: claim.Context.AccessScopeRef, AccessScopeDigest: claim.Context.AccessScopeDigest,
	})
	if err != nil || contextDigest != claim.Context.ContextDigest {
		return ErrInvalidClaim
	}
	return nil
}

// NewDirectResponse 返回与 Claim 稳定身份绑定的固定安全 Card。
func NewDirectResponse(claim Claim) DirectResponseCard {
	return DirectResponseCard{
		SchemaVersion: DirectResponseCardSchemaVersion,
		TurnID:        claim.Context.TurnID, RunID: claim.RunID, InputID: claim.Context.InputID,
		Status: "completed", MessageCode: DirectResponseMessageCode, Summary: DirectResponseSummary,
		AvailableActions: []string{DirectResponseActionOpenToolbox},
	}
}

// NewFailure 返回稳定脱敏 Failure Card；poison 与执行耗尽使用不同 error_code。
func NewFailure(claim Claim, invalidInput bool) FailureCard {
	code, summary := FailureCodeProcessingFailed, FailureSummaryProcessing
	if invalidInput {
		code, summary = FailureCodeInvalidInput, FailureSummaryInvalidInput
	}
	return FailureCard{
		SchemaVersion: FailureCardSchemaVersion,
		TurnID:        claim.Context.TurnID, RunID: claim.RunID, InputID: claim.Context.InputID,
		Status: "failed", ErrorCode: code, Retryable: false, Summary: summary,
	}
}

// DecodeDirectResponseCard 严格拒绝未知/重复/缺失字段、尾随 JSON 和超限正文。
func DecodeDirectResponseCard(content string) (DirectResponseCard, error) {
	if content == "" || len(content) > MaxModelOutputBytes || !utf8.ValidString(content) {
		return DirectResponseCard{}, ErrOutputContract
	}
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return DirectResponseCard{}, ErrOutputContract
	}
	seen := make(map[string]struct{}, 8)
	var card DirectResponseCard
	for decoder.More() {
		token, tokenErr := decoder.Token()
		key, ok := token.(string)
		if tokenErr != nil || !ok {
			return DirectResponseCard{}, ErrOutputContract
		}
		if _, duplicate := seen[key]; duplicate {
			return DirectResponseCard{}, ErrOutputContract
		}
		seen[key] = struct{}{}
		switch key {
		case "schema_version":
			err = decoder.Decode(&card.SchemaVersion)
		case "turn_id":
			err = decoder.Decode(&card.TurnID)
		case "run_id":
			err = decoder.Decode(&card.RunID)
		case "input_id":
			err = decoder.Decode(&card.InputID)
		case "status":
			err = decoder.Decode(&card.Status)
		case "message_code":
			err = decoder.Decode(&card.MessageCode)
		case "summary":
			err = decoder.Decode(&card.Summary)
		case "available_actions":
			err = decoder.Decode(&card.AvailableActions)
		default:
			return DirectResponseCard{}, ErrOutputContract
		}
		if err != nil {
			return DirectResponseCard{}, ErrOutputContract
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != 8 {
		return DirectResponseCard{}, ErrOutputContract
	}
	if token, trailingErr := decoder.Token(); trailingErr != io.EOF || token != nil {
		return DirectResponseCard{}, ErrOutputContract
	}
	return card, nil
}

// ValidateDirectResponse 要求 Card 常量与当前 Claim 稳定身份逐值相等。
func ValidateDirectResponse(card DirectResponseCard, claim Claim) error {
	if card.SchemaVersion != DirectResponseCardSchemaVersion || card.TurnID != claim.Context.TurnID ||
		card.RunID != claim.RunID || card.InputID != claim.Context.InputID || card.Status != "completed" ||
		card.MessageCode != DirectResponseMessageCode || card.Summary != DirectResponseSummary ||
		len(card.AvailableActions) != 1 || card.AvailableActions[0] != DirectResponseActionOpenToolbox {
		return ErrOutputContract
	}
	return nil
}

// ValidateOutput 校验 Output union、Receipt stage 与当前 Claim 的稳定身份。
func ValidateOutput(output Output, claim Claim) error {
	if (output.DirectResponse == nil) == (output.Failure == nil) {
		return ErrOutputContract
	}
	if output.DirectResponse != nil {
		return ValidateDirectResponse(*output.DirectResponse, claim)
	}
	failure := output.Failure
	if failure.SchemaVersion != FailureCardSchemaVersion || failure.TurnID != claim.Context.TurnID ||
		failure.RunID != claim.RunID || failure.InputID != claim.Context.InputID || failure.Status != "failed" ||
		failure.ErrorCode == "" || failure.Summary == "" || failure.Retryable {
		return ErrOutputContract
	}
	return nil
}

// ValidateOutputReceipt 拒绝 stage/payload 不相容或未知状态。
func ValidateOutputReceipt(snapshot OutputReceiptSnapshot, claim Claim) error {
	switch snapshot.Stage {
	case OutputReceiptOpen:
		if snapshot.Output != nil {
			return ErrOutputContract
		}
		return nil
	case OutputReceiptCompleted:
		if snapshot.Output == nil || snapshot.Output.DirectResponse == nil || snapshot.Output.Failure != nil {
			return ErrOutputContract
		}
	case OutputReceiptFailed:
		if snapshot.Output == nil || snapshot.Output.Failure == nil || snapshot.Output.DirectResponse != nil {
			return ErrOutputContract
		}
	default:
		return ErrOutputContract
	}
	return ValidateOutput(*snapshot.Output, claim)
}

// CanonicalJSON 返回 Output Receipt 加密前的稳定 exact-set JSON。
func (output Output) CanonicalJSON() ([]byte, error) {
	if (output.DirectResponse == nil) == (output.Failure == nil) {
		return nil, ErrOutputContract
	}
	var value any = output.DirectResponse
	if output.Failure != nil {
		value = output.Failure
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode user message output: %w", err)
	}
	return encoded, nil
}

func isCanonicalUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.String() == value
}

func isCanonicalUUIDv4(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 4 && id.String() == value
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
