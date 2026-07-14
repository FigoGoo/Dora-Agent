package agentsessionrpc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/gopkg/cloud/metainfo"
)

const (
	serviceAuthSchema        = "session_rpc_service_auth.v1"
	serviceAuthCaller        = "dora-business-service"
	serviceAuthTarget        = "dora.agent.session.v1"
	authCallerMetadataKey    = "dora-session-auth-caller"
	authIssuedAtMetadataKey  = "dora-session-auth-issued-at-unix-ms"
	authSignatureMetadataKey = "dora-session-auth-signature"
)

// requestAuthenticator 为每次 Agent Session RPC 构造与方法、请求、命令和语义摘要绑定的短时 HMAC 身份证明。
type requestAuthenticator struct {
	secret []byte
	clock  func() time.Time
}

// newRequestAuthenticator 复制启动密钥，避免调用方后续修改底层切片。
func newRequestAuthenticator(secret []byte, clock func() time.Time) (*requestAuthenticator, error) {
	if len(secret) != 32 || clock == nil {
		return nil, errors.New("create Agent Session RPC authenticator: invalid dependency")
	}
	return &requestAuthenticator{secret: append([]byte(nil), secret...), clock: clock}, nil
}

// withAuthentication 把三个 transient metadata 写入单次调用 Context；Secret 与 Canonical Message 均不进入 DTO 或日志。
func (authenticator *requestAuthenticator) withAuthentication(
	ctx context.Context,
	method string,
	requestID string,
	commandID string,
	semanticDigest string,
) context.Context {
	issuedAt := strconv.FormatInt(authenticator.clock().UTC().UnixMilli(), 10)
	canonical := strings.Join([]string{
		serviceAuthSchema,
		serviceAuthCaller,
		serviceAuthTarget,
		method,
		requestID,
		commandID,
		semanticDigest,
		issuedAt,
	}, "\n")
	mac := hmac.New(sha256.New, authenticator.secret)
	_, _ = mac.Write([]byte(canonical))
	signature := hex.EncodeToString(mac.Sum(nil))
	return metainfo.WithValues(
		ctx,
		authCallerMetadataKey, serviceAuthCaller,
		authIssuedAtMetadataKey, issuedAt,
		authSignatureMetadataKey, signature,
	)
}
