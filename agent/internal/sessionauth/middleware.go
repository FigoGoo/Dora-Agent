// Package sessionauth 在 Kitex Transport 边界校验 Business→Agent Session RPC 服务身份。
package sessionauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
	"github.com/bytedance/gopkg/cloud/metainfo"
	"github.com/cloudwego/kitex/pkg/endpoint"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/google/uuid"
)

const (
	// SchemaVersion 冻结 HMAC Canonical Message 的字段顺序和算法。
	SchemaVersion = "session_rpc_service_auth.v1"
	// CallerServiceName 是唯一获准调用 Agent Session RPC 的 Business Runtime 身份。
	CallerServiceName = "dora-business-service"

	// CallerMetadataKey、IssuedAtMetadataKey 和 SignatureMetadataKey 是 TTHeader Metainfo 瞬时字段。
	CallerMetadataKey    = "dora-session-auth-caller"
	IssuedAtMetadataKey  = "dora-session-auth-issued-at-unix-ms"
	SignatureMetadataKey = "dora-session-auth-signature"

	ensureMethod = "EnsureProjectSessionV1"
	queryMethod  = "QueryProjectSessionCommandV1"
)

// Authenticator 保存启动时冻结的服务认证 Secret、时间窗口和可测试时钟。
// secret 的副本只用于 HMAC，不进入错误、日志、响应或领域 DTO。
type Authenticator struct {
	secret       []byte
	maxClockSkew time.Duration
	now          func() time.Time
}

// New 创建失败关闭的服务认证器。
func New(secret []byte, maxClockSkew time.Duration) (*Authenticator, error) {
	return newWithClock(secret, maxClockSkew, time.Now)
}

func newWithClock(secret []byte, maxClockSkew time.Duration, now func() time.Time) (*Authenticator, error) {
	if len(secret) != 32 {
		return nil, fmt.Errorf("create Session RPC authenticator: secret must contain exactly 32 bytes")
	}
	if maxClockSkew <= 0 || maxClockSkew > 5*time.Minute {
		return nil, fmt.Errorf("create Session RPC authenticator: invalid clock skew")
	}
	if now == nil {
		return nil, fmt.Errorf("create Session RPC authenticator: clock is required")
	}
	return &Authenticator{
		secret: append([]byte(nil), secret...), maxClockSkew: maxClockSkew, now: now,
	}, nil
}

// Middleware 在生成 Handler 之前校验 TTHeader 来源身份、瞬时凭据、时间窗口和请求绑定签名。
// 失败时直接写入 IDL declared exception；所有认证失败使用相同安全响应，避免形成校验 Oracle。
func (authenticator *Authenticator) Middleware() endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, req, resp interface{}) error {
			binding, requestID, ok := transportBinding(ctx, req)
			if !ok || !authenticator.authenticate(ctx, binding) {
				if setUnauthenticated(resp, safeRequestID(requestID)) {
					return nil
				}
				return fmt.Errorf("Session RPC authentication failed")
			}
			return next(ctx, req, resp)
		}
	}
}

type binding struct {
	method         string
	requestID      string
	commandID      string
	semanticDigest string
}

func (authenticator *Authenticator) authenticate(ctx context.Context, binding binding) bool {
	caller, callerOK := metainfo.GetValue(ctx, CallerMetadataKey)
	issuedRaw, issuedOK := metainfo.GetValue(ctx, IssuedAtMetadataKey)
	signatureRaw, signatureOK := metainfo.GetValue(ctx, SignatureMetadataKey)
	if !callerOK || !issuedOK || !signatureOK || caller != CallerServiceName {
		return false
	}

	rpc := rpcinfo.GetRPCInfo(ctx)
	if rpc == nil || rpc.Invocation() == nil || rpc.From() == nil || rpc.To() == nil ||
		rpc.Invocation().MethodName() != binding.method || rpc.From().ServiceName() != CallerServiceName ||
		rpc.To().ServiceName() != sessionv1.AGENT_SESSION_SERVICE_NAME {
		return false
	}

	issuedAtUnixMS, err := strconv.ParseInt(issuedRaw, 10, 64)
	if err != nil || issuedAtUnixMS <= 0 || issuedRaw != strconv.FormatInt(issuedAtUnixMS, 10) {
		return false
	}
	issuedAt := time.UnixMilli(issuedAtUnixMS)
	now := authenticator.now()
	if issuedAt.Before(now.Add(-authenticator.maxClockSkew)) || issuedAt.After(now.Add(authenticator.maxClockSkew)) {
		return false
	}

	providedSignature, err := hex.DecodeString(signatureRaw)
	if err != nil || len(providedSignature) != sha256.Size || signatureRaw != strings.ToLower(signatureRaw) {
		return false
	}
	expectedSignature := sign(authenticator.secret, binding, issuedAtUnixMS)
	return hmac.Equal(providedSignature, expectedSignature)
}

// transportBinding 同时核对 Kitex 实际方法名与生成 Args 类型，禁止跨方法复用签名。
func transportBinding(ctx context.Context, req interface{}) (binding, string, bool) {
	rpc := rpcinfo.GetRPCInfo(ctx)
	if rpc == nil || rpc.Invocation() == nil {
		return binding{}, "", false
	}
	switch args := req.(type) {
	case *sessionv1.AgentSessionServiceV1EnsureProjectSessionV1Args:
		if args == nil || args.Request == nil || rpc.Invocation().MethodName() != ensureMethod {
			return binding{}, "", false
		}
		request := args.Request
		return binding{
			method: ensureMethod, requestID: request.RequestId, commandID: request.CommandId,
			semanticDigest: request.RequestDigest,
		}, request.RequestId, true
	case *sessionv1.AgentSessionServiceV1QueryProjectSessionCommandV1Args:
		if args == nil || args.Request == nil || rpc.Invocation().MethodName() != queryMethod {
			return binding{}, "", false
		}
		request := args.Request
		return binding{
			method: queryMethod, requestID: request.RequestId, commandID: request.CommandId,
			semanticDigest: request.ExpectedRequestDigest,
		}, request.RequestId, true
	default:
		return binding{}, "", false
	}
}

// sign 按冻结顺序构造 UTF-8 Canonical Message：八行、行间单个 LF、末尾无换行。
func sign(secret []byte, binding binding, issuedAtUnixMS int64) []byte {
	canonical := strings.Join([]string{
		SchemaVersion,
		CallerServiceName,
		sessionv1.AGENT_SESSION_SERVICE_NAME,
		binding.method,
		binding.requestID,
		binding.commandID,
		binding.semanticDigest,
		strconv.FormatInt(issuedAtUnixMS, 10),
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	return mac.Sum(nil)
}

func setUnauthenticated(resp interface{}, requestID string) bool {
	serviceError := &sessionv1.SessionServiceExceptionV1{
		Code: "UNAUTHENTICATED", Message: "service authentication failed", Retryable: false, RequestId: requestID,
	}
	switch result := resp.(type) {
	case *sessionv1.AgentSessionServiceV1EnsureProjectSessionV1Result:
		result.ServiceError = serviceError
		return true
	case *sessionv1.AgentSessionServiceV1QueryProjectSessionCommandV1Result:
		result.ServiceError = serviceError
		return true
	default:
		return false
	}
}

func safeRequestID(value string) string {
	id, err := uuid.Parse(value)
	if err != nil || id.Version() != 7 {
		return ""
	}
	return strings.ToLower(id.String())
}
