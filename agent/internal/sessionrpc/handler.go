// Package sessionrpc 实现 Business→Agent Session RPC v1 的严格协议边界。
package sessionrpc

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
	"github.com/google/uuid"
)

const (
	errorCodeInvalidArgument         = "INVALID_ARGUMENT"
	errorCodeIdempotencyConflict     = "IDEMPOTENCY_CONFLICT"
	errorCodeProjectSessionConflict  = "PROJECT_SESSION_CONFLICT"
	errorCodeContentProtectionFailed = "CONTENT_PROTECTION_FAILED"
	errorCodePersistenceUnavailable  = "PERSISTENCE_UNAVAILABLE"
	errorCodeDeadlineExceeded        = "DEADLINE_EXCEEDED"
	errorCodeRequestCanceled         = "REQUEST_CANCELED"
	errorCodeInternal                = "INTERNAL"
)

// Service 是 RPC Handler 消费的最小 Session 用例接口，生成类型不会越过该边界。
type Service interface {
	// EnsureProjectSession 幂等建立 Session 基础事实并返回冻结 Receipt。
	EnsureProjectSession(ctx context.Context, command session.EnsureCommand) (session.EnsureResult, error)
	// QueryProjectSessionCommand 只读核对原命令的 not_found/completed/conflict 状态。
	QueryProjectSessionCommand(ctx context.Context, command session.QueryCommand) (session.QueryCommandResult, error)
}

// Handler 严格映射 Session Thrift DTO，独立复核 Prompt Canonical Digest，并把领域错误收敛为安全业务异常。
// Handler 不记录 Prompt、Digest、密钥、Envelope 或完整 RPC Payload，也不执行框架自动重试。
type Handler struct {
	service Service
}

// NewHandler 创建 Session RPC Handler；Session 用例缺失时阻止 Transport 启动。
func NewHandler(service Service) (*Handler, error) {
	if service == nil {
		return nil, fmt.Errorf("Session RPC Handler 依赖缺失")
	}
	return &Handler{service: service}, nil
}

// EnsureProjectSessionV1 显式映射、独立重算摘要并调用幂等 Session 用例。
// 写操作不在 Handler 或 Kitex 层自动重试；Unknown Outcome 必须由 Business 先调用 Query 方法核对。
func (h *Handler) EnsureProjectSessionV1(ctx context.Context, request *sessionv1.EnsureProjectSessionRequestV1) (*sessionv1.EnsureProjectSessionResponseV1, error) {
	requestID := safeRequestIDFromEnsure(request)
	command, err := mapEnsureRequest(request)
	if err != nil {
		return nil, newServiceError(errorCodeInvalidArgument, "Session 创建请求不符合 v1 契约", false, requestID)
	}
	result, err := h.service.EnsureProjectSession(ctx, command)
	if err != nil {
		return nil, mapServiceError(err, requestID)
	}
	response, err := mapEnsureResponse(command.RequestID, command.CommandID, result)
	if err != nil {
		return nil, newServiceError(errorCodeInternal, "Session 服务内部错误", false, requestID)
	}
	return response, nil
}

// QueryProjectSessionCommandV1 映射 Unknown Outcome 查询并返回严格三态结果。
// Query 是纯只读核对，不会隐式重试 Ensure，也不会在 conflict 时泄漏既有 Receipt。
func (h *Handler) QueryProjectSessionCommandV1(ctx context.Context, request *sessionv1.QueryProjectSessionCommandRequestV1) (*sessionv1.QueryProjectSessionCommandResponseV1, error) {
	requestID := safeRequestIDFromQuery(request)
	command, err := mapQueryRequest(request)
	if err != nil {
		return nil, newServiceError(errorCodeInvalidArgument, "Session 命令查询不符合 v1 契约", false, requestID)
	}
	result, err := h.service.QueryProjectSessionCommand(ctx, command)
	if err != nil {
		return nil, mapServiceError(err, requestID)
	}
	response, err := mapQueryResponse(command.RequestID, command.CommandID, result)
	if err != nil {
		return nil, newServiceError(errorCodeInternal, "Session 服务内部错误", false, requestID)
	}
	return response, nil
}

// mapEnsureRequest 把生成 DTO 显式转换为领域命令，并在 Transport 层独立重算 Prompt 与请求摘要。
// 调用方传入的 prompt_digest/request_digest 只用于比对，永远不会作为 Agent 权威摘要直接落库。
func mapEnsureRequest(request *sessionv1.EnsureProjectSessionRequestV1) (session.EnsureCommand, error) {
	if request == nil || request.SchemaVersion != sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION {
		return session.EnsureCommand{}, fmt.Errorf("unsupported Ensure schema")
	}
	if !isUUIDv7(request.RequestId) || !isUUIDv7(request.CommandId) {
		return session.EnsureCommand{}, fmt.Errorf("request_id and command_id must be UUIDv7")
	}
	if request.CreationSource != sessionv1.CreationSourceV1_QUICK_CREATE ||
		request.SkillSnapshotMode != sessionv1.SkillSnapshotModeV1_EMPTY {
		return session.EnsureCommand{}, fmt.Errorf("unsupported Ensure enum")
	}
	if request.RequestedAtUnixMs <= 0 {
		return session.EnsureCommand{}, fmt.Errorf("requested_at_unix_ms is required")
	}
	initialPrompt := request.GetInitialPrompt()
	requestDigest, promptDigest, promptPresent, err := session.CalculateRequestDigest(
		request.ProjectId, request.OwnerUserId, initialPrompt, session.SkillSnapshotKindEmpty,
	)
	if err != nil {
		return session.EnsureCommand{}, err
	}
	if !equalDigest(request.RequestDigest, requestDigest) {
		return session.EnsureCommand{}, fmt.Errorf("request_digest mismatch")
	}
	if promptPresent {
		if !equalDigest(request.PromptDigest, promptDigest) {
			return session.EnsureCommand{}, fmt.Errorf("prompt_digest mismatch")
		}
	} else if request.PromptDigest != "" {
		return session.EnsureCommand{}, fmt.Errorf("blank Prompt must use empty digest")
	}
	return session.EnsureCommand{
		SchemaVersion: request.SchemaVersion, RequestID: request.RequestId, CommandID: request.CommandId,
		RequestDigest: requestDigest, ProjectID: request.ProjectId, OwnerUserID: request.OwnerUserId,
		CreationSource: session.CreationSourceQuickCreate, InitialPrompt: initialPrompt, PromptDigest: promptDigest,
		SkillSnapshotMode: session.SkillSnapshotKindEmpty,
		RequestedAt:       time.UnixMilli(request.RequestedAtUnixMs).UTC(),
	}, nil
}

// mapQueryRequest 显式转换 Query DTO，并在调用领域层前拒绝未知版本、非 UUIDv7 和非法摘要编码。
func mapQueryRequest(request *sessionv1.QueryProjectSessionCommandRequestV1) (session.QueryCommand, error) {
	if request == nil || request.SchemaVersion != sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION {
		return session.QueryCommand{}, fmt.Errorf("unsupported Query schema")
	}
	if !isUUIDv7(request.RequestId) || !isUUIDv7(request.CommandId) {
		return session.QueryCommand{}, fmt.Errorf("request_id and command_id must be UUIDv7")
	}
	if !isLowerSHA256(request.ExpectedRequestDigest) {
		return session.QueryCommand{}, fmt.Errorf("expected_request_digest must be lowercase SHA-256")
	}
	return session.QueryCommand{
		SchemaVersion: request.SchemaVersion, RequestID: request.RequestId,
		CommandID: request.CommandId, ExpectedRequestDigest: request.ExpectedRequestDigest,
	}, nil
}

// mapEnsureResponse 把领域 Receipt 显式映射为严格枚举响应，未知领域状态失败关闭。
func mapEnsureResponse(requestID, expectedCommandID string, result session.EnsureResult) (*sessionv1.EnsureProjectSessionResponseV1, error) {
	var disposition sessionv1.EnsureDispositionV1
	switch result.Disposition {
	case session.EnsureDispositionCreated:
		disposition = sessionv1.EnsureDispositionV1_CREATED
	case session.EnsureDispositionReplayed:
		disposition = sessionv1.EnsureDispositionV1_REPLAYED
	default:
		return nil, fmt.Errorf("unknown Ensure disposition")
	}
	receipt, err := mapReceipt(expectedCommandID, result)
	if err != nil {
		return nil, err
	}
	return &sessionv1.EnsureProjectSessionResponseV1{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION,
		RequestId:     requestID,
		Disposition:   disposition,
		Receipt:       receipt,
	}, nil
}

// mapQueryResponse 把领域 Query 三态映射为 Thrift 枚举，并强制只有 completed 携带 Receipt。
func mapQueryResponse(requestID, expectedCommandID string, result session.QueryCommandResult) (*sessionv1.QueryProjectSessionCommandResponseV1, error) {
	response := &sessionv1.QueryProjectSessionCommandResponseV1{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION,
		RequestId:     requestID,
	}
	switch result.Status {
	case session.QueryCommandStatusNotFound:
		if result.Receipt != nil {
			return nil, fmt.Errorf("not_found must not contain Receipt")
		}
		response.Status = sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND
	case session.QueryCommandStatusConflict:
		if result.Receipt != nil {
			return nil, fmt.Errorf("conflict must not contain Receipt")
		}
		response.Status = sessionv1.QueryProjectSessionCommandStatusV1_CONFLICT
	case session.QueryCommandStatusCompleted:
		if result.Receipt == nil {
			return nil, fmt.Errorf("completed requires Receipt")
		}
		response.Status = sessionv1.QueryProjectSessionCommandStatusV1_COMPLETED
		receipt, err := mapReceipt(expectedCommandID, *result.Receipt)
		if err != nil {
			return nil, err
		}
		response.Receipt = receipt
	default:
		return nil, fmt.Errorf("unknown Query status")
	}
	return response, nil
}

// mapReceipt 先验证冻结结果的标识、版本、可选字段配对与时间，再复制安全字段。
// Response 失败关闭，禁止领域实现缺陷把空标识或畸形 Receipt 变成已成功的跨 Module 事实。
func mapReceipt(expectedCommandID string, result session.EnsureResult) (*sessionv1.ProjectSessionReceiptV1, error) {
	if result.CommandID != expectedCommandID || !isUUIDv7(result.CommandID) || !isUUIDv7(result.SessionID) ||
		result.ResultVersion != session.ResultVersionV1 || result.AcceptedAt.IsZero() || result.AcceptedAt.UnixMilli() <= 0 ||
		(result.MessageID == nil) != (result.InputID == nil) {
		return nil, fmt.Errorf("invalid Session Receipt")
	}
	if result.MessageID != nil && (!isUUIDv7(*result.MessageID) || !isUUIDv7(*result.InputID)) {
		return nil, fmt.Errorf("invalid Session Receipt optional IDs")
	}
	return &sessionv1.ProjectSessionReceiptV1{
		CommandId: result.CommandID, SessionId: result.SessionID,
		MessageId: cloneOptionalString(result.MessageID), InputId: cloneOptionalString(result.InputID),
		ResultVersion: int32(result.ResultVersion), CompletedAtUnixMs: result.AcceptedAt.UTC().UnixMilli(),
	}, nil
}

// cloneOptionalString 复制可选标识，避免领域结果与生成 DTO 共享可变地址。
func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

// mapServiceError 把领域、Context 与未知错误收敛为不泄漏 SQL、Prompt、密钥或内部地址的稳定异常。
func mapServiceError(err error, requestID string) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return newServiceError(errorCodeDeadlineExceeded, "Session 请求处理超时", true, requestID)
	case errors.Is(err, context.Canceled):
		return newServiceError(errorCodeRequestCanceled, "Session 请求已取消", false, requestID)
	case errors.Is(err, session.ErrInvalidCommand):
		return newServiceError(errorCodeInvalidArgument, "Session 请求不符合 v1 契约", false, requestID)
	case errors.Is(err, session.ErrCommandConflict):
		return newServiceError(errorCodeIdempotencyConflict, "Session 命令幂等语义冲突", false, requestID)
	case errors.Is(err, session.ErrProjectSessionConflict):
		return newServiceError(errorCodeProjectSessionConflict, "Project 已绑定其他 Session 命令", false, requestID)
	case errors.Is(err, session.ErrContentProtection):
		return newServiceError(errorCodeContentProtectionFailed, "Session 内容保护暂时不可用", true, requestID)
	case errors.Is(err, session.ErrPersistence):
		return newServiceError(errorCodePersistenceUnavailable, "Session 持久化暂时不可用", true, requestID)
	default:
		return newServiceError(errorCodeInternal, "Session 服务内部错误", false, requestID)
	}
}

// newServiceError 构造固定字段业务异常；message 仅使用代码内安全文案，禁止拼接底层错误。
func newServiceError(code, message string, retryable bool, requestID string) error {
	return &sessionv1.SessionServiceExceptionV1{
		Code: code, Message: message, Retryable: retryable, RequestId: requestID,
	}
}

// safeRequestIDFromEnsure 只在 ID 符合 UUIDv7 时回显，非法输入不进入错误关联字段。
func safeRequestIDFromEnsure(request *sessionv1.EnsureProjectSessionRequestV1) string {
	if request != nil && isUUIDv7(request.RequestId) {
		return request.RequestId
	}
	return ""
}

// safeRequestIDFromQuery 只在 ID 符合 UUIDv7 时回显，非法输入不进入错误关联字段。
func safeRequestIDFromQuery(request *sessionv1.QueryProjectSessionCommandRequestV1) string {
	if request != nil && isUUIDv7(request.RequestId) {
		return request.RequestId
	}
	return ""
}

// isUUIDv7 验证应用侧标识使用 UUIDv7。
func isUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7
}

// isLowerSHA256 校验固定长度小写十六进制摘要。
func isLowerSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

// equalDigest 对固定长度摘要执行常量时间比较；格式错误自然比较失败。
func equalDigest(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

var _ sessionv1.AgentSessionServiceV1 = (*Handler)(nil)
