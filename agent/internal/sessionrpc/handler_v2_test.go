package sessionrpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
)

const (
	testEmptySnapshotDigestV2 = "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"
	testRuntimeDigestV2       = "d81700e078c331dc271db6d9c7c169f75f48f9fd89f944671883316044594168"
	testSnapshotDigestV2      = "69ef1ba7ca41c90986204308043cb4587097ce3d4edbcea921b00eafc7cdfcdc"
	testEmptyRequestDigestV2  = "904b88d91a452522b95b0925e61ac94d93e89def4af29944ff563a4ff9ffc1b5"
	testRequestDigestV2       = "2dcc22f80c546ff992c2f3d82a9252adc338deb1d4805b14f9477f66bdab52f1"
)

// TestEnsureProjectSessionV2MapsExplicitEmptySnapshot 验证显式 empty 仍调用 V2，并保持非 nil 空列表与冻结摘要。
func TestEnsureProjectSessionV2MapsExplicitEmptySnapshot(t *testing.T) {
	messageID := testMessageID
	inputID := testInputID
	service := &fakeService{ensureResultV2: session.EnsureResult{
		CommandID: testCommandID, SessionID: testSessionID, MessageID: &messageID, InputID: &inputID,
		Disposition: session.EnsureDispositionCreated, ResultVersion: session.ResultVersionV2,
		AcceptedAt:          time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC),
		SkillSnapshotDigest: testEmptySnapshotDigestV2, SkillCount: 0,
	}}
	handler, err := NewHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	response, err := handler.EnsureProjectSessionV2(context.Background(), validEnsureRequestV2(false))
	if err != nil {
		t.Fatalf("Ensure v2 empty 失败: %v", err)
	}
	if service.ensureCallsV2 != 1 || service.ensureCalls != 0 || service.queryCalls != 0 {
		t.Fatalf("必须只调用 V2：v1=%d v2=%d query=%d", service.ensureCalls, service.ensureCallsV2, service.queryCalls)
	}
	command := service.ensureCommandV2
	if command.SchemaVersion != skill.EnsureProjectSessionSchemaVersionV2 || command.RequestDigest != testEmptyRequestDigestV2 ||
		command.PromptDigest != testPromptHash || command.InitialPrompt != " é " ||
		command.SkillSnapshot.SnapshotKind != skill.SessionSkillSnapshotKindEmptyV1 ||
		command.SkillSnapshot.SkillCount != 0 || command.SkillSnapshot.Skills == nil || len(command.SkillSnapshot.Skills) != 0 {
		t.Fatalf("empty V2 command 映射错误: %+v", command)
	}
	if response.SchemaVersion != sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2 ||
		response.Disposition != sessionv1.EnsureDispositionV1_CREATED || response.Receipt == nil ||
		response.Receipt.SkillSnapshotDigest != testEmptySnapshotDigestV2 || response.Receipt.SkillCount != 0 ||
		response.Receipt.GetMessageId() != testMessageID || response.Receipt.GetInputId() != testInputID {
		t.Fatalf("empty V2 response 映射错误: %+v", response)
	}
}

// TestEnsureProjectSessionV2MapsNonEmptySnapshot 验证完整 Runtime、metadata 和能力声明逐字段进入领域 DTO。
func TestEnsureProjectSessionV2MapsNonEmptySnapshot(t *testing.T) {
	messageID := testMessageID
	inputID := testInputID
	service := &fakeService{ensureResultV2: session.EnsureResult{
		CommandID: testCommandID, SessionID: testSessionID, MessageID: &messageID, InputID: &inputID,
		Disposition: session.EnsureDispositionReplayed, ResultVersion: session.ResultVersionV2,
		AcceptedAt:          time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC),
		SkillSnapshotDigest: testSnapshotDigestV2, SkillCount: 1,
	}}
	handler, _ := NewHandler(service)
	response, err := handler.EnsureProjectSessionV2(context.Background(), validEnsureRequestV2(true))
	if err != nil {
		t.Fatalf("Ensure v2 non-empty 失败: %v", err)
	}
	if service.ensureCalls != 0 || service.ensureCallsV2 != 1 {
		t.Fatalf("V2 发生降级：v1=%d v2=%d", service.ensureCalls, service.ensureCallsV2)
	}
	command := service.ensureCommandV2
	if command.RequestDigest != testRequestDigestV2 || command.SkillSnapshot.SkillCount != 1 ||
		command.SkillSnapshot.SnapshotSetDigest != testSnapshotDigestV2 || len(command.SkillSnapshot.Skills) != 1 {
		t.Fatalf("non-empty V2 header 映射错误: %+v", command)
	}
	item := command.SkillSnapshot.Skills[0]
	if item.LoadOrder != 1 || item.Priority != 100 || item.Namespace != skill.SkillNamespaceUserV1 ||
		item.SkillID != "019f0000-0000-7000-8000-000000000101" ||
		item.PublisherUserID != "019f0000-0000-7000-8000-000000000102" ||
		item.PublishedSnapshotID != "019f0000-0000-7000-8000-000000000103" || item.PublicationRevision != 2 ||
		item.DefinitionSchemaVersion != skill.DefinitionSchemaVersionV1 ||
		item.RuntimeContentSchemaVersion != skill.RuntimeContentSchemaVersionV1 || item.RuntimeContentDigest != testRuntimeDigestV2 ||
		item.RuntimeContent.Name != "Prompt helper" || item.RuntimeContent.WritePrompts.Applicability != skill.SkillGuidanceEnabledV1 ||
		item.PermissionSnapshotDigest != "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25" ||
		item.RuntimePolicyRef != skill.RuntimePolicyRefV1 || item.GovernanceEpoch != 3 || item.PublishedAtUnixMS != 1784011500123 ||
		len(item.AllowedGraphToolKeys) != 1 ||
		item.AllowedGraphToolKeys[0] != "write_prompts" || item.PublicToolRefs == nil || len(item.PublicToolRefs) != 0 {
		t.Fatalf("non-empty V2 item 映射错误: %+v", item)
	}
	if response.Disposition != sessionv1.EnsureDispositionV1_REPLAYED || response.Receipt.SkillCount != 1 ||
		response.Receipt.SkillSnapshotDigest != testSnapshotDigestV2 {
		t.Fatalf("non-empty V2 response 映射错误: %+v", response)
	}
}

// TestEnsureProjectSessionV2RejectsDigestTamperingBeforeService 验证 request/prompt/set/runtime 任一摘要篡改都失败关闭。
func TestEnsureProjectSessionV2RejectsDigestTamperingBeforeService(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*sessionv1.EnsureProjectSessionRequestV2)
	}{
		{"request", func(request *sessionv1.EnsureProjectSessionRequestV2) {
			request.RequestDigest = strings.Repeat("0", 64)
		}},
		{"prompt", func(request *sessionv1.EnsureProjectSessionRequestV2) { request.PromptDigest = strings.Repeat("0", 64) }},
		{"snapshot", func(request *sessionv1.EnsureProjectSessionRequestV2) {
			request.SkillSnapshot.SnapshotSetDigest = strings.Repeat("0", 64)
		}},
		{"runtime", func(request *sessionv1.EnsureProjectSessionRequestV2) {
			request.SkillSnapshot.Skills[0].RuntimeContentDigest = strings.Repeat("0", 64)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeService{}
			handler, _ := NewHandler(service)
			request := validEnsureRequestV2(true)
			test.mutate(request)
			_, err := handler.EnsureProjectSessionV2(context.Background(), request)
			assertV2ServiceError(t, err, errorCodeSnapshotDigestMismatchV2, false)
			if service.ensureCalls != 0 || service.ensureCallsV2 != 0 || service.queryCalls != 0 {
				t.Fatalf("摘要篡改进入用例：%+v", service)
			}
		})
	}
}

// TestEnsureProjectSessionV2AppliesEffectiveLimits 验证 Handler 使用启动时冻结的 effective profile，而非协议 ceiling 或静默截断。
func TestEnsureProjectSessionV2AppliesEffectiveLimits(t *testing.T) {
	limits := skill.DefaultLimitsProfileV1()
	limits.MaxItems = 1
	service := &fakeService{}
	handler, err := NewHandlerWithSkillSnapshotLimits(service, limits)
	if err != nil {
		t.Fatal(err)
	}
	request := validEnsureRequestV2(true)
	second := *request.SkillSnapshot.Skills[0]
	request.SkillSnapshot.Skills = append(request.SkillSnapshot.Skills, &second)
	request.SkillSnapshot.SkillCount = 2
	_, err = handler.EnsureProjectSessionV2(context.Background(), request)
	assertV2ServiceError(t, err, errorCodeSnapshotLimitExceededV2, false)
	if service.ensureCalls != 0 || service.ensureCallsV2 != 0 {
		t.Fatalf("超限 Snapshot 进入用例：v1=%d v2=%d", service.ensureCalls, service.ensureCallsV2)
	}

	invalid := skill.DefaultLimitsProfileV1()
	invalid.MaxPublicToolRefsPerItem = 1
	if _, err := NewHandlerWithSkillSnapshotLimits(service, invalid); err == nil {
		t.Fatal("W1 public Tool limits 非零必须阻止 Handler 启动")
	}
}

// TestEnsureProjectSessionV2MapsStableServiceErrors 验证 V2 领域错误收敛为冻结 code 和 retryable。
func TestEnsureProjectSessionV2MapsStableServiceErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		code      string
		retryable bool
	}{
		{"version conflict", session.ErrCommandVersionConflict, errorCodeCommandVersionConflictV2, false},
		{"command conflict", session.ErrCommandConflict, errorCodeCommandConflictV2, false},
		{"project conflict", session.ErrProjectSessionConflict, errorCodeProjectSessionConflict, false},
		{"snapshot limit", session.ErrSnapshotLimitExceeded, errorCodeSnapshotLimitExceededV2, false},
		{"snapshot integrity", session.ErrSnapshotIntegrity, errorCodeSnapshotDigestMismatchV2, false},
		{"content protection", session.ErrContentProtection, errorCodeContentProtectionUnavailableV2, true},
		{"content unavailable", session.ErrContentUnavailable, errorCodeContentProtectionUnavailableV2, true},
		{"persistence", session.ErrPersistence, errorCodePersistenceUnavailable, true},
		{"invalid", session.ErrInvalidCommand, errorCodeInvalidArgument, false},
		{"deadline", context.DeadlineExceeded, errorCodeDeadlineExceeded, true},
		{"canceled", context.Canceled, errorCodeRequestCanceled, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeService{ensureErrV2: test.err}
			handler, _ := NewHandler(service)
			_, err := handler.EnsureProjectSessionV2(context.Background(), validEnsureRequestV2(false))
			assertV2ServiceError(t, err, test.code, test.retryable)
			if service.ensureCalls != 0 || service.ensureCallsV2 != 1 {
				t.Fatalf("V2 错误路径发生降级：v1=%d v2=%d", service.ensureCalls, service.ensureCallsV2)
			}
		})
	}
}

// TestQueryProjectSessionCommandV2MapsThreeStates 验证 Query v2 三态、V2 command type 与安全 Receipt。
func TestQueryProjectSessionCommandV2MapsThreeStates(t *testing.T) {
	receipt := validEnsureResultV2(testEmptySnapshotDigestV2, 0)
	tests := []struct {
		name        string
		result      session.QueryCommandResult
		status      sessionv1.QueryProjectSessionCommandStatusV1
		wantReceipt bool
	}{
		{"not found", session.QueryCommandResult{Status: session.QueryCommandStatusNotFound}, sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND, false},
		{"completed", session.QueryCommandResult{Status: session.QueryCommandStatusCompleted, Receipt: &receipt}, sessionv1.QueryProjectSessionCommandStatusV1_COMPLETED, true},
		{"conflict", session.QueryCommandResult{Status: session.QueryCommandStatusConflict}, sessionv1.QueryProjectSessionCommandStatusV1_CONFLICT, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeService{queryResult: test.result}
			handler, _ := NewHandler(service)
			response, err := handler.QueryProjectSessionCommandV2(context.Background(), validQueryRequestV2())
			if err != nil {
				t.Fatalf("Query v2 失败: %v", err)
			}
			if response.SchemaVersion != sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2 ||
				response.Status != test.status || (response.Receipt != nil) != test.wantReceipt {
				t.Fatalf("Query v2 映射错误: %+v", response)
			}
			if service.queryCalls != 1 || service.queryCommand.ExpectedCommandType != session.CommandTypeEnsureProjectSessionV2 ||
				service.queryCommand.SchemaVersion != session.QueryCommandSchemaVersionV2 ||
				service.ensureCalls != 0 || service.ensureCallsV2 != 0 {
				t.Fatalf("Query v2 调用了错误边界: %+v", service)
			}
		})
	}
}

// TestQueryProjectSessionCommandV2MapsCrossVersionError 验证同 CommandID 命中 V1 Receipt 时不重放、不降级。
func TestQueryProjectSessionCommandV2MapsCrossVersionError(t *testing.T) {
	service := &fakeService{queryErr: session.ErrCommandVersionConflict}
	handler, _ := NewHandler(service)
	_, err := handler.QueryProjectSessionCommandV2(context.Background(), validQueryRequestV2())
	assertV2ServiceError(t, err, errorCodeCommandVersionConflictV2, false)
	if service.queryCalls != 1 || service.ensureCalls != 0 || service.ensureCallsV2 != 0 ||
		service.queryCommand.ExpectedCommandType != session.CommandTypeEnsureProjectSessionV2 {
		t.Fatalf("跨版本 Query 发生写入或降级: %+v", service)
	}
}

// TestQueryProjectSessionCommandV2RejectsMalformedCompletedReceipt 验证 Query 不把越界或 count/digest 矛盾的持久化结果暴露到 wire。
func TestQueryProjectSessionCommandV2RejectsMalformedCompletedReceipt(t *testing.T) {
	tests := []struct {
		name   string
		digest string
		count  int
	}{
		{"effective limit exceeded", testSnapshotDigestV2, skill.DefaultLimitsProfileV1().MaxItems + 1},
		{"empty count with non-empty digest", testSnapshotDigestV2, 0},
		{"non-empty count with empty digest", testEmptySnapshotDigestV2, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			receipt := validEnsureResultV2(test.digest, test.count)
			service := &fakeService{queryResult: session.QueryCommandResult{Status: session.QueryCommandStatusCompleted, Receipt: &receipt}}
			handler, _ := NewHandler(service)
			_, err := handler.QueryProjectSessionCommandV2(context.Background(), validQueryRequestV2())
			assertV2ServiceError(t, err, errorCodeInternal, false)
			if service.queryCalls != 1 || service.ensureCalls != 0 || service.ensureCallsV2 != 0 {
				t.Fatalf("畸形 Query Receipt 触发了写入: %+v", service)
			}
		})
	}
}

// validEnsureRequestV2 构造 Snapshot v2 评审的 empty 或 non-empty 固定向量。
func validEnsureRequestV2(nonEmpty bool) *sessionv1.EnsureProjectSessionRequestV2 {
	prompt := " e\u0301 "
	snapshot := &sessionv1.SessionSkillSnapshotV1{
		SchemaVersion:     sessionv1.SESSION_SKILL_SNAPSHOT_SCHEMA_VERSION_V1,
		SnapshotKind:      sessionv1.SessionSkillSnapshotKindV1_EMPTY,
		SkillCount:        0,
		SnapshotSetDigest: testEmptySnapshotDigestV2,
		Skills:            []*sessionv1.PublishedSkillSnapshotRefV1{},
	}
	requestDigest := testEmptyRequestDigestV2
	if nonEmpty {
		snapshot.SnapshotKind = sessionv1.SessionSkillSnapshotKindV1_PUBLISHED_REFS
		snapshot.SkillCount = 1
		snapshot.SnapshotSetDigest = testSnapshotDigestV2
		snapshot.Skills = []*sessionv1.PublishedSkillSnapshotRefV1{validPublishedSkillV2()}
		requestDigest = testRequestDigestV2
	}
	return &sessionv1.EnsureProjectSessionRequestV2{
		SchemaVersion: sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2,
		RequestId:     testRequestID, CommandId: testCommandID, RequestDigest: requestDigest,
		ProjectId: testProjectID, OwnerUserId: testOwnerID,
		CreationSource: sessionv1.CreationSourceV1_QUICK_CREATE,
		InitialPrompt:  &prompt, PromptDigest: testPromptHash, SkillSnapshot: snapshot,
		RequestedAtUnixMs: time.Date(2026, 7, 14, 6, 59, 0, 0, time.UTC).UnixMilli(),
	}
}

// validPublishedSkillV2 构造 opaque permission golden vector 的完整 Thrift Item。
func validPublishedSkillV2() *sessionv1.PublishedSkillSnapshotRefV1 {
	notApplicable := func() *sessionv1.CapabilityGuidanceV1 {
		return &sessionv1.CapabilityGuidanceV1{
			Applicability: sessionv1.SkillGuidanceApplicabilityV1_NOT_APPLICABLE,
			Guidance:      "", NotApplicableReason: "not used",
		}
	}
	runtime := &sessionv1.SkillRuntimeContentV1{
		SchemaVersion: sessionv1.SKILL_RUNTIME_CONTENT_SCHEMA_VERSION_V1,
		Name:          "Prompt helper", InputDescription: "text", OutputDescription: "prompt",
		InvocationRules:  "Use for prompt writing.",
		PlanCreationSpec: notApplicable(), AnalyzeMaterials: notApplicable(), PlanStoryboard: notApplicable(),
		GenerateMedia: notApplicable(), AssembleOutput: notApplicable(),
		WritePrompts: &sessionv1.CapabilityGuidanceV1{
			Applicability: sessionv1.SkillGuidanceApplicabilityV1_ENABLED,
			Guidance:      "Write concise prompts.", NotApplicableReason: "",
		},
		Examples: []*sessionv1.SkillExampleV1{}, StarterPrompts: []string{"Improve this prompt."},
	}
	return &sessionv1.PublishedSkillSnapshotRefV1{
		LoadOrder: 1, Priority: 100, Namespace: sessionv1.SkillNamespaceV1_USER,
		SkillId:             "019f0000-0000-7000-8000-000000000101",
		PublisherUserId:     "019f0000-0000-7000-8000-000000000102",
		PublishedSnapshotId: "019f0000-0000-7000-8000-000000000103",
		PublicationRevision: 2, DefinitionSchemaVersion: "skill_definition.v1",
		ContentDigest:               "dc18b1bbe2824f462cbef7373e48074d609cdd4d57897dd87e1b26c85b96d513",
		RuntimeContentSchemaVersion: sessionv1.SKILL_RUNTIME_CONTENT_SCHEMA_VERSION_V1,
		RuntimeContentDigest:        testRuntimeDigestV2, RuntimeContent: runtime,
		AllowedGraphToolKeys: []string{"write_prompts"}, PublicToolRefs: []*sessionv1.PublicToolSnapshotRefV1{},
		PermissionSnapshotDigest: "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25",
		RuntimePolicyRef:         skill.RuntimePolicyRefV1, GovernanceEpoch: 3, PublishedAtUnixMs: 1784011500123,
	}
}

// validQueryRequestV2 构造严格 V2 Query 请求。
func validQueryRequestV2() *sessionv1.QueryProjectSessionCommandRequestV2 {
	return &sessionv1.QueryProjectSessionCommandRequestV2{
		SchemaVersion: sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2,
		RequestId:     testRequestID, CommandId: testCommandID, ExpectedRequestDigest: testEmptyRequestDigestV2,
	}
}

// validEnsureResultV2 构造不含 Prompt 的 Query completed 冻结结果。
func validEnsureResultV2(snapshotDigest string, skillCount int) session.EnsureResult {
	return session.EnsureResult{
		CommandID: testCommandID, SessionID: testSessionID,
		Disposition: session.EnsureDispositionReplayed, ResultVersion: session.ResultVersionV2,
		AcceptedAt:          time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC),
		SkillSnapshotDigest: snapshotDigest, SkillCount: skillCount,
	}
}

// assertV2ServiceError 校验稳定 code、重试语义和安全 request_id。
func assertV2ServiceError(t *testing.T, err error, code string, retryable bool) {
	t.Helper()
	var serviceErr *sessionv1.SessionServiceExceptionV1
	if !errors.As(err, &serviceErr) || serviceErr.Code != code || serviceErr.Retryable != retryable ||
		serviceErr.RequestId != testRequestID {
		t.Fatalf("V2 stable error mismatch: err=%v parsed=%+v", err, serviceErr)
	}
}
