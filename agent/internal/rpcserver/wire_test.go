package rpcserver

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/sessionauth"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1/agentsessionservicev1"
	"github.com/bytedance/gopkg/cloud/metainfo"
	kitexclient "github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/pkg/transmeta"
	"github.com/cloudwego/kitex/transport"
)

const (
	wireRequestID = "0190f4d4-0000-7000-8000-000000000004"
	wireCommandID = "0190f4d4-0000-7000-8000-000000000001"
	wireDigest    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

var wireSecret = []byte("0123456789abcdef0123456789abcdef")

// wireHandler 是线级测试最小生成 Handler，用调用次数证明认证中间件在业务代码之前失败关闭。
type wireHandler struct {
	ensureCalls   atomic.Int32
	queryCalls    atomic.Int32
	ensureV2Calls atomic.Int32
	queryV2Calls  atomic.Int32
}

func (handler *wireHandler) EnsureProjectSessionV2(
	_ context.Context,
	request *sessionv1.EnsureProjectSessionRequestV2,
) (*sessionv1.EnsureProjectSessionResponseV2, error) {
	handler.ensureV2Calls.Add(1)
	return &sessionv1.EnsureProjectSessionResponseV2{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2,
		RequestId:     request.RequestId,
		Disposition:   sessionv1.EnsureDispositionV1_CREATED,
		Receipt: &sessionv1.ProjectSessionReceiptV2{
			CommandId: request.CommandId, SessionId: "0190f4d4-0001-7000-8000-000000000002",
			ResultVersion: 2, CompletedAtUnixMs: time.Now().UnixMilli(),
			SkillSnapshotDigest: request.SkillSnapshot.SnapshotSetDigest,
			SkillCount:          request.SkillSnapshot.SkillCount,
		},
	}, nil
}

func (handler *wireHandler) EnsureProjectSessionV1(
	_ context.Context,
	request *sessionv1.EnsureProjectSessionRequestV1,
) (*sessionv1.EnsureProjectSessionResponseV1, error) {
	handler.ensureCalls.Add(1)
	return &sessionv1.EnsureProjectSessionResponseV1{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION,
		RequestId:     request.RequestId,
		Disposition:   sessionv1.EnsureDispositionV1_CREATED,
		Receipt: &sessionv1.ProjectSessionReceiptV1{
			CommandId: request.CommandId, SessionId: "0190f4d4-0001-7000-8000-000000000001",
			ResultVersion: 1, CompletedAtUnixMs: time.Now().UnixMilli(),
		},
	}, nil
}

func (handler *wireHandler) QueryProjectSessionCommandV1(
	_ context.Context,
	request *sessionv1.QueryProjectSessionCommandRequestV1,
) (*sessionv1.QueryProjectSessionCommandResponseV1, error) {
	handler.queryCalls.Add(1)
	return &sessionv1.QueryProjectSessionCommandResponseV1{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION,
		RequestId:     request.RequestId, Status: sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND,
	}, nil
}

func (handler *wireHandler) QueryProjectSessionCommandV2(
	_ context.Context,
	request *sessionv1.QueryProjectSessionCommandRequestV2,
) (*sessionv1.QueryProjectSessionCommandResponseV2, error) {
	handler.queryV2Calls.Add(1)
	return &sessionv1.QueryProjectSessionCommandResponseV2{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2,
		RequestId:     request.RequestId, Status: sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND,
	}, nil
}

// TestAuthenticatedTTHeaderWire 验证真实 Listener 上的 TTHeader+Metainfo、HMAC 校验和 IDL declared exception。
func TestAuthenticatedTTHeaderWire(t *testing.T) {
	handler := &wireHandler{}
	server, err := New(
		config.RPCConfig{
			ListenAddress: "127.0.0.1:0", ReadWriteTimeout: time.Second, MaxConnectionIdleTime: time.Minute,
		},
		config.SessionRPCAuthConfig{SharedSecret: wireSecret, MaxClockSkew: 30 * time.Second},
		time.Second,
		handler,
	)
	if err != nil {
		t.Fatalf("创建线级测试 Server 失败: %v", err)
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve() }()
	t.Cleanup(func() {
		if stopErr := server.Stop(); stopErr != nil {
			t.Errorf("停止线级测试 Server 失败: %v", stopErr)
		}
		select {
		case serveErr := <-serveErrors:
			if serveErr != nil {
				t.Errorf("线级测试 Server 退出错误: %v", serveErr)
			}
		case <-time.After(2 * time.Second):
			t.Error("等待线级测试 Server 退出超时")
		}
	})

	client, err := agentsessionservicev1.NewClient(
		sessionv1.AGENT_SESSION_SERVICE_NAME,
		kitexclient.WithHostPorts(server.Address().String()),
		kitexclient.WithClientBasicInfo(&rpcinfo.EndpointBasicInfo{ServiceName: sessionauth.CallerServiceName}),
		kitexclient.WithTransportProtocol(transport.TTHeaderFramed),
		kitexclient.WithMetaHandler(transmeta.ClientTTHeaderHandler),
		kitexclient.WithMetaHandler(transmeta.MetainfoClientHandler),
		kitexclient.WithConnectTimeout(time.Second),
		kitexclient.WithRPCTimeout(time.Second),
	)
	if err != nil {
		t.Fatalf("创建线级测试 Client 失败: %v", err)
	}

	ensureRequest := &sessionv1.EnsureProjectSessionRequestV1{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION,
		RequestId:     wireRequestID, CommandId: wireCommandID, RequestDigest: wireDigest,
		ProjectId: "0190f4d4-0000-7000-8000-000000000002", OwnerUserId: "0190f4d4-0000-7000-8000-000000000003",
		CreationSource: sessionv1.CreationSourceV1_QUICK_CREATE, PromptDigest: "",
		SkillSnapshotMode: sessionv1.SkillSnapshotModeV1_EMPTY, RequestedAtUnixMs: time.Now().UnixMilli(),
	}
	requestCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.EnsureProjectSessionV1(requestCtx, ensureRequest); err == nil {
		t.Fatal("缺少认证的真实 RPC 未失败关闭")
	} else {
		var serviceError *sessionv1.SessionServiceExceptionV1
		if !errors.As(err, &serviceError) || serviceError.Code != "UNAUTHENTICATED" || serviceError.Retryable {
			t.Fatalf("认证错误未按 declared exception 序列化: %T %v", err, err)
		}
	}
	if handler.ensureCalls.Load() != 0 {
		t.Fatalf("未认证请求进入 Handler: calls=%d", handler.ensureCalls.Load())
	}
	wrongCredentialCtx := withWireAssertionUsingSecret(
		requestCtx, "EnsureProjectSessionV1", wireRequestID, wireCommandID, wireDigest, time.Now(),
		[]byte("abcdef0123456789abcdef0123456789"),
	)
	if _, err := client.EnsureProjectSessionV1(wrongCredentialCtx, ensureRequest); err == nil {
		t.Fatal("错签名的真实 RPC 未失败关闭")
	} else {
		var serviceError *sessionv1.SessionServiceExceptionV1
		if !errors.As(err, &serviceError) || serviceError.Code != "UNAUTHENTICATED" {
			t.Fatalf("错签名未返回统一 declared exception: %T %v", err, err)
		}
	}
	staleCredentialCtx := withWireAssertion(
		requestCtx, "EnsureProjectSessionV1", wireRequestID, wireCommandID, wireDigest, time.Now().Add(-31*time.Second),
	)
	if _, err := client.EnsureProjectSessionV1(staleCredentialCtx, ensureRequest); err == nil {
		t.Fatal("过期签名的真实 RPC 未失败关闭")
	} else {
		var serviceError *sessionv1.SessionServiceExceptionV1
		if !errors.As(err, &serviceError) || serviceError.Code != "UNAUTHENTICATED" {
			t.Fatalf("过期签名未返回统一 declared exception: %T %v", err, err)
		}
	}
	if handler.ensureCalls.Load() != 0 {
		t.Fatalf("错签名或过期请求进入 Handler: calls=%d", handler.ensureCalls.Load())
	}

	signedEnsureCtx := withWireAssertion(requestCtx, "EnsureProjectSessionV1", wireRequestID, wireCommandID, wireDigest, time.Now())
	response, err := client.EnsureProjectSessionV1(signedEnsureCtx, ensureRequest)
	if err != nil || response == nil || response.Receipt == nil || response.Receipt.CommandId != wireCommandID {
		t.Fatalf("合法 Ensure 线级调用失败: response=%+v err=%v", response, err)
	}
	if handler.ensureCalls.Load() != 1 {
		t.Fatalf("合法 Ensure Handler 调用数=%d，want 1", handler.ensureCalls.Load())
	}

	queryRequestID := "0190f4d4-0000-7000-8000-000000000005"
	queryRequest := &sessionv1.QueryProjectSessionCommandRequestV1{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION,
		RequestId:     queryRequestID, CommandId: wireCommandID, ExpectedRequestDigest: wireDigest,
	}
	signedQueryCtx := withWireAssertion(requestCtx, "QueryProjectSessionCommandV1", queryRequestID, wireCommandID, wireDigest, time.Now())
	queryResponse, err := client.QueryProjectSessionCommandV1(signedQueryCtx, queryRequest)
	if err != nil || queryResponse == nil || queryResponse.Status != sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND {
		t.Fatalf("合法 Query 线级调用失败: response=%+v err=%v", queryResponse, err)
	}
	if handler.queryCalls.Load() != 1 {
		t.Fatalf("合法 Query Handler 调用数=%d，want 1", handler.queryCalls.Load())
	}

	emptySnapshotDigest := "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"
	ensureV2RequestID := "0190f4d4-0000-7000-8000-000000000006"
	ensureV2Request := &sessionv1.EnsureProjectSessionRequestV2{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2,
		RequestId:     ensureV2RequestID, CommandId: wireCommandID, RequestDigest: wireDigest,
		ProjectId: "0190f4d4-0000-7000-8000-000000000002", OwnerUserId: "0190f4d4-0000-7000-8000-000000000003",
		CreationSource: sessionv1.CreationSourceV1_QUICK_CREATE, PromptDigest: "",
		SkillSnapshot: &sessionv1.SessionSkillSnapshotV1{
			SchemaVersion: sessionv1.SESSION_SKILL_SNAPSHOT_SCHEMA_VERSION_V1,
			SnapshotKind:  sessionv1.SessionSkillSnapshotKindV1_EMPTY,
			SkillCount:    0, SnapshotSetDigest: emptySnapshotDigest, Skills: []*sessionv1.PublishedSkillSnapshotRefV1{},
		},
		RequestedAtUnixMs: time.Now().UnixMilli(),
	}
	signedEnsureV2Ctx := withWireAssertion(requestCtx, "EnsureProjectSessionV2", ensureV2RequestID, wireCommandID, wireDigest, time.Now())
	ensureV2Response, err := client.EnsureProjectSessionV2(signedEnsureV2Ctx, ensureV2Request)
	if err != nil || ensureV2Response == nil || ensureV2Response.Receipt == nil ||
		ensureV2Response.Receipt.SkillSnapshotDigest != emptySnapshotDigest {
		t.Fatalf("合法 Ensure v2 线级调用失败: response=%+v err=%v", ensureV2Response, err)
	}
	if handler.ensureV2Calls.Load() != 1 {
		t.Fatalf("合法 Ensure v2 Handler 调用数=%d，want 1", handler.ensureV2Calls.Load())
	}

	queryV2RequestID := "0190f4d4-0000-7000-8000-000000000007"
	queryV2Request := &sessionv1.QueryProjectSessionCommandRequestV2{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2,
		RequestId:     queryV2RequestID, CommandId: wireCommandID, ExpectedRequestDigest: wireDigest,
	}
	signedQueryV2Ctx := withWireAssertion(requestCtx, "QueryProjectSessionCommandV2", queryV2RequestID, wireCommandID, wireDigest, time.Now())
	queryV2Response, err := client.QueryProjectSessionCommandV2(signedQueryV2Ctx, queryV2Request)
	if err != nil || queryV2Response == nil || queryV2Response.Status != sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND {
		t.Fatalf("合法 Query v2 线级调用失败: response=%+v err=%v", queryV2Response, err)
	}
	if handler.queryV2Calls.Load() != 1 {
		t.Fatalf("合法 Query v2 Handler 调用数=%d，want 1", handler.queryV2Calls.Load())
	}
}

func withWireAssertion(
	ctx context.Context,
	method string,
	requestID string,
	commandID string,
	digest string,
	issuedAt time.Time,
) context.Context {
	return withWireAssertionUsingSecret(ctx, method, requestID, commandID, digest, issuedAt, wireSecret)
}

func withWireAssertionUsingSecret(
	ctx context.Context,
	method string,
	requestID string,
	commandID string,
	digest string,
	issuedAt time.Time,
	secret []byte,
) context.Context {
	issuedAtRaw := strconv.FormatInt(issuedAt.UnixMilli(), 10)
	canonical := strings.Join([]string{
		sessionauth.SchemaVersion,
		sessionauth.CallerServiceName,
		sessionv1.AGENT_SESSION_SERVICE_NAME,
		method,
		requestID,
		commandID,
		digest,
		issuedAtRaw,
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	ctx = metainfo.WithValue(ctx, sessionauth.CallerMetadataKey, sessionauth.CallerServiceName)
	ctx = metainfo.WithValue(ctx, sessionauth.IssuedAtMetadataKey, issuedAtRaw)
	return metainfo.WithValue(ctx, sessionauth.SignatureMetadataKey, hex.EncodeToString(mac.Sum(nil)))
}
