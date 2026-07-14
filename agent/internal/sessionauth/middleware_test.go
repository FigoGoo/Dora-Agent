package sessionauth

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
	"github.com/bytedance/gopkg/cloud/metainfo"
	"github.com/cloudwego/kitex/pkg/endpoint"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
)

var (
	testSecret = []byte("0123456789abcdef0123456789abcdef")
	testNow    = time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
)

const (
	testRequestID = "0190f4d4-0000-7000-8000-000000000004"
	testCommandID = "0190f4d4-0000-7000-8000-000000000001"
	testDigest    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// TestMiddlewareAuthenticatesBoundEnsure 验证合法服务身份和请求绑定签名可以进入生成 Handler。
func TestMiddlewareAuthenticatesBoundEnsure(t *testing.T) {
	authenticator := newTestAuthenticator(t)
	authBinding := binding{method: ensureMethod, requestID: testRequestID, commandID: testCommandID, semanticDigest: testDigest}
	ctx := signedTestContext(authBinding, testSecret, testNow)
	args := ensureArgs(testDigest)
	result := sessionv1.NewAgentSessionServiceV1EnsureProjectSessionV1Result()
	called := false
	next := endpoint.Endpoint(func(_ context.Context, _, _ interface{}) error {
		called = true
		return nil
	})

	if err := authenticator.Middleware()(next)(ctx, args, result); err != nil {
		t.Fatalf("合法认证失败: %v", err)
	}
	if !called || result.ServiceError != nil {
		t.Fatalf("合法认证未进入 Handler: called=%v error=%+v", called, result.ServiceError)
	}
}

// TestMiddlewareFailsClosedWithDeclaredException 覆盖缺失、错签、过期、来源错误和跨方法复用。
func TestMiddlewareFailsClosedWithDeclaredException(t *testing.T) {
	authenticator := newTestAuthenticator(t)
	authBinding := binding{method: ensureMethod, requestID: testRequestID, commandID: testCommandID, semanticDigest: testDigest}
	tests := []struct {
		name      string
		ctx       context.Context
		requestID string
	}{
		{name: "missing metadata", ctx: rpcTestContext(ensureMethod, CallerServiceName), requestID: testRequestID},
		{name: "wrong signature", ctx: signedTestContext(authBinding, []byte("abcdef0123456789abcdef0123456789"), testNow), requestID: testRequestID},
		{name: "expired signature", ctx: signedTestContext(authBinding, testSecret, testNow.Add(-31*time.Second)), requestID: testRequestID},
		{name: "future signature", ctx: signedTestContext(authBinding, testSecret, testNow.Add(31*time.Second)), requestID: testRequestID},
		{name: "wrong caller endpoint", ctx: signedTestContextWithCaller(authBinding, testSecret, testNow, "forged-service"), requestID: testRequestID},
		{name: "cross method replay", ctx: signedTestContext(binding{method: queryMethod, requestID: testRequestID, commandID: testCommandID, semanticDigest: testDigest}, testSecret, testNow)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			result := sessionv1.NewAgentSessionServiceV1EnsureProjectSessionV1Result()
			next := endpoint.Endpoint(func(_ context.Context, _, _ interface{}) error {
				called = true
				return errors.New("must not run")
			})
			if err := authenticator.Middleware()(next)(test.ctx, ensureArgs(testDigest), result); err != nil {
				t.Fatalf("认证失败未编码为 declared exception: %v", err)
			}
			if called || result.ServiceError == nil || result.ServiceError.Code != "UNAUTHENTICATED" ||
				result.ServiceError.Retryable || result.ServiceError.RequestId != test.requestID {
				t.Fatalf("失败关闭响应错误: called=%v result=%+v", called, result)
			}
		})
	}
}

// TestMiddlewareRejectsPayloadMutationWithReplayedAssertion 验证截获的原签名不能修改语义摘要后重放。
func TestMiddlewareRejectsPayloadMutationWithReplayedAssertion(t *testing.T) {
	authenticator := newTestAuthenticator(t)
	binding := binding{method: ensureMethod, requestID: testRequestID, commandID: testCommandID, semanticDigest: testDigest}
	ctx := signedTestContext(binding, testSecret, testNow)
	mutatedDigest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	result := sessionv1.NewAgentSessionServiceV1EnsureProjectSessionV1Result()
	called := false
	next := endpoint.Endpoint(func(_ context.Context, _, _ interface{}) error { called = true; return nil })

	if err := authenticator.Middleware()(next)(ctx, ensureArgs(mutatedDigest), result); err != nil {
		t.Fatalf("重放变体未编码为 declared exception: %v", err)
	}
	if called || result.ServiceError == nil || result.ServiceError.Code != "UNAUTHENTICATED" {
		t.Fatalf("原签名可用于修改载荷: called=%v result=%+v", called, result)
	}
}

// TestMiddlewareAuthenticatesBoundQuery 验证 Query 使用 expected_request_digest 参与同一冻结协议。
func TestMiddlewareAuthenticatesBoundQuery(t *testing.T) {
	authenticator := newTestAuthenticator(t)
	binding := binding{method: queryMethod, requestID: testRequestID, commandID: testCommandID, semanticDigest: testDigest}
	ctx := signedTestContext(binding, testSecret, testNow)
	args := &sessionv1.AgentSessionServiceV1QueryProjectSessionCommandV1Args{Request: &sessionv1.QueryProjectSessionCommandRequestV1{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION,
		RequestId:     testRequestID, CommandId: testCommandID, ExpectedRequestDigest: testDigest,
	}}
	result := sessionv1.NewAgentSessionServiceV1QueryProjectSessionCommandV1Result()
	called := false
	next := endpoint.Endpoint(func(_ context.Context, _, _ interface{}) error { called = true; return nil })

	if err := authenticator.Middleware()(next)(ctx, args, result); err != nil || !called || result.ServiceError != nil {
		t.Fatalf("Query 合法认证失败: err=%v called=%v result=%+v", err, called, result)
	}
}

func newTestAuthenticator(t *testing.T) *Authenticator {
	t.Helper()
	authenticator, err := newWithClock(testSecret, 30*time.Second, func() time.Time { return testNow })
	if err != nil {
		t.Fatalf("创建测试认证器失败: %v", err)
	}
	return authenticator
}

func ensureArgs(digest string) *sessionv1.AgentSessionServiceV1EnsureProjectSessionV1Args {
	return &sessionv1.AgentSessionServiceV1EnsureProjectSessionV1Args{Request: &sessionv1.EnsureProjectSessionRequestV1{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION,
		RequestId:     testRequestID, CommandId: testCommandID, RequestDigest: digest,
	}}
}

func signedTestContext(binding binding, secret []byte, issuedAt time.Time) context.Context {
	return signedTestContextWithCaller(binding, secret, issuedAt, CallerServiceName)
}

func signedTestContextWithCaller(binding binding, secret []byte, issuedAt time.Time, endpointCaller string) context.Context {
	ctx := rpcTestContext(binding.method, endpointCaller)
	ctx = metainfo.WithValue(ctx, CallerMetadataKey, CallerServiceName)
	ctx = metainfo.WithValue(ctx, IssuedAtMetadataKey, timeToUnixMilli(issuedAt))
	ctx = metainfo.WithValue(ctx, SignatureMetadataKey, hex.EncodeToString(sign(secret, binding, issuedAt.UnixMilli())))
	return ctx
}

func rpcTestContext(method string, caller string) context.Context {
	from := rpcinfo.NewEndpointInfo(caller, method, nil, nil)
	to := rpcinfo.NewEndpointInfo(sessionv1.AGENT_SESSION_SERVICE_NAME, method, nil, nil)
	invocation := rpcinfo.NewInvocation(sessionv1.AGENT_SESSION_SERVICE_NAME, method)
	rpc := rpcinfo.NewRPCInfo(from, to, invocation, rpcinfo.NewRPCConfig(), nil)
	return rpcinfo.NewCtxWithRPCInfo(context.Background(), rpc)
}

func timeToUnixMilli(value time.Time) string {
	return strconv.FormatInt(value.UnixMilli(), 10)
}
