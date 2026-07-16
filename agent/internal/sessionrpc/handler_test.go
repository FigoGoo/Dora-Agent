package sessionrpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
)

const (
	testRequestID   = "019f0000-0000-7000-8000-000000000001"
	testCommandID   = "019f0000-0000-7000-8000-000000000002"
	testProjectID   = "019f0000-0000-7000-8000-0000000000ab"
	testOwnerID     = "019f0000-0000-7000-8000-0000000000cd"
	testSessionID   = "019f0000-0000-7000-8000-000000000003"
	testMessageID   = "019f0000-0000-7000-8000-000000000004"
	testInputID     = "019f0000-0000-7000-8000-000000000005"
	testPromptHash  = "273f7787225c057d3b40cecfdad67cefd35e4b0fa95eacff5668011fc44497df"
	testRequestHash = "35141e4689f43dc9778773f4cf20cd9a6633e22eed18cfde4059f6d5d9841fc4"
)

// fakeService 捕获 Mapper 输出并允许测试驱动所有领域状态与安全错误。
type fakeService struct {
	ensureCommand   session.EnsureCommand
	ensureResult    session.EnsureResult
	ensureErr       error
	ensureCalls     int
	ensureCommandV2 session.EnsureCommandV2
	ensureResultV2  session.EnsureResult
	ensureErrV2     error
	ensureCallsV2   int
	queryCommand    session.QueryCommand
	queryResult     session.QueryCommandResult
	queryErr        error
	queryCalls      int
}

// EnsureProjectSessionV2 为共享 Handler 测试替身补齐 V2 用例边界；V1 测试断言它不会被调用。
func (f *fakeService) EnsureProjectSessionV2(_ context.Context, command session.EnsureCommandV2) (session.EnsureResult, error) {
	f.ensureCallsV2++
	f.ensureCommandV2 = command
	return f.ensureResultV2, f.ensureErrV2
}

// EnsureProjectSession 捕获显式领域 DTO，不读取生成类型。
func (f *fakeService) EnsureProjectSession(_ context.Context, command session.EnsureCommand) (session.EnsureResult, error) {
	f.ensureCalls++
	f.ensureCommand = command
	return f.ensureResult, f.ensureErr
}

// QueryProjectSessionCommand 捕获 Query DTO 并返回测试三态。
func (f *fakeService) QueryProjectSessionCommand(_ context.Context, command session.QueryCommand) (session.QueryCommandResult, error) {
	f.queryCalls++
	f.queryCommand = command
	return f.queryResult, f.queryErr
}

// TestEnsureProjectSessionV1RecalculatesUnicodeDigest 验证 Transport 独立接受 NFC 固定向量并映射安全 Receipt。
func TestEnsureProjectSessionV1RecalculatesUnicodeDigest(t *testing.T) {
	messageID := testMessageID
	inputID := testInputID
	service := &fakeService{ensureResult: session.EnsureResult{
		CommandID: testCommandID, SessionID: testSessionID, MessageID: &messageID, InputID: &inputID,
		Disposition: session.EnsureDispositionCreated, ResultVersion: 1,
		AcceptedAt: time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC),
	}}
	handler, err := NewHandler(service)
	if err != nil {
		t.Fatalf("创建 Handler 失败: %v", err)
	}
	prompt := " e\u0301 "
	response, err := handler.EnsureProjectSessionV1(context.Background(), validEnsureRequest(&prompt))
	if err != nil {
		t.Fatalf("Ensure RPC 失败: %v", err)
	}
	if service.ensureCalls != 1 || service.ensureCommand.RequestDigest != testRequestHash ||
		service.ensureCommand.PromptDigest != testPromptHash || service.ensureCommand.InitialPrompt != prompt {
		t.Fatalf("领域命令映射错误: %+v", service.ensureCommand)
	}
	if response.Disposition != sessionv1.EnsureDispositionV1_CREATED || response.RequestId != testRequestID ||
		response.Receipt == nil || response.Receipt.SessionId != testSessionID ||
		response.Receipt.GetMessageId() != testMessageID || response.Receipt.GetInputId() != testInputID {
		t.Fatalf("Ensure 响应映射错误: %+v", response)
	}
}

// TestEnsureProjectSessionV1RejectsUntrustedDigestAndEnum 验证伪造摘要和未知枚举在调用领域用例前失败关闭。
func TestEnsureProjectSessionV1RejectsUntrustedDigestAndEnum(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*sessionv1.EnsureProjectSessionRequestV1)
	}{
		{name: "request digest mismatch", mutate: func(request *sessionv1.EnsureProjectSessionRequestV1) {
			request.RequestDigest = strings.Repeat("a", 64)
		}},
		{name: "prompt digest mismatch", mutate: func(request *sessionv1.EnsureProjectSessionRequestV1) {
			request.PromptDigest = strings.Repeat("b", 64)
		}},
		{name: "unknown creation source", mutate: func(request *sessionv1.EnsureProjectSessionRequestV1) {
			request.CreationSource = sessionv1.CreationSourceV1(99)
		}},
		{name: "unknown skill mode", mutate: func(request *sessionv1.EnsureProjectSessionRequestV1) {
			request.SkillSnapshotMode = sessionv1.SkillSnapshotModeV1(99)
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &fakeService{}
			handler, _ := NewHandler(service)
			prompt := " e\u0301 "
			request := validEnsureRequest(&prompt)
			testCase.mutate(request)
			_, err := handler.EnsureProjectSessionV1(context.Background(), request)
			var serviceErr *sessionv1.SessionServiceExceptionV1
			if !errors.As(err, &serviceErr) || serviceErr.Code != errorCodeInvalidArgument || serviceErr.Retryable {
				t.Fatalf("错误=%v", err)
			}
			if service.ensureCalls != 0 {
				t.Fatalf("非法请求调用了领域用例 %d 次", service.ensureCalls)
			}
		})
	}
}

// TestEnsureProjectSessionV1MapsUnicodeWhitespaceToAbsentPrompt 验证纯 Unicode 空白保留传输输入但权威摘要折叠为空。
func TestEnsureProjectSessionV1MapsUnicodeWhitespaceToAbsentPrompt(t *testing.T) {
	service := &fakeService{ensureResult: session.EnsureResult{
		CommandID: testCommandID, SessionID: testSessionID,
		Disposition: session.EnsureDispositionReplayed, ResultVersion: 1, AcceptedAt: time.Now().UTC(),
	}}
	handler, _ := NewHandler(service)
	prompt := "\u00a0\u3000"
	requestDigest, _, _, err := session.CalculateRequestDigest(testProjectID, testOwnerID, prompt, session.SkillSnapshotKindEmpty)
	if err != nil {
		t.Fatalf("计算空 Prompt 摘要失败: %v", err)
	}
	request := validEnsureRequest(&prompt)
	request.RequestDigest = requestDigest
	request.PromptDigest = ""
	response, err := handler.EnsureProjectSessionV1(context.Background(), request)
	if err != nil {
		t.Fatalf("空 Prompt Ensure 失败: %v", err)
	}
	if service.ensureCommand.PromptDigest != "" || response.Receipt.MessageId != nil || response.Receipt.InputId != nil {
		t.Fatalf("空 Prompt 产生了内容事实: command=%+v response=%+v", service.ensureCommand, response)
	}
	if response.Disposition != sessionv1.EnsureDispositionV1_REPLAYED {
		t.Fatalf("重放状态=%v", response.Disposition)
	}
}

// TestQueryProjectSessionCommandV1MapsThreeStates 验证 Unknown Outcome Query 严格映射 not_found/completed/conflict。
func TestQueryProjectSessionCommandV1MapsThreeStates(t *testing.T) {
	receipt := session.EnsureResult{
		CommandID: testCommandID, SessionID: testSessionID, Disposition: session.EnsureDispositionReplayed,
		ResultVersion: 1, AcceptedAt: time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC),
	}
	testCases := []struct {
		name        string
		result      session.QueryCommandResult
		wantStatus  sessionv1.QueryProjectSessionCommandStatusV1
		wantReceipt bool
	}{
		{name: "not found", result: session.QueryCommandResult{Status: session.QueryCommandStatusNotFound}, wantStatus: sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND},
		{name: "completed", result: session.QueryCommandResult{Status: session.QueryCommandStatusCompleted, Receipt: &receipt}, wantStatus: sessionv1.QueryProjectSessionCommandStatusV1_COMPLETED, wantReceipt: true},
		{name: "conflict", result: session.QueryCommandResult{Status: session.QueryCommandStatusConflict}, wantStatus: sessionv1.QueryProjectSessionCommandStatusV1_CONFLICT},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &fakeService{queryResult: testCase.result}
			handler, _ := NewHandler(service)
			response, err := handler.QueryProjectSessionCommandV1(context.Background(), validQueryRequest())
			if err != nil {
				t.Fatalf("Query RPC 失败: %v", err)
			}
			if response.Status != testCase.wantStatus || (response.Receipt != nil) != testCase.wantReceipt {
				t.Fatalf("Query 响应=%+v", response)
			}
			if service.queryCalls != 1 || service.queryCommand.ExpectedRequestDigest != testRequestHash {
				t.Fatalf("Query DTO 映射错误: %+v", service.queryCommand)
			}
		})
	}
}

// TestHandlerErrorsNeverLeakInternalDetails 验证 SQL、Prompt 和密钥服务详情不会进入业务异常。
func TestHandlerErrorsNeverLeakInternalDetails(t *testing.T) {
	service := &fakeService{ensureErr: errors.New("SELECT prompt FROM secret_table key=top-secret")}
	handler, _ := NewHandler(service)
	prompt := " e\u0301 "
	_, err := handler.EnsureProjectSessionV1(context.Background(), validEnsureRequest(&prompt))
	var serviceErr *sessionv1.SessionServiceExceptionV1
	if !errors.As(err, &serviceErr) || serviceErr.Code != errorCodeInternal {
		t.Fatalf("错误=%v", err)
	}
	if strings.Contains(serviceErr.Error(), "SELECT") || strings.Contains(serviceErr.Error(), "top-secret") ||
		strings.Contains(serviceErr.Error(), prompt) {
		t.Fatalf("业务异常泄漏内部详情: %v", serviceErr)
	}
}

// TestHandlerMapsStableServiceErrors 验证领域冲突、保护、持久化与 Context 错误映射为稳定 code/retryable。
func TestHandlerMapsStableServiceErrors(t *testing.T) {
	testCases := []struct {
		name      string
		err       error
		wantCode  string
		retryable bool
	}{
		{name: "idempotency conflict", err: session.ErrCommandConflict, wantCode: errorCodeIdempotencyConflict},
		{name: "project conflict", err: session.ErrProjectSessionConflict, wantCode: errorCodeProjectSessionConflict},
		{name: "content protection", err: session.ErrContentProtection, wantCode: errorCodeContentProtectionFailed, retryable: true},
		{name: "persistence", err: session.ErrPersistence, wantCode: errorCodePersistenceUnavailable, retryable: true},
		{name: "deadline", err: context.DeadlineExceeded, wantCode: errorCodeDeadlineExceeded, retryable: true},
		{name: "canceled", err: context.Canceled, wantCode: errorCodeRequestCanceled},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &fakeService{ensureErr: testCase.err}
			handler, _ := NewHandler(service)
			prompt := " e\u0301 "
			_, err := handler.EnsureProjectSessionV1(context.Background(), validEnsureRequest(&prompt))
			var serviceErr *sessionv1.SessionServiceExceptionV1
			if !errors.As(err, &serviceErr) || serviceErr.Code != testCase.wantCode ||
				serviceErr.Retryable != testCase.retryable || serviceErr.RequestId != testRequestID {
				t.Fatalf("稳定错误=%v", err)
			}
		})
	}
}

// TestHandlerRejectsMalformedDomainReceipt 验证领域层返回空或错命令 Receipt 时协议边界失败关闭。
func TestHandlerRejectsMalformedDomainReceipt(t *testing.T) {
	service := &fakeService{ensureResult: session.EnsureResult{
		CommandID: "019f0000-0000-7000-8000-000000000099",
		SessionID: testSessionID, Disposition: session.EnsureDispositionCreated,
		ResultVersion: 1, AcceptedAt: time.Now().UTC(),
	}}
	handler, _ := NewHandler(service)
	prompt := " e\u0301 "
	_, err := handler.EnsureProjectSessionV1(context.Background(), validEnsureRequest(&prompt))
	var serviceErr *sessionv1.SessionServiceExceptionV1
	if !errors.As(err, &serviceErr) || serviceErr.Code != errorCodeInternal || serviceErr.Retryable {
		t.Fatalf("畸形领域 Receipt 错误=%v", err)
	}
}

// TestQueryProjectSessionCommandV1RejectsInvalidDigest 验证 Query 不接受大写、短值或非十六进制摘要。
func TestQueryProjectSessionCommandV1RejectsInvalidDigest(t *testing.T) {
	for _, digest := range []string{"ABC", strings.Repeat("A", 64), strings.Repeat("z", 64)} {
		service := &fakeService{}
		handler, _ := NewHandler(service)
		request := validQueryRequest()
		request.ExpectedRequestDigest = digest
		_, err := handler.QueryProjectSessionCommandV1(context.Background(), request)
		var serviceErr *sessionv1.SessionServiceExceptionV1
		if !errors.As(err, &serviceErr) || serviceErr.Code != errorCodeInvalidArgument || service.queryCalls != 0 {
			t.Fatalf("非法 Query Digest=%q err=%v calls=%d", digest, err, service.queryCalls)
		}
	}
}

// validEnsureRequest 构造跨 Module 固定向量请求。
func validEnsureRequest(prompt *string) *sessionv1.EnsureProjectSessionRequestV1 {
	return &sessionv1.EnsureProjectSessionRequestV1{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION,
		RequestId:     testRequestID, CommandId: testCommandID, RequestDigest: testRequestHash,
		ProjectId: testProjectID, OwnerUserId: testOwnerID,
		CreationSource: sessionv1.CreationSourceV1_QUICK_CREATE, InitialPrompt: prompt,
		PromptDigest: testPromptHash, SkillSnapshotMode: sessionv1.SkillSnapshotModeV1_EMPTY,
		RequestedAtUnixMs: time.Date(2026, 7, 14, 5, 59, 0, 0, time.UTC).UnixMilli(),
	}
}

// validQueryRequest 构造严格 v1 Query 请求。
func validQueryRequest() *sessionv1.QueryProjectSessionCommandRequestV1 {
	return &sessionv1.QueryProjectSessionCommandRequestV1{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION,
		RequestId:     testRequestID, CommandId: testCommandID, ExpectedRequestDigest: testRequestHash,
	}
}
