package agentsessionrpc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/sessionv1"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/sessionv1/agentsessionservicev1"
	kitexclient "github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/client/callopt"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/pkg/transmeta"
	"github.com/cloudwego/kitex/transport"
	"github.com/google/uuid"
)

// ClientConfig 冻结 Agent Session RPC 的连接与单次请求超时；写操作不配置框架自动重试。
type ClientConfig struct {
	// ConnectTimeout 限制 Kitex 建立连接的时间。
	ConnectTimeout time.Duration
	// RequestTimeout 限制单次 Ensure 或 Query 的总时间。
	RequestTimeout time.Duration
	// AuthSecret 是逐请求 HMAC 服务间身份证明使用的 32 字节共享密钥。
	AuthSecret []byte
	// Environment 与互斥的本地 Preview 开关共同决定是否允许单机 loopback 注册地址。
	Environment                  string
	PlanStoryboardRuntimeEnabled bool
	// WritePromptsRuntimeEnabled 表示当前进程只在本地运行 write_prompts Development Preview，可接受精确回环注册地址。
	WritePromptsRuntimeEnabled bool
}

// protocolClient 是消费方定义的最小生成 Client 接口，便于隔离映射和错误测试。
type protocolClient interface {
	EnsureProjectSessionV1(ctx context.Context, request *sessionv1.EnsureProjectSessionRequestV1, callOptions ...callopt.Option) (*sessionv1.EnsureProjectSessionResponseV1, error)
	QueryProjectSessionCommandV1(ctx context.Context, request *sessionv1.QueryProjectSessionCommandRequestV1, callOptions ...callopt.Option) (*sessionv1.QueryProjectSessionCommandResponseV1, error)
}

// Client 显式映射 Project Dispatcher DTO 与 Agent-owned Thrift DTO，并管理 etcd Resolver 生命周期。
type Client struct {
	protocol protocolClient
	resolver *EtcdResolver
	config   ClientConfig
	idgen    project.IDGenerator
	auth     *requestAuthenticator
}

var _ project.AgentSessionClient = (*Client)(nil)

// NewClient 创建具有显式超时和 etcd 发现的 Agent Session Client；写操作不启用框架自动重试。
func NewClient(ctx context.Context, clientConfig ClientConfig, etcdConfig config.EtcdConfig) (*Client, error) {
	if clientConfig.ConnectTimeout <= 0 || clientConfig.RequestTimeout <= 0 || len(clientConfig.AuthSecret) != 32 {
		return nil, fmt.Errorf("create Agent Session RPC client: invalid timeout config")
	}
	authenticator, err := newRequestAuthenticator(clientConfig.AuthSecret, time.Now)
	if err != nil {
		return nil, err
	}
	allowLoopback := allowLoopbackRegistration(clientConfig)
	resolver, err := NewEtcdResolver(ctx, etcdConfig, allowLoopback)
	if err != nil {
		return nil, err
	}
	protocol, err := agentsessionservicev1.NewClient(
		sessionv1.AGENT_SESSION_SERVICE_NAME,
		kitexclient.WithResolver(resolver),
		kitexclient.WithClientBasicInfo(&rpcinfo.EndpointBasicInfo{ServiceName: serviceAuthCaller}),
		kitexclient.WithTransportProtocol(transport.TTHeaderFramed),
		kitexclient.WithMetaHandler(transmeta.ClientTTHeaderHandler),
		kitexclient.WithMetaHandler(transmeta.MetainfoClientHandler),
		kitexclient.WithConnectTimeout(clientConfig.ConnectTimeout),
		kitexclient.WithRPCTimeout(clientConfig.RequestTimeout),
	)
	if err != nil {
		_ = resolver.Close()
		return nil, fmt.Errorf("create Agent Session generated client: %w", err)
	}
	return &Client{
		protocol: protocol, resolver: resolver, config: clientConfig,
		idgen: idgen.UUIDv7{}, auth: authenticator,
	}, nil
}

// allowLoopbackRegistration 只在冻结的 local Preview Profile 下放宽同机发现；其他环境保持失败关闭。
func allowLoopbackRegistration(clientConfig ClientConfig) bool {
	return (clientConfig.PlanStoryboardRuntimeEnabled || clientConfig.WritePromptsRuntimeEnabled) && strings.EqualFold(clientConfig.Environment, "local")
}

// Ensure 调用无框架重试的写方法，并严格校验响应版本、关联 ID、Disposition 与 Receipt。
func (client *Client) Ensure(ctx context.Context, request project.EnsureSessionRequest) (project.EnsureSessionReceipt, error) {
	protocolRequest, err := mapEnsureRequest(request)
	if err != nil {
		return project.EnsureSessionReceipt{}, project.ErrAgentSessionInvalid
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.config.RequestTimeout)
	defer cancel()
	requestCtx = client.auth.withAuthentication(
		requestCtx,
		"EnsureProjectSessionV1",
		protocolRequest.RequestId,
		protocolRequest.CommandId,
		protocolRequest.RequestDigest,
	)
	response, err := client.protocol.EnsureProjectSessionV1(requestCtx, protocolRequest)
	if err != nil {
		return project.EnsureSessionReceipt{}, mapAgentServiceError(err)
	}
	return mapEnsureResponse(request, response)
}

// Query 使用新的关联 Request ID 核对原 command_id 与 expected digest，不产生写副作用。
func (client *Client) Query(ctx context.Context, commandID string, expectedDigest project.Digest) (project.QuerySessionResult, error) {
	requestID, err := client.idgen.New()
	if err != nil {
		return project.QuerySessionResult{}, project.ErrAgentSessionUnavailable
	}
	if !isUUIDv7(commandID) || expectedDigest == (project.Digest{}) || !isUUIDv7(requestID) {
		return project.QuerySessionResult{}, project.ErrAgentSessionInvalid
	}
	protocolRequest := &sessionv1.QueryProjectSessionCommandRequestV1{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION,
		RequestId:     requestID, CommandId: commandID, ExpectedRequestDigest: expectedDigest.Hex(),
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.config.RequestTimeout)
	defer cancel()
	requestCtx = client.auth.withAuthentication(
		requestCtx,
		"QueryProjectSessionCommandV1",
		protocolRequest.RequestId,
		protocolRequest.CommandId,
		protocolRequest.ExpectedRequestDigest,
	)
	response, err := client.protocol.QueryProjectSessionCommandV1(requestCtx, protocolRequest)
	if err != nil {
		return project.QuerySessionResult{}, mapAgentServiceError(err)
	}
	return mapQueryResponse(requestID, commandID, expectedDigest, response)
}

// Close 关闭 Client 自有的 etcd Resolver；测试注入 Client 可以没有 Resolver。
func (client *Client) Close() error {
	if client.resolver == nil {
		return nil
	}
	return client.resolver.Close()
}

// mapEnsureRequest 把应用 DTO 显式映射为 Agent-owned 生成类型，并拒绝 Prompt Presence/Digest 漂移。
func mapEnsureRequest(request project.EnsureSessionRequest) (*sessionv1.EnsureProjectSessionRequestV1, error) {
	if !isUUIDv7(request.RequestID) || !isUUIDv7(request.CommandID) || !isUUIDv7(request.ProjectID) || !isUUIDv7(request.OwnerUserID) ||
		request.RequestDigest == (project.Digest{}) || request.RequestedAt.IsZero() {
		return nil, project.ErrAgentSessionInvalid
	}
	promptDigest := ""
	var initialPrompt *string
	if request.PromptPresent {
		normalized, digest, present, err := project.NormalizeEnsureSessionPrompt(request.InitialPrompt)
		if err != nil || !present || normalized != request.InitialPrompt || digest != request.PromptDigest {
			return nil, project.ErrAgentSessionInvalid
		}
		prompt := request.InitialPrompt
		initialPrompt = &prompt
		promptDigest = request.PromptDigest.Hex()
	} else if request.InitialPrompt != "" || request.PromptDigest != (project.Digest{}) {
		return nil, project.ErrAgentSessionInvalid
	}
	expectedDigest, err := project.CalculateEnsureSessionRequestDigest(request.ProjectID, request.OwnerUserID, request.PromptPresent, request.PromptDigest)
	if err != nil || expectedDigest != request.RequestDigest {
		return nil, project.ErrAgentSessionInvalid
	}
	return &sessionv1.EnsureProjectSessionRequestV1{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION,
		RequestId:     request.RequestID, CommandId: request.CommandID, RequestDigest: request.RequestDigest.Hex(),
		ProjectId: request.ProjectID, OwnerUserId: request.OwnerUserID,
		CreationSource: sessionv1.CreationSourceV1_QUICK_CREATE, InitialPrompt: initialPrompt, PromptDigest: promptDigest,
		SkillSnapshotMode: sessionv1.SkillSnapshotModeV1_EMPTY, RequestedAtUnixMs: request.RequestedAt.UTC().UnixMilli(),
	}, nil
}

// mapEnsureResponse 校验响应只属于当前请求，并显式转换冻结 Receipt。
func mapEnsureResponse(request project.EnsureSessionRequest, response *sessionv1.EnsureProjectSessionResponseV1) (project.EnsureSessionReceipt, error) {
	if response == nil || response.SchemaVersion != sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION || response.RequestId != request.RequestID ||
		(response.Disposition != sessionv1.EnsureDispositionV1_CREATED && response.Disposition != sessionv1.EnsureDispositionV1_REPLAYED) {
		return project.EnsureSessionReceipt{}, project.ErrInvalidAgentReceipt
	}
	receipt, err := mapReceipt(request.CommandID, request.RequestDigest, response.Receipt, &request.PromptPresent)
	if err != nil {
		return project.EnsureSessionReceipt{}, err
	}
	receipt.Replayed = response.Disposition == sessionv1.EnsureDispositionV1_REPLAYED
	return receipt, nil
}

// mapQueryResponse 校验严格三态；not_found/conflict 禁止携带 Receipt，completed 必须携带安全 Receipt。
func mapQueryResponse(requestID, commandID string, expectedDigest project.Digest, response *sessionv1.QueryProjectSessionCommandResponseV1) (project.QuerySessionResult, error) {
	if response == nil || response.SchemaVersion != sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION || response.RequestId != requestID {
		return project.QuerySessionResult{}, project.ErrInvalidAgentReceipt
	}
	switch response.Status {
	case sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND:
		if response.Receipt != nil {
			return project.QuerySessionResult{}, project.ErrInvalidAgentReceipt
		}
		return project.QuerySessionResult{Status: project.QueryStatusNotFound}, nil
	case sessionv1.QueryProjectSessionCommandStatusV1_CONFLICT:
		if response.Receipt != nil {
			return project.QuerySessionResult{}, project.ErrInvalidAgentReceipt
		}
		return project.QuerySessionResult{Status: project.QueryStatusConflict}, nil
	case sessionv1.QueryProjectSessionCommandStatusV1_COMPLETED:
		receipt, err := mapReceipt(commandID, expectedDigest, response.Receipt, nil)
		if err != nil {
			return project.QuerySessionResult{}, err
		}
		return project.QuerySessionResult{Status: project.QueryStatusCompleted, Receipt: &receipt}, nil
	default:
		return project.QuerySessionResult{}, project.ErrInvalidAgentReceipt
	}
}

// mapReceipt 校验命令、UUIDv7、版本、时间和 Message/Input 配对，再映射为应用安全回执。
func mapReceipt(commandID string, requestDigest project.Digest, receipt *sessionv1.ProjectSessionReceiptV1, expectedPromptPresent *bool) (project.EnsureSessionReceipt, error) {
	if receipt == nil || receipt.CommandId != commandID || !isUUIDv7(receipt.CommandId) || !isUUIDv7(receipt.SessionId) ||
		receipt.ResultVersion != 1 || receipt.CompletedAtUnixMs <= 0 || (receipt.MessageId == nil) != (receipt.InputId == nil) {
		return project.EnsureSessionReceipt{}, project.ErrInvalidAgentReceipt
	}
	if receipt.MessageId != nil && (!isUUIDv7(*receipt.MessageId) || !isUUIDv7(*receipt.InputId)) {
		return project.EnsureSessionReceipt{}, project.ErrInvalidAgentReceipt
	}
	if expectedPromptPresent != nil && *expectedPromptPresent != (receipt.InputId != nil) {
		return project.EnsureSessionReceipt{}, project.ErrInvalidAgentReceipt
	}
	return project.EnsureSessionReceipt{
		CommandID: receipt.CommandId, RequestDigest: requestDigest, SessionID: receipt.SessionId,
		InputID: cloneOptionalString(receipt.InputId),
	}, nil
}

// mapAgentServiceError 保留 Context 取消/超时并把其他 Kitex/服务端错误收敛为稳定应用错误。
func mapAgentServiceError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var serviceErr *sessionv1.SessionServiceExceptionV1
	if errors.As(err, &serviceErr) {
		switch serviceErr.Code {
		case "IDEMPOTENCY_CONFLICT", "PROJECT_SESSION_CONFLICT":
			return project.ErrAgentSessionConflict
		case "COMMAND_CONFLICT", "COMMAND_VERSION_CONFLICT":
			return project.ErrAgentSessionConflict
		case "INVALID_ARGUMENT", "SNAPSHOT_DIGEST_MISMATCH", "SNAPSHOT_LIMIT_EXCEEDED":
			return project.ErrAgentSessionInvalid
		default:
			return project.ErrAgentSessionUnavailable
		}
	}
	// Kitex 原错可能含地址与传输细节，不能沿 Dispatcher/HTTP 错误链泄漏。
	return project.ErrAgentSessionUnavailable
}

// cloneOptionalString 复制生成 DTO 的可选 ID，避免共享可变地址。
func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

// isUUIDv7 校验应用侧稳定标识版本。
func isUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7
}
